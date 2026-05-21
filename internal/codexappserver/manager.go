package codexappserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type MachineRuntimeConfig struct {
	MachineID    string
	WebSocketURL string
	BearerToken  string
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
	ResumeThread(ctx context.Context, threadID string) (Thread, error)
	RespondToServerRequest(ctx context.Context, requestID string, result any) error
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
				URL:         machine.WebSocketURL,
				BearerToken: machine.BearerToken,
				ClientInfo: ClientInfo{
					Name:    "alterego",
					Version: "1",
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

	if _, err := m.watchTaskThread(ctx, machine, threadID, false); err != nil {
		return "", "", err
	}

	turnID, err := runtime.client.StartTurn(ctx, TurnStartRequest{
		ThreadID: threadID,
		Input:    textInput(req.Input),
	})
	if err != nil {
		return "", "", fmt.Errorf("start turn: %w", err)
	}

	return threadID, turnID, nil
}

func (m *Manager) WatchTaskThread(ctx context.Context, machine MachineRuntimeConfig, threadID string) (*ThreadWatcher, error) {
	return m.watchTaskThread(ctx, machine, threadID, false)
}

func (m *Manager) ResumeTaskThread(ctx context.Context, machine MachineRuntimeConfig, threadID string) (*ThreadWatcher, error) {
	return m.watchTaskThread(ctx, machine, threadID, true)
}

func (m *Manager) watchTaskThread(ctx context.Context, machine MachineRuntimeConfig, threadID string, resume bool) (*ThreadWatcher, error) {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	watcher := runtime.watchers[threadID]
	created := false
	if watcher == nil {
		watcher = newThreadWatcher(threadID)
		runtime.watchers[threadID] = watcher
		created = true
	}
	if created && resume {
		thread, err := runtime.client.ResumeThread(ctx, threadID)
		if err != nil {
			delete(runtime.watchers, threadID)
			return nil, err
		}
		watcher.hydrate(thread)
	}
	watcher.markConnecting()

	return watcher, nil
}

func (m *Manager) Snapshot(machineID, threadID string) (ThreadSnapshot, bool) {
	m.mu.Lock()
	runtime := m.machines[machineID]
	if runtime == nil {
		m.mu.Unlock()
		return ThreadSnapshot{}, false
	}

	watcher := runtime.watchers[threadID]
	m.mu.Unlock()
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
			ThreadID:       threadID,
			ExpectedTurnID: activeTurnID,
			Input:          textInput(input),
		})
	}

	return runtime.client.StartTurn(ctx, TurnStartRequest{
		ThreadID:       threadID,
		ExpectedTurnID: activeTurnID,
		Input:          textInput(input),
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

func (m *Manager) RespondToServerRequest(ctx context.Context, machine MachineRuntimeConfig, requestID string, result any) error {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return err
	}
	return runtime.client.RespondToServerRequest(ctx, requestID, result)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for _, runtime := range m.machines {
		if runtime == nil || runtime.client == nil {
			continue
		}
		if err := runtime.client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (m *Manager) ensureMachine(ctx context.Context, machine MachineRuntimeConfig) (*machineRuntime, error) {
	m.mu.Lock()
	if runtime := m.machines[machine.MachineID]; runtime != nil {
		clientAlive := runtime.client != nil
		m.mu.Unlock()
		if clientAlive {
			return runtime, nil
		}
		return m.redialMachine(ctx, machine)
	}
	m.mu.Unlock()

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

func (m *Manager) redialMachine(ctx context.Context, machine MachineRuntimeConfig) (*machineRuntime, error) {
	client, err := m.dialClient(ctx, machine)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	runtime := m.machines[machine.MachineID]
	if runtime == nil {
		runtime = &machineRuntime{
			machine:  machine,
			watchers: make(map[string]*ThreadWatcher),
		}
		m.machines[machine.MachineID] = runtime
	}
	runtime.machine = machine
	runtime.client = client
	for threadID, watcher := range runtime.watchers {
		thread, err := runtime.client.ResumeThread(ctx, threadID)
		if err != nil {
			return nil, err
		}
		watcher.hydrate(thread)
		watcher.markConnecting()
	}

	go m.consumeNotifications(machine.MachineID, runtime)

	return runtime, nil
}

func textInput(input string) []InputItem {
	return []InputItem{{
		Type: "text",
		Text: input,
	}}
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
	m.markRuntimeDisconnected(machineID, runtime, markMessage)
}

func (m *Manager) markRuntimeDisconnected(machineID string, runtime *machineRuntime, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current := m.machines[machineID]
	if current == nil || current != runtime {
		return
	}

	runtime.client = nil
	for _, watcher := range current.watchers {
		watcher.markError(message)
	}
}
