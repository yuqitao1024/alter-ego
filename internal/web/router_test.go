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
	}, stubOAuthClient{}, &stubDataProvider{}, nil), callback)

	loginReq := httptest.NewRequest(http.MethodGet, "/login", nil)
	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, loginReq)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login code = %d, want 200", loginRecorder.Code)
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/web/events", nil)
	eventsRecorder := httptest.NewRecorder()
	router.ServeHTTP(eventsRecorder, eventsReq)
	if eventsRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("events code = %d, want 401", eventsRecorder.Code)
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

func TestRouterServesAuthStartRoute(t *testing.T) {
	t.Parallel()

	router := NewRouter(NewHandler(Config{
		PublicBaseURL: "https://dashboard.example.com",
		ListenAddr:    "127.0.0.1:18080",
		SessionSecret: "secret",
		AllowUsers:    map[string]bool{"ou_allowed_1": true},
	}, stubOAuthClient{}, &stubDataProvider{}, nil), nil)

	req := httptest.NewRequest(http.MethodGet, "/auth/lark/start", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusFound {
		t.Fatalf("auth start code = %d, want 302", recorder.Code)
	}
	if got := recorder.Header().Get("Location"); got == "" || got == "/login" || got[:13] != "/oauth?state=" {
		t.Fatalf("Location = %q, want stub OAuth authorize url", got)
	}
}
