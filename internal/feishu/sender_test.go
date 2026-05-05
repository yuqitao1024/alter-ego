package feishu

import (
	"context"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type fakeMessageCreator struct {
	receiveIDType string
	receiveID     string
	text          string
}

func (f *fakeMessageCreator) CreateTextMessage(ctx context.Context, receiveIDType, receiveID, text string) error {
	f.receiveIDType = receiveIDType
	f.receiveID = receiveID
	f.text = text
	return nil
}

func TestSenderUsesChatIDForDirectAndGroupConversations(t *testing.T) {
	fake := &fakeMessageCreator{}
	sender := NewSender(fake)

	err := sender.SendMessage(context.Background(), channel.OutgoingMessage{
		Text: "hello",
		Conversation: channel.Conversation{
			ID:   "oc_chat",
			Kind: channel.ConversationDirect,
		},
	})
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}

	if fake.receiveIDType != "chat_id" {
		t.Fatalf("receiveIDType = %q, want chat_id", fake.receiveIDType)
	}
	if fake.receiveID != "oc_chat" {
		t.Fatalf("receiveID = %q", fake.receiveID)
	}
	if fake.text != "hello" {
		t.Fatalf("text = %q", fake.text)
	}
}

func TestSenderRejectsEmptyConversationID(t *testing.T) {
	sender := NewSender(&fakeMessageCreator{})

	err := sender.SendMessage(context.Background(), channel.OutgoingMessage{
		Text: "hello",
		Conversation: channel.Conversation{
			Kind: channel.ConversationGroup,
		},
	})
	if err == nil {
		t.Fatal("SendMessage returned nil error for empty conversation ID")
	}
}
