package gitcode

import (
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
