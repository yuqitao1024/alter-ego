package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/channel"
)

const defaultSystemPrompt = "You are Alter Ego, a concise and pragmatic AI assistant that helps the user with work and exploration."

type ChatMessage struct {
	Role    string
	Content string
}

type ChatRequest struct {
	Model    string
	Messages []ChatMessage
}

type ChatHandler struct {
	cfg        Config
	sessions   *SessionStore
	provider   Provider
	systemText string
}

func NewChatHandler(cfg Config, sessions *SessionStore, provider Provider) *ChatHandler {
	if provider == nil && cfg.APIKey != "" && cfg.Model != "" {
		provider = NewProvider(cfg, nil)
	}
	return &ChatHandler{
		cfg:        cfg,
		sessions:   sessions,
		provider:   provider,
		systemText: defaultSystemPrompt,
	}
}

func (h *ChatHandler) HandleMessage(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	reply := channel.OutgoingMessage{
		Conversation: event.Conversation,
	}

	if h.cfg.APIKey == "" || h.cfg.Model == "" || h.provider == nil {
		reply.Text = "LLM is not configured."
		return reply, nil
	}

	messages := []ChatMessage{{Role: h.provider.SystemRole(), Content: h.systemText}}
	for _, message := range h.sessions.Snapshot(sessionKey(event)) {
		messages = append(messages, ChatMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}
	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: strings.TrimSpace(event.Text),
	})

	text, err := h.provider.CreateResponse(ctx, ChatRequest{
		Model:    h.cfg.Model,
		Messages: messages,
	})
	if err != nil {
		reply.Text = fmt.Sprintf("LLM request failed: %v", err)
		return reply, nil
	}

	text = strings.TrimSpace(text)
	if text == "" {
		reply.Text = "The model returned an empty response."
		return reply, nil
	}

	h.sessions.AppendTurn(sessionKey(event), strings.TrimSpace(event.Text), text)
	reply.Text = text
	return reply, nil
}
