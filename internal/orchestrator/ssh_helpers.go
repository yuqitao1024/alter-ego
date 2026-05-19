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
)

type sshTransport interface {
	Run(ctx context.Context, machine MachineConfig, command string, stdin string) (string, error)
}

var ErrRemoteCommandTimeout = errors.New("remote command timeout")

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
