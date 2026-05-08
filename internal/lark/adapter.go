package lark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	larkapi "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

type Adapter struct {
	cfg      Config
	handler  channel.Handler
	sender   channel.MessageSender
	wsClient wsStarter
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
	}

	eventHandler := dispatcher.NewEventDispatcher("", "")
	eventHandler.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		return adapter.handleP2Message(ctx, event)
	})

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

	reply, err := a.handler.HandleMessage(ctx, normalized)
	if err != nil {
		return err
	}
	if reply.Text == "" {
		return nil
	}
	return a.sender.SendMessage(ctx, reply)
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
