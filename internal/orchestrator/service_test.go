package orchestrator

import (
	"context"
	"errors"
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

func TestTickStartsInteractiveSessionAndStoresThreadMetadata(t *testing.T) {
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
		MachineID:    "machine_a",
		Workdir:      "/srv/codex-tasks/task-running/repo",
		ThreadID:     "thread_123",
		ActiveTurnID: "turn_456",
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
	if persisted.RemoteWorkdir != "/srv/codex-tasks/task-running/repo" {
		t.Fatalf("persisted.RemoteWorkdir = %q, want /srv/codex-tasks/task-running/repo", persisted.RemoteWorkdir)
	}
	if persisted.ThreadID != "thread_123" || persisted.ActiveTurnID != "turn_456" {
		t.Fatalf("persisted thread identity = %#v", persisted)
	}
}

func TestTickMarksRunningTaskDetachedWhenCaptureTimesOut(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-timeout",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Investigate timeout",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-timeout",
	})
	service.runner.(*fakeServiceRunner).captureErr = fmt.Errorf("capture app-server output: %w", ErrRemoteCommandTimeout)

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

func TestTickMarksRunningTaskDetachedWhenThreadMissingDuringCapture(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-missing-thread",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Investigate missing thread",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread_123",
	})
	service.runner.(*fakeServiceRunner).captureErr = fmt.Errorf("get app-server thread: %w", ErrAppServerThreadMissing)

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
		TaskID:        "task-running-timeout",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Task one",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-running-timeout",
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
	runner.captureErr = fmt.Errorf("capture app-server output: %w", ErrRemoteCommandTimeout)

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

func TestTickMovesTaskToWaitingUserInputWhenDecisionRequiresUser(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-wait",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-wait",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `I found two implementation approaches.

Which approach should I take for the implementation?
1. Event-driven
2. Polling`,
		Summary: "Two implementation approaches are possible",
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:       DecisionActionAskUser,
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
		TaskID:        "task-phase-promotion",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		Phase:         TaskPhasePlanning,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-phase-promotion",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Ready to start implementation",
		Summary:   "Ready to start implementation",
		SessionState: SessionState{
			ThreadStatus: "running",
		},
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:        DecisionActionReplyToCodex,
		NextPhase:     TaskPhaseExecuting,
		WorkflowStage: WorkflowStageImplementation,
		CodexReply:    "Start coding now.",
		Summary:       "Implementation starts now",
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
	if persisted.WorkflowStage != WorkflowStageImplementation {
		t.Fatalf("persisted.WorkflowStage = %q, want %q", persisted.WorkflowStage, WorkflowStageImplementation)
	}
}

func TestReplyDoesNotPromotePhaseFromExecutionKeywordsWithoutWorkflowStage(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-reply-no-keyword-promotion",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusWaitingUserInput,
		Phase:         TaskPhasePlanning,
		WorkflowStage: WorkflowStagePlanWriting,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread_123",
		AwaitingQuestion: &AwaitingQuestion{
			QuestionText: "Ready to continue?",
			QuestionType: "plan_review",
			AskedAt:      time.Now().UTC().Add(-time.Minute),
		},
	})

	if err := service.Reply(ctx, task.TaskID, "Start with step 1 and implement it now."); err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Phase != TaskPhasePlanning {
		t.Fatalf("persisted.Phase = %q, want %q", persisted.Phase, TaskPhasePlanning)
	}
}

func TestServicePromotesToExecutingOnlyFromImplementationStage(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-phase",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		Phase:         TaskPhasePlanning,
		WorkflowStage: WorkflowStagePlanWriting,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread_123",
	})

	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Spec approved. Writing implementation plan now.",
		Summary:   "Spec approved. Writing implementation plan now.",
		SessionState: SessionState{
			ThreadStatus: "running",
		},
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:        DecisionActionWait,
		NextPhase:     TaskPhasePlanning,
		WorkflowStage: WorkflowStagePlanWriting,
		Summary:       "Codex is writing the implementation plan.",
	}

	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Phase != TaskPhasePlanning {
		t.Fatalf("persisted.Phase = %q, want %q", persisted.Phase, TaskPhasePlanning)
	}
	if persisted.WorkflowStage != WorkflowStagePlanWriting {
		t.Fatalf("persisted.WorkflowStage = %q, want %q", persisted.WorkflowStage, WorkflowStagePlanWriting)
	}
}

func TestTickUsesFixedContinueReplyDuringExecuting(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-executing-continue",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		Phase:         TaskPhaseExecuting,
		WorkflowStage: WorkflowStageImplementation,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-executing-continue",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Waiting for a short continuation",
		Summary:   "Waiting for a short continuation",
		SessionState: SessionState{
			ThreadStatus: "running",
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

func TestTickForcesUserApprovalBeforeReturningExecutingTaskToPlanning(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-phase-regression",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		Phase:         TaskPhaseExecuting,
		WorkflowStage: WorkflowStageImplementation,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-phase-regression",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Need to revisit the plan",
		Summary:   "Need to revisit the plan",
		SessionState: SessionState{
			ThreadStatus: "running",
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
		TaskID:        "task-working-skip",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-working-skip",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Working\nPress Esc to interrupt",
		Summary:   "Working",
		SessionState: SessionState{
			ThreadStatus: "running",
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

func TestTickKeepsTaskRunningWhenDecisionEngineReturnsWait(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-working",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-working",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: "Need operator guidance",
		Summary:   "Inspecting repository state",
		SessionState: SessionState{
			ThreadStatus: "running",
		},
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
		TaskID:        "task-direct-reply",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-direct-reply",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `当前状态有冲突，需要先对齐目标。

你现在要我继续哪一条：
1. 切回真正的 issue #30
2. 继续围绕已经合并的 BlockReduce 做收尾/补充`,
		Summary: "需要对齐目标并等待选择",
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:       DecisionActionReplyToCodex,
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
		TaskID:        "task-complete",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-complete",
			ActiveTurnID: "turn-complete",
	})
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `Implementation finished successfully.

Waiting for next instruction.`,
		Summary: "Implementation finished and waiting for next instruction.",
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:  DecisionActionCompleteTask,
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

func TestReplyResumesWaitingTask(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	askedAt := time.Now().UTC().Add(-time.Minute)
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-reply",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusWaitingUserInput,
		UserRequest:   "Implement orchestrator",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread_123",
		AwaitingQuestion: &AwaitingQuestion{
			QuestionText: "You've hit your usage limit.",
			QuestionType: "usage_limit",
			AskedAt:      askedAt,
		},
	})
	if err := store.AppendQuestion(ctx, TaskQuestion{
		TaskID:         task.TaskID,
		QuestionType:   "usage_limit",
		QuestionText:   "You've hit your usage limit.",
		OptionsSummary: "",
		ContextExcerpt: "",
		AskedAt:        askedAt,
	}); err != nil {
		t.Fatalf("AppendQuestion returned error: %v", err)
	}
	service.runner.(*fakeServiceRunner).sendSession = RemoteSession{
		MachineID:    "machine_a",
		Workdir:      "/srv/backend",
		ThreadID:     "thread_123",
		ActiveTurnID: "turn_999",
	}

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
	if persisted.ActiveTurnID != "turn_999" {
		t.Fatalf("persisted.ActiveTurnID = %q, want %q", persisted.ActiveTurnID, "turn_999")
	}

	questions, err := store.ListQuestions(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("ListQuestions returned error: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("len(ListQuestions) = %d, want 1", len(questions))
	}
	if questions[0].AnsweredAt == nil || questions[0].AnswerText != "Use polling." {
		t.Fatalf("question = %#v, want answered question", questions[0])
	}
}

func TestRecoverDetachedTaskReconnectsByThreadID(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-recover-thread",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusDetached,
		Phase:         TaskPhaseExecuting,
		WorkflowStage: WorkflowStageImplementation,
		UserRequest:   "Resume detached task",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread_123",
			ActiveTurnID: "turn_456",
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
	if persisted.ThreadID != "thread_123" || persisted.ActiveTurnID != "turn_456" {
		t.Fatalf("persisted thread identity = %#v", persisted)
	}
	if !reflect.DeepEqual(runner.calls, []string{"has-session"}) {
		t.Fatalf("runner.calls = %v, want [has-session]", runner.calls)
	}
}

func TestStopMarksTaskStoppedAndCallsRunner(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-stop",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Stop me",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread-stop",
			ActiveTurnID: "turn-stop",
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

func TestStopDoesNotPersistStoppedWhenRunnerStopFails(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	ctx := context.Background()
	task := seedTask(t, store, TaskRun{
		TaskID:        "task-stop-fail",
		TemplateID:    "feature_dev",
		RepositoryID:  "repo_backend",
		MachineID:     "machine_a",
		Status:        StatusRunning,
		UserRequest:   "Stop me",
		CreatedBy:     "tester",
		RemoteWorkdir: "/srv/backend",
		ThreadID: "thread_123",
			ActiveTurnID: "turn_456",
	})
	service.runner.(*fakeServiceRunner).stopErr = errors.New("interrupt app-server turn: denied")

	if err := service.Stop(ctx, task.TaskID); err == nil {
		t.Fatal("Stop returned nil error, want stop failure")
	}

	persisted, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
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

	service.runner.(*fakeServiceRunner).startSession = RemoteSession{
		MachineID:    "machine_b",
		Workdir:      "/srv/codex-tasks/" + task.TaskID + "/repo",
		ThreadID:     "thread-lifecycle",
		ActiveTurnID: "turn-lifecycle",
	}

	for i := 0; i < 3; i++ {
		if err := service.TickOnce(ctx); err != nil {
			t.Fatalf("TickOnce #%d returned error: %v", i+1, err)
		}
	}

	askedAt := time.Now().UTC()
	service.runner.(*fakeServiceRunner).outputWindow = OutputWindow{
		RawOutput: `I need clarification on the requirement before I proceed.

Please clarify the requirement before I proceed.`,
		Summary: "Please clarify the requirement before I proceed.",
	}
	service.decider.(*fakeDecisionEngine).result = DecisionResult{
		Action:       DecisionActionAskUser,
		DecisionType: "requirement_clarification",
		Summary:      "Please clarify the requirement before I proceed.",
		Question: &AwaitingQuestion{
			QuestionText:   "Please clarify the requirement before I proceed.",
			OptionsSummary: "",
			ContextExcerpt: "Need more information.",
			QuestionType:   "requirement_clarification",
			AskedAt:        askedAt,
		},
	}
	if err := service.TickOnce(ctx); err != nil {
		t.Fatalf("TickOnce for clarification returned error: %v", err)
	}

	service.runner.(*fakeServiceRunner).sendSession = RemoteSession{
		MachineID:    "machine_b",
		Workdir:      "/srv/codex-tasks/" + task.TaskID + "/repo",
		ThreadID:     "thread-lifecycle",
		ActiveTurnID: "turn-after-reply",
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
	sendSession   RemoteSession
	outputWindow  OutputWindow
	hasSession    bool
	lastSentInput string
	startErr      error
	captureErr    error
	sendErr       error
	hasSessionErr error
	stopErr       error
}

func (f *fakeServiceRunner) StartInteractiveSession(context.Context, StartRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "start")
	if f.startErr != nil {
		return RemoteSession{}, f.startErr
	}
	if f.startSession == (RemoteSession{}) {
		return RemoteSession{}, nil
	}
	return f.startSession, nil
}

func (f *fakeServiceRunner) SendInteractiveInput(_ context.Context, session RemoteSession, input string) (RemoteSession, error) {
	f.calls = append(f.calls, "send")
	f.lastSentInput = input
	if f.sendErr != nil {
		return RemoteSession{}, f.sendErr
	}
	if f.sendSession == (RemoteSession{}) {
		return session, nil
	}
	return f.sendSession, nil
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

func (f *fakeTaskNotifier) NotifyTaskQuestion(_ context.Context, task TaskRun) error {
	f.lastTaskID = task.TaskID
	f.lastUserID = task.CreatedBy
	return nil
}
