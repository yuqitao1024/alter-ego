package agent

import (
	"context"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type StubHandler struct{}

func NewStubHandler() *StubHandler {
	return &StubHandler{}
}

func (h *StubHandler) HandleMessage(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	text := strings.TrimSpace(event.Text)
	if text == "" {
		text = "(empty message)"
	}

	return channel.OutgoingMessage{
		Text:         "Alter Ego received: " + text,
		Conversation: event.Conversation,
	}, nil
}
