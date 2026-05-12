package orchestrator

import (
	"context"
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

	startSession RemoteSession
	outputWindow OutputWindow
	hasSession   bool
}

func (f *fakeServiceRunner) StartInteractiveSession(context.Context, StartRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "start")
	return f.startSession, nil
}

func (f *fakeServiceRunner) SendInteractiveInput(context.Context, RemoteSession, string) error {
	f.calls = append(f.calls, "send")
	return nil
}

func (f *fakeServiceRunner) CaptureOutput(context.Context, RemoteSession) (OutputWindow, error) {
	f.calls = append(f.calls, "capture")
	return f.outputWindow, nil
}

func (f *fakeServiceRunner) HasSession(context.Context, RemoteSession) (bool, error) {
	f.calls = append(f.calls, "has-session")
	return f.hasSession, nil
}

func (f *fakeServiceRunner) StopSession(context.Context, RemoteSession) error {
	f.calls = append(f.calls, "stop")
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
