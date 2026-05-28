package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const sessionCookieName = "alterego_session"

type Session struct {
	OpenID string    `json:"open_id"`
	Name   string    `json:"name,omitempty"`
	Issued time.Time `json:"issued"`
}

type CookieSessionManager struct {
	secret []byte
	ttl    time.Duration
}

func NewCookieSessionManager(secret []byte, ttl time.Duration) *CookieSessionManager {
	return &CookieSessionManager{secret: append([]byte(nil), secret...), ttl: ttl}
}

func (m *CookieSessionManager) SetSession(w http.ResponseWriter, session Session) error {
	if session.Issued.IsZero() {
		session.Issued = time.Now().UTC()
	}
	body, err := json.Marshal(session)
	if err != nil {
		return err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	signature := signValue(m.secret, payload)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    payload + "." + signature,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(m.ttl.Seconds()),
	})
	return nil
}

func (m *CookieSessionManager) ReadSession(r *http.Request) (Session, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return Session{}, false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return Session{}, false
	}
	if !hmac.Equal([]byte(parts[1]), []byte(signValue(m.secret, parts[0]))) {
		return Session{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Session{}, false
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return Session{}, false
	}
	if session.OpenID == "" {
		return Session{}, false
	}
	return session, true
}

func (m *CookieSessionManager) ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func signValue(secret []byte, value string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
