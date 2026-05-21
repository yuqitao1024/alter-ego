package lark

import (
	"context"
	"encoding/json"
	"fmt"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im"
	larkimv1 "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type MessageCreator interface {
	CreateTextMessage(ctx context.Context, receiveIDType, receiveID, text string) error
	CreateMessage(ctx context.Context, receiveIDType, receiveID, msgType, content string) error
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
	if message.Card != nil {
		content, err := json.Marshal(message.Card.Payload)
		if err != nil {
			return err
		}
		return s.creator.CreateMessage(ctx, larkimv1.ReceiveIdTypeChatId, message.Conversation.ID, "interactive", string(content))
	}
	if message.Text == "" {
		return fmt.Errorf("message text is required")
	}
	return s.creator.CreateTextMessage(ctx, larkimv1.ReceiveIdTypeChatId, message.Conversation.ID, message.Text)
}

func (s *Sender) SendDirectMessage(ctx context.Context, openID, text string) error {
	if openID == "" {
		return fmt.Errorf("open ID is required")
	}
	if text == "" {
		return fmt.Errorf("message text is required")
	}
	return s.creator.CreateTextMessage(ctx, larkimv1.ReceiveIdTypeOpenId, openID, text)
}

type SDKMessageCreator struct {
	client *larkim.Service
}

func NewSDKMessageCreator(client *larkim.Service) *SDKMessageCreator {
	return &SDKMessageCreator{client: client}
}

func (c *SDKMessageCreator) CreateTextMessage(ctx context.Context, receiveIDType, receiveID, text string) error {
	content, err := textMessageContent(text)
	if err != nil {
		return err
	}
	return c.CreateMessage(ctx, receiveIDType, receiveID, "text", content)
}

func (c *SDKMessageCreator) CreateMessage(ctx context.Context, receiveIDType, receiveID, msgType, content string) error {
	req := larkimv1.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType).
		Body(larkimv1.NewCreateMessageReqBodyBuilder().
			ReceiveId(receiveID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build()

	resp, err := c.client.Message.Create(ctx, req)
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("lark create message returned nil response")
	}
	if !resp.Success() {
		return resp.CodeError
	}
	return nil
}

func textMessageContent(text string) (string, error) {
	content, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: text})
	if err != nil {
		return "", err
	}
	return string(content), nil
}
