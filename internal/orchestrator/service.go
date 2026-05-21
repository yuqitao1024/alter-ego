package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const completionCheckPrompt = "Please verify once, against the confirmed task scope only, whether all requested work is complete. Reply with either: 1) all requested work is complete, or 2) remaining work still exists."

type Service struct {
	store     *Store
	registry  *Registry
	scheduler *Scheduler
	runner    RemoteRunner
	decider   DecisionEngine
	notifier  TaskNotifier

	now func() time.Time
}

type TaskNotifier interface {
	NotifyTaskQuestion(ctx context.Context, task TaskRun) error
	NotifyTaskProgress(ctx context.Context, task TaskRun, message string) error
}

func NewService(store *Store, registry *Registry, scheduler *Scheduler, runner RemoteRunner, decider DecisionEngine) *Service {
	return &Service{
		store:     store,
		registry:  registry,
		scheduler: scheduler,
		runner:    runner,
		decider:   decider,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetNotifier(notifier TaskNotifier) {
	s.notifier = notifier
}

func (s *Service) StartTask(ctx context.Context, templateID, createdBy, userRequest string) (TaskRun, error) {
	template, err := s.lookupTemplate(templateID)
	if err != nil {
		return TaskRun{}, err
	}

	active, err := s.store.ListActiveTasks(ctx)
	if err != nil {
		return TaskRun{}, fmt.Errorf("list active tasks: %w", err)
	}

	machineID, err := SelectMachine(*template.Repository, active)
	if err != nil {
		return TaskRun{}, err
	}

	now := s.now()
	task := TaskRun{
		TaskID:                fmt.Sprintf("task-%d", now.UnixNano()),
		TemplateID:            template.ID,
		RepositoryID:          template.Repository.ID,
		MachineID:             machineID,
		Status:                StatusPending,
		UserRequest:           userRequest,
		CreatedBy:             createdBy,
		CompletionCheckStatus: CompletionCheckStatusNotStarted,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := s.store.CreateTask(ctx, task); err != nil {
		return TaskRun{}, fmt.Errorf("create task: %w", err)
	}
	if err := s.appendEvent(ctx, task.TaskID, "task_created", fmt.Sprintf("task created on machine %s", machineID)); err != nil {
		return TaskRun{}, err
	}

	if err := s.startPendingTask(ctx, task); err != nil {
		return TaskRun{}, err
	}
	return s.store.GetTask(ctx, task.TaskID)
}

func (s *Service) TickOnce(ctx context.Context) error {
	tasks, err := s.store.ListActiveTasks(ctx)
	if err != nil {
		return fmt.Errorf("list active tasks: %w", err)
	}

	for _, task := range tasks {
		switch task.Status {
		case StatusPending, StatusStarting:
			if err := s.startPendingTask(ctx, task); err != nil {
				return err
			}
		case StatusRecovering:
			if err := s.recoverTask(ctx, task); err != nil {
				return err
			}
		case StatusRunning:
			if err := s.handleProgressAndCompletionOnly(ctx, task); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Service) HandleRuntimeEvent(ctx context.Context, event RuntimeEvent) error {
	if strings.TrimSpace(event.ThreadID) == "" {
		return nil
	}

	task, err := s.store.GetTaskByThread(ctx, event.ThreadID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	if event.ResolvedRequestID != "" {
		if err := s.store.MarkTaskServerRequestResolved(ctx, event.ResolvedRequestID, s.now()); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if task.PendingRequestID == event.ResolvedRequestID {
			task.PendingRequestID = ""
			if task.Status == StatusWaitingUserInput {
				task.Status = StatusRunning
				task.AwaitingQuestion = nil
			}
			task.UpdatedAt = s.now()
			if err := s.store.UpdateTask(ctx, task); err != nil {
				return err
			}
		}
		return nil
	}

	if event.ServerRequest == nil {
		return nil
	}

	req := *event.ServerRequest
	req.TaskID = task.TaskID
	req.ThreadID = task.ThreadID
	existing, err := s.store.GetTaskServerRequest(ctx, req.RequestID)
	switch {
	case err == nil:
		if existing.Status == ServerRequestStatusReplied || existing.Status == ServerRequestStatusResolved || existing.Status == ServerRequestStatusIgnored {
			return nil
		}
		req.Status = existing.Status
	case errors.Is(err, sql.ErrNoRows):
		req.Status = ServerRequestStatusPending
	default:
		return err
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = s.now()
	}

	if err := s.store.UpsertTaskServerRequest(ctx, req); err != nil {
		return err
	}
	task.PendingRequestID = req.RequestID
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	if err := s.appendEvent(ctx, task.TaskID, "server_request_received", string(req.RequestType)); err != nil {
		return err
	}
	return s.handlePendingRequest(ctx, task.TaskID, req.RequestID)
}

func (s *Service) Reply(ctx context.Context, taskID, text string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status != StatusWaitingUserInput {
		return fmt.Errorf("task %q is not waiting for user input", taskID)
	}

	session := sessionFromTask(task)

	if task.PendingRequestID != "" {
		req, err := s.store.GetTaskServerRequest(ctx, task.PendingRequestID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if err := s.store.MarkTaskServerRequestReplying(ctx, task.PendingRequestID, s.now()); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err := s.runner.RespondToServerRequest(ctx, sessionFromTask(task), req, text); err != nil {
				return fmt.Errorf("respond to server request for %q: %w", taskID, err)
			}
		}
		if err := s.store.MarkTaskServerRequestReplied(ctx, task.PendingRequestID, text, s.now()); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	} else {
		session = sessionFromTask(task)
		session, err = s.runner.SendInteractiveInput(ctx, session, text)
		if err != nil {
			return fmt.Errorf("send task reply for %q: %w", taskID, err)
		}
	}

	question := task.AwaitingQuestion
	task.Status = StatusRunning
	task.AwaitingQuestion = nil
	task.LastInput = text
	applySessionToTask(&task, session)
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return fmt.Errorf("update replied task %q: %w", taskID, err)
	}
	if err := s.markAnsweredQuestion(ctx, task.TaskID, question, text); err != nil {
		return err
	}
	if err := s.appendEvent(ctx, task.TaskID, "user_input_applied", "user input applied to task"); err != nil {
		return err
	}

	return nil
}

func (s *Service) Stop(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	if err := s.runner.StopSession(ctx, sessionFromTask(task)); err != nil {
		return fmt.Errorf("stop task %q: %w", taskID, err)
	}

	task.Status = StatusStopped
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return fmt.Errorf("persist stopped task %q: %w", taskID, err)
	}
	if err := s.appendEvent(ctx, task.TaskID, "task_stopped", "task stopped by operator"); err != nil {
		return err
	}

	return nil
}

func (s *Service) List(ctx context.Context) ([]TaskRun, error) {
	return s.store.ListActiveTasks(ctx)
}

func (s *Service) ListAll(ctx context.Context) ([]TaskRun, error) {
	return s.store.ListTasks(ctx)
}

func (s *Service) Status(ctx context.Context, taskID string) (TaskRun, error) {
	return s.store.GetTask(ctx, taskID)
}

func (s *Service) Delete(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if !task.Status.IsDeletable() {
		return fmt.Errorf("task %q is not deletable in status %q", taskID, task.Status)
	}
	if err := s.store.DeleteTask(ctx, taskID); err != nil {
		return err
	}
	return s.appendEvent(ctx, taskID, "task_deleted", "task deleted by operator")
}

func (s *Service) startPendingTask(ctx context.Context, task TaskRun) error {
	if task.Status == StatusRunning {
		return nil
	}
	task.Status = StatusStarting
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}

	template, err := s.lookupTemplate(task.TemplateID)
	if err != nil {
		return err
	}

	workflowText, err := LoadWorkflow(template.ResolvedWorkflowPath)
	if err != nil {
		return err
	}

	machine, err := s.lookupMachine(task.MachineID)
	if err != nil {
		return err
	}

	session, err := s.runner.StartInteractiveSession(ctx, StartRequest{
		Machine:             *machine,
		RepositoryID:        template.Repository.ID,
		TaskID:              task.TaskID,
		RemoteRepoURL:       template.Repository.RemoteRepoURL,
		RemoteWorkspaceRoot: template.Repository.RemoteWorkspaceRoot,
		CheckoutBranch:      template.Repository.DefaultBranch,
		PreCloneBootstrap:   append([]string(nil), template.Repository.PreCloneBootstrap...),
		PostCloneBootstrap:  append([]string(nil), template.Repository.PostCloneBootstrap...),
		UserRequest:         task.UserRequest,
		WorkflowContent:     workflowText,
	})
	if err != nil {
		if errors.Is(err, ErrRemoteCommandTimeout) {
			task.Status = StatusFailed
			task.UpdatedAt = s.now()
			if updateErr := s.store.UpdateTask(ctx, task); updateErr != nil {
				return updateErr
			}
			return s.appendEvent(ctx, task.TaskID, "task_failed", "remote session startup timed out")
		}
		return fmt.Errorf("start remote session for task %q: %w", task.TaskID, err)
	}

	task.Status = StatusRunning
	task.RemoteWorkdir = coalesceString(session.Workdir, task.RemoteWorkdir)
	task.ThreadID = session.ThreadID
	task.ActiveTurnID = session.ActiveTurnID
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "app_server_thread_started", fmt.Sprintf("app-server thread %s started", task.ThreadID))
}

func (s *Service) recoverTask(ctx context.Context, task TaskRun) error {
	session, err := ReconnectInteractiveSession(ctx, s.runner, task)
	if err != nil {
		if errors.Is(err, ErrRemoteCommandTimeout) {
			return s.appendEvent(ctx, task.TaskID, "task_reconnect_timeout", "reconnect probe timed out")
		}
		if isRemoteSessionMissingError(err) {
			task.Status = StatusFailed
			task.UpdatedAt = s.now()
			if updateErr := s.store.UpdateTask(ctx, task); updateErr != nil {
				return updateErr
			}
			return s.appendEvent(ctx, task.TaskID, "task_failed", "codex thread is missing from app-server state; task marked failed for restart")
		}
		return err
	}

	task.Status = StatusRunning
	task.RemoteWorkdir = coalesceString(session.Workdir, task.RemoteWorkdir)
	task.ThreadID = coalesceString(session.ThreadID, task.ThreadID)
	task.ActiveTurnID = coalesceString(session.ActiveTurnID, task.ActiveTurnID)
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "task_reconnected", fmt.Sprintf("reconnected to app-server thread %s", task.ThreadID))
}

func (s *Service) handlePendingRequest(ctx context.Context, taskID, requestID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	req, err := s.store.GetTaskServerRequest(ctx, requestID)
	if err != nil {
		return err
	}
	if req.Status != ServerRequestStatusPending {
		return nil
	}

	decision, err := s.decider.ClassifySupervisorEvent(ctx, SupervisorContext{
		Task:    task,
		Request: req,
		Summary: task.LastOutputSummary,
	})
	if err != nil {
		return fmt.Errorf("classify supervisor request for task %q: %w", task.TaskID, err)
	}
	policy := ApplySupervisorPolicy(task, &req, decision)
	if !policy.AllowReply && !policy.EscalateToUser && decision.Classification != ClassificationIgnore {
		policy.EscalateToUser = true
	}

	switch {
	case policy.AllowReply:
		if err := s.store.MarkTaskServerRequestReplying(ctx, req.RequestID, s.now()); err != nil {
			return err
		}
		reply := strings.TrimSpace(policy.ReplyContent)
		if reply == "" {
			reply = "accept"
		}
		if err := s.runner.RespondToServerRequest(ctx, sessionFromTask(task), req, reply); err != nil {
			return fmt.Errorf("respond to server request for task %q: %w", task.TaskID, err)
		}
		task.Status = StatusRunning
		task.LastInput = reply
		task.UpdatedAt = s.now()
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return err
		}
		if err := s.store.MarkTaskServerRequestReplied(ctx, req.RequestID, reply, s.now()); err != nil {
			return err
		}
		return s.appendEvent(ctx, task.TaskID, "server_request_replied", string(req.RequestType))
	case policy.EscalateToUser:
		task.Status = StatusWaitingUserInput
		task.AwaitingQuestion = &AwaitingQuestion{
			QuestionText:   firstNonEmpty(policy.UserQuestion, decision.UserQuestion, task.LastOutputSummary, req.RequestPayload),
			OptionsSummary: "",
			ContextExcerpt: task.LastOutputSummary,
			QuestionType:   string(decision.Classification),
			AskedAt:        s.now(),
		}
		task.UpdatedAt = s.now()
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return err
		}
		if err := s.appendQuestion(ctx, task); err != nil {
			return err
		}
		if err := s.appendEvent(ctx, task.TaskID, "waiting_user_input", fmt.Sprintf("waiting for %s", task.AwaitingQuestion.QuestionType)); err != nil {
			return err
		}
		if s.notifier != nil {
			if err := s.notifier.NotifyTaskQuestion(ctx, task); err != nil {
				return fmt.Errorf("notify task question for %q: %w", task.TaskID, err)
			}
		}
		return nil
	default:
		if err := s.store.MarkTaskServerRequestIgnored(ctx, req.RequestID, s.now()); err != nil {
			return err
		}
		return s.appendEvent(ctx, task.TaskID, "server_request_ignored", string(req.RequestType))
	}
}

func (s *Service) handleProgressAndCompletionOnly(ctx context.Context, task TaskRun) error {
	window, err := s.runner.CaptureOutput(ctx, sessionFromTask(task))
	if err != nil {
		if errors.Is(err, ErrRemoteCommandTimeout) {
			return s.markTaskRecovering(ctx, task, "remote session probe timed out")
		}
		if isRemoteSessionMissingError(err) {
			return s.markTaskRecovering(ctx, task, "remote session no longer exists")
		}
		return fmt.Errorf("read remote output for task %q: %w", task.TaskID, err)
	}
	if strings.TrimSpace(window.Summary) != "" {
		task.LastOutputSummary = window.Summary
	}
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	if strings.TrimSpace(window.Summary) == "" {
		return nil
	}

	updatedTask, handled, err := s.maybeSendCompletionCheck(ctx, task, window.Summary)
	if err != nil {
		return err
	}
	if handled {
		task = updatedTask
	}

	return s.maybeNotifyProgress(ctx, task, window.Summary)
}

func (s *Service) maybeSendCompletionCheck(ctx context.Context, task TaskRun, summary string) (TaskRun, bool, error) {
	decision, err := s.decider.EvaluateCompletionSignal(ctx, task, summary)
	if err != nil {
		return task, false, err
	}

	switch task.CompletionCheckStatus {
	case CompletionCheckStatusNotStarted:
		if decision.CompletionDisposition != CompletionDispositionSignalComplete {
			return task, false, nil
		}

		now := s.now()
		task.CompletionCheckStatus = CompletionCheckStatusSent
		task.CompletionCheckSentAt = &now
		task.LastInput = completionCheckPrompt
		task.UpdatedAt = now
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return task, false, err
		}

		session, err := s.runner.SendInteractiveInput(ctx, sessionFromTask(task), completionCheckPrompt)
		if err != nil {
			return task, false, fmt.Errorf("send completion check for task %q: %w", task.TaskID, err)
		}
		applySessionToTask(&task, session)
		task.UpdatedAt = s.now()
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return task, false, err
		}
		if err := s.appendEvent(ctx, task.TaskID, "completion_check_sent", "sent one-time completion verification prompt"); err != nil {
			return task, false, err
		}
		return task, true, nil

	case CompletionCheckStatusSent:
		now := s.now()
		switch decision.CompletionDisposition {
		case CompletionDispositionConfirmedDone:
			task.CompletionCheckStatus = CompletionCheckStatusConfirmedDone
			task.CompletionCheckDoneAt = &now
			task.Status = StatusCompleted
			task.UpdatedAt = now
			if err := s.store.UpdateTask(ctx, task); err != nil {
				return task, false, err
			}
			if err := s.runner.StopSession(ctx, sessionFromTask(task)); err != nil && !errors.Is(err, ErrAppServerStopUnsupported) {
				return task, false, err
			}
			if err := s.appendEvent(ctx, task.TaskID, "task_completed", "codex confirmed the full task scope is complete"); err != nil {
				return task, false, err
			}
			return task, true, nil
		case CompletionDispositionReportedRemaining:
			task.CompletionCheckStatus = CompletionCheckStatusReportedPending
			task.CompletionCheckDoneAt = &now
			task.Status = StatusWaitingUserInput
			task.AwaitingQuestion = &AwaitingQuestion{
				QuestionText:   "Codex reports there is remaining work after the completion check. Reply with whether to continue the remaining work.",
				OptionsSummary: "",
				ContextExcerpt: summary,
				QuestionType:   "completion_follow_up",
				AskedAt:        now,
			}
			task.UpdatedAt = now
			if err := s.store.UpdateTask(ctx, task); err != nil {
				return task, false, err
			}
			if err := s.appendQuestion(ctx, task); err != nil {
				return task, false, err
			}
			if s.notifier != nil {
				if err := s.notifier.NotifyTaskQuestion(ctx, task); err != nil {
					return task, false, err
				}
			}
			if err := s.appendEvent(ctx, task.TaskID, "completion_check_reported_remaining", "codex reported remaining work after completion check"); err != nil {
				return task, false, err
			}
			return task, true, nil
		}
	}

	return task, false, nil
}

func (s *Service) maybeNotifyProgress(ctx context.Context, task TaskRun, summary string) error {
	if strings.TrimSpace(summary) == "" || s.notifier == nil {
		return nil
	}
	decision, err := s.decider.EvaluateProgressUpdate(ctx, task, summary)
	if err != nil {
		return err
	}
	if !decision.ShouldNotifyUser || strings.TrimSpace(decision.UserUpdate) == "" {
		return nil
	}
	return s.notifier.NotifyTaskProgress(ctx, task, decision.UserUpdate)
}

func (s *Service) markTaskRecovering(ctx context.Context, task TaskRun, message string) error {
	task.Status = StatusRecovering
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "task_recovering", message)
}

func (s *Service) lookupTemplate(templateID string) (*TemplateConfig, error) {
	template := s.registry.Templates[templateID]
	if template == nil {
		return nil, fmt.Errorf("unknown template %q", templateID)
	}
	if template.Repository == nil {
		return nil, fmt.Errorf("template %q is missing bound repository", templateID)
	}
	return template, nil
}

func (s *Service) lookupMachine(machineID string) (*MachineConfig, error) {
	machine := s.registry.Machines[machineID]
	if machine == nil {
		return nil, fmt.Errorf("unknown machine %q", machineID)
	}
	return machine, nil
}

func sessionFromTask(task TaskRun) RemoteSession {
	return RemoteSession{
		MachineID:    task.MachineID,
		Workdir:      task.RemoteWorkdir,
		ThreadID:     task.ThreadID,
		ActiveTurnID: task.ActiveTurnID,
	}
}

func applySessionToTask(task *TaskRun, session RemoteSession) {
	if task == nil {
		return
	}
	task.RemoteWorkdir = coalesceString(session.Workdir, task.RemoteWorkdir)
	task.ThreadID = coalesceString(session.ThreadID, task.ThreadID)
	task.ActiveTurnID = coalesceString(session.ActiveTurnID, task.ActiveTurnID)
}

func (s *Service) appendEvent(ctx context.Context, taskID, eventType, message string) error {
	if err := s.store.AppendEvent(ctx, TaskEvent{
		TaskID:    taskID,
		EventType: eventType,
		Message:   message,
		CreatedAt: s.now(),
	}); err != nil {
		return fmt.Errorf("append task event for %q: %w", taskID, err)
	}
	return nil
}

func (s *Service) appendQuestion(ctx context.Context, task TaskRun) error {
	if task.AwaitingQuestion == nil {
		return nil
	}
	if err := s.store.AppendQuestion(ctx, TaskQuestion{
		TaskID:         task.TaskID,
		QuestionType:   task.AwaitingQuestion.QuestionType,
		QuestionText:   task.AwaitingQuestion.QuestionText,
		OptionsSummary: task.AwaitingQuestion.OptionsSummary,
		ContextExcerpt: task.AwaitingQuestion.ContextExcerpt,
		AskedAt:        task.AwaitingQuestion.AskedAt,
	}); err != nil {
		return fmt.Errorf("append task question for %q: %w", task.TaskID, err)
	}
	return nil
}

func (s *Service) markAnsweredQuestion(ctx context.Context, taskID string, question *AwaitingQuestion, answerText string) error {
	if question == nil {
		return nil
	}
	answeredAt := s.now()
	if err := s.store.MarkQuestionAnswered(ctx, taskID, question.AskedAt, answeredAt, answerText); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("mark task question answered for %q: %w", taskID, err)
	}
	return nil
}

func isRemoteSessionMissingError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrAppServerThreadMissing) {
		return true
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "thread missing"):
		return true
	case strings.Contains(text, "thread") && strings.Contains(text, "not found"):
		return true
	default:
		return false
	}
}
