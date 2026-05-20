package codexappserver

import (
	"context"
	"strings"
	"testing"
)

func TestBuildSystemdUnitIncludesDangerousBypassAndWSListen(t *testing.T) {
	t.Parallel()

	unit := buildSystemdUnit(MachineInstallConfig{
		ServiceName: "codex-app-server",
		ListenHost:  "0.0.0.0",
		ListenPort:  4317,
		RunUser:     "coder",
	})

	for _, part := range []string{
		"ExecStart=/bin/bash -lc",
		"codex --dangerously-bypass-approvals-and-sandbox app-server --listen ws://0.0.0.0:4317",
		"User=coder",
		"WorkingDirectory=/home/coder",
		"Restart=always",
	} {
		if !strings.Contains(unit, part) {
			t.Fatalf("unit = %q, missing %q", unit, part)
		}
	}
}

func TestBuildSystemdUnitUsesRootHomeForRootUser(t *testing.T) {
	t.Parallel()

	unit := buildSystemdUnit(MachineInstallConfig{
		ServiceName: "codex-app-server",
		ListenHost:  "127.0.0.1",
		ListenPort:  4317,
		RunUser:     "root",
	})

	if !strings.Contains(unit, "WorkingDirectory=/root") {
		t.Fatalf("unit = %q, want WorkingDirectory=/root", unit)
	}
	if strings.Contains(unit, "WorkingDirectory=/home/root") {
		t.Fatalf("unit = %q, must not use /home/root", unit)
	}
}

func TestInstallerRunsSystemctlSequence(t *testing.T) {
	t.Parallel()

	ssh := &fakeSSHRunner{}
	installer := NewInstaller(ssh, func(machineID string) (MachineInstallConfig, error) {
		return MachineInstallConfig{
			MachineID:   machineID,
			Host:        "machine-a.example.com",
			Port:        22,
			SSHUser:     "ops",
			RunUser:     "coder",
			ListenHost:  "0.0.0.0",
			ListenPort:  4317,
			ServiceName: "codex-app-server",
			ShellInit:   []string{"source ~/.zshrc"},
		}, nil
	})

	if err := installer.InitMachine(context.Background(), "machine_a"); err != nil {
		t.Fatalf("InitMachine returned error: %v", err)
	}
	if len(ssh.commands) != 1 {
		t.Fatalf("ssh.commands = %d, want 1", len(ssh.commands))
	}
	for _, part := range []string{
		"source ~/.zshrc",
		"set -e",
		"command -v codex",
		"systemctl daemon-reload",
		"systemctl enable codex-app-server",
		"systemctl restart codex-app-server",
		"systemctl is-enabled codex-app-server",
		"systemctl is-active codex-app-server",
	} {
		if !strings.Contains(ssh.commands[0], part) {
			t.Fatalf("command = %q, missing %q", ssh.commands[0], part)
		}
	}
}

func TestBuildInstallCommandKeepsHeredocTerminatorSeparate(t *testing.T) {
	t.Parallel()

	command := buildInstallCommand(MachineInstallConfig{
		RunUser:     "root",
		ListenHost:  "192.168.1.10",
		ListenPort:  4317,
		ServiceName: "codex-app-server",
	})

	if !strings.Contains(command, "\nEOF\nsudo systemctl daemon-reload") {
		t.Fatalf("command = %q, want heredoc terminator before systemctl sequence", command)
	}
	if strings.Contains(command, "EOF &&") {
		t.Fatalf("command = %q, heredoc terminator must not be chained with &&", command)
	}
}

type fakeSSHRunner struct {
	commands []string
}

func (f *fakeSSHRunner) Run(_ context.Context, _ string, _ int, _ string, command string) (string, error) {
	f.commands = append(f.commands, command)
	return "", nil
}
