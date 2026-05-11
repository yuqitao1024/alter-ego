package orchestrator

import (
	"context"
	"reflect"
	"testing"
)

func TestRecoverPrefersAttachWhenRemoteProcessIsAlive(t *testing.T) {
	t.Parallel()

	runner := &fakeRemoteRunner{
		probeResult: ProbeResult{
			Alive:           true,
			ProcessIdentity: "pid-123",
		},
		attachSession: RemoteSession{
			MachineID:        "machine_a",
			Workdir:          "/srv/repo",
			CodexSessionID:   "session-1",
			ProcessIdentity:  "pid-123",
			AttachedToLive:   true,
			LastOutputWindow: OutputWindow{Summary: "attached"},
		},
	}
	task := TaskRun{
		TaskID:                "task-1",
		MachineID:             "machine_a",
		RemoteWorkdir:         "/srv/repo",
		RemoteCodexSessionID:  "session-1",
		RemoteProcessIdentity: "pid-previous",
	}
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder"}

	session, err := RecoverRemoteSession(context.Background(), runner, machine, task)
	if err != nil {
		t.Fatalf("RecoverRemoteSession returned error: %v", err)
	}

	if session.CodexSessionID != "session-1" {
		t.Fatalf("session.CodexSessionID = %q, want session-1", session.CodexSessionID)
	}

	wantCalls := []string{"probe", "attach"}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestRecoverUsesResumeWhenRemoteProcessIsGone(t *testing.T) {
	t.Parallel()

	runner := &fakeRemoteRunner{
		probeResult: ProbeResult{Alive: false},
		resumeSession: RemoteSession{
			MachineID:       "machine_a",
			Workdir:         "/srv/repo",
			CodexSessionID:  "session-2",
			ProcessIdentity: "pid-456",
		},
	}
	task := TaskRun{
		TaskID:                "task-2",
		MachineID:             "machine_a",
		RemoteWorkdir:         "/srv/repo",
		RemoteCodexSessionID:  "session-2",
		RemoteProcessIdentity: "pid-old",
	}
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder"}

	session, err := RecoverRemoteSession(context.Background(), runner, machine, task)
	if err != nil {
		t.Fatalf("RecoverRemoteSession returned error: %v", err)
	}

	if session.ProcessIdentity != "pid-456" {
		t.Fatalf("session.ProcessIdentity = %q, want pid-456", session.ProcessIdentity)
	}

	wantCalls := []string{"probe", "resume"}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestRecoverNeverStartsDuplicateSessionBeforeProbe(t *testing.T) {
	t.Parallel()

	runner := &fakeRemoteRunner{
		probeResult: ProbeResult{Alive: false},
		resumeSession: RemoteSession{
			MachineID:       "machine_b",
			Workdir:         "/srv/repo",
			CodexSessionID:  "session-3",
			ProcessIdentity: "pid-789",
		},
	}
	task := TaskRun{
		TaskID:               "task-3",
		MachineID:            "machine_b",
		RemoteWorkdir:        "/srv/repo",
		RemoteCodexSessionID: "session-3",
	}
	machine := MachineConfig{ID: "machine_b", Host: "host-b", User: "coder"}

	if _, err := RecoverRemoteSession(context.Background(), runner, machine, task); err != nil {
		t.Fatalf("RecoverRemoteSession returned error: %v", err)
	}

	for _, call := range runner.calls {
		if call == "start" {
			t.Fatalf("calls = %v, unexpected start call", runner.calls)
		}
	}
}

type fakeRemoteRunner struct {
	calls []string

	probeResult   ProbeResult
	attachSession RemoteSession
	resumeSession RemoteSession
	startSession  RemoteSession
	outputWindow  OutputWindow
}

func (f *fakeRemoteRunner) StartNewSession(context.Context, StartRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "start")
	return f.startSession, nil
}

func (f *fakeRemoteRunner) ProbeSession(context.Context, ProbeRequest) (ProbeResult, error) {
	f.calls = append(f.calls, "probe")
	return f.probeResult, nil
}

func (f *fakeRemoteRunner) AttachLiveSession(context.Context, AttachRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "attach")
	return f.attachSession, nil
}

func (f *fakeRemoteRunner) ResumeExitedSession(context.Context, ResumeRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "resume")
	return f.resumeSession, nil
}

func (f *fakeRemoteRunner) SendInput(context.Context, RemoteSession, string) error {
	f.calls = append(f.calls, "send")
	return nil
}

func (f *fakeRemoteRunner) ReadWindow(context.Context, RemoteSession) (OutputWindow, error) {
	f.calls = append(f.calls, "read")
	return f.outputWindow, nil
}

func (f *fakeRemoteRunner) StopTask(context.Context, RemoteSession) error {
	f.calls = append(f.calls, "stop")
	return nil
}
