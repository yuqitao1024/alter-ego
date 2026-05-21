package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/yuqitao1024/alter-ego/internal/codexappserver"
)

type codexRuntime interface {
	StartTaskSession(ctx context.Context, machine codexappserver.MachineRuntimeConfig, req codexappserver.StartTaskSessionRequest) (string, string, error)
	WatchTaskThread(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID string) (*codexappserver.ThreadWatcher, error)
	ResumeTaskThread(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID string) (*codexappserver.ThreadWatcher, error)
	SendTaskInput(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID, activeTurnID, input string) (string, error)
	RespondToServerRequest(ctx context.Context, machine codexappserver.MachineRuntimeConfig, requestID string, result any) error
	InterruptTask(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID, activeTurnID string) error
	Snapshot(machineID, threadID string) (codexappserver.ThreadSnapshot, bool)
}

type AppServerRunner struct {
	transport       sshTransport
	manager         codexRuntime
	machineResolver func(machineID string) (MachineConfig, error)
	events          chan RuntimeEvent
	mu              sync.Mutex
	bridgedThreads  map[string]struct{}
}

var ErrAppServerThreadMissing = errors.New("app-server thread missing")
var ErrAppServerStopUnsupported = errors.New("app-server runner does not support stopping remote threads without thread and turn identity")

func NewAppServerRunner(manager codexRuntime) *AppServerRunner {
	return &AppServerRunner{
		transport:      shellSSHTransport{},
		manager:        manager,
		events:         make(chan RuntimeEvent, 64),
		bridgedThreads: make(map[string]struct{}),
		machineResolver: func(machineID string) (MachineConfig, error) {
			return MachineConfig{}, fmt.Errorf("machine resolver is not configured for %q", machineID)
		},
	}
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

	threadID, turnID, err := r.manager.StartTaskSession(ctx, machineRuntimeConfig(req.Machine), codexappserver.StartTaskSessionRequest{
		Cwd:              repoDir,
		BaseInstructions: strings.TrimSpace(req.WorkflowContent),
		Input:            buildStartInput(req.WorkflowContent, req.UserRequest),
		ApprovalPolicy:   "never",
		SandboxPolicy: codexappserver.SandboxPolicy{
			Type:          "workspaceWrite",
			WritableRoots: []string{repoDir},
			NetworkAccess: true,
		},
	})
	if err != nil {
		return RemoteSession{}, fmt.Errorf("start app-server session: %w", err)
	}

	if _, err := r.manager.WatchTaskThread(ctx, machineRuntimeConfig(req.Machine), threadID); err != nil {
		return RemoteSession{}, fmt.Errorf("watch app-server thread: %w", err)
	}
	if err := r.bridgeWatcher(ctx, req.Machine.ID, threadID); err != nil {
		return RemoteSession{}, err
	}

	return RemoteSession{
		MachineID:    req.Machine.ID,
		Workdir:      repoDir,
		ThreadID:     threadID,
		ActiveTurnID: turnID,
	}, nil
}

func (r *AppServerRunner) CaptureOutput(ctx context.Context, session RemoteSession) (OutputWindow, error) {
	_ = ctx

	snapshot, ok := r.manager.Snapshot(session.MachineID, session.ThreadID)
	if !ok {
		return OutputWindow{}, ErrAppServerThreadMissing
	}
	summary := firstNonEmpty(snapshot.LatestSummary, snapshot.LatestAgentMessage, snapshot.LatestPlan, snapshot.LatestCommand)
	return OutputWindow{
		RawOutput: summary,
		Summary:   summary,
		SessionState: SessionState{
			ThreadStatus: snapshot.ThreadStatus,
		},
	}, nil
}

func (r *AppServerRunner) SendInteractiveInput(ctx context.Context, session RemoteSession, input string) (RemoteSession, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return RemoteSession{}, err
	}

	turnID, err := r.manager.SendTaskInput(ctx, machineRuntimeConfig(machine), session.ThreadID, session.ActiveTurnID, input)
	if err != nil {
		return RemoteSession{}, fmt.Errorf("send app-server input: %w", err)
	}
	if strings.TrimSpace(turnID) != "" {
		session.ActiveTurnID = turnID
	}
	return session, nil
}

func (r *AppServerRunner) RespondToServerRequest(ctx context.Context, session RemoteSession, req TaskServerRequest, response string) error {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}

	payload := any(strings.TrimSpace(response))
	if req.RequestType == ServerRequestTypeCommandApproval || req.RequestType == ServerRequestTypeFileApproval {
		payload = mapApprovalResponse(response)
	}
	if err := r.manager.RespondToServerRequest(ctx, machineRuntimeConfig(machine), req.RequestID, payload); err != nil {
		return fmt.Errorf("respond to server request: %w", err)
	}
	return nil
}

func (r *AppServerRunner) HasSession(ctx context.Context, session RemoteSession) (bool, error) {
	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return false, err
	}
	_, err = r.manager.ResumeTaskThread(ctx, machineRuntimeConfig(machine), session.ThreadID)
	if err != nil {
		return false, nil
	}
	if err := r.bridgeWatcher(ctx, session.MachineID, session.ThreadID); err != nil {
		return false, err
	}
	return true, nil
}

func (r *AppServerRunner) Events() <-chan RuntimeEvent {
	return r.events
}

func (r *AppServerRunner) StopSession(ctx context.Context, session RemoteSession) error {
	if strings.TrimSpace(session.ThreadID) == "" || strings.TrimSpace(session.ActiveTurnID) == "" {
		return ErrAppServerStopUnsupported
	}

	machine, err := r.machineResolver(session.MachineID)
	if err != nil {
		return err
	}

	if err := r.manager.InterruptTask(ctx, machineRuntimeConfig(machine), session.ThreadID, session.ActiveTurnID); err != nil {
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

func machineRuntimeConfig(machine MachineConfig) codexappserver.MachineRuntimeConfig {
	return codexappserver.MachineRuntimeConfig{
		MachineID:    machine.ID,
		WebSocketURL: machine.AppServerWebSocketURL(),
		BearerToken:  machine.AppServerWSAuthToken,
	}
}

func (r *AppServerRunner) bridgeWatcher(ctx context.Context, machineID, threadID string) error {
	r.mu.Lock()
	key := machineID + "/" + threadID
	if _, ok := r.bridgedThreads[key]; ok {
		r.mu.Unlock()
		return nil
	}
	r.bridgedThreads[key] = struct{}{}
	r.mu.Unlock()

	machine, err := r.machineResolver(machineID)
	if err != nil {
		return err
	}
	watcher, err := r.manager.WatchTaskThread(ctx, machineRuntimeConfig(machine), threadID)
	if err != nil {
		return err
	}
	if watcher == nil {
		return nil
	}

	go func() {
		for event := range watcher.Events() {
			r.events <- RuntimeEvent{
				MachineID:         machineID,
				ThreadID:          threadID,
				ServerRequest:     convertServerRequest(event.ServerRequest),
				ResolvedRequestID: event.ResolvedRequestID,
			}
		}
	}()
	return nil
}

func convertServerRequest(req *codexappserver.ServerRequest) *TaskServerRequest {
	if req == nil {
		return nil
	}
	payload := string(req.RawParams)
	if payload == "" {
		raw, _ := json.Marshal(req)
		payload = string(raw)
	}
	return &TaskServerRequest{
		RequestID:      req.RequestID,
		ThreadID:       req.ThreadID,
		TurnID:         req.TurnID,
		RequestType:    mapServerRequestType(req.Method),
		RequestPayload: payload,
	}
}

func mapServerRequestType(method string) ServerRequestType {
	switch method {
	case "item/tool/requestUserInput":
		return ServerRequestTypeUserInput
	case "item/commandExecution/requestApproval":
		return ServerRequestTypeCommandApproval
	case "item/fileChange/requestApproval":
		return ServerRequestTypeFileApproval
	default:
		return ServerRequestTypeUserInput
	}
}

func mapApprovalResponse(response string) string {
	normalized := strings.ToLower(strings.TrimSpace(response))
	switch normalized {
	case "", "continue", "approve", "approved", "accept", "yes", "y", "ok":
		return "accept"
	case "accept_for_session", "accept-session", "accept for session":
		return "accept_for_session"
	case "decline", "reject", "no", "n":
		return "decline"
	case "cancel":
		return "cancel"
	default:
		return "accept"
	}
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
