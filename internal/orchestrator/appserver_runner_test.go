package orchestrator

import (
	"context"
	"errors"
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
	if runtime.startRequest.Cwd != "/srv/codex-tasks/task-1/repo" {
		t.Fatalf("startRequest.Cwd = %q", runtime.startRequest.Cwd)
	}
	if runtime.startRequest.ApprovalPolicy != "never" {
		t.Fatalf("startRequest.ApprovalPolicy = %q, want never", runtime.startRequest.ApprovalPolicy)
	}
	if runtime.startRequest.SandboxPolicy.Type != "workspace-write" {
		t.Fatalf("startRequest.SandboxPolicy.Type = %q, want workspace-write", runtime.startRequest.SandboxPolicy.Type)
	}
	if !runtime.startRequest.SandboxPolicy.NetworkAccess {
		t.Fatal("startRequest.SandboxPolicy.NetworkAccess = false, want true")
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
		resumeWatchThreadID: "thread-1",
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
	if runtime.resumeWatchThreadID != "thread-1" {
		t.Fatalf("resumeWatchThreadID = %q, want thread-1", runtime.resumeWatchThreadID)
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

func TestAppServerRunnerStopSessionTreatsNoActiveTurnAsStopped(t *testing.T) {
	t.Parallel()

	runtime := &fakeCodexRuntime{interruptErr: errors.New("turn/interrupt: no active turn to interrupt")}
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
}

func TestAppServerRunnerDeleteTaskWorkspaceRemovesTaskRoot(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{}
	runner := NewAppServerRunner(&fakeCodexRuntime{})
	runner.transport = transport

	err := runner.DeleteTaskWorkspace(context.Background(), DeleteWorkspaceRequest{
		Machine: MachineConfig{
			ID:        "machine_a",
			Host:      "machine-a.example.com",
			User:      "coder",
			ShellInit: []string{"source /opt/env.sh"},
		},
		TaskID:              "task-1",
		RemoteWorkspaceRoot: "/srv/codex-tasks",
	})
	if err != nil {
		t.Fatalf("DeleteTaskWorkspace returned error: %v", err)
	}
	if len(transport.commands) != 1 {
		t.Fatalf("commands = %#v, want one command", transport.commands)
	}
	want := "source /opt/env.sh && rm -rf -- '/srv/codex-tasks/task-1'"
	if transport.commands[0] != want {
		t.Fatalf("command = %q, want %q", transport.commands[0], want)
	}
}

func TestAppServerRunnerCleanupSessionCleansAppServerThread(t *testing.T) {
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

	err := runner.CleanupSession(context.Background(), RemoteSession{
		MachineID: "machine_a",
		ThreadID:  "thread-1",
	})
	if err != nil {
		t.Fatalf("CleanupSession returned error: %v", err)
	}
	if runtime.cleanupThreadID != "thread-1" {
		t.Fatalf("cleanupThreadID = %q, want thread-1", runtime.cleanupThreadID)
	}
}

func TestAppServerRunnerCleanupSessionTreatsMissingThreadAsClean(t *testing.T) {
	t.Parallel()

	runtime := &fakeCodexRuntime{cleanupErr: errors.New("thread/archive: thread not found")}
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

	err := runner.CleanupSession(context.Background(), RemoteSession{
		MachineID: "machine_a",
		ThreadID:  "thread-1",
	})
	if err != nil {
		t.Fatalf("CleanupSession returned error: %v", err)
	}
}

type fakeCodexRuntime struct {
	startThreadID       string
	startTurnID         string
	startRequest        codexappserver.StartTaskSessionRequest
	steerTurnID         string
	watchThreadID       string
	resumeWatchThreadID string
	requestID           string
	requestResult       any

	interruptThreadID string
	interruptTurnID   string
	interruptErr      error
	cleanupThreadID   string
	cleanupErr        error

	snapshots map[string]codexappserver.ThreadSnapshot
}

func (f *fakeCodexRuntime) StartTaskSession(_ context.Context, _ codexappserver.MachineRuntimeConfig, req codexappserver.StartTaskSessionRequest) (string, string, error) {
	f.startRequest = req
	return f.startThreadID, f.startTurnID, nil
}

func (f *fakeCodexRuntime) WatchTaskThread(_ context.Context, _ codexappserver.MachineRuntimeConfig, threadID string) (*codexappserver.ThreadWatcher, error) {
	f.watchThreadID = threadID
	return nil, nil
}

func (f *fakeCodexRuntime) ResumeTaskThread(_ context.Context, _ codexappserver.MachineRuntimeConfig, threadID string) (*codexappserver.ThreadWatcher, error) {
	f.resumeWatchThreadID = threadID
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
	return f.interruptErr
}

func (f *fakeCodexRuntime) CleanupTaskThread(_ context.Context, _ codexappserver.MachineRuntimeConfig, threadID string) error {
	f.cleanupThreadID = threadID
	return f.cleanupErr
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
