package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type MachineInitService interface {
	InitMachine(ctx context.Context, machineID string) error
}

type CommandHandler struct {
	cfg         Config
	sessions    *SessionStore
	machineInit MachineInitService
}

func NewCommandHandler(cfg Config, sessions *SessionStore, machineInit ...MachineInitService) *CommandHandler {
	var svc MachineInitService
	if len(machineInit) > 0 {
		svc = machineInit[0]
	}
	return &CommandHandler{
		cfg:         cfg,
		sessions:    sessions,
		machineInit: svc,
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
		reply.Text = "/help - show supported commands\n/status - show handler status\n/reset - clear current conversation context\n/machine init <machine-id> - install and enable Codex App Server on a machine"
	case "/machine":
		if len(fields) != 3 || fields[1] != "init" {
			reply.Text = "Usage: /machine init <machine-id>"
			return reply, nil
		}
		if h.machineInit == nil {
			reply.Text = "Machine init service is not configured."
			return reply, nil
		}
		machineID := fields[2]
		if err := h.machineInit.InitMachine(ctx, machineID); err != nil {
			return reply, err
		}
		reply.Text = fmt.Sprintf("Machine %s initialized for Codex App Server.", machineID)
	case "/status":
		provider := h.cfg.Provider
		if provider == "" {
			provider = "openai"
		}
		model := h.cfg.Model
		if model == "" {
			model = "not configured"
		}
		reply.Text = fmt.Sprintf(
			"platform: %s\nconversation: %s\nconversation_id: %s\nsender_id: %s\nprovider: %s\nmodel: %s\nhistory_messages: %d",
			event.Platform,
			event.Conversation.Kind,
			event.Conversation.ID,
			event.Sender.ID,
			provider,
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
