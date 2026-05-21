package lark

import (
	"context"
	"io"
	"net/http"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
)

type callbackDispatcher interface {
	Handle(ctx context.Context, req *larkevent.EventReq) *larkevent.EventResp
}

type CallbackHandler struct {
	dispatcher callbackDispatcher
}

func NewCallbackHandler(dispatcher callbackDispatcher) *CallbackHandler {
	return &CallbackHandler{dispatcher: dispatcher}
}

func (h *CallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.dispatcher == nil {
		http.Error(w, "callback dispatcher is not configured", http.StatusInternalServerError)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp := h.dispatcher.Handle(r.Context(), &larkevent.EventReq{
		Header:     r.Header,
		Body:       body,
		RequestURI: r.URL.Path,
	})
	if resp == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
		return
	}
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if resp.StatusCode == 0 {
		resp.StatusCode = http.StatusOK
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(resp.Body)
}
