package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProtectedRootRedirectsToLoginWhenLoggedOut(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, stubDataProvider{})

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
	}, stubOAuthClient{}, stubDataProvider{})

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

func TestProtectedMockTasksEndpointRequiresSession(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, stubDataProvider{})

	req := httptest.NewRequest("GET", "/api/web/mock/tasks", nil)
	recorder := httptest.NewRecorder()
	handler.MockTasks(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("Code = %d, want 401", recorder.Code)
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

type stubDataProvider struct{}

func (stubDataProvider) MockDashboard(context.Context) any {
	return map[string]any{
		"tasks": []map[string]any{
			{"id": "task-1", "status": "running"},
		},
	}
}
