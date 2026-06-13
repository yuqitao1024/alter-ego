package gitcode

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type IssueEvent struct {
	DeliveryID  string
	EventUUID   string
	IssueKey    string
	IssueIID    int
	Title       string
	Description string
	State       string
	Action      string
	Labels      []string
	IssueURL    string
	LastActor   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MergeRequestEvent struct {
	DeliveryID          string
	EventUUID           string
	PullRequestIID      int
	Title               string
	State               string
	Action              string
	URL                 string
	SourceBranch        string
	TargetBranch        string
	UpdatedAt           time.Time
	LastActor           string
	AssociatedIssueKeys []string
}

func (e MergeRequestEvent) SummaryLine() string {
	return fmt.Sprintf("!%d %s [%s]", e.PullRequestIID, e.Title, e.State)
}

type webhookEnvelope struct {
	UUID       string            `json:"uuid"`
	EventType  string            `json:"event_type"`
	ObjectKind string            `json:"object_kind"`
	Labels     []webhookLabel    `json:"labels"`
	Issues     []webhookIssueRef `json:"issues"`
	User       webhookUser       `json:"user"`

	ObjectAttributes webhookObjectAttributes `json:"object_attributes"`
}

type webhookLabel struct {
	Title string `json:"title"`
}

type webhookIssueRef struct {
	IID int    `json:"iid"`
	URL string `json:"url"`
}

type webhookUser struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}

type webhookObjectAttributes struct {
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	State        string `json:"state"`
	Action       string `json:"action"`
	URL          string `json:"url"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func VerifyRequest(cfg Config, header http.Header, body []byte) error {
	switch cfg.VerificationMode {
	case VerificationModeSignature:
		return verifySignatureHeader(cfg.Secret, header.Get("X-GitCode-Signature-256"), body)
	case "", VerificationModeToken:
		return verifyTokenHeader(cfg.Secret, header.Get("X-GitCode-Token"))
	default:
		return fmt.Errorf("unsupported verification mode %q", cfg.VerificationMode)
	}
}

func ParseEvent(header http.Header, body []byte) (interface{}, error) {
	var payload webhookEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode gitcode webhook payload: %w", err)
	}

	eventType := firstNonEmpty(strings.TrimSpace(payload.EventType), strings.TrimSpace(payload.ObjectKind))
	switch eventType {
	case "issue":
		return parseIssueEvent(header, payload)
	case "merge_request":
		return parseMergeRequestEvent(header, payload)
	default:
		return nil, fmt.Errorf("unsupported gitcode event type %q", eventType)
	}
}

func parseIssueEvent(header http.Header, payload webhookEnvelope) (IssueEvent, error) {
	createdAt, err := parseTimestamp(payload.ObjectAttributes.CreatedAt)
	if err != nil {
		return IssueEvent{}, fmt.Errorf("parse issue created_at: %w", err)
	}
	updatedAt, err := parseTimestamp(payload.ObjectAttributes.UpdatedAt)
	if err != nil {
		return IssueEvent{}, fmt.Errorf("parse issue updated_at: %w", err)
	}
	issueKey, err := issuePathFromURL(payload.ObjectAttributes.URL)
	if err != nil {
		return IssueEvent{}, fmt.Errorf("parse issue key: %w", err)
	}

	labels := make([]string, 0, len(payload.Labels))
	for _, label := range payload.Labels {
		title := strings.TrimSpace(label.Title)
		if title != "" {
			labels = append(labels, title)
		}
	}

	return IssueEvent{
		DeliveryID:  strings.TrimSpace(header.Get("X-GitCode-Delivery")),
		EventUUID:   strings.TrimSpace(payload.UUID),
		IssueKey:    issueKey,
		IssueIID:    payload.ObjectAttributes.IID,
		Title:       payload.ObjectAttributes.Title,
		Description: payload.ObjectAttributes.Description,
		State:       payload.ObjectAttributes.State,
		Action:      payload.ObjectAttributes.Action,
		Labels:      labels,
		IssueURL:    payload.ObjectAttributes.URL,
		LastActor:   firstNonEmpty(payload.User.Name, payload.User.Username),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func parseMergeRequestEvent(header http.Header, payload webhookEnvelope) (MergeRequestEvent, error) {
	updatedAt, err := parseTimestamp(payload.ObjectAttributes.UpdatedAt)
	if err != nil {
		return MergeRequestEvent{}, fmt.Errorf("parse merge request updated_at: %w", err)
	}

	issueKeys := make([]string, 0, len(payload.Issues))
	seen := make(map[string]struct{}, len(payload.Issues))
	for _, issue := range payload.Issues {
		issueKey, err := issuePathFromURL(issue.URL)
		if err != nil {
			return MergeRequestEvent{}, fmt.Errorf("parse associated issue key: %w", err)
		}
		if _, ok := seen[issueKey]; ok {
			continue
		}
		seen[issueKey] = struct{}{}
		issueKeys = append(issueKeys, issueKey)
	}

	return MergeRequestEvent{
		DeliveryID:          strings.TrimSpace(header.Get("X-GitCode-Delivery")),
		EventUUID:           strings.TrimSpace(payload.UUID),
		PullRequestIID:      payload.ObjectAttributes.IID,
		Title:               payload.ObjectAttributes.Title,
		State:               payload.ObjectAttributes.State,
		Action:              payload.ObjectAttributes.Action,
		URL:                 payload.ObjectAttributes.URL,
		SourceBranch:        payload.ObjectAttributes.SourceBranch,
		TargetBranch:        payload.ObjectAttributes.TargetBranch,
		UpdatedAt:           updatedAt,
		LastActor:           firstNonEmpty(payload.User.Name, payload.User.Username),
		AssociatedIssueKeys: issueKeys,
	}, nil
}

func verifyTokenHeader(secret, got string) error {
	if subtleTrim(got) == secret {
		return nil
	}
	return fmt.Errorf("gitcode token mismatch")
}

func verifySignatureHeader(secret, got string, body []byte) error {
	got = subtleTrim(got)
	if !strings.HasPrefix(got, "sha256=") {
		return fmt.Errorf("gitcode signature header must use sha256=<hex>")
	}

	want := "sha256=" + computeSignature(secret, body)
	if !hmac.Equal([]byte(got), []byte(want)) {
		return fmt.Errorf("gitcode signature mismatch")
	}
	return nil
}

func computeSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func parseTimestamp(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}

	t, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return t, nil
	}

	t, err = time.Parse(time.RFC3339, raw)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unsupported timestamp %q", raw)
}

func issuePathFromURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	path := strings.TrimSpace(parsed.Path)
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return "", fmt.Errorf("issue url path is empty")
	}
	return path, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func subtleTrim(value string) string {
	return strings.TrimSpace(value)
}
