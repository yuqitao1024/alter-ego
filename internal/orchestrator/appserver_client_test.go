package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppServerClientStartsThreadAndTurn(t *testing.T) {
	t.Parallel()

	transport := newFakeAppServerTransport()
	transport.enqueueResponse(`{"id":"1","result":{"thread":{"id":"thread_123"}}}`)
	transport.enqueueResponse(`{"id":"2","result":{"turn":{"id":"turn_456"}}}`)

	client := NewAppServerClient(transport)

	threadID, err := client.StartThread(context.Background(), ThreadStartRequest{
		Cwd:              "/srv/tasks/task-1/repo",
		BaseInstructions: "workflow",
	})
	if err != nil {
		t.Fatalf("StartThread returned error: %v", err)
	}
	if threadID != "thread_123" {
		t.Fatalf("threadID = %q, want %q", threadID, "thread_123")
	}

	turnID, err := client.StartTurn(context.Background(), TurnStartRequest{
		ThreadID: "thread_123",
		Input:    "continue",
	})
	if err != nil {
		t.Fatalf("StartTurn returned error: %v", err)
	}
	if turnID != "turn_456" {
		t.Fatalf("turnID = %q, want %q", turnID, "turn_456")
	}

	if len(transport.sentRequests) != 2 {
		t.Fatalf("len(sentRequests) = %d, want 2", len(transport.sentRequests))
	}

	var threadRequest map[string]any
	if err := json.Unmarshal(transport.sentRequests[0], &threadRequest); err != nil {
		t.Fatalf("unmarshal thread request: %v", err)
	}
	if threadRequest["method"] != "thread/start" {
		t.Fatalf("thread request method = %#v, want %q", threadRequest["method"], "thread/start")
	}
	threadParams := threadRequest["params"].(map[string]any)
	if threadParams["cwd"] != "/srv/tasks/task-1/repo" {
		t.Fatalf("thread request cwd = %#v", threadParams["cwd"])
	}
	if threadParams["base_instructions"] != "workflow" {
		t.Fatalf("thread request base_instructions = %#v", threadParams["base_instructions"])
	}

	var turnRequest map[string]any
	if err := json.Unmarshal(transport.sentRequests[1], &turnRequest); err != nil {
		t.Fatalf("unmarshal turn request: %v", err)
	}
	if turnRequest["method"] != "turn/start" {
		t.Fatalf("turn request method = %#v, want %q", turnRequest["method"], "turn/start")
	}
	turnParams := turnRequest["params"].(map[string]any)
	if turnParams["thread_id"] != "thread_123" {
		t.Fatalf("turn request thread_id = %#v", turnParams["thread_id"])
	}
	if turnParams["input"] != "continue" {
		t.Fatalf("turn request input = %#v", turnParams["input"])
	}
}

func TestAppServerClientSteersAndInterruptsTurn(t *testing.T) {
	t.Parallel()

	transport := newFakeAppServerTransport()
	transport.enqueueResponse(`{"id":"1","result":{"turnId":"turn_999"}}`)
	transport.enqueueResponse(`{"id":"2","result":{}}`)

	client := NewAppServerClient(transport)

	turnID, err := client.SteerTurn(context.Background(), TurnSteerRequest{
		TurnID: "turn_456",
		Input:  "continue",
	})
	if err != nil {
		t.Fatalf("SteerTurn returned error: %v", err)
	}
	if turnID != "turn_999" {
		t.Fatalf("turnID = %q, want %q", turnID, "turn_999")
	}

	if err := client.InterruptTurn(context.Background(), TurnInterruptRequest{
		ThreadID: "thread_123",
		TurnID:   "turn_999",
	}); err != nil {
		t.Fatalf("InterruptTurn returned error: %v", err)
	}

	if len(transport.sentRequests) != 2 {
		t.Fatalf("len(sentRequests) = %d, want 2", len(transport.sentRequests))
	}
}

func TestAppServerClientSerializesOverlappingRPCs(t *testing.T) {
	t.Parallel()

	transport := newBlockingAppServerTransport()
	client := NewAppServerClient(transport)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = client.StartThread(context.Background(), ThreadStartRequest{
			Cwd:              "/srv/tasks/task-1/repo",
			BaseInstructions: "workflow",
		})
	}()

	transport.waitForSentRequests(t, 1)

	secondStarted := make(chan struct{})
	secondDone := make(chan struct{})
	go func() {
		close(secondStarted)
		defer close(secondDone)
		_, _ = client.StartTurn(context.Background(), TurnStartRequest{
			ThreadID: "thread_123",
			Input:    "continue",
		})
	}()

	<-secondStarted

	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		transport.mu.Lock()
		got := len(transport.sentRequests)
		transport.mu.Unlock()
		if got > 1 {
			t.Fatal("second RPC sent before first RPC completed")
		}
		time.Sleep(2 * time.Millisecond)
	}

	transport.enqueueResponse(`{"id":"1","result":{"thread":{"id":"thread_123"}}}`)

	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first RPC did not complete")
	}

	transport.waitForSentRequests(t, 2)
	transport.enqueueResponse(`{"id":"2","result":{"turn":{"id":"turn_456"}}}`)

	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("second RPC did not complete")
	}
}

func TestStdioAppServerTransportRecvCanceledContextDoesNotConsumeFutureFrame(t *testing.T) {
	t.Parallel()

	stdoutReader, stdoutWriter := io.Pipe()
	transport := newTestStdioAppServerTransport(stdoutReader, strings.NewReader(""), nil)
	defer func() {
		_ = transport.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := transport.Recv(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Recv error = %v, want deadline exceeded", err)
	}

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_, _ = io.WriteString(stdoutWriter, "{\"id\":\"1\",\"result\":{\"thread\":{\"id\":\"thread_123\"}}}\n")
		_ = stdoutWriter.Close()
	}()

	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("writer blocked; recv cancellation likely leaked reader state")
	}

	frame, err := transport.Recv(context.Background())
	if err != nil {
		t.Fatalf("second Recv returned error: %v", err)
	}
	if string(frame) != `{"id":"1","result":{"thread":{"id":"thread_123"}}}` {
		t.Fatalf("frame = %q", frame)
	}
}

type fakeAppServerTransport struct {
	mu           sync.Mutex
	sentRequests [][]byte
	responses    [][]byte
	closed       bool
}

func newFakeAppServerTransport() *fakeAppServerTransport {
	return &fakeAppServerTransport{}
}

func (f *fakeAppServerTransport) enqueueResponse(response string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append(f.responses, []byte(response))
}

func (f *fakeAppServerTransport) Send(_ context.Context, request []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := append([]byte(nil), request...)
	f.sentRequests = append(f.sentRequests, copied)
	return nil, nil
}

func (f *fakeAppServerTransport) Recv(_ context.Context) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.responses) == 0 {
		return nil, nil
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return append([]byte(nil), response...), nil
}

func (f *fakeAppServerTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

type blockingAppServerTransport struct {
	mu                sync.Mutex
	sentRequests      [][]byte
	sentRequestSignal chan struct{}
	responses         chan []byte
}

func newBlockingAppServerTransport() *blockingAppServerTransport {
	return &blockingAppServerTransport{
		sentRequestSignal: make(chan struct{}, 8),
		responses:         make(chan []byte, 8),
	}
}

func (f *blockingAppServerTransport) enqueueResponse(response string) {
	f.responses <- []byte(response)
}

func (f *blockingAppServerTransport) waitForSentRequests(t *testing.T, want int) {
	t.Helper()

	deadline := time.After(time.Second)
	for {
		f.mu.Lock()
		got := len(f.sentRequests)
		f.mu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-f.sentRequestSignal:
		case <-deadline:
			t.Fatalf("len(sentRequests) = %d, want at least %d", got, want)
		}
	}
}

func (f *blockingAppServerTransport) Send(_ context.Context, request []byte) ([]byte, error) {
	f.mu.Lock()
	f.sentRequests = append(f.sentRequests, append([]byte(nil), request...))
	f.mu.Unlock()
	f.sentRequestSignal <- struct{}{}
	return nil, nil
}

func (f *blockingAppServerTransport) Recv(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response := <-f.responses:
		return append([]byte(nil), response...), nil
	}
}

func (f *blockingAppServerTransport) Close() error {
	return nil
}
