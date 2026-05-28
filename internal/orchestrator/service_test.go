package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/yuqitao1024/alter-ego/internal/codexappserver"
)

func TestStartTaskSelectsMachineAndStartsSession(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

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

	runner := service.runner.(*fakeServiceRunner)
	runner.startSession = RemoteSession{
		MachineID:    "machine_b",
		Workdir:      "/srv/codex-tasks/task-1/repo",
		ThreadID:     "thread-1",
		ActiveTurnID: "turn-1",
	}

	task, err := service.StartTask(context.Background(), "feature_dev", "yuqitao", "Add remote control")
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}
	if task.Status != StatusRunning {
		t.Fatalf("task.Status = %q, want %q", task.Status, StatusRunning)
	}
	if task.MachineID != "machine_b" {
		t.Fatalf("task.MachineID = %q, want machine_b", task.MachineID)
	}
	if task.ThreadID != "thread-1" || task.ActiveTurnID != "turn-1" {
		t.Fatalf("task thread identity = %#v", task)
	}
}

func TestStartTaskUsesBase36MillisecondTaskID(t *testing.T) {
	t.Parallel()

	service, _, cleanup := newTestService(t)
	defer cleanup()
	service.now = func() time.Time {
		return time.Unix(1_234_567, 890_123_456).UTC()
	}

	task, err := service.StartTask(context.Background(), "feature_dev", "yuqitao", "Add remote control")
	if err != nil {
		t.Fatalf("StartTask returned error: %v", err)
	}
	if task.TaskID != "task-kf12oi" {
		t.Fatalf("TaskID = %q, want task-kf12oi", task.TaskID)
	}
}

func TestTickDoesNotReplyToCodexWithoutExplicitPendingRequest(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{
			Summary:      "Need clarification about architecture",
			SessionState: SessionState{ThreadStatus: "running"},
		},
	}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{
		progressDecision: SupervisorDecision{
			Classification:   ClassificationProgressUpdate,
			ShouldNotifyUser: false,
		},
	})
	defer cleanup()

	task := sampleTaskRun("task-no-request", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	seedTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 0 {
		t.Fatalf("sentInputs = %#v, want none", runner.sentInputs)
	}
}

func TestHandleRuntimeEventRepliesOnceForPendingExecutionApprovalRequest(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{
		supervisorDecision: SupervisorDecision{
			Classification:   ClassificationExecutionApproval,
			ShouldReplyCodex: true,
			ReplyPolicy:      ReplyPolicyAutoContinue,
			CodexReply:       "continue",
		},
	})
	defer cleanup()

	task := sampleTaskRun("task-request", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	event := RuntimeEvent{
		ThreadID: "thread-1",
		ServerRequest: &TaskServerRequest{
			RequestID:      "req-1",
			ThreadID:       "thread-1",
			TurnID:         "turn-1",
			RequestType:    ServerRequestTypeUserInput,
			RequestPayload: `{"prompt":"continue?"}`,
		},
	}

	if err := service.HandleRuntimeEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleRuntimeEvent returned error: %v", err)
	}
	if len(runner.serverReplies) != 1 || runner.serverReplies[0] != "continue" {
		t.Fatalf("serverReplies = %#v", runner.serverReplies)
	}

	req, err := store.GetTaskServerRequest(context.Background(), "req-1")
	if err != nil {
		t.Fatalf("GetTaskServerRequest returned error: %v", err)
	}
	if req.Status != ServerRequestStatusReplied {
		t.Fatalf("req.Status = %q, want %q", req.Status, ServerRequestStatusReplied)
	}

	if err := service.HandleRuntimeEvent(context.Background(), event); err != nil {
		t.Fatalf("second HandleRuntimeEvent returned error: %v", err)
	}
	if len(runner.serverReplies) != 1 {
		t.Fatalf("serverReplies after second event = %#v, want one reply", runner.serverReplies)
	}
}

func TestHandleRuntimeEventEscalatesPlanDecisionToUser(t *testing.T) {
	t.Parallel()

	notifier := &fakeTaskNotifier{}
	service, store, cleanup := newCustomTestServiceWithNotifier(t, &fakeServiceRunner{}, &fakeDecisionEngine{
		supervisorDecision: SupervisorDecision{
			Classification: ClassificationPlanDecision,
			ReplyPolicy:    ReplyPolicyAskUser,
			UserQuestion:   "Codex wants a scope decision. Continue with option A or B?",
		},
	}, notifier)
	defer cleanup()

	task := sampleTaskRun("task-plan", StatusRunning)
	task.ThreadID = "thread-plan"
	task.ActiveTurnID = "turn-plan"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	err := service.HandleRuntimeEvent(context.Background(), RuntimeEvent{
		ThreadID: "thread-plan",
		ServerRequest: &TaskServerRequest{
			RequestID:      "req-plan",
			ThreadID:       "thread-plan",
			TurnID:         "turn-plan",
			RequestType:    ServerRequestTypeUserInput,
			RequestPayload: `{"prompt":"A or B?"}`,
		},
	})
	if err != nil {
		t.Fatalf("HandleRuntimeEvent returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusWaitingUserInput {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusWaitingUserInput)
	}
	if persisted.AwaitingQuestion == nil || persisted.AwaitingQuestion.QuestionText == "" {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want question", persisted.AwaitingQuestion)
	}
	if notifier.lastTaskID != task.TaskID {
		t.Fatalf("notifier.lastTaskID = %q, want %q", notifier.lastTaskID, task.TaskID)
	}
}

func TestReplyResumesWaitingTaskAndMarksRequestReplied(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	askedAt := time.Now().UTC().Add(-time.Minute)
	task := sampleTaskRun("task-reply", StatusWaitingUserInput)
	task.ThreadID = "thread-123"
	task.ActiveTurnID = "turn-123"
	task.RemoteWorkdir = "/srv/backend"
	task.PendingRequestID = "req-1"
	task.AwaitingQuestion = &AwaitingQuestion{
		QuestionText: "Continue?",
		QuestionType: "execution_approval",
		AskedAt:      askedAt,
	}
	seedTask(t, store, task)
	if err := store.AppendQuestion(context.Background(), TaskQuestion{
		TaskID:         task.TaskID,
		QuestionType:   "execution_approval",
		QuestionText:   "Continue?",
		OptionsSummary: "",
		ContextExcerpt: "",
		AskedAt:        askedAt,
	}); err != nil {
		t.Fatalf("AppendQuestion returned error: %v", err)
	}
	mustUpsertRequest(t, store, TaskServerRequest{
		RequestID:      "req-1",
		TaskID:         task.TaskID,
		ThreadID:       "thread-123",
		TurnID:         "turn-123",
		RequestType:    ServerRequestTypeUserInput,
		RequestPayload: `{"prompt":"Continue?"}`,
		Status:         ServerRequestStatusPending,
		CreatedAt:      time.Now().UTC().Add(-time.Minute),
	})

	runner := service.runner.(*fakeServiceRunner)
	runner.sendSession = RemoteSession{
		MachineID:    "machine_a",
		Workdir:      "/srv/backend",
		ThreadID:     "thread-123",
		ActiveTurnID: "turn-999",
	}

	if err := service.Reply(context.Background(), task.TaskID, "continue"); err != nil {
		t.Fatalf("Reply returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.AwaitingQuestion != nil {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want nil", persisted.AwaitingQuestion)
	}

	req, err := store.GetTaskServerRequest(context.Background(), "req-1")
	if err != nil {
		t.Fatalf("GetTaskServerRequest returned error: %v", err)
	}
	if req.Status != ServerRequestStatusReplied {
		t.Fatalf("req.Status = %q, want %q", req.Status, ServerRequestStatusReplied)
	}
	if len(runner.serverReplies) != 1 || runner.serverReplies[0] != "continue" {
		t.Fatalf("serverReplies = %#v, want [continue]", runner.serverReplies)
	}
}

func TestStopRejectsNonStoppableStatus(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{})
	defer cleanup()

	task := sampleTaskRun("task-stop", StatusRecovering)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	err := service.Stop(context.Background(), task.TaskID)
	if err == nil {
		t.Fatal("Stop returned nil error")
	}
	if want := `task "task-stop" is recovering and cannot be stopped`; err.Error() != want {
		t.Fatalf("Stop error = %q, want %q", err.Error(), want)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRecovering {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRecovering)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner.calls = %#v, want none", runner.calls)
	}
}

func TestCompleteMarksWaitingTaskCompleted(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{})
	defer cleanup()

	askedAt := time.Now().UTC().Add(-time.Minute)
	task := sampleTaskRun("task-complete", StatusWaitingUserInput)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	task.AwaitingQuestion = &AwaitingQuestion{
		QuestionText: "请确认任务是否完成。",
		QuestionType: "plan_decision",
		AskedAt:      askedAt,
	}
	seedTask(t, store, task)
	if err := store.AppendQuestion(context.Background(), TaskQuestion{
		TaskID:         task.TaskID,
		QuestionType:   "plan_decision",
		QuestionText:   "请确认任务是否完成。",
		OptionsSummary: "",
		ContextExcerpt: "",
		AskedAt:        askedAt,
	}); err != nil {
		t.Fatalf("AppendQuestion returned error: %v", err)
	}

	if err := service.Complete(context.Background(), task.TaskID); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusCompleted {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusCompleted)
	}
	if persisted.AwaitingQuestion != nil {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want nil", persisted.AwaitingQuestion)
	}
	if len(runner.calls) != 1 || runner.calls[0] != "stop" {
		t.Fatalf("runner.calls = %#v, want [stop]", runner.calls)
	}
}

func TestDashboardBuildsRealTaskSnapshot(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	now := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)

	running := sampleTaskRun("task-running", StatusRunning)
	running.TemplateID = "feature_dev"
	running.RepositoryID = "repo_backend"
	running.MachineID = "machine_a"
	running.UserRequest = "Fix websocket reconnect handling"
	running.LastOutputSummary = "Applied reconnect retry logic and running tests."
	running.CreatedAt = now.Add(-10 * time.Minute)
	running.UpdatedAt = now.Add(-2 * time.Minute)
	seedTask(t, store, running)
	if err := store.AppendEvent(context.Background(), TaskEvent{
		TaskID:    running.TaskID,
		EventType: "turn_completed_replied",
		Message:   "continue",
		CreatedAt: now.Add(-90 * time.Second),
	}); err != nil {
		t.Fatalf("AppendEvent(running) returned error: %v", err)
	}

	waiting := sampleTaskRun("task-waiting", StatusWaitingUserInput)
	waiting.TemplateID = "simt-stl-research"
	waiting.RepositoryID = "repo_backend"
	waiting.MachineID = "machine_b"
	waiting.UserRequest = "Compare three paper directions and pick one."
	waiting.LastOutputSummary = "Codex finished the first pass and needs operator direction."
	waiting.CreatedAt = now.Add(-8 * time.Minute)
	waiting.UpdatedAt = now.Add(-time.Minute)
	waiting.AwaitingQuestion = &AwaitingQuestion{
		QuestionText:   "Which research direction should proceed to full report drafting?",
		ContextExcerpt: "Option A has better benchmarks, option B has cleaner implementation cost.",
		QuestionType:   "plan_decision",
		AskedAt:        now.Add(-75 * time.Second),
	}
	seedTask(t, store, waiting)
	if err := store.AppendQuestion(context.Background(), TaskQuestion{
		TaskID:         waiting.TaskID,
		QuestionType:   "plan_decision",
		QuestionText:   waiting.AwaitingQuestion.QuestionText,
		OptionsSummary: "",
		ContextExcerpt: waiting.AwaitingQuestion.ContextExcerpt,
		AskedAt:        waiting.AwaitingQuestion.AskedAt,
	}); err != nil {
		t.Fatalf("AppendQuestion(waiting) returned error: %v", err)
	}
	if err := store.AppendEvent(context.Background(), TaskEvent{
		TaskID:    waiting.TaskID,
		EventType: "waiting_user_input",
		Message:   "waiting for plan_decision",
		CreatedAt: now.Add(-70 * time.Second),
	}); err != nil {
		t.Fatalf("AppendEvent(waiting) returned error: %v", err)
	}

	completed := sampleTaskRun("task-completed", StatusCompleted)
	completed.TemplateID = "feature_dev"
	completed.RepositoryID = "repo_backend"
	completed.MachineID = "machine_a"
	completed.UserRequest = "Ship dashboard phase 1."
	completed.LastOutputSummary = "Feature merged and validated."
	completed.CreatedAt = now.Add(-20 * time.Minute)
	completed.UpdatedAt = now.Add(-15 * time.Minute)
	seedTask(t, store, completed)

	snapshot, err := service.Dashboard(context.Background())
	if err != nil {
		t.Fatalf("Dashboard returned error: %v", err)
	}

	if snapshot.Summary.Total != 3 {
		t.Fatalf("Summary.Total = %d, want 3", snapshot.Summary.Total)
	}
	if snapshot.Summary.Running != 1 {
		t.Fatalf("Summary.Running = %d, want 1", snapshot.Summary.Running)
	}
	if snapshot.Summary.WaitingUserInput != 1 {
		t.Fatalf("Summary.WaitingUserInput = %d, want 1", snapshot.Summary.WaitingUserInput)
	}
	if snapshot.Summary.Completed != 1 {
		t.Fatalf("Summary.Completed = %d, want 1", snapshot.Summary.Completed)
	}
	if len(snapshot.Tasks) != 3 {
		t.Fatalf("len(Tasks) = %d, want 3", len(snapshot.Tasks))
	}

	if snapshot.Tasks[0].ID != waiting.TaskID {
		t.Fatalf("Tasks[0].ID = %q, want %q", snapshot.Tasks[0].ID, waiting.TaskID)
	}
	if snapshot.Tasks[0].Title != waiting.UserRequest {
		t.Fatalf("Tasks[0].Title = %q, want %q", snapshot.Tasks[0].Title, waiting.UserRequest)
	}
	if snapshot.Tasks[0].AwaitingQuestion == nil {
		t.Fatal("Tasks[0].AwaitingQuestion = nil, want question")
	}
	if snapshot.Tasks[0].AwaitingQuestion.QuestionText != waiting.AwaitingQuestion.QuestionText {
		t.Fatalf("Tasks[0].AwaitingQuestion.QuestionText = %q, want %q", snapshot.Tasks[0].AwaitingQuestion.QuestionText, waiting.AwaitingQuestion.QuestionText)
	}
	if len(snapshot.Tasks[0].RecentEvents) != 1 {
		t.Fatalf("len(Tasks[0].RecentEvents) = %d, want 1", len(snapshot.Tasks[0].RecentEvents))
	}
	if snapshot.Tasks[0].RecentEvents[0].EventType != "waiting_user_input" {
		t.Fatalf("Tasks[0].RecentEvents[0].EventType = %q, want waiting_user_input", snapshot.Tasks[0].RecentEvents[0].EventType)
	}

	if snapshot.Tasks[1].ID != running.TaskID {
		t.Fatalf("Tasks[1].ID = %q, want %q", snapshot.Tasks[1].ID, running.TaskID)
	}
	if snapshot.Tasks[1].Summary != running.LastOutputSummary {
		t.Fatalf("Tasks[1].Summary = %q, want %q", snapshot.Tasks[1].Summary, running.LastOutputSummary)
	}
	if len(snapshot.Tasks[1].RecentEvents) != 1 {
		t.Fatalf("len(Tasks[1].RecentEvents) = %d, want 1", len(snapshot.Tasks[1].RecentEvents))
	}
	if snapshot.Tasks[1].RecentEvents[0].Message != "continue" {
		t.Fatalf("Tasks[1].RecentEvents[0].Message = %q, want continue", snapshot.Tasks[1].RecentEvents[0].Message)
	}
}

func TestCompleteIgnoresStopErrorsAndStillMarksTaskCompleted(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{stopErr: errors.New("interrupt failed")}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{})
	defer cleanup()

	askedAt := time.Now().UTC().Add(-time.Minute)
	task := sampleTaskRun("task-complete-ignore-stop-error", StatusWaitingUserInput)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	task.AwaitingQuestion = &AwaitingQuestion{
		QuestionText: "请确认任务是否完成。",
		QuestionType: "plan_decision",
		AskedAt:      askedAt,
	}
	seedTask(t, store, task)
	if err := store.AppendQuestion(context.Background(), TaskQuestion{
		TaskID:         task.TaskID,
		QuestionType:   "plan_decision",
		QuestionText:   "请确认任务是否完成。",
		OptionsSummary: "",
		ContextExcerpt: "",
		AskedAt:        askedAt,
	}); err != nil {
		t.Fatalf("AppendQuestion returned error: %v", err)
	}

	if err := service.Complete(context.Background(), task.TaskID); err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusCompleted {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusCompleted)
	}

	events, err := store.ListEvents(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("events = %#v, want interrupt skipped and completed events", events)
	}
	if events[len(events)-2].EventType != "task_complete_interrupt_skipped" {
		t.Fatalf("events[len-2].EventType = %q, want task_complete_interrupt_skipped", events[len(events)-2].EventType)
	}
	if events[len(events)-1].EventType != "task_completed" {
		t.Fatalf("events[len-1].EventType = %q, want task_completed", events[len(events)-1].EventType)
	}
}

func TestReopenStoppedTaskSendsNewInputOnSameThread(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{})
	defer cleanup()

	askedAt := time.Now().UTC().Add(-2 * time.Minute)
	task := sampleTaskRun("task-reopen", StatusStopped)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	task.PendingRequestID = "req-1"
	task.AwaitingQuestion = &AwaitingQuestion{
		QuestionText: "Need your input.",
		QuestionType: "plan_decision",
		AskedAt:      askedAt,
	}
	seedTask(t, store, task)
	mustUpsertRequest(t, store, TaskServerRequest{
		RequestID:      "req-1",
		TaskID:         task.TaskID,
		ThreadID:       "thread-1",
		TurnID:         "turn-1",
		RequestType:    ServerRequestTypeUserInput,
		RequestPayload: `{"prompt":"choose"}`,
		Status:         ServerRequestStatusPending,
		CreatedAt:      time.Now().UTC().Add(-time.Minute),
	})

	runner.sendSession = RemoteSession{
		MachineID:    "machine_a",
		Workdir:      "/srv/backend",
		ThreadID:     "thread-1",
		ActiveTurnID: "turn-2",
	}

	if err := service.Reopen(context.Background(), task.TaskID, "Resolve the git conflict, rerun tests, and report the result."); err != nil {
		t.Fatalf("Reopen returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.TaskID != task.TaskID {
		t.Fatalf("persisted.TaskID = %q, want %q", persisted.TaskID, task.TaskID)
	}
	if persisted.ThreadID != "thread-1" {
		t.Fatalf("persisted.ThreadID = %q, want thread-1", persisted.ThreadID)
	}
	if persisted.ActiveTurnID != "turn-2" {
		t.Fatalf("persisted.ActiveTurnID = %q, want turn-2", persisted.ActiveTurnID)
	}
	if persisted.LastInput != "Resolve the git conflict, rerun tests, and report the result." {
		t.Fatalf("persisted.LastInput = %q", persisted.LastInput)
	}
	if persisted.PendingRequestID != "" {
		t.Fatalf("persisted.PendingRequestID = %q, want empty", persisted.PendingRequestID)
	}
	if persisted.AwaitingQuestion != nil {
		t.Fatalf("persisted.AwaitingQuestion = %#v, want nil", persisted.AwaitingQuestion)
	}

	if len(runner.sentInputs) != 1 || runner.sentInputs[0] != "Resolve the git conflict, rerun tests, and report the result." {
		t.Fatalf("runner.sentInputs = %#v", runner.sentInputs)
	}

	req, err := store.GetTaskServerRequest(context.Background(), "req-1")
	if err != nil {
		t.Fatalf("GetTaskServerRequest returned error: %v", err)
	}
	if req.Status != ServerRequestStatusResolved {
		t.Fatalf("req.Status = %q, want %q", req.Status, ServerRequestStatusResolved)
	}

	events, err := store.ListEvents(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if len(events) == 0 || events[len(events)-1].EventType != "task_reopened" {
		t.Fatalf("last event = %#v, want task_reopened", events)
	}
}

func TestReopenRejectsNonTerminalTask(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	task := sampleTaskRun("task-reopen-invalid", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	err := service.Reopen(context.Background(), task.TaskID, "Do one more thing.")
	if err == nil {
		t.Fatal("Reopen returned nil error")
	}
	if want := `task "task-reopen-invalid" is running and cannot be reopened`; err.Error() != want {
		t.Fatalf("Reopen error = %q, want %q", err.Error(), want)
	}
}

func TestHandleRuntimeEventEscalatesPlanDecisionOnCompletedTurnToUser(t *testing.T) {
	t.Parallel()

	notifier := &fakeTaskNotifier{}
	service, store, cleanup := newCustomTestServiceWithNotifier(t, &fakeServiceRunner{}, &fakeDecisionEngine{
		supervisorDecision: SupervisorDecision{
			Classification: ClassificationPlanDecision,
			ReplyPolicy:    ReplyPolicyAskUser,
			UserQuestion:   "Choose A or B",
		},
	}, notifier)
	defer cleanup()

	task := sampleTaskRun("task-turn-plan", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	if err := service.HandleRuntimeEvent(context.Background(), RuntimeEvent{
		ThreadID: "thread-1",
		TurnCompleted: &TurnCompletedEvent{
			TurnID:   "turn-1",
			Summary:  "先确认范围：A 还是 B？",
			ThreadID: "thread-1",
		},
	}); err != nil {
		t.Fatalf("HandleRuntimeEvent returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusWaitingUserInput {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusWaitingUserInput)
	}
	if persisted.AwaitingQuestion == nil {
		t.Fatal("persisted.AwaitingQuestion = nil, want question")
	}
	if notifier.lastTaskID != task.TaskID {
		t.Fatalf("notifier.lastTaskID = %q, want %q", notifier.lastTaskID, task.TaskID)
	}
}

func TestTickProgressPollingSkipsUserNotifyByDefault(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{Summary: "Completed migration and all tests passed.", SessionState: SessionState{ThreadStatus: "running"}},
	}
	notifier := &fakeTaskNotifier{}
	service, store, cleanup := newCustomTestServiceWithNotifier(t, runner, &fakeDecisionEngine{
		progressDecision: SupervisorDecision{
			Classification:   ClassificationProgressUpdate,
			ShouldNotifyUser: true,
			UserUpdate:       "Codex completed migration and passed tests.",
		},
	}, notifier)
	defer cleanup()

	task := sampleTaskRun("task-progress", StatusRunning)
	task.ThreadID = "thread-1"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 0 {
		t.Fatalf("sentInputs = %#v, want none", runner.sentInputs)
	}
	if len(notifier.progressMessages) != 0 {
		t.Fatalf("progressMessages = %#v, want none by default", notifier.progressMessages)
	}
}

func TestTickProgressPollingNotifiesUserWhenEnabled(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{Summary: "Completed migration and all tests passed.", SessionState: SessionState{ThreadStatus: "running"}},
	}
	notifier := &fakeTaskNotifier{}
	service, store, cleanup := newCustomTestServiceWithNotifier(t, runner, &fakeDecisionEngine{
		progressDecision: SupervisorDecision{
			Classification:   ClassificationProgressUpdate,
			ShouldNotifyUser: true,
			UserUpdate:       "Codex completed migration and passed tests.",
		},
	}, notifier)
	defer cleanup()
	service.SetProgressReportsEnabled(true)

	task := sampleTaskRun("task-progress-enabled", StatusRunning)
	task.ThreadID = "thread-1"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 0 {
		t.Fatalf("sentInputs = %#v, want none", runner.sentInputs)
	}
	if len(notifier.progressMessages) != 1 {
		t.Fatalf("progressMessages = %#v, want one user update", notifier.progressMessages)
	}
}

func TestTickSkipsDecisionEvaluationWhenSummaryIsEmpty(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{Summary: "", SessionState: SessionState{ThreadStatus: "running"}},
	}
	notifier := &fakeTaskNotifier{}
	service, store, cleanup := newCustomTestServiceWithNotifier(t, runner, &fakeDecisionEngine{
		err: context.DeadlineExceeded,
	}, notifier)
	defer cleanup()

	task := sampleTaskRun("task-empty-summary", StatusRunning)
	task.ThreadID = "thread-1"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}
	if len(notifier.progressMessages) != 0 {
		t.Fatalf("progressMessages = %#v, want none", notifier.progressMessages)
	}
	if len(runner.sentInputs) != 0 {
		t.Fatalf("sentInputs = %#v, want none", runner.sentInputs)
	}
}

func TestHandleRuntimeEventTurnCompletedSkipsProgressNotifyByDefault(t *testing.T) {
	t.Parallel()

	notifier := &fakeTaskNotifier{}
	service, store, cleanup := newCustomTestServiceWithNotifier(t, &fakeServiceRunner{}, &fakeDecisionEngine{
		supervisorDecision: SupervisorDecision{
			Classification:   ClassificationProgressUpdate,
			ShouldNotifyUser: true,
			UserUpdate:       "made progress",
		},
	}, notifier)
	defer cleanup()

	task := sampleTaskRun("task-turn-progress", StatusRunning)
	task.ThreadID = "thread-progress"
	task.ActiveTurnID = "turn-old"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	err := service.HandleRuntimeEvent(context.Background(), RuntimeEvent{
		ThreadID: "thread-progress",
		TurnCompleted: &TurnCompletedEvent{
			ThreadID: "thread-progress",
			TurnID:   "turn-new",
			Summary:  "Completed another chunk of work.",
		},
	})
	if err != nil {
		t.Fatalf("HandleRuntimeEvent returned error: %v", err)
	}
	if len(notifier.progressMessages) != 0 {
		t.Fatalf("progressMessages = %#v, want none by default", notifier.progressMessages)
	}
}

func TestTickCompletesTaskWhenCodexThreadCompletedWithoutNewSummary(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{Summary: "", SessionState: SessionState{ThreadStatus: "completed"}},
	}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{})
	defer cleanup()

	task := sampleTaskRun("task-thread-complete", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	task.LastOutputSummary = "先按研究工作流梳理"
	seedTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if persisted.LastOutputSummary != "先按研究工作流梳理" {
		t.Fatalf("persisted.LastOutputSummary = %q", persisted.LastOutputSummary)
	}
	if len(runner.sentInputs) != 0 {
		t.Fatalf("sentInputs = %#v, want none", runner.sentInputs)
	}
}

func TestResumeActiveTasksReconnectsRunningTaskImmediately(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	task := sampleTaskRun("task-resume-running", StatusRunning)
	task.RemoteWorkdir = "/srv/backend"
	task.ThreadID = "thread-123"
	task.ActiveTurnID = "turn-456"
	seedTask(t, store, task)

	runner := service.runner.(*fakeServiceRunner)
	runner.hasSession = true

	if err := service.ResumeActiveTasks(context.Background()); err != nil {
		t.Fatalf("ResumeActiveTasks returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if !reflect.DeepEqual(runner.calls, []string{"has-session"}) {
		t.Fatalf("runner.calls = %v, want [has-session]", runner.calls)
	}
}

func TestResumeActiveTasksRestoresUnprocessedCompletedTurn(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		snapshot: codexappserver.ThreadSnapshot{
			ThreadID:          "thread-restore",
			ActiveTurnID:      "turn-restore",
			ActiveTurnStatus:  "completed",
			LatestSummary:     "Task complete.",
			ThreadStatus:      "active",
			SubscriptionState: codexappserver.SubscriptionStateActive,
		},
	}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{
		supervisorDecision: SupervisorDecision{
			Classification:   ClassificationExecutionApproval,
			ReplyPolicy:      ReplyPolicyAutoContinue,
			ShouldReplyCodex: true,
			CodexReply:       "continue",
		},
	})
	defer cleanup()

	task := sampleTaskRun("task-restore-completed", StatusRunning)
	task.ThreadID = "thread-restore"
	task.ActiveTurnID = "turn-restore"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	runner.hasSession = true

	if err := service.ResumeActiveTasks(context.Background()); err != nil {
		t.Fatalf("ResumeActiveTasks returned error: %v", err)
	}
	if len(runner.sentInputs) != 1 {
		t.Fatalf("sentInputs = %#v, want one continue reply", runner.sentInputs)
	}
	if runner.sentInputs[0] != "continue" {
		t.Fatalf("sentInputs[0] = %q, want continue", runner.sentInputs[0])
	}
}

func TestResumeActiveTasksRehandlesPersistedPendingRequest(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{
		supervisorDecision: SupervisorDecision{
			Classification:   ClassificationExecutionApproval,
			ShouldReplyCodex: true,
			ReplyPolicy:      ReplyPolicyAutoContinue,
			CodexReply:       "continue",
		},
	})
	defer cleanup()

	task := sampleTaskRun("task-persisted-request", StatusRunning)
	task.RemoteWorkdir = "/srv/backend"
	task.ThreadID = "thread-123"
	task.ActiveTurnID = "turn-456"
	task.PendingRequestID = "req-1"
	seedTask(t, store, task)
	mustUpsertRequest(t, store, TaskServerRequest{
		RequestID:      "req-1",
		TaskID:         task.TaskID,
		ThreadID:       "thread-123",
		TurnID:         "turn-456",
		RequestType:    ServerRequestTypeUserInput,
		RequestPayload: `{"prompt":"Continue?"}`,
		Status:         ServerRequestStatusPending,
		CreatedAt:      time.Now().UTC().Add(-time.Minute),
	})

	runner.hasSession = true

	if err := service.ResumeActiveTasks(context.Background()); err != nil {
		t.Fatalf("ResumeActiveTasks returned error: %v", err)
	}

	req, err := store.GetTaskServerRequest(context.Background(), "req-1")
	if err != nil {
		t.Fatalf("GetTaskServerRequest returned error: %v", err)
	}
	if req.Status != ServerRequestStatusReplied {
		t.Fatalf("req.Status = %q, want %q", req.Status, ServerRequestStatusReplied)
	}
	if len(runner.serverReplies) != 1 || runner.serverReplies[0] != "continue" {
		t.Fatalf("serverReplies = %#v, want [continue]", runner.serverReplies)
	}
}

func TestResumeActiveTasksRenotifiesExistingWaitingQuestion(t *testing.T) {
	t.Parallel()

	notifier := &fakeTaskNotifier{}
	service, store, cleanup := newCustomTestServiceWithNotifier(t, &fakeServiceRunner{}, &fakeDecisionEngine{}, notifier)
	defer cleanup()

	task := sampleTaskRun("task-waiting-existing", StatusWaitingUserInput)
	task.RemoteWorkdir = "/srv/backend"
	task.ThreadID = "thread-123"
	task.ActiveTurnID = "turn-456"
	task.AwaitingQuestion = &AwaitingQuestion{
		QuestionText:   "Codex reports there is remaining work and needs another decision.",
		ContextExcerpt: "2) remaining work still exists.",
		QuestionType:   "plan_decision",
		AskedAt:        time.Now().UTC().Add(-time.Minute),
	}
	seedTask(t, store, task)

	runner := service.runner.(*fakeServiceRunner)
	runner.hasSession = true

	if err := service.ResumeActiveTasks(context.Background()); err != nil {
		t.Fatalf("ResumeActiveTasks returned error: %v", err)
	}

	if notifier.lastTaskID != task.TaskID {
		t.Fatalf("notifier.lastTaskID = %q, want %q", notifier.lastTaskID, task.TaskID)
	}
}

func TestTickTransitionsRecoveredWaitingThreadToWaitingUserInput(t *testing.T) {
	t.Parallel()

	notifier := &fakeTaskNotifier{}
	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{
			Summary: "Need one more input to continue.",
			SessionState: SessionState{
				ThreadStatus:      "active",
				ThreadActiveFlags: []string{"waitingOnUserInput"},
			},
		},
	}
	service, store, cleanup := newCustomTestServiceWithNotifier(t, runner, &fakeDecisionEngine{}, notifier)
	defer cleanup()

	task := sampleTaskRun("task-waiting-flags", StatusRunning)
	task.ThreadID = "thread-1"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusWaitingUserInput {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusWaitingUserInput)
	}
	if persisted.AwaitingQuestion == nil {
		t.Fatal("persisted.AwaitingQuestion = nil, want question")
	}
	if notifier.lastTaskID != task.TaskID {
		t.Fatalf("notifier.lastTaskID = %q, want %q", notifier.lastTaskID, task.TaskID)
	}
}

func TestRecoveringTaskReconnectsByThreadID(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	task := sampleTaskRun("task-recover-thread", StatusRecovering)
	task.UserRequest = "Resume recovering task"
	task.RemoteWorkdir = "/srv/backend"
	task.ThreadID = "thread-123"
	task.ActiveTurnID = "turn-456"
	seedTask(t, store, task)

	runner := service.runner.(*fakeServiceRunner)
	runner.hasSession = true

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusRunning {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusRunning)
	}
	if !reflect.DeepEqual(runner.calls, []string{"has-session"}) {
		t.Fatalf("runner.calls = %v, want [has-session]", runner.calls)
	}
}

func TestRecoveringTaskFailsWhenThreadIsMissing(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	task := sampleTaskRun("task-missing-thread", StatusRecovering)
	task.UserRequest = "Resume recovering task"
	task.RemoteWorkdir = "/srv/backend"
	task.ThreadID = "thread-missing"
	task.ActiveTurnID = "turn-456"
	seedTask(t, store, task)

	runner := service.runner.(*fakeServiceRunner)
	runner.hasSession = false

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusFailed {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusFailed)
	}
	if !reflect.DeepEqual(runner.calls, []string{"has-session"}) {
		t.Fatalf("runner.calls = %v, want [has-session]", runner.calls)
	}

	events, err := store.ListEvents(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("events = 0, want failure event")
	}
	last := events[len(events)-1]
	if last.EventType != "task_failed" {
		t.Fatalf("last.EventType = %q, want task_failed", last.EventType)
	}
	if last.Message != "codex thread is missing from app-server state; task marked failed for restart" {
		t.Fatalf("last.Message = %q", last.Message)
	}
}

func TestDeleteRejectsActiveTask(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	task := sampleTaskRun("task-delete-active", StatusRunning)
	seedTask(t, store, task)

	if err := service.Delete(context.Background(), task.TaskID); err == nil {
		t.Fatal("Delete returned nil error, want rejection")
	}
}

func TestDeleteRemovesStoppedTask(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	task := sampleTaskRun("task-delete-stopped", StatusStopped)
	task.ThreadID = "thread-delete"
	task.MachineID = "machine_a"
	seedTask(t, store, task)

	if err := service.Delete(context.Background(), task.TaskID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := store.GetTask(context.Background(), task.TaskID); err == nil {
		t.Fatal("GetTask returned nil error after delete, want not found")
	}
	runner := service.runner.(*fakeServiceRunner)
	if len(runner.deletedWorkspaces) != 1 {
		t.Fatalf("deletedWorkspaces = %#v, want one", runner.deletedWorkspaces)
	}
	if runner.deletedWorkspaces[0].TaskID != task.TaskID {
		t.Fatalf("deleted workspace task = %q, want %q", runner.deletedWorkspaces[0].TaskID, task.TaskID)
	}
	if runner.deletedWorkspaces[0].RemoteWorkspaceRoot != "/srv/codex-tasks" {
		t.Fatalf("RemoteWorkspaceRoot = %q, want /srv/codex-tasks", runner.deletedWorkspaces[0].RemoteWorkspaceRoot)
	}
	if len(runner.cleanedSessions) != 1 {
		t.Fatalf("cleanedSessions = %#v, want one", runner.cleanedSessions)
	}
	if runner.cleanedSessions[0].ThreadID != "thread-delete" {
		t.Fatalf("cleaned session thread = %q, want thread-delete", runner.cleanedSessions[0].ThreadID)
	}
}

func TestDeleteTerminalTasksDeletesOnlyTerminalTasks(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	tasks := []TaskRun{
		sampleTaskRun("task-running", StatusRunning),
		sampleTaskRun("task-completed", StatusCompleted),
		sampleTaskRun("task-failed", StatusFailed),
		sampleTaskRun("task-stopped", StatusStopped),
	}
	for _, task := range tasks {
		seedTask(t, store, task)
	}

	deleted, err := service.DeleteTerminalTasks(context.Background())
	if err != nil {
		t.Fatalf("DeleteTerminalTasks returned error: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deleted)
	}
	if _, err := store.GetTask(context.Background(), "task-running"); err != nil {
		t.Fatalf("running task was deleted: %v", err)
	}
	for _, taskID := range []string{"task-completed", "task-failed", "task-stopped"} {
		if _, err := store.GetTask(context.Background(), taskID); err == nil {
			t.Fatalf("GetTask(%q) returned nil error after delete, want not found", taskID)
		}
	}
	runner := service.runner.(*fakeServiceRunner)
	if len(runner.deletedWorkspaces) != 3 {
		t.Fatalf("deletedWorkspaces = %#v, want 3", runner.deletedWorkspaces)
	}
}

func newTestService(t *testing.T) (*Service, *Store, func()) {
	t.Helper()
	return newCustomTestServiceWithNotifier(t, &fakeServiceRunner{}, &fakeDecisionEngine{}, &fakeTaskNotifier{})
}

func newCustomTestService(t *testing.T, runner *fakeServiceRunner, decider *fakeDecisionEngine) (*Service, *Store, func()) {
	t.Helper()
	return newCustomTestServiceWithNotifier(t, runner, decider, &fakeTaskNotifier{})
}

func newCustomTestServiceWithNotifier(t *testing.T, runner *fakeServiceRunner, decider *fakeDecisionEngine, notifier *fakeTaskNotifier) (*Service, *Store, func()) {
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

func mustUpsertRequest(t *testing.T, store *Store, req TaskServerRequest) {
	t.Helper()
	if err := store.UpsertTaskServerRequest(context.Background(), req); err != nil {
		t.Fatalf("UpsertTaskServerRequest returned error: %v", err)
	}
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
	sendSession  RemoteSession
	outputWindow OutputWindow
	snapshot     codexappserver.ThreadSnapshot
	hasSession   bool
	eventCh      chan RuntimeEvent

	sentInputs    []string
	serverReplies []string
	startErr      error
	captureErr    error
	sendErr       error
	hasSessionErr error
	stopErr       error
	deleteErr     error
	cleanupErr    error

	deletedWorkspaces []DeleteWorkspaceRequest
	cleanedSessions   []RemoteSession
}

func (f *fakeServiceRunner) StartInteractiveSession(context.Context, StartRequest) (RemoteSession, error) {
	f.calls = append(f.calls, "start")
	if f.startErr != nil {
		return RemoteSession{}, f.startErr
	}
	if f.startSession.MachineID == "" && f.startSession.Workdir == "" && f.startSession.ThreadID == "" && f.startSession.ActiveTurnID == "" {
		return RemoteSession{}, nil
	}
	return f.startSession, nil
}

func (f *fakeServiceRunner) SendInteractiveInput(_ context.Context, session RemoteSession, input string) (RemoteSession, error) {
	f.calls = append(f.calls, "send")
	f.sentInputs = append(f.sentInputs, input)
	if f.sendErr != nil {
		return RemoteSession{}, f.sendErr
	}
	if f.sendSession.MachineID == "" && f.sendSession.Workdir == "" && f.sendSession.ThreadID == "" && f.sendSession.ActiveTurnID == "" {
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

func (f *fakeServiceRunner) Snapshot(string, string) (codexappserver.ThreadSnapshot, bool) {
	if f.snapshot.ThreadID == "" {
		return codexappserver.ThreadSnapshot{}, false
	}
	return f.snapshot, true
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

func (f *fakeServiceRunner) DeleteTaskWorkspace(_ context.Context, req DeleteWorkspaceRequest) error {
	f.calls = append(f.calls, "delete-workspace")
	f.deletedWorkspaces = append(f.deletedWorkspaces, req)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return nil
}

func (f *fakeServiceRunner) CleanupSession(_ context.Context, session RemoteSession) error {
	f.calls = append(f.calls, "cleanup-session")
	f.cleanedSessions = append(f.cleanedSessions, session)
	if f.cleanupErr != nil {
		return f.cleanupErr
	}
	return nil
}

func (f *fakeServiceRunner) RespondToServerRequest(_ context.Context, _ RemoteSession, _ TaskServerRequest, response string) error {
	f.calls = append(f.calls, "respond")
	f.serverReplies = append(f.serverReplies, response)
	return nil
}

func (f *fakeServiceRunner) Events() <-chan RuntimeEvent {
	if f.eventCh == nil {
		f.eventCh = make(chan RuntimeEvent)
	}
	return f.eventCh
}

type fakeDecisionEngine struct {
	supervisorDecision SupervisorDecision
	progressDecision   SupervisorDecision
	err                error
}

func (f *fakeDecisionEngine) ClassifySupervisorEvent(context.Context, SupervisorContext) (SupervisorDecision, error) {
	return f.supervisorDecision, f.err
}

func (f *fakeDecisionEngine) EvaluateProgressUpdate(context.Context, TaskRun, string) (SupervisorDecision, error) {
	return f.progressDecision, f.err
}

type fakeTaskNotifier struct {
	lastTaskID       string
	progressMessages []string
}

func (f *fakeTaskNotifier) NotifyTaskQuestion(_ context.Context, task TaskRun) error {
	f.lastTaskID = task.TaskID
	return nil
}

func (f *fakeTaskNotifier) NotifyTaskProgress(_ context.Context, _ TaskRun, message string) error {
	f.progressMessages = append(f.progressMessages, message)
	return nil
}
