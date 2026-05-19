package orchestrator

import (
	"context"
	"encoding/json"
	"testing"
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

type fakeAppServerTransport struct {
	sentRequests [][]byte
	responses    [][]byte
	closed       bool
}

func newFakeAppServerTransport() *fakeAppServerTransport {
	return &fakeAppServerTransport{}
}

func (f *fakeAppServerTransport) enqueueResponse(response string) {
	f.responses = append(f.responses, []byte(response))
}

func (f *fakeAppServerTransport) Send(_ context.Context, request []byte) ([]byte, error) {
	copied := append([]byte(nil), request...)
	f.sentRequests = append(f.sentRequests, copied)
	return nil, nil
}

func (f *fakeAppServerTransport) Recv(_ context.Context) ([]byte, error) {
	if len(f.responses) == 0 {
		return nil, nil
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	return append([]byte(nil), response...), nil
}

func (f *fakeAppServerTransport) Close() error {
	f.closed = true
	return nil
}
