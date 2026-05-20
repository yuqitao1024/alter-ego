package codexappserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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

		_, payload, err = conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage initialized ack error: %v", err)
			return
		}
		var initialized rpcMessage
		if err := json.Unmarshal(payload, &initialized); err != nil {
			t.Errorf("Unmarshal initialized error: %v", err)
			return
		}
		if initialized.Method != "initialized" {
			t.Fatalf("second method = %q, want initialized", initialized.Method)
		}

		requestsByMethod := make(map[string]rpcMessage, 2)
		for len(requestsByMethod) < 2 {
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
			requestsByMethod[msg.Method] = msg
		}

		threadRequest, ok := requestsByMethod["thread/start"]
		if !ok {
			t.Fatal("thread/start request was not received")
		}
		turnRequest, ok := requestsByMethod["turn/start"]
		if !ok {
			t.Fatal("turn/start request was not received")
		}
		var turnParams map[string]any
		if err := json.Unmarshal(mustJSON(t, turnRequest.Params), &turnParams); err != nil {
			t.Fatalf("Unmarshal turn params: %v", err)
		}
		if _, ok := turnParams["threadId"]; !ok {
			t.Fatalf("turn/start params = %#v, want threadId", turnParams)
		}
		if _, ok := turnParams["input"]; !ok {
			t.Fatalf("turn/start params = %#v, want input", turnParams)
		}

		if err := conn.WriteJSON(rpcMessage{ID: turnRequest.ID, Result: mustJSON(t, map[string]any{"turn": map[string]any{"id": "turn-2"}})}); err != nil {
			t.Fatalf("WriteJSON second response: %v", err)
		}
		if err := conn.WriteJSON(rpcMessage{ID: threadRequest.ID, Result: mustJSON(t, map[string]any{"thread": map[string]any{"id": "thread-1"}})}); err != nil {
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

	threadIDCh := make(chan string, 1)
	turnIDCh := make(chan string, 1)

	go func() {
		defer wg.Done()
		threadID, err := client.StartThread(ctx, ThreadStartRequest{Cwd: "/srv/task/repo"})
		if err != nil {
			t.Errorf("StartThread returned error: %v", err)
			return
		}
		threadIDCh <- threadID
	}()
	go func() {
		defer wg.Done()
		turnID, err := client.StartTurn(ctx, TurnStartRequest{ThreadID: "thread-1", Input: []InputItem{{Type: "text", Text: "continue"}}})
		if err != nil {
			t.Errorf("StartTurn returned error: %v", err)
			return
		}
		turnIDCh <- turnID
	}()

	wg.Wait()

	select {
	case threadID := <-threadIDCh:
		if threadID != "thread-1" {
			t.Fatalf("StartThread threadID = %q, want %q", threadID, "thread-1")
		}
	default:
		t.Fatal("StartThread did not return a thread ID")
	}

	select {
	case turnID := <-turnIDCh:
		if turnID != "turn-2" {
			t.Fatalf("StartTurn turnID = %q, want %q", turnID, "turn-2")
		}
	default:
		t.Fatal("StartTurn did not return a turn ID")
	}
}

func TestNewClientSendsInitializedNotificationAfterInitialize(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	seen := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error: %v", err)
			return
		}
		defer conn.Close()

		methods := make([]string, 0, 2)
		for len(methods) < 2 {
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
			methods = append(methods, request.Method)
			if request.Method == "initialize" {
				if err := conn.WriteJSON(rpcMessage{
					ID:     request.ID,
					Result: mustJSON(t, map[string]any{"userAgent": "alterego-test"}),
				}); err != nil {
					t.Errorf("WriteJSON() error: %v", err)
					return
				}
			}
		}
		seen <- methods
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientOptions{
		URL:        wsURLFromHTTP(server.URL),
		ClientInfo: ClientInfo{Name: "alterego"},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	defer client.Close()

	methods := <-seen
	if len(methods) != 2 || methods[0] != "initialize" || methods[1] != "initialized" {
		t.Fatalf("methods = %v, want [initialize initialized]", methods)
	}
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

func TestClientDoesNotDropNotificationsWhenBufferIsFull(t *testing.T) {
	t.Parallel()

	transport := &stubTransport{
		recvCh: make(chan recvResult, 4),
	}
	client := newTestClientWithNotificationBuffer(transport, 1)

	const notificationCount = 2
	for i := 0; i < notificationCount; i++ {
		transport.recvCh <- recvResult{payload: mustJSON(t, rpcMessage{
			Method: "event/" + strconv.Itoa(i),
		})}
	}
	transport.recvCh <- recvResult{err: errors.New("transport closed")}
	time.Sleep(50 * time.Millisecond)

	gotMethods := make([]string, 0, notificationCount)
	for msg := range client.Notifications() {
		gotMethods = append(gotMethods, msg.Method)
	}

	if len(gotMethods) != notificationCount {
		t.Fatalf("received %d notifications, want %d", len(gotMethods), notificationCount)
	}
	for i, method := range gotMethods {
		want := "event/" + strconv.Itoa(i)
		if method != want {
			t.Fatalf("notification %d method = %q, want %q", i, method, want)
		}
	}
}

func TestClientPublishesServerInitiatedRequestsWithIDs(t *testing.T) {
	t.Parallel()

	transport := &stubTransport{
		recvCh: make(chan recvResult, 2),
	}
	client := newTestClient(transport)
	defer client.Close()

	transport.recvCh <- recvResult{payload: mustJSON(t, rpcMessage{
		ID:     "srv-1",
		Method: "item/tool/requestUserInput",
		Params: map[string]any{"threadId": "thread-1", "prompt": "Choose A or B"},
	})}

	select {
	case msg := <-client.Notifications():
		if msg.Method != "item/tool/requestUserInput" {
			t.Fatalf("Method = %q, want item/tool/requestUserInput", msg.Method)
		}
		if msg.ID != "srv-1" {
			t.Fatalf("ID = %q, want srv-1", msg.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("server request notification was not published")
	}
}

func TestClientRespondToServerRequestSendsJSONRPCResult(t *testing.T) {
	t.Parallel()

	transport := &stubTransport{
		recvCh: make(chan recvResult),
	}
	client := newTestClient(transport)
	defer client.Close()

	if err := client.RespondToServerRequest(context.Background(), "srv-1", map[string]any{"decision": "accept"}); err != nil {
		t.Fatalf("RespondToServerRequest returned error: %v", err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(transport.sent))
	}

	var msg rpcMessage
	if err := json.Unmarshal(transport.sent[0], &msg); err != nil {
		t.Fatalf("Unmarshal sent message: %v", err)
	}
	if msg.ID != "srv-1" || msg.Method != "" {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestClientSteerTurnSendsThreadIdAndExpectedTurnId(t *testing.T) {
	t.Parallel()

	transport := &stubTransport{
		recvCh: make(chan recvResult, 1),
	}
	client := newTestClient(transport)
	defer client.Close()

	go func() {
		time.Sleep(10 * time.Millisecond)
		transport.recvCh <- recvResult{payload: mustJSON(t, rpcMessage{
			ID:     "1",
			Result: mustJSON(t, map[string]any{"turnId": "turn-2"}),
		})}
	}()

	_, err := client.SteerTurn(context.Background(), TurnSteerRequest{
		ThreadID:       "thread-1",
		ExpectedTurnID: "turn-1",
		Input:          []InputItem{{Type: "text", Text: "continue"}},
	})
	if err != nil {
		t.Fatalf("SteerTurn returned error: %v", err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(transport.sent))
	}

	var msg rpcMessage
	if err := json.Unmarshal(transport.sent[0], &msg); err != nil {
		t.Fatalf("Unmarshal sent message: %v", err)
	}
	var params map[string]any
	if err := json.Unmarshal(mustJSON(t, msg.Params), &params); err != nil {
		t.Fatalf("Unmarshal params: %v", err)
	}
	if params["threadId"] != "thread-1" {
		t.Fatalf("threadId = %#v, want thread-1", params["threadId"])
	}
	if params["expectedTurnId"] != "turn-1" {
		t.Fatalf("expectedTurnId = %#v, want turn-1", params["expectedTurnId"])
	}
	if _, ok := params["turnId"]; ok {
		t.Fatalf("params unexpectedly included turnId: %#v", params)
	}
}

type stubTransport struct {
	recvCh  chan recvResult
	closeMu sync.Mutex
	closed  bool
	sent    [][]byte
}

type recvResult struct {
	payload []byte
	err     error
}

func (s *stubTransport) Send(_ context.Context, payload []byte) error {
	s.sent = append(s.sent, append([]byte(nil), payload...))
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
	return newTestClientWithNotificationBuffer(transport, 16)
}

func newTestClientWithNotificationBuffer(transport Transport, notificationBuffer int) *Client {
	client := &Client{
		transport:     transport,
		pending:       make(map[string]chan callResult),
		notifications: make(chan rpcMessage, notificationBuffer),
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
