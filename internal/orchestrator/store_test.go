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
	task.RemoteCodexSessionID = "codex-session-002"
	task.RemoteProcessIdentity = "pid:2002"
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

	task := sampleTaskRun("task-003", StatusWaitingUserDecision)
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

func TestStoreListsActiveTasksForScheduler(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	tasks := []TaskRun{
		sampleTaskRun("task-pending", StatusPending),
		sampleTaskRun("task-running", StatusRunning),
		sampleTaskRun("task-waiting", StatusWaitingUserDecision),
		sampleTaskRun("task-detached", StatusDetached),
		sampleTaskRun("task-probing", StatusProbing),
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

	wantActiveIDs := []string{"task-pending", "task-running", "task-waiting", "task-detached", "task-probing"}
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
		TaskID:                taskID,
		TemplateID:            "feature_dev",
		RepositoryID:          "repo_backend",
		MachineID:             "machine_a",
		Status:                status,
		UserRequest:           "Implement persisted store",
		CreatedBy:             "user_123",
		RemoteWorkdir:         "",
		RemoteCodexSessionID:  "",
		RemoteProcessIdentity: "",
		LastInput:             "Continue with Task 2",
		LastOutputSummary:     "Store not started yet",
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
	if got.RemoteCodexSessionID != want.RemoteCodexSessionID {
		t.Fatalf("RemoteCodexSessionID = %q, want %q", got.RemoteCodexSessionID, want.RemoteCodexSessionID)
	}
	if got.UserRequest != want.UserRequest {
		t.Fatalf("UserRequest = %q, want %q", got.UserRequest, want.UserRequest)
	}
	if got.CreatedBy != want.CreatedBy {
		t.Fatalf("CreatedBy = %q, want %q", got.CreatedBy, want.CreatedBy)
	}
	if got.RemoteProcessIdentity != want.RemoteProcessIdentity {
		t.Fatalf("RemoteProcessIdentity = %q, want %q", got.RemoteProcessIdentity, want.RemoteProcessIdentity)
	}
	if got.LastInput != want.LastInput {
		t.Fatalf("LastInput = %q, want %q", got.LastInput, want.LastInput)
	}
	if got.LastOutputSummary != want.LastOutputSummary {
		t.Fatalf("LastOutputSummary = %q, want %q", got.LastOutputSummary, want.LastOutputSummary)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("CreatedAt = %s, want %s", got.CreatedAt, want.CreatedAt)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("UpdatedAt = %s, want %s", got.UpdatedAt, want.UpdatedAt)
	}
}
