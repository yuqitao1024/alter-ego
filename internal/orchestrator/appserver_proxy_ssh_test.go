package orchestrator

import (
	"context"
	"strings"
	"testing"
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
	if !strings.Contains(transport.lastCommand, "test -S '/home/y00621698/.codex/app-server.sock' || codex remote-control >/tmp/alterego-app-server.log 2>&1 &") {
		t.Fatalf("command = %q, want bootstrap guard", transport.lastCommand)
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
	if strings.Contains(transport.lastCommand, "/tmp/alterego-app-server.log") {
		t.Fatalf("command = %q, want configured bootstrap instead of default remote-control launcher", transport.lastCommand)
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
