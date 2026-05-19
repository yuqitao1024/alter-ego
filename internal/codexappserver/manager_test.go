package codexappserver

import (
	"context"
	"sync/atomic"
	"testing"
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

type fakeClient struct {
	notifications chan rpcMessage
}

func newFakeClient() *fakeClient {
	return &fakeClient{notifications: make(chan rpcMessage)}
}

func (f *fakeClient) Close() error                     { return nil }
func (f *fakeClient) Notifications() <-chan rpcMessage { return f.notifications }
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
