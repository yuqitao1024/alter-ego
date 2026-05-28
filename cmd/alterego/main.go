package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yuqitao1024/alter-ego/internal/agent"
	"github.com/yuqitao1024/alter-ego/internal/codexappserver"
	"github.com/yuqitao1024/alter-ego/internal/lark"
	"github.com/yuqitao1024/alter-ego/internal/orchestrator"
	"github.com/yuqitao1024/alter-ego/internal/web"
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
	webCfg, webEnabled, err := web.ConfigFromEnvOptional()
	if err != nil {
		return err
	}
	agentCfg := agent.ConfigFromEnv()
	var streamBroker *web.StreamBroker
	if webEnabled {
		streamBroker = web.NewStreamBroker()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sessions := agent.NewSessionStore(12)
	taskSubsystem, err := buildTaskSubsystem(ctx, taskSubsystemConfig{
		RegistryRoot:           taskRegistryRoot(),
		DBPath:                 taskDBPath(),
		Notifier:               lark.NewTaskNotifier(larkCfg),
		LLMConfig:              agentCfg,
		ProgressReportsEnabled: taskProgressReportsEnabled(),
	})
	if err != nil {
		return err
	}
	defer taskSubsystem.Close()
	if streamBroker != nil {
		taskSubsystem.Service.SetChangeHook(func(taskID string) {
			streamBroker.Publish(web.StreamEvent{
				Type:   "task_updated",
				TaskID: taskID,
			})
		})
	}
	go taskSubsystem.Run(ctx)

	commandHandler := agent.NewCommandHandler(agentCfg, sessions, taskSubsystem.MachineInstaller)
	chatHandler := agent.NewChatHandler(agentCfg, sessions, nil)

	handler := agent.NewRouter(commandHandler, taskSubsystem.TaskHandler, chatHandler)

	adapter := lark.NewAdapter(larkCfg, handler)
	callbackHandler := lark.NewCallbackHandler(adapter)
	httpHandler, listenAddr, err := buildHTTPHandler(larkCfg, webCfg, webEnabled, callbackHandler, taskSubsystem.Service, taskSubsystem.Registry, streamBroker)
	if err != nil {
		return err
	}
	if listenAddr != "" {
		go func() {
			if err := http.ListenAndServe(listenAddr, httpHandler); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("http server stopped: %v", err)
			}
		}()
	}
	err = adapter.Start(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func buildHTTPHandler(larkCfg lark.Config, webCfg web.Config, webEnabled bool, callbackHandler http.Handler, taskService web.TaskDashboardService, registry *orchestrator.Registry, streamBroker *web.StreamBroker) (http.Handler, string, error) {
	if !webEnabled {
		mux := http.NewServeMux()
		if callbackHandler != nil {
			mux.Handle("/lark/card/callback", callbackHandler)
		}
		return mux, larkCfg.CallbackListenAddr, nil
	}

	listenAddr := webCfg.ListenAddr
	if callbackListen := strings.TrimSpace(larkCfg.CallbackListenAddr); callbackListen != "" && !sameListenTarget(callbackListen, listenAddr) {
		return nil, "", fmt.Errorf("ALTER_EGO_LARK_CALLBACK_LISTEN_ADDR must match ALTER_EGO_WEB_LISTEN_ADDR when web is enabled")
	}

	oauth := web.LarkOAuthClient{
		AppID:       larkCfg.AppID,
		AppSecret:   larkCfg.AppSecret,
		BaseURL:     lark.OpenBaseURL(larkCfg.Domain),
		RedirectURI: strings.TrimRight(webCfg.PublicBaseURL, "/") + "/auth/lark/callback",
	}
	webHandler := web.NewHandler(webCfg, oauth, web.OrchestratorDashboardProvider{
		Service: taskService,
		Catalog: web.RegistryTemplateCatalog{Registry: registry},
	}, streamBroker)
	return web.NewRouter(webHandler, callbackHandler), listenAddr, nil
}

func sameListenTarget(a, b string) bool {
	if strings.TrimSpace(a) == strings.TrimSpace(b) {
		return true
	}

	hostA, portA, errA := splitListenAddr(a)
	hostB, portB, errB := splitListenAddr(b)
	if errA != nil || errB != nil {
		return false
	}
	if portA != portB {
		return false
	}
	return hostA == hostB || hostA == "" || hostB == ""
}

func splitListenAddr(addr string) (string, string, error) {
	if strings.HasPrefix(addr, ":") {
		return "", strings.TrimPrefix(addr, ":"), nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	return host, port, nil
}

type taskSubsystemConfig struct {
	RegistryRoot           string
	DBPath                 string
	Notifier               orchestrator.TaskNotifier
	LLMConfig              agent.Config
	ProgressReportsEnabled bool
}

type taskSubsystem struct {
	Registry         *orchestrator.Registry
	Store            *orchestrator.Store
	Runner           orchestrator.RemoteRunner
	Service          *orchestrator.Service
	TaskHandler      *agent.TaskCommandHandler
	MachineInstaller agent.MachineInitService
	Manager          io.Closer
}

const taskTickInterval = 2 * time.Minute

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

	manager := codexappserver.NewManager(codexappserver.ManagerOptions{})
	installer := codexappserver.NewInstaller(nil, func(machineID string) (codexappserver.MachineInstallConfig, error) {
		machine := registry.Machines[machineID]
		if machine == nil {
			return codexappserver.MachineInstallConfig{}, errors.New("unknown machine: " + machineID)
		}
		return codexappserver.MachineInstallConfig{
			MachineID:   machine.ID,
			Host:        machine.Host,
			Port:        machine.Port,
			SSHUser:     machine.User,
			RunUser:     machine.AppServerInstallUser,
			ListenHost:  machine.AppServerListenHost,
			ListenPort:  machine.AppServerListenPort,
			ServiceName: machine.AppServerServiceName,
			ShellInit:   append([]string(nil), machine.ShellInit...),
			WSToken:     machine.AppServerWSAuthToken,
		}, nil
	})

	runner := orchestrator.NewAppServerRunner(manager)
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
	service.SetProgressReportsEnabled(cfg.ProgressReportsEnabled)

	return &taskSubsystem{
		Registry:         registry,
		Store:            store,
		Runner:           runner,
		Service:          service,
		TaskHandler:      agent.NewTaskCommandHandler(service),
		MachineInstaller: installer,
		Manager:          manager,
	}, nil
}

func (s *taskSubsystem) Close() error {
	if s == nil {
		return nil
	}
	if s.Manager != nil {
		_ = s.Manager.Close()
	}
	if s.Store != nil {
		return s.Store.Close()
	}
	return nil
}

func (s *taskSubsystem) Run(ctx context.Context) {
	if s == nil || s.Service == nil {
		return
	}

	if err := s.Service.ResumeActiveTasks(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("task subsystem startup resume failed: %v", err)
	}

	ticker := time.NewTicker(taskTickInterval)
	defer ticker.Stop()
	eventCh := s.Runner.Events()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-eventCh:
			if err := s.Service.HandleRuntimeEvent(ctx, event); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("task subsystem runtime event failed: %v", err)
			}
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

func taskProgressReportsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ALTER_EGO_TASK_PROGRESS_REPORTS_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
