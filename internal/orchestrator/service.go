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

const resolvedResponderCooldown = 10 * time.Second

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
	if err := s.runner.SendInteractiveInput(ctx, session, text); err != nil {
		return fmt.Errorf("send task reply for %q: %w", taskID, err)
	}

	question := task.AwaitingQuestion
	task.Status = StatusRunning
	task.AwaitingQuestion = nil
	task.LastInput = text
	task.LastResolvedResponderName = task.ActiveResponderName
	task.LastResolvedScreenDigest = task.ActiveResponderScreenDigest
	cooldownUntil := s.now().Add(resolvedResponderCooldown)
	task.ResponderCooldownUntil = &cooldownUntil
	task.ActiveResponderName = ""
	task.ActiveResponderScreenDigest = ""
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
	return s.appendEvent(ctx, task.TaskID, "session_starting", "starting tmux-backed codex session")
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
		return fmt.Errorf("start remote session for task %q: %w", task.TaskID, err)
	}

	task.Status = StatusRunning
	task.RemoteWorkdir = coalesceString(session.Workdir, taskRepoWorkdir(template.Repository.RemoteWorkspaceRoot, task.TaskID))
	task.TMUXSessionName = coalesceString(session.TMUXSessionName, defaultTMUXSessionName(task.TaskID))
	task.RemoteCodexSessionID = session.CodexSessionID
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "tmux_session_started", fmt.Sprintf("tmux session %s started", task.TMUXSessionName))
}

func (s *Service) recoverDetachedTask(ctx context.Context, task TaskRun) error {
	session, err := ReconnectInteractiveSession(ctx, s.runner, task)
	if err != nil {
		return err
	}

	task.Status = StatusRunning
	task.RemoteWorkdir = coalesceString(session.Workdir, task.RemoteWorkdir)
	task.TMUXSessionName = coalesceString(session.TMUXSessionName, task.TMUXSessionName)
	task.RemoteCodexSessionID = coalesceString(session.CodexSessionID, task.RemoteCodexSessionID)
	task.LastOutputSummary = coalesceString(session.LastOutputWindow.Summary, task.LastOutputSummary)
	task.ActiveResponderName = ""
	task.ActiveResponderScreenDigest = ""
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return err
	}
	return s.appendEvent(ctx, task.TaskID, "task_reconnected", fmt.Sprintf("reconnected to tmux session %s", task.TMUXSessionName))
}

func (s *Service) advanceRunningTask(ctx context.Context, task TaskRun) error {
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
		return fmt.Errorf("read remote output for task %q: %w", task.TaskID, err)
	}
	if strings.TrimSpace(window.Summary) != "" {
		task.LastOutputSummary = window.Summary
	}
	task.LastScreenDigest = ScreenDigest(window)

	if response := EvaluateTerminalResponse(task, window, s.now()); response.Handled {
		if s.shouldIgnoreResolvedResponder(task, response, task.LastScreenDigest) {
			task.Status = StatusRunning
			task.UpdatedAt = s.now()
			return s.store.UpdateTask(ctx, task)
		}
		if response.AutoInput != "" {
			if err := s.runner.SendInteractiveInput(ctx, sessionFromTask(task), response.AutoInput); err != nil {
				return fmt.Errorf("send terminal responder input for task %q: %w", task.TaskID, err)
			}
			task.ActiveResponderName = response.Name
			task.ActiveResponderScreenDigest = task.LastScreenDigest
			task.LastInput = response.AutoInput
			task.UpdatedAt = s.now()
			if err := s.store.UpdateTask(ctx, task); err != nil {
				return err
			}
			if err := s.appendEvent(ctx, task.TaskID, "terminal_responder_applied", response.Name); err != nil {
				return err
			}
			return nil
		}
		if response.Question != nil {
			task.Status = StatusWaitingUserInput
			task.ActiveResponderName = response.Name
			task.ActiveResponderScreenDigest = task.LastScreenDigest
			task.AwaitingQuestion = response.Question
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
		}
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
		task.ActiveResponderName = ""
		task.ActiveResponderScreenDigest = ""
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
		if reply != "" {
			if err := s.runner.SendInteractiveInput(ctx, sessionFromTask(task), reply); err != nil {
				return fmt.Errorf("send remote input for task %q: %w", task.TaskID, err)
			}
			task.LastInput = reply
		}
		task.ActiveResponderName = ""
		task.ActiveResponderScreenDigest = ""
		task.Status = StatusRunning
		task.UpdatedAt = s.now()
		return s.store.UpdateTask(ctx, task)
	case DecisionActionCompleteTask:
		if err := s.runner.StopSession(ctx, sessionFromTask(task)); err != nil {
			return fmt.Errorf("stop completed task %q: %w", task.TaskID, err)
		}
		task.Status = StatusCompleted
		task.ActiveResponderName = ""
		task.ActiveResponderScreenDigest = ""
		task.UpdatedAt = s.now()
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return err
		}
		return s.appendEvent(ctx, task.TaskID, "task_completed", coalesceString(result.Summary, "task completed"))
	case DecisionActionWait, "":
		task.ActiveResponderName = ""
		task.ActiveResponderScreenDigest = ""
		task.Status = StatusRunning
		task.UpdatedAt = s.now()
		return s.store.UpdateTask(ctx, task)
	}
	return fmt.Errorf("unsupported decision action %q for task %q", result.Action, task.TaskID)
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
		MachineID:       task.MachineID,
		Workdir:         task.RemoteWorkdir,
		TMUXSessionName: task.TMUXSessionName,
		CodexSessionID:  task.RemoteCodexSessionID,
	}
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

func (s *Service) shouldIgnoreResolvedResponder(task TaskRun, response TerminalResponse, digest string) bool {
	if response.Name == "" || digest == "" {
		return false
	}
	if task.LastResolvedResponderName != response.Name || task.LastResolvedScreenDigest != digest {
		return false
	}
	if task.ResponderCooldownUntil == nil {
		return false
	}
	return s.now().Before(*task.ResponderCooldownUntil)
}
