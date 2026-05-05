package feishu

import (
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

func TestConvertDirectTextMessage(t *testing.T) {
	input := IncomingMessage{
		MessageID:        "om_1",
		ChatID:           "oc_direct",
		ChatType:         "p2p",
		SenderOpenID:     "ou_sender",
		Text:             "hello",
		TextWithoutAtBot: "hello",
		IsMention:        false,
	}

	event := NormalizeIncoming(input)

	if event.ID != "om_1" {
		t.Fatalf("ID = %q", event.ID)
	}
	if event.Text != "hello" || event.RawText != "hello" {
		t.Fatalf("text = %q raw = %q", event.Text, event.RawText)
	}
	if event.Conversation.Kind != channel.ConversationDirect {
		t.Fatalf("conversation kind = %q", event.Conversation.Kind)
	}
	if event.Conversation.ID != "oc_direct" {
		t.Fatalf("conversation ID = %q", event.Conversation.ID)
	}
	if event.Sender.ID != "ou_sender" {
		t.Fatalf("sender ID = %q", event.Sender.ID)
	}
	if event.Platform != "feishu" {
		t.Fatalf("platform = %q", event.Platform)
	}
	if event.MentionedBot {
		t.Fatal("MentionedBot = true, want false")
	}
}

func TestConvertGroupTextMessageUsesTextWithoutAtBot(t *testing.T) {
	input := IncomingMessage{
		MessageID:        "om_2",
		ChatID:           "oc_group",
		ChatType:         "group",
		SenderOpenID:     "ou_sender",
		Text:             "@bot status",
		TextWithoutAtBot: "status",
		IsMention:        true,
	}

	event := NormalizeIncoming(input)

	if event.Text != "status" {
		t.Fatalf("text = %q, want status", event.Text)
	}
	if event.RawText != "@bot status" {
		t.Fatalf("raw text = %q", event.RawText)
	}
	if event.Conversation.Kind != channel.ConversationGroup {
		t.Fatalf("conversation kind = %q", event.Conversation.Kind)
	}
	if !event.MentionedBot {
		t.Fatal("MentionedBot = false, want true")
	}
}

func TestConvertGroupChatTypeCaseInsensitive(t *testing.T) {
	input := IncomingMessage{
		ChatID:       "oc_group",
		ChatType:     "GROUP",
		SenderOpenID: "ou_sender",
		Text:         "status",
	}

	event := NormalizeIncoming(input)

	if event.Conversation.Kind != channel.ConversationGroup {
		t.Fatalf("conversation kind = %q", event.Conversation.Kind)
	}
}

func TestConvertDefaultsUnknownChatTypeToDirect(t *testing.T) {
	tests := []struct {
		name     string
		chatType string
	}{
		{name: "empty", chatType: ""},
		{name: "unknown", chatType: "topic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := IncomingMessage{
				ChatID:       "oc_direct",
				ChatType:     tt.chatType,
				SenderOpenID: "ou_sender",
				Text:         "hello",
			}

			event := NormalizeIncoming(input)

			if event.Conversation.Kind != channel.ConversationDirect {
				t.Fatalf("conversation kind = %q", event.Conversation.Kind)
			}
		})
	}
}

func TestConvertBlankTextWithoutAtBotFallsBackToTrimmedText(t *testing.T) {
	input := IncomingMessage{
		ChatID:           "oc_group",
		ChatType:         "group",
		SenderOpenID:     "ou_sender",
		Text:             "  @bot status  ",
		TextWithoutAtBot: "   ",
		IsMention:        true,
	}

	event := NormalizeIncoming(input)

	if event.Text != "@bot status" {
		t.Fatalf("text = %q, want @bot status", event.Text)
	}
	if event.RawText != "@bot status" {
		t.Fatalf("raw text = %q, want @bot status", event.RawText)
	}
}
