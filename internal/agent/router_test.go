package agent

import (
	"context"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type fakeCommandExecutor struct {
	called bool
	reply  channel.OutgoingMessage
}

func (f *fakeCommandExecutor) HandleCommand(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	f.called = true
	return f.reply, nil
}

type fakeTaskExecutor struct {
	called bool
	reply  channel.OutgoingMessage
}

func (f *fakeTaskExecutor) HandleCommand(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	f.called = true
	return f.reply, nil
}

type fakeTaskCardExecutor struct {
	fakeTaskExecutor
	cardCalled bool
	cardEvent  channel.CardActionEvent
	cardReply  channel.CardActionResponse
}

func (f *fakeTaskCardExecutor) HandleCardAction(ctx context.Context, event channel.CardActionEvent) (channel.CardActionResponse, error) {
	f.cardCalled = true
	f.cardEvent = event
	return f.cardReply, nil
}

type fakeMessageHandler struct {
	called bool
	reply  channel.OutgoingMessage
}

func (f *fakeMessageHandler) HandleMessage(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	f.called = true
	return f.reply, nil
}

func TestRouterRoutesSlashCommandToCommandHandler(t *testing.T) {
	command := &fakeCommandExecutor{
		reply: channel.OutgoingMessage{Text: "command"},
	}
	chat := &fakeMessageHandler{
		reply: channel.OutgoingMessage{Text: "chat"},
	}
	router := NewRouter(command, nil, chat)

	reply, err := router.HandleMessage(context.Background(), channel.MessageEvent{
		Text: "/help",
	})
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if !command.called {
		t.Fatal("command handler was not called")
	}
	if chat.called {
		t.Fatal("chat handler should not have been called")
	}
	if reply.Text != "command" {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestRouterRoutesNormalTextToChatHandler(t *testing.T) {
	command := &fakeCommandExecutor{
		reply: channel.OutgoingMessage{Text: "command"},
	}
	chat := &fakeMessageHandler{
		reply: channel.OutgoingMessage{Text: "chat"},
	}
	router := NewRouter(command, nil, chat)

	reply, err := router.HandleMessage(context.Background(), channel.MessageEvent{
		Text: "hello",
	})
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if command.called {
		t.Fatal("command handler should not have been called")
	}
	if !chat.called {
		t.Fatal("chat handler was not called")
	}
	if reply.Text != "chat" {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestRouterPreservesConversationTarget(t *testing.T) {
	conversation := channel.Conversation{ID: "oc_1", Kind: channel.ConversationDirect}
	chat := &fakeMessageHandler{
		reply: channel.OutgoingMessage{
			Text:         "chat",
			Conversation: conversation,
		},
	}
	router := NewRouter(&fakeCommandExecutor{}, nil, chat)

	reply, err := router.HandleMessage(context.Background(), channel.MessageEvent{
		Text:         "hello",
		Conversation: conversation,
	})
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if reply.Conversation != conversation {
		t.Fatalf("reply.Conversation = %#v", reply.Conversation)
	}
}

func TestRouterRoutesTaskCommandToTaskHandler(t *testing.T) {
	command := &fakeCommandExecutor{
		reply: channel.OutgoingMessage{Text: "command"},
	}
	task := &fakeTaskExecutor{
		reply: channel.OutgoingMessage{Text: "task"},
	}
	chat := &fakeMessageHandler{
		reply: channel.OutgoingMessage{Text: "chat"},
	}
	router := NewRouter(command, task, chat)

	reply, err := router.HandleMessage(context.Background(), channel.MessageEvent{
		Text: "/task list",
	})
	if err != nil {
		t.Fatalf("HandleMessage returned error: %v", err)
	}
	if !task.called {
		t.Fatal("task handler was not called")
	}
	if command.called {
		t.Fatal("command handler should not have been called")
	}
	if chat.called {
		t.Fatal("chat handler should not have been called")
	}
	if reply.Text != "task" {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}

func TestRouterRoutesCardActionToTaskHandler(t *testing.T) {
	task := &fakeTaskCardExecutor{
		cardReply: channel.CardActionResponse{ToastText: "Task task-1 stopped."},
	}
	router := NewRouter(&fakeCommandExecutor{}, task, &fakeMessageHandler{})

	reply, err := router.HandleCardAction(context.Background(), channel.CardActionEvent{
		Action: "stop",
		Value: map[string]interface{}{
			"task_id": "task-1",
		},
	})
	if err != nil {
		t.Fatalf("HandleCardAction returned error: %v", err)
	}
	if !task.cardCalled {
		t.Fatal("task card action handler was not called")
	}
	if task.cardEvent.Action != "stop" {
		t.Fatalf("cardEvent.Action = %q, want stop", task.cardEvent.Action)
	}
	if reply.ToastText != "Task task-1 stopped." {
		t.Fatalf("reply.ToastText = %q", reply.ToastText)
	}
}
