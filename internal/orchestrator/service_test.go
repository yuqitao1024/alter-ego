package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCreateTaskSelectsMachineAndPersistsPendingTask(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	seedTask(t, store, TaskRun{
		TaskID:       "existing",
		TemplateID:   "feature_dev",
		RepositoryID: "repo_backend",
		MachineID:    "machine_a",
		Status:       StatusRunning,
		UserRequest:  "existing work",
		CreatedBy:    "tester",
		CreatedAt:    time.Now().UTC().Add(-time.Minute),
		UpdatedAt:    time.Now().UTC().Add(-time.Minute),
	})

	task, err := service.StartTask(ctx, "feature_dev", "yuqitao", "Add remote control")
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}

	if task.Status != StatusPending {
		t.Fatalf("task.Status = %q, want %q", task.Status, StatusPending)
	}
	if task.MachineID != "machine_b" {
		t.Fatalf("task.MachineID = %q, want machine_b", task.MachineID)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.TemplateID != "feature_dev" || persisted.RepositoryID != "repo_backend" {
		t.Fatalf("persisted template/repository = %q/%q", persisted.TemplateID, persisted.RepositoryID)
	}
}

func TestTickMovesPendingTaskToPreparingWorkspace(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:       "task-start",
		TemplateID:   "feature_dev",
		RepositoryID: "repo_backend",
		MachineID:    "machine_a",
		Status:       StatusPending,
		UserRequest:  "Implement remote start",
		CreatedBy:    "tester",
	})

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusPreparingWorkspace {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusPreparingWorkspace)
	}
}

func TestTickMarksRunningTaskDetachedWhenCaptureTimesOut(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-timeout",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Investigate timeout",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-timeout",
		RemoteCodexSessionID: "session-timeout",
	})
	runner := service.runner.(*fakeServiceRunner)
	runner.captureErr = fmt.Errorf("capture tmux output: %w", ErrRemoteCommandTimeout)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusDetached {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusDetached)
	}
}

func TestTickMarksRunningTaskDetachedWhenSessionNotFoundDuringCapture(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-missing-session",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Investigate missing session",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-missing-session",
		RemoteCodexSessionID: "session-missing-session",
	})
	runner := service.runner.(*fakeServiceRunner)
	runner.captureErr = fmt.Errorf("inspect tmux session state: exit status 1: can't find pane: alterego-task-missing-session")

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusDetached {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusDetached)
	}
}

func TestTickTimeoutOnOneTaskDoesNotBlockNextTask(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	runningTask := seedTask(t, store, TaskRun{
		TaskID:               "task-running-timeout",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Task one",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-running-timeout",
		RemoteCodexSessionID: "session-running-timeout",
	})
	pendingTask := seedTask(t, store, TaskRun{
		TaskID:       "task-pending-next",
		TemplateID:   "feature_dev",
		RepositoryID: "repo_backend",
		MachineID:    "machine_b",
		Status:       StatusPending,
		UserRequest:  "Task two",
		CreatedBy:    "tester",
	})
	runner := service.runner.(*fakeServiceRunner)
	runner.captureErr = fmt.Errorf("capture tmux output: %w", ErrRemoteCommandTimeout)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("first TickOnce returned error: %v", err)
	}
	runner.captureErr = nil

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("second TickOnce returned error: %v", err)
	}

	persistedRunning, err := store.GetTask(ctx, runningTask.TaskID)
	if err != nil {
		t.Fatalf("GetTask(running) returned error: %v", err)
	}
	if persistedRunning.Status != StatusDetached {
		t.Fatalf("persistedRunning.Status = %q, want %q", persistedRunning.Status, StatusDetached)
	}

	persistedPending, err := store.GetTask(ctx, pendingTask.TaskID)
	if err != nil {
		t.Fatalf("GetTask(pending) returned error: %v", err)
	}
	if persistedPending.Status != StatusPreparingWorkspace {
		t.Fatalf("persistedPending.Status = %q, want %q", persistedPending.Status, StatusPreparingWorkspace)
	}
}

func TestTickMovesPreparingWorkspaceTaskToStartingSession(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:       "task-starting",
		TemplateID:   "feature_dev",
		RepositoryID: "repo_backend",
		MachineID:    "machine_a",
		Status:       StatusPreparingWorkspace,
		UserRequest:  "Implement remote start",
		CreatedBy:    "tester",
	})

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusStartingSession {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusStartingSession)
	}
}

func TestTickStartsInteractiveSessionAndStoresTTYMetadata(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:       "task-running",
		TemplateID:   "feature_dev",
		RepositoryID: "repo_backend",
		MachineID:    "machine_a",
		Status:       StatusStartingSession,
		UserRequest:  "Implement remote start",
		CreatedBy:    "tester",
	})
	service.runner.(*fakeServiceRunner).startSession = RemoteSession{
		MachineID:       "machine_a",
		Workdir:         "/srv/codex-tasks/task-running/repo",
		TMUXSessionName: "alterego-task-running",
		CodexSessionID:  "session-start",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.RemoteCodexSessionID != "session-start" {
		t.Fatalf("persisted.RemoteCodexSessionID = %q, want session-start", persisted.RemoteCodexSessionID)
	}
	if persisted.RemoteWorkdir != "/srv/codex-tasks/task-running/repo" {
		t.Fatalf("persisted.RemoteWorkdir = %q, want /srv/codex-tasks/task-running/repo", persisted.RemoteWorkdir)
	}
	if persisted.TMUXSessionName != "alterego-task-running" {
		t.Fatalf("persisted.TMUXSessionName = %q, want alterego-task-running", persisted.TMUXSessionName)
	}
}

func TestTickMovesTaskToWaitingUserInputWhenDecisionRequiresUser(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-wait",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-wait",
		RemoteCodexSessionID: "session-wait",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{Summary: "Two implementation approaches are possible"}
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `I found two implementation approaches.

Which approach should I take for the implementation?
1. Event-driven
2. Polling`,
		Summary: "Two implementation approaches are possible",
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:       "ask_user",
		DecisionType: "implementation_solution_choice",
		Summary:      "Need user to choose approach",
		Question: &AwaitingQuestion{
			QuestionText:   "Choose event-driven or polling design?",
			OptionsSummary: "event-driven | polling",
			ContextExcerpt: "Both approaches are viable",
		},
	}
	notifier := service.notifier.(*fakeTaskNotifier)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusWaitingUserInput {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusWaitingUserInput)
	}
	if persisted.AwaitingQuestion == nil || !strings.Contains(persisted.AwaitingQuestion.QuestionText, "Choose") {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want question", persisted.AwaitingQuestion)
	}
	if notifier.lastTaskID != task.TaskID || notifier.lastUserID != "tester" {
		t.Fatalf("notifier captured task=%q user=%q", notifier.lastTaskID, notifier.lastUserID)
	}
}

func TestTickPromotesPlanningTaskToExecutingOnModelReply(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-phase-promotion",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		Phase:                TaskPhasePlanning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-phase-promotion",
		RemoteCodexSessionID: "session-phase-promotion",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Ready to start implementation",
		Summary:   "Ready to start implementation",
		SessionState: SessionState{
			CurrentCommand: "codex",
		},
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:     DecisionActionReplyToCodex,
		NextPhase:  TaskPhaseExecuting,
		CodexReply: "Start coding now.",
		Summary:    "Implementation starts now",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Phase != TaskPhaseExecuting {
		t.Fatalf("persisted.Phase = %q, want %q", persisted.Phase, TaskPhaseExecuting)
	}
}

func TestTickUsesFixedContinueReplyDuringExecuting(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-executing-continue",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		Phase:                TaskPhaseExecuting,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-executing-continue",
		RemoteCodexSessionID: "session-executing-continue",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Waiting for a short continuation",
		Summary:   "Waiting for a short continuation",
		SessionState: SessionState{
			CurrentCommand: "codex",
		},
	}
	runner := service.runner.(*fakeServiceRunner)
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:     DecisionActionReplyToCodex,
		CodexReply: "Long detailed plan that should be ignored.",
		Summary:    "Continue execution",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.LastInput != executingContinueReply {
		t.Fatalf("persisted.LastInput = %q, want %q", persisted.LastInput, executingContinueReply)
	}
	if len(runner.calls) < 2 || runner.calls[1] != "send" {
		t.Fatalf("runner.calls = %v, want capture then send", runner.calls)
	}
}

func TestTickLeavesPlanPromptForDecisionEngineInExecuting(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-plan-dismiss-continue",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		Phase:                TaskPhaseExecuting,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-plan-dismiss-continue",
		RemoteCodexSessionID: "session-plan-dismiss-continue",
	})
	runner := service.runner.(*fakeServiceRunner)
	runner.outputWindow = OutputWindow{
		RawOutput: "Create a plan?  shift + tab use Plan mode   esc dismiss",
		Summary:   "Create a plan prompt is visible",
		SessionState: SessionState{
			CurrentCommand: "codex",
		},
	}
	decider := service.decider.(*fakeDecisionEngine)
	decider.result = DecisionResult{
		Action:  DecisionActionWait,
		Summary: "Still waiting",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.LastInput != "" {
		t.Fatalf("persisted.LastInput = %q, want empty", persisted.LastInput)
	}
	wantCalls := []string{"capture"}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("runner.calls = %v, want %v", runner.calls, wantCalls)
	}
	if decider.callCount != 1 {
		t.Fatalf("decider.callCount = %d, want 1", decider.callCount)
	}
}

func TestTickForcesUserApprovalBeforeReturningExecutingTaskToPlanning(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-phase-regression",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		Phase:                TaskPhaseExecuting,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-phase-regression",
		RemoteCodexSessionID: "session-phase-regression",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Need to revisit the plan",
		Summary:   "Need to revisit the plan",
		SessionState: SessionState{
			CurrentCommand: "codex",
		},
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:    DecisionActionReplyToCodex,
		NextPhase: TaskPhasePlanning,
		Summary:   "Need to reopen planning",
	}
	notifier := service.notifier.(*fakeTaskNotifier)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusWaitingUserInput {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusWaitingUserInput)
	}
	if persisted.Phase != TaskPhaseExecuting {
		t.Fatalf("persisted.Phase = %q, want %q", persisted.Phase, TaskPhaseExecuting)
	}
	if notifier.lastTaskID != task.TaskID {
		t.Fatalf("notifier.lastTaskID = %q, want %q", notifier.lastTaskID, task.TaskID)
	}
}

func TestTickSkipsDecisionModelWhileCodexIsStillWorking(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-working-skip",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-working-skip",
		RemoteCodexSessionID: "session-working-skip",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Working\nPress Esc to interrupt",
		Summary:   "Working",
		SessionState: SessionState{
			CurrentCommand: "codex",
		},
	}
	decider := service.decider.(*fakeDecisionEngine)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if decider.callCount != 0 {
		t.Fatalf("decider.callCount = %d, want 0", decider.callCount)
	}
}

func TestTickSkipsDecisionModelForSameScreenDuringCooldown(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	now := time.Date(2026, 5, 14, 1, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	ctx := context.Background()
	window := OutputWindow{
		RawOutput: "Need operator confirmation",
		Summary:   "Need operator confirmation",
		SessionState: SessionState{
			CurrentCommand: "codex",
		},
	}
	cooldown := now.Add(30 * time.Second)
	task := seedTask(t, store, TaskRun{
		TaskID:                   "task-decision-cooldown",
		TemplateID:               "feature_dev",
		RepositoryID:             "repo_backend",
		MachineID:                "machine_a",
		Status:                   StatusRunning,
		UserRequest:              "Implement orchestrator",
		CreatedBy:                "tester",
		RemoteWorkdir:            "/srv/backend",
		TMUXSessionName:          "alterego-task-decision-cooldown",
		RemoteCodexSessionID:     "session-decision-cooldown",
		LastScreenDigest:         ScreenDigest(window),
		LastDecisionScreenDigest: ScreenDigest(window),
		LastDecisionAction:       DecisionActionWait,
		DecisionCooldownUntil:    &cooldown,
	})
	service.runner.(*fakeServiceRunner).outputWindow = window
	decider := service.decider.(*fakeDecisionEngine)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if decider.callCount != 0 {
		t.Fatalf("decider.callCount = %d, want 0", decider.callCount)
	}
}

func TestTickRechecksDecisionModelAfterDecisionCooldownExpires(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	now := time.Date(2026, 5, 14, 1, 5, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	ctx := context.Background()
	window := OutputWindow{
		RawOutput: "Need operator confirmation",
		Summary:   "Need operator confirmation",
		SessionState: SessionState{
			CurrentCommand: "codex",
		},
	}
	expired := now.Add(-time.Second)
	task := seedTask(t, store, TaskRun{
		TaskID:                   "task-decision-recheck",
		TemplateID:               "feature_dev",
		RepositoryID:             "repo_backend",
		MachineID:                "machine_a",
		Status:                   StatusRunning,
		UserRequest:              "Implement orchestrator",
		CreatedBy:                "tester",
		RemoteWorkdir:            "/srv/backend",
		TMUXSessionName:          "alterego-task-decision-recheck",
		RemoteCodexSessionID:     "session-decision-recheck",
		LastScreenDigest:         ScreenDigest(window),
		LastDecisionScreenDigest: ScreenDigest(window),
		LastDecisionAction:       DecisionActionWait,
		DecisionCooldownUntil:    &expired,
	})
	service.runner.(*fakeServiceRunner).outputWindow = window
	decider := service.decider.(*fakeDecisionEngine)
	decider.result = DecisionResult{
		Action:  DecisionActionWait,
		Summary: "Still waiting",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if decider.callCount != 1 {
		t.Fatalf("decider.callCount = %d, want 1", decider.callCount)
	}
	if persisted.LastDecisionScreenDigest != ScreenDigest(window) {
		t.Fatalf("persisted.LastDecisionScreenDigest = %q, want %q", persisted.LastDecisionScreenDigest, ScreenDigest(window))
	}
}

func TestTickKeepsTaskRunningWhenDecisionEngineReturnsWait(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-working",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-working",
		RemoteCodexSessionID: "session-working",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "• Working (3s • esc to interrupt)",
		Summary:   "Inspecting repository state",
	}
	decider := service.decider.(*fakeDecisionEngine)
	decider.result = DecisionResult{
		Action:  DecisionActionWait,
		Summary: "Inspecting repository state",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if decider.callCount != 1 {
		t.Fatalf("decider.callCount = %d, want 1", decider.callCount)
	}
}

func TestTickRepliesToCodexWhenDecisionEngineRequestsDirectReply(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-direct-reply",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-direct-reply",
		RemoteCodexSessionID: "session-direct-reply",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `当前状态有冲突，需要先对齐目标。

你现在要我继续哪一条：
1. 切回真正的 issue #30
2. 继续围绕已经合并的 BlockReduce 做收尾/补充`,
		Summary: "需要对齐目标并等待选择",
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:       "reply_to_codex",
		DecisionType: "none",
		Summary:      "Continue with issue #30.",
		CodexReply:   "切回 issue #30，继续开发 simt/std/span。",
	}
	runner := service.runner.(*fakeServiceRunner)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.LastInput != "切回 issue #30，继续开发 simt/std/span。" {
		t.Fatalf("persisted.LastInput = %q", persisted.LastInput)
	}
	if !reflect.DeepEqual(runner.calls, []string{"capture", "send"}) {
		t.Fatalf("runner.calls = %v, want [capture send]", runner.calls)
	}
}

func TestTickMarksTaskCompletedWhenDecisionEngineRequestsCompletion(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-complete",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-complete",
		RemoteCodexSessionID: "session-complete",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `Implementation finished successfully.

Waiting for next instruction.`,
		Summary: "Implementation finished and waiting for next instruction.",
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:  "complete_task",
		Summary: "Task completed successfully.",
	}
	runner := service.runner.(*fakeServiceRunner)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusCompleted {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusCompleted)
	}
	if len(runner.calls) < 2 || runner.calls[0] != "capture" || runner.calls[1] != "stop" {
		t.Fatalf("runner.calls = %v, want capture then stop", runner.calls)
	}
}

func TestTickResumesLastCodexSessionWhenTMUXStillAliveButCodexExited(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-resume-last",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Continue issue 30",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-resume-last",
		RemoteCodexSessionID: "",
	})
	runner := service.runner.(*fakeServiceRunner)
	runner.outputWindow = OutputWindow{
		RawOutput: "stale screen contents",
		Summary:   "stale screen contents",
		SessionState: SessionState{
			CurrentCommand: "bash",
		},
	}
	decider := service.decider.(*fakeDecisionEngine)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if decider.callCount != 0 {
		t.Fatalf("decider.callCount = %d, want 0", decider.callCount)
	}
	if !reflect.DeepEqual(runner.calls, []string{"capture", "resume"}) {
		t.Fatalf("runner.calls = %v, want [capture resume]", runner.calls)
	}
}

func TestTickDoesNotLoopResumeForSameExitedScreen(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	window := OutputWindow{
		RawOutput: "All workflow steps have been completed.\nWaiting for next instruction.",
		Summary:   "All workflow steps have been completed.",
		SessionState: SessionState{
			CurrentCommand: "bash",
		},
	}
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-resume-once",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Continue issue 30",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-resume-once",
		RemoteCodexSessionID: "",
		LastInput:            "[system] codex resume --last",
		LastScreenDigest:     ScreenDigest(window),
	})
	runner := service.runner.(*fakeServiceRunner)
	runner.outputWindow = window
	decider := service.decider.(*fakeDecisionEngine)
	decider.result = DecisionResult{
		Action:  DecisionActionCompleteTask,
		Summary: "Task completed successfully.",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusCompleted {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusCompleted)
	}
	if !reflect.DeepEqual(runner.calls, []string{"capture", "stop"}) {
		t.Fatalf("runner.calls = %v, want [capture stop]", runner.calls)
	}
	if decider.callCount != 1 {
		t.Fatalf("decider.callCount = %d, want 1", decider.callCount)
	}
}

func TestTickAutoRespondsToTrustDirectoryPrompt(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-trust",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-trust",
		RemoteCodexSessionID: "session-trust",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `Do you trust the contents of this directory?
1. Yes, continue
2. No, quit
Press enter to continue`,
		Summary: "Do you trust the contents of this directory?",
	}
	decider := service.decider.(*fakeDecisionEngine)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.LastInput != "1" {
		t.Fatalf("persisted.LastInput = %q, want 1", persisted.LastInput)
	}
	if persisted.LastScreenDigest == "" {
		t.Fatal("persisted.LastScreenDigest is empty, want digest")
	}
	if decider.callCount != 0 {
		t.Fatalf("decider.callCount = %d, want 0", decider.callCount)
	}

	runner := service.runner.(*fakeServiceRunner)
	wantCalls := []string{"capture", "send"}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("runner.calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestTickMovesTaskToWaitingUserInputWhenLoginPromptDetected(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-login",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-login",
		RemoteCodexSessionID: "session-login",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `Welcome to Codex
1. Sign in with ChatGPT
2. Sign in with Device Code
3. Provide your own API key`,
		Summary: "Sign in with ChatGPT",
	}
	decider := service.decider.(*fakeDecisionEngine)
	notifier := service.notifier.(*fakeTaskNotifier)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusWaitingUserInput {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusWaitingUserInput)
	}
	if persisted.AwaitingQuestion == nil || persisted.AwaitingQuestion.QuestionType != "login_required" {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want login_required question", persisted.AwaitingQuestion)
	}
	if decider.callCount != 0 {
		t.Fatalf("decider.callCount = %d, want 0", decider.callCount)
	}
	if notifier.lastTaskID != task.TaskID {
		t.Fatalf("notifier.lastTaskID = %q, want %q", notifier.lastTaskID, task.TaskID)
	}
}

func TestTickMovesTaskToWaitingUserInputWhenUsageLimitPromptDetected(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-usage-limit",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-usage-limit",
		RemoteCodexSessionID: "session-usage-limit",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `Do you trust the contents of this directory?
1. Yes, continue
2. No, quit
Press enter to continue

You've hit your usage limit. Upgrade to Pro, purchase more credits or try again later.`,
		Summary: "You've hit your usage limit.",
	}
	decider := service.decider.(*fakeDecisionEngine)
	notifier := service.notifier.(*fakeTaskNotifier)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusWaitingUserInput {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusWaitingUserInput)
	}
	if persisted.AwaitingQuestion == nil || persisted.AwaitingQuestion.QuestionType != "usage_limit" {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want usage_limit question", persisted.AwaitingQuestion)
	}
	if decider.callCount != 0 {
		t.Fatalf("decider.callCount = %d, want 0", decider.callCount)
	}
	if notifier.lastTaskID != task.TaskID {
		t.Fatalf("notifier.lastTaskID = %q, want %q", notifier.lastTaskID, task.TaskID)
	}
}

func TestTickLeavesPlanPromptForDecisionEngine(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-plan-prompt",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Implement orchestrator",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-plan-prompt",
		RemoteCodexSessionID: "session-plan-prompt",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `Create a plan?
shift + tab use Plan mode
esc dismiss`,
		Summary: "Create a plan? esc dismiss",
	}
	decider := service.decider.(*fakeDecisionEngine)
	runner := service.runner.(*fakeServiceRunner)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.LastInput != "" {
		t.Fatalf("persisted.LastInput = %q, want empty", persisted.LastInput)
	}
	if runner.lastSentKey != "" {
		t.Fatalf("runner.lastSentKey = %q, want empty", runner.lastSentKey)
	}
	if !reflect.DeepEqual(runner.calls, []string{"capture"}) {
		t.Fatalf("runner.calls = %v, want [capture]", runner.calls)
	}
	if decider.callCount != 1 {
		t.Fatalf("decider.callCount = %d, want 1", decider.callCount)
	}
}

func TestReplyResumesWaitingTask(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:                      "task-reply",
		TemplateID:                  "feature_dev",
		RepositoryID:                "repo_backend",
		MachineID:                   "machine_a",
		Status:                      StatusWaitingUserInput,
		UserRequest:                 "Implement orchestrator",
		CreatedBy:                   "tester",
		RemoteWorkdir:               "/srv/backend",
		TMUXSessionName:             "alterego-task-reply",
		RemoteCodexSessionID:        "session-reply",
		ActiveResponderName:         "usage_limit_prompt",
		ActiveResponderScreenDigest: "digest:usage-limit",
		AwaitingQuestion: &AwaitingQuestion{
			QuestionText: "You've hit your usage limit.",
			QuestionType: "usage_limit",
			AskedAt:      time.Now().UTC().Add(-time.Minute),
		},
	})

	if err := service.Reply(ctx, task.TaskID, "Use polling."); err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.AwaitingQuestion != nil {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want nil", persisted.AwaitingQuestion)
	}
	if persisted.LastInput != "Use polling." {
		t.Fatalf("persisted.LastInput = %q, want %q", persisted.LastInput, "Use polling.")
	}
	if persisted.LastResolvedResponderName == "" || persisted.LastResolvedScreenDigest == "" {
		t.Fatalf("resolved responder fields not set: %#v", persisted)
	}
	if persisted.ResponderCooldownUntil == nil {
		t.Fatalf("ResponderCooldownUntil = nil, want cooldown timestamp")
	}
}

func TestTickSkipsRecentlyResolvedResponderForSameScreen(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	now := time.Date(2026, 5, 12, 3, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	ctx := context.Background()
	window := OutputWindow{
		RawOutput: `You've hit your usage limit. Upgrade to Pro, purchase more credits or try again later.`,
		Summary:   "You've hit your usage limit.",
	}
	task := seedTask(t, store, TaskRun{
		TaskID:                      "task-cooldown",
		TemplateID:                  "feature_dev",
		RepositoryID:                "repo_backend",
		MachineID:                   "machine_a",
		Status:                      StatusWaitingUserInput,
		UserRequest:                 "Implement orchestrator",
		CreatedBy:                   "tester",
		RemoteWorkdir:               "/srv/backend",
		TMUXSessionName:             "alterego-task-cooldown",
		RemoteCodexSessionID:        "session-cooldown",
		ActiveResponderName:         "usage_limit_prompt",
		ActiveResponderScreenDigest: ScreenDigest(window),
		AwaitingQuestion: &AwaitingQuestion{
			QuestionText: "You've hit your usage limit.",
			QuestionType: "usage_limit",
			AskedAt:      now.Add(-time.Minute),
		},
	})

	if err := service.Reply(ctx, task.TaskID, "额度已经刷新了，你继续工作吧"); err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	service.runner.(*fakeServiceRunner).outputWindow = window
	notifier := service.notifier.(*fakeTaskNotifier)
	decider := service.decider.(*fakeDecisionEngine)
	runner := service.runner.(*fakeServiceRunner)
	runner.calls = nil

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if notifier.lastTaskID != "" {
		t.Fatalf("notifier.lastTaskID = %q, want empty", notifier.lastTaskID)
	}
	if decider.callCount != 0 {
		t.Fatalf("decider.callCount = %d, want 0", decider.callCount)
	}
	if !reflect.DeepEqual(runner.calls, []string{"capture"}) {
		t.Fatalf("runner.calls = %v, want [capture]", runner.calls)
	}
}

func TestTickReEscalatesResolvedResponderAfterCooldown(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	now := time.Date(2026, 5, 12, 3, 10, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	ctx := context.Background()
	window := OutputWindow{
		RawOutput: `You've hit your usage limit. Upgrade to Pro, purchase more credits or try again later.`,
		Summary:   "You've hit your usage limit.",
	}
	cooldownExpired := now.Add(-time.Second)
	task := seedTask(t, store, TaskRun{
		TaskID:                    "task-recurring",
		TemplateID:                "feature_dev",
		RepositoryID:              "repo_backend",
		MachineID:                 "machine_a",
		Status:                    StatusRunning,
		UserRequest:               "Implement orchestrator",
		CreatedBy:                 "tester",
		RemoteWorkdir:             "/srv/backend",
		TMUXSessionName:           "alterego-task-recurring",
		RemoteCodexSessionID:      "session-recurring",
		LastResolvedResponderName: "usage_limit_prompt",
		LastResolvedScreenDigest:  ScreenDigest(window),
		ResponderCooldownUntil:    &cooldownExpired,
	})
	service.runner.(*fakeServiceRunner).outputWindow = window
	notifier := service.notifier.(*fakeTaskNotifier)
	decider := service.decider.(*fakeDecisionEngine)

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusWaitingUserInput {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusWaitingUserInput)
	}
	if persisted.AwaitingQuestion == nil || persisted.AwaitingQuestion.QuestionType != "usage_limit" {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want usage_limit", persisted.AwaitingQuestion)
	}
	if notifier.lastTaskID != task.TaskID {
		t.Fatalf("notifier.lastTaskID = %q, want %q", notifier.lastTaskID, task.TaskID)
	}
	if decider.callCount != 0 {
		t.Fatalf("decider.callCount = %d, want 0", decider.callCount)
	}
}

func TestRecoverDetachedTaskUsesTMUXSessionWhenPresent(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-detached",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusDetached,
		UserRequest:          "Resume detached task",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-detached",
		RemoteCodexSessionID: "session-detached",
	})
	runner := service.runner.(*fakeServiceRunner)
	runner.hasSession = true

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}

	wantCalls := []string{"has-session"}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestStopMarksTaskStoppedAndCallsRunner(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:               "task-stop",
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               StatusRunning,
		UserRequest:          "Stop me",
		CreatedBy:            "tester",
		RemoteWorkdir:        "/srv/backend",
		TMUXSessionName:      "alterego-task-stop",
		RemoteCodexSessionID: "session-stop",
	})

	if err := service.Stop(ctx, task.TaskID); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusStopped {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusStopped)
	}

	runner := service.runner.(*fakeServiceRunner)
	if len(runner.calls) == 0 || runner.calls[len(runner.calls)-1] != "stop" {
		t.Fatalf("runner calls = %v, want stop", runner.calls)
	}
}

func TestLifecyclePersistsEventsAndQuestions(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task, err := service.StartTask(ctx, "feature_dev", "tester", "Implement orchestrator")
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := service.TickOnce(ctx); err != nil {
			t.Fatalf("TickOnce #%d returned error: %v", i+1, err)
		}
	}

	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `I need clarification on the requirement before I proceed.

Please clarify the requirement before I proceed.`,
		Summary: "Please clarify the requirement before I proceed.",
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:       "ask_user",
		DecisionType: "requirement_clarification",
		Summary:      "Please clarify the requirement before I proceed.",
		Question: &AwaitingQuestion{
			QuestionText:   "Please clarify the requirement before I proceed.",
			OptionsSummary: "",
			ContextExcerpt: "Need more information.",
			QuestionType:   "requirement_clarification",
			AskedAt:        time.Now().UTC(),
		},
	}
	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce for clarification returned error: %v", err)
	}

	if err := service.Reply(ctx, task.TaskID, "Use behavior A."); err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	events, err := store.ListEvents(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("ListEvents returned no events")
	}

	questions, err := store.ListQuestions(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("ListQuestions returned error: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("len(ListQuestions) = %d, want 1", len(questions))
	}
	if questions[0].AnsweredAt == nil || questions[0].AnswerText != "Use behavior A." {
		t.Fatalf("question = %#v, want answered question", questions[0])
	}
}

func newTestService(t *testing.T) (*Service, *Store, func()) {
	t.Helper()

	storePath := filepath.Join(t.TempDir(), "orchestrator.db")
	store, err := OpenStore(storePath)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	registry := &Registry{
		Machines: map[string]*MachineConfig{
			"machine_a": {ID: "machine_a", Host: "host-a", User: "coder"},
			"machine_b": {ID: "machine_b", Host: "host-b", User: "coder"},
		},
		Repositories: map[string]*RepositoryConfig{
			"repo_backend": {
				ID:                  "repo_backend",
				RemoteRepoURL:       "git@github.com:example/backend.git",
				RemoteWorkspaceRoot: "/srv/codex-tasks",
				DefaultBranch:       "main",
				MachineIDs:          []string{"machine_a", "machine_b"},
				PreCloneBootstrap:   []string{"setup-git-auth"},
				PostCloneBootstrap:  []string{"pnpm install"},
			},
		},
		Templates: map[string]*TemplateConfig{
			"feature_dev": {
				ID:                   "feature_dev",
				RepositoryID:         "repo_backend",
				ResolvedWorkflowPath: writeWorkflowFixture(t, "Feature workflow: analyze first\n"),
			},
		},
	}
	registry.Repositories["repo_backend"].Machines = []*MachineConfig{
		registry.Machines["machine_a"],
		registry.Machines["machine_b"],
	}
	registry.Templates["feature_dev"].Repository = registry.Repositories["repo_backend"]

	runner := &fakeServiceRunner{}
	decider := &fakeDecisionEngine{}
	notifier := &fakeTaskNotifier{}
	service := NewService(store, registry, NewScheduler(), runner, decider)
	service.SetNotifier(notifier)

	return service, store, func() { _ = store.Close() }
}

func seedTask(t *testing.T, store *Store, task TaskRun) TaskRun {
	t.Helper()

	if task.TaskID == "" {
		task.TaskID = "task-seed"
	}
	now := time.Now().UTC()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = now
	}

	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}
	return task
}

func writeWorkflowFixture(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "workflow.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write workflow fixture: %v", err)
	}
	return path
}

type fakeServiceRunner struct {
	calls []string

	startSession  RemoteSession
	outputWindow  OutputWindow
	hasSession    bool
	lastSentInput string
	lastSentKey   string
	startErr      error
	captureErr    error
	sendErr       error
	hasSessionErr error
	resumeErr     error
	stopErr       error
}

func (f *fakeServiceRunner) StartInteractiveSession(context.Context, StartRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "start")
	if f.startErr != nil {
		return RemoteSession{}, f.startErr
	}
	return f.startSession, nil
}

func (f *fakeServiceRunner) SendInteractiveInput(_ context.Context, _ RemoteSession, input string) error {
	f.calls = append(f.calls, "send")
	f.lastSentInput = input
	if f.sendErr != nil {
		return f.sendErr
	}
	return nil
}

func (f *fakeServiceRunner) SendInteractiveKey(_ context.Context, _ RemoteSession, key string) error {
	f.calls = append(f.calls, "send-key")
	f.lastSentKey = key
	if f.sendErr != nil {
		return f.sendErr
	}
	return nil
}

func (f *fakeServiceRunner) CaptureOutput(context.Context, RemoteSession) (OutputWindow, error) {
	f.calls = append(f.calls, "capture")
	if f.captureErr != nil {
		return OutputWindow{}, f.captureErr
	}
	return f.outputWindow, nil
}

func (f *fakeServiceRunner) HasSession(context.Context, RemoteSession) (bool, error) {
	f.calls = append(f.calls, "has-session")
	if f.hasSessionErr != nil {
		return false, f.hasSessionErr
	}
	return f.hasSession, nil
}

func (f *fakeServiceRunner) ResumeLastCodexSession(context.Context, RemoteSession) error {
	f.calls = append(f.calls, "resume")
	if f.resumeErr != nil {
		return f.resumeErr
	}
	return nil
}

func (f *fakeServiceRunner) StopSession(context.Context, RemoteSession) error {
	f.calls = append(f.calls, "stop")
	if f.stopErr != nil {
		return f.stopErr
	}
	return nil
}

type fakeDecisionEngine struct {
	result    DecisionResult
	callCount int
}

func (f *fakeDecisionEngine) DecideNextStep(context.Context, DecisionContext) (DecisionResult, error) {
	f.callCount++
	return f.result, nil
}

type fakeTaskNotifier struct {
	lastTaskID string
	lastUserID string
}

func (f *fakeTaskNotifier) NotifyTaskQuestion(ctx context.Context, task TaskRun) error {
	f.lastTaskID = task.TaskID
	f.lastUserID = task.CreatedBy
	return nil
}
