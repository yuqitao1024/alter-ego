package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	larkapi "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type Adapter struct {
	cfg      Config
	handler  channel.Handler
	sender   channel.MessageSender
	wsClient wsStarter
	events   *dispatcher.EventDispatcher
	deduper  *messageDeduper
}

type cardActionHandler interface {
	HandleCardAction(ctx context.Context, event channel.CardActionEvent) (channel.CardActionResponse, error)
}

type cardActionInput struct {
	ID           string
	OpenID       string
	OpenChatID   string
	ActionName   string
	ActionValue  map[string]interface{}
	Conversation channel.Conversation
}

type wsStarter interface {
	Start(ctx context.Context) error
}

func NewAdapter(cfg Config, handler channel.Handler) *Adapter {
	apiClient := larkapi.NewClient(cfg.AppID, cfg.AppSecret, larkapi.WithOpenBaseUrl(baseURL(cfg.Domain)))
	sender := NewSender(NewSDKMessageCreator(apiClient.Im))

	adapter := &Adapter{
		cfg:     cfg,
		handler: handler,
		sender:  sender,
		deduper: newMessageDeduper(defaultMessageDedupeTTL, nil),
	}

	eventHandler := dispatcher.NewEventDispatcher("", "")
	eventHandler.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		return adapter.handleP2Message(ctx, event)
	})
	eventHandler.OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
		return adapter.handleP2CardActionTrigger(ctx, event)
	})
	adapter.events = eventHandler

	adapter.wsClient = ws.NewClient(
		cfg.AppID,
		cfg.AppSecret,
		ws.WithDomain(baseURL(cfg.Domain)),
		ws.WithEventHandler(eventHandler),
		ws.WithLogLevel(larkcore.LogLevelInfo),
	)

	return adapter
}

func (a *Adapter) Start(ctx context.Context) error {
	if a.wsClient == nil {
		return fmt.Errorf("websocket client is not configured")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.wsClient.Start(ctx)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Adapter) handleP2Message(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil || event.Event.Sender == nil {
		return nil
	}

	message := event.Event.Message
	sender := event.Event.Sender
	if value(message.MessageType) != "text" {
		return nil
	}

	senderOpenID := ""
	if sender.SenderId != nil {
		senderOpenID = value(sender.SenderId.OpenId)
	}

	text := textFromContent(value(message.Content))
	incoming := IncomingMessage{
		MessageID:        value(message.MessageId),
		ChatID:           value(message.ChatId),
		ChatType:         value(message.ChatType),
		SenderOpenID:     senderOpenID,
		Text:             text,
		TextWithoutAtBot: text,
		IsMention:        len(message.Mentions) > 0,
	}

	normalized := NormalizeIncoming(incoming)
	if !Allowed(a.cfg, normalized) {
		return nil
	}
	if a.deduper != nil && !a.deduper.MarkIfNew(normalized.ID) {
		return nil
	}

	reply, err := a.handler.HandleMessage(ctx, normalized)
	if err != nil {
		return err
	}
	if reply.Text == "" {
		if reply.Card == nil {
			return nil
		}
	}
	return a.sender.SendMessage(ctx, reply)
}

func (a *Adapter) Handle(ctx context.Context, req *larkevent.EventReq) *larkevent.EventResp {
	if a == nil || a.events == nil {
		return nil
	}
	return a.events.Handle(ctx, req)
}

func (a *Adapter) handleP2CardActionTrigger(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	if event == nil || event.Event == nil || event.Event.Action == nil || event.Event.Operator == nil || event.Event.Context == nil {
		return nil, nil
	}

	resp, err := a.handleCardAction(ctx, cardActionInput{
		OpenID:      event.Event.Operator.OpenID,
		OpenChatID:  event.Event.Context.OpenChatID,
		ActionName:  event.Event.Action.Name,
		ActionValue: event.Event.Action.Value,
		Conversation: channel.Conversation{
			ID:   event.Event.Context.OpenChatID,
			Kind: channel.ConversationGroup,
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.ToastText == "" {
		return nil, nil
	}
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{
			Type:    "info",
			Content: resp.ToastText,
		},
	}, nil
}

func (a *Adapter) handleCardAction(ctx context.Context, input cardActionInput) (channel.CardActionResponse, error) {
	handler, ok := a.handler.(cardActionHandler)
	if !ok {
		return channel.CardActionResponse{ToastText: "Card actions are not configured."}, nil
	}

	event := channel.CardActionEvent{
		ID:           input.ID,
		Action:       input.ActionName,
		Value:        input.ActionValue,
		Conversation: input.Conversation,
		Sender: channel.Sender{
			ID: input.OpenID,
		},
		Platform: platformName,
	}
	if event.Conversation.ID == "" {
		event.Conversation.ID = input.OpenChatID
	}
	if event.Conversation.Kind == "" {
		event.Conversation.Kind = channel.ConversationGroup
	}

	if !Allowed(a.cfg, channel.MessageEvent{
		ID:           event.ID,
		Conversation: event.Conversation,
		Sender:       event.Sender,
		MentionedBot: true,
		Platform:     platformName,
	}) {
		return channel.CardActionResponse{ToastText: "You are not allowed to operate this task."}, nil
	}

	resp, err := handler.HandleCardAction(ctx, event)
	if err != nil {
		return channel.CardActionResponse{}, err
	}
	if resp.Message != nil && a.sender != nil {
		if err := a.sender.SendMessage(ctx, *resp.Message); err != nil {
			return channel.CardActionResponse{}, err
		}
	}
	return resp, nil
}

func textFromContent(raw string) string {
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return strings.TrimSpace(raw)
	}
	return strings.TrimSpace(payload.Text)
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func baseURL(domain string) string {
	if domain == "" || strings.EqualFold(domain, "lark") {
		return larkapi.LarkBaseUrl
	}
	return domain
}
