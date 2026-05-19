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
	task.WorkflowStage = WorkflowStageImplementation
	task.RemoteWorkdir = "/srv/repos/backend/.codex/task-002"
	task.ThreadID = "thread-update-002"
	task.ActiveTurnID = "turn-update-002"
	task.LastThreadStatus = "running"
	task.LastTurnStatus = "completed"
	task.LastObservedItemID = "item-update-002"
	lastRemoteActivityAt := task.UpdatedAt.Add(15 * time.Second)
	task.LastRemoteActivityAt = &lastRemoteActivityAt
	task.LastDecisionAction = DecisionActionWait
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

func TestStorePersistsAppServerThreadState(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	lastRemoteActivityAt := now.Add(90 * time.Second)
	task := TaskRun{
		TaskID:        "task-appserver",
		TemplateID:    "simt-stl-dev",
		RepositoryID:  "simt-stl",
		MachineID:     "A5-82",
		Status:        StatusRunning,
		Phase:         TaskPhasePlanning,
		WorkflowStage: WorkflowStagePlanWriting,
		AppServerState: AppServerState{
			ThreadID:             "thread_123",
			ActiveTurnID:         "turn_456",
			LastThreadStatus:     "running",
			LastTurnStatus:       "running",
			LastObservedItemID:   "item_789",
			LastRemoteActivityAt: &lastRemoteActivityAt,
		},
		CreatedAt: now,
		UpdatedAt: now,
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

func TestStoreListsActiveTasksForScheduler(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	tasks := []TaskRun{
		sampleTaskRun("task-pending", StatusPending),
		sampleTaskRun("task-running", StatusRunning),
		sampleTaskRun("task-waiting", StatusWaitingUserInput),
		sampleTaskRun("task-detached", StatusDetached),
		sampleTaskRun("task-preparing", StatusPreparingWorkspace),
		sampleTaskRun("task-starting", StatusStartingSession),
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

	wantActiveIDs := []string{"task-pending", "task-running", "task-waiting", "task-detached", "task-preparing", "task-starting"}
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

func sampleTaskRun(taskID string, status TaskStatus) TaskRun {
	now := time.Date(2026, 5, 11, 9, 30, 0, 0, time.UTC)
	return TaskRun{
		TaskID:               taskID,
		TemplateID:           "feature_dev",
		RepositoryID:         "repo_backend",
		MachineID:            "machine_a",
		Status:               status,
		Phase:                TaskPhasePlanning,
		WorkflowStage:        WorkflowStageRequirementDiscussion,
		UserRequest:          "Implement persisted store",
		CreatedBy:            "user_123",
		RemoteWorkdir:        "",
		LastInput:            "Continue with Task 2",
		LastOutputSummary:    "Store not started yet",
		CreatedAt:            now,
		UpdatedAt:            now,
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
	if got.Phase != want.Phase {
		t.Fatalf("Phase = %q, want %q", got.Phase, want.Phase)
	}
	if got.WorkflowStage != want.WorkflowStage {
		t.Fatalf("WorkflowStage = %q, want %q", got.WorkflowStage, want.WorkflowStage)
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
	if got.LastThreadStatus != want.LastThreadStatus {
		t.Fatalf("LastThreadStatus = %q, want %q", got.LastThreadStatus, want.LastThreadStatus)
	}
	if got.LastTurnStatus != want.LastTurnStatus {
		t.Fatalf("LastTurnStatus = %q, want %q", got.LastTurnStatus, want.LastTurnStatus)
	}
	if got.LastObservedItemID != want.LastObservedItemID {
		t.Fatalf("LastObservedItemID = %q, want %q", got.LastObservedItemID, want.LastObservedItemID)
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
	switch {
	case got.LastRemoteActivityAt == nil && want.LastRemoteActivityAt == nil:
	case got.LastRemoteActivityAt == nil || want.LastRemoteActivityAt == nil:
		t.Fatalf("LastRemoteActivityAt = %v, want %v", got.LastRemoteActivityAt, want.LastRemoteActivityAt)
	case !got.LastRemoteActivityAt.Equal(*want.LastRemoteActivityAt):
		t.Fatalf("LastRemoteActivityAt = %s, want %s", got.LastRemoteActivityAt, want.LastRemoteActivityAt)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("CreatedAt = %s, want %s", got.CreatedAt, want.CreatedAt)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("UpdatedAt = %s, want %s", got.UpdatedAt, want.UpdatedAt)
	}
}
