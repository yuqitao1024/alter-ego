package lark

import (
	"context"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type fakeMessageHandler struct {
	event channel.MessageEvent
	reply channel.OutgoingMessage
}

func (f *fakeMessageHandler) HandleMessage(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	f.event = event
	return f.reply, nil
}

func TestAdapterStripsLeadingBotMentionFromGroupCommand(t *testing.T) {
	t.Parallel()

	handler := &fakeMessageHandler{}
	adapter := &Adapter{
		cfg: Config{
			AllowUsers:  map[string]bool{"ou_user": true},
			AllowGroups: map[string]bool{"oc_group": true},
		},
		handler: handler,
		sender:  &fakeAdapterSender{},
	}

	err := adapter.handleP2Message(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringPtr("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringPtr("om_1"),
				ChatId:      stringPtr("oc_group"),
				ChatType:    stringPtr("group"),
				MessageType: stringPtr("text"),
				Content:     stringPtr(`{"text":"<at user_id=\"ou_bot\">Alter Ego</at> /status"}`),
				Mentions: []*larkim.MentionEvent{
					{
						Id:   &larkim.UserId{OpenId: stringPtr("ou_bot")},
						Name: stringPtr("Alter Ego"),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handleP2Message returned error: %v", err)
	}

	if handler.event.Text != "/status" {
		t.Fatalf("event.Text = %q, want /status", handler.event.Text)
	}
	if handler.event.RawText != `<at user_id="ou_bot">Alter Ego</at> /status` {
		t.Fatalf("event.RawText = %q", handler.event.RawText)
	}
	if !handler.event.MentionedBot {
		t.Fatal("event.MentionedBot = false, want true")
	}
}

func stringPtr(value string) *string {
	return &value
}
