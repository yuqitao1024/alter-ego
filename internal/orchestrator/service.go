package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	store                  *Store
	registry               *Registry
	scheduler              *Scheduler
	runner                 RemoteRunner
	decider                DecisionEngine
	notifier               TaskNotifier
	changeHook             func(taskID string)
	progressReportsEnabled bool

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

func (s *Service) SetProgressReportsEnabled(enabled bool) {
	s.progressReportsEnabled = enabled
}

func (s *Service) SetChangeHook(hook func(taskID string)) {
	s.changeHook = hook
}

func (s *Service) notifyChanged(taskID string) {
	if s == nil || s.changeHook == nil {
		return
	}
	s.changeHook(strings.TrimSpace(taskID))
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
		TaskID:       newTaskID(now),
		TemplateID:   template.ID,
		RepositoryID: template.Repository.ID,
		MachineID:    machineID,
		Status:       StatusPending,
		UserRequest:  userRequest,
		CreatedBy:    createdBy,
		CreatedAt:    now,
		UpdatedAt:    now,
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
	s.notifyChanged(task.TaskID)
	return s.store.GetTask(ctx, task.TaskID)
}

func newTaskID(now time.Time) string {
	return "task-" + strconv.FormatInt(now.UnixMilli(), 36)
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

	if len(tasks) > 0 {
		s.notifyChanged("")
	}
	return nil
}

func (s *Service) ResumeActiveTasks(ctx context.Context) error {
	tasks, err := s.store.ListActiveTasks(ctx)
	if err != nil {
		return fmt.Errorf("list active tasks: %w", err)
	}

	var firstErr error
	for _, task := range tasks {
		refreshed, err := s.resumeActiveTask(ctx, task)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := s.reconcilePersistedRequests(ctx, refreshed); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	if len(tasks) > 0 {
		s.notifyChanged("")
	}
	return firstErr
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
			s.notifyChanged(task.TaskID)
		}
		s.notifyChanged(task.TaskID)
		return nil
	}

	if event.ServerRequest == nil {
		if event.TurnCompleted != nil {
			updatedTask, err := s.handleTurnCompleted(ctx, task, *event.TurnCompleted)
			if err != nil {
				return err
			}
			s.notifyChanged(updatedTask.TaskID)
			return nil
		}
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
	if err := s.handlePendingRequest(ctx, task.TaskID, req.RequestID); err != nil {
		return err
	}
	s.notifyChanged(task.TaskID)
	return nil
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
	s.notifyChanged(task.TaskID)

	return nil
}

func (s *Service) Stop(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if !task.Status.IsStoppable() {
		return fmt.Errorf("task %q is %s and cannot be stopped", taskID, task.Status)
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
	s.notifyChanged(task.TaskID)
	return nil
}

func (s *Service) Complete(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status != StatusWaitingUserInput {
		return fmt.Errorf("task %q is not waiting for user input", taskID)
	}

	if err := s.runner.StopSession(ctx, sessionFromTask(task)); err != nil && !errors.Is(err, ErrAppServerStopUnsupported) {
		if appendErr := s.appendEvent(ctx, task.TaskID, "task_complete_interrupt_skipped", err.Error()); appendErr != nil {
			return appendErr
		}
	}

	if task.PendingRequestID != "" {
		if err := s.store.MarkTaskServerRequestResolved(ctx, task.PendingRequestID, s.now()); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		task.PendingRequestID = ""
	}

	question := task.AwaitingQuestion
	task.Status = StatusCompleted
	task.AwaitingQuestion = nil
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return fmt.Errorf("persist completed task %q: %w", taskID, err)
	}
	if err := s.markAnsweredQuestion(ctx, task.TaskID, question, "task complete"); err != nil {
		return err
	}
	if err := s.appendEvent(ctx, task.TaskID, "task_completed", "task marked completed by operator"); err != nil {
		return err
	}
	s.notifyChanged(task.TaskID)
	return nil
}

func (s *Service) Reopen(ctx context.Context, taskID, extraRequirement string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if !task.Status.IsReopenable() {
		return fmt.Errorf("task %q is %s and cannot be reopened", taskID, task.Status)
	}

	requirement := strings.TrimSpace(extraRequirement)
	if requirement == "" {
		return fmt.Errorf("task %q reopen requirement cannot be empty", taskID)
	}

	session, err := s.runner.SendInteractiveInput(ctx, sessionFromTask(task), requirement)
	if err != nil {
		return fmt.Errorf("reopen task %q: %w", taskID, err)
	}

	if task.PendingRequestID != "" {
		if err := s.store.MarkTaskServerRequestResolved(ctx, task.PendingRequestID, s.now()); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		task.PendingRequestID = ""
	}

	task.Status = StatusRunning
	task.AwaitingQuestion = nil
	task.LastInput = requirement
	applySessionToTask(&task, session)
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return fmt.Errorf("persist reopened task %q: %w", taskID, err)
	}
	if err := s.appendEvent(ctx, task.TaskID, "task_reopened", "task reopened with additional requirement"); err != nil {
		return err
	}
	s.notifyChanged(task.TaskID)
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

func (s *Service) Dashboard(ctx context.Context) (DashboardSnapshot, error) {
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		return DashboardSnapshot{}, err
	}

	snapshot := DashboardSnapshot{
		Tasks: make([]DashboardTask, 0, len(tasks)),
	}
	for i := len(tasks) - 1; i >= 0; i-- {
		task := tasks[i]
		snapshot.Summary.Total++
		switch task.Status {
		case StatusPending:
			snapshot.Summary.Pending++
		case StatusStarting:
			snapshot.Summary.Starting++
		case StatusRunning:
			snapshot.Summary.Running++
		case StatusWaitingUserInput:
			snapshot.Summary.WaitingUserInput++
		case StatusRecovering:
			snapshot.Summary.Recovering++
		case StatusCompleted:
			snapshot.Summary.Completed++
		case StatusFailed:
			snapshot.Summary.Failed++
		case StatusStopped:
			snapshot.Summary.Stopped++
		}

		events, err := s.store.ListEvents(ctx, task.TaskID)
		if err != nil {
			return DashboardSnapshot{}, err
		}

		dashboardTask := DashboardTask{
			ID:            task.TaskID,
			Title:         firstNonEmpty(task.UserRequest, task.TemplateID, task.TaskID),
			Status:        task.Status,
			TemplateID:    task.TemplateID,
			RepositoryID:  task.RepositoryID,
			MachineID:     task.MachineID,
			ThreadID:      task.ThreadID,
			Summary:       task.LastOutputSummary,
			LastInput:     task.LastInput,
			LastUpdatedAt: task.UpdatedAt,
			CreatedAt:     task.CreatedAt,
			RecentEvents:  make([]DashboardTaskEvent, 0, minInt(3, len(events))),
		}
		if task.AwaitingQuestion != nil {
			dashboardTask.AwaitingQuestion = &DashboardQuestion{
				QuestionType:   task.AwaitingQuestion.QuestionType,
				QuestionText:   task.AwaitingQuestion.QuestionText,
				OptionsSummary: task.AwaitingQuestion.OptionsSummary,
				ContextExcerpt: task.AwaitingQuestion.ContextExcerpt,
				AskedAt:        task.AwaitingQuestion.AskedAt,
			}
		}
		for idx := len(events) - 1; idx >= 0 && len(dashboardTask.RecentEvents) < 3; idx-- {
			event := events[idx]
			dashboardTask.RecentEvents = append(dashboardTask.RecentEvents, DashboardTaskEvent{
				EventType: event.EventType,
				Message:   event.Message,
				CreatedAt: event.CreatedAt,
			})
		}
		snapshot.Tasks = append(snapshot.Tasks, dashboardTask)
	}

	return snapshot, nil
}

func (s *Service) TaskDetail(ctx context.Context, taskID string) (DashboardTaskDetail, error) {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return DashboardTaskDetail{}, err
	}

	events, err := s.store.ListEvents(ctx, taskID)
	if err != nil {
		return DashboardTaskDetail{}, err
	}
	questions, err := s.store.ListQuestions(ctx, taskID)
	if err != nil {
		return DashboardTaskDetail{}, err
	}

	detail := DashboardTaskDetail{
		ID:            task.TaskID,
		Title:         firstNonEmpty(task.UserRequest, task.TemplateID, task.TaskID),
		Status:        task.Status,
		TemplateID:    task.TemplateID,
		RepositoryID:  task.RepositoryID,
		MachineID:     task.MachineID,
		ThreadID:      task.ThreadID,
		Summary:       task.LastOutputSummary,
		LastInput:     task.LastInput,
		LastUpdatedAt: task.UpdatedAt,
		CreatedAt:     task.CreatedAt,
		Events:        make([]DashboardTaskEvent, 0, len(events)),
		Questions:     make([]DashboardTaskQuestion, 0, len(questions)),
	}
	if task.AwaitingQuestion != nil {
		detail.AwaitingQuestion = &DashboardQuestion{
			QuestionType:   task.AwaitingQuestion.QuestionType,
			QuestionText:   task.AwaitingQuestion.QuestionText,
			OptionsSummary: task.AwaitingQuestion.OptionsSummary,
			ContextExcerpt: task.AwaitingQuestion.ContextExcerpt,
			AskedAt:        task.AwaitingQuestion.AskedAt,
		}
	}

	for _, event := range events {
		detail.Events = append(detail.Events, DashboardTaskEvent{
			EventType: event.EventType,
			Message:   event.Message,
			CreatedAt: event.CreatedAt,
		})
	}
	for _, question := range questions {
		detail.Questions = append(detail.Questions, DashboardTaskQuestion{
			QuestionType:   question.QuestionType,
			QuestionText:   question.QuestionText,
			OptionsSummary: question.OptionsSummary,
			ContextExcerpt: question.ContextExcerpt,
			AskedAt:        question.AskedAt,
			AnsweredAt:     question.AnsweredAt,
			AnswerText:     question.AnswerText,
		})
	}

	return detail, nil
}

func (s *Service) Delete(ctx context.Context, taskID string) error {
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if !task.Status.IsDeletable() {
		return fmt.Errorf("task %q is not deletable in status %q", taskID, task.Status)
	}
	if err := s.runner.CleanupSession(ctx, sessionFromTask(task)); err != nil && !errors.Is(err, ErrAppServerStopUnsupported) {
		return fmt.Errorf("cleanup app-server session for task %q: %w", taskID, err)
	}
	if err := s.store.DeleteTask(ctx, taskID); err != nil {
		return err
	}
	if err := s.deleteTaskWorkspace(ctx, task); err != nil {
		return err
	}
	s.notifyChanged(taskID)
	return nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Service) DeleteTerminalTasks(ctx context.Context) (int, error) {
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, task := range tasks {
		if !task.Status.IsDeletable() {
			continue
		}
		if err := s.Delete(ctx, task.TaskID); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (s *Service) deleteTaskWorkspace(ctx context.Context, task TaskRun) error {
	repository := s.registry.Repositories[task.RepositoryID]
	if repository == nil {
		return fmt.Errorf("unknown repository %q for task %q", task.RepositoryID, task.TaskID)
	}
	machine := s.registry.Machines[task.MachineID]
	if machine == nil {
		return fmt.Errorf("unknown machine %q for task %q", task.MachineID, task.TaskID)
	}
	return s.runner.DeleteTaskWorkspace(ctx, DeleteWorkspaceRequest{
		Machine:             *machine,
		TaskID:              task.TaskID,
		RemoteWorkspaceRoot: repository.RemoteWorkspaceRoot,
	})
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
	task, err := s.reconnectTaskSession(ctx, task)
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
	if !policy.AllowReply && !policy.EscalateToUser {
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

	if task.PendingRequestID == "" && task.Status != StatusWaitingUserInput && window.SessionState.WaitingOnExternalInput() {
		return s.transitionTaskToRecoveredWaitingState(ctx, task, window)
	}

	if strings.TrimSpace(window.Summary) == "" {
		return nil
	}
	return s.maybeNotifyProgress(ctx, task, window.Summary)
}

func (s *Service) resumeActiveTask(ctx context.Context, task TaskRun) (TaskRun, error) {
	switch task.Status {
	case StatusRunning, StatusRecovering, StatusWaitingUserInput:
		if strings.TrimSpace(task.ThreadID) == "" || strings.TrimSpace(task.RemoteWorkdir) == "" {
			return task, nil
		}
		return s.reconnectTaskSession(ctx, task)
	default:
		return task, nil
	}
}

func (s *Service) reconnectTaskSession(ctx context.Context, task TaskRun) (TaskRun, error) {
	session, err := ReconnectInteractiveSession(ctx, s.runner, task)
	if err != nil {
		return task, err
	}

	if task.Status == StatusRecovering {
		task.Status = StatusRunning
	}
	task.RemoteWorkdir = coalesceString(session.Workdir, task.RemoteWorkdir)
	task.ThreadID = coalesceString(session.ThreadID, task.ThreadID)
	task.ActiveTurnID = coalesceString(session.ActiveTurnID, task.ActiveTurnID)
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return task, err
	}
	return task, nil
}

func (s *Service) reconcilePersistedRequests(ctx context.Context, task TaskRun) error {
	if task.Status == StatusWaitingUserInput && task.AwaitingQuestion != nil {
		return s.renotifyWaitingTask(ctx, task)
	}

	if task.PendingRequestID != "" {
		req, err := s.store.GetTaskServerRequest(ctx, task.PendingRequestID)
		if err == nil {
			return s.reconcilePersistedRequest(ctx, task, req)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}

	requests, err := s.store.ListOpenTaskServerRequests(ctx, task.TaskID)
	if err != nil {
		return err
	}
	if len(requests) == 0 {
		return s.reconcileCompletedTurnAfterResume(ctx, task)
	}

	task.PendingRequestID = requests[0].RequestID
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.reconcilePersistedRequest(ctx, task, requests[0])
}

func (s *Service) renotifyWaitingTask(ctx context.Context, task TaskRun) error {
	if s.notifier == nil || task.AwaitingQuestion == nil {
		return nil
	}
	return s.notifier.NotifyTaskQuestion(ctx, task)
}

func (s *Service) reconcilePersistedRequest(ctx context.Context, task TaskRun, req TaskServerRequest) error {
	if task.Status == StatusWaitingUserInput && task.AwaitingQuestion != nil {
		return nil
	}

	switch req.Status {
	case ServerRequestStatusPending:
		return s.handlePendingRequest(ctx, task.TaskID, req.RequestID)
	case ServerRequestStatusReplying:
		now := s.now()
		task.Status = StatusWaitingUserInput
		task.AwaitingQuestion = &AwaitingQuestion{
			QuestionText:   "Task recovery found an in-flight Codex request that was interrupted during restart. Review the context and reply to continue recovery.",
			OptionsSummary: "",
			ContextExcerpt: firstNonEmpty(task.LastOutputSummary, req.RequestPayload),
			QuestionType:   "recovery_interrupted_request",
			AskedAt:        now,
		}
		task.UpdatedAt = now
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return err
		}
		if err := s.appendQuestion(ctx, task); err != nil {
			return err
		}
		if s.notifier != nil {
			if err := s.notifier.NotifyTaskQuestion(ctx, task); err != nil {
				return err
			}
		}
		return s.appendEvent(ctx, task.TaskID, "waiting_user_input", "waiting for recovery of interrupted Codex request")
	default:
		return nil
	}
}

func (s *Service) transitionTaskToRecoveredWaitingState(ctx context.Context, task TaskRun, window OutputWindow) error {
	now := s.now()
	questionText := "Codex is waiting for further input after recovery. The original waiting event was not available, so reply here to continue the task."
	for _, flag := range window.SessionState.ThreadActiveFlags {
		if strings.EqualFold(strings.TrimSpace(flag), "waitingOnApproval") {
			questionText = "Codex is waiting for an approval decision after recovery. The original approval event was not available, so decide whether to continue and reply here to attempt recovery."
			break
		}
	}
	task.Status = StatusWaitingUserInput
	task.AwaitingQuestion = &AwaitingQuestion{
		QuestionText:   questionText,
		OptionsSummary: "",
		ContextExcerpt: firstNonEmpty(window.Summary, task.LastOutputSummary),
		QuestionType:   "recovered_waiting_input",
		AskedAt:        now,
	}
	task.UpdatedAt = now
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	if err := s.appendQuestion(ctx, task); err != nil {
		return err
	}
	if s.notifier != nil {
		if err := s.notifier.NotifyTaskQuestion(ctx, task); err != nil {
			return err
		}
	}
	return s.appendEvent(ctx, task.TaskID, "waiting_user_input", "codex thread is waiting for input after recovery")
}

func (s *Service) handleTurnCompleted(ctx context.Context, task TaskRun, turn TurnCompletedEvent) (TaskRun, error) {
	turnID := strings.TrimSpace(turn.TurnID)
	if turnID == "" || task.LastCompletedTurnID == turnID || task.PendingRequestID != "" {
		return task, nil
	}
	task.ActiveTurnID = turnID

	summary := strings.TrimSpace(turn.Summary)
	if summary != "" {
		task.LastOutputSummary = summary
	}

	decision, err := s.decider.ClassifySupervisorEvent(ctx, SupervisorContext{
		Task:          task,
		TurnCompleted: &turn,
		EventType:     "turn_completed",
		Summary:       summary,
	})
	if err != nil {
		return task, fmt.Errorf("classify completed turn for task %q: %w", task.TaskID, err)
	}

	switch decision.Classification {
	case ClassificationPlanDecision, ClassificationExecutionApproval:
	default:
		decision = SupervisorDecision{
			Classification: ClassificationPlanDecision,
			ReplyPolicy:    ReplyPolicyAskUser,
			UserQuestion:   firstNonEmpty(decision.UserQuestion, summary),
		}
	}
	return s.applyTurnDecision(ctx, task, turnID, summary, decision)
}

func (s *Service) applyTurnDecision(ctx context.Context, task TaskRun, turnID string, summary string, decision SupervisorDecision) (TaskRun, error) {
	if decision.ShouldReplyCodex && decision.ReplyPolicy == ReplyPolicyAutoContinue {
		reply := strings.TrimSpace(decision.CodexReply)
		if reply == "" {
			reply = "continue"
		}
		session, err := s.runner.SendInteractiveInput(ctx, sessionFromTask(task), reply)
		if err != nil {
			return task, fmt.Errorf("send completed-turn reply for task %q: %w", task.TaskID, err)
		}
		task.Status = StatusRunning
		task.LastInput = reply
		task.LastCompletedTurnID = turnID
		applySessionToTask(&task, session)
		task.UpdatedAt = s.now()
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return task, err
		}
		if err := s.appendEvent(ctx, task.TaskID, "turn_completed_replied", reply); err != nil {
			return task, err
		}
		return task, nil
	}

	if decision.ReplyPolicy == ReplyPolicyAskUser {
		task.Status = StatusWaitingUserInput
		task.AwaitingQuestion = &AwaitingQuestion{
			QuestionText:   firstNonEmpty(decision.UserQuestion, summary),
			OptionsSummary: "",
			ContextExcerpt: summary,
			QuestionType:   string(decision.Classification),
			AskedAt:        s.now(),
		}
		task.LastCompletedTurnID = turnID
		task.UpdatedAt = s.now()
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return task, err
		}
		if err := s.appendQuestion(ctx, task); err != nil {
			return task, err
		}
		if err := s.appendEvent(ctx, task.TaskID, "waiting_user_input", fmt.Sprintf("waiting for %s", task.AwaitingQuestion.QuestionType)); err != nil {
			return task, err
		}
		if s.notifier != nil {
			if err := s.notifier.NotifyTaskQuestion(ctx, task); err != nil {
				return task, err
			}
		}
		return task, nil
	}

	task.LastCompletedTurnID = turnID
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return task, err
	}
	return task, nil
}

func (s *Service) reconcileCompletedTurnAfterResume(ctx context.Context, task TaskRun) error {
	snapshot, ok := s.runner.Snapshot(task.MachineID, task.ThreadID)
	if !ok {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(snapshot.ActiveTurnStatus), "completed") {
		return nil
	}
	if strings.TrimSpace(snapshot.ActiveTurnID) == "" || task.LastCompletedTurnID == snapshot.ActiveTurnID {
		return nil
	}
	if task.PendingRequestID != "" || task.Status == StatusWaitingUserInput {
		return nil
	}

	_, err := s.handleTurnCompleted(ctx, task, TurnCompletedEvent{
		ThreadID:          task.ThreadID,
		TurnID:            snapshot.ActiveTurnID,
		Summary:           snapshot.LatestSummary,
		ThreadStatus:      snapshot.ThreadStatus,
		ThreadActiveFlags: append([]string(nil), snapshot.ThreadActiveFlags...),
	})
	return err
}

func (s *Service) maybeNotifyProgress(ctx context.Context, task TaskRun, summary string) error {
	if !s.progressReportsEnabled || strings.TrimSpace(summary) == "" || s.notifier == nil {
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
