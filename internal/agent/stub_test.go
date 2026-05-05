package agent

import (
	"context"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

func TestStubHandlerRepliesToSameConversation(t *testing.T) {
	handler := NewStubHandler()
	event := channel.MessageEvent{
		Text: "hello",
		Conversation: channel.Conversation{
			ID:   "oc_group",
			Kind: channel.ConversationGroup,
		},
	}

	reply, err := handler.HandleMessage(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}

	if reply.Conversation != event.Conversation {
		t.Fatalf("reply conversation = %#v, want %#v", reply.Conversation, event.Conversation)
	}
	if reply.Text != "Alter Ego received: hello" {
		t.Fatalf("reply text = %q", reply.Text)
	}
}
