package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProtectedRootRedirectsToLoginWhenLoggedOut(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, nil)

	req := httptest.NewRequest("GET", "/", nil)
	recorder := httptest.NewRecorder()
	handler.Root(recorder, req)

	if recorder.Code != http.StatusFound {
		t.Fatalf("Code = %d, want 302", recorder.Code)
	}
	if got := recorder.Header().Get("Location"); got != "/login" {
		t.Fatalf("Location = %q, want /login", got)
	}
}

func TestProtectedSessionEndpointReturnsSessionMetadata(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, nil)

	recorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(recorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/web/session", nil)
	for _, cookie := range recorder.Result().Cookies() {
		req.AddCookie(cookie)
	}
	recorder = httptest.NewRecorder()
	handler.Session(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", recorder.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if payload["open_id"] != "ou_allowed_1" {
		t.Fatalf("open_id = %#v", payload["open_id"])
	}
}

func TestLogoutClearsSessionAndRedirectsToLogin(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, nil)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.Logout(recorder, req)

	if recorder.Code != http.StatusFound {
		t.Fatalf("Code = %d, want 302", recorder.Code)
	}
	if got := recorder.Header().Get("Location"); got != "/login" {
		t.Fatalf("Location = %q, want /login", got)
	}

	foundClearedCookie := false
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.MaxAge < 0 {
			foundClearedCookie = true
			break
		}
	}
	if !foundClearedCookie {
		t.Fatalf("expected cleared %q cookie", sessionCookieName)
	}
}

func TestProtectedDashboardEndpointRequiresSession(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, nil)

	req := httptest.NewRequest("GET", "/api/web/dashboard", nil)
	recorder := httptest.NewRecorder()
	handler.Dashboard(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("Code = %d, want 401", recorder.Code)
	}
}

func TestProtectedDashboardEndpointReturnsPayload(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, nil)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/web/dashboard", nil)
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.Dashboard(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", recorder.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	tasks, ok := payload["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("tasks = %#v, want one task", payload["tasks"])
	}
}

func TestProtectedTemplatesEndpointReturnsPayload(t *testing.T) {
	t.Parallel()

	provider := &stubDataProvider{}
	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, provider, nil)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/web/templates", nil)
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.Templates(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", recorder.Code)
	}

	var payload []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("len(payload) = %d, want 2", len(payload))
	}
	if payload[0]["id"] != "feature_dev" {
		t.Fatalf("payload[0].id = %#v, want feature_dev", payload[0]["id"])
	}
}

func TestEventsRequiresSession(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, NewStreamBroker())

	req := httptest.NewRequest(http.MethodGet, "/api/web/events", nil)
	recorder := httptest.NewRecorder()
	handler.Events(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("Code = %d, want 401", recorder.Code)
	}
}

func TestEventsStreamsPublishedMessages(t *testing.T) {
	t.Parallel()

	streams := NewStreamBroker()
	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, streams)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/web/events", nil).WithContext(ctx)
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.Events(recorder, req)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	streams.Publish(StreamEvent{Type: "task_updated", TaskID: "task-1"})
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	body := recorder.Body.String()
	if !strings.Contains(body, "event: ready") {
		t.Fatalf("body = %q, want ready event", body)
	}
	if !strings.Contains(body, "\"type\":\"task_updated\"") {
		t.Fatalf("body = %q, want task_updated payload", body)
	}
	if !strings.Contains(body, "\"task_id\":\"task-1\"") {
		t.Fatalf("body = %q, want task_id payload", body)
	}
}

func TestTaskDetailRequiresSession(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/web/tasks/task-1", nil)
	recorder := httptest.NewRecorder()
	handler.TaskAction(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("Code = %d, want 401", recorder.Code)
	}
}

func TestTaskDetailReturnsPayload(t *testing.T) {
	t.Parallel()

	provider := &stubDataProvider{}
	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, provider, nil)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/web/tasks/task-1", nil)
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.TaskAction(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", recorder.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if payload["id"] != "task-1" {
		t.Fatalf("payload.id = %#v, want task-1", payload["id"])
	}
	events, ok := payload["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("payload.events = %#v, want two events", payload["events"])
	}
	questions, ok := payload["questions"].([]any)
	if !ok || len(questions) != 1 {
		t.Fatalf("payload.questions = %#v, want one question", payload["questions"])
	}
	if provider.lastTaskID != "task-1" {
		t.Fatalf("provider.lastTaskID = %q, want task-1", provider.lastTaskID)
	}
}

func TestTaskDetailPropagatesNotFound(t *testing.T) {
	t.Parallel()

	provider := &stubDataProvider{detailErr: "task not found"}
	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, provider, nil)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/web/tasks/task-missing", nil)
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.TaskAction(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("Code = %d, want 404", recorder.Code)
	}
	if body := strings.TrimSpace(recorder.Body.String()); body != "task not found" {
		t.Fatalf("body = %q, want task not found", body)
	}
}

func TestTaskCreateReadsTemplateAndRequirement(t *testing.T) {
	t.Parallel()

	provider := &stubDataProvider{}
	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, provider, nil)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1", Name: "Tester"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/web/tasks", strings.NewReader(`{"template_id":"feature_dev","requirement":"Fix websocket reconnect handling"}`))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.CreateTask(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", recorder.Code)
	}
	if provider.startedTemplateID != "feature_dev" || provider.startedRequirement != "Fix websocket reconnect handling" || provider.startedBy != "ou_allowed_1" {
		t.Fatalf("provider start state = %#v", provider)
	}
}

func TestTaskStopRequiresSession(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/web/tasks/task-1/stop", nil)
	recorder := httptest.NewRecorder()
	handler.TaskAction(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("Code = %d, want 401", recorder.Code)
	}
}

func TestTaskStopCallsProvider(t *testing.T) {
	t.Parallel()

	provider := &stubDataProvider{}
	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, provider, nil)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/web/tasks/task-1/stop", nil)
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.TaskAction(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", recorder.Code)
	}
	if provider.lastAction != "stop" || provider.lastTaskID != "task-1" {
		t.Fatalf("provider action = %q task = %q", provider.lastAction, provider.lastTaskID)
	}
}

func TestTaskReplyReadsJSONBody(t *testing.T) {
	t.Parallel()

	provider := &stubDataProvider{}
	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, provider, nil)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/web/tasks/task-2/reply", strings.NewReader(`{"text":"continue with option B"}`))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.TaskAction(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("Code = %d, want 200", recorder.Code)
	}
	if provider.lastAction != "reply" || provider.lastTaskID != "task-2" || provider.lastText != "continue with option B" {
		t.Fatalf("provider state = %#v", provider)
	}
}

func TestTaskActionReturnsProviderErrorMessage(t *testing.T) {
	t.Parallel()

	provider := &stubDataProvider{stopErr: "task is already stopped"}
	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, provider, nil)

	loginRecorder := httptest.NewRecorder()
	if err := handler.sessions.SetSession(loginRecorder, Session{OpenID: "ou_allowed_1"}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/web/tasks/task-3/stop", nil)
	for _, cookie := range loginRecorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.TaskAction(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want 400", recorder.Code)
	}
	if body := strings.TrimSpace(recorder.Body.String()); body != "task is already stopped" {
		t.Fatalf("body = %q, want task is already stopped", body)
	}
}

type stubOAuthClient struct{}

func (stubOAuthClient) BuildAuthorizeURL(state string) (string, error) {
	return "/oauth?state=" + state, nil
}

func (stubOAuthClient) ExchangeCode(context.Context, string) (OAuthToken, error) {
	return OAuthToken{AccessToken: "token"}, nil
}

func (stubOAuthClient) FetchUser(context.Context, string) (OAuthUser, error) {
	return OAuthUser{OpenID: "ou_allowed_1", Name: "Tester"}, nil
}

type stubDataProvider struct {
	lastAction         string
	lastTaskID         string
	lastText           string
	stopErr            string
	detailErr          string
	startedTemplateID  string
	startedRequirement string
	startedBy          string
}

func (*stubDataProvider) Dashboard(context.Context) (any, error) {
	return map[string]any{
		"tasks": []map[string]any{
			{"id": "task-1", "status": "running"},
		},
	}, nil
}

func (*stubDataProvider) Templates(context.Context) (any, error) {
	return []map[string]any{
		{"id": "feature_dev", "display_name": "Feature Development", "description": "Default feature workflow", "task_type": "general", "workspace_id": "backend_workspace"},
		{"id": "code_review", "display_name": "Code Review", "description": "Review workflow", "task_type": "code_review", "workspace_id": "backend_workspace"},
	}, nil
}

func (s *stubDataProvider) TaskDetail(_ context.Context, taskID string) (any, error) {
	s.lastTaskID = taskID
	if s.detailErr != "" {
		return nil, contextErrorString(s.detailErr)
	}
	return map[string]any{
		"id":             taskID,
		"title":          "Investigate stalled task",
		"status":         "waiting_user_input",
		"template_id":    "feature_dev",
		"machine_id":     "machine_a",
		"thread_id":      "thread-1",
		"remote_workdir": "/srv/codex-tasks/task-1",
		"summary":        "Need operator confirmation before continuing.",
		"last_input":     "Investigate websocket reconnect handling",
		"events": []map[string]any{
			{"event_type": "task_started", "message": "task started", "created_at": "2026-05-28T10:00:00Z"},
			{"event_type": "waiting_user_input", "message": "waiting for plan_decision", "created_at": "2026-05-28T10:05:00Z"},
		},
		"questions": []map[string]any{
			{
				"question_type":   "plan_decision",
				"question_text":   "Choose between websocket and polling fallback.",
				"options_summary": "A websocket only; B websocket plus polling",
				"context_excerpt": "Current subscription mode is unreliable after reconnect.",
				"asked_at":        "2026-05-28T10:05:00Z",
			},
		},
	}, nil
}

func (s *stubDataProvider) StartTask(_ context.Context, templateID, createdBy, requirement string) (any, error) {
	s.startedTemplateID = templateID
	s.startedBy = createdBy
	s.startedRequirement = requirement
	return map[string]any{
		"task_id": "task-new",
		"status":  "running",
	}, nil
}

func (s *stubDataProvider) StopTask(_ context.Context, taskID string) error {
	if s.stopErr != "" {
		return contextErrorString(s.stopErr)
	}
	s.lastAction = "stop"
	s.lastTaskID = taskID
	return nil
}

func (s *stubDataProvider) CompleteTask(_ context.Context, taskID string) error {
	s.lastAction = "complete"
	s.lastTaskID = taskID
	return nil
}

func (s *stubDataProvider) DeleteTask(_ context.Context, taskID string) error {
	s.lastAction = "delete"
	s.lastTaskID = taskID
	return nil
}

func (s *stubDataProvider) ReplyTask(_ context.Context, taskID, text string) error {
	s.lastAction = "reply"
	s.lastTaskID = taskID
	s.lastText = text
	return nil
}

func (s *stubDataProvider) ReopenTask(_ context.Context, taskID, text string) error {
	s.lastAction = "reopen"
	s.lastTaskID = taskID
	s.lastText = text
	return nil
}

type contextErrorString string

func (e contextErrorString) Error() string {
	return string(e)
}
