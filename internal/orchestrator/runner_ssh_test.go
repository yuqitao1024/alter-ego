package orchestrator

import (
	"context"
	"strings"
	"testing"
)

func TestSSHRunnerStartCommandShape(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{
		stdout: "session_id=session-start\nprocess_identity=pid-100\nworkdir=/srv/backend\n",
	}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder", Port: 2222}

	session, err := runner.StartNewSession(context.Background(), StartRequest{
		Machine:         machine,
		RepositoryID:    "repo_backend",
		Workdir:         "/srv/backend",
		UserRequest:     "Implement scheduler",
		WorkflowContent: "Workflow: inspect first",
	})
	if err != nil {
		t.Fatalf("StartNewSession returned error: %v", err)
	}

	if session.CodexSessionID != "session-start" {
		t.Fatalf("session.CodexSessionID = %q, want session-start", session.CodexSessionID)
	}
	if !strings.Contains(transport.lastCommand, "cd '/srv/backend'") {
		t.Fatalf("command = %q, want cd into workdir", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "codex exec --json -") {
		t.Fatalf("command = %q, want codex exec --json -", transport.lastCommand)
	}
	if !strings.Contains(transport.lastStdin, "Workflow: inspect first") || !strings.Contains(transport.lastStdin, "Implement scheduler") {
		t.Fatalf("stdin = %q, want workflow and user request", transport.lastStdin)
	}
}

func TestSSHRunnerProbeCommandShape(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{stdout: "alive pid-123\n"}
	runner := NewSSHRunner(transport)

	result, err := runner.ProbeSession(context.Background(), ProbeRequest{
		Machine:         MachineConfig{ID: "machine_a", Host: "host-a", User: "coder"},
		Workdir:         "/srv/backend",
		CodexSessionID:  "session-1",
		ProcessIdentity: "pid-123",
	})
	if err != nil {
		t.Fatalf("ProbeSession returned error: %v", err)
	}

	if !result.Alive || result.ProcessIdentity != "pid-123" {
		t.Fatalf("result = %#v, want alive pid-123", result)
	}
	if !strings.Contains(transport.lastCommand, "kill -0 'pid-123'") {
		t.Fatalf("command = %q, want kill -0 probe", transport.lastCommand)
	}
}

func TestSSHRunnerAttachAndResumeCommandShape(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{
		stdoutByCall: []string{
			"session_id=session-attach\nprocess_identity=pid-live\nworkdir=/srv/backend\n",
			"session_id=session-resume\nprocess_identity=pid-new\nworkdir=/srv/backend\n",
		},
	}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder"}

	attached, err := runner.AttachLiveSession(context.Background(), AttachRequest{
		Machine:         machine,
		Workdir:         "/srv/backend",
		CodexSessionID:  "session-attach",
		ProcessIdentity: "pid-live",
	})
	if err != nil {
		t.Fatalf("AttachLiveSession returned error: %v", err)
	}
	if !attached.AttachedToLive {
		t.Fatalf("attached.AttachedToLive = false, want true")
	}

	attachCommand := transport.commands[0]
	if !strings.Contains(attachCommand, "codex exec resume 'session-attach' --json") {
		t.Fatalf("attach command = %q", attachCommand)
	}

	resumed, err := runner.ResumeExitedSession(context.Background(), ResumeRequest{
		Machine:        machine,
		Workdir:        "/srv/backend",
		CodexSessionID: "session-resume",
	})
	if err != nil {
		t.Fatalf("ResumeExitedSession returned error: %v", err)
	}
	if resumed.AttachedToLive {
		t.Fatalf("resumed.AttachedToLive = true, want false")
	}

	resumeCommand := transport.commands[1]
	if !strings.Contains(resumeCommand, "codex exec resume 'session-resume' --json") {
		t.Fatalf("resume command = %q", resumeCommand)
	}
}

func TestSSHRunnerStopAndSendCommands(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{}
	runner := NewSSHRunner(transport)
	session := RemoteSession{
		MachineID:       "machine_a",
		Workdir:         "/srv/backend",
		CodexSessionID:  "session-1",
		ProcessIdentity: "pid-123",
	}
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder"}
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return machine, nil }

	if err := runner.SendInput(context.Background(), session, "Continue and run tests."); err != nil {
		t.Fatalf("SendInput returned error: %v", err)
	}
	if !strings.Contains(transport.commands[0], "codex exec resume 'session-1' --json -") {
		t.Fatalf("send command = %q", transport.commands[0])
	}
	if !strings.Contains(transport.stdins[0], "Continue and run tests.") {
		t.Fatalf("send stdin = %q", transport.stdins[0])
	}

	if err := runner.StopTask(context.Background(), session); err != nil {
		t.Fatalf("StopTask returned error: %v", err)
	}
	if !strings.Contains(transport.commands[1], "kill 'pid-123'") {
		t.Fatalf("stop command = %q", transport.commands[1])
	}
}

type fakeSSHTransport struct {
	stdout       string
	stdoutByCall []string
	callCount    int

	lastCommand string
	lastStdin   string
	commands    []string
	stdins      []string
}

func (f *fakeSSHTransport) Run(_ context.Context, _ MachineConfig, command string, stdin string) (string, error) {
	f.callCount++
	f.lastCommand = command
	f.lastStdin = stdin
	f.commands = append(f.commands, command)
	f.stdins = append(f.stdins, stdin)

	if len(f.stdoutByCall) >= f.callCount {
		return f.stdoutByCall[f.callCount-1], nil
	}
	return f.stdout, nil
}
