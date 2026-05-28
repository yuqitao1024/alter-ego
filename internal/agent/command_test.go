package agent

import (
	"context"
	"encoding/json"
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
	if reply.Card == nil {
		t.Fatal("reply.Card is nil")
	}
	for _, part := range []string{"/help", "/status", "/reset"} {
		if !cardPayloadContains(reply.Card.Payload, part) {
			t.Fatalf("reply.Card.Payload missing %q: %#v", part, reply.Card.Payload)
		}
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
		if !cardPayloadContains(reply.Card.Payload, part) {
			t.Fatalf("reply.Card.Payload missing %q: %#v", part, reply.Card.Payload)
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
	if reply.Card == nil {
		t.Fatal("reply.Card is nil")
	}
	if !cardPayloadContains(reply.Card.Payload, "Conversation context cleared.") {
		t.Fatalf("reply.Card.Payload = %#v", reply.Card.Payload)
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
	if reply.Card == nil {
		t.Fatal("reply.Card is nil")
	}
	for _, part := range []string{"Unknown command", "/help"} {
		if !cardPayloadContains(reply.Card.Payload, part) {
			t.Fatalf("reply.Card.Payload missing %q: %#v", part, reply.Card.Payload)
		}
	}
}

func TestCommandHandlerMachineInitInvokesService(t *testing.T) {
	handler := NewCommandHandler(Config{}, NewSessionStore(12), &fakeMachineInitService{})
	event := channel.MessageEvent{
		Text: "/machine init machine_a",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleCommand(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	if reply.Card == nil {
		t.Fatal("reply.Card is nil")
	}
	if !cardPayloadContains(reply.Card.Payload, "Machine machine_a initialized for Codex App Server.") {
		t.Fatalf("reply.Card.Payload = %#v", reply.Card.Payload)
	}
}

func cardPayloadContains(payload interface{}, needle string) bool {
	b, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), needle)
}

type fakeMachineInitService struct {
	machineID string
}

func (f *fakeMachineInitService) InitMachine(_ context.Context, machineID string) error {
	f.machineID = machineID
	return nil
}
