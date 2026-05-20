package orchestrator

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCreatesTaskAndReloadsIt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "store.db")

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}

	task := sampleTaskRun("task-001", StatusPending)
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	store, err = OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore reload returned error: %v", err)
	}
	defer store.Close()

	got, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}

	assertTaskFields(t, got, task)
	if got.AwaitingQuestion != nil {
		t.Fatalf("AwaitingQuestion = %#v, want nil", got.AwaitingQuestion)
	}
}

func TestStoreUpdatesTaskStatusAndSessionFields(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	task := sampleTaskRun("task-002", StatusPending)
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}

	task.Status = StatusRunning
	task.RemoteWorkdir = "/srv/repos/backend/.codex/task-002"
	task.ThreadID = "thread-update-002"
	task.ActiveTurnID = "turn-update-002"
	task.LastDecisionAction = "wait"
	task.UpdatedAt = task.UpdatedAt.Add(2 * time.Minute)
	if err := store.UpdateTask(ctx, task); err != nil {
		t.Fatalf("UpdateTask returned error: %v", err)
	}

	got, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}

	assertTaskFields(t, got, task)
}

func TestStorePersistsAwaitingQuestion(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	task := sampleTaskRun("task-003", StatusWaitingUserInput)
	task.AwaitingQuestion = &AwaitingQuestion{
		QuestionText:   "Choose implementation approach",
		OptionsSummary: "A: refactor parser; B: add translation layer",
		ContextExcerpt: "Codex found two viable approaches with different migration costs.",
		AskedAt:        time.Date(2026, 5, 11, 10, 5, 0, 0, time.UTC),
	}
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}

	got, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}

	assertTaskFields(t, got, task)
	if got.AwaitingQuestion == nil {
		t.Fatal("AwaitingQuestion = nil, want persisted question")
	}
	if *got.AwaitingQuestion != *task.AwaitingQuestion {
		t.Fatalf("AwaitingQuestion = %#v, want %#v", *got.AwaitingQuestion, *task.AwaitingQuestion)
	}
}

func TestStorePersistsThreadIdentity(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	task := TaskRun{
		TaskID:                "task-appserver",
		TemplateID:            "simt-stl-dev",
		RepositoryID:          "simt-stl",
		MachineID:             "A5-82",
		Status:                StatusRunning,
		ThreadID:              "thread_123",
		ActiveTurnID:          "turn_456",
		CompletionCheckStatus: CompletionCheckStatusNotStarted,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}

	assertTaskFields(t, persisted, task)
	if persisted.AwaitingQuestion != nil {
		t.Fatalf("AwaitingQuestion = %#v, want nil", persisted.AwaitingQuestion)
	}
}

func TestStorePersistsSupervisorControlFields(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	task := TaskRun{
		TaskID:                "task-supervisor",
		TemplateID:            "feature_dev",
		RepositoryID:          "repo_backend",
		MachineID:             "machine_a",
		Status:                StatusRecovering,
		UserRequest:           "continue",
		CreatedBy:             "ou_1",
		ThreadID:              "thread-1",
		ActiveTurnID:          "turn-1",
		LastOutputSummary:     "running tests",
		PendingRequestID:      "req-1",
		CompletionCheckStatus: CompletionCheckStatusSent,
		CompletionCheckSentAt: ptrTime(now.Add(time.Minute)),
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}

	got, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}

	if got.Status != StatusRecovering {
		t.Fatalf("Status = %q, want %q", got.Status, StatusRecovering)
	}
	if got.PendingRequestID != "req-1" {
		t.Fatalf("PendingRequestID = %q, want %q", got.PendingRequestID, "req-1")
	}
	if got.CompletionCheckStatus != CompletionCheckStatusSent {
		t.Fatalf("CompletionCheckStatus = %q, want %q", got.CompletionCheckStatus, CompletionCheckStatusSent)
	}
	if got.CompletionCheckSentAt == nil || !got.CompletionCheckSentAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("CompletionCheckSentAt = %#v, want %s", got.CompletionCheckSentAt, now.Add(time.Minute))
	}
}

func TestStorePersistsTaskServerRequestLifecycle(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	req := TaskServerRequest{
		RequestID:      "req-1",
		TaskID:         "task-1",
		ThreadID:       "thread-1",
		TurnID:         "turn-1",
		RequestType:    ServerRequestTypeUserInput,
		RequestPayload: `{"prompt":"choose"}`,
		Status:         ServerRequestStatusPending,
		CreatedAt:      time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	}

	if err := store.UpsertTaskServerRequest(context.Background(), req); err != nil {
		t.Fatalf("UpsertTaskServerRequest returned error: %v", err)
	}

	if err := store.MarkTaskServerRequestReplying(context.Background(), "req-1", req.CreatedAt.Add(time.Minute)); err != nil {
		t.Fatalf("MarkTaskServerRequestReplying returned error: %v", err)
	}

	if err := store.MarkTaskServerRequestReplied(context.Background(), "req-1", "continue", req.CreatedAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkTaskServerRequestReplied returned error: %v", err)
	}

	got, err := store.GetTaskServerRequest(context.Background(), "req-1")
	if err != nil {
		t.Fatalf("GetTaskServerRequest returned error: %v", err)
	}
	if got.Status != ServerRequestStatusReplied {
		t.Fatalf("Status = %q, want %q", got.Status, ServerRequestStatusReplied)
	}
	if got.ReplyContent != "continue" {
		t.Fatalf("ReplyContent = %q, want %q", got.ReplyContent, "continue")
	}
}

func TestStoreListsActiveTasksForScheduler(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	tasks := []TaskRun{
		sampleTaskRun("task-pending", StatusPending),
		sampleTaskRun("task-running", StatusRunning),
		sampleTaskRun("task-waiting", StatusWaitingUserInput),
		sampleTaskRun("task-recovering", StatusRecovering),
		sampleTaskRun("task-starting", StatusStarting),
		sampleTaskRun("task-completed", StatusCompleted),
		sampleTaskRun("task-failed", StatusFailed),
		sampleTaskRun("task-stopped", StatusStopped),
	}
	for _, task := range tasks {
		if err := store.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask(%q) returned error: %v", task.TaskID, err)
		}
	}

	got, err := store.ListActiveTasks(ctx)
	if err != nil {
		t.Fatalf("ListActiveTasks returned error: %v", err)
	}

	gotByID := map[string]TaskRun{}
	for _, task := range got {
		gotByID[task.TaskID] = task
	}

	wantActiveIDs := []string{"task-pending", "task-running", "task-waiting", "task-recovering", "task-starting"}
	if len(gotByID) != len(wantActiveIDs) {
		t.Fatalf("len(ListActiveTasks) = %d, want %d", len(gotByID), len(wantActiveIDs))
	}
	for _, taskID := range wantActiveIDs {
		task, ok := gotByID[taskID]
		if !ok {
			t.Fatalf("ListActiveTasks missing %q", taskID)
		}
		if task.TaskID != taskID {
			t.Fatalf("TaskID = %q, want %q", task.TaskID, taskID)
		}
	}
	for _, taskID := range []string{"task-completed", "task-failed", "task-stopped"} {
		if _, ok := gotByID[taskID]; ok {
			t.Fatalf("ListActiveTasks unexpectedly included %q", taskID)
		}
	}
}

func TestStoreAppendsAndListsTaskEvents(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	for _, event := range []TaskEvent{
		{TaskID: "task-1", EventType: "task_created", Message: "created", CreatedAt: now},
		{TaskID: "task-1", EventType: "task_started", Message: "started", CreatedAt: now.Add(time.Minute)},
	} {
		if err := store.AppendEvent(ctx, event); err != nil {
			t.Fatalf("AppendEvent returned error: %v", err)
		}
	}

	got, err := store.ListEvents(ctx, "task-1")
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ListEvents) = %d, want 2", len(got))
	}
	if got[0].EventType != "task_created" || got[1].EventType != "task_started" {
		t.Fatalf("events = %#v, want ordered events", got)
	}
}

func TestStorePersistsAndUpdatesTaskQuestions(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	askedAt := time.Date(2026, 5, 11, 11, 0, 0, 0, time.UTC)
	question := TaskQuestion{
		TaskID:         "task-2",
		QuestionType:   "requirement_clarification",
		QuestionText:   "Please clarify the expected behavior.",
		OptionsSummary: "n/a",
		ContextExcerpt: "Need more information to continue.",
		AskedAt:        askedAt,
	}
	if err := store.AppendQuestion(ctx, question); err != nil {
		t.Fatalf("AppendQuestion returned error: %v", err)
	}

	answeredAt := askedAt.Add(5 * time.Minute)
	if err := store.MarkQuestionAnswered(ctx, "task-2", askedAt, answeredAt, "Proceed with behavior A."); err != nil {
		t.Fatalf("MarkQuestionAnswered returned error: %v", err)
	}

	got, err := store.ListQuestions(ctx, "task-2")
	if err != nil {
		t.Fatalf("ListQuestions returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(ListQuestions) = %d, want 1", len(got))
	}
	if got[0].AnswerText != "Proceed with behavior A." {
		t.Fatalf("AnswerText = %q, want %q", got[0].AnswerText, "Proceed with behavior A.")
	}
	if got[0].AnsweredAt == nil || !got[0].AnsweredAt.Equal(answeredAt) {
		t.Fatalf("AnsweredAt = %#v, want %s", got[0].AnsweredAt, answeredAt)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := OpenStore(filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	return store
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func sampleTaskRun(taskID string, status TaskStatus) TaskRun {
	now := time.Date(2026, 5, 11, 9, 30, 0, 0, time.UTC)
	return TaskRun{
		TaskID:                taskID,
		TemplateID:            "feature_dev",
		RepositoryID:          "repo_backend",
		MachineID:             "machine_a",
		Status:                status,
		UserRequest:           "Implement persisted store",
		CreatedBy:             "user_123",
		RemoteWorkdir:         "",
		LastInput:             "Continue with Task 2",
		LastOutputSummary:     "Store not started yet",
		CompletionCheckStatus: CompletionCheckStatusNotStarted,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func assertTaskFields(t *testing.T, got, want TaskRun) {
	t.Helper()

	if got.TaskID != want.TaskID {
		t.Fatalf("TaskID = %q, want %q", got.TaskID, want.TaskID)
	}
	if got.TemplateID != want.TemplateID {
		t.Fatalf("TemplateID = %q, want %q", got.TemplateID, want.TemplateID)
	}
	if got.RepositoryID != want.RepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, want.RepositoryID)
	}
	if got.MachineID != want.MachineID {
		t.Fatalf("MachineID = %q, want %q", got.MachineID, want.MachineID)
	}
	if got.Status != want.Status {
		t.Fatalf("Status = %q, want %q", got.Status, want.Status)
	}
	if got.RemoteWorkdir != want.RemoteWorkdir {
		t.Fatalf("RemoteWorkdir = %q, want %q", got.RemoteWorkdir, want.RemoteWorkdir)
	}
	if got.ThreadID != want.ThreadID {
		t.Fatalf("ThreadID = %q, want %q", got.ThreadID, want.ThreadID)
	}
	if got.ActiveTurnID != want.ActiveTurnID {
		t.Fatalf("ActiveTurnID = %q, want %q", got.ActiveTurnID, want.ActiveTurnID)
	}
	if got.UserRequest != want.UserRequest {
		t.Fatalf("UserRequest = %q, want %q", got.UserRequest, want.UserRequest)
	}
	if got.CreatedBy != want.CreatedBy {
		t.Fatalf("CreatedBy = %q, want %q", got.CreatedBy, want.CreatedBy)
	}
	if got.LastInput != want.LastInput {
		t.Fatalf("LastInput = %q, want %q", got.LastInput, want.LastInput)
	}
	if got.LastOutputSummary != want.LastOutputSummary {
		t.Fatalf("LastOutputSummary = %q, want %q", got.LastOutputSummary, want.LastOutputSummary)
	}
	if got.LastDecisionAction != want.LastDecisionAction {
		t.Fatalf("LastDecisionAction = %q, want %q", got.LastDecisionAction, want.LastDecisionAction)
	}
	if got.PendingRequestID != want.PendingRequestID {
		t.Fatalf("PendingRequestID = %q, want %q", got.PendingRequestID, want.PendingRequestID)
	}
	if got.CompletionCheckStatus != want.CompletionCheckStatus {
		t.Fatalf("CompletionCheckStatus = %q, want %q", got.CompletionCheckStatus, want.CompletionCheckStatus)
	}
	if !timesEqual(got.CompletionCheckSentAt, want.CompletionCheckSentAt) {
		t.Fatalf("CompletionCheckSentAt = %#v, want %#v", got.CompletionCheckSentAt, want.CompletionCheckSentAt)
	}
	if !timesEqual(got.CompletionCheckDoneAt, want.CompletionCheckDoneAt) {
		t.Fatalf("CompletionCheckDoneAt = %#v, want %#v", got.CompletionCheckDoneAt, want.CompletionCheckDoneAt)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("CreatedAt = %s, want %s", got.CreatedAt, want.CreatedAt)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("UpdatedAt = %s, want %s", got.UpdatedAt, want.UpdatedAt)
	}
}

func timesEqual(left, right *time.Time) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.Equal(*right)
	}
}
