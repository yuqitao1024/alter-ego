package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type DataProvider interface {
	Dashboard(ctx context.Context) (any, error)
}

type Handler struct {
	cfg      Config
	oauth    OAuthClient
	sessions *CookieSessionManager
	states   *StateStore
	data     DataProvider
}

func NewHandler(cfg Config, oauth OAuthClient, data DataProvider) *Handler {
	return &Handler{
		cfg:      cfg,
		oauth:    oauth,
		sessions: NewCookieSessionManager([]byte(cfg.SessionSecret), 12*time.Hour),
		states:   NewStateStore(5 * time.Minute),
		data:     data,
	}
}

func (h *Handler) Root(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.sessions.ReadSession(r); !ok {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<!doctype html><html><body><div id=\"app\">dashboard shell</div></body></html>"))
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<!doctype html><html><body><a href=\"/auth/lark/start\">Sign in with Lark</a></body></html>"))
}

func (h *Handler) StartAuth(w http.ResponseWriter, r *http.Request) {
	state := h.states.Issue("/")
	raw, err := h.oauth.BuildAuthorizeURL(state)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, raw, http.StatusFound)
}

func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if state == "" || code == "" {
		http.Error(w, "missing state or code", http.StatusBadRequest)
		return
	}
	redirectTo, ok := h.states.Consume(state)
	if !ok {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	token, err := h.oauth.ExchangeCode(r.Context(), code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	user, err := h.oauth.FetchUser(r.Context(), token.AccessToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if user.OpenID == "" || !h.cfg.AllowUsers[user.OpenID] {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := h.sessions.SetSession(w, Session{
		OpenID: user.OpenID,
		Name:   user.Name,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.sessions.ClearSession(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	session, ok := h.sessions.ReadSession(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"open_id":       session.OpenID,
		"name":          session.Name,
	})
}

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.sessions.ReadSession(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	payload, err := h.data.Dashboard(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
