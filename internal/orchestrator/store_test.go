package orchestrator

import (
	"context"
	"database/sql"
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
	task.TMUXSessionName = "alterego-task-002"
	task.RemoteCodexSessionID = "codex-session-002"
	task.ThreadID = "thread-update-002"
	task.ActiveTurnID = "turn-update-002"
	task.LastThreadStatus = "running"
	task.LastTurnStatus = "completed"
	task.LastObservedItemID = "item-update-002"
	lastRemoteActivityAt := task.UpdatedAt.Add(15 * time.Second)
	task.LastRemoteActivityAt = &lastRemoteActivityAt
	task.LastScreenDigest = "digest:2002"
	task.ActiveResponderName = "usage_limit_prompt"
	task.ActiveResponderScreenDigest = "digest:active"
	task.LastResolvedResponderName = "trust_directory_prompt"
	task.LastResolvedScreenDigest = "digest:resolved"
	cooldown := task.UpdatedAt.Add(30 * time.Second)
	task.ResponderCooldownUntil = &cooldown
	task.LastDecisionScreenDigest = "digest:decision"
	task.LastDecisionAction = DecisionActionWait
	decisionCooldown := task.UpdatedAt.Add(45 * time.Second)
	task.DecisionCooldownUntil = &decisionCooldown
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

func TestStoreMigratesLegacyTaskSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-store.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	legacySchema := `CREATE TABLE tasks (
		task_id TEXT PRIMARY KEY,
		template_id TEXT NOT NULL,
		repository_id TEXT NOT NULL,
		machine_id TEXT NOT NULL,
		status TEXT NOT NULL,
		phase TEXT NOT NULL DEFAULT 'planning',
		user_request TEXT NOT NULL,
		created_by TEXT NOT NULL,
		remote_workdir TEXT NOT NULL,
		tmux_session_name TEXT NOT NULL,
		remote_codex_session_id TEXT NOT NULL,
		last_input TEXT NOT NULL,
		last_output_summary TEXT NOT NULL,
		last_screen_digest TEXT NOT NULL,
		active_responder_name TEXT NOT NULL DEFAULT '',
		active_responder_screen_digest TEXT NOT NULL DEFAULT '',
		last_resolved_responder_name TEXT NOT NULL DEFAULT '',
		last_resolved_screen_digest TEXT NOT NULL DEFAULT '',
		responder_cooldown_until TEXT,
		pending_post_responder_action TEXT NOT NULL DEFAULT '',
		last_continuation_screen_digest TEXT NOT NULL DEFAULT '',
		last_decision_screen_digest TEXT NOT NULL DEFAULT '',
		last_decision_action TEXT NOT NULL DEFAULT '',
		decision_cooldown_until TEXT,
		awaiting_question TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`
	if _, err := db.ExecContext(ctx, legacySchema); err != nil {
		t.Fatalf("Exec legacy schema returned error: %v", err)
	}

	createdAt := time.Date(2026, 5, 18, 8, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(5 * time.Minute)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tasks (
			task_id,
			template_id,
			repository_id,
			machine_id,
			status,
			phase,
			user_request,
			created_by,
			remote_workdir,
			tmux_session_name,
			remote_codex_session_id,
			last_input,
			last_output_summary,
			last_screen_digest,
			active_responder_name,
			active_responder_screen_digest,
			last_resolved_responder_name,
			last_resolved_screen_digest,
			responder_cooldown_until,
			pending_post_responder_action,
			last_continuation_screen_digest,
			last_decision_screen_digest,
			last_decision_action,
			decision_cooldown_until,
			awaiting_question,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"task-legacy",
		"feature_dev",
		"repo_backend",
		"machine_a",
		StatusRunning,
		TaskPhaseExecuting,
		"Continue implementation",
		"user_legacy",
		"/srv/repos/backend/.codex/task-legacy",
		"alterego-task-legacy",
		"codex-session-legacy",
		"Continue",
		"Implementation in progress",
		"digest:legacy",
		"",
		"",
		"",
		"",
		nil,
		"",
		"",
		"",
		"",
		nil,
		nil,
		createdAt.Format(time.RFC3339Nano),
		updatedAt.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("Insert legacy row returned error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close legacy db returned error: %v", err)
	}

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	got, err := store.GetTask(ctx, "task-legacy")
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}
	if got.Phase != TaskPhaseExecuting {
		t.Fatalf("Phase = %q, want %q", got.Phase, TaskPhaseExecuting)
	}
	if got.WorkflowStage != WorkflowStageImplementation {
		t.Fatalf("WorkflowStage = %q, want %q", got.WorkflowStage, WorkflowStageImplementation)
	}
	if got.ThreadID != "" || got.ActiveTurnID != "" || got.LastThreadStatus != "" || got.LastTurnStatus != "" || got.LastObservedItemID != "" {
		t.Fatalf("migrated app-server fields = %#v", got)
	}
	if got.LastRemoteActivityAt != nil {
		t.Fatalf("LastRemoteActivityAt = %v, want nil", got.LastRemoteActivityAt)
	}

	got.WorkflowStage = WorkflowStageVerification
	got.ThreadID = "thread-migrated"
	got.ActiveTurnID = "turn-migrated"
	got.LastThreadStatus = "running"
	got.LastTurnStatus = "completed"
	got.LastObservedItemID = "item-migrated"
	lastRemoteActivityAt := updatedAt.Add(30 * time.Second)
	got.LastRemoteActivityAt = &lastRemoteActivityAt
	got.UpdatedAt = updatedAt.Add(time.Minute)
	if err := store.UpdateTask(ctx, got); err != nil {
		t.Fatalf("UpdateTask returned error: %v", err)
	}

	persisted, err := store.GetTask(ctx, "task-legacy")
	if err != nil {
		t.Fatalf("GetTask after update returned error: %v", err)
	}
	assertTaskFields(t, persisted, got)
}

func TestStoreRepairsWorkflowStageFromBrokenInitialMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "broken-migration.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schemaAfterBrokenMigration := `CREATE TABLE tasks (
		task_id TEXT PRIMARY KEY,
		template_id TEXT NOT NULL,
		repository_id TEXT NOT NULL,
		machine_id TEXT NOT NULL,
		status TEXT NOT NULL,
		phase TEXT NOT NULL DEFAULT 'planning',
		workflow_stage TEXT NOT NULL DEFAULT 'requirement_discussion',
		user_request TEXT NOT NULL,
		created_by TEXT NOT NULL,
		remote_workdir TEXT NOT NULL,
		tmux_session_name TEXT NOT NULL,
		remote_codex_session_id TEXT NOT NULL,
		thread_id TEXT NOT NULL DEFAULT '',
		active_turn_id TEXT NOT NULL DEFAULT '',
		last_thread_status TEXT NOT NULL DEFAULT '',
		last_turn_status TEXT NOT NULL DEFAULT '',
		last_observed_item_id TEXT NOT NULL DEFAULT '',
		last_remote_activity_at TEXT,
		last_input TEXT NOT NULL,
		last_output_summary TEXT NOT NULL,
		last_screen_digest TEXT NOT NULL,
		active_responder_name TEXT NOT NULL DEFAULT '',
		active_responder_screen_digest TEXT NOT NULL DEFAULT '',
		last_resolved_responder_name TEXT NOT NULL DEFAULT '',
		last_resolved_screen_digest TEXT NOT NULL DEFAULT '',
		responder_cooldown_until TEXT,
		pending_post_responder_action TEXT NOT NULL DEFAULT '',
		last_continuation_screen_digest TEXT NOT NULL DEFAULT '',
		last_decision_screen_digest TEXT NOT NULL DEFAULT '',
		last_decision_action TEXT NOT NULL DEFAULT '',
		decision_cooldown_until TEXT,
		awaiting_question TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`
	if _, err := db.ExecContext(ctx, schemaAfterBrokenMigration); err != nil {
		t.Fatalf("Exec schemaAfterBrokenMigration returned error: %v", err)
	}

	createdAt := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(10 * time.Minute)
	insertTask := func(taskID string, phase TaskPhase, workflowStage WorkflowStage) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
			INSERT INTO tasks (
				task_id,
				template_id,
				repository_id,
				machine_id,
				status,
				phase,
				workflow_stage,
				user_request,
				created_by,
				remote_workdir,
				tmux_session_name,
				remote_codex_session_id,
				thread_id,
				active_turn_id,
				last_thread_status,
				last_turn_status,
				last_observed_item_id,
				last_remote_activity_at,
				last_input,
				last_output_summary,
				last_screen_digest,
				active_responder_name,
				active_responder_screen_digest,
				last_resolved_responder_name,
				last_resolved_screen_digest,
				responder_cooldown_until,
				pending_post_responder_action,
				last_continuation_screen_digest,
				last_decision_screen_digest,
				last_decision_action,
				decision_cooldown_until,
				awaiting_question,
				created_at,
				updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			taskID,
			"feature_dev",
			"repo_backend",
			"machine_a",
			StatusRunning,
			phase,
			workflowStage,
			"Continue implementation",
			"user_legacy",
			"/srv/repos/backend/.codex/"+taskID,
			"alterego-"+taskID,
			"codex-session-"+taskID,
			"",
			"",
			"",
			"",
			"",
			nil,
			"Continue",
			"Implementation in progress",
			"digest:"+taskID,
			"",
			"",
			"",
			"",
			nil,
			"",
			"",
			"",
			"",
			nil,
			nil,
			createdAt.Format(time.RFC3339Nano),
			updatedAt.Format(time.RFC3339Nano),
		); err != nil {
			t.Fatalf("Insert task %q returned error: %v", taskID, err)
		}
	}

	insertTask("task-broken", TaskPhaseExecuting, WorkflowStageRequirementDiscussion)
	insertTask("task-correct", TaskPhaseExecuting, WorkflowStageImplementation)
	if err := db.Close(); err != nil {
		t.Fatalf("Close db returned error: %v", err)
	}

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	defer store.Close()

	broken, err := store.GetTask(ctx, "task-broken")
	if err != nil {
		t.Fatalf("GetTask(task-broken) returned error: %v", err)
	}
	if broken.WorkflowStage != WorkflowStageImplementation {
		t.Fatalf("broken.WorkflowStage = %q, want %q", broken.WorkflowStage, WorkflowStageImplementation)
	}

	correct, err := store.GetTask(ctx, "task-correct")
	if err != nil {
		t.Fatalf("GetTask(task-correct) returned error: %v", err)
	}
	if correct.WorkflowStage != WorkflowStageImplementation {
		t.Fatalf("correct.WorkflowStage = %q, want %q", correct.WorkflowStage, WorkflowStageImplementation)
	}
}

func TestStoreDoesNotReapplyWorkflowStageRepairAfterMarker(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "workflow-stage-marker.db")

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}

	schemaAfterBrokenMigration := `CREATE TABLE tasks (
		task_id TEXT PRIMARY KEY,
		template_id TEXT NOT NULL,
		repository_id TEXT NOT NULL,
		machine_id TEXT NOT NULL,
		status TEXT NOT NULL,
		phase TEXT NOT NULL DEFAULT 'planning',
		workflow_stage TEXT NOT NULL DEFAULT 'requirement_discussion',
		user_request TEXT NOT NULL,
		created_by TEXT NOT NULL,
		remote_workdir TEXT NOT NULL,
		tmux_session_name TEXT NOT NULL,
		remote_codex_session_id TEXT NOT NULL,
		thread_id TEXT NOT NULL DEFAULT '',
		active_turn_id TEXT NOT NULL DEFAULT '',
		last_thread_status TEXT NOT NULL DEFAULT '',
		last_turn_status TEXT NOT NULL DEFAULT '',
		last_observed_item_id TEXT NOT NULL DEFAULT '',
		last_remote_activity_at TEXT,
		last_input TEXT NOT NULL,
		last_output_summary TEXT NOT NULL,
		last_screen_digest TEXT NOT NULL,
		active_responder_name TEXT NOT NULL DEFAULT '',
		active_responder_screen_digest TEXT NOT NULL DEFAULT '',
		last_resolved_responder_name TEXT NOT NULL DEFAULT '',
		last_resolved_screen_digest TEXT NOT NULL DEFAULT '',
		responder_cooldown_until TEXT,
		pending_post_responder_action TEXT NOT NULL DEFAULT '',
		last_continuation_screen_digest TEXT NOT NULL DEFAULT '',
		last_decision_screen_digest TEXT NOT NULL DEFAULT '',
		last_decision_action TEXT NOT NULL DEFAULT '',
		decision_cooldown_until TEXT,
		awaiting_question TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`
	if _, err := db.ExecContext(ctx, schemaAfterBrokenMigration); err != nil {
		t.Fatalf("Exec schemaAfterBrokenMigration returned error: %v", err)
	}

	insertLegacyTaskRow(t, ctx, db, "task-initial", TaskPhaseExecuting, WorkflowStageRequirementDiscussion)
	if err := db.Close(); err != nil {
		t.Fatalf("Close db returned error: %v", err)
	}

	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore returned error: %v", err)
	}
	initial, err := store.GetTask(ctx, "task-initial")
	if err != nil {
		t.Fatalf("GetTask(task-initial) returned error: %v", err)
	}
	if initial.WorkflowStage != WorkflowStageImplementation {
		t.Fatalf("initial.WorkflowStage = %q, want %q", initial.WorkflowStage, WorkflowStageImplementation)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close store returned error: %v", err)
	}

	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open reopen returned error: %v", err)
	}
	insertLegacyTaskRow(t, ctx, db, "task-after-marker", TaskPhaseExecuting, WorkflowStageRequirementDiscussion)
	if err := db.Close(); err != nil {
		t.Fatalf("Close db after insert returned error: %v", err)
	}

	store, err = OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore second reopen returned error: %v", err)
	}
	defer store.Close()

	got, err := store.GetTask(ctx, "task-after-marker")
	if err != nil {
		t.Fatalf("GetTask(task-after-marker) returned error: %v", err)
	}
	if got.WorkflowStage != WorkflowStageRequirementDiscussion {
		t.Fatalf("got.WorkflowStage = %q, want %q", got.WorkflowStage, WorkflowStageRequirementDiscussion)
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

func insertLegacyTaskRow(t *testing.T, ctx context.Context, db *sql.DB, taskID string, phase TaskPhase, workflowStage WorkflowStage) {
	t.Helper()

	createdAt := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(10 * time.Minute)
	if _, err := db.ExecContext(ctx, `
		INSERT INTO tasks (
			task_id,
			template_id,
			repository_id,
			machine_id,
			status,
			phase,
			workflow_stage,
			user_request,
			created_by,
			remote_workdir,
			tmux_session_name,
			remote_codex_session_id,
			thread_id,
			active_turn_id,
			last_thread_status,
			last_turn_status,
			last_observed_item_id,
			last_remote_activity_at,
			last_input,
			last_output_summary,
			last_screen_digest,
			active_responder_name,
			active_responder_screen_digest,
			last_resolved_responder_name,
			last_resolved_screen_digest,
			responder_cooldown_until,
			pending_post_responder_action,
			last_continuation_screen_digest,
			last_decision_screen_digest,
			last_decision_action,
			decision_cooldown_until,
			awaiting_question,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		taskID,
		"feature_dev",
		"repo_backend",
		"machine_a",
		StatusRunning,
		phase,
		workflowStage,
		"Continue implementation",
		"user_legacy",
		"/srv/repos/backend/.codex/"+taskID,
		"alterego-"+taskID,
		"codex-session-"+taskID,
		"",
		"",
		"",
		"",
		"",
		nil,
		"Continue",
		"Implementation in progress",
		"digest:"+taskID,
		"",
		"",
		"",
		"",
		nil,
		"",
		"",
		"",
		"",
		nil,
		nil,
		createdAt.Format(time.RFC3339Nano),
		updatedAt.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("Insert task %q returned error: %v", taskID, err)
	}
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
		TMUXSessionName:      "",
		RemoteCodexSessionID: "",
		LastScreenDigest:     "",
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
	if got.TMUXSessionName != want.TMUXSessionName {
		t.Fatalf("TMUXSessionName = %q, want %q", got.TMUXSessionName, want.TMUXSessionName)
	}
	if got.RemoteCodexSessionID != want.RemoteCodexSessionID {
		t.Fatalf("RemoteCodexSessionID = %q, want %q", got.RemoteCodexSessionID, want.RemoteCodexSessionID)
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
	if got.LastScreenDigest != want.LastScreenDigest {
		t.Fatalf("LastScreenDigest = %q, want %q", got.LastScreenDigest, want.LastScreenDigest)
	}
	if got.LastInput != want.LastInput {
		t.Fatalf("LastInput = %q, want %q", got.LastInput, want.LastInput)
	}
	if got.LastOutputSummary != want.LastOutputSummary {
		t.Fatalf("LastOutputSummary = %q, want %q", got.LastOutputSummary, want.LastOutputSummary)
	}
	if got.ActiveResponderName != want.ActiveResponderName {
		t.Fatalf("ActiveResponderName = %q, want %q", got.ActiveResponderName, want.ActiveResponderName)
	}
	if got.ActiveResponderScreenDigest != want.ActiveResponderScreenDigest {
		t.Fatalf("ActiveResponderScreenDigest = %q, want %q", got.ActiveResponderScreenDigest, want.ActiveResponderScreenDigest)
	}
	if got.LastResolvedResponderName != want.LastResolvedResponderName {
		t.Fatalf("LastResolvedResponderName = %q, want %q", got.LastResolvedResponderName, want.LastResolvedResponderName)
	}
	if got.LastResolvedScreenDigest != want.LastResolvedScreenDigest {
		t.Fatalf("LastResolvedScreenDigest = %q, want %q", got.LastResolvedScreenDigest, want.LastResolvedScreenDigest)
	}
	if got.LastDecisionScreenDigest != want.LastDecisionScreenDigest {
		t.Fatalf("LastDecisionScreenDigest = %q, want %q", got.LastDecisionScreenDigest, want.LastDecisionScreenDigest)
	}
	if got.LastDecisionAction != want.LastDecisionAction {
		t.Fatalf("LastDecisionAction = %q, want %q", got.LastDecisionAction, want.LastDecisionAction)
	}
	switch {
	case got.ResponderCooldownUntil == nil && want.ResponderCooldownUntil == nil:
	case got.ResponderCooldownUntil == nil || want.ResponderCooldownUntil == nil:
		t.Fatalf("ResponderCooldownUntil = %v, want %v", got.ResponderCooldownUntil, want.ResponderCooldownUntil)
	case !got.ResponderCooldownUntil.Equal(*want.ResponderCooldownUntil):
		t.Fatalf("ResponderCooldownUntil = %s, want %s", got.ResponderCooldownUntil, want.ResponderCooldownUntil)
	}
	switch {
	case got.DecisionCooldownUntil == nil && want.DecisionCooldownUntil == nil:
	case got.DecisionCooldownUntil == nil || want.DecisionCooldownUntil == nil:
		t.Fatalf("DecisionCooldownUntil = %v, want %v", got.DecisionCooldownUntil, want.DecisionCooldownUntil)
	case !got.DecisionCooldownUntil.Equal(*want.DecisionCooldownUntil):
		t.Fatalf("DecisionCooldownUntil = %s, want %s", got.DecisionCooldownUntil, want.DecisionCooldownUntil)
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
