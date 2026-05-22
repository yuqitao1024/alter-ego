package agent

import (
	"context"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type CommandExecutor interface {
	HandleCommand(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error)
}

type CardActionExecutor interface {
	HandleCardAction(ctx context.Context, event channel.CardActionEvent) (channel.CardActionResponse, error)
}

type Router struct {
	command CommandExecutor
	task    CommandExecutor
	chat    channel.Handler
}

func NewRouter(command, task CommandExecutor, chat channel.Handler) *Router {
	return &Router{
		command: command,
		task:    task,
		chat:    chat,
	}
}

func (r *Router) HandleMessage(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	commandText := strings.TrimSpace(event.Text)
	if strings.HasPrefix(commandText, "/task") && r.task != nil {
		return r.task.HandleCommand(ctx, event)
	}
	if strings.HasPrefix(commandText, "/") {
		return r.command.HandleCommand(ctx, event)
	}
	return r.chat.HandleMessage(ctx, event)
}

func (r *Router) HandleCardAction(ctx context.Context, event channel.CardActionEvent) (channel.CardActionResponse, error) {
	handler, ok := r.task.(CardActionExecutor)
	if !ok || handler == nil {
		return channel.CardActionResponse{ToastText: "Task card actions are not configured."}, nil
	}
	return handler.HandleCardAction(ctx, event)
}
