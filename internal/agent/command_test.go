package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

func TestCommandHandlerHelpListsSupportedCommands(t *testing.T) {
	handler := NewCommandHandler(Config{}, NewSessionStore(12))
	event := channel.MessageEvent{
		Text: "/help",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleCommand(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	if !strings.Contains(reply.Text, "/help") || !strings.Contains(reply.Text, "/status") || !strings.Contains(reply.Text, "/reset") {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestCommandHandlerStatusReportsModelAndHistoryCount(t *testing.T) {
	store := NewSessionStore(12)
	store.AppendTurn("lark:oc_1", "hello", "world")
	handler := NewCommandHandler(Config{Provider: "dashscope", Model: "glm-5.1"}, store)
	event := channel.MessageEvent{
		Text:     "/status",
		Platform: "lark",
		Sender:   channel.Sender{ID: "ou_1"},
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleCommand(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	for _, part := range []string{"platform: lark", "conversation: direct", "conversation_id: oc_1", "sender_id: ou_1", "provider: dashscope", "model: glm-5.1", "history_messages: 2"} {
		if !strings.Contains(reply.Text, part) {
			t.Fatalf("reply.Text = %q, missing %q", reply.Text, part)
		}
	}
}

func TestCommandHandlerResetClearsCurrentSession(t *testing.T) {
	store := NewSessionStore(12)
	store.AppendTurn("lark:oc_1", "hello", "world")
	handler := NewCommandHandler(Config{}, store)
	event := channel.MessageEvent{
		Text:     "/reset",
		Platform: "lark",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleCommand(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	if reply.Text != "Conversation context cleared." {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
	if store.Count("lark:oc_1") != 0 {
		t.Fatalf("Count = %d, want 0", store.Count("lark:oc_1"))
	}
}

func TestCommandHandlerUnknownCommandPointsToHelp(t *testing.T) {
	handler := NewCommandHandler(Config{}, NewSessionStore(12))
	event := channel.MessageEvent{
		Text: "/unknown",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleCommand(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	if !strings.Contains(reply.Text, "Unknown command") || !strings.Contains(reply.Text, "/help") {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}
