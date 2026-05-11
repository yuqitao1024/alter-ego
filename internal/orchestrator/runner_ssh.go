package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

type sshTransport interface {
	Run(ctx context.Context, machine MachineConfig, command string, stdin string) (string, error)
}

type SSHRunner struct {
	transport       sshTransport
	machineResolver func(machineID string) (MachineConfig, error)
}

const codexBypassFlags = "--dangerously-bypass-approvals-and-sandbox"

func NewSSHRunner(transport sshTransport) *SSHRunner {
	if transport == nil {
		transport = shellSSHTransport{}
	}
	return &SSHRunner{
		transport: transport,
		machineResolver: func(machineID string) (MachineConfig, error) {
			return MachineConfig{}, fmt.Errorf("machine resolver is not configured for %q", machineID)
		},
	}
}

func (r *SSHRunner) SetMachineResolver(resolver func(machineID string) (MachineConfig, error)) {
	if resolver != nil {
		r.machineResolver = resolver
	}
}

func (r *SSHRunner) StartInteractiveSession(ctx context.Context, req StartRequest) (RemoteSession, error) {
	command := buildStartCommand(req)
	output, err := r.transport.Run(ctx, req.Machine, command, "")
	if err != nil {
		return RemoteSession{}, fmt.Errorf("start remote tmux session: %w", err)
	}
	return parseRemoteSession(output, req.Machine.ID)
}

func (r *SSHRunner) CaptureOutput(ctx context.Context, session RemoteSession) (OutputWindow, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return OutputWindow{}, err
	}

	command := buildCaptureCommand(session.TMUXSessionName)
	output, err := r.transport.Run(ctx, machine, command, "")
	if err != nil {
		return OutputWindow{}, fmt.Errorf("capture tmux output: %w", err)
	}

	summary := strings.TrimSpace(output)
	return OutputWindow{
		RawOutput: output,
		Summary:   summary,
	}, nil
}

func (r *SSHRunner) SendInteractiveInput(ctx context.Context, session RemoteSession, input string) error {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}

	command := buildSendKeysCommand(session.TMUXSessionName, input)
	if _, err := r.transport.Run(ctx, machine, command, ""); err != nil {
		return fmt.Errorf("send tmux input: %w", err)
	}
	return nil
}

func (r *SSHRunner) HasSession(ctx context.Context, session RemoteSession) (bool, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return false, err
	}

	command := buildHasSessionCommand(session.TMUXSessionName)
	if _, err := r.transport.Run(ctx, machine, command, ""); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "can't find session") {
			return false, nil
		}
		return false, fmt.Errorf("probe tmux session: %w", err)
	}
	return true, nil
}

func (r *SSHRunner) StopSession(ctx context.Context, session RemoteSession) error {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}

	command := buildStopCommand(session.TMUXSessionName)
	if _, err := r.transport.Run(ctx, machine, command, ""); err != nil {
		return fmt.Errorf("stop tmux session: %w", err)
	}
	return nil
}

type shellSSHTransport struct{}

func (shellSSHTransport) Run(ctx context.Context, machine MachineConfig, command string, stdin string) (string, error) {
	args := make([]string, 0, 4)
	if machine.Port != 0 {
		args = append(args, "-p", strconv.Itoa(machine.Port))
	}
	args = append(args, sshTarget(machine), command)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", err
	}
	return stdout.String(), nil
}

func buildStartCommand(req StartRequest) string {
	taskRoot := taskRootDir(req.RemoteWorkspaceRoot, req.TaskID)
	repoDir := taskRepoWorkdir(req.RemoteWorkspaceRoot, req.TaskID)
	sessionName := defaultTMUXSessionName(req.TaskID)
	codexInput := shellQuote(buildStartInput(req.WorkflowContent, req.UserRequest))
	codexCommand := fmt.Sprintf("cd %s && printf %%s %s | codex %s", shellQuote(repoDir), codexInput, codexBypassFlags)

	steps := []string{
		fmt.Sprintf("mkdir -p %s", shellQuote(taskRoot)),
		fmt.Sprintf("cd %s", shellQuote(taskRoot)),
	}
	steps = append(steps, req.PreCloneBootstrap...)
	steps = append(steps,
		fmt.Sprintf("git clone %s repo", shellQuote(req.RemoteRepoURL)),
		fmt.Sprintf("cd %s", shellQuote(repoDir)),
		fmt.Sprintf("git checkout %s", shellQuote(req.CheckoutBranch)),
	)
	steps = append(steps, req.PostCloneBootstrap...)
	steps = append(steps,
		fmt.Sprintf("tmux new-session -d -s %s %s", shellQuote(sessionName), shellQuote(codexCommand)),
		fmt.Sprintf("printf 'tmux_session_name=%s\\nworkdir=%s\\n'", sessionName, repoDir),
	)
	return strings.Join(steps, " && ")
}

func buildStartInput(workflow, userRequest string) string {
	var builder strings.Builder
	if strings.TrimSpace(workflow) != "" {
		builder.WriteString("[Workflow]\n")
		builder.WriteString(strings.TrimSpace(workflow))
		builder.WriteString("\n\n")
	}
	builder.WriteString("[User Request]\n")
	builder.WriteString(strings.TrimSpace(userRequest))
	builder.WriteString("\n")
	return builder.String()
}

func buildCaptureCommand(sessionName string) string {
	return fmt.Sprintf("tmux capture-pane -p -t %s -S -200", shellQuote(sessionName))
}

func buildSendKeysCommand(sessionName, input string) string {
	return fmt.Sprintf("tmux send-keys -t %s -- %s Enter", shellQuote(sessionName), shellQuote(input))
}

func buildHasSessionCommand(sessionName string) string {
	return fmt.Sprintf("tmux has-session -t %s", shellQuote(sessionName))
}

func buildStopCommand(sessionName string) string {
	return fmt.Sprintf("tmux kill-session -t %s", shellQuote(sessionName))
}

func parseRemoteSession(output, machineID string) (RemoteSession, error) {
	session := RemoteSession{MachineID: machineID}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "tmux_session_name":
			session.TMUXSessionName = value
		case "workdir":
			session.Workdir = value
		case "codex_session_id", "session_id":
			session.CodexSessionID = value
		case "summary":
			session.LastOutputWindow.Summary = value
		}
	}

	if strings.TrimSpace(session.TMUXSessionName) == "" {
		return RemoteSession{}, fmt.Errorf("remote tmux session output missing tmux_session_name")
	}
	if strings.TrimSpace(session.Workdir) == "" {
		return RemoteSession{}, fmt.Errorf("remote tmux session output missing workdir")
	}
	return session, nil
}

func sshTarget(machine MachineConfig) string {
	return machine.User + "@" + machine.Host
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func taskRootDir(workspaceRoot, taskID string) string {
	return path.Join(workspaceRoot, taskID)
}

func taskRepoWorkdir(workspaceRoot, taskID string) string {
	return path.Join(taskRootDir(workspaceRoot, taskID), "repo")
}
