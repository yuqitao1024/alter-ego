package orchestrator

import (
	"context"
	"fmt"
	"strings"
)

type RemoteRunner interface {
	StartInteractiveSession(ctx context.Context, req StartRequest) (RemoteSession, error)
	CaptureOutput(ctx context.Context, session RemoteSession) (OutputWindow, error)
	SendInteractiveInput(ctx context.Context, session RemoteSession, input string) (RemoteSession, error)
	RespondToServerRequest(ctx context.Context, session RemoteSession, req TaskServerRequest, response string) error
	HasSession(ctx context.Context, session RemoteSession) (bool, error)
	StopSession(ctx context.Context, session RemoteSession) error
	Events() <-chan RuntimeEvent
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
	ThreadID         string
	ActiveTurnID     string
	LastOutputWindow OutputWindow
}

type OutputWindow struct {
	RawOutput    string
	Summary      string
	SessionState SessionState
}

type SessionState struct {
	ThreadStatus   string
}

type RuntimeEvent struct {
	MachineID         string
	ThreadID          string
	ServerRequest     *TaskServerRequest
	ResolvedRequestID string
}

func (s SessionState) CodexActive() bool {
	if strings.TrimSpace(s.ThreadStatus) != "" {
		switch strings.ToLower(strings.TrimSpace(s.ThreadStatus)) {
		case "running", "in_progress", "active":
			return true
		}
	}
	return false
}

func ReconnectInteractiveSession(ctx context.Context, runner RemoteRunner, task TaskRun) (RemoteSession, error) {
	session := RemoteSession{
		MachineID:    task.MachineID,
		Workdir:      task.RemoteWorkdir,
		ThreadID:     task.ThreadID,
		ActiveTurnID: task.ActiveTurnID,
	}
	if strings.TrimSpace(session.Workdir) == "" {
		return RemoteSession{}, fmt.Errorf("task %q has no remote workdir", task.TaskID)
	}
	if strings.TrimSpace(session.ThreadID) == "" {
		return RemoteSession{}, fmt.Errorf("task %q has no remote thread identity", task.TaskID)
	}

	ok, err := runner.HasSession(ctx, session)
	if err != nil {
		return RemoteSession{}, fmt.Errorf("check remote session for task %q: %w", task.TaskID, err)
	}
	if !ok {
		return RemoteSession{}, fmt.Errorf("thread %q not found for task %q", session.ThreadID, task.TaskID)
	}
	return session, nil
}

func coalesceString(preferred, fallback string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
