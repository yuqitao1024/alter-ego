package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/yuqitao1024/alter-ego/internal/agent"
	"github.com/yuqitao1024/alter-ego/internal/lark"
	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	larkCfg, err := lark.ConfigFromEnv()
	if err != nil {
		return err
	}
	agentCfg := agent.ConfigFromEnv()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sessions := agent.NewSessionStore(12)
	commandHandler := agent.NewCommandHandler(agentCfg, sessions)
	chatHandler := agent.NewChatHandler(agentCfg, sessions, nil)

	taskSubsystem, err := buildTaskSubsystem(ctx, taskSubsystemConfig{
		RegistryRoot: taskRegistryRoot(),
		DBPath:       taskDBPath(),
		Notifier:     lark.NewTaskNotifier(larkCfg),
		LLMConfig:    agentCfg,
	})
	if err != nil {
		return err
	}
	defer taskSubsystem.Close()
	go taskSubsystem.Run(ctx)

	handler := agent.NewRouter(commandHandler, taskSubsystem.TaskHandler, chatHandler)

	adapter := lark.NewAdapter(larkCfg, handler)
	err = adapter.Start(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

type taskSubsystemConfig struct {
	RegistryRoot string
	DBPath       string
	Notifier     orchestrator.TaskNotifier
	LLMConfig    agent.Config
}

type taskSubsystem struct {
	Registry    *orchestrator.Registry
	Store       *orchestrator.Store
	Runner      *orchestrator.SSHRunner
	Service     *orchestrator.Service
	TaskHandler *agent.TaskCommandHandler
}

func buildTaskSubsystem(ctx context.Context, cfg taskSubsystemConfig) (*taskSubsystem, error) {
	_ = ctx

	registry, err := orchestrator.LoadRegistry(cfg.RegistryRoot)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, err
	}

	store, err := orchestrator.OpenStore(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	runner := orchestrator.NewSSHRunner(nil)
	runner.SetMachineResolver(func(machineID string) (orchestrator.MachineConfig, error) {
		machine := registry.Machines[machineID]
		if machine == nil {
			return orchestrator.MachineConfig{}, errors.New("unknown machine: " + machineID)
		}
		return *machine, nil
	})

	decider, err := buildDecisionEngine(cfg.LLMConfig)
	if err != nil {
		return nil, err
	}

	service := orchestrator.NewService(
		store,
		registry,
		orchestrator.NewScheduler(),
		runner,
		decider,
	)
	service.SetNotifier(cfg.Notifier)

	return &taskSubsystem{
		Registry:    registry,
		Store:       store,
		Runner:      runner,
		Service:     service,
		TaskHandler: agent.NewTaskCommandHandler(service),
	}, nil
}

func (s *taskSubsystem) Close() error {
	if s == nil || s.Store == nil {
		return nil
	}
	return s.Store.Close()
}

func (s *taskSubsystem) Run(ctx context.Context) {
	if s == nil || s.Service == nil {
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Service.TickOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("task subsystem tick failed: %v", err)
			}
		}
	}
}

func taskRegistryRoot() string {
	if root := os.Getenv("ALTER_EGO_TASK_CONFIG_ROOT"); root != "" {
		return root
	}
	return "."
}

func taskDBPath() string {
	if path := os.Getenv("ALTER_EGO_TASK_DB_PATH"); path != "" {
		return path
	}
	return ".alterego/tasks.db"
}

type decisionModelAdapter struct {
	model    string
	provider agent.Provider
}

func (a decisionModelAdapter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return a.provider.CreateResponse(ctx, agent.ChatRequest{
		Model: a.model,
		Messages: []agent.ChatMessage{
			{Role: a.provider.SystemRole(), Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
}

func buildDecisionEngine(cfg agent.Config) (orchestrator.DecisionEngine, error) {
	if cfg.APIKey == "" || cfg.Model == "" {
		return nil, errors.New("task orchestration requires ALTER_EGO_LLM_API_KEY and ALTER_EGO_LLM_MODEL")
	}
	provider := agent.NewProvider(cfg, nil)
	if provider == nil {
		return nil, errors.New("task orchestration decision provider is not available")
	}
	return orchestrator.NewModelDecisionEngine(decisionModelAdapter{
		model:    cfg.Model,
		provider: provider,
	}), nil
}
