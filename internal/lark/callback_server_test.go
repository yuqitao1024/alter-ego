package lark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
)

type fakeEventDispatcher struct {
	req *larkevent.EventReq
}

func (f *fakeEventDispatcher) Handle(ctx context.Context, req *larkevent.EventReq) *larkevent.EventResp {
	f.req = req
	return &larkevent.EventResp{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"toast":{"type":"info","content":"ok"}}`),
	}
}

func TestCallbackServerForwardsCardCallbackToDispatcher(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeEventDispatcher{}
	handler := NewCallbackHandler(dispatcher)
	req := httptest.NewRequest(http.MethodPost, "/lark/card/callback", strings.NewReader(`{"schema":"2.0"}`))
	req.Header.Set("X-Test", "value")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != `{"toast":{"type":"info","content":"ok"}}` {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if dispatcher.req == nil {
		t.Fatal("dispatcher was not called")
	}
	if string(dispatcher.req.Body) != `{"schema":"2.0"}` {
		t.Fatalf("dispatcher body = %q", string(dispatcher.req.Body))
	}
	if dispatcher.req.RequestURI != "/lark/card/callback" {
		t.Fatalf("RequestURI = %q", dispatcher.req.RequestURI)
	}
	if got := dispatcher.req.Header["X-Test"]; len(got) != 1 || got[0] != "value" {
		t.Fatalf("header was not forwarded: %#v", dispatcher.req.Header)
	}
}
