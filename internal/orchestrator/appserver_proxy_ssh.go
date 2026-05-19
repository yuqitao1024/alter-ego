package orchestrator

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type sshAppServerCommandTransport interface {
	Start(ctx context.Context, machine MachineConfig, command string) (AppServerTransport, error)
}

type SSHAppServerProxy struct {
	transport sshAppServerCommandTransport
}

func NewSSHAppServerProxy(transport sshAppServerCommandTransport) *SSHAppServerProxy {
	if transport == nil {
		transport = shellSSHAppServerCommandTransport{}
	}
	return &SSHAppServerProxy{transport: transport}
}

func (p *SSHAppServerProxy) Connect(ctx context.Context, machine MachineConfig) (AppServerTransport, error) {
	if strings.TrimSpace(machine.AppServerSocket) == "" {
		return nil, fmt.Errorf("machine %q is missing app_server_socket", machine.ID)
	}

	command := buildAppServerProxyCommand(machine)
	transport, err := p.transport.Start(ctx, machine, command)
	if err != nil {
		return nil, err
	}
	return transport, nil
}

type shellSSHAppServerCommandTransport struct{}

func (shellSSHAppServerCommandTransport) Start(ctx context.Context, machine MachineConfig, command string) (AppServerTransport, error) {
	args := make([]string, 0, 4)
	if machine.Port != 0 {
		args = append(args, "-p", strconv.Itoa(machine.Port))
	}
	args = append(args, sshTarget(machine), command)

	cmd := exec.CommandContext(ctx, "ssh", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open ssh stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open ssh stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open ssh stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ssh proxy: %w", err)
	}

	return &stdioAppServerTransport{
		cmd:          cmd,
		stdin:        stdin,
		stdoutReader: bufio.NewReader(stdout),
		stderrReader: io.ReadAll,
		stderr:       stderr,
	}, nil
}

type stdioAppServerTransport struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stdoutReader *bufio.Reader
	stderrReader func(io.Reader) ([]byte, error)
	stderr       io.Reader

	closeOnce sync.Once
	closeErr  error
	mu        sync.Mutex
}

func (t *stdioAppServerTransport) Send(_ context.Context, request []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	payload := append([]byte(nil), request...)
	payload = append(payload, '\n')
	if _, err := t.stdin.Write(payload); err != nil {
		return nil, err
	}
	return nil, nil
}

func (t *stdioAppServerTransport) Recv(ctx context.Context) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}

	resultCh := make(chan result, 1)
	go func() {
		data, err := t.stdoutReader.ReadBytes('\n')
		resultCh <- result{data: data, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resultCh:
		if res.err != nil && len(res.data) == 0 {
			return nil, res.err
		}
		return bytesTrimSpace(res.data), nil
	}
}

func (t *stdioAppServerTransport) Close() error {
	t.closeOnce.Do(func() {
		_ = t.stdin.Close()
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		err := t.cmd.Wait()
		if err == nil {
			return
		}

		stderr := ""
		if output, readErr := t.stderrReader(t.stderr); readErr == nil {
			stderr = strings.TrimSpace(string(output))
		}
		if stderr != "" {
			t.closeErr = fmt.Errorf("%w: %s", err, stderr)
			return
		}
		t.closeErr = err
	})
	return t.closeErr
}

func buildAppServerProxyCommand(machine MachineConfig) string {
	socketPath := shellQuote(machine.AppServerSocket)

	steps := make([]string, 0, 3)
	if initPrefix := shellInitPrefix(machine.ShellInit); initPrefix != "" {
		steps = append(steps, initPrefix)
	}
	if bootstrapCommand := buildAppServerBootstrapCommand(machine); bootstrapCommand != "" {
		steps = append(steps, fmt.Sprintf("test -S %s || %s", socketPath, bootstrapCommand))
	}
	steps = append(steps, fmt.Sprintf("codex app-server proxy --sock %s", socketPath))
	return strings.Join(steps, " && ")
}

func buildAppServerBootstrapCommand(machine MachineConfig) string {
	bootstrapParts := filterNonEmpty(machine.AppServerBootstrap)
	if len(bootstrapParts) == 1 {
		return bootstrapParts[0]
	}
	if len(bootstrapParts) > 1 {
		return "(" + strings.Join(bootstrapParts, " && ") + ")"
	}
	return ""
}

func filterNonEmpty(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func bytesTrimSpace(data []byte) []byte {
	return []byte(strings.TrimSpace(string(data)))
}
