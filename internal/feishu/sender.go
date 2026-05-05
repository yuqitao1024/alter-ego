package feishu

import (
	"context"
	"fmt"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im"
	larkimv1 "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type MessageCreator interface {
	CreateTextMessage(ctx context.Context, receiveIDType, receiveID, text string) error
}

type Sender struct {
	creator MessageCreator
}

func NewSender(creator MessageCreator) *Sender {
	return &Sender{creator: creator}
}

func (s *Sender) SendMessage(ctx context.Context, message channel.OutgoingMessage) error {
	if message.Conversation.ID == "" {
		return fmt.Errorf("conversation ID is required")
	}
	if message.Text == "" {
		return fmt.Errorf("message text is required")
	}
	return s.creator.CreateTextMessage(ctx, larkimv1.ReceiveIdTypeChatId, message.Conversation.ID, message.Text)
}

type SDKMessageCreator struct {
	client *larkim.Service
}

func NewSDKMessageCreator(client *larkim.Service) *SDKMessageCreator {
	return &SDKMessageCreator{client: client}
}

func (c *SDKMessageCreator) CreateTextMessage(ctx context.Context, receiveIDType, receiveID, text string) error {
	req := larkimv1.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkimv1.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType("text").
			Content(larkimv1.NewMessageTextBuilder().Text(text).Build()).
			Build()).
		Build()

	_, err := c.client.Message.Create(ctx, req)
	return err
}
