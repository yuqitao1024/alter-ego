package orchestrator

import (
	"context"
	"fmt"
	"strings"
)

type RemoteRunner interface {
	StartNewSession(ctx context.Context, req StartRequest) (RemoteSession, error)
	ProbeSession(ctx context.Context, req ProbeRequest) (ProbeResult, error)
	AttachLiveSession(ctx context.Context, req AttachRequest) (RemoteSession, error)
	ResumeExitedSession(ctx context.Context, req ResumeRequest) (RemoteSession, error)
	SendInput(ctx context.Context, session RemoteSession, input string) error
	ReadWindow(ctx context.Context, session RemoteSession) (OutputWindow, error)
	StopTask(ctx context.Context, session RemoteSession) error
}

type StartRequest struct {
	Machine             MachineConfig
	RepositoryID        string
	TaskID              string
	RemoteRepoURL       string
	RemoteWorkspaceRoot string
	CheckoutBranch      string
	PreCloneBootstrap   []string
	PostCloneBootstrap  []string
	UserRequest         string
	WorkflowContent     string
}

type ProbeRequest struct {
	Machine         MachineConfig
	Workdir         string
	CodexSessionID  string
	ProcessIdentity string
}

type AttachRequest struct {
	Machine         MachineConfig
	Workdir         string
	CodexSessionID  string
	ProcessIdentity string
}

type ResumeRequest struct {
	Machine        MachineConfig
	Workdir        string
	CodexSessionID string
}

type RemoteSession struct {
	MachineID        string
	Workdir          string
	CodexSessionID   string
	ProcessIdentity  string
	AttachedToLive   bool
	LastOutputWindow OutputWindow
}

type OutputWindow struct {
	RawOutput string
	Summary   string
}

type ProbeResult struct {
	Alive           bool
	ProcessIdentity string
}

func RecoverRemoteSession(ctx context.Context, runner RemoteRunner, machine MachineConfig, task TaskRun) (RemoteSession, error) {
	if strings.TrimSpace(task.RemoteWorkdir) == "" {
		return RemoteSession{}, fmt.Errorf("task %q has no remote workdir", task.TaskID)
	}
	if strings.TrimSpace(task.RemoteCodexSessionID) == "" {
		return RemoteSession{}, fmt.Errorf("task %q has no remote codex session id", task.TaskID)
	}

	probe, err := runner.ProbeSession(ctx, ProbeRequest{
		Machine:         machine,
		Workdir:         task.RemoteWorkdir,
		CodexSessionID:  task.RemoteCodexSessionID,
		ProcessIdentity: task.RemoteProcessIdentity,
	})
	if err != nil {
		return RemoteSession{}, fmt.Errorf("probe remote session for task %q: %w", task.TaskID, err)
	}

	if probe.Alive {
		return runner.AttachLiveSession(ctx, AttachRequest{
			Machine:         machine,
			Workdir:         task.RemoteWorkdir,
			CodexSessionID:  task.RemoteCodexSessionID,
			ProcessIdentity: coalesceString(probe.ProcessIdentity, task.RemoteProcessIdentity),
		})
	}

	return runner.ResumeExitedSession(ctx, ResumeRequest{
		Machine:        machine,
		Workdir:        task.RemoteWorkdir,
		CodexSessionID: task.RemoteCodexSessionID,
	})
}

func coalesceString(preferred, fallback string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return fallback
}
