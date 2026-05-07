package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type CommandHandler struct {
	cfg      Config
	sessions *SessionStore
}

func NewCommandHandler(cfg Config, sessions *SessionStore) *CommandHandler {
	return &CommandHandler{
		cfg:      cfg,
		sessions: sessions,
	}
}

func (h *CommandHandler) HandleCommand(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	_ = ctx

	commandLine := strings.TrimSpace(event.Text)
	fields := strings.Fields(commandLine)
	command := ""
	if len(fields) > 0 {
		command = fields[0]
	}

	reply := channel.OutgoingMessage{
		Conversation: event.Conversation,
	}

	switch command {
	case "/help":
		reply.Text = "/help - show supported commands\n/status - show handler status\n/reset - clear current conversation context"
	case "/status":
		model := h.cfg.Model
		if model == "" {
			model = "not configured"
		}
		reply.Text = fmt.Sprintf(
			"platform: %s\nconversation: %s\nconversation_id: %s\nsender_id: %s\nmodel: %s\nhistory_messages: %d",
			event.Platform,
			event.Conversation.Kind,
			event.Conversation.ID,
			event.Sender.ID,
			model,
			h.sessions.Count(sessionKey(event)),
		)
	case "/reset":
		h.sessions.Reset(sessionKey(event))
		reply.Text = "Conversation context cleared."
	default:
		reply.Text = fmt.Sprintf("Unknown command: %s. Use /help.", command)
	}

	return reply, nil
}

func sessionKey(event channel.MessageEvent) string {
	return event.Platform + ":" + event.Conversation.ID
}
