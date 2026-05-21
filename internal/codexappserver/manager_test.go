package codexappserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestManagerReusesOneConnectionPerMachine(t *testing.T) {
	t.Parallel()

	var dialCount int32
	manager := NewManager(ManagerOptions{
		DialClient: func(ctx context.Context, machine MachineRuntimeConfig) (ClientAPI, error) {
			atomic.AddInt32(&dialCount, 1)
			return newFakeClient(), nil
		},
	})

	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: "ws://machine-a:4317"}

	if _, err := manager.WatchTaskThread(context.Background(), machine, "thread-1"); err != nil {
		t.Fatalf("WatchTaskThread thread-1 error: %v", err)
	}
	if _, err := manager.WatchTaskThread(context.Background(), machine, "thread-2"); err != nil {
		t.Fatalf("WatchTaskThread thread-2 error: %v", err)
	}
	if got := atomic.LoadInt32(&dialCount); got != 1 {
		t.Fatalf("dialCount = %d, want 1", got)
	}
}

func TestManagerRedialsMachineAfterNotificationStreamCloses(t *testing.T) {
	t.Parallel()

	var dialCount int32
	first := newFakeClient()
	second := newFakeClient()

	manager := NewManager(ManagerOptions{
		DialClient: func(ctx context.Context, machine MachineRuntimeConfig) (ClientAPI, error) {
			switch atomic.AddInt32(&dialCount, 1) {
			case 1:
				return first, nil
			case 2:
				return second, nil
			default:
				return nil, errors.New("unexpected extra dial")
			}
		},
	})

	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: "ws://machine-a:4317"}
	watcher, err := manager.WatchTaskThread(context.Background(), machine, "thread-1")
	if err != nil {
		t.Fatalf("WatchTaskThread error: %v", err)
	}

	close(first.notifications)
	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot := watcher.Snapshot()
		if snapshot.SubscriptionState == SubscriptionStateError {
			if snapshot.LastSubscriptionError == "" {
				t.Fatal("LastSubscriptionError is empty")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("watcher never entered error state: %#v", snapshot)
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := manager.WatchTaskThread(context.Background(), machine, "thread-1"); err != nil {
		t.Fatalf("WatchTaskThread redial error: %v", err)
	}
	if got := atomic.LoadInt32(&dialCount); got != 2 {
		t.Fatalf("dialCount = %d, want 2", got)
	}
	if snapshot := watcher.Snapshot(); snapshot.SubscriptionState != SubscriptionStateConnecting {
		t.Fatalf("snapshot.SubscriptionState = %q, want %q", snapshot.SubscriptionState, SubscriptionStateConnecting)
	}
}

func TestManagerDefaultDialClientIncludesVersionInInitialize(t *testing.T) {
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
		var params struct {
			ClientInfo ClientInfo `json:"clientInfo"`
		}
		if err := json.Unmarshal(mustJSON(t, request.Params), &params); err != nil {
			t.Fatalf("Unmarshal params: %v", err)
		}
		if params.ClientInfo.Name != "alterego" {
			t.Fatalf("clientInfo.name = %q, want alterego", params.ClientInfo.Name)
		}
		if params.ClientInfo.Version == "" {
			t.Fatal("clientInfo.version is empty")
		}

		if err := conn.WriteJSON(rpcMessage{ID: request.ID, Result: mustJSON(t, map[string]any{"userAgent": "alterego-test"})}); err != nil {
			t.Errorf("WriteJSON initialize response: %v", err)
			return
		}

		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	manager := NewManager(ManagerOptions{})
	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: wsURLFromHTTP(server.URL)}

	watcher, err := manager.WatchTaskThread(context.Background(), machine, "thread-1")
	if err != nil {
		t.Fatalf("WatchTaskThread returned error: %v", err)
	}
	if watcher == nil {
		t.Fatal("watcher is nil")
	}
}

func TestManagerResumeTaskThreadResumesThreadOnFirstAttach(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	manager := NewManager(ManagerOptions{
		DialClient: func(context.Context, MachineRuntimeConfig) (ClientAPI, error) {
			return client, nil
		},
	})

	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: "ws://machine-a:4317"}
	watcher, err := manager.ResumeTaskThread(context.Background(), machine, "thread-1")
	if err != nil {
		t.Fatalf("ResumeTaskThread returned error: %v", err)
	}
	if watcher == nil {
		t.Fatal("watcher is nil")
	}
	if len(client.resumeThreadIDs) != 1 || client.resumeThreadIDs[0] != "thread-1" {
		t.Fatalf("resumeThreadIDs = %#v, want [thread-1]", client.resumeThreadIDs)
	}
}

type fakeClient struct {
	notifications chan rpcMessage
	resumeThreadIDs []string
}

func newFakeClient() *fakeClient {
	return &fakeClient{notifications: make(chan rpcMessage)}
}

func (f *fakeClient) Close() error                     { return nil }
func (f *fakeClient) Notifications() <-chan rpcMessage { return f.notifications }
func (f *fakeClient) ResumeThread(_ context.Context, threadID string) error {
	f.resumeThreadIDs = append(f.resumeThreadIDs, threadID)
	return nil
}
func (f *fakeClient) RespondToServerRequest(context.Context, string, any) error { return nil }
func (f *fakeClient) StartThread(context.Context, ThreadStartRequest) (string, error) {
	return "thread-1", nil
}
func (f *fakeClient) StartTurn(context.Context, TurnStartRequest) (string, error) {
	return "turn-1", nil
}
func (f *fakeClient) SteerTurn(context.Context, TurnSteerRequest) (string, error) {
	return "turn-1", nil
}
func (f *fakeClient) InterruptTurn(context.Context, TurnInterruptRequest) error { return nil }
