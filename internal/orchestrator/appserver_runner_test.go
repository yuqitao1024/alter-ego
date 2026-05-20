package orchestrator

import (
	"context"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/codexappserver"
)

func TestAppServerRunnerCaptureOutputReadsSnapshotInsteadOfPolling(t *testing.T) {
	t.Parallel()

	runtime := &fakeCodexRuntime{
		snapshots: map[string]codexappserver.ThreadSnapshot{
			"machine_a/thread-1": {
				ThreadID:           "thread-1",
				ThreadStatus:       "running",
				ActiveTurnID:       "turn-1",
				LatestAgentMessage: "Applied migration and running tests",
				LatestSummary:      "Applied migration and running tests",
			},
		},
	}

	runner := NewAppServerRunner(runtime)
	window, err := runner.CaptureOutput(context.Background(), RemoteSession{
		MachineID: "machine_a",
		ThreadID:  "thread-1",
	})
	if err != nil {
		t.Fatalf("CaptureOutput returned error: %v", err)
	}
	if window.Summary != "Applied migration and running tests" {
		t.Fatalf("window.Summary = %q", window.Summary)
	}
	if window.SessionState.ThreadStatus != "running" {
		t.Fatalf("window.SessionState.ThreadStatus = %q", window.SessionState.ThreadStatus)
	}
}

func TestAppServerRunnerStartInteractiveSessionStartsWatcher(t *testing.T) {
	t.Parallel()

	runtime := &fakeCodexRuntime{
		startThreadID: "thread-1",
		startTurnID:   "turn-1",
	}
	runner := NewAppServerRunner(runtime)
	runner.transport = &fakeSSHTransport{}
	runner.machineResolver = func(machineID string) (MachineConfig, error) {
		return MachineConfig{
			ID:                   machineID,
			Host:                 "machine-a.example.com",
			User:                 "coder",
			AppServerListenHost:  "0.0.0.0",
			AppServerListenPort:  4317,
			AppServerServiceName: "codex-app-server",
			AppServerInstallUser: "coder",
		}, nil
	}

	session, err := runner.StartInteractiveSession(context.Background(), StartRequest{
		Machine: MachineConfig{
			ID:                   "machine_a",
			Host:                 "machine-a.example.com",
			User:                 "coder",
			AppServerListenHost:  "0.0.0.0",
			AppServerListenPort:  4317,
			AppServerServiceName: "codex-app-server",
			AppServerInstallUser: "coder",
		},
		RepositoryID:        "repo_backend",
		TaskID:              "task-1",
		RemoteRepoURL:       "git@github.com:example/backend.git",
		RemoteWorkspaceRoot: "/srv/codex-tasks",
		CheckoutBranch:      "main",
		UserRequest:         "Continue implementation",
		WorkflowContent:     "workflow",
	})
	if err != nil {
		t.Fatalf("StartInteractiveSession returned error: %v", err)
	}
	if session.ThreadID != "thread-1" || session.ActiveTurnID != "turn-1" {
		t.Fatalf("session = %#v", session)
	}
	if runtime.watchThreadID != "thread-1" {
		t.Fatalf("watchThreadID = %q, want thread-1", runtime.watchThreadID)
	}
}

func TestAppServerRunnerSendInteractiveInputSteersActiveTurn(t *testing.T) {
	t.Parallel()

	runtime := &fakeCodexRuntime{
		steerTurnID: "turn-999",
	}
	runner := NewAppServerRunner(runtime)
	runner.machineResolver = func(machineID string) (MachineConfig, error) {
		return MachineConfig{
			ID:                   machineID,
			Host:                 "machine-a.example.com",
			User:                 "coder",
			AppServerListenHost:  "0.0.0.0",
			AppServerListenPort:  4317,
			AppServerServiceName: "codex-app-server",
			AppServerInstallUser: "coder",
		}, nil
	}

	updated, err := runner.SendInteractiveInput(context.Background(), RemoteSession{
		MachineID:    "machine_a",
		ThreadID:     "thread-1",
		ActiveTurnID: "turn-1",
	}, "continue with the fix")
	if err != nil {
		t.Fatalf("SendInteractiveInput returned error: %v", err)
	}
	if updated.ActiveTurnID != "turn-999" {
		t.Fatalf("updated.ActiveTurnID = %q, want turn-999", updated.ActiveTurnID)
	}
}

func TestAppServerRunnerHasSessionChecksSnapshotPresence(t *testing.T) {
	t.Parallel()

	runtime := &fakeCodexRuntime{
		snapshots: map[string]codexappserver.ThreadSnapshot{
			"machine_a/thread-1": {ThreadID: "thread-1"},
		},
	}
	runner := NewAppServerRunner(runtime)

	ok, err := runner.HasSession(context.Background(), RemoteSession{
		MachineID: "machine_a",
		ThreadID:  "thread-1",
	})
	if err != nil {
		t.Fatalf("HasSession returned error: %v", err)
	}
	if !ok {
		t.Fatal("HasSession returned false, want true")
	}
}

func TestAppServerRunnerStopSessionInterruptsActiveTurn(t *testing.T) {
	t.Parallel()

	runtime := &fakeCodexRuntime{}
	runner := NewAppServerRunner(runtime)
	runner.machineResolver = func(machineID string) (MachineConfig, error) {
		return MachineConfig{
			ID:                   machineID,
			Host:                 "machine-a.example.com",
			User:                 "coder",
			AppServerListenHost:  "0.0.0.0",
			AppServerListenPort:  4317,
			AppServerServiceName: "codex-app-server",
			AppServerInstallUser: "coder",
		}, nil
	}

	err := runner.StopSession(context.Background(), RemoteSession{
		MachineID:    "machine_a",
		ThreadID:     "thread-1",
		ActiveTurnID: "turn-1",
	})
	if err != nil {
		t.Fatalf("StopSession returned error: %v", err)
	}
	if runtime.interruptThreadID != "thread-1" || runtime.interruptTurnID != "turn-1" {
		t.Fatalf("interrupt = %s/%s", runtime.interruptThreadID, runtime.interruptTurnID)
	}
}

type fakeCodexRuntime struct {
	startThreadID string
	startTurnID   string
	steerTurnID   string
	watchThreadID string
	requestID     string
	requestResult any

	interruptThreadID string
	interruptTurnID   string

	snapshots map[string]codexappserver.ThreadSnapshot
}

func (f *fakeCodexRuntime) StartTaskSession(_ context.Context, _ codexappserver.MachineRuntimeConfig, _ codexappserver.StartTaskSessionRequest) (string, string, error) {
	return f.startThreadID, f.startTurnID, nil
}

func (f *fakeCodexRuntime) WatchTaskThread(_ context.Context, _ codexappserver.MachineRuntimeConfig, threadID string) (*codexappserver.ThreadWatcher, error) {
	f.watchThreadID = threadID
	return nil, nil
}

func (f *fakeCodexRuntime) SendTaskInput(_ context.Context, _ codexappserver.MachineRuntimeConfig, _, activeTurnID, _ string) (string, error) {
	if activeTurnID != "" {
		return f.steerTurnID, nil
	}
	return "turn-new", nil
}

func (f *fakeCodexRuntime) RespondToServerRequest(_ context.Context, _ codexappserver.MachineRuntimeConfig, requestID string, result any) error {
	f.requestID = requestID
	f.requestResult = result
	return nil
}

func (f *fakeCodexRuntime) InterruptTask(_ context.Context, _ codexappserver.MachineRuntimeConfig, threadID, activeTurnID string) error {
	f.interruptThreadID = threadID
	f.interruptTurnID = activeTurnID
	return nil
}

func (f *fakeCodexRuntime) Snapshot(machineID, threadID string) (codexappserver.ThreadSnapshot, bool) {
	snapshot, ok := f.snapshots[machineID+"/"+threadID]
	return snapshot, ok
}

type fakeSSHTransport struct {
	commands []string
}

func (f *fakeSSHTransport) Run(_ context.Context, _ MachineConfig, command string, _ string) (string, error) {
	f.commands = append(f.commands, command)
	return "", nil
}
