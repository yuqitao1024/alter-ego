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
			thread_id,
			active_turn_id,
			last_input,
			last_output_summary,
			last_decision_action,
			pending_request_id,
			completion_check_status,
			completion_check_sent_at,
			completion_check_done_at,
			awaiting_question,
			created_at,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		task.TaskID,
		task.TemplateID,
		task.RepositoryID,
		task.MachineID,
		task.Status,
		task.UserRequest,
		task.CreatedBy,
		task.RemoteWorkdir,
		task.ThreadID,
		task.ActiveTurnID,
		task.LastInput,
		task.LastOutputSummary,
		task.LastDecisionAction,
		task.PendingRequestID,
		firstNonEmpty(string(task.CompletionCheckStatus), string(CompletionCheckStatusNotStarted)),
		formatOptionalTime(task.CompletionCheckSentAt),
		formatOptionalTime(task.CompletionCheckDoneAt),
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
			thread_id = ?,
			active_turn_id = ?,
			last_input = ?,
			last_output_summary = ?,
			last_decision_action = ?,
			pending_request_id = ?,
			completion_check_status = ?,
			completion_check_sent_at = ?,
			completion_check_done_at = ?,
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
		task.ThreadID,
		task.ActiveTurnID,
		task.LastInput,
		task.LastOutputSummary,
		task.LastDecisionAction,
		task.PendingRequestID,
		firstNonEmpty(string(task.CompletionCheckStatus), string(CompletionCheckStatusNotStarted)),
		formatOptionalTime(task.CompletionCheckSentAt),
		formatOptionalTime(task.CompletionCheckDoneAt),
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
			thread_id,
			active_turn_id,
			last_input,
			last_output_summary,
			last_decision_action,
			pending_request_id,
			completion_check_status,
			completion_check_sent_at,
			completion_check_done_at,
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

func (s *Store) DeleteTask(ctx context.Context, taskID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE task_id = ?`, taskID)
	if err != nil {
		return fmt.Errorf("delete task %q: %w", taskID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete task %q: rows affected: %w", taskID, err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	_, _ = s.db.ExecContext(ctx, `DELETE FROM task_events WHERE task_id = ?`, taskID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM task_questions WHERE task_id = ?`, taskID)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM task_server_requests WHERE task_id = ?`, taskID)
	return nil
}

func (s *Store) GetTaskByThread(ctx context.Context, threadID string) (TaskRun, error) {
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
			thread_id,
			active_turn_id,
			last_input,
			last_output_summary,
			last_decision_action,
			pending_request_id,
			completion_check_status,
			completion_check_sent_at,
			completion_check_done_at,
			awaiting_question,
			created_at,
			updated_at
		FROM tasks
		WHERE thread_id = ?
	`, threadID)

	task, err := scanTask(row)
	if err != nil {
		return TaskRun{}, fmt.Errorf("get task by thread %q: %w", threadID, err)
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
			thread_id,
			active_turn_id,
			last_input,
			last_output_summary,
			last_decision_action,
			pending_request_id,
			completion_check_status,
			completion_check_sent_at,
			completion_check_done_at,
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

func (s *Store) ListTasks(ctx context.Context) ([]TaskRun, error) {
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
			thread_id,
			active_turn_id,
			last_input,
			last_output_summary,
			last_decision_action,
			pending_request_id,
			completion_check_status,
			completion_check_sent_at,
			completion_check_done_at,
			awaiting_question,
			created_at,
			updated_at
		FROM tasks
		ORDER BY created_at, task_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []TaskRun
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("list tasks: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
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

func (s *Store) UpsertTaskServerRequest(ctx context.Context, req TaskServerRequest) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_server_requests (
			request_id,
			task_id,
			thread_id,
			turn_id,
			request_type,
			request_payload,
			status,
			decision_source,
			reply_content,
			created_at,
			reply_started_at,
			replied_at,
			resolved_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(request_id) DO UPDATE SET
			task_id = excluded.task_id,
			thread_id = excluded.thread_id,
			turn_id = excluded.turn_id,
			request_type = excluded.request_type,
			request_payload = excluded.request_payload,
			status = excluded.status,
			decision_source = excluded.decision_source,
			reply_content = excluded.reply_content,
			created_at = excluded.created_at,
			reply_started_at = excluded.reply_started_at,
			replied_at = excluded.replied_at,
			resolved_at = excluded.resolved_at
	`,
		req.RequestID,
		req.TaskID,
		req.ThreadID,
		req.TurnID,
		req.RequestType,
		req.RequestPayload,
		req.Status,
		req.DecisionSource,
		req.ReplyContent,
		req.CreatedAt.UTC().Format(time.RFC3339Nano),
		formatOptionalTime(req.ReplyStartedAt),
		formatOptionalTime(req.RepliedAt),
		formatOptionalTime(req.ResolvedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert task server request %q: %w", req.RequestID, err)
	}
	return nil
}

func (s *Store) GetTaskServerRequest(ctx context.Context, requestID string) (TaskServerRequest, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			request_id,
			task_id,
			thread_id,
			turn_id,
			request_type,
			request_payload,
			status,
			decision_source,
			reply_content,
			created_at,
			reply_started_at,
			replied_at,
			resolved_at
		FROM task_server_requests
		WHERE request_id = ?
	`, requestID)

	req, err := scanTaskServerRequest(row)
	if err != nil {
		return TaskServerRequest{}, fmt.Errorf("get task server request %q: %w", requestID, err)
	}
	return req, nil
}

func (s *Store) ListOpenTaskServerRequests(ctx context.Context, taskID string) ([]TaskServerRequest, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			request_id,
			task_id,
			thread_id,
			turn_id,
			request_type,
			request_payload,
			status,
			decision_source,
			reply_content,
			created_at,
			reply_started_at,
			replied_at,
			resolved_at
		FROM task_server_requests
		WHERE task_id = ? AND status IN (?, ?)
		ORDER BY created_at, request_id
	`, taskID, ServerRequestStatusPending, ServerRequestStatusReplying)
	if err != nil {
		return nil, fmt.Errorf("list open task server requests for %q: %w", taskID, err)
	}
	defer rows.Close()

	var requests []TaskServerRequest
	for rows.Next() {
		req, err := scanTaskServerRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("list open task server requests for %q: %w", taskID, err)
		}
		requests = append(requests, req)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list open task server requests for %q: %w", taskID, err)
	}
	return requests, nil
}

func (s *Store) MarkTaskServerRequestReplying(ctx context.Context, requestID string, at time.Time) error {
	return s.updateTaskServerRequestStatus(ctx, requestID, ServerRequestStatusReplying, "", &at, nil, nil, false)
}

func (s *Store) MarkTaskServerRequestReplied(ctx context.Context, requestID, reply string, at time.Time) error {
	return s.updateTaskServerRequestStatus(ctx, requestID, ServerRequestStatusReplied, reply, nil, &at, nil, true)
}

func (s *Store) MarkTaskServerRequestResolved(ctx context.Context, requestID string, at time.Time) error {
	return s.updateTaskServerRequestStatus(ctx, requestID, ServerRequestStatusResolved, "", nil, nil, &at, false)
}

func (s *Store) MarkTaskServerRequestIgnored(ctx context.Context, requestID string, at time.Time) error {
	return s.updateTaskServerRequestStatus(ctx, requestID, ServerRequestStatusIgnored, "", nil, nil, &at, false)
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
			thread_id TEXT NOT NULL DEFAULT '',
			active_turn_id TEXT NOT NULL DEFAULT '',
			last_input TEXT NOT NULL,
			last_output_summary TEXT NOT NULL,
			last_decision_action TEXT NOT NULL DEFAULT '',
			pending_request_id TEXT NOT NULL DEFAULT '',
			completion_check_status TEXT NOT NULL DEFAULT 'not_started',
			completion_check_sent_at TEXT,
			completion_check_done_at TEXT,
			awaiting_question TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS task_server_requests (
			request_id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			thread_id TEXT NOT NULL,
			turn_id TEXT NOT NULL,
			request_type TEXT NOT NULL,
			request_payload TEXT NOT NULL,
			status TEXT NOT NULL,
			decision_source TEXT NOT NULL DEFAULT '',
			reply_content TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			reply_started_at TEXT,
			replied_at TEXT,
			resolved_at TEXT
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
		`CREATE INDEX IF NOT EXISTS idx_task_server_requests_task_id_status ON task_server_requests(task_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_task_events_task_id ON task_events(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_questions_task_id ON task_questions(task_id)`,
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
	var completionStatus string
	var completionSentAt sql.NullString
	var completionDoneAt sql.NullString
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
		&task.ThreadID,
		&task.ActiveTurnID,
		&task.LastInput,
		&task.LastOutputSummary,
		&task.LastDecisionAction,
		&task.PendingRequestID,
		&completionStatus,
		&completionSentAt,
		&completionDoneAt,
		&awaitingQuestion,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return TaskRun{}, err
	}

	task.Status = TaskStatus(status)
	task.CompletionCheckStatus = CompletionCheckStatus(completionStatus)

	task.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return TaskRun{}, fmt.Errorf("parse created_at: %w", err)
	}
	task.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return TaskRun{}, fmt.Errorf("parse updated_at: %w", err)
	}
	task.CompletionCheckSentAt, err = parseOptionalTime(completionSentAt)
	if err != nil {
		return TaskRun{}, fmt.Errorf("parse completion_check_sent_at: %w", err)
	}
	task.CompletionCheckDoneAt, err = parseOptionalTime(completionDoneAt)
	if err != nil {
		return TaskRun{}, fmt.Errorf("parse completion_check_done_at: %w", err)
	}
	task.AwaitingQuestion, err = unmarshalAwaitingQuestion(awaitingQuestion)
	if err != nil {
		return TaskRun{}, err
	}

	return task, nil
}

func scanTaskServerRequest(scanner taskScanner) (TaskServerRequest, error) {
	var req TaskServerRequest
	var requestType string
	var status string
	var createdAt string
	var replyStartedAt sql.NullString
	var repliedAt sql.NullString
	var resolvedAt sql.NullString

	err := scanner.Scan(
		&req.RequestID,
		&req.TaskID,
		&req.ThreadID,
		&req.TurnID,
		&requestType,
		&req.RequestPayload,
		&status,
		&req.DecisionSource,
		&req.ReplyContent,
		&createdAt,
		&replyStartedAt,
		&repliedAt,
		&resolvedAt,
	)
	if err != nil {
		return TaskServerRequest{}, err
	}

	req.RequestType = ServerRequestType(requestType)
	req.Status = ServerRequestStatus(status)
	req.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return TaskServerRequest{}, fmt.Errorf("parse request created_at: %w", err)
	}
	req.ReplyStartedAt, err = parseOptionalTime(replyStartedAt)
	if err != nil {
		return TaskServerRequest{}, fmt.Errorf("parse reply_started_at: %w", err)
	}
	req.RepliedAt, err = parseOptionalTime(repliedAt)
	if err != nil {
		return TaskServerRequest{}, fmt.Errorf("parse replied_at: %w", err)
	}
	req.ResolvedAt, err = parseOptionalTime(resolvedAt)
	if err != nil {
		return TaskServerRequest{}, fmt.Errorf("parse resolved_at: %w", err)
	}
	return req, nil
}

func (s *Store) updateTaskServerRequestStatus(ctx context.Context, requestID string, status ServerRequestStatus, reply string, replyStartedAt, repliedAt, resolvedAt *time.Time, updateReply bool) error {
	query := `
		UPDATE task_server_requests
		SET status = ?,
			reply_started_at = COALESCE(?, reply_started_at),
			replied_at = COALESCE(?, replied_at),
			resolved_at = COALESCE(?, resolved_at),
			reply_content = CASE WHEN ? THEN ? ELSE reply_content END
		WHERE request_id = ?
	`
	result, err := s.db.ExecContext(ctx,
		query,
		status,
		formatOptionalTime(replyStartedAt),
		formatOptionalTime(repliedAt),
		formatOptionalTime(resolvedAt),
		updateReply,
		reply,
		requestID,
	)
	if err != nil {
		return fmt.Errorf("update task server request %q: %w", requestID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update task server request %q: rows affected: %w", requestID, err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
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

func formatOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseOptionalTime(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func IsTaskNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
