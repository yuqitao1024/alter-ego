package issuesync

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yuqitao1024/alter-ego/internal/bitable"
	"github.com/yuqitao1024/alter-ego/internal/gitcode"
)

type TableClient interface {
	FindRecordByIssueKey(ctx context.Context, issueKey string) (string, map[string]any, error)
	CreateRecord(ctx context.Context, fields map[string]any) (string, error)
	UpdateRecord(ctx context.Context, recordID string, fields map[string]any) error
}

type Service struct {
	table  TableClient
	fields bitable.FieldMapping
}

func NewService(table TableClient, fields bitable.FieldMapping) *Service {
	return &Service{
		table:  table,
		fields: fields,
	}
}

func (s *Service) ApplyIssueEvent(ctx context.Context, event gitcode.IssueEvent) error {
	recordID, _, err := s.table.FindRecordByIssueKey(ctx, event.IssueKey)
	if err != nil {
		return fmt.Errorf("find issue row %q: %w", event.IssueKey, err)
	}

	fields := s.issueFields(event)
	if recordID == "" {
		if _, err := s.table.CreateRecord(ctx, fields); err != nil {
			return fmt.Errorf("create issue row %q: %w", event.IssueKey, err)
		}
		return nil
	}

	if err := s.table.UpdateRecord(ctx, recordID, fields); err != nil {
		return fmt.Errorf("update issue row %q: %w", event.IssueKey, err)
	}
	return nil
}

func (s *Service) ApplyMergeRequestEvent(ctx context.Context, event gitcode.MergeRequestEvent) error {
	seen := make(map[string]struct{}, len(event.AssociatedIssueKeys))
	for _, issueKey := range event.AssociatedIssueKeys {
		issueKey = strings.TrimSpace(issueKey)
		if issueKey == "" {
			continue
		}
		if _, ok := seen[issueKey]; ok {
			continue
		}
		seen[issueKey] = struct{}{}

		recordID, existingFields, err := s.table.FindRecordByIssueKey(ctx, issueKey)
		if err != nil {
			return fmt.Errorf("find issue row %q: %w", issueKey, err)
		}
		if recordID == "" {
			continue
		}

		fields := s.mergeRequestFields(existingFields, event)
		if len(fields) == 0 {
			continue
		}
		if err := s.table.UpdateRecord(ctx, recordID, fields); err != nil {
			return fmt.Errorf("update issue row %q for merge request !%d: %w", issueKey, event.PullRequestIID, err)
		}
	}
	return nil
}

func (s *Service) issueFields(event gitcode.IssueEvent) map[string]any {
	fields := make(map[string]any)

	assign(fields, s.fields.IssueKey, event.IssueKey)
	assign(fields, s.fields.IssueIID, event.IssueIID)
	assign(fields, s.fields.Title, event.Title)
	assign(fields, s.fields.Description, event.Description)
	assign(fields, s.fields.State, event.State)
	assign(fields, s.fields.Action, event.Action)
	assign(fields, s.fields.Labels, strings.Join(event.Labels, "\n"))
	assign(fields, s.fields.IssueURL, event.IssueURL)
	assign(fields, s.fields.CreatedAt, formatTime(event.CreatedAt))
	assign(fields, s.fields.UpdatedAt, formatTime(event.UpdatedAt))
	assign(fields, s.fields.LastActor, event.LastActor)

	return fields
}

func (s *Service) mergeRequestFields(existingFields map[string]any, event gitcode.MergeRequestEvent) map[string]any {
	entries := parsePRProjection(existingFields, s.fields)

	entry := entries[event.PullRequestIID]
	entry.iid = event.PullRequestIID
	entry.summary = event.SummaryLine()
	entry.urlLine = fmt.Sprintf("!%d %s", event.PullRequestIID, strings.TrimSpace(event.URL))
	entry.statusLine = fmt.Sprintf("!%d %s", event.PullRequestIID, strings.TrimSpace(event.State))
	entries[event.PullRequestIID] = entry

	summaries, urls, statuses := renderPRProjection(entries)
	fields := make(map[string]any)

	assign(fields, s.fields.RelatedPRs, strings.Join(summaries, "\n"))
	assign(fields, s.fields.RelatedPRURLs, strings.Join(urls, "\n"))
	assign(fields, s.fields.RelatedPRStatus, strings.Join(statuses, "\n"))

	lastPRUpdatedAt := maxTime(parseMappedTime(existingFields, s.fields.LastPRUpdatedAt), event.UpdatedAt)
	assign(fields, s.fields.LastPRUpdatedAt, formatTime(lastPRUpdatedAt))

	return fields
}

type prProjection struct {
	iid        int
	summary    string
	urlLine    string
	statusLine string
}

func parsePRProjection(existingFields map[string]any, fields bitable.FieldMapping) map[int]prProjection {
	entries := make(map[int]prProjection)

	mergeProjectionField(entries, fieldString(existingFields, fields.RelatedPRs), func(entry *prProjection, line string) {
		entry.summary = line
	})
	mergeProjectionField(entries, fieldString(existingFields, fields.RelatedPRURLs), func(entry *prProjection, line string) {
		entry.urlLine = line
	})
	mergeProjectionField(entries, fieldString(existingFields, fields.RelatedPRStatus), func(entry *prProjection, line string) {
		entry.statusLine = line
	})

	return entries
}

func mergeProjectionField(entries map[int]prProjection, raw string, apply func(*prProjection, string)) {
	for _, line := range splitLines(raw) {
		iid, ok := parsePRIID(line)
		if !ok {
			continue
		}
		entry := entries[iid]
		entry.iid = iid
		apply(&entry, line)
		entries[iid] = entry
	}
}

func renderPRProjection(entries map[int]prProjection) ([]string, []string, []string) {
	ids := make([]int, 0, len(entries))
	for iid, entry := range entries {
		if entry.summary == "" && entry.urlLine == "" && entry.statusLine == "" {
			continue
		}
		ids = append(ids, iid)
	}
	sort.Ints(ids)

	summaries := make([]string, 0, len(ids))
	urls := make([]string, 0, len(ids))
	statuses := make([]string, 0, len(ids))
	for _, iid := range ids {
		entry := entries[iid]
		if entry.summary != "" {
			summaries = append(summaries, entry.summary)
		}
		if entry.urlLine != "" {
			urls = append(urls, entry.urlLine)
		}
		if entry.statusLine != "" {
			statuses = append(statuses, entry.statusLine)
		}
	}

	return summaries, urls, statuses
}

func assign(fields map[string]any, name string, value any) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	switch v := value.(type) {
	case string:
		fields[name] = v
	default:
		fields[name] = value
	}
}

func fieldString(fields map[string]any, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || fields == nil {
		return ""
	}

	value, ok := fields[name]
	if !ok || value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func splitLines(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			lines = append(lines, part)
		}
	}
	return lines
}

func parsePRIID(line string) (int, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "!") {
		return 0, false
	}

	end := strings.IndexByte(line, ' ')
	if end == -1 {
		end = len(line)
	}

	iid, err := strconv.Atoi(line[1:end])
	if err != nil {
		return 0, false
	}
	return iid, true
}

func parseMappedTime(fields map[string]any, name string) time.Time {
	name = strings.TrimSpace(name)
	if name == "" || fields == nil {
		return time.Time{}
	}

	value, ok := fields[name]
	if !ok || value == nil {
		return time.Time{}
	}

	switch v := value.(type) {
	case float64:
		return unixMillisTime(int64(math.Round(v)))
	case float32:
		return unixMillisTime(int64(math.Round(float64(v))))
	case int:
		return unixMillisTime(int64(v))
	case int8:
		return unixMillisTime(int64(v))
	case int16:
		return unixMillisTime(int64(v))
	case int32:
		return unixMillisTime(int64(v))
	case int64:
		return unixMillisTime(v)
	case uint:
		return unixMillisTime(int64(v))
	case uint8:
		return unixMillisTime(int64(v))
	case uint16:
		return unixMillisTime(int64(v))
	case uint32:
		return unixMillisTime(int64(v))
	case uint64:
		if v > math.MaxInt64 {
			return time.Time{}
		}
		return unixMillisTime(int64(v))
	case string:
		return parseMappedTimeString(v)
	case fmt.Stringer:
		return parseMappedTimeString(v.String())
	default:
		return parseMappedTimeString(fmt.Sprint(v))
	}
}

func parseMappedTimeString(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}

	if millis, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return unixMillisTime(millis)
	}

	t, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return t
	}
	t, err = time.Parse(time.RFC3339, raw)
	if err == nil {
		return t
	}
	return time.Time{}
}

func unixMillisTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func maxTime(left, right time.Time) time.Time {
	switch {
	case left.IsZero():
		return right
	case right.IsZero():
		return left
	case left.After(right):
		return left
	default:
		return right
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
