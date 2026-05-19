package orchestrator

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

	return newStdioAppServerTransport(stdin, stdout, stderr, cmd.Wait, func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	}), nil
}

type stdioAppServerTransport struct {
	stdin        io.WriteCloser
	stdoutReader *bufio.Reader
	stderr       io.Reader
	waitFn       func() error
	killFn       func() error

	recvResultCh    chan appServerReadResult
	stdoutDrainDone chan struct{}
	stderrDrainDone chan struct{}

	stderrMu     sync.Mutex
	stderrBuffer bytes.Buffer
	closeOnce    sync.Once
	closeErr     error
	sendMu       sync.Mutex
}

type appServerReadResult struct {
	data []byte
	err  error
}

func newStdioAppServerTransport(stdin io.WriteCloser, stdout io.Reader, stderr io.Reader, waitFn func() error, killFn func() error) *stdioAppServerTransport {
	transport := &stdioAppServerTransport{
		stdin:           stdin,
		stdoutReader:    bufio.NewReader(stdout),
		stderr:          stderr,
		waitFn:          waitFn,
		killFn:          killFn,
		recvResultCh:    make(chan appServerReadResult, 16),
		stdoutDrainDone: make(chan struct{}),
		stderrDrainDone: make(chan struct{}),
	}
	go transport.drainStdout()
	go transport.drainStderr()
	return transport
}

func (t *stdioAppServerTransport) Send(_ context.Context, request []byte) ([]byte, error) {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()

	payload := append([]byte(nil), request...)
	payload = append(payload, '\n')
	if t.stdin == nil {
		return nil, errors.New("stdio app-server transport stdin is not configured")
	}
	if _, err := t.stdin.Write(payload); err != nil {
		return nil, err
	}
	return nil, nil
}

func (t *stdioAppServerTransport) Recv(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res, ok := <-t.recvResultCh:
		if !ok {
			return nil, io.EOF
		}
		if res.err != nil && len(res.data) == 0 {
			return nil, res.err
		}
		return bytesTrimSpace(res.data), nil
	}
}

func (t *stdioAppServerTransport) Close() error {
	t.closeOnce.Do(func() {
		if t.stdin != nil {
			_ = t.stdin.Close()
		}
		if t.killFn != nil {
			_ = t.killFn()
		}
		var waitErr error
		if t.waitFn != nil {
			waitErr = t.waitFn()
		}
		<-t.stdoutDrainDone
		<-t.stderrDrainDone

		if waitErr == nil {
			return
		}

		stderr := strings.TrimSpace(t.stderrString())
		if stderr != "" {
			t.closeErr = fmt.Errorf("%w: %s", waitErr, stderr)
			return
		}
		t.closeErr = waitErr
	})
	return t.closeErr
}

func (t *stdioAppServerTransport) drainStdout() {
	defer close(t.stdoutDrainDone)
	defer close(t.recvResultCh)

	if t.stdoutReader == nil {
		return
	}

	for {
		data, err := t.stdoutReader.ReadBytes('\n')
		if len(data) > 0 {
			t.recvResultCh <- appServerReadResult{data: append([]byte(nil), data...)}
		}
		if err != nil {
			if len(data) == 0 {
				t.recvResultCh <- appServerReadResult{err: err}
			}
			return
		}
	}
}

func (t *stdioAppServerTransport) drainStderr() {
	defer close(t.stderrDrainDone)
	if t.stderr == nil {
		return
	}

	var buffer bytes.Buffer
	_, _ = io.Copy(&buffer, t.stderr)

	t.stderrMu.Lock()
	t.stderrBuffer.Write(buffer.Bytes())
	t.stderrMu.Unlock()
}

func (t *stdioAppServerTransport) stderrString() string {
	t.stderrMu.Lock()
	defer t.stderrMu.Unlock()
	return t.stderrBuffer.String()
}

func buildAppServerProxyCommand(machine MachineConfig) string {
	socketPath := shellQuote(machine.AppServerSocket)

	steps := make([]string, 0, 5)
	if initPrefix := shellInitPrefix(machine.ShellInit); initPrefix != "" {
		steps = append(steps, initPrefix)
	}
	if bootstrapCommand := buildAppServerBootstrapCommand(machine); bootstrapCommand != "" {
		steps = append(steps,
			fmt.Sprintf("test -S %s || %s", socketPath, bootstrapCommand),
			buildAppServerSocketWaitCommand(socketPath),
			fmt.Sprintf("test -S %s", socketPath),
		)
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

func buildAppServerSocketWaitCommand(socketPath string) string {
	return fmt.Sprintf("for app_server_wait_attempt in 1 2 3 4 5 6 7 8 9 10; do test -S %s && break; sleep 1; done", socketPath)
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
