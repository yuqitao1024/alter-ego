package orchestrator

import (
	"context"
	"errors"
	"strings"
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
	if runtime.startRequest.SandboxPolicy.Type != "dangerFullAccess" {
		t.Fatalf("startRequest.SandboxPolicy.Type = %q, want dangerFullAccess", runtime.startRequest.SandboxPolicy.Type)
	}
	if !runtime.startRequest.SandboxPolicy.NetworkAccess {
		t.Fatal("startRequest.SandboxPolicy.NetworkAccess = false, want true")
	}
	if len(runtime.startRequest.SandboxPolicy.WritableRoots) != 0 {
		t.Fatalf("startRequest.SandboxPolicy.WritableRoots = %#v, want empty", runtime.startRequest.SandboxPolicy.WritableRoots)
	}
}

func TestAppServerRunnerStartInteractiveSessionUsesTaskRootForEmptyWorkspace(t *testing.T) {
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
		TaskID:              "task-empty",
		RemoteWorkspaceRoot: "/srv/codex-tasks",
		WorkspaceSetup: WorkspaceSetup{
			Type: WorkspaceSetupTypeEmpty,
		},
		UserRequest:     "Continue implementation",
		WorkflowContent: "workflow",
	})
	if err != nil {
		t.Fatalf("StartInteractiveSession returned error: %v", err)
	}
	if session.Workdir != "/srv/codex-tasks/task-empty" {
		t.Fatalf("session.Workdir = %q, want /srv/codex-tasks/task-empty", session.Workdir)
	}
	if runtime.startRequest.Cwd != "/srv/codex-tasks/task-empty" {
		t.Fatalf("startRequest.Cwd = %q, want /srv/codex-tasks/task-empty", runtime.startRequest.Cwd)
	}
}

func TestAppServerRunnerStartInteractiveSessionIncludesCodeReviewContext(t *testing.T) {
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

	_, err := runner.StartInteractiveSession(context.Background(), StartRequest{
		Machine: MachineConfig{
			ID:                   "machine_a",
			Host:                 "machine-a.example.com",
			User:                 "coder",
			AppServerListenHost:  "0.0.0.0",
			AppServerListenPort:  4317,
			AppServerServiceName: "codex-app-server",
			AppServerInstallUser: "coder",
		},
		TaskID:              "task-review",
		TaskType:            TaskTypeCodeReview,
		RemoteWorkspaceRoot: "/srv/codex-tasks",
		WorkspaceSetup: WorkspaceSetup{
			Type:           WorkspaceSetupTypeRepo,
			RemoteRepoURL:  "git@github.com:example/backend.git",
			CheckoutBranch: "main",
		},
		CodeReview: &CodeReviewConfig{
			GitCodeProject: "example/backend",
			PRSelector:     "latest_open",
			ReviewTool:     "codex_builtin",
			HumanizerSkill: "humanizer:humanizer",
			Approval:       "lark",
			Publisher:      "gitcode",
		},
		UserRequest:     "Review the latest PR",
		WorkflowContent: "review workflow",
	})
	if err != nil {
		t.Fatalf("StartInteractiveSession returned error: %v", err)
	}
	for _, want := range []string{
		"[Task Type]\ncode_review",
		"[Code Review Config]",
		"gitcode_project: example/backend",
		"publisher: gitcode",
		"[User Request]\nReview the latest PR",
	} {
		if !strings.Contains(runtime.startRequest.Input, want) {
			t.Fatalf("startRequest.Input = %q, want substring %q", runtime.startRequest.Input, want)
		}
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
	if runtime.sendRequest.ThreadID != "thread-1" {
		t.Fatalf("sendRequest.ThreadID = %q, want thread-1", runtime.sendRequest.ThreadID)
	}
	if runtime.sendRequest.Cwd != "" {
		t.Fatalf("sendRequest.Cwd = %q, want empty when steering existing turn", runtime.sendRequest.Cwd)
	}
	if runtime.sendRequest.ApprovalPolicy != "never" {
		t.Fatalf("sendRequest.ApprovalPolicy = %q, want never", runtime.sendRequest.ApprovalPolicy)
	}
	if runtime.sendRequest.SandboxPolicy.Type != "dangerFullAccess" {
		t.Fatalf("sendRequest.SandboxPolicy.Type = %q, want dangerFullAccess", runtime.sendRequest.SandboxPolicy.Type)
	}
	if !runtime.sendRequest.SandboxPolicy.NetworkAccess {
		t.Fatal("sendRequest.SandboxPolicy.NetworkAccess = false, want true")
	}
	if len(runtime.sendRequest.SandboxPolicy.WritableRoots) != 0 {
		t.Fatalf("sendRequest.SandboxPolicy.WritableRoots = %#v, want empty", runtime.sendRequest.SandboxPolicy.WritableRoots)
	}
}

func TestAppServerRunnerSendInteractiveInputStartsTurnWithSessionContext(t *testing.T) {
	t.Parallel()

	runtime := &fakeCodexRuntime{
		steerErr: errors.New("turn/steer: no active turn to steer"),
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
		ActiveTurnID: "turn-old",
		Workdir:      "/srv/backend",
	}, "continue with the fix")
	if err != nil {
		t.Fatalf("SendInteractiveInput returned error: %v", err)
	}
	if updated.ActiveTurnID != "turn-new" {
		t.Fatalf("updated.ActiveTurnID = %q, want turn-new", updated.ActiveTurnID)
	}
	if runtime.sendRequest.Cwd != "/srv/backend" {
		t.Fatalf("sendRequest.Cwd = %q, want /srv/backend", runtime.sendRequest.Cwd)
	}
	if runtime.sendRequest.ApprovalPolicy != "never" {
		t.Fatalf("sendRequest.ApprovalPolicy = %q, want never", runtime.sendRequest.ApprovalPolicy)
	}
	if runtime.sendRequest.SandboxPolicy.Type != "dangerFullAccess" {
		t.Fatalf("sendRequest.SandboxPolicy.Type = %q, want dangerFullAccess", runtime.sendRequest.SandboxPolicy.Type)
	}
	if len(runtime.sendRequest.SandboxPolicy.WritableRoots) != 0 {
		t.Fatalf("sendRequest.SandboxPolicy.WritableRoots = %#v, want empty", runtime.sendRequest.SandboxPolicy.WritableRoots)
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
	steerErr            error
	sendRequest         codexappserver.SendTaskInputRequest
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

func (f *fakeCodexRuntime) SendTaskInput(_ context.Context, _ codexappserver.MachineRuntimeConfig, req codexappserver.SendTaskInputRequest) (string, error) {
	f.sendRequest = req
	if req.ActiveTurnID != "" {
		if f.steerErr != nil {
			if strings.Contains(strings.ToLower(f.steerErr.Error()), "no active turn") {
				f.sendRequest.ActiveTurnID = ""
				return "turn-new", nil
			}
			return "", f.steerErr
		}
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
