package orchestrator

import (
	"context"
	"strings"
	"testing"
)

func TestReconnectUsesTMUXSessionWhenPresent(t *testing.T) {
	t.Parallel()

	runner := &fakeRemoteRunner{hasSession: true}
	task := TaskRun{
		TaskID:               "task-1",
		MachineID:            "machine_a",
		RemoteWorkdir:        "/srv/repo",
		TMUXSessionName:      "alterego-task-1",
		RemoteCodexSessionID: "session-1",
		LastScreenDigest:     "digest-1",
	}

	session, err := ReconnectInteractiveSession(context.Background(), runner, task)
	if err != nil {
		t.Fatalf("ReconnectInteractiveSession returned error: %v", err)
	}

	if session.TMUXSessionName != "alterego-task-1" {
		t.Fatalf("session.TMUXSessionName = %q, want alterego-task-1", session.TMUXSessionName)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "has-session" {
		t.Fatalf("calls = %v, want [has-session]", runner.calls)
	}
}

func TestReconnectUsesThreadWhenPresent(t *testing.T) {
	t.Parallel()

	runner := &fakeRemoteRunner{hasSession: true}
	task := TaskRun{
		TaskID:        "task-1",
		MachineID:     "machine_a",
		RemoteWorkdir: "/srv/repo",
		AppServerState: AppServerState{
			ThreadID:     "thread_123",
			ActiveTurnID: "turn_456",
		},
	}

	session, err := ReconnectInteractiveSession(context.Background(), runner, task)
	if err != nil {
		t.Fatalf("ReconnectInteractiveSession returned error: %v", err)
	}

	if session.ThreadID != "thread_123" {
		t.Fatalf("session.ThreadID = %q, want %q", session.ThreadID, "thread_123")
	}
	if session.ActiveTurnID != "turn_456" {
		t.Fatalf("session.ActiveTurnID = %q, want %q", session.ActiveTurnID, "turn_456")
	}
	if len(runner.calls) != 1 || runner.calls[0] != "has-session" {
		t.Fatalf("calls = %v, want [has-session]", runner.calls)
	}
}

func TestReconnectFailsWhenTMUXSessionMissing(t *testing.T) {
	t.Parallel()

	runner := &fakeRemoteRunner{hasSession: false}
	task := TaskRun{
		TaskID:               "task-2",
		MachineID:            "machine_a",
		RemoteWorkdir:        "/srv/repo",
		TMUXSessionName:      "alterego-task-2",
		RemoteCodexSessionID: "session-2",
	}

	_, err := ReconnectInteractiveSession(context.Background(), runner, task)
	if err == nil {
		t.Fatal("ReconnectInteractiveSession returned nil error, want failure")
	}
	if !strings.Contains(err.Error(), "tmux session") {
		t.Fatalf("ReconnectInteractiveSession error = %v, want tmux session failure", err)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "has-session" {
		t.Fatalf("calls = %v, want [has-session]", runner.calls)
	}
}

type fakeRemoteRunner struct {
	calls []string

	startSession RemoteSession
	outputWindow OutputWindow
	hasSession   bool
}

func (f *fakeRemoteRunner) StartInteractiveSession(context.Context, StartRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "start")
	return f.startSession, nil
}

func (f *fakeRemoteRunner) CaptureOutput(context.Context, RemoteSession) (OutputWindow, error) {
	f.calls = append(f.calls, "capture")
	return f.outputWindow, nil
}

func (f *fakeRemoteRunner) SendInteractiveInput(_ context.Context, session RemoteSession, _ string) (RemoteSession, error) {
	f.calls = append(f.calls, "send")
	return session, nil
}

func (f *fakeRemoteRunner) SendInteractiveKey(context.Context, RemoteSession, string) error {
	f.calls = append(f.calls, "send-key")
	return nil
}

func (f *fakeRemoteRunner) HasSession(context.Context, RemoteSession) (bool, error) {
	f.calls = append(f.calls, "has-session")
	return f.hasSession, nil
}

func (f *fakeRemoteRunner) ResumeLastCodexSession(context.Context, RemoteSession) error {
	f.calls = append(f.calls, "resume")
	return nil
}

func (f *fakeRemoteRunner) StopSession(context.Context, RemoteSession) error {
	f.calls = append(f.calls, "stop")
	return nil
}
