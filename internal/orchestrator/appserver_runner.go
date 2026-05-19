package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type appServerRunnerClient interface {
	StartThread(ctx context.Context, req ThreadStartRequest) (string, error)
	StartTurn(ctx context.Context, req TurnStartRequest) (string, error)
	SteerTurn(ctx context.Context, req TurnSteerRequest) (string, error)
	InterruptTurn(ctx context.Context, req TurnInterruptRequest) error
	GetThread(ctx context.Context, req ThreadGetRequest) (AppServerThread, error)
	ListThreadItems(ctx context.Context, req ThreadItemsListRequest) ([]AppServerThreadItem, error)
}

type appServerProxyConnector interface {
	Connect(ctx context.Context, machine MachineConfig) (AppServerTransport, error)
}

type AppServerRunner struct {
	transport       sshTransport
	proxy           appServerProxyConnector
	clientFactory   func(AppServerTransport) appServerRunnerClient
	machineResolver func(machineID string) (MachineConfig, error)
}

var ErrAppServerThreadMissing = errors.New("app-server thread missing")
var ErrAppServerStopUnsupported = errors.New("app-server runner does not support stopping remote threads without thread and turn identity")

func NewAppServerRunner(proxy appServerProxyConnector, client appServerRunnerClient) *AppServerRunner {
	if proxy == nil {
		proxy = NewSSHAppServerProxy(nil)
	}

	runner := &AppServerRunner{
		transport: shellSSHTransport{},
		proxy:     proxy,
		machineResolver: func(machineID string) (MachineConfig, error) {
			return MachineConfig{}, fmt.Errorf("machine resolver is not configured for %q", machineID)
		},
	}
	if client != nil {
		runner.clientFactory = func(AppServerTransport) appServerRunnerClient { return client }
	} else {
		runner.clientFactory = func(transport AppServerTransport) appServerRunnerClient {
			return NewAppServerClient(transport)
		}
	}
	return runner
}

func (r *AppServerRunner) SetMachineResolver(resolver func(machineID string) (MachineConfig, error)) {
	if resolver != nil {
		r.machineResolver = resolver
	}
}

func (r *AppServerRunner) StartInteractiveSession(ctx context.Context, req StartRequest) (RemoteSession, error) {
	repoDir := taskRepoWorkdir(req.RemoteWorkspaceRoot, req.TaskID)
	command := wrapRemoteCommand(req.Machine, buildPrepareWorkspaceCommand(req))
	if _, err := r.runWorkspaceCommand(ctx, req.Machine, "prepare remote workspace", command); err != nil {
		return RemoteSession{}, err
	}

	client, closeFn, err := r.connectClient(ctx, req.Machine)
	if err != nil {
		return RemoteSession{}, err
	}
	defer closeFn()

	threadID, err := client.StartThread(ctx, ThreadStartRequest{
		Cwd:              repoDir,
		BaseInstructions: strings.TrimSpace(req.WorkflowContent),
	})
	if err != nil {
		return RemoteSession{}, fmt.Errorf("start app-server thread: %w", err)
	}

	turnID, err := client.StartTurn(ctx, TurnStartRequest{
		ThreadID: threadID,
		Input:    buildStartInput(req.WorkflowContent, req.UserRequest),
	})
	if err != nil {
		return RemoteSession{}, fmt.Errorf("start app-server turn: %w", err)
	}

	return RemoteSession{
		MachineID:    req.Machine.ID,
		Workdir:      repoDir,
		ThreadID:     threadID,
		ActiveTurnID: turnID,
	}, nil
}

func (r *AppServerRunner) CaptureOutput(ctx context.Context, session RemoteSession) (OutputWindow, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return OutputWindow{}, err
	}

	client, closeFn, err := r.connectClient(ctx, machine)
	if err != nil {
		return OutputWindow{}, err
	}
	defer closeFn()

	thread, err := client.GetThread(ctx, ThreadGetRequest{ThreadID: session.ThreadID})
	if err != nil {
		return OutputWindow{}, fmt.Errorf("get app-server thread: %w", err)
	}
	items, err := client.ListThreadItems(ctx, ThreadItemsListRequest{ThreadID: session.ThreadID})
	if err != nil {
		return OutputWindow{}, fmt.Errorf("list app-server thread items: %w", err)
	}

	summary := summarizeThreadItems(items)
	return OutputWindow{
		RawOutput: summary,
		Summary:   summary,
		SessionState: SessionState{
			ThreadStatus: thread.Status,
		},
	}, nil
}

func (r *AppServerRunner) SendInteractiveInput(ctx context.Context, session RemoteSession, input string) (RemoteSession, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return RemoteSession{}, err
	}

	client, closeFn, err := r.connectClient(ctx, machine)
	if err != nil {
		return RemoteSession{}, err
	}
	defer closeFn()

	if strings.TrimSpace(session.ActiveTurnID) != "" {
		turnID, err := client.SteerTurn(ctx, TurnSteerRequest{
			TurnID: session.ActiveTurnID,
			Input:  input,
		})
		if err != nil {
			return RemoteSession{}, fmt.Errorf("steer app-server turn: %w", err)
		}
		if strings.TrimSpace(turnID) != "" {
			session.ActiveTurnID = turnID
		}
		return session, nil
	}

	turnID, err := client.StartTurn(ctx, TurnStartRequest{
		ThreadID: session.ThreadID,
		Input:    input,
	})
	if err != nil {
		return RemoteSession{}, fmt.Errorf("start app-server turn: %w", err)
	}
	session.ActiveTurnID = turnID
	return session, nil
}

func (r *AppServerRunner) HasSession(ctx context.Context, session RemoteSession) (bool, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return false, err
	}

	client, closeFn, err := r.connectClient(ctx, machine)
	if err != nil {
		return false, err
	}
	defer closeFn()

	_, err = client.GetThread(ctx, ThreadGetRequest{ThreadID: session.ThreadID})
	if err == nil {
		return true, nil
	}
	if isAppServerThreadMissingError(err) {
		return false, nil
	}
	return false, fmt.Errorf("get app-server thread: %w", err)
}

func (r *AppServerRunner) StopSession(ctx context.Context, session RemoteSession) error {
	if strings.TrimSpace(session.ThreadID) == "" || strings.TrimSpace(session.ActiveTurnID) == "" {
		return ErrAppServerStopUnsupported
	}

	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}

	client, closeFn, err := r.connectClient(ctx, machine)
	if err != nil {
		return err
	}
	defer closeFn()

	if err := client.InterruptTurn(ctx, TurnInterruptRequest{
		ThreadID: session.ThreadID,
		TurnID:   session.ActiveTurnID,
	}); err != nil {
		return fmt.Errorf("interrupt app-server turn: %w", err)
	}
	return nil
}

func (r *AppServerRunner) runWorkspaceCommand(ctx context.Context, machine MachineConfig, operation string, command string) (string, error) {
	output, err := r.transport.Run(ctx, machine, command, "")
	if err != nil {
		return "", fmt.Errorf("%s: %w", operation, err)
	}
	return output, nil
}

func (r *AppServerRunner) connectClient(ctx context.Context, machine MachineConfig) (appServerRunnerClient, func(), error) {
	transport, err := r.proxy.Connect(ctx, machine)
	if err != nil {
		return nil, nil, fmt.Errorf("connect app-server proxy: %w", err)
	}
	closeFn := func() { _ = transport.Close() }
	return r.clientFactory(transport), closeFn, nil
}

func buildPrepareWorkspaceCommand(req StartRequest) string {
	taskRoot := taskRootDir(req.RemoteWorkspaceRoot, req.TaskID)
	repoDir := taskRepoWorkdir(req.RemoteWorkspaceRoot, req.TaskID)

	steps := []string{
		fmt.Sprintf("mkdir -p %s", shellQuote(taskRoot)),
		fmt.Sprintf("rm -rf %s", shellQuote(repoDir)),
		fmt.Sprintf("cd %s", shellQuote(taskRoot)),
	}
	steps = append(steps, req.PreCloneBootstrap...)
	steps = append(steps,
		fmt.Sprintf("git clone %s repo", shellQuote(req.RemoteRepoURL)),
		fmt.Sprintf("cd %s", shellQuote(repoDir)),
		fmt.Sprintf("git checkout %s", shellQuote(req.CheckoutBranch)),
	)
	steps = append(steps, req.PostCloneBootstrap...)
	return strings.Join(steps, " && ")
}

func summarizeThreadItems(items []AppServerThreadItem) string {
	var latestAgentMessage string
	var latestPlan string
	var latestCommand string

	for _, item := range items {
		switch normalizeThreadItemType(item.Type) {
		case "agentmessage":
			if text := extractThreadItemText(item.Payload); text != "" {
				latestAgentMessage = text
			}
		case "plan":
			if text := extractThreadItemText(item.Payload); text != "" {
				latestPlan = text
			}
		case "commandexecution":
			if text := extractCommandExecutionText(item.Payload); text != "" {
				latestCommand = text
			}
		}
	}

	parts := make([]string, 0, 3)
	if latestAgentMessage != "" {
		parts = append(parts, latestAgentMessage)
	}
	if latestPlan != "" {
		parts = append(parts, latestPlan)
	}
	if latestCommand != "" {
		parts = append(parts, latestCommand)
	}
	return strings.Join(parts, "\n\n")
}

func normalizeThreadItemType(itemType string) string {
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(itemType)))
}

func extractThreadItemText(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}

	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return ""
	}

	for _, key := range []string{"text", "message", "summary"} {
		if value, ok := data[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractCommandExecutionText(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}

	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return ""
	}
	for _, key := range []string{"command", "text", "summary"} {
		if value, ok := data[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isAppServerThreadMissingError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return errors.Is(err, ErrAppServerThreadMissing) ||
		strings.Contains(message, "thread missing") ||
		strings.Contains(message, "thread not found")
}
