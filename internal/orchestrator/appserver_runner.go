package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/codexappserver"
)

type codexRuntime interface {
	StartTaskSession(ctx context.Context, machine codexappserver.MachineRuntimeConfig, req codexappserver.StartTaskSessionRequest) (string, string, error)
	WatchTaskThread(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID string) (*codexappserver.ThreadWatcher, error)
	SendTaskInput(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID, activeTurnID, input string) (string, error)
	InterruptTask(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID, activeTurnID string) error
	Snapshot(machineID, threadID string) (codexappserver.ThreadSnapshot, bool)
}

type AppServerRunner struct {
	transport       sshTransport
	manager         codexRuntime
	machineResolver func(machineID string) (MachineConfig, error)
}

var ErrAppServerThreadMissing = errors.New("app-server thread missing")
var ErrAppServerStopUnsupported = errors.New("app-server runner does not support stopping remote threads without thread and turn identity")

func NewAppServerRunner(manager codexRuntime) *AppServerRunner {
	return &AppServerRunner{
		transport: shellSSHTransport{},
		manager:   manager,
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
	})
	if err != nil {
		return RemoteSession{}, fmt.Errorf("start app-server session: %w", err)
	}

	if _, err := r.manager.WatchTaskThread(ctx, machineRuntimeConfig(req.Machine), threadID); err != nil {
		return RemoteSession{}, fmt.Errorf("watch app-server thread: %w", err)
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

func (r *AppServerRunner) HasSession(ctx context.Context, session RemoteSession) (bool, error) {
	_ = ctx
	_, ok := r.manager.Snapshot(session.MachineID, session.ThreadID)
	return ok, nil
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
