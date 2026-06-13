package issuesync

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/yuqitao1024/alter-ego/internal/bitable"
	"github.com/yuqitao1024/alter-ego/internal/gitcode"
)

func TestServiceApplyIssueEventCreatesMissingRow(t *testing.T) {
	t.Parallel()

	fields := bitable.FieldMapping{
		IssueKey:    "IssueKey",
		IssueIID:    "IssueIID",
		Title:       "Title",
		Description: "Description",
		State:       "State",
		Action:      "Action",
		Labels:      "Labels",
		IssueURL:    "IssueURL",
		CreatedAt:   "CreatedAt",
		UpdatedAt:   "UpdatedAt",
		LastActor:   "LastActor",
	}
	table := &stubTableClient{}
	service := NewService(table, fields)

	createdAt := time.Date(2026, 6, 13, 8, 0, 0, 123000000, time.UTC)
	updatedAt := createdAt.Add(10 * time.Minute)
	event := gitcode.IssueEvent{
		IssueKey:    "org/repo/issues/18",
		IssueIID:    18,
		Title:       "Sync issue row",
		Description: "Populate fields",
		State:       "opened",
		Action:      "open",
		Labels:      []string{"backend", "p1"},
		IssueURL:    "https://gitcode.example/org/repo/-/issues/18",
		LastActor:   "alice",
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	if err := service.ApplyIssueEvent(context.Background(), event); err != nil {
		t.Fatalf("ApplyIssueEvent returned error: %v", err)
	}

	if len(table.created) != 1 {
		t.Fatalf("create calls = %d, want 1", len(table.created))
	}
	if len(table.updated) != 0 {
		t.Fatalf("update calls = %d, want 0", len(table.updated))
	}

	want := map[string]any{
		"IssueKey":    "org/repo/issues/18",
		"IssueIID":    18,
		"Title":       "Sync issue row",
		"Description": "Populate fields",
		"State":       "opened",
		"Action":      "open",
		"Labels":      "backend\np1",
		"IssueURL":    "https://gitcode.example/org/repo/-/issues/18",
		"CreatedAt":   createdAt.UTC().Format(time.RFC3339Nano),
		"UpdatedAt":   updatedAt.UTC().Format(time.RFC3339Nano),
		"LastActor":   "alice",
	}
	if !reflect.DeepEqual(table.created[0], want) {
		t.Fatalf("created fields = %#v, want %#v", table.created[0], want)
	}
}

func TestServiceApplyMergeRequestEventUpdatesExistingRow(t *testing.T) {
	t.Parallel()

	fields := bitable.FieldMapping{
		IssueKey:        "IssueKey",
		RelatedPRs:      "RelatedPRs",
		RelatedPRURLs:   "RelatedPRURLs",
		RelatedPRStatus: "RelatedPRStatus",
		LastPRUpdatedAt: "LastPRUpdatedAt",
	}
	table := &stubTableClient{
		findResults: map[string]stubFindResult{
			"org/repo/issues/18": {
				recordID: "rec_18",
				fields: map[string]any{
					"IssueKey":        "org/repo/issues/18",
					"RelatedPRs":      "!3 Existing PR [merged]\n!9 Old title [opened]",
					"RelatedPRURLs":   "!3 https://gitcode.example/org/repo/-/merge_requests/3\n!9 https://gitcode.example/org/repo/-/merge_requests/9",
					"RelatedPRStatus": "!3 merged\n!9 opened",
					"LastPRUpdatedAt": "2026-06-13T07:50:00Z",
				},
			},
		},
	}
	service := NewService(table, fields)

	updatedAt := time.Date(2026, 6, 13, 8, 5, 0, 0, time.UTC)
	event := gitcode.MergeRequestEvent{
		PullRequestIID:      9,
		Title:               "New title",
		State:               "merged",
		URL:                 "https://gitcode.example/org/repo/-/merge_requests/9",
		UpdatedAt:           updatedAt,
		AssociatedIssueKeys: []string{"org/repo/issues/18"},
	}

	if err := service.ApplyMergeRequestEvent(context.Background(), event); err != nil {
		t.Fatalf("ApplyMergeRequestEvent returned error: %v", err)
	}

	if len(table.updated) != 1 {
		t.Fatalf("update calls = %d, want 1", len(table.updated))
	}
	if table.updated[0].recordID != "rec_18" {
		t.Fatalf("update recordID = %q, want rec_18", table.updated[0].recordID)
	}

	want := map[string]any{
		"RelatedPRs":      "!3 Existing PR [merged]\n!9 New title [merged]",
		"RelatedPRURLs":   "!3 https://gitcode.example/org/repo/-/merge_requests/3\n!9 https://gitcode.example/org/repo/-/merge_requests/9",
		"RelatedPRStatus": "!3 merged\n!9 merged",
		"LastPRUpdatedAt": updatedAt.UTC().Format(time.RFC3339Nano),
	}
	if !reflect.DeepEqual(table.updated[0].fields, want) {
		t.Fatalf("updated fields = %#v, want %#v", table.updated[0].fields, want)
	}
}

func TestServiceApplyMergeRequestEventSkipsMissingRows(t *testing.T) {
	t.Parallel()

	table := &stubTableClient{}
	service := NewService(table, bitable.FieldMapping{IssueKey: "IssueKey"})

	event := gitcode.MergeRequestEvent{
		PullRequestIID:      12,
		Title:               "Ignore missing",
		State:               "opened",
		URL:                 "https://gitcode.example/org/repo/-/merge_requests/12",
		UpdatedAt:           time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC),
		AssociatedIssueKeys: []string{"org/repo/issues/99"},
	}

	if err := service.ApplyMergeRequestEvent(context.Background(), event); err != nil {
		t.Fatalf("ApplyMergeRequestEvent returned error: %v", err)
	}
	if len(table.updated) != 0 {
		t.Fatalf("update calls = %d, want 0", len(table.updated))
	}
}

func TestServiceApplyMergeRequestEventPreservesNewerNumericLastPRUpdatedAt(t *testing.T) {
	t.Parallel()

	fields := bitable.FieldMapping{
		IssueKey:        "IssueKey",
		RelatedPRs:      "RelatedPRs",
		RelatedPRURLs:   "RelatedPRURLs",
		RelatedPRStatus: "RelatedPRStatus",
		LastPRUpdatedAt: "LastPRUpdatedAt",
	}
	newerStored := time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
	table := &stubTableClient{
		findResults: map[string]stubFindResult{
			"org/repo/issues/18": {
				recordID: "rec_18",
				fields: map[string]any{
					"IssueKey":        "org/repo/issues/18",
					"RelatedPRs":      "!9 Existing PR [merged]",
					"RelatedPRURLs":   "!9 https://gitcode.example/org/repo/-/merge_requests/9",
					"RelatedPRStatus": "!9 merged",
					"LastPRUpdatedAt": float64(newerStored.UnixMilli()),
				},
			},
		},
	}
	service := NewService(table, fields)

	olderEvent := gitcode.MergeRequestEvent{
		PullRequestIID:      9,
		Title:               "Existing PR",
		State:               "merged",
		URL:                 "https://gitcode.example/org/repo/-/merge_requests/9",
		UpdatedAt:           newerStored.Add(-30 * time.Minute),
		AssociatedIssueKeys: []string{"org/repo/issues/18"},
	}

	if err := service.ApplyMergeRequestEvent(context.Background(), olderEvent); err != nil {
		t.Fatalf("ApplyMergeRequestEvent returned error: %v", err)
	}

	if len(table.updated) != 1 {
		t.Fatalf("update calls = %d, want 1", len(table.updated))
	}
	if got := table.updated[0].fields["LastPRUpdatedAt"]; got != newerStored.Format(time.RFC3339Nano) {
		t.Fatalf("LastPRUpdatedAt = %#v, want %q", got, newerStored.Format(time.RFC3339Nano))
	}
}

func TestServiceApplyMergeRequestEventDeduplicatesWhitespacePaddedIssueKeys(t *testing.T) {
	t.Parallel()

	fields := bitable.FieldMapping{
		IssueKey:        "IssueKey",
		RelatedPRs:      "RelatedPRs",
		RelatedPRURLs:   "RelatedPRURLs",
		RelatedPRStatus: "RelatedPRStatus",
		LastPRUpdatedAt: "LastPRUpdatedAt",
	}
	table := &stubTableClient{
		findResults: map[string]stubFindResult{
			"org/repo/issues/18": {
				recordID: "rec_18",
				fields: map[string]any{
					"IssueKey": "org/repo/issues/18",
				},
			},
		},
	}
	service := NewService(table, fields)

	event := gitcode.MergeRequestEvent{
		PullRequestIID: 21,
		Title:          "Normalize issue keys",
		State:          "opened",
		URL:            "https://gitcode.example/org/repo/-/merge_requests/21",
		UpdatedAt:      time.Date(2026, 6, 13, 10, 0, 0, 0, time.UTC),
		AssociatedIssueKeys: []string{
			" org/repo/issues/18 ",
			"org/repo/issues/18",
			"",
			"  ",
		},
	}

	if err := service.ApplyMergeRequestEvent(context.Background(), event); err != nil {
		t.Fatalf("ApplyMergeRequestEvent returned error: %v", err)
	}

	if len(table.findCalls) != 1 {
		t.Fatalf("find calls = %d, want 1", len(table.findCalls))
	}
	if len(table.updated) != 1 {
		t.Fatalf("update calls = %d, want 1", len(table.updated))
	}
}

func TestParseMappedTimeAcceptsNumericStringMilliseconds(t *testing.T) {
	t.Parallel()

	stored := time.Date(2026, 6, 13, 11, 0, 0, 0, time.UTC)
	fields := map[string]any{
		"LastPRUpdatedAt": strconv.FormatInt(stored.UnixMilli(), 10),
	}

	got := parseMappedTime(fields, "LastPRUpdatedAt")
	if !got.Equal(stored) {
		t.Fatalf("parseMappedTime() = %s, want %s", got, stored)
	}
}

type stubTableClient struct {
	findResults map[string]stubFindResult
	findCalls   []string
	created     []map[string]any
	updated     []stubUpdateCall
}

type stubFindResult struct {
	recordID string
	fields   map[string]any
	err      error
}

type stubUpdateCall struct {
	recordID string
	fields   map[string]any
}

func (s *stubTableClient) FindRecordByIssueKey(_ context.Context, issueKey string) (string, map[string]any, error) {
	s.findCalls = append(s.findCalls, issueKey)
	result, ok := s.findResults[issueKey]
	if !ok {
		return "", nil, nil
	}
	return result.recordID, cloneMap(result.fields), result.err
}

func (s *stubTableClient) CreateRecord(_ context.Context, fields map[string]any) (string, error) {
	s.created = append(s.created, cloneMap(fields))
	return "rec_created", nil
}

func (s *stubTableClient) UpdateRecord(_ context.Context, recordID string, fields map[string]any) error {
	s.updated = append(s.updated, stubUpdateCall{recordID: recordID, fields: cloneMap(fields)})
	return nil
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
