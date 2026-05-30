package agent

import (
	"context"
	"fmt"
	"strings"

	larkcard "github.com/larksuite/oapi-sdk-go/v3/card"

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
		reply.Card = &channel.CardMessage{Payload: buildCommandCard(
			"Command Help",
			larkcard.TemplateBlue,
			"/help - show supported commands\n/status - show handler status\n/reset - clear current conversation context\n/machine init <machine-id> - install and enable Codex App Server on a machine\n\nTask commands:\n/task start <template> <requirement> - create a new task\n/task list [-a] - list active tasks or all tasks\n/task status <task-id> - show task detail\n/task reply <task-id> <text> - reply to a waiting task\n/task reopen <task-id> <extra requirement> - continue a completed or stopped task\n/task stop <task-id> - stop a running task\n/task delete <task-id|-a> - delete terminal task(s)",
		)}
	case "/machine":
		if len(fields) != 3 || fields[1] != "init" {
			reply.Card = &channel.CardMessage{Payload: buildCommandCard("Machine Command Usage", larkcard.TemplateGrey, "Usage: /machine init <machine-id>")}
			return reply, nil
		}
		if h.machineInit == nil {
			reply.Card = &channel.CardMessage{Payload: buildCommandCard("Machine Init", larkcard.TemplateRed, "Machine init service is not configured.")}
			return reply, nil
		}
		machineID := fields[2]
		if err := h.machineInit.InitMachine(ctx, machineID); err != nil {
			return reply, err
		}
		reply.Card = &channel.CardMessage{Payload: buildCommandCard("Machine Initialized", larkcard.TemplateGreen, fmt.Sprintf("Machine %s initialized for Codex App Server.", machineID))}
	case "/status":
		provider := h.cfg.Provider
		if provider == "" {
			provider = "openai"
		}
		model := h.cfg.Model
		if model == "" {
			model = "not configured"
		}
		reply.Card = &channel.CardMessage{Payload: buildCommandCard("Handler Status", larkcard.TemplateBlue, fmt.Sprintf(
			"platform: %s\nconversation: %s\nconversation_id: %s\nsender_id: %s\nprovider: %s\nmodel: %s\nhistory_messages: %d",
			event.Platform,
			event.Conversation.Kind,
			event.Conversation.ID,
			event.Sender.ID,
			provider,
			model,
			h.sessions.Count(sessionKey(event)),
		))}
	case "/reset":
		h.sessions.Reset(sessionKey(event))
		reply.Card = &channel.CardMessage{Payload: buildCommandCard("Conversation Reset", larkcard.TemplateGreen, "Conversation context cleared.")}
	default:
		reply.Card = &channel.CardMessage{Payload: buildCommandCard("Unknown Command", larkcard.TemplateGrey, fmt.Sprintf("Unknown command: %s. Use /help.", command))}
	}

	return reply, nil
}

func buildCommandCard(title string, template string, message string) interface{} {
	return larkcard.NewMessageCard().
		Config(larkcard.NewMessageCardConfig().WideScreenMode(true).Build()).
		Header(larkcard.NewMessageCardHeader().
			Template(template).
			Title(larkcard.NewMessageCardPlainText().Content(title).Build()).
			Build()).
		Elements([]larkcard.MessageCardElement{
			larkcard.NewMessageCardMarkdown().
				Content(strings.TrimSpace(message)).
				Build(),
		}).
		Build()
}

func sessionKey(event channel.MessageEvent) string {
	return event.Platform + ":" + event.Conversation.ID
}
