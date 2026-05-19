package codexappserver

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
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

type fakeClient struct {
	notifications chan rpcMessage
}

func newFakeClient() *fakeClient {
	return &fakeClient{notifications: make(chan rpcMessage)}
}

func (f *fakeClient) Close() error                     { return nil }
func (f *fakeClient) Notifications() <-chan rpcMessage { return f.notifications }
func (f *fakeClient) ResumeThread(context.Context, string) error { return nil }
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
