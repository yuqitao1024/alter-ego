package orchestrator

import (
	"context"
	"strings"
	"testing"
)

func TestSSHRunnerStartCreatesSessionAndLaunchesCodex(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{
		stdout: "tmux_session_name=alterego-task-1\nworkdir=/srv/codex-tasks/task-1/repo\n",
	}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{
		ID: "machine_a", Host: "host-a", User: "coder", Port: 2222,
		ShellInit: []string{"source /opt/codex/env.sh", "export CODEX_HOME=/opt/codex-home"},
	}

	session, err := runner.StartInteractiveSession(context.Background(), StartRequest{
		Machine:             machine,
		RepositoryID:        "repo_backend",
		TaskID:              "task-1",
		RemoteRepoURL:       "git@github.com:example/backend.git",
		RemoteWorkspaceRoot: "/srv/codex-tasks",
		CheckoutBranch:      "main",
		PreCloneBootstrap: []string{
			"setup-git-auth",
			"prepare-network",
		},
		PostCloneBootstrap: []string{
			"git submodule update --init --recursive",
			"pnpm install",
		},
		UserRequest:     "Implement scheduler",
		WorkflowContent: "Workflow: inspect first",
	})
	if err != nil {
		t.Fatalf("StartInteractiveSession returned error: %v", err)
	}

	if session.TMUXSessionName != "alterego-task-1" {
		t.Fatalf("session.TMUXSessionName = %q, want alterego-task-1", session.TMUXSessionName)
	}
	if session.Workdir != "/srv/codex-tasks/task-1/repo" {
		t.Fatalf("session.Workdir = %q, want /srv/codex-tasks/task-1/repo", session.Workdir)
	}
	if !strings.Contains(transport.lastCommand, "mkdir -p '/srv/codex-tasks/task-1'") {
		t.Fatalf("command = %q, want task directory creation", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "git clone 'git@github.com:example/backend.git' repo") {
		t.Fatalf("command = %q, want git clone into repo subdirectory", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "setup-git-auth") || !strings.Contains(transport.lastCommand, "prepare-network") {
		t.Fatalf("command = %q, want pre-clone bootstrap commands", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "git submodule update --init --recursive") || !strings.Contains(transport.lastCommand, "pnpm install") {
		t.Fatalf("command = %q, want post-clone bootstrap commands", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "tmux new-session -d -s 'alterego-task-1'") {
		t.Fatalf("command = %q, want tmux new-session", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "source /opt/codex/env.sh") {
		t.Fatalf("command = %q, want shell init on outer ssh command", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "export CODEX_HOME=/opt/codex-home") {
		t.Fatalf("command = %q, want CODEX_HOME init", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "codex --dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("command = %q, want codex launch with bypass flag", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "tmux new-session -d -s 'alterego-task-1' 'source /opt/codex/env.sh && export CODEX_HOME=/opt/codex-home") {
		t.Fatalf("command = %q, want shell init inside tmux codex command", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "codex --dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("command = %q, want codex launch inside tmux command", transport.lastCommand)
	}
	if strings.TrimSpace(transport.lastStdin) != "" {
		t.Fatalf("stdin = %q, want empty stdin for tmux startup", transport.lastStdin)
	}
}

func TestSSHRunnerCaptureUsesCapturePane(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{
		stdoutByCall: []string{
			"node|0|0\n",
			"recent output",
		},
	}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder", ShellInit: []string{"source /opt/codex/env.sh"}}
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return machine, nil }

	window, err := runner.CaptureOutput(context.Background(), RemoteSession{
		MachineID:       "machine_a",
		TMUXSessionName: "alterego-task-2",
	})
	if err != nil {
		t.Fatalf("CaptureOutput returned error: %v", err)
	}
	if window.Summary != "recent output" {
		t.Fatalf("window.Summary = %q, want recent output", window.Summary)
	}
	if !strings.Contains(transport.lastCommand, "tmux capture-pane -p -t 'alterego-task-2'") {
		t.Fatalf("command = %q, want capture-pane", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "-S -80 -E -") {
		t.Fatalf("command = %q, want tail-window capture flags", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "source /opt/codex/env.sh && tmux capture-pane") {
		t.Fatalf("command = %q, want shell init before capture-pane", transport.lastCommand)
	}
}

func TestSSHRunnerSendInputUsesSendKeys(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder", ShellInit: []string{"source /opt/codex/env.sh"}}
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return machine, nil }

	err := runner.SendInteractiveInput(context.Background(), RemoteSession{
		MachineID:       "machine_a",
		TMUXSessionName: "alterego-task-3",
	}, "Continue and run tests.")
	if err != nil {
		t.Fatalf("SendInteractiveInput returned error: %v", err)
	}
	if !strings.Contains(transport.lastCommand, "tmux send-keys -t 'alterego-task-3' -- 'Continue and run tests.' Enter") {
		t.Fatalf("command = %q, want send-keys", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "source /opt/codex/env.sh && tmux send-keys") {
		t.Fatalf("command = %q, want shell init before send-keys", transport.lastCommand)
	}
}

func TestSSHRunnerSendKeyUsesSendKeysWithoutEnter(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder", ShellInit: []string{"source /opt/codex/env.sh"}}
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return machine, nil }

	err := runner.SendInteractiveKey(context.Background(), RemoteSession{
		MachineID:       "machine_a",
		TMUXSessionName: "alterego-task-escape",
	}, "Escape")
	if err != nil {
		t.Fatalf("SendInteractiveKey returned error: %v", err)
	}
	if !strings.Contains(transport.lastCommand, "tmux send-keys -t 'alterego-task-escape' 'Escape'") {
		t.Fatalf("command = %q, want send-keys Escape", transport.lastCommand)
	}
	if strings.Contains(transport.lastCommand, "Enter") {
		t.Fatalf("command = %q, want no Enter for raw key send", transport.lastCommand)
	}
}

func TestSSHRunnerHasSessionUsesTMUXHasSession(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder", ShellInit: []string{"source /opt/codex/env.sh"}}
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return machine, nil }

	ok, err := runner.HasSession(context.Background(), RemoteSession{
		MachineID:       "machine_a",
		TMUXSessionName: "alterego-task-4",
	})
	if err != nil {
		t.Fatalf("HasSession returned error: %v", err)
	}
	if !ok {
		t.Fatal("HasSession returned false, want true")
	}
	if !strings.Contains(transport.lastCommand, "tmux has-session -t 'alterego-task-4'") {
		t.Fatalf("command = %q, want has-session", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "source /opt/codex/env.sh && tmux has-session") {
		t.Fatalf("command = %q, want shell init before has-session", transport.lastCommand)
	}
}

func TestSSHRunnerCaptureIncludesPaneState(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{
		stdoutByCall: []string{
			"bash|0|0\n",
			"recent output",
		},
	}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder", ShellInit: []string{"source /opt/codex/env.sh"}}
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return machine, nil }

	window, err := runner.CaptureOutput(context.Background(), RemoteSession{
		MachineID:       "machine_a",
		TMUXSessionName: "alterego-task-state",
	})
	if err != nil {
		t.Fatalf("CaptureOutput returned error: %v", err)
	}
	if window.SessionState.CurrentCommand != "bash" {
		t.Fatalf("window.SessionState.CurrentCommand = %q, want bash", window.SessionState.CurrentCommand)
	}
	if len(transport.commands) != 2 {
		t.Fatalf("len(transport.commands) = %d, want 2", len(transport.commands))
	}
	if !strings.Contains(transport.commands[0], "tmux list-panes -t 'alterego-task-state'") {
		t.Fatalf("first command = %q, want list-panes probe", transport.commands[0])
	}
	if !strings.Contains(transport.commands[1], "tmux capture-pane -p -t 'alterego-task-state'") {
		t.Fatalf("second command = %q, want capture-pane", transport.commands[1])
	}
}

func TestSSHRunnerResumeLastCodexSessionUsesTMUXSendKeys(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder", ShellInit: []string{"source /opt/codex/env.sh", "export CODEX_HOME=/opt/codex-home"}}
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return machine, nil }

	err := runner.ResumeLastCodexSession(context.Background(), RemoteSession{
		MachineID:       "machine_a",
		TMUXSessionName: "alterego-task-6",
		Workdir:         "/srv/codex-tasks/task-6/repo",
	})
	if err != nil {
		t.Fatalf("ResumeLastCodexSession returned error: %v", err)
	}
	if !strings.Contains(transport.lastCommand, "tmux send-keys -t 'alterego-task-6' -- 'source /opt/codex/env.sh && export CODEX_HOME=/opt/codex-home && cd '\\''/srv/codex-tasks/task-6/repo'\\'' && codex resume --last --dangerously-bypass-approvals-and-sandbox' Enter") {
		t.Fatalf("command = %q, want tmux send-keys with codex resume --last", transport.lastCommand)
	}
}

func TestSSHRunnerStopUsesKillSession(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHTransport{}
	runner := NewSSHRunner(transport)
	machine := MachineConfig{ID: "machine_a", Host: "host-a", User: "coder", ShellInit: []string{"source /opt/codex/env.sh"}}
	runner.machineResolver = func(machineID string) (MachineConfig, error) { return machine, nil }

	err := runner.StopSession(context.Background(), RemoteSession{
		MachineID:       "machine_a",
		TMUXSessionName: "alterego-task-5",
	})
	if err != nil {
		t.Fatalf("StopSession returned error: %v", err)
	}
	if !strings.Contains(transport.lastCommand, "tmux kill-session -t 'alterego-task-5'") {
		t.Fatalf("command = %q, want kill-session", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "source /opt/codex/env.sh && tmux kill-session") {
		t.Fatalf("command = %q, want shell init before kill-session", transport.lastCommand)
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
	err         error
}

func (f *fakeSSHTransport) Run(_ context.Context, _ MachineConfig, command string, stdin string) (string, error) {
	f.callCount++
	f.lastCommand = command
	f.lastStdin = stdin
	f.commands = append(f.commands, command)
	f.stdins = append(f.stdins, stdin)
	if f.err != nil {
		return "", f.err
	}
	if len(f.stdoutByCall) >= f.callCount {
		return f.stdoutByCall[f.callCount-1], nil
	}
	return f.stdout, nil
}
