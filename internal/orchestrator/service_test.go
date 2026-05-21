package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStartTaskSelectsMachineAndStartsSession(t *testing.T) {
	t.Parallel()

	service, store, cleanup := newTestService(t)
	defer cleanup()

	seedTask(t, store, TaskRun{
		TaskID:                "existing",
		TemplateID:            "feature_dev",
		RepositoryID:          "repo_backend",
		MachineID:             "machine_a",
		Status:                StatusRunning,
		UserRequest:           "existing work",
		CreatedBy:             "tester",
		CompletionCheckStatus: CompletionCheckStatusNotStarted,
		CreatedAt:             time.Now().UTC().Add(-time.Minute),
		UpdatedAt:             time.Now().UTC().Add(-time.Minute),
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

func TestTickSendsCompletionCheckOnlyOnce(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{Summary: "Task complete.", SessionState: SessionState{ThreadStatus: "completed"}},
	}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{
		completionDecision: SupervisorDecision{
			Classification:        ClassificationCompletionSignal,
			CompletionDisposition: CompletionDispositionSignalComplete,
		},
	})
	defer cleanup()

	task := sampleTaskRun("task-complete-once", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	seedTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 1 {
		t.Fatalf("sentInputs = %#v, want one completion check", runner.sentInputs)
	}

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("second TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 1 {
		t.Fatalf("sentInputs after second tick = %#v, want still one completion check", runner.sentInputs)
	}
}

func TestTickCompletesTaskAfterCompletionCheckConfirmation(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{Summary: "All requested work is complete.", SessionState: SessionState{ThreadStatus: "completed"}},
	}
	service, store, cleanup := newCustomTestService(t, runner, &fakeDecisionEngine{
		completionDecision: SupervisorDecision{
			Classification:        ClassificationCompletionSignal,
			CompletionDisposition: CompletionDispositionConfirmedDone,
		},
	})
	defer cleanup()

	task := sampleTaskRun("task-done", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.RemoteWorkdir = "/srv/backend"
	now := time.Now().UTC().Add(-time.Minute)
	task.CompletionCheckStatus = CompletionCheckStatusSent
	task.CompletionCheckSentAt = &now
	seedTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if persisted.Status != StatusCompleted {
		t.Fatalf("persisted.Status = %q, want %q", persisted.Status, StatusCompleted)
	}
}

func TestTickProgressPollingOnlyNotifiesUserAndNeverRepliesCodex(t *testing.T) {
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
	if task.CompletionCheckStatus == "" {
		task.CompletionCheckStatus = CompletionCheckStatusNotStarted
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

	deletedWorkspaces []DeleteWorkspaceRequest
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
	f.sentInputs = append(f.sentInputs, input)
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

func (f *fakeServiceRunner) DeleteTaskWorkspace(_ context.Context, req DeleteWorkspaceRequest) error {
	f.calls = append(f.calls, "delete-workspace")
	f.deletedWorkspaces = append(f.deletedWorkspaces, req)
	if f.deleteErr != nil {
		return f.deleteErr
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
	completionDecision SupervisorDecision
	err                error
}

func (f *fakeDecisionEngine) ClassifySupervisorEvent(context.Context, SupervisorContext) (SupervisorDecision, error) {
	return f.supervisorDecision, f.err
}

func (f *fakeDecisionEngine) EvaluateProgressUpdate(context.Context, TaskRun, string) (SupervisorDecision, error) {
	return f.progressDecision, f.err
}

func (f *fakeDecisionEngine) EvaluateCompletionSignal(context.Context, TaskRun, string) (SupervisorDecision, error) {
	return f.completionDecision, f.err
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
