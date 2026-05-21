package lark

import (
	"context"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type fakeCardActionHandler struct {
	event channel.CardActionEvent
	reply channel.CardActionResponse
}

func (f *fakeCardActionHandler) HandleMessage(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	return channel.OutgoingMessage{}, nil
}

func (f *fakeCardActionHandler) HandleCardAction(ctx context.Context, event channel.CardActionEvent) (channel.CardActionResponse, error) {
	f.event = event
	return f.reply, nil
}

type fakeAdapterSender struct {
	message channel.OutgoingMessage
	called  bool
}

func (f *fakeAdapterSender) SendMessage(ctx context.Context, message channel.OutgoingMessage) error {
	f.called = true
	f.message = message
	return nil
}

func TestAdapterHandlesCardActionTrigger(t *testing.T) {
	t.Parallel()

	handler := &fakeCardActionHandler{
		reply: channel.CardActionResponse{ToastText: "Task task-1 stopped."},
	}
	adapter := &Adapter{
		cfg: Config{
			AllowUsers:  map[string]bool{"ou_user": true},
			AllowGroups: map[string]bool{"oc_chat": true},
		},
		handler: handler,
		sender:  &fakeAdapterSender{},
	}

	resp, err := adapter.handleCardAction(context.Background(), cardActionInput{
		ID:           "evt_1",
		OpenID:       "ou_user",
		OpenChatID:   "oc_chat",
		ActionName:   "stop",
		ActionValue:  map[string]interface{}{"action": "stop", "task_id": "task-1"},
		Conversation: channel.Conversation{ID: "oc_chat", Kind: channel.ConversationGroup},
	})
	if err != nil {
		t.Fatalf("handleCardAction returned error: %v", err)
	}

	if handler.event.Action != "stop" {
		t.Fatalf("handler.event.Action = %q", handler.event.Action)
	}
	if handler.event.Sender.ID != "ou_user" {
		t.Fatalf("handler.event.Sender.ID = %q", handler.event.Sender.ID)
	}
	if handler.event.Conversation.ID != "oc_chat" {
		t.Fatalf("handler.event.Conversation = %#v", handler.event.Conversation)
	}
	if resp.ToastText != "Task task-1 stopped." {
		t.Fatalf("resp.ToastText = %q", resp.ToastText)
	}
}

func TestAdapterSendsCardActionFollowUpMessage(t *testing.T) {
	t.Parallel()

	sender := &fakeAdapterSender{}
	handler := &fakeCardActionHandler{
		reply: channel.CardActionResponse{
			ToastText: "Task status sent.",
			Message: &channel.OutgoingMessage{
				Conversation: channel.Conversation{ID: "oc_chat", Kind: channel.ConversationGroup},
				Text:         "task: task-1",
			},
		},
	}
	adapter := &Adapter{
		cfg: Config{
			AllowUsers:  map[string]bool{"ou_user": true},
			AllowGroups: map[string]bool{"oc_chat": true},
		},
		handler: handler,
		sender:  sender,
	}

	_, err := adapter.handleCardAction(context.Background(), cardActionInput{
		ID:           "evt_1",
		OpenID:       "ou_user",
		OpenChatID:   "oc_chat",
		ActionName:   "status",
		ActionValue:  map[string]interface{}{"action": "status", "task_id": "task-1"},
		Conversation: channel.Conversation{ID: "oc_chat", Kind: channel.ConversationGroup},
	})
	if err != nil {
		t.Fatalf("handleCardAction returned error: %v", err)
	}

	if !sender.called {
		t.Fatal("SendMessage was not called")
	}
	if sender.message.Text != "task: task-1" {
		t.Fatalf("sent message text = %q", sender.message.Text)
	}
}

func TestAdapterRejectsUnauthorizedCardAction(t *testing.T) {
	t.Parallel()

	handler := &fakeCardActionHandler{}
	adapter := &Adapter{
		cfg: Config{
			AllowUsers:  map[string]bool{"ou_allowed": true},
			AllowGroups: map[string]bool{"oc_chat": true},
		},
		handler: handler,
		sender:  &fakeAdapterSender{},
	}

	resp, err := adapter.handleCardAction(context.Background(), cardActionInput{
		ID:           "evt_1",
		OpenID:       "ou_denied",
		OpenChatID:   "oc_chat",
		ActionName:   "stop",
		ActionValue:  map[string]interface{}{"action": "stop", "task_id": "task-1"},
		Conversation: channel.Conversation{ID: "oc_chat", Kind: channel.ConversationGroup},
	})
	if err != nil {
		t.Fatalf("handleCardAction returned error: %v", err)
	}

	if handler.event.Sender.ID != "" {
		t.Fatalf("handler should not be called, event = %#v", handler.event)
	}
	if resp.ToastText == "" {
		t.Fatal("expected rejection toast")
	}
}
