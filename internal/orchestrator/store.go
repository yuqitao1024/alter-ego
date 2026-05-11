package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
			user_request,
			created_by,
			remote_workdir,
			tmux_session_name,
			remote_codex_session_id,
			last_input,
			last_output_summary,
			last_screen_digest,
			awaiting_question,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		task.TaskID,
		task.TemplateID,
		task.RepositoryID,
		task.MachineID,
		task.Status,
		task.UserRequest,
		task.CreatedBy,
		task.RemoteWorkdir,
		task.TMUXSessionName,
		task.RemoteCodexSessionID,
		task.LastInput,
		task.LastOutputSummary,
		task.LastScreenDigest,
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
			user_request = ?,
			created_by = ?,
			remote_workdir = ?,
			tmux_session_name = ?,
			remote_codex_session_id = ?,
			last_input = ?,
			last_output_summary = ?,
			last_screen_digest = ?,
			awaiting_question = ?,
			created_at = ?,
			updated_at = ?
		WHERE task_id = ?
	`,
		task.TemplateID,
		task.RepositoryID,
		task.MachineID,
		task.Status,
		task.UserRequest,
		task.CreatedBy,
		task.RemoteWorkdir,
		task.TMUXSessionName,
		task.RemoteCodexSessionID,
		task.LastInput,
		task.LastOutputSummary,
		task.LastScreenDigest,
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
			user_request,
			created_by,
			remote_workdir,
			tmux_session_name,
			remote_codex_session_id,
			last_input,
			last_output_summary,
			last_screen_digest,
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
			user_request,
			created_by,
			remote_workdir,
			tmux_session_name,
			remote_codex_session_id,
			last_input,
			last_output_summary,
			last_screen_digest,
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

func (s *Store) init(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
			task_id TEXT PRIMARY KEY,
			template_id TEXT NOT NULL,
			repository_id TEXT NOT NULL,
			machine_id TEXT NOT NULL,
			status TEXT NOT NULL,
			user_request TEXT NOT NULL,
			created_by TEXT NOT NULL,
			remote_workdir TEXT NOT NULL,
			tmux_session_name TEXT NOT NULL,
			remote_codex_session_id TEXT NOT NULL,
			last_input TEXT NOT NULL,
			last_output_summary TEXT NOT NULL,
			last_screen_digest TEXT NOT NULL,
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
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)`,
		`CREATE INDEX IF NOT EXISTS idx_task_events_task_id ON task_events(task_id)`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize sqlite store: %w", err)
		}
	}

	return nil
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (TaskRun, error) {
	var task TaskRun
	var status string
	var awaitingQuestion sql.NullString
	var createdAt string
	var updatedAt string

	err := scanner.Scan(
		&task.TaskID,
		&task.TemplateID,
		&task.RepositoryID,
		&task.MachineID,
		&status,
		&task.UserRequest,
		&task.CreatedBy,
		&task.RemoteWorkdir,
		&task.TMUXSessionName,
		&task.RemoteCodexSessionID,
		&task.LastInput,
		&task.LastOutputSummary,
		&task.LastScreenDigest,
		&awaitingQuestion,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return TaskRun{}, err
	}

	task.Status = TaskStatus(status)

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

func IsTaskNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
