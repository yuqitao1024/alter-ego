package lark

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type fakeMessageCreator struct {
	receiveIDType string
	receiveID     string
	text          string
	err           error
}

func (f *fakeMessageCreator) CreateTextMessage(ctx context.Context, receiveIDType, receiveID, text string) error {
	f.receiveIDType = receiveIDType
	f.receiveID = receiveID
	f.text = text
	return f.err
}

func TestSenderUsesChatIDForConversations(t *testing.T) {
	testCases := []struct {
		name string
		kind channel.ConversationKind
	}{
		{name: "direct", kind: channel.ConversationDirect},
		{name: "group", kind: channel.ConversationGroup},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeMessageCreator{}
			sender := NewSender(fake)

			err := sender.SendMessage(context.Background(), channel.OutgoingMessage{
				Text: "hello",
				Conversation: channel.Conversation{
					ID:   "oc_chat",
					Kind: tc.kind,
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
		})
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

func TestSenderUsesOpenIDForDirectMessages(t *testing.T) {
	fake := &fakeMessageCreator{}
	sender := NewSender(fake)

	err := sender.SendDirectMessage(context.Background(), "ou_user", "hello")
	if err != nil {
		t.Fatalf("SendDirectMessage returned error: %v", err)
	}
	if fake.receiveIDType != "open_id" {
		t.Fatalf("receiveIDType = %q, want open_id", fake.receiveIDType)
	}
	if fake.receiveID != "ou_user" {
		t.Fatalf("receiveID = %q, want ou_user", fake.receiveID)
	}
}

func TestSenderRejectsEmptyMessageText(t *testing.T) {
	sender := NewSender(&fakeMessageCreator{})

	err := sender.SendMessage(context.Background(), channel.OutgoingMessage{
		Conversation: channel.Conversation{
			ID:   "oc_chat",
			Kind: channel.ConversationDirect,
		},
	})
	if err == nil {
		t.Fatal("SendMessage returned nil error for empty message text")
	}
}

func TestSenderPropagatesCreatorError(t *testing.T) {
	wantErr := errors.New("send failed")
	sender := NewSender(&fakeMessageCreator{err: wantErr})

	err := sender.SendMessage(context.Background(), channel.OutgoingMessage{
		Text: "hello",
		Conversation: channel.Conversation{
			ID:   "oc_chat",
			Kind: channel.ConversationDirect,
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestTextMessageContentEscapesJSON(t *testing.T) {
	content, err := textMessageContent("line 1\n\"quoted\" \\ slash")
	if err != nil {
		t.Fatalf("textMessageContent returned error: %v", err)
	}

	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		t.Fatalf("content is not valid JSON: %v", err)
	}
	if payload.Text != "line 1\n\"quoted\" \\ slash" {
		t.Fatalf("payload text = %q", payload.Text)
	}
}
