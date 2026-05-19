package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite store: %w", err)
	}

	store := &Store{db: db}
	if err := store.init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) CreateTask(ctx context.Context, task TaskRun) error {
	awaitingQuestion, err := marshalAwaitingQuestion(task.AwaitingQuestion)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
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
		task.TaskID,
		task.TemplateID,
		task.RepositoryID,
		task.MachineID,
		task.Status,
		task.Phase,
		task.WorkflowStage,
		task.UserRequest,
		task.CreatedBy,
		task.RemoteWorkdir,
		task.TMUXSessionName,
		task.RemoteCodexSessionID,
		task.ThreadID,
		task.ActiveTurnID,
		task.LastThreadStatus,
		task.LastTurnStatus,
		task.LastObservedItemID,
		formatOptionalTime(task.LastRemoteActivityAt),
		task.LastInput,
		task.LastOutputSummary,
		task.LastScreenDigest,
		task.ActiveResponderName,
		task.ActiveResponderScreenDigest,
		task.LastResolvedResponderName,
		task.LastResolvedScreenDigest,
		formatOptionalTime(task.ResponderCooldownUntil),
		task.PendingPostResponderAction,
		task.LastContinuationScreenDigest,
		task.LastDecisionScreenDigest,
		task.LastDecisionAction,
		formatOptionalTime(task.DecisionCooldownUntil),
		awaitingQuestion,
		task.CreatedAt.UTC().Format(time.RFC3339Nano),
		task.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert task %q: %w", task.TaskID, err)
	}

	return nil
}

func (s *Store) UpdateTask(ctx context.Context, task TaskRun) error {
	awaitingQuestion, err := marshalAwaitingQuestion(task.AwaitingQuestion)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET template_id = ?,
			repository_id = ?,
			machine_id = ?,
			status = ?,
			phase = ?,
			workflow_stage = ?,
			user_request = ?,
			created_by = ?,
			remote_workdir = ?,
			tmux_session_name = ?,
			remote_codex_session_id = ?,
			thread_id = ?,
			active_turn_id = ?,
			last_thread_status = ?,
			last_turn_status = ?,
			last_observed_item_id = ?,
			last_remote_activity_at = ?,
			last_input = ?,
			last_output_summary = ?,
			last_screen_digest = ?,
			active_responder_name = ?,
			active_responder_screen_digest = ?,
			last_resolved_responder_name = ?,
			last_resolved_screen_digest = ?,
			responder_cooldown_until = ?,
			pending_post_responder_action = ?,
			last_continuation_screen_digest = ?,
			last_decision_screen_digest = ?,
			last_decision_action = ?,
			decision_cooldown_until = ?,
			awaiting_question = ?,
			created_at = ?,
			updated_at = ?
		WHERE task_id = ?
	`,
		task.TemplateID,
		task.RepositoryID,
		task.MachineID,
		task.Status,
		task.Phase,
		task.WorkflowStage,
		task.UserRequest,
		task.CreatedBy,
		task.RemoteWorkdir,
		task.TMUXSessionName,
		task.RemoteCodexSessionID,
		task.ThreadID,
		task.ActiveTurnID,
		task.LastThreadStatus,
		task.LastTurnStatus,
		task.LastObservedItemID,
		formatOptionalTime(task.LastRemoteActivityAt),
		task.LastInput,
		task.LastOutputSummary,
		task.LastScreenDigest,
		task.ActiveResponderName,
		task.ActiveResponderScreenDigest,
		task.LastResolvedResponderName,
		task.LastResolvedScreenDigest,
		formatOptionalTime(task.ResponderCooldownUntil),
		task.PendingPostResponderAction,
		task.LastContinuationScreenDigest,
		task.LastDecisionScreenDigest,
		task.LastDecisionAction,
		formatOptionalTime(task.DecisionCooldownUntil),
		awaitingQuestion,
		task.CreatedAt.UTC().Format(time.RFC3339Nano),
		task.UpdatedAt.UTC().Format(time.RFC3339Nano),
		task.TaskID,
	)
	if err != nil {
		return fmt.Errorf("update task %q: %w", task.TaskID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update task %q: rows affected: %w", task.TaskID, err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (s *Store) GetTask(ctx context.Context, taskID string) (TaskRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
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
		FROM tasks
		WHERE task_id = ?
	`, taskID)

	task, err := scanTask(row)
	if err != nil {
		return TaskRun{}, fmt.Errorf("get task %q: %w", taskID, err)
	}
	return task, nil
}

func (s *Store) ListActiveTasks(ctx context.Context) ([]TaskRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
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
		FROM tasks
		WHERE status NOT IN (?, ?, ?)
		ORDER BY created_at, task_id
	`, StatusCompleted, StatusFailed, StatusStopped)
	if err != nil {
		return nil, fmt.Errorf("list active tasks: %w", err)
	}
	defer rows.Close()

	var tasks []TaskRun
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("list active tasks: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active tasks: %w", err)
	}

	return tasks, nil
}

func (s *Store) AppendEvent(ctx context.Context, event TaskEvent) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_events (
			task_id,
			event_type,
			message,
			created_at
		) VALUES (?, ?, ?, ?)
	`,
		event.TaskID,
		event.EventType,
		event.Message,
		event.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("append task event for %q: %w", event.TaskID, err)
	}

	return nil
}

func (s *Store) ListEvents(ctx context.Context, taskID string) ([]TaskEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_id, event_type, message, created_at
		FROM task_events
		WHERE task_id = ?
		ORDER BY id
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task events for %q: %w", taskID, err)
	}
	defer rows.Close()

	var events []TaskEvent
	for rows.Next() {
		var event TaskEvent
		var createdAt string
		if err := rows.Scan(&event.TaskID, &event.EventType, &event.Message, &createdAt); err != nil {
			return nil, fmt.Errorf("list task events for %q: %w", taskID, err)
		}
		event.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse task event created_at: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list task events for %q: %w", taskID, err)
	}

	return events, nil
}

func (s *Store) AppendQuestion(ctx context.Context, question TaskQuestion) error {
	var answeredAt any
	if question.AnsweredAt != nil {
		answeredAt = question.AnsweredAt.UTC().Format(time.RFC3339Nano)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_questions (
			task_id,
			question_type,
			question_text,
			options_summary,
			context_excerpt,
			asked_at,
			answered_at,
			answer_text
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		question.TaskID,
		question.QuestionType,
		question.QuestionText,
		question.OptionsSummary,
		question.ContextExcerpt,
		question.AskedAt.UTC().Format(time.RFC3339Nano),
		answeredAt,
		question.AnswerText,
	)
	if err != nil {
		return fmt.Errorf("append task question for %q: %w", question.TaskID, err)
	}
	return nil
}

func (s *Store) MarkQuestionAnswered(ctx context.Context, taskID string, askedAt, answeredAt time.Time, answerText string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE task_questions
		SET answered_at = ?, answer_text = ?
		WHERE task_id = ? AND asked_at = ? AND answered_at IS NULL
	`,
		answeredAt.UTC().Format(time.RFC3339Nano),
		answerText,
		taskID,
		askedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("mark task question answered for %q: %w", taskID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark task question answered for %q: rows affected: %w", taskID, err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListQuestions(ctx context.Context, taskID string) ([]TaskQuestion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			task_id,
			question_type,
			question_text,
			options_summary,
			context_excerpt,
			asked_at,
			answered_at,
			answer_text
		FROM task_questions
		WHERE task_id = ?
		ORDER BY id
	`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task questions for %q: %w", taskID, err)
	}
	defer rows.Close()

	var questions []TaskQuestion
	for rows.Next() {
		var question TaskQuestion
		var askedAt string
		var answeredAt sql.NullString
		if err := rows.Scan(
			&question.ID,
			&question.TaskID,
			&question.QuestionType,
			&question.QuestionText,
			&question.OptionsSummary,
			&question.ContextExcerpt,
			&askedAt,
			&answeredAt,
			&question.AnswerText,
		); err != nil {
			return nil, fmt.Errorf("list task questions for %q: %w", taskID, err)
		}
		question.AskedAt, err = time.Parse(time.RFC3339Nano, askedAt)
		if err != nil {
			return nil, fmt.Errorf("parse task question asked_at: %w", err)
		}
		if answeredAt.Valid && answeredAt.String != "" {
			tm, err := time.Parse(time.RFC3339Nano, answeredAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse task question answered_at: %w", err)
			}
			question.AnsweredAt = &tm
		}
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list task questions for %q: %w", taskID, err)
	}

	return questions, nil
}

func (s *Store) init(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
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
		)`,
		`CREATE TABLE IF NOT EXISTS task_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS task_questions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			question_type TEXT NOT NULL,
			question_text TEXT NOT NULL,
			options_summary TEXT NOT NULL,
			context_excerpt TEXT NOT NULL,
			asked_at TEXT NOT NULL,
			answered_at TEXT,
			answer_text TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_task_events_task_id ON task_events(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_questions_task_id ON task_questions(task_id)`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize sqlite store: %w", err)
		}
	}

	migrations := []struct {
		statement string
		column    string
	}{
		{statement: `ALTER TABLE tasks ADD COLUMN active_responder_name TEXT NOT NULL DEFAULT ''`, column: "active_responder_name"},
		{statement: `ALTER TABLE tasks ADD COLUMN active_responder_screen_digest TEXT NOT NULL DEFAULT ''`, column: "active_responder_screen_digest"},
		{statement: `ALTER TABLE tasks ADD COLUMN last_resolved_responder_name TEXT NOT NULL DEFAULT ''`, column: "last_resolved_responder_name"},
		{statement: `ALTER TABLE tasks ADD COLUMN last_resolved_screen_digest TEXT NOT NULL DEFAULT ''`, column: "last_resolved_screen_digest"},
		{statement: `ALTER TABLE tasks ADD COLUMN responder_cooldown_until TEXT`, column: "responder_cooldown_until"},
		{statement: `ALTER TABLE tasks ADD COLUMN pending_post_responder_action TEXT NOT NULL DEFAULT ''`, column: "pending_post_responder_action"},
		{statement: `ALTER TABLE tasks ADD COLUMN last_continuation_screen_digest TEXT NOT NULL DEFAULT ''`, column: "last_continuation_screen_digest"},
		{statement: `ALTER TABLE tasks ADD COLUMN last_decision_screen_digest TEXT NOT NULL DEFAULT ''`, column: "last_decision_screen_digest"},
		{statement: `ALTER TABLE tasks ADD COLUMN last_decision_action TEXT NOT NULL DEFAULT ''`, column: "last_decision_action"},
		{statement: `ALTER TABLE tasks ADD COLUMN decision_cooldown_until TEXT`, column: "decision_cooldown_until"},
		{statement: `ALTER TABLE tasks ADD COLUMN phase TEXT NOT NULL DEFAULT 'planning'`, column: "phase"},
		{statement: `ALTER TABLE tasks ADD COLUMN workflow_stage TEXT NOT NULL DEFAULT 'requirement_discussion'`, column: "workflow_stage"},
		{statement: `ALTER TABLE tasks ADD COLUMN thread_id TEXT NOT NULL DEFAULT ''`, column: "thread_id"},
		{statement: `ALTER TABLE tasks ADD COLUMN active_turn_id TEXT NOT NULL DEFAULT ''`, column: "active_turn_id"},
		{statement: `ALTER TABLE tasks ADD COLUMN last_thread_status TEXT NOT NULL DEFAULT ''`, column: "last_thread_status"},
		{statement: `ALTER TABLE tasks ADD COLUMN last_turn_status TEXT NOT NULL DEFAULT ''`, column: "last_turn_status"},
		{statement: `ALTER TABLE tasks ADD COLUMN last_observed_item_id TEXT NOT NULL DEFAULT ''`, column: "last_observed_item_id"},
		{statement: `ALTER TABLE tasks ADD COLUMN last_remote_activity_at TEXT`, column: "last_remote_activity_at"},
	}

	for _, migration := range migrations {
		_, err := s.addColumnIfMissing(ctx, migration.statement)
		if err != nil {
			return fmt.Errorf("migrate sqlite store: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET workflow_stage = ?
		WHERE workflow_stage = ? AND phase = ?
	`,
		WorkflowStageImplementation,
		WorkflowStageRequirementDiscussion,
		TaskPhaseExecuting,
	); err != nil {
		return fmt.Errorf("repair workflow_stage for executing tasks: %w", err)
	}

	return nil
}

func (s *Store) addColumnIfMissing(ctx context.Context, statement string) (bool, error) {
	if _, err := s.db.ExecContext(ctx, statement); err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (TaskRun, error) {
	var task TaskRun
	var status string
	var phase string
	var workflowStage string
	var awaitingQuestion sql.NullString
	var responderCooldownUntil sql.NullString
	var decisionCooldownUntil sql.NullString
	var lastRemoteActivityAt sql.NullString
	var createdAt string
	var updatedAt string

	err := scanner.Scan(
		&task.TaskID,
		&task.TemplateID,
		&task.RepositoryID,
		&task.MachineID,
		&status,
		&phase,
		&workflowStage,
		&task.UserRequest,
		&task.CreatedBy,
		&task.RemoteWorkdir,
		&task.TMUXSessionName,
		&task.RemoteCodexSessionID,
		&task.ThreadID,
		&task.ActiveTurnID,
		&task.LastThreadStatus,
		&task.LastTurnStatus,
		&task.LastObservedItemID,
		&lastRemoteActivityAt,
		&task.LastInput,
		&task.LastOutputSummary,
		&task.LastScreenDigest,
		&task.ActiveResponderName,
		&task.ActiveResponderScreenDigest,
		&task.LastResolvedResponderName,
		&task.LastResolvedScreenDigest,
		&responderCooldownUntil,
		&task.PendingPostResponderAction,
		&task.LastContinuationScreenDigest,
		&task.LastDecisionScreenDigest,
		&task.LastDecisionAction,
		&decisionCooldownUntil,
		&awaitingQuestion,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return TaskRun{}, err
	}

	task.Status = TaskStatus(status)
	task.Phase = TaskPhase(phase)
	task.WorkflowStage = WorkflowStage(workflowStage)

	task.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return TaskRun{}, fmt.Errorf("parse created_at: %w", err)
	}
	task.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return TaskRun{}, fmt.Errorf("parse updated_at: %w", err)
	}

	task.AwaitingQuestion, err = unmarshalAwaitingQuestion(awaitingQuestion)
	if err != nil {
		return TaskRun{}, err
	}
	task.ResponderCooldownUntil, err = parseOptionalTime(responderCooldownUntil)
	if err != nil {
		return TaskRun{}, fmt.Errorf("parse responder_cooldown_until: %w", err)
	}
	task.DecisionCooldownUntil, err = parseOptionalTime(decisionCooldownUntil)
	if err != nil {
		return TaskRun{}, fmt.Errorf("parse decision_cooldown_until: %w", err)
	}
	task.LastRemoteActivityAt, err = parseOptionalTime(lastRemoteActivityAt)
	if err != nil {
		return TaskRun{}, fmt.Errorf("parse last_remote_activity_at: %w", err)
	}

	return task, nil
}

func marshalAwaitingQuestion(question *AwaitingQuestion) (any, error) {
	if question == nil {
		return nil, nil
	}

	data, err := json.Marshal(question)
	if err != nil {
		return nil, fmt.Errorf("marshal awaiting question: %w", err)
	}
	return string(data), nil
}

func unmarshalAwaitingQuestion(raw sql.NullString) (*AwaitingQuestion, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}

	var question AwaitingQuestion
	if err := json.Unmarshal([]byte(raw.String), &question); err != nil {
		return nil, fmt.Errorf("unmarshal awaiting question: %w", err)
	}
	return &question, nil
}

func formatOptionalTime(tm *time.Time) any {
	if tm == nil {
		return nil
	}
	return tm.UTC().Format(time.RFC3339Nano)
}

func parseOptionalTime(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	tm, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return nil, err
	}
	return &tm, nil
}

func IsTaskNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
