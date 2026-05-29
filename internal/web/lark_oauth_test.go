package web

import (
	"context"
	"net/http"
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

func TestExchangeCodeAcceptsTopLevelAccessTokenResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/open-apis/authen/v2/oauth/token" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"","access_token":"token-123","token_type":"Bearer","expires_in":7200}`))
	}))
	defer server.Close()

	client := LarkOAuthClient{
		AppID:       "cli_test",
		AppSecret:   "secret",
		BaseURL:     server.URL,
		RedirectURI: "https://dashboard.example.com/auth/lark/callback",
		HTTPClient:  server.Client(),
	}

	token, err := client.ExchangeCode(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("ExchangeCode returned error: %v", err)
	}
	if token.AccessToken != "token-123" {
		t.Fatalf("token.AccessToken = %q, want token-123", token.AccessToken)
	}
}
