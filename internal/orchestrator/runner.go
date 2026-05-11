package orchestrator

import (
	"context"
	"fmt"
	"strings"
)

type RemoteRunner interface {
	StartInteractiveSession(ctx context.Context, req StartRequest) (RemoteSession, error)
	CaptureOutput(ctx context.Context, session RemoteSession) (OutputWindow, error)
	SendInteractiveInput(ctx context.Context, session RemoteSession, input string) error
	HasSession(ctx context.Context, session RemoteSession) (bool, error)
	StopSession(ctx context.Context, session RemoteSession) error
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

type RemoteSession struct {
	MachineID        string
	Workdir          string
	TMUXSessionName  string
	CodexSessionID   string
	LastOutputWindow OutputWindow
}

type OutputWindow struct {
	RawOutput string
	Summary   string
}

func ReconnectInteractiveSession(ctx context.Context, runner RemoteRunner, task TaskRun) (RemoteSession, error) {
	session := sessionFromTask(task)
	if strings.TrimSpace(session.Workdir) == "" {
		return RemoteSession{}, fmt.Errorf("task %q has no remote workdir", task.TaskID)
	}
	if strings.TrimSpace(session.TMUXSessionName) == "" {
		return RemoteSession{}, fmt.Errorf("task %q has no tmux session name", task.TaskID)
	}

	ok, err := runner.HasSession(ctx, session)
	if err != nil {
		return RemoteSession{}, fmt.Errorf("check tmux session for task %q: %w", task.TaskID, err)
	}
	if !ok {
		return RemoteSession{}, fmt.Errorf("tmux session %q not found for task %q", session.TMUXSessionName, task.TaskID)
	}
	return session, nil
}

func defaultTMUXSessionName(taskID string) string {
	return "alterego-" + taskID
}

func coalesceString(preferred, fallback string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return fallback
}
