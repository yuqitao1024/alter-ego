package codexappserver

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type SSHRunner interface {
	Run(ctx context.Context, host string, port int, user string, command string) (string, error)
}

type shellSSHRunner struct{}

func (shellSSHRunner) Run(ctx context.Context, host string, port int, user string, command string) (string, error) {
	args := make([]string, 0, 4)
	if port > 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	args = append(args, user+"@"+host, command)

	cmd := exec.CommandContext(ctx, "ssh", args...)
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
