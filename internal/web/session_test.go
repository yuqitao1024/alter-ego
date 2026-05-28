package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCookieSessionManagerCreatesAndReadsSession(t *testing.T) {
	t.Parallel()

	manager := NewCookieSessionManager([]byte("secret-key-123"), time.Hour)
	recorder := httptest.NewRecorder()

	if err := manager.SetSession(recorder, Session{
		OpenID: "ou_allowed_1",
	}); err != nil {
		t.Fatalf("SetSession returned error: %v", err)
	}

	req := httptest.NewRequest("GET", "/", nil)
	for _, cookie := range recorder.Result().Cookies() {
		req.AddCookie(cookie)
	}

	session, ok := manager.ReadSession(req)
	if !ok {
		t.Fatal("ReadSession = false, want true")
	}
	if session.OpenID != "ou_allowed_1" {
		t.Fatalf("session.OpenID = %q", session.OpenID)
	}
}

func TestCookieSessionManagerRejectsTamperedCookie(t *testing.T) {
	t.Parallel()

	manager := NewCookieSessionManager([]byte("secret-key-123"), time.Hour)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookieWithValue(sessionCookieName, "tampered"))

	if _, ok := manager.ReadSession(req); ok {
		t.Fatal("ReadSession = true, want false for tampered cookie")
	}
}

func TestCookieSessionManagerClearsSession(t *testing.T) {
	t.Parallel()

	manager := NewCookieSessionManager([]byte("secret-key-123"), time.Hour)
	recorder := httptest.NewRecorder()
	manager.ClearSession(recorder)

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("len(cookies) = %d, want 1", len(cookies))
	}
	if cookies[0].MaxAge != -1 {
		t.Fatalf("cookie.MaxAge = %d, want -1", cookies[0].MaxAge)
	}
}

func cookieWithValue(name, value string) *http.Cookie {
	return &http.Cookie{Name: name, Value: value}
}
