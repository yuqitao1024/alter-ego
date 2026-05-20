package codexappserver

import (
	"context"
	"fmt"
	"strings"
)

type MachineInstallConfig struct {
	MachineID   string
	Host        string
	Port        int
	SSHUser     string
	RunUser     string
	ListenHost  string
	ListenPort  int
	ServiceName string
	ShellInit   []string
	WSToken     string
}

type MachineConfigResolver func(machineID string) (MachineInstallConfig, error)

type Installer struct {
	ssh     SSHRunner
	resolve MachineConfigResolver
}

func NewInstaller(ssh SSHRunner, resolve MachineConfigResolver) *Installer {
	if ssh == nil {
		ssh = shellSSHRunner{}
	}
	return &Installer{
		ssh:     ssh,
		resolve: resolve,
	}
}

func (i *Installer) InitMachine(ctx context.Context, machineID string) error {
	if i == nil || i.resolve == nil {
		return fmt.Errorf("machine installer is not configured")
	}

	cfg, err := i.resolve(machineID)
	if err != nil {
		return err
	}

	command := buildInstallCommand(cfg)
	if _, err := i.ssh.Run(ctx, cfg.Host, cfg.Port, cfg.SSHUser, command); err != nil {
		return fmt.Errorf("init machine %q: %w", machineID, err)
	}
	return nil
}

func buildInstallCommand(cfg MachineInstallConfig) string {
	unitPath := fmt.Sprintf("/etc/systemd/system/%s.service", cfg.ServiceName)

	steps := []string{}
	if prefix := shellInitPrefix(cfg.ShellInit); prefix != "" {
		steps = append(steps, prefix)
	}
	steps = append(steps,
		"set -e",
		"command -v codex >/dev/null 2>&1",
		"sudo install -d -m 755 /etc/codex-app-server",
		"sudo install -m 600 /dev/null /etc/codex-app-server/ws.token",
		fmt.Sprintf("printf '%%s\\n' %s | sudo tee /etc/codex-app-server/ws.token >/dev/null", shellSingleQuote(cfg.WSToken)),
		fmt.Sprintf("sudo tee %s >/dev/null <<'EOF'\n%s\nEOF", unitPath, buildSystemdUnit(cfg)),
		"sudo systemctl daemon-reload",
		fmt.Sprintf("sudo systemctl enable %s", cfg.ServiceName),
		fmt.Sprintf("sudo systemctl restart %s", cfg.ServiceName),
		fmt.Sprintf("sudo systemctl is-enabled %s", cfg.ServiceName),
		fmt.Sprintf("sudo systemctl is-active %s", cfg.ServiceName),
	)

	return strings.Join(steps, "\n")
}

func buildSystemdUnit(cfg MachineInstallConfig) string {
	startCommand := fmt.Sprintf("exec /usr/bin/env codex --dangerously-bypass-approvals-and-sandbox app-server --listen ws://%s:%d --ws-auth capability-token --ws-token-file /etc/codex-app-server/ws.token", cfg.ListenHost, cfg.ListenPort)
	if prefix := shellInitPrefix(cfg.ShellInit); prefix != "" {
		startCommand = prefix + " && " + startCommand
	}
	workdir := defaultHomeDir(cfg.RunUser)

	return strings.TrimSpace(fmt.Sprintf(`
[Unit]
Description=Codex App Server
After=network.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
ExecStart=/bin/bash -lc %s
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
`, cfg.RunUser, workdir, shellSingleQuote(startCommand)))
}

func shellInitPrefix(commands []string) string {
	filtered := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		filtered = append(filtered, command)
	}
	return strings.Join(filtered, " && ")
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func defaultHomeDir(user string) string {
	if strings.TrimSpace(user) == "root" {
		return "/root"
	}
	return "/home/" + user
}
