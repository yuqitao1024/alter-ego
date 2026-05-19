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
		"ExecStart=/usr/bin/env codex app-server --listen ws://0.0.0.0:4317 --dangerously-bypass-approvals-and-sandbox",
		"User=coder",
		"Restart=always",
	} {
		if !strings.Contains(unit, part) {
			t.Fatalf("unit = %q, missing %q", unit, part)
		}
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

type fakeSSHRunner struct {
	commands []string
}

func (f *fakeSSHRunner) Run(_ context.Context, _ string, _ int, _ string, command string) (string, error) {
	f.commands = append(f.commands, command)
	return "", nil
}
