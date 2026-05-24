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

func TestManagerCleanupTaskThreadUnsubscribesAndArchives(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	manager := NewManager(ManagerOptions{
		DialClient: func(context.Context, MachineRuntimeConfig) (ClientAPI, error) {
			return client, nil
		},
	})

	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: "ws://machine-a:4317"}
	if err := manager.CleanupTaskThread(context.Background(), machine, "thread-1"); err != nil {
		t.Fatalf("CleanupTaskThread returned error: %v", err)
	}
	if client.unsubscribedThreadID != "thread-1" {
		t.Fatalf("unsubscribedThreadID = %q, want thread-1", client.unsubscribedThreadID)
	}
	if client.archivedThreadID != "thread-1" {
		t.Fatalf("archivedThreadID = %q, want thread-1", client.archivedThreadID)
	}
}

func TestManagerCleanupTaskThreadArchivesWhenAlreadyUnsubscribed(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	client.unsubscribeStatus = "notSubscribed"
	manager := NewManager(ManagerOptions{
		DialClient: func(context.Context, MachineRuntimeConfig) (ClientAPI, error) {
			return client, nil
		},
	})

	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: "ws://machine-a:4317"}
	if err := manager.CleanupTaskThread(context.Background(), machine, "thread-1"); err != nil {
		t.Fatalf("CleanupTaskThread returned error: %v", err)
	}
	if client.archivedThreadID != "thread-1" {
		t.Fatalf("archivedThreadID = %q, want thread-1", client.archivedThreadID)
	}
}

func TestManagerResumeTaskThreadHydratesSnapshotFromResumeHistory(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	client.resumeResult = mustRawJSON(t, map[string]any{
		"thread": map[string]any{
			"id": "thread-1",
			"status": map[string]any{
				"type": "idle",
			},
			"turns": []map[string]any{
				{
					"id":     "turn-1",
					"status": "completed",
					"items": []map[string]any{
						{
							"id":   "item-plan",
							"type": "plan",
							"text": "1. Inspect state",
						},
						{
							"id":      "item-cmd",
							"type":    "commandExecution",
							"command": "go test ./...",
						},
						{
							"id":   "item-msg",
							"type": "agentMessage",
							"text": "Applied parser fix.",
						},
					},
				},
			},
		},
	})
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

	snapshot := watcher.Snapshot()
	if snapshot.ThreadStatus != "idle" {
		t.Fatalf("ThreadStatus = %q, want idle", snapshot.ThreadStatus)
	}
	if snapshot.ActiveTurnID != "turn-1" {
		t.Fatalf("ActiveTurnID = %q, want turn-1", snapshot.ActiveTurnID)
	}
	if snapshot.ActiveTurnStatus != "completed" {
		t.Fatalf("ActiveTurnStatus = %q, want completed", snapshot.ActiveTurnStatus)
	}
	if snapshot.LatestPlan != "1. Inspect state" {
		t.Fatalf("LatestPlan = %q", snapshot.LatestPlan)
	}
	if snapshot.LatestCommand != "go test ./..." {
		t.Fatalf("LatestCommand = %q, want go test ./...", snapshot.LatestCommand)
	}
	if snapshot.LatestSummary != "Applied parser fix." {
		t.Fatalf("LatestSummary = %q, want Applied parser fix.", snapshot.LatestSummary)
	}
}

type fakeClient struct {
	notifications        chan rpcMessage
	resumeThreadIDs      []string
	resumeResult         json.RawMessage
	startThreadRequests  []ThreadStartRequest
	startTurnRequests    []TurnStartRequest
	steerTurnRequests    []TurnSteerRequest
	steerTurnErr         error
	unsubscribedThreadID string
	archivedThreadID     string
	unsubscribeStatus    string
}

func newFakeClient() *fakeClient {
	return &fakeClient{notifications: make(chan rpcMessage)}
}

func (f *fakeClient) Close() error                     { return nil }
func (f *fakeClient) Notifications() <-chan rpcMessage { return f.notifications }
func (f *fakeClient) ResumeThread(_ context.Context, threadID string) (Thread, error) {
	f.resumeThreadIDs = append(f.resumeThreadIDs, threadID)
	if len(f.resumeResult) == 0 {
		return Thread{}, nil
	}
	var result struct {
		Thread Thread `json:"thread"`
	}
	if err := json.Unmarshal(f.resumeResult, &result); err != nil {
		return Thread{}, err
	}
	return result.Thread, nil
}
func (f *fakeClient) RespondToServerRequest(context.Context, string, any) error { return nil }
func (f *fakeClient) StartThread(_ context.Context, req ThreadStartRequest) (string, error) {
	f.startThreadRequests = append(f.startThreadRequests, req)
	return "thread-1", nil
}
func (f *fakeClient) StartTurn(_ context.Context, req TurnStartRequest) (string, error) {
	f.startTurnRequests = append(f.startTurnRequests, req)
	return "turn-1", nil
}
func (f *fakeClient) SteerTurn(_ context.Context, req TurnSteerRequest) (string, error) {
	f.steerTurnRequests = append(f.steerTurnRequests, req)
	if f.steerTurnErr != nil {
		return "", f.steerTurnErr
	}
	return "turn-1", nil
}
func (f *fakeClient) InterruptTurn(context.Context, TurnInterruptRequest) error { return nil }
func (f *fakeClient) UnsubscribeThread(_ context.Context, threadID string) (string, error) {
	f.unsubscribedThreadID = threadID
	if f.unsubscribeStatus != "" {
		return f.unsubscribeStatus, nil
	}
	return "unsubscribed", nil
}
func (f *fakeClient) ArchiveThread(_ context.Context, threadID string) error {
	f.archivedThreadID = threadID
	return nil
}

func TestManagerSendTaskInputStartsNewTurnWhenActiveTurnIsGone(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	client.steerTurnErr = errors.New("turn/steer: no active turn to steer")
	manager := NewManager(ManagerOptions{
		DialClient: func(context.Context, MachineRuntimeConfig) (ClientAPI, error) {
			return client, nil
		},
	})

	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: "ws://machine-a:4317"}
	turnID, err := manager.SendTaskInput(context.Background(), machine, SendTaskInputRequest{
		ThreadID:       "thread-1",
		ActiveTurnID:   "turn-old",
		Cwd:            "/srv/backend",
		ApprovalPolicy: "never",
		SandboxPolicy: SandboxPolicy{
			Type:          "dangerFullAccess",
			NetworkAccess: true,
		},
		Input: "continue",
	})
	if err != nil {
		t.Fatalf("SendTaskInput returned error: %v", err)
	}

	if turnID != "turn-1" {
		t.Fatalf("turnID = %q, want turn-1", turnID)
	}
	if len(client.steerTurnRequests) != 1 {
		t.Fatalf("steerTurnRequests = %#v, want one request", client.steerTurnRequests)
	}
	if len(client.startTurnRequests) != 1 {
		t.Fatalf("startTurnRequests = %#v, want one fallback request", client.startTurnRequests)
	}
	if client.startTurnRequests[0].ThreadID != "thread-1" {
		t.Fatalf("fallback ThreadID = %q, want thread-1", client.startTurnRequests[0].ThreadID)
	}
	if client.startTurnRequests[0].ExpectedTurnID != "" {
		t.Fatalf("fallback ExpectedTurnID = %q, want empty", client.startTurnRequests[0].ExpectedTurnID)
	}
	if client.startTurnRequests[0].Cwd != "/srv/backend" {
		t.Fatalf("fallback Cwd = %q, want /srv/backend", client.startTurnRequests[0].Cwd)
	}
	if client.startTurnRequests[0].ApprovalPolicy != "never" {
		t.Fatalf("fallback ApprovalPolicy = %q, want never", client.startTurnRequests[0].ApprovalPolicy)
	}
	if client.startTurnRequests[0].SandboxPolicy.Type != "dangerFullAccess" {
		t.Fatalf("fallback SandboxPolicy.Type = %q, want dangerFullAccess", client.startTurnRequests[0].SandboxPolicy.Type)
	}
}

func TestManagerStartTaskSessionUsesThreadSandboxVariantExpectedByThreadStart(t *testing.T) {
	t.Parallel()

	client := newFakeClient()
	manager := NewManager(ManagerOptions{
		DialClient: func(context.Context, MachineRuntimeConfig) (ClientAPI, error) {
			return client, nil
		},
	})

	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: "ws://machine-a:4317"}
	threadID, turnID, err := manager.StartTaskSession(context.Background(), machine, StartTaskSessionRequest{
		Cwd:            "/srv/backend",
		Input:          "continue",
		ApprovalPolicy: "never",
		SandboxPolicy: SandboxPolicy{
			Type:          "dangerFullAccess",
			NetworkAccess: true,
		},
	})
	if err != nil {
		t.Fatalf("StartTaskSession returned error: %v", err)
	}
	if threadID != "thread-1" || turnID != "turn-1" {
		t.Fatalf("threadID=%q turnID=%q, want thread-1/turn-1", threadID, turnID)
	}
	if len(client.startThreadRequests) != 1 {
		t.Fatalf("startThreadRequests = %#v, want one request", client.startThreadRequests)
	}
	if client.startThreadRequests[0].Sandbox != "danger-full-access" {
		t.Fatalf("thread sandbox = %q, want danger-full-access", client.startThreadRequests[0].Sandbox)
	}
	if len(client.startTurnRequests) != 1 {
		t.Fatalf("startTurnRequests = %#v, want one request", client.startTurnRequests)
	}
	if client.startTurnRequests[0].SandboxPolicy.Type != "dangerFullAccess" {
		t.Fatalf("turn sandbox type = %q, want dangerFullAccess", client.startTurnRequests[0].SandboxPolicy.Type)
	}
}
