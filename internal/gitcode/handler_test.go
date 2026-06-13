package gitcode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeSyncService struct {
	issueCalls []IssueEvent
	prCalls    []MergeRequestEvent
	issueErr   error
	prErr      error
}

func (f *fakeSyncService) ApplyIssueEvent(_ context.Context, event IssueEvent) error {
	f.issueCalls = append(f.issueCalls, event)
	return f.issueErr
}

func (f *fakeSyncService) ApplyMergeRequestEvent(_ context.Context, event MergeRequestEvent) error {
	f.prCalls = append(f.prCalls, event)
	return f.prErr
}

func TestWebhookHandlerRejectsNonPost(t *testing.T) {
	t.Parallel()

	handler := NewWebhookHandler(Config{Secret: "secret", VerificationMode: VerificationModeToken}, openTestDeliveryStore(t), &fakeSyncService{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/gitcode/webhook", nil)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebhookHandlerDispatchesIssueEventOnce(t *testing.T) {
	t.Parallel()

	store := openTestDeliveryStore(t)
	service := &fakeSyncService{}
	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, store, service)

	body := `{
		"uuid":"uuid-1",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":8,
			"title":"Issue 8",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/8",
			"created_at":"2025-05-07T14:19:24Z",
			"updated_at":"2025-05-07T14:19:24Z"
		},
		"user":{"name":"alice"}
	}`

	req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
	req.Header.Set("X-GitCode-Token", "secret")
	req.Header.Set("X-GitCode-Delivery", "delivery-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(service.issueCalls) != 1 {
		t.Fatalf("issue calls = %d, want 1", len(service.issueCalls))
	}
	if len(service.prCalls) != 0 {
		t.Fatalf("pr calls = %d, want 0", len(service.prCalls))
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
	req.Header.Set("X-GitCode-Token", "secret")
	req.Header.Set("X-GitCode-Delivery", "delivery-1")

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(service.issueCalls) != 1 {
		t.Fatalf("issue calls after duplicate = %d, want 1", len(service.issueCalls))
	}
}

func TestWebhookHandlerDispatchesMergeRequestEvent(t *testing.T) {
	t.Parallel()

	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), &fakeSyncService{})

	body := `{
		"uuid":"uuid-pr-1",
		"event_type":"merge_request",
		"object_kind":"merge_request",
		"issues":[
			{"iid":8,"path":"org/repo/issues/8"}
		],
		"object_attributes":{
			"iid":45,
			"title":"feat: issue 8",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/pulls/45",
			"source_branch":"feat-8",
			"target_branch":"main",
			"updated_at":"2025-05-08T10:00:00Z"
		},
		"user":{"name":"alice"}
	}`

	service := &fakeSyncService{}
	handler = NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), service)
	req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
	req.Header.Set("X-GitCode-Token", "secret")
	req.Header.Set("X-GitCode-Delivery", "delivery-pr-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(service.issueCalls) != 0 {
		t.Fatalf("issue calls = %d, want 0", len(service.issueCalls))
	}
	if len(service.prCalls) != 1 {
		t.Fatalf("pr calls = %d, want 1", len(service.prCalls))
	}
}

func TestWebhookHandlerSkipsMergeRequestWithoutAssociatedIssues(t *testing.T) {
	t.Parallel()

	service := &fakeSyncService{}
	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), service)

	body := `{
		"uuid":"uuid-pr-2",
		"event_type":"merge_request",
		"object_kind":"merge_request",
		"issues":[],
		"object_attributes":{
			"iid":46,
			"title":"docs: no issue",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/pulls/46",
			"source_branch":"docs",
			"target_branch":"main",
			"updated_at":"2025-05-08T10:00:00Z"
		},
		"user":{"name":"alice"}
	}`

	req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
	req.Header.Set("X-GitCode-Token", "secret")
	req.Header.Set("X-GitCode-Delivery", "delivery-pr-2")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(service.prCalls) != 0 {
		t.Fatalf("pr calls = %d, want 0", len(service.prCalls))
	}
}

func TestWebhookHandlerRejectsFailedVerification(t *testing.T) {
	t.Parallel()

	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), &fakeSyncService{})

	req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(`{}`))
	req.Header.Set("X-GitCode-Token", "wrong")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestWebhookHandlerRejectsParseFailure(t *testing.T) {
	t.Parallel()

	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), &fakeSyncService{})

	req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(`{"event_type":"issue"`))
	req.Header.Set("X-GitCode-Token", "secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWebhookHandlerPropagatesIssueSyncFailure(t *testing.T) {
	t.Parallel()

	service := &fakeSyncService{issueErr: errors.New("sync failed")}
	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), service)

	body := `{
		"uuid":"uuid-3",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":9,
			"title":"Issue 9",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/9",
			"created_at":"2025-05-07T14:19:24Z",
			"updated_at":"2025-05-07T14:19:24Z"
		},
		"user":{"name":"alice"}
	}`

	req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
	req.Header.Set("X-GitCode-Token", "secret")
	req.Header.Set("X-GitCode-Delivery", "delivery-3")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
