package feishu

import (
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

const platformName = "feishu"

type IncomingMessage struct {
	MessageID        string
	ChatID           string
	ChatType         string
	SenderOpenID     string
	Text             string
	TextWithoutAtBot string
	IsMention        bool
}

func NormalizeIncoming(input IncomingMessage) channel.MessageEvent {
	kind := channel.ConversationDirect
	if strings.EqualFold(input.ChatType, "group") {
		kind = channel.ConversationGroup
	}

	text := strings.TrimSpace(input.TextWithoutAtBot)
	if text == "" {
		text = strings.TrimSpace(input.Text)
	}

	return channel.MessageEvent{
		ID:      input.MessageID,
		Text:    text,
		RawText: strings.TrimSpace(input.Text),
		Conversation: channel.Conversation{
			ID:   input.ChatID,
			Kind: kind,
		},
		Sender: channel.Sender{
			ID: input.SenderOpenID,
		},
		MentionedBot: input.IsMention,
		Platform:     platformName,
	}
}
