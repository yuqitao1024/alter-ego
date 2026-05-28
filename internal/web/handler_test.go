package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProtectedRootRedirectsToLoginWhenLoggedOut(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{})

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
	}, stubOAuthClient{}, &stubDataProvider{})

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

func TestProtectedDashboardEndpointRequiresSession(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{})

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
	}, stubOAuthClient{}, &stubDataProvider{})

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

func TestTaskStopRequiresSession(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{})

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
	}, stubOAuthClient{}, provider)

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
	}, stubOAuthClient{}, provider)

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
	}, stubOAuthClient{}, provider)

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
	lastAction string
	lastTaskID string
	lastText   string
	stopErr    string
}

func (*stubDataProvider) Dashboard(context.Context) (any, error) {
	return map[string]any{
		"tasks": []map[string]any{
			{"id": "task-1", "status": "running"},
		},
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
