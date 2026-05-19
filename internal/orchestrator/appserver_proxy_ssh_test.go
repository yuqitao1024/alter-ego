package orchestrator

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestSSHAppServerProxyStartsRemoteProxyCommand(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHCommandTransport{}
	proxy := NewSSHAppServerProxy(transport)

	appTransport, err := proxy.Connect(context.Background(), MachineConfig{
		ID:              "A5-82",
		Host:            "127.0.0.1",
		Port:            20002,
		User:            "root",
		ShellInit:       []string{"source /home/y00621698/env.sh"},
		AppServerSocket: "/home/y00621698/.codex/app-server.sock",
	})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	if appTransport == nil {
		t.Fatal("Connect returned nil transport")
	}
	if !strings.Contains(transport.lastCommand, "codex app-server proxy --sock '/home/y00621698/.codex/app-server.sock'") {
		t.Fatalf("command = %q", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "source /home/y00621698/env.sh") {
		t.Fatalf("command = %q, want shell init", transport.lastCommand)
	}
	if strings.Contains(transport.lastCommand, "test -S '/home/y00621698/.codex/app-server.sock' ||") {
		t.Fatalf("command = %q, want no bootstrap guard without app_server_bootstrap", transport.lastCommand)
	}
	if strings.Contains(transport.lastCommand, "codex remote-control") {
		t.Fatalf("command = %q, want no fallback bootstrap command", transport.lastCommand)
	}
}

func TestSSHAppServerProxyWrapsConfiguredBootstrapBehindSocketGuard(t *testing.T) {
	t.Parallel()

	transport := &fakeSSHCommandTransport{}
	proxy := NewSSHAppServerProxy(transport)

	_, err := proxy.Connect(context.Background(), MachineConfig{
		ID:                 "A5-82",
		Host:               "127.0.0.1",
		User:               "root",
		AppServerSocket:    "/home/y00621698/.codex/app-server.sock",
		AppServerBootstrap: []string{"source /home/y00621698/env.sh", "codex remote-control --listen unix:///tmp/app-server.sock"},
	})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	if !strings.Contains(transport.lastCommand, "test -S '/home/y00621698/.codex/app-server.sock' || (source /home/y00621698/env.sh && codex remote-control --listen unix:///tmp/app-server.sock)") {
		t.Fatalf("command = %q, want grouped bootstrap override", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "for app_server_wait_attempt in") {
		t.Fatalf("command = %q, want readiness wait loop after bootstrap", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "test -S '/home/y00621698/.codex/app-server.sock' && break") {
		t.Fatalf("command = %q, want readiness loop socket check", transport.lastCommand)
	}
	if !strings.Contains(transport.lastCommand, "&& test -S '/home/y00621698/.codex/app-server.sock' && codex app-server proxy --sock '/home/y00621698/.codex/app-server.sock'") {
		t.Fatalf("command = %q, want final readiness check before proxy launch", transport.lastCommand)
	}
	if strings.Contains(transport.lastCommand, "/tmp/alterego-app-server.log") {
		t.Fatalf("command = %q, want configured bootstrap instead of default remote-control launcher", transport.lastCommand)
	}
}

func TestStdioAppServerTransportCloseIncludesDrainedStderr(t *testing.T) {
	t.Parallel()

	stderrReader, stderrWriter := io.Pipe()
	transport := newTestStdioAppServerTransport(strings.NewReader(""), stderrReader, errors.New("wait: exit status 1"))

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_, _ = io.WriteString(stderrWriter, strings.Repeat("remote failure\n", 8))
		_ = stderrWriter.Close()
	}()

	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("stderr writer blocked; stderr is not being drained")
	}

	err := transport.Close()
	if err == nil {
		t.Fatal("Close returned nil error, want wait failure")
	}
	if !strings.Contains(err.Error(), "wait: exit status 1") {
		t.Fatalf("Close error = %v, want wait error", err)
	}
	if !strings.Contains(err.Error(), "remote failure") {
		t.Fatalf("Close error = %v, want drained stderr text", err)
	}
}

type fakeSSHCommandTransport struct {
	lastMachine MachineConfig
	lastCommand string
}

func (f *fakeSSHCommandTransport) Start(_ context.Context, machine MachineConfig, command string) (AppServerTransport, error) {
	f.lastMachine = machine
	f.lastCommand = command
	return newFakeAppServerTransport(), nil
}

func newTestStdioAppServerTransport(stdout io.Reader, stderr io.Reader, waitErr error) *stdioAppServerTransport {
	return newStdioAppServerTransport(nil, stdout, stderr, func() error { return waitErr }, func() error { return nil })
}
