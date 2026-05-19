package codexappserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type MachineRuntimeConfig struct {
	MachineID    string
	WebSocketURL string
}

type StartTaskSessionRequest struct {
	Cwd              string
	BaseInstructions string
	Input            string
}

type ClientAPI interface {
	Close() error
	Notifications() <-chan rpcMessage
	StartThread(ctx context.Context, req ThreadStartRequest) (string, error)
	StartTurn(ctx context.Context, req TurnStartRequest) (string, error)
	SteerTurn(ctx context.Context, req TurnSteerRequest) (string, error)
	InterruptTurn(ctx context.Context, req TurnInterruptRequest) error
}

type ManagerOptions struct {
	DialClient func(ctx context.Context, machine MachineRuntimeConfig) (ClientAPI, error)
}

type Manager struct {
	mu         sync.Mutex
	dialClient func(ctx context.Context, machine MachineRuntimeConfig) (ClientAPI, error)
	machines   map[string]*machineRuntime
}

type machineRuntime struct {
	machine  MachineRuntimeConfig
	client   ClientAPI
	watchers map[string]*ThreadWatcher
}

func NewManager(opts ManagerOptions) *Manager {
	dialClient := opts.DialClient
	if dialClient == nil {
		dialClient = func(ctx context.Context, machine MachineRuntimeConfig) (ClientAPI, error) {
			return NewClient(ctx, ClientOptions{
				URL: machine.WebSocketURL,
				ClientInfo: ClientInfo{
					Name: "alterego",
				},
			})
		}
	}

	return &Manager{
		dialClient: dialClient,
		machines:   make(map[string]*machineRuntime),
	}
}

func (m *Manager) StartTaskSession(ctx context.Context, machine MachineRuntimeConfig, req StartTaskSessionRequest) (string, string, error) {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return "", "", err
	}

	threadID, err := runtime.client.StartThread(ctx, ThreadStartRequest{
		Cwd:              req.Cwd,
		BaseInstructions: req.BaseInstructions,
	})
	if err != nil {
		return "", "", fmt.Errorf("start thread: %w", err)
	}

	if _, err := m.WatchTaskThread(ctx, machine, threadID); err != nil {
		return "", "", err
	}

	turnID, err := runtime.client.StartTurn(ctx, TurnStartRequest{
		ThreadID: threadID,
		Input:    req.Input,
	})
	if err != nil {
		return "", "", fmt.Errorf("start turn: %w", err)
	}

	return threadID, turnID, nil
}

func (m *Manager) WatchTaskThread(ctx context.Context, machine MachineRuntimeConfig, threadID string) (*ThreadWatcher, error) {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	watcher := runtime.watchers[threadID]
	if watcher == nil {
		watcher = newThreadWatcher(threadID)
		runtime.watchers[threadID] = watcher
	}

	return watcher, nil
}

func (m *Manager) Snapshot(machineID, threadID string) (ThreadSnapshot, bool) {
	m.mu.Lock()
	runtime := m.machines[machineID]
	m.mu.Unlock()
	if runtime == nil {
		return ThreadSnapshot{}, false
	}

	watcher := runtime.watchers[threadID]
	if watcher == nil {
		return ThreadSnapshot{}, false
	}

	return watcher.Snapshot(), true
}

func (m *Manager) SendTaskInput(ctx context.Context, machine MachineRuntimeConfig, threadID, activeTurnID, input string) (string, error) {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(activeTurnID) != "" {
		return runtime.client.SteerTurn(ctx, TurnSteerRequest{
			TurnID: activeTurnID,
			Input:  input,
		})
	}

	return runtime.client.StartTurn(ctx, TurnStartRequest{
		ThreadID: threadID,
		Input:    input,
	})
}

func (m *Manager) InterruptTask(ctx context.Context, machine MachineRuntimeConfig, threadID, activeTurnID string) error {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return err
	}

	return runtime.client.InterruptTurn(ctx, TurnInterruptRequest{
		ThreadID: threadID,
		TurnID:   activeTurnID,
	})
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for _, runtime := range m.machines {
		if err := runtime.client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (m *Manager) ensureMachine(ctx context.Context, machine MachineRuntimeConfig) (*machineRuntime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if runtime := m.machines[machine.MachineID]; runtime != nil {
		return runtime, nil
	}

	client, err := m.dialClient(ctx, machine)
	if err != nil {
		return nil, err
	}

	runtime := &machineRuntime{
		machine:  machine,
		client:   client,
		watchers: make(map[string]*ThreadWatcher),
	}
	m.machines[machine.MachineID] = runtime

	go m.consumeNotifications(machine.MachineID, runtime)

	return runtime, nil
}

func (m *Manager) consumeNotifications(machineID string, runtime *machineRuntime) {
	for msg := range runtime.client.Notifications() {
		m.mu.Lock()
		for _, watcher := range runtime.watchers {
			watcher.apply(msg)
		}
		m.mu.Unlock()
	}

	markMessage := "app-server websocket disconnected"
	if runtime.client == nil {
		markMessage = "app-server client unavailable"
	}
	m.markRuntimeError(machineID, markMessage)
}

func (m *Manager) markRuntimeError(machineID, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	runtime := m.machines[machineID]
	if runtime == nil {
		return
	}

	now := time.Now().UTC()
	for _, watcher := range runtime.watchers {
		watcher.mu.Lock()
		watcher.snapshot.SubscriptionState = SubscriptionStateError
		watcher.snapshot.LastSubscriptionError = message
		watcher.snapshot.LastActivityAt = now
		watcher.mu.Unlock()
	}
}
