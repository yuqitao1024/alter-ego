package orchestrator

import (
	"context"
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

	task.Status = StatusRunning
	task.AwaitingQuestion = nil
	task.LastInput = text
	task.UpdatedAt = s.now()
	if err := s.store.UpdateTask(ctx, task); err != nil {
		return fmt.Errorf("update replied task %q: %w", taskID, err)
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
	return s.store.UpdateTask(ctx, task)
}

func (s *Service) moveTaskToStartingSession(ctx context.Context, task TaskRun) error {
	task.Status = StatusStartingSession
	task.UpdatedAt = s.now()
	return s.store.UpdateTask(ctx, task)
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
	return s.store.UpdateTask(ctx, task)
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
	task.UpdatedAt = s.now()
	return s.store.UpdateTask(ctx, task)
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

	result, err := s.decider.DecideNextStep(ctx, DecisionContext{
		Task:         task,
		WorkflowText: workflowText,
		UserRequest:  task.UserRequest,
	})
	if err != nil {
		return fmt.Errorf("decide next step for task %q: %w", task.TaskID, err)
	}

	task.LastOutputSummary = coalesceString(result.Summary, task.LastOutputSummary)
	if ShouldEscalateDecision(result.DecisionType) {
		task.Status = StatusWaitingUserInput
		task.AwaitingQuestion = result.Question
		task.UpdatedAt = s.now()
		if err := s.store.UpdateTask(ctx, task); err != nil {
			return err
		}
		if s.notifier != nil {
			if err := s.notifier.NotifyTaskQuestion(ctx, task); err != nil {
				return fmt.Errorf("notify task question for %q: %w", task.TaskID, err)
			}
		}
		return nil
	}

	if strings.TrimSpace(result.NextInput) != "" {
		if err := s.runner.SendInteractiveInput(ctx, sessionFromTask(task), result.NextInput); err != nil {
			return fmt.Errorf("send remote input for task %q: %w", task.TaskID, err)
		}
		task.LastInput = result.NextInput
	}

	task.UpdatedAt = s.now()
	return s.store.UpdateTask(ctx, task)
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
