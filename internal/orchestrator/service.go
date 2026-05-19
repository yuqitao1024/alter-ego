package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	store     *Store
	registry  *Registry
	scheduler *Scheduler
	runner    RemoteRunner
	decider   DecisionEngine
	notifier  TaskNotifier

	now func() time.Time
}

const executingContinueReply = "Continue according to the already confirmed plan and current workflow. Do not reopen planning. If you truly need to change scope or approach, stop and ask the user explicitly."

type TaskNotifier interface {
	NotifyTaskQuestion(ctx context.Context, task TaskRun) error
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
		TaskID:       fmt.Sprintf("task-%d", now.UnixNano()),
		TemplateID:   template.ID,
		RepositoryID: template.Repository.ID,
		MachineID:    machineID,
		Status:       StatusPending,
		Phase:        TaskPhasePlanning,
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

	return task, nil
}

func (s *Service) TickOnce(ctx context.Context) error {
	tasks, err := s.store.ListActiveTasks(ctx)
	if err != nil {
		return fmt.Errorf("list active tasks: %w", err)
	}

	task, ok := s.scheduler.Next(tasks)
	if !ok {
		return nil
	}

	switch task.Status {
	case StatusPending:
		return s.moveTaskToPreparingWorkspace(ctx, task)
	case StatusPreparingWorkspace:
		return s.moveTaskToStartingSession(ctx, task)
	case StatusStartingSession:
		return s.startInteractiveSession(ctx, task)
	case StatusDetached:
		return s.recoverDetachedTask(ctx, task)
	case StatusRunning:
		return s.advanceRunningTask(ctx, task)
	default:
		return nil
	}
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
	session, err = s.runner.SendInteractiveInput(ctx, session, text)
	if err != nil {
		return fmt.Errorf("send task reply for %q: %w", taskID, err)
	}

	question := task.AwaitingQuestion
	task.Status = StatusRunning
	task.AwaitingQuestion = nil
	task.LastInput = text
	task.WorkflowStage = normalizeWorkflowStage(task.WorkflowStage, task.Phase)
	task.Phase = taskPhaseForWorkflowStage(task.WorkflowStage)
	applySessionToTask(&task, session)
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return fmt.Errorf("update replied task %q: %w", taskID, err)
	}
	if err := s.markAnsweredQuestion(ctx, task.TaskID, question, text); err != nil {
		return err
	}
	if err := s.appendEvent(ctx, task.TaskID, "user_input_applied", "user input applied to live session"); err != nil {
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

func (s *Service) Status(ctx context.Context, taskID string) (TaskRun, error) {
	return s.store.GetTask(ctx, taskID)
}

func (s *Service) moveTaskToPreparingWorkspace(ctx context.Context, task TaskRun) error {
	task.Status = StatusPreparingWorkspace
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "workspace_preparation_started", "preparing remote workspace")
}

func (s *Service) moveTaskToStartingSession(ctx context.Context, task TaskRun) error {
	task.Status = StatusStartingSession
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "session_starting", "starting app-server-backed codex session")
}

func (s *Service) startInteractiveSession(ctx context.Context, task TaskRun) error {
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
	task.RemoteWorkdir = coalesceString(session.Workdir, taskRepoWorkdir(template.Repository.RemoteWorkspaceRoot, task.TaskID))
	task.ThreadID = session.ThreadID
	task.ActiveTurnID = session.ActiveTurnID
	task.WorkflowStage = normalizeWorkflowStage(task.WorkflowStage, task.Phase)
	task.Phase = taskPhaseForWorkflowStage(task.WorkflowStage)
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "app_server_thread_started", fmt.Sprintf("app-server thread %s started", task.ThreadID))
}

func (s *Service) recoverDetachedTask(ctx context.Context, task TaskRun) error {
	session, err := ReconnectInteractiveSession(ctx, s.runner, task)
	if err != nil {
		if errors.Is(err, ErrRemoteCommandTimeout) {
			return s.appendEvent(ctx, task.TaskID, "task_reconnect_timeout", "reconnect probe timed out")
		}
		return err
	}

	task.Status = StatusRunning
	task.RemoteWorkdir = coalesceString(session.Workdir, task.RemoteWorkdir)
	task.ThreadID = coalesceString(session.ThreadID, task.ThreadID)
	task.ActiveTurnID = coalesceString(session.ActiveTurnID, task.ActiveTurnID)
	task.LastOutputSummary = coalesceString(session.LastOutputWindow.Summary, task.LastOutputSummary)
	task.WorkflowStage = normalizeWorkflowStage(task.WorkflowStage, task.Phase)
	task.Phase = taskPhaseForWorkflowStage(task.WorkflowStage)
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "task_reconnected", fmt.Sprintf("reconnected to app-server thread %s", task.ThreadID))
}

func (s *Service) advanceRunningTask(ctx context.Context, task TaskRun) error {
	task.WorkflowStage = normalizeWorkflowStage(task.WorkflowStage, task.Phase)
	task.Phase = taskPhaseForWorkflowStage(task.WorkflowStage)

	template, err := s.lookupTemplate(task.TemplateID)
	if err != nil {
		return err
	}

	workflowText, err := LoadWorkflow(template.ResolvedWorkflowPath)
	if err != nil {
		return err
	}

	window, err := s.runner.CaptureOutput(ctx, sessionFromTask(task))
	if err != nil {
		if errors.Is(err, ErrRemoteCommandTimeout) {
			return s.markTaskDetached(ctx, task, "remote session probe timed out")
		}
		if isRemoteSessionMissingError(err) {
			return s.markTaskDetached(ctx, task, "remote session no longer exists")
		}
		return fmt.Errorf("read remote output for task %q: %w", task.TaskID, err)
	}
	if strings.TrimSpace(window.Summary) != "" {
		task.LastOutputSummary = window.Summary
	}
	if shouldSkipDecisionForWorkingWindow(window) {
		task.Status = StatusRunning
		task.UpdatedAt = s.now()
		return s.store.UpdateTask(ctx, task)
	}

	result, err := s.decider.DecideNextStep(ctx, DecisionContext{
		Task:         task,
		WorkflowText: workflowText,
		UserRequest:  task.UserRequest,
		OutputWindow: window,
	})
	if err != nil {
		return fmt.Errorf("decide next step for task %q: %w", task.TaskID, err)
	}

	task.LastOutputSummary = coalesceString(result.Summary, task.LastOutputSummary)
	task.LastDecisionAction = result.Action
	result = s.enforcePhasePolicy(task, result, window)
	if result.WorkflowStage != "" {
		task.WorkflowStage = result.WorkflowStage
	}
	if result.NextPhase != "" {
		task.Phase = result.NextPhase
	} else {
		task.Phase = taskPhaseForWorkflowStage(task.WorkflowStage)
	}
	switch result.Action {
	case DecisionActionAskUser:
		question := result.Question
		if question == nil {
			question = &AwaitingQuestion{
				QuestionText:   firstNonEmpty(strings.TrimSpace(task.LastOutputSummary), strings.TrimSpace(window.Summary), strings.TrimSpace(window.RawOutput)),
				OptionsSummary: "",
				ContextExcerpt: strings.TrimSpace(window.Summary),
				QuestionType:   coalesceString(result.DecisionType, "missing_context"),
				AskedAt:        s.now(),
			}
		}
		task.Status = StatusWaitingUserInput
		task.AwaitingQuestion = question
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
	case DecisionActionReplyToCodex:
		reply := strings.TrimSpace(firstNonEmpty(result.CodexReply, result.NextInput))
		if task.Phase == TaskPhaseExecuting {
			reply = executingContinueReply
		}
		if reply != "" {
			session, err := s.runner.SendInteractiveInput(ctx, sessionFromTask(task), reply)
			if err != nil {
				if errors.Is(err, ErrRemoteCommandTimeout) {
					return s.markTaskDetached(ctx, task, "remote session reply timed out")
				}
				return fmt.Errorf("send remote input for task %q: %w", task.TaskID, err)
			}
			task.LastInput = reply
			applySessionToTask(&task, session)
		}
		task.Status = StatusRunning
		task.UpdatedAt = s.now()
		return s.store.UpdateTask(ctx, task)
	case DecisionActionCompleteTask:
		if err := s.runner.StopSession(ctx, sessionFromTask(task)); err != nil {
			return fmt.Errorf("stop completed task %q: %w", task.TaskID, err)
		}
		task.Status = StatusCompleted
		task.UpdatedAt = s.now()
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return err
		}
		return s.appendEvent(ctx, task.TaskID, "task_completed", coalesceString(result.Summary, "task completed"))
	case DecisionActionWait, "":
		task.Status = StatusRunning
		task.UpdatedAt = s.now()
		return s.store.UpdateTask(ctx, task)
	}
	return fmt.Errorf("unsupported decision action %q for task %q", result.Action, task.TaskID)
}

func (s *Service) markTaskDetached(ctx context.Context, task TaskRun, message string) error {
	task.Status = StatusDetached
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "task_detached", message)
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
	case strings.Contains(text, "can't find pane"):
		return true
	case strings.Contains(text, "can't find window"):
		return true
	case strings.Contains(text, "can't find session"):
		return true
	case strings.Contains(text, "no server running on"):
		return true
	case strings.Contains(text, "thread missing"):
		return true
	case strings.Contains(text, "thread") && strings.Contains(text, "not found"):
		return true
	default:
		return false
	}
}

func (s *Service) enforcePhasePolicy(task TaskRun, result DecisionResult, window OutputWindow) DecisionResult {
	currentPhase := normalizeTaskPhase(task)
	currentStage := normalizeWorkflowStage(task.WorkflowStage, task.Phase)

	if result.NextPhase == "" {
		result.NextPhase = currentPhase
	}
	if result.WorkflowStage == "" {
		result.WorkflowStage = currentStage
	}
	if result.NextPhase == "" {
		result.NextPhase = taskPhaseForWorkflowStage(result.WorkflowStage)
	}

	if currentPhase == TaskPhaseExecuting && result.NextPhase == TaskPhasePlanning {
		result.Action = DecisionActionAskUser
		result.DecisionType = firstNonEmpty(result.DecisionType, "scope_confirmation")
		result.NextPhase = TaskPhaseExecuting
		result.WorkflowStage = currentStage
		if result.Question == nil {
			result.Question = &AwaitingQuestion{
				QuestionText:   "Codex wants to reopen planning during execution. Reply in Lark only if you want to allow a return to planning.",
				OptionsSummary: "",
				ContextExcerpt: firstNonEmpty(strings.TrimSpace(window.Summary), strings.TrimSpace(task.LastOutputSummary)),
				QuestionType:   result.DecisionType,
				AskedAt:        s.now(),
			}
		}
	}

	return result
}

func normalizeTaskPhase(task TaskRun) TaskPhase {
	return taskPhaseForWorkflowStage(normalizeWorkflowStage(task.WorkflowStage, task.Phase))
}

func shouldSkipDecisionForWorkingWindow(window OutputWindow) bool {
	if !window.SessionState.CodexActive() {
		return false
	}

	text := strings.ToLower(strings.TrimSpace(window.RawOutput + "\n" + window.Summary))
	switch {
	case strings.Contains(text, "esc to interrupt"):
		return true
	case strings.Contains(text, "\nworking"):
		return true
	case strings.HasPrefix(text, "working"):
		return true
	case strings.Contains(text, " working "):
		return true
	case strings.Contains(text, "applying patch"):
		return true
	case strings.Contains(text, "running tests"):
		return true
	case strings.Contains(text, "reading file"):
		return true
	default:
		return false
	}
}

func normalizeWorkflowStage(stage WorkflowStage, phase TaskPhase) WorkflowStage {
	stage = normalizeWorkflowStageValue(string(stage))
	if stage != "" {
		return stage
	}
	return workflowStageForPhase(phase, "")
}

func workflowStageForPhase(phase TaskPhase, current WorkflowStage) WorkflowStage {
	current = normalizeWorkflowStageValue(string(current))
	if current != "" {
		return current
	}
	if phase == TaskPhaseExecuting {
		return WorkflowStageImplementation
	}
	return WorkflowStageRequirementDiscussion
}

func taskPhaseForWorkflowStage(stage WorkflowStage) TaskPhase {
	switch normalizeWorkflowStageValue(string(stage)) {
	case WorkflowStageImplementation, WorkflowStageVerification, WorkflowStageIntegration:
		return TaskPhaseExecuting
	default:
		return TaskPhasePlanning
	}
}
