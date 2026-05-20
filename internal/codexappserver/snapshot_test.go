package codexappserver

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeServerRequest(t *testing.T) {
	t.Parallel()

	msg := rpcMessage{
		ID:     "srv-1",
		Method: "item/tool/requestUserInput",
		Params: map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"prompt":   "Choose A or B",
		},
	}

	req, ok, err := DecodeServerRequest(msg)
	if err != nil {
		t.Fatalf("DecodeServerRequest returned error: %v", err)
	}
	if !ok {
		t.Fatal("DecodeServerRequest returned ok=false")
	}
	if req.RequestID != "srv-1" || req.ThreadID != "thread-1" || req.TurnID != "turn-1" {
		t.Fatalf("req = %#v", req)
	}
}

func TestThreadWatcherPublishesServerRequestAndResolvedEvent(t *testing.T) {
	t.Parallel()

	watcher := newThreadWatcher("thread-1")
	watcher.apply(rpcMessage{
		ID:     "srv-1",
		Method: "item/tool/requestUserInput",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"prompt":   "Choose A or B",
		}),
	})

	select {
	case event := <-watcher.Events():
		if event.ServerRequest == nil || event.ServerRequest.RequestID != "srv-1" {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("server request event was not published")
	}

	watcher.apply(rpcMessage{
		Method: "serverRequest/resolved",
		Params: mustRawJSON(t, map[string]any{
			"requestId": "srv-1",
		}),
	})

	select {
	case event := <-watcher.Events():
		if event.ResolvedRequestID != "srv-1" {
			t.Fatalf("ResolvedRequestID = %q, want srv-1", event.ResolvedRequestID)
		}
	case <-time.After(time.Second):
		t.Fatal("resolved event was not published")
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return json.RawMessage(data)
}
