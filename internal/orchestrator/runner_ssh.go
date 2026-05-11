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

func (r *SSHRunner) StartNewSession(ctx context.Context, req StartRequest) (RemoteSession, error) {
	command := buildStartCommand(req)
	stdin := buildStartInput(req.WorkflowContent, req.UserRequest)

	output, err := r.transport.Run(ctx, req.Machine, command, stdin)
	if err != nil {
		return RemoteSession{}, fmt.Errorf("start remote codex session: %w", err)
	}
	return parseRemoteSession(output, req.Machine.ID, false)
}

func (r *SSHRunner) ProbeSession(ctx context.Context, req ProbeRequest) (ProbeResult, error) {
	command := buildProbeCommand(req.ProcessIdentity)
	output, err := r.transport.Run(ctx, req.Machine, command, "")
	if err != nil {
		return ProbeResult{}, fmt.Errorf("probe remote codex session: %w", err)
	}
	return parseProbeResult(output), nil
}

func (r *SSHRunner) AttachLiveSession(ctx context.Context, req AttachRequest) (RemoteSession, error) {
	command := buildResumeCommand(req.Workdir, req.CodexSessionID, "")
	output, err := r.transport.Run(ctx, req.Machine, command, "")
	if err != nil {
		return RemoteSession{}, fmt.Errorf("attach live codex session: %w", err)
	}
	return parseRemoteSession(output, req.Machine.ID, true)
}

func (r *SSHRunner) ResumeExitedSession(ctx context.Context, req ResumeRequest) (RemoteSession, error) {
	command := buildResumeCommand(req.Workdir, req.CodexSessionID, "")
	output, err := r.transport.Run(ctx, req.Machine, command, "")
	if err != nil {
		return RemoteSession{}, fmt.Errorf("resume exited codex session: %w", err)
	}
	return parseRemoteSession(output, req.Machine.ID, false)
}

func (r *SSHRunner) SendInput(ctx context.Context, session RemoteSession, input string) error {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}

	command := buildResumeCommand(session.Workdir, session.CodexSessionID, "-")
	if _, err := r.transport.Run(ctx, machine, command, input); err != nil {
		return fmt.Errorf("send codex session input: %w", err)
	}
	return nil
}

func (r *SSHRunner) ReadWindow(ctx context.Context, session RemoteSession) (OutputWindow, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return OutputWindow{}, err
	}

	command := buildResumeCommand(session.Workdir, session.CodexSessionID, "")
	output, err := r.transport.Run(ctx, machine, command, "")
	if err != nil {
		return OutputWindow{}, fmt.Errorf("read codex session window: %w", err)
	}

	summary := strings.TrimSpace(output)
	return OutputWindow{
		RawOutput: output,
		Summary:   summary,
	}, nil
}

func (r *SSHRunner) StopTask(ctx context.Context, session RemoteSession) error {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}

	command := buildStopCommand(session.ProcessIdentity)
	if _, err := r.transport.Run(ctx, machine, command, ""); err != nil {
		return fmt.Errorf("stop codex session: %w", err)
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
	steps = append(steps, fmt.Sprintf("cd %s && codex exec --json -", shellQuote(repoDir)))
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

func buildProbeCommand(processIdentity string) string {
	pid := strings.TrimSpace(processIdentity)
	if pid == "" {
		return "printf 'dead\\n'"
	}
	return fmt.Sprintf("if kill -0 %s 2>/dev/null; then printf 'alive %s\\n'; else printf 'dead\\n'; fi", shellQuote(pid), shellQuote(pid))
}

func buildResumeCommand(workdir, sessionID, promptArg string) string {
	command := fmt.Sprintf("cd %s && codex exec resume %s --json", shellQuote(workdir), shellQuote(sessionID))
	if strings.TrimSpace(promptArg) != "" {
		command += " " + promptArg
	}
	return command
}

func buildStopCommand(processIdentity string) string {
	pid := strings.TrimSpace(processIdentity)
	if pid == "" {
		return "printf 'no-process\\n'"
	}
	return fmt.Sprintf("kill %s", shellQuote(pid))
}

func parseProbeResult(output string) ProbeResult {
	trimmed := strings.TrimSpace(output)
	if strings.HasPrefix(trimmed, "alive ") {
		return ProbeResult{
			Alive:           true,
			ProcessIdentity: strings.TrimSpace(strings.TrimPrefix(trimmed, "alive ")),
		}
	}
	return ProbeResult{}
}

func parseRemoteSession(output, machineID string, attachedToLive bool) (RemoteSession, error) {
	session := RemoteSession{
		MachineID:      machineID,
		AttachedToLive: attachedToLive,
	}

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
		case "session_id":
			session.CodexSessionID = value
		case "process_identity":
			session.ProcessIdentity = value
		case "workdir":
			session.Workdir = value
		case "summary":
			session.LastOutputWindow.Summary = value
		}
	}

	if strings.TrimSpace(session.CodexSessionID) == "" {
		return RemoteSession{}, fmt.Errorf("remote codex session output missing session_id")
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
