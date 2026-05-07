package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yuqitao1024/alter-ego/internal/agent"
	"github.com/yuqitao1024/alter-ego/internal/lark"
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
	handler := agent.NewRouter(commandHandler, chatHandler)

	adapter := lark.NewAdapter(larkCfg, handler)
	err = adapter.Start(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
