package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestStateStoreIssuesAndConsumesState(t *testing.T) {
	t.Parallel()

	store := NewStateStore(time.Minute)
	state := store.Issue("/dashboard")
	if strings.TrimSpace(state) == "" {
		t.Fatal("Issue returned empty state")
	}

	redirect, ok := store.Consume(state)
	if !ok {
		t.Fatal("Consume = false, want true")
	}
	if redirect != "/dashboard" {
		t.Fatalf("redirect = %q", redirect)
	}
	if _, ok := store.Consume(state); ok {
		t.Fatal("Consume reused state, want one-time use")
	}
}

func TestBuildAuthorizeURLIncludesStateAndRedirectURI(t *testing.T) {
	t.Parallel()

	client := LarkOAuthClient{
		AppID:       "cli_test",
		BaseURL:     "https://open.feishu.cn",
		RedirectURI: "https://dashboard.example.com/auth/lark/callback",
	}

	raw, err := client.BuildAuthorizeURL("state-1")
	if err != nil {
		t.Fatalf("BuildAuthorizeURL returned error: %v", err)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	query := parsed.Query()
	if query.Get("app_id") != "cli_test" {
		t.Fatalf("app_id = %q", query.Get("app_id"))
	}
	if query.Get("state") != "state-1" {
		t.Fatalf("state = %q", query.Get("state"))
	}
	if query.Get("redirect_uri") != "https://dashboard.example.com/auth/lark/callback" {
		t.Fatalf("redirect_uri = %q", query.Get("redirect_uri"))
	}
}

func TestCallbackRejectsMismatchedState(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, nil)

	req := httptest.NewRequest("GET", "/auth/lark/callback?code=code-1&state=bad-state", nil)
	recorder := httptest.NewRecorder()
	handler.Callback(recorder, req)

	if recorder.Code != 400 {
		t.Fatalf("Code = %d, want 400", recorder.Code)
	}
}
