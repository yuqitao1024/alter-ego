package agent

import (
	"context"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

func TestStubHandlerRepliesToSameConversation(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantText string
	}{
		{
			name:     "plain text",
			text:     "hello",
			wantText: "Alter Ego received: hello",
		},
		{
			name:     "trims surrounding whitespace",
			text:     "  hello  ",
			wantText: "Alter Ego received: hello",
		},
		{
			name:     "empty after trimming",
			text:     "   ",
			wantText: "Alter Ego received: (empty message)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewStubHandler()
			event := channel.MessageEvent{
				Text: tt.text,
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
			if reply.Text != tt.wantText {
				t.Fatalf("reply text = %q, want %q", reply.Text, tt.wantText)
			}
		})
	}
}
