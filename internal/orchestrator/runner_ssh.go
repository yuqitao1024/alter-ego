package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

type sshTransport interface {
	Run(ctx context.Context, machine MachineConfig, command string, stdin string) (string, error)
}

type SSHRunner struct {
	transport       sshTransport
	machineResolver func(machineID string) (MachineConfig, error)
}

const codexBypassFlags = "--dangerously-bypass-approvals-and-sandbox"

var ErrRemoteCommandTimeout = errors.New("remote command timeout")

const (
	startSessionTimeout      = 60 * time.Second
	captureOutputTimeout     = 8 * time.Second
	sendInteractiveTimeout   = 8 * time.Second
	hasSessionTimeout        = 8 * time.Second
	resumeLastSessionTimeout = 15 * time.Second
	stopSessionTimeout       = 8 * time.Second
)

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
	command := wrapRemoteCommand(req.Machine, buildStartCommand(req))
	output, err := r.runWithTimeout(ctx, startSessionTimeout, req.Machine, "start remote tmux session", command, "")
	if err != nil {
		return RemoteSession{}, err
	}
	return parseRemoteSession(output, req.Machine.ID)
}

func (r *SSHRunner) CaptureOutput(ctx context.Context, session RemoteSession) (OutputWindow, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return OutputWindow{}, err
	}

	stateCommand := wrapRemoteCommand(machine, buildSessionStateCommand(session.TMUXSessionName))
	stateOutput, err := r.runWithTimeout(ctx, captureOutputTimeout, machine, "inspect tmux session state", stateCommand, "")
	if err != nil {
		return OutputWindow{}, err
	}
	state, err := parseSessionState(stateOutput)
	if err != nil {
		return OutputWindow{}, fmt.Errorf("parse tmux session state: %w", err)
	}

	command := wrapRemoteCommand(machine, buildCaptureCommand(session.TMUXSessionName))
	output, err := r.runWithTimeout(ctx, captureOutputTimeout, machine, "capture tmux output", command, "")
	if err != nil {
		return OutputWindow{}, err
	}

	summary := strings.TrimSpace(output)
	return OutputWindow{
		RawOutput:    output,
		Summary:      summary,
		SessionState: state,
	}, nil
}

func (r *SSHRunner) SendInteractiveInput(ctx context.Context, session RemoteSession, input string) error {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}

	command := wrapRemoteCommand(machine, buildSendKeysCommand(session.TMUXSessionName, input))
	if _, err := r.runWithTimeout(ctx, sendInteractiveTimeout, machine, "send tmux input", command, ""); err != nil {
		return err
	}
	return nil
}

func (r *SSHRunner) HasSession(ctx context.Context, session RemoteSession) (bool, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return false, err
	}

	command := wrapRemoteCommand(machine, buildHasSessionCommand(session.TMUXSessionName))
	if _, err := r.runWithTimeout(ctx, hasSessionTimeout, machine, "probe tmux session", command, ""); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "can't find session") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *SSHRunner) ResumeLastCodexSession(ctx context.Context, session RemoteSession) error {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(session.Workdir) == "" {
		return fmt.Errorf("resume codex session: workdir is required")
	}

	command := wrapRemoteCommand(machine, buildResumeLastCommand(session.TMUXSessionName, session.Workdir, machine.ShellInit))
	if _, err := r.runWithTimeout(ctx, resumeLastSessionTimeout, machine, "resume last codex session", command, ""); err != nil {
		return err
	}
	return nil
}

func (r *SSHRunner) StopSession(ctx context.Context, session RemoteSession) error {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}

	command := wrapRemoteCommand(machine, buildStopCommand(session.TMUXSessionName))
	if _, err := r.runWithTimeout(ctx, stopSessionTimeout, machine, "stop tmux session", command, ""); err != nil {
		return err
	}
	return nil
}

func (r *SSHRunner) runWithTimeout(parent context.Context, timeout time.Duration, machine MachineConfig, operation string, command string, stdin string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	output, err := r.transport.Run(ctx, machine, command, stdin)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("%s: %w", operation, ErrRemoteCommandTimeout)
		}
		return "", fmt.Errorf("%s: %w", operation, err)
	}
	return output, nil
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
	promptArg := shellQuote(buildStartInput(req.WorkflowContent, req.UserRequest))
	codexCommandParts := []string{}
	if initPrefix := shellInitPrefix(req.Machine.ShellInit); initPrefix != "" {
		codexCommandParts = append(codexCommandParts, initPrefix)
	}
	codexCommandParts = append(codexCommandParts, fmt.Sprintf("cd %s", shellQuote(repoDir)))
	codexCommandParts = append(codexCommandParts, fmt.Sprintf("codex %s %s", codexBypassFlags, promptArg))
	codexCommand := strings.Join(codexCommandParts, " && ")

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
	return fmt.Sprintf("tmux capture-pane -p -t %s -S -80 -E -", shellQuote(sessionName))
}

func buildSessionStateCommand(sessionName string) string {
	return fmt.Sprintf("tmux list-panes -t %s -F '#{pane_current_command}|#{pane_dead}|#{pane_in_mode}'", shellQuote(sessionName))
}

func buildSendKeysCommand(sessionName, input string) string {
	return fmt.Sprintf("tmux send-keys -t %s -- %s Enter", shellQuote(sessionName), shellQuote(input))
}

func buildResumeLastCommand(sessionName, workdir string, shellInit []string) string {
	parts := make([]string, 0, len(shellInit)+2)
	if initPrefix := shellInitPrefix(shellInit); initPrefix != "" {
		parts = append(parts, initPrefix)
	}
	parts = append(parts,
		fmt.Sprintf("cd %s", shellQuote(workdir)),
		fmt.Sprintf("codex resume --last %s", codexBypassFlags),
	)
	return buildSendKeysCommand(sessionName, strings.Join(parts, " && "))
}

func buildHasSessionCommand(sessionName string) string {
	return fmt.Sprintf("tmux has-session -t %s", shellQuote(sessionName))
}

func buildStopCommand(sessionName string) string {
	return fmt.Sprintf("tmux kill-session -t %s", shellQuote(sessionName))
}

func wrapRemoteCommand(machine MachineConfig, command string) string {
	prefix := shellInitPrefix(machine.ShellInit)
	if prefix == "" {
		return command
	}
	return prefix + " && " + command
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

func parseSessionState(output string) (SessionState, error) {
	line := strings.TrimSpace(output)
	if line == "" {
		return SessionState{}, nil
	}
	line = strings.Split(line, "\n")[0]
	parts := strings.Split(line, "|")
	if len(parts) != 3 {
		return SessionState{}, fmt.Errorf("unexpected session state output %q", line)
	}
	return SessionState{
		CurrentCommand: strings.TrimSpace(parts[0]),
		PaneDead:       strings.TrimSpace(parts[1]) == "1",
		InMode:         strings.TrimSpace(parts[2]) == "1",
	}, nil
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
