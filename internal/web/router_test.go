package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterServesCallbackAndLoginRoutes(t *testing.T) {
	t.Parallel()

	callbackHit := false
	callback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callbackHit = true
		w.WriteHeader(http.StatusNoContent)
	})

	router := NewRouter(NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}), callback)

	loginReq := httptest.NewRequest(http.MethodGet, "/login", nil)
	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginReq)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login code = %d, want 200", loginRecorder.Code)
	}

	callbackReq := httptest.NewRequest(http.MethodPost, "/lark/card/callback", nil)
	callbackRecorder := httptest.NewRecorder()
	router.ServeHTTP(callbackRecorder, callbackReq)
	if callbackRecorder.Code != http.StatusNoContent {
		t.Fatalf("callback code = %d, want 204", callbackRecorder.Code)
	}
	if !callbackHit {
		t.Fatal("callback handler was not invoked")
	}
}
