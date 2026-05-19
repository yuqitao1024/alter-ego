package codexappserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClientInitializesAndRoutesOutOfOrderResponses(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error: %v", err)
			return
		}
		defer conn.Close()

		var requests []rpcMessage
		for len(requests) < 3 {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("ReadMessage() error: %v", err)
				return
			}
			var msg rpcMessage
			if err := json.Unmarshal(payload, &msg); err != nil {
				t.Errorf("Unmarshal() error: %v", err)
				return
			}
			requests = append(requests, msg)
		}

		if requests[0].Method != "initialize" {
			t.Fatalf("first method = %q, want initialize", requests[0].Method)
		}

		if err := conn.WriteJSON(rpcMessage{ID: requests[0].ID, Result: mustJSON(t, map[string]any{"userAgent": "alterego-test"})}); err != nil {
			t.Fatalf("WriteJSON initialize response: %v", err)
		}
		if err := conn.WriteJSON(rpcMessage{ID: requests[2].ID, Result: mustJSON(t, map[string]any{"turn": map[string]any{"id": "turn-2"}})}); err != nil {
			t.Fatalf("WriteJSON second response: %v", err)
		}
		if err := conn.WriteJSON(rpcMessage{ID: requests[1].ID, Result: mustJSON(t, map[string]any{"thread": map[string]any{"id": "thread-1"}})}); err != nil {
			t.Fatalf("WriteJSON first response: %v", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientOptions{
		URL: wsURLFromHTTP(server.URL),
		ClientInfo: ClientInfo{
			Name:    "alterego",
			Title:   "Alter Ego",
			Version: "test",
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	defer client.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if _, err := client.StartThread(ctx, ThreadStartRequest{Cwd: "/srv/task/repo"}); err != nil {
			t.Errorf("StartThread returned error: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := client.StartTurn(ctx, TurnStartRequest{ThreadID: "thread-1", Input: "continue"}); err != nil {
			t.Errorf("StartTurn returned error: %v", err)
		}
	}()

	wg.Wait()
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	return data
}
