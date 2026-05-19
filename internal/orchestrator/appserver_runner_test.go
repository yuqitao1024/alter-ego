package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAppServerRunnerStartsThreadBackedSession(t *testing.T) {
	t.Parallel()

	client := &fakeAppServerRunnerClient{
		startThreadID: "thread_123",
		startTurnID:   "turn_456",
	}
	proxy := &fakeAppServerRunnerProxy{transport: &fakeAppServerRunnerTransport{}}
	runner := NewAppServerRunner(proxy, client)
	workspace := &fakeSSHTransport{}
	runner.transport = workspace
	runner.machineResolver = func(machineID string) (MachineConfig, error) {
		return MachineConfig{ID: machineID}, nil
	}

	session, err := runner.StartInteractiveSession(context.Background(), StartRequest{
		Machine: MachineConfig{
			ID:                 "A5-82",
			Host:               "127.0.0.1",
			User:               "root",
			ShellInit:          []string{"source /home/y00621698/env.sh"},
			AppServerSocket:    "/home/y00621698/.codex/app-server.sock",
			AppServerBootstrap: []string{`codex remote-control -c model=\"gpt-5.4\"`},
		},
		TaskID:              "task-1",
		RepositoryID:        "simt-stl",
		RemoteRepoURL:       "git@gitcode.com:cann-sigs/simt-stl.git",
		RemoteWorkspaceRoot: "/srv/codex-tasks",
		CheckoutBranch:      "main",
		UserRequest:         "Implement issue #20",
		WorkflowContent:     "workflow",
	})
	if err != nil {
		t.Fatalf("StartInteractiveSession returned error: %v", err)
	}

	if session.ThreadID != "thread_123" {
		t.Fatalf("session.ThreadID = %q, want %q", session.ThreadID, "thread_123")
	}
	if session.ActiveTurnID != "turn_456" {
		t.Fatalf("session.ActiveTurnID = %q, want %q", session.ActiveTurnID, "turn_456")
	}
	if session.Workdir != "/srv/codex-tasks/task-1/repo" {
		t.Fatalf("session.Workdir = %q, want %q", session.Workdir, "/srv/codex-tasks/task-1/repo")
	}

	if len(workspace.commands) != 1 {
		t.Fatalf("len(commands) = %d, want 1", len(workspace.commands))
	}
	if !strings.Contains(workspace.commands[0], "git clone 'git@gitcode.com:cann-sigs/simt-stl.git' repo") {
		t.Fatalf("command = %q", workspace.commands[0])
	}
	if client.startThreadReq.Cwd != "/srv/codex-tasks/task-1/repo" {
		t.Fatalf("StartThread cwd = %q", client.startThreadReq.Cwd)
	}
	if !strings.Contains(client.startTurnReq.Input, "Implement issue #20") {
		t.Fatalf("StartTurn input = %q", client.startTurnReq.Input)
	}
}

func TestAppServerRunnerCaptureOutputAggregatesThreadItems(t *testing.T) {
	t.Parallel()

	client := &fakeAppServerRunnerClient{
		thread: AppServerThread{ID: "thread_123", Status: "running"},
		items: []AppServerThreadItem{
			fakeThreadItem(t, "item_1", "agent_message", map[string]any{"text": "Investigating the failing test."}),
			fakeThreadItem(t, "item_2", "plan", map[string]any{"text": "1. Update runner\n2. Add tests"}),
			fakeThreadItem(t, "item_3", "command_execution", map[string]any{"command": "go test ./internal/orchestrator"}),
		},
	}
	runner := NewAppServerRunner(&fakeAppServerRunnerProxy{}, client)
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return MachineConfig{ID: machineID}, nil }

	window, err := runner.CaptureOutput(context.Background(), RemoteSession{
		ThreadID: "thread_123",
	})
	if err != nil {
		t.Fatalf("CaptureOutput returned error: %v", err)
	}

	if window.Summary == "" {
		t.Fatal("Summary is empty")
	}
	if !strings.Contains(window.Summary, "Investigating the failing test.") {
		t.Fatalf("Summary = %q", window.Summary)
	}
	if !strings.Contains(window.Summary, "1. Update runner") {
		t.Fatalf("Summary = %q", window.Summary)
	}
	if !strings.Contains(window.Summary, "go test ./internal/orchestrator") {
		t.Fatalf("Summary = %q", window.Summary)
	}
	if window.SessionState.ThreadStatus != "running" {
		t.Fatalf("SessionState.ThreadStatus = %q, want %q", window.SessionState.ThreadStatus, "running")
	}
}

func TestAppServerRunnerSendInteractiveInputSteersActiveTurn(t *testing.T) {
	t.Parallel()

	client := &fakeAppServerRunnerClient{}
	runner := NewAppServerRunner(&fakeAppServerRunnerProxy{}, client)
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return MachineConfig{ID: machineID}, nil }

	client.steerTurnID = "turn_999"
	updated, err := runner.SendInteractiveInput(context.Background(), RemoteSession{
		ThreadID:     "thread_123",
		ActiveTurnID: "turn_456",
	}, "continue with the fix")
	if err != nil {
		t.Fatalf("SendInteractiveInput returned error: %v", err)
	}
	if updated.ActiveTurnID != "turn_999" {
		t.Fatalf("updated.ActiveTurnID = %q, want %q", updated.ActiveTurnID, "turn_999")
	}
	if client.steerTurnReq.TurnID != "turn_456" {
		t.Fatalf("SteerTurn turn_id = %q, want %q", client.steerTurnReq.TurnID, "turn_456")
	}
	if client.startTurnReq.ThreadID != "" {
		t.Fatalf("StartTurn called unexpectedly: %#v", client.startTurnReq)
	}
}

func TestAppServerRunnerSendInteractiveInputStartsNewTurnWithoutActiveTurn(t *testing.T) {
	t.Parallel()

	client := &fakeAppServerRunnerClient{startTurnID: "turn_789"}
	runner := NewAppServerRunner(&fakeAppServerRunnerProxy{}, client)
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return MachineConfig{ID: machineID}, nil }

	updated, err := runner.SendInteractiveInput(context.Background(), RemoteSession{
		ThreadID: "thread_123",
	}, "continue with the fix")
	if err != nil {
		t.Fatalf("SendInteractiveInput returned error: %v", err)
	}
	if updated.ActiveTurnID != "turn_789" {
		t.Fatalf("updated.ActiveTurnID = %q, want %q", updated.ActiveTurnID, "turn_789")
	}
	if client.startTurnReq.ThreadID != "thread_123" {
		t.Fatalf("StartTurn thread_id = %q, want %q", client.startTurnReq.ThreadID, "thread_123")
	}
	if client.steerTurnReq.TurnID != "" {
		t.Fatalf("SteerTurn called unexpectedly: %#v", client.steerTurnReq)
	}
}

func TestAppServerRunnerHasSessionChecksThreadExistence(t *testing.T) {
	t.Parallel()

	client := &fakeAppServerRunnerClient{
		thread:       AppServerThread{ID: "thread_123"},
		getThreadErr: ErrAppServerThreadMissing,
	}
	runner := NewAppServerRunner(&fakeAppServerRunnerProxy{}, client)
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return MachineConfig{ID: machineID}, nil }

	ok, err := runner.HasSession(context.Background(), RemoteSession{ThreadID: "thread_123"})
	if err != nil {
		t.Fatalf("HasSession returned error: %v", err)
	}
	if ok {
		t.Fatal("HasSession returned true, want false")
	}
}

func TestAppServerRunnerStopSessionInterruptsActiveTurn(t *testing.T) {
	t.Parallel()

	client := &fakeAppServerRunnerClient{}
	runner := NewAppServerRunner(&fakeAppServerRunnerProxy{}, client)
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return MachineConfig{ID: machineID}, nil }

	err := runner.StopSession(context.Background(), RemoteSession{
		MachineID:    "machine_a",
		ThreadID:     "thread_123",
		ActiveTurnID: "turn_456",
	})
	if err != nil {
		t.Fatalf("StopSession returned error: %v", err)
	}
	if client.interruptTurnReq.ThreadID != "thread_123" || client.interruptTurnReq.TurnID != "turn_456" {
		t.Fatalf("InterruptTurn request = %#v", client.interruptTurnReq)
	}
}

func fakeThreadItem(t *testing.T, id, itemType string, payload map[string]any) AppServerThreadItem {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal payload: %v", err)
	}
	return AppServerThreadItem{ID: id, Type: itemType, Payload: data}
}

type fakeAppServerRunnerClient struct {
	startThreadReq ThreadStartRequest
	startThreadID  string

	startTurnReq TurnStartRequest
	startTurnID  string

	steerTurnReq     TurnSteerRequest
	steerTurnID      string
	interruptTurnReq TurnInterruptRequest

	thread       AppServerThread
	getThreadErr error
	items        []AppServerThreadItem
}

func (f *fakeAppServerRunnerClient) StartThread(_ context.Context, req ThreadStartRequest) (string, error) {
	f.startThreadReq = req
	return f.startThreadID, nil
}

func (f *fakeAppServerRunnerClient) StartTurn(_ context.Context, req TurnStartRequest) (string, error) {
	f.startTurnReq = req
	return f.startTurnID, nil
}

func (f *fakeAppServerRunnerClient) SteerTurn(_ context.Context, req TurnSteerRequest) (string, error) {
	f.steerTurnReq = req
	return f.steerTurnID, nil
}

func (f *fakeAppServerRunnerClient) InterruptTurn(_ context.Context, req TurnInterruptRequest) error {
	f.interruptTurnReq = req
	return nil
}

func (f *fakeAppServerRunnerClient) GetThread(_ context.Context, req ThreadGetRequest) (AppServerThread, error) {
	if f.getThreadErr != nil {
		return AppServerThread{}, f.getThreadErr
	}
	if f.thread.ID == "" {
		return AppServerThread{ID: req.ThreadID}, nil
	}
	return f.thread, nil
}

func (f *fakeAppServerRunnerClient) ListThreadItems(_ context.Context, _ ThreadItemsListRequest) ([]AppServerThreadItem, error) {
	return f.items, nil
}

type fakeAppServerRunnerProxy struct{ transport AppServerTransport }

func (f *fakeAppServerRunnerProxy) Connect(_ context.Context, _ MachineConfig) (AppServerTransport, error) {
	if f.transport == nil {
		return &fakeAppServerRunnerTransport{}, nil
	}
	return f.transport, nil
}

type fakeAppServerRunnerTransport struct{}

func (f *fakeAppServerRunnerTransport) Send(context.Context, []byte) ([]byte, error) { return nil, nil }
func (f *fakeAppServerRunnerTransport) Recv(context.Context) ([]byte, error)         { return nil, nil }
func (f *fakeAppServerRunnerTransport) Close() error                                 { return nil }

type fakeSSHTransport struct {
	commands []string
}

func (f *fakeSSHTransport) Run(_ context.Context, _ MachineConfig, command string, _ string) (string, error) {
	f.commands = append(f.commands, command)
	return "", nil
}
