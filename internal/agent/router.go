package agent

import (
	"context"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type CommandExecutor interface {
	HandleCommand(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error)
}

type Router struct {
	command CommandExecutor
	chat    channel.Handler
}

func NewRouter(command CommandExecutor, chat channel.Handler) *Router {
	return &Router{
		command: command,
		chat:    chat,
	}
}

func (r *Router) HandleMessage(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	if strings.HasPrefix(strings.TrimSpace(event.Text), "/") {
		return r.command.HandleCommand(ctx, event)
	}
	return r.chat.HandleMessage(ctx, event)
}
