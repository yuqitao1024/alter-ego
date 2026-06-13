package gitcode

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSyncService struct {
	mu         sync.Mutex
	issueCalls []IssueEvent
	prCalls    []MergeRequestEvent
	issueErr   error
	prErr      error
	issueFn    func(context.Context, IssueEvent) error
	prFn       func(context.Context, MergeRequestEvent) error
}

func (f *fakeSyncService) ApplyIssueEvent(ctx context.Context, event IssueEvent) error {
	f.mu.Lock()
	f.issueCalls = append(f.issueCalls, event)
	fn := f.issueFn
	err := f.issueErr
	f.mu.Unlock()

	if fn != nil {
		return fn(ctx, event)
	}
	return err
}

func (f *fakeSyncService) ApplyMergeRequestEvent(ctx context.Context, event MergeRequestEvent) error {
	f.mu.Lock()
	f.prCalls = append(f.prCalls, event)
	fn := f.prFn
	err := f.prErr
	f.mu.Unlock()

	if fn != nil {
		return fn(ctx, event)
	}
	return err
}

func (f *fakeSyncService) issueCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.issueCalls)
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

func TestWebhookHandlerRetriesAfterIssueSyncFailure(t *testing.T) {
	t.Parallel()

	store := openTestDeliveryStore(t)
	service := &fakeSyncService{issueErr: errors.New("sync failed")}
	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, store, service)

	body := `{
		"uuid":"uuid-retry-1",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":10,
			"title":"Issue 10",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/10",
			"created_at":"2025-05-07T14:19:24Z",
			"updated_at":"2025-05-07T14:19:24Z"
		},
		"user":{"name":"alice"}
	}`

	req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
	req.Header.Set("X-GitCode-Token", "secret")
	req.Header.Set("X-GitCode-Delivery", "delivery-retry-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("first status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if service.issueCallCount() != 1 {
		t.Fatalf("issue calls after first failure = %d, want 1", service.issueCallCount())
	}

	service.mu.Lock()
	service.issueErr = nil
	service.mu.Unlock()
	req = httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
	req.Header.Set("X-GitCode-Token", "secret")
	req.Header.Set("X-GitCode-Delivery", "delivery-retry-1")
	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want %d", rec.Code, http.StatusOK)
	}
	if service.issueCallCount() != 2 {
		t.Fatalf("issue calls after retry = %d, want 2", service.issueCallCount())
	}

	req = httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
	req.Header.Set("X-GitCode-Token", "secret")
	req.Header.Set("X-GitCode-Delivery", "delivery-retry-1")
	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate-after-success status = %d, want %d", rec.Code, http.StatusOK)
	}
	if service.issueCallCount() != 2 {
		t.Fatalf("issue calls after duplicate success = %d, want 2", service.issueCallCount())
	}
}

func TestWebhookHandlerConcurrentDuplicateSuccessDispatchesOnce(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	service := &fakeSyncService{
		issueFn: func(context.Context, IssueEvent) error {
			started <- struct{}{}
			<-release
			return nil
		},
	}
	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), service)

	body := `{
		"uuid":"uuid-concurrent-1",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":11,
			"title":"Issue 11",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/11",
			"created_at":"2025-05-07T14:19:24Z",
			"updated_at":"2025-05-07T14:19:24Z"
		},
		"user":{"name":"alice"}
	}`

	runRequest := func() <-chan int {
		result := make(chan int, 1)
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
			req.Header.Set("X-GitCode-Token", "secret")
			req.Header.Set("X-GitCode-Delivery", "delivery-concurrent-1")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			result <- rec.Code
		}()
		return result
	}

	first := runRequest()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not reach sync service")
	}

	second := runRequest()
	select {
	case <-started:
		t.Fatal("concurrent duplicate reached sync service before first attempt completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	if code := <-first; code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", code, http.StatusOK)
	}
	if code := <-second; code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", code, http.StatusOK)
	}
	if service.issueCallCount() != 1 {
		t.Fatalf("issue calls = %d, want 1", service.issueCallCount())
	}
}

func TestWebhookHandlerConcurrentDuplicateRetriesAfterFailure(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	attempts := 0
	var attemptsMu sync.Mutex
	service := &fakeSyncService{
		issueFn: func(context.Context, IssueEvent) error {
			started <- struct{}{}
			attemptsMu.Lock()
			attempts++
			currentAttempt := attempts
			attemptsMu.Unlock()
			if currentAttempt == 1 {
				<-release
				return errors.New("sync failed")
			}
			return nil
		},
	}
	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), service)

	body := `{
		"uuid":"uuid-concurrent-2",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":12,
			"title":"Issue 12",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/12",
			"created_at":"2025-05-07T14:19:24Z",
			"updated_at":"2025-05-07T14:19:24Z"
		},
		"user":{"name":"alice"}
	}`

	runRequest := func() <-chan int {
		result := make(chan int, 1)
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
			req.Header.Set("X-GitCode-Token", "secret")
			req.Header.Set("X-GitCode-Delivery", "delivery-concurrent-2")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			result <- rec.Code
		}()
		return result
	}

	first := runRequest()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not reach sync service")
	}

	second := runRequest()
	select {
	case <-started:
		t.Fatal("concurrent duplicate reached sync service before first attempt completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	if code := <-first; code != http.StatusInternalServerError {
		t.Fatalf("first status = %d, want %d", code, http.StatusInternalServerError)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("waiting duplicate did not retry after first failure")
	}
	if code := <-second; code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", code, http.StatusOK)
	}
	if service.issueCallCount() != 2 {
		t.Fatalf("issue calls = %d, want 2", service.issueCallCount())
	}
}

func TestWebhookHandlerConcurrentSameEventUUIDDifferentDeliveryDispatchesOnce(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	service := &fakeSyncService{
		issueFn: func(context.Context, IssueEvent) error {
			started <- struct{}{}
			<-release
			return nil
		},
	}
	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), service)

	body := `{
		"uuid":"uuid-concurrent-shared-1",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":13,
			"title":"Issue 13",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/13",
			"created_at":"2025-05-07T14:19:24Z",
			"updated_at":"2025-05-07T14:19:24Z"
		},
		"user":{"name":"alice"}
	}`

	runRequest := func(deliveryID string) <-chan int {
		result := make(chan int, 1)
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
			req.Header.Set("X-GitCode-Token", "secret")
			req.Header.Set("X-GitCode-Delivery", deliveryID)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			result <- rec.Code
		}()
		return result
	}

	first := runRequest("delivery-concurrent-shared-1a")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not reach sync service")
	}

	second := runRequest("delivery-concurrent-shared-1b")
	select {
	case <-started:
		t.Fatal("same event uuid with different delivery id reached sync service before first attempt completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	if code := <-first; code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", code, http.StatusOK)
	}
	if code := <-second; code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", code, http.StatusOK)
	}
	if service.issueCallCount() != 1 {
		t.Fatalf("issue calls = %d, want 1", service.issueCallCount())
	}
}

func TestWebhookHandlerConcurrentSameEventUUIDDifferentDeliveryRetriesAfterFailure(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	attempts := 0
	var attemptsMu sync.Mutex
	service := &fakeSyncService{
		issueFn: func(context.Context, IssueEvent) error {
			started <- struct{}{}
			attemptsMu.Lock()
			attempts++
			currentAttempt := attempts
			attemptsMu.Unlock()
			if currentAttempt == 1 {
				<-release
				return errors.New("sync failed")
			}
			return nil
		},
	}
	handler := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, openTestDeliveryStore(t), service)

	body := `{
		"uuid":"uuid-concurrent-shared-2",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":14,
			"title":"Issue 14",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/14",
			"created_at":"2025-05-07T14:19:24Z",
			"updated_at":"2025-05-07T14:19:24Z"
		},
		"user":{"name":"alice"}
	}`

	runRequest := func(deliveryID string) <-chan int {
		result := make(chan int, 1)
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
			req.Header.Set("X-GitCode-Token", "secret")
			req.Header.Set("X-GitCode-Delivery", deliveryID)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			result <- rec.Code
		}()
		return result
	}

	first := runRequest("delivery-concurrent-shared-2a")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not reach sync service")
	}

	second := runRequest("delivery-concurrent-shared-2b")
	select {
	case <-started:
		t.Fatal("same event uuid with different delivery id reached sync service before first attempt completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	if code := <-first; code != http.StatusInternalServerError {
		t.Fatalf("first status = %d, want %d", code, http.StatusInternalServerError)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("waiting same-event duplicate did not retry after first failure")
	}
	if code := <-second; code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", code, http.StatusOK)
	}
	if service.issueCallCount() != 2 {
		t.Fatalf("issue calls = %d, want 2", service.issueCallCount())
	}
}

func TestWebhookHandlerConcurrentSameEventUUIDAcrossHandlersDispatchesOnce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "deliveries.db")
	storeA, err := OpenDeliveryStore(path)
	if err != nil {
		t.Fatalf("OpenDeliveryStore storeA returned error: %v", err)
	}
	defer storeA.Close()

	storeB, err := OpenDeliveryStore(path)
	if err != nil {
		t.Fatalf("OpenDeliveryStore storeB returned error: %v", err)
	}
	defer storeB.Close()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	service := &fakeSyncService{
		issueFn: func(context.Context, IssueEvent) error {
			started <- struct{}{}
			<-release
			return nil
		},
	}
	handlerA := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, storeA, service)
	handlerB := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, storeB, service)

	body := `{
		"uuid":"uuid-concurrent-cross-handler-1",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":15,
			"title":"Issue 15",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/15",
			"created_at":"2025-05-07T14:19:24Z",
			"updated_at":"2025-05-07T14:19:24Z"
		},
		"user":{"name":"alice"}
	}`

	runRequest := func(handler *WebhookHandler, deliveryID string) <-chan int {
		result := make(chan int, 1)
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
			req.Header.Set("X-GitCode-Token", "secret")
			req.Header.Set("X-GitCode-Delivery", deliveryID)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			result <- rec.Code
		}()
		return result
	}

	first := runRequest(handlerA, "delivery-cross-handler-1a")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not reach sync service")
	}

	second := runRequest(handlerB, "delivery-cross-handler-1b")
	select {
	case <-started:
		t.Fatal("same event uuid across handlers reached sync service before first attempt completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	if code := <-first; code != http.StatusOK {
		t.Fatalf("first status = %d, want %d", code, http.StatusOK)
	}
	if code := <-second; code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", code, http.StatusOK)
	}
	if service.issueCallCount() != 1 {
		t.Fatalf("issue calls = %d, want 1", service.issueCallCount())
	}
}

func TestWebhookHandlerConcurrentSameEventUUIDAcrossHandlersRetriesAfterFailure(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "deliveries.db")
	storeA, err := OpenDeliveryStore(path)
	if err != nil {
		t.Fatalf("OpenDeliveryStore storeA returned error: %v", err)
	}
	defer storeA.Close()

	storeB, err := OpenDeliveryStore(path)
	if err != nil {
		t.Fatalf("OpenDeliveryStore storeB returned error: %v", err)
	}
	defer storeB.Close()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	attempts := 0
	var attemptsMu sync.Mutex
	service := &fakeSyncService{
		issueFn: func(context.Context, IssueEvent) error {
			started <- struct{}{}
			attemptsMu.Lock()
			attempts++
			currentAttempt := attempts
			attemptsMu.Unlock()
			if currentAttempt == 1 {
				<-release
				return errors.New("sync failed")
			}
			return nil
		},
	}
	handlerA := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, storeA, service)
	handlerB := NewWebhookHandler(Config{
		Secret:           "secret",
		VerificationMode: VerificationModeToken,
	}, storeB, service)

	body := `{
		"uuid":"uuid-concurrent-cross-handler-2",
		"event_type":"issue",
		"object_kind":"issue",
		"object_attributes":{
			"iid":16,
			"title":"Issue 16",
			"description":"content",
			"state":"opened",
			"action":"open",
			"url":"https://gitcode.com/org/repo/issues/16",
			"created_at":"2025-05-07T14:19:24Z",
			"updated_at":"2025-05-07T14:19:24Z"
		},
		"user":{"name":"alice"}
	}`

	runRequest := func(handler *WebhookHandler, deliveryID string) <-chan int {
		result := make(chan int, 1)
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/gitcode/webhook", strings.NewReader(body))
			req.Header.Set("X-GitCode-Token", "secret")
			req.Header.Set("X-GitCode-Delivery", deliveryID)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			result <- rec.Code
		}()
		return result
	}

	first := runRequest(handlerA, "delivery-cross-handler-2a")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not reach sync service")
	}

	second := runRequest(handlerB, "delivery-cross-handler-2b")
	select {
	case <-started:
		t.Fatal("same event uuid across handlers reached sync service before first attempt completed")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)

	if code := <-first; code != http.StatusInternalServerError {
		t.Fatalf("first status = %d, want %d", code, http.StatusInternalServerError)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("waiting cross-handler duplicate did not retry after first failure")
	}
	if code := <-second; code != http.StatusOK {
		t.Fatalf("second status = %d, want %d", code, http.StatusOK)
	}
	if service.issueCallCount() != 2 {
		t.Fatalf("issue calls = %d, want 2", service.issueCallCount())
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
