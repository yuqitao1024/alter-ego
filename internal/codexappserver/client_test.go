package codexappserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage() error: %v", err)
			return
		}

		var initialize rpcMessage
		if err := json.Unmarshal(payload, &initialize); err != nil {
			t.Errorf("Unmarshal initialize error: %v", err)
			return
		}
		if initialize.Method != "initialize" {
			t.Fatalf("first method = %q, want initialize", initialize.Method)
		}

		if err := conn.WriteJSON(rpcMessage{ID: initialize.ID, Result: mustJSON(t, map[string]any{"userAgent": "alterego-test"})}); err != nil {
			t.Fatalf("WriteJSON initialize response: %v", err)
		}

		var requests []rpcMessage
		for len(requests) < 2 {
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

		if err := conn.WriteJSON(rpcMessage{ID: requests[1].ID, Result: mustJSON(t, map[string]any{"turn": map[string]any{"id": "turn-2"}})}); err != nil {
			t.Fatalf("WriteJSON second response: %v", err)
		}
		if err := conn.WriteJSON(rpcMessage{ID: requests[0].ID, Result: mustJSON(t, map[string]any{"thread": map[string]any{"id": "thread-1"}})}); err != nil {
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

func TestNewClientReturnsInitializeError(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error: %v", err)
			return
		}
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage() error: %v", err)
			return
		}

		var request rpcMessage
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Errorf("Unmarshal() error: %v", err)
			return
		}

		if err := conn.WriteJSON(rpcMessage{
			ID:    request.ID,
			Error: &rpcError{Code: -32000, Message: "initialize failed"},
		}); err != nil {
			t.Errorf("WriteJSON() error: %v", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := NewClient(ctx, ClientOptions{
		URL:        wsURLFromHTTP(server.URL),
		ClientInfo: ClientInfo{Name: "alterego"},
	})
	if err == nil {
		t.Fatal("NewClient returned nil error")
	}
	if got := err.Error(); got != "initialize: initialize failed" {
		t.Fatalf("NewClient error = %q", got)
	}
}

func TestNewClientWaitsForInitializeResponse(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	releaseInitialize := make(chan struct{})
	var initializeSeen atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error: %v", err)
			return
		}
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage() error: %v", err)
			return
		}

		var request rpcMessage
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Errorf("Unmarshal() error: %v", err)
			return
		}
		initializeSeen.Store(true)

		<-releaseInitialize

		if err := conn.WriteJSON(rpcMessage{
			ID:     request.ID,
			Result: mustJSON(t, map[string]any{"userAgent": "alterego-test"}),
		}); err != nil {
			t.Errorf("WriteJSON() error: %v", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resultCh := make(chan error, 1)
	go func() {
		_, err := NewClient(ctx, ClientOptions{
			URL:        wsURLFromHTTP(server.URL),
			ClientInfo: ClientInfo{Name: "alterego"},
		})
		resultCh <- err
	}()

	deadline := time.After(200 * time.Millisecond)
	for !initializeSeen.Load() {
		select {
		case <-deadline:
			t.Fatal("initialize request was not observed")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	select {
	case err := <-resultCh:
		t.Fatalf("NewClient returned before initialize response: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseInitialize)

	select {
	case err := <-resultCh:
		if err != nil {
			t.Fatalf("NewClient returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("NewClient did not return after initialize response")
	}
}

func TestClientDoesNotPublishUnmatchedResponseIDsToNotifications(t *testing.T) {
	t.Parallel()

	transport := &stubTransport{
		recvCh: make(chan recvResult, 4),
	}
	client := newTestClient(transport)
	defer client.Close()

	transport.recvCh <- recvResult{payload: mustJSON(t, rpcMessage{
		ID:     "999",
		Result: mustJSON(t, map[string]any{"ignored": true}),
	})}

	select {
	case msg, ok := <-client.Notifications():
		if !ok {
			t.Fatal("Notifications closed unexpectedly")
		}
		t.Fatalf("unexpected notification: %+v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

type stubTransport struct {
	recvCh  chan recvResult
	closeMu sync.Mutex
	closed  bool
}

type recvResult struct {
	payload []byte
	err     error
}

func (s *stubTransport) Send(context.Context, []byte) error {
	return nil
}

func (s *stubTransport) Recv(context.Context) ([]byte, error) {
	result, ok := <-s.recvCh
	if !ok {
		return nil, errors.New("stub transport closed")
	}
	return result.payload, result.err
}

func (s *stubTransport) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.recvCh)
	return nil
}

func newTestClient(transport Transport) *Client {
	client := &Client{
		transport:     transport,
		pending:       make(map[string]chan callResult),
		notifications: make(chan rpcMessage, 16),
		readDone:      make(chan struct{}),
	}
	go client.readLoop()
	return client
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	return data
}
