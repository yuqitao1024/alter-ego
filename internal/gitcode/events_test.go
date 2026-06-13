package gitcode

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

func TestVerifyRequestTokenModeAcceptsMatchingHeader(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set("X-GitCode-Token", "secret")

	if err := VerifyRequest(Config{Secret: "secret", VerificationMode: VerificationModeToken}, header, []byte(`{}`)); err != nil {
		t.Fatalf("VerifyRequest returned error: %v", err)
	}
}

func TestVerifyRequestRejectsEmptySecret(t *testing.T) {
	t.Parallel()

	header := http.Header{}

	if err := VerifyRequest(Config{}, header, []byte(`{}`)); err == nil {
		t.Fatal("VerifyRequest returned nil error, want empty secret rejection")
	}
}

func TestVerifyRequestSignatureModeAcceptsMatchingHeader(t *testing.T) {
	t.Parallel()

	body := []byte(`{"ok":true}`)
	header := http.Header{}
	header.Set("X-GitCode-Signature-256", "sha256="+hmacSHA256Hex("secret", body))

	if err := VerifyRequest(Config{Secret: "secret", VerificationMode: VerificationModeSignature}, header, body); err != nil {
		t.Fatalf("VerifyRequest returned error: %v", err)
	}
}

func TestParseEventIssueBuildsIssuePathKey(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"uuid":"uuid-1",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"id":3084068,
			"iid":8,
			"title":"Test Issue",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/8",
			"created_at":"2025-05-07T14:19:24.490+08:00",
			"updated_at":"2025-05-07T14:19:24.490+08:00",
			"assignee_ids":[]
		},
		"labels":[{"title":"bug"}],
		"user":{"name":"alice","username":"alice"}
	}`)

	event, err := ParseEvent(http.Header{"X-GitCode-Delivery": []string{"delivery-1"}}, raw)
	if err != nil {
		t.Fatalf("ParseEvent returned error: %v", err)
	}

	issue, ok := event.(IssueEvent)
	if !ok {
		t.Fatalf("event type = %T, want IssueEvent", event)
	}
	if issue.IssueKey != "org/repo/issues/8" {
		t.Fatalf("IssueKey = %q, want %q", issue.IssueKey, "org/repo/issues/8")
	}
}

func TestParseEventMergeRequestCollectsAssociatedIssueKeys(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"uuid":"uuid-pr-1",
		"event_type":"merge_request",
		"object_kind":"merge_request",
		"issues":[
			{"iid":8,"url":"https://gitcode.com/org/repo/issues/8"}
		],
		"object_attributes":{
			"id":11,
			"iid":45,
			"title":"feat: issue 8",
			"description":"body",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/pulls/45",
			"source_branch":"feat-8",
			"target_branch":"main",
			"updated_at":"2025-05-08T10:00:00.000+08:00"
		},
		"user":{"name":"alice","username":"alice"}
	}`)

	event, err := ParseEvent(http.Header{"X-GitCode-Delivery": []string{"delivery-2"}}, raw)
	if err != nil {
		t.Fatalf("ParseEvent returned error: %v", err)
	}

	pr, ok := event.(MergeRequestEvent)
	if !ok {
		t.Fatalf("event type = %T, want MergeRequestEvent", event)
	}
	if len(pr.AssociatedIssueKeys) != 1 || pr.AssociatedIssueKeys[0] != "org/repo/issues/8" {
		t.Fatalf("AssociatedIssueKeys = %#v, want %#v", pr.AssociatedIssueKeys, []string{"org/repo/issues/8"})
	}
	if !strings.Contains(pr.SummaryLine(), "!45") {
		t.Fatalf("SummaryLine = %q, want it to contain %q", pr.SummaryLine(), "!45")
	}
}

func TestParseEventMergeRequestPrefersAssociatedIssuePath(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"uuid":"uuid-pr-2",
		"event_type":"merge_request",
		"object_kind":"merge_request",
		"issues":[
			{"iid":8,"path":"org/repo/issues/8","url":"https://gitcode.com/wrong/repo/issues/999"}
		],
		"object_attributes":{
			"iid":46,
			"title":"feat: issue 8 path first",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/pulls/46",
			"source_branch":"feat-8",
			"target_branch":"main",
			"updated_at":"2025-05-08T10:00:00.000+08:00"
		},
		"user":{"name":"alice","username":"alice"}
	}`)

	event, err := ParseEvent(http.Header{"X-GitCode-Delivery": []string{"delivery-3"}}, raw)
	if err != nil {
		t.Fatalf("ParseEvent returned error: %v", err)
	}

	pr, ok := event.(MergeRequestEvent)
	if !ok {
		t.Fatalf("event type = %T, want MergeRequestEvent", event)
	}
	if len(pr.AssociatedIssueKeys) != 1 || pr.AssociatedIssueKeys[0] != "org/repo/issues/8" {
		t.Fatalf("AssociatedIssueKeys = %#v, want %#v", pr.AssociatedIssueKeys, []string{"org/repo/issues/8"})
	}
}

func TestParseEventIssueRejectsMissingRequiredTimestamps(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"uuid":"uuid-2",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":9,
			"title":"Missing Timestamp",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/9",
			"created_at":"",
			"updated_at":"2025-05-07T14:19:24.490+08:00"
		},
		"user":{"name":"alice","username":"alice"}
	}`)

	if _, err := ParseEvent(http.Header{"X-GitCode-Delivery": []string{"delivery-4"}}, raw); err == nil {
		t.Fatal("ParseEvent returned nil error, want missing created_at rejection")
	}
}

func TestParseEventMergeRequestRejectsMissingRequiredUpdatedAt(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"uuid":"uuid-pr-3",
		"event_type":"merge_request",
		"object_kind":"merge_request",
		"issues":[
			{"iid":8,"path":"org/repo/issues/8"}
		],
		"object_attributes":{
			"iid":47,
			"title":"Missing updated_at",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/pulls/47",
			"source_branch":"feat-8",
			"target_branch":"main",
			"updated_at":""
		},
		"user":{"name":"alice","username":"alice"}
	}`)

	if _, err := ParseEvent(http.Header{"X-GitCode-Delivery": []string{"delivery-5"}}, raw); err == nil {
		t.Fatal("ParseEvent returned nil error, want missing updated_at rejection")
	}
}

func hmacSHA256Hex(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
