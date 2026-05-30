package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type DataProvider interface {
	Dashboard(ctx context.Context) (any, error)
	Templates(ctx context.Context) (any, error)
	TaskDetail(ctx context.Context, taskID string) (any, error)
	StartTask(ctx context.Context, templateID, createdBy, requirement string) (any, error)
	StopTask(ctx context.Context, taskID string) error
	CompleteTask(ctx context.Context, taskID string) error
	DeleteTask(ctx context.Context, taskID string) error
	ReplyTask(ctx context.Context, taskID, text string) error
	ReopenTask(ctx context.Context, taskID, text string) error
}

type Handler struct {
	cfg      Config
	oauth    OAuthClient
	sessions *CookieSessionManager
	states   *StateStore
	data     DataProvider
	streams  *StreamBroker
}

func NewHandler(cfg Config, oauth OAuthClient, data DataProvider, streams *StreamBroker) *Handler {
	return &Handler{
		cfg:      cfg,
		oauth:    oauth,
		sessions: NewCookieSessionManager([]byte(cfg.SessionSecret), 12*time.Hour),
		states:   NewStateStore(5 * time.Minute),
		data:     data,
		streams:  streams,
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
	http.Redirect(w, r, "/login", http.StatusFound)
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

func (h *Handler) Templates(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.sessions.ReadSession(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	payload, err := h.data.Templates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.sessions.ReadSession(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if h.streams == nil {
		_, _ = io.WriteString(w, "event: ready\ndata: {}\n\n")
		flusher.Flush()
		<-r.Context().Done()
		return
	}

	subID, ch := h.streams.Subscribe()
	defer h.streams.Unsubscribe(subID)

	_, _ = io.WriteString(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case event, ok := <-ch:
			if !ok {
				return
			}
			if _, err := io.WriteString(w, "event: task_updated\ndata: "); err != nil {
				return
			}
			if _, err := w.Write(event.JSON()); err != nil {
				return
			}
			if _, err := io.WriteString(w, "\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	session, ok := h.sessions.ReadSession(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		TemplateID  string `json:"template_id"`
		Requirement string `json:"requirement"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	body.TemplateID = strings.TrimSpace(body.TemplateID)
	body.Requirement = strings.TrimSpace(body.Requirement)
	if body.TemplateID == "" || body.Requirement == "" {
		http.Error(w, "template_id and requirement are required", http.StatusBadRequest)
		return
	}

	payload, err := h.data.StartTask(r.Context(), body.TemplateID, session.OpenID, body.Requirement)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) TaskAction(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.sessions.ReadSession(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	taskID, action, err := parseTaskActionPath(r.URL.Path, r.Method)
	if err != nil {
		if errors.Is(err, errMethodNotAllowed) {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if r.Method == http.MethodGet && action == "" {
		payload, err := h.data.TaskDetail(r.Context(), taskID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || strings.Contains(strings.ToLower(err.Error()), "not found") {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Text string `json:"text"`
	}
	if action == "reply" || action == "reopen" {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		body.Text = strings.TrimSpace(body.Text)
		if body.Text == "" {
			http.Error(w, "text is required", http.StatusBadRequest)
			return
		}
	}

	switch action {
	case "stop":
		err = h.data.StopTask(r.Context(), taskID)
	case "complete":
		err = h.data.CompleteTask(r.Context(), taskID)
	case "delete":
		err = h.data.DeleteTask(r.Context(), taskID)
	case "reply":
		err = h.data.ReplyTask(r.Context(), taskID, body.Text)
	case "reopen":
		err = h.data.ReopenTask(r.Context(), taskID, body.Text)
	default:
		http.Error(w, "unsupported action", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"task_id": taskID,
		"action":  action,
	})
}

var errMethodNotAllowed = errors.New("method not allowed")

func parseTaskActionPath(path string, method string) (taskID string, action string, err error) {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 4 || len(parts) > 5 || parts[0] != "api" || parts[1] != "web" || parts[2] != "tasks" {
		return "", "", fmt.Errorf("not found")
	}
	taskID = strings.TrimSpace(parts[3])
	if taskID == "" {
		return "", "", fmt.Errorf("not found")
	}
	if len(parts) == 4 {
		if method != http.MethodGet {
			return "", "", errMethodNotAllowed
		}
		return taskID, "", nil
	}
	if method != http.MethodPost {
		return "", "", errMethodNotAllowed
	}
	action = strings.TrimSpace(parts[4])
	if action == "" {
		return "", "", fmt.Errorf("not found")
	}
	return taskID, action, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
