package web

import "net/http"

func NewRouter(handler *Handler, callback http.Handler) http.Handler {
	mux := http.NewServeMux()
	if callback != nil {
		mux.Handle("/lark/card/callback", callback)
	}
	if handler == nil {
		return mux
	}
	mux.HandleFunc("/", handler.Root)
	mux.HandleFunc("/login", handler.Login)
	mux.HandleFunc("/auth/lark/start", handler.StartAuth)
	mux.HandleFunc("/auth/lark/callback", handler.Callback)
	mux.HandleFunc("/auth/logout", handler.Logout)
	mux.HandleFunc("/api/web/session", handler.Session)
	mux.HandleFunc("/api/web/dashboard", handler.Dashboard)
	mux.HandleFunc("/api/web/events", handler.Events)
	mux.HandleFunc("/api/web/templates", handler.Templates)
	mux.HandleFunc("/api/web/tasks", handler.CreateTask)
	mux.HandleFunc("/api/web/tasks/", handler.TaskAction)
	return mux
}
