package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/ability"
	"github.com/yuqitao1024/alter-ego/internal/channel"
)

const defaultSystemPrompt = `你是 Alter Ego，是用户的高能力分身，不是旁观式助手。
你的目标是像用户本人一样处理工作和思考问题，但能力更强：判断更清晰，表达更自然，推进更有章法。
默认使用简体中文，OKR、PR、RAG、skill、workflow 这类关键名词可以保留英文。
回答要短、直接、像真人。优先给结论和下一步，不写大段空泛解释，不堆格式，不说套话。
不要用“当然”“总结一下”“希望有帮助”“我们来深入探讨”这类模板化表达。
少用机械小标题、粗体标签和三段式排比；除非用户要求详细方案，否则每段尽量短。
不要提到内部 skill、RAG、检索、资料标题、文件名、路径或引用来源。`

type ChatMessage struct {
	Role    string
	Content string
}

type ChatRequest struct {
	Model    string
	Messages []ChatMessage
}

type abilityContextBuilder interface {
	BuildContext(ctx context.Context, userText string) (string, error)
}

type ChatHandler struct {
	cfg            Config
	sessions       *SessionStore
	provider       Provider
	systemText     string
	abilityContext abilityContextBuilder
}

func NewChatHandler(cfg Config, sessions *SessionStore, provider Provider) *ChatHandler {
	if provider == nil && cfg.APIKey != "" && cfg.Model != "" {
		provider = NewProvider(cfg, nil)
	}
	return &ChatHandler{
		cfg:            cfg,
		sessions:       sessions,
		provider:       provider,
		systemText:     defaultSystemPrompt,
		abilityContext: ability.NewBuilder(ability.Options{}),
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

	userText := strings.TrimSpace(event.Text)
	systemText := h.systemText
	if h.abilityContext != nil {
		if contextText, err := h.abilityContext.BuildContext(ctx, userText); err == nil && strings.TrimSpace(contextText) != "" {
			systemText = strings.TrimSpace(systemText) + "\n\n" + strings.TrimSpace(contextText)
		}
	}

	messages := []ChatMessage{{Role: h.provider.SystemRole(), Content: systemText}}
	for _, message := range h.sessions.Snapshot(sessionKey(event)) {
		messages = append(messages, ChatMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}
	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: userText,
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

	h.sessions.AppendTurn(sessionKey(event), userText, text)
	reply.Text = text
	return reply, nil
}
