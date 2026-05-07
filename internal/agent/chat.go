package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

type ChatClient interface {
	CreateResponse(ctx context.Context, req ChatRequest) (string, error)
}

type ChatHandler struct {
	cfg        Config
	sessions   *SessionStore
	client     ChatClient
	systemText string
}

func NewChatHandler(cfg Config, sessions *SessionStore, client ChatClient) *ChatHandler {
	if client == nil && cfg.APIKey != "" && cfg.Model != "" {
		client = NewOpenAIClient(cfg, nil)
	}
	return &ChatHandler{
		cfg:        cfg,
		sessions:   sessions,
		client:     client,
		systemText: defaultSystemPrompt,
	}
}

func (h *ChatHandler) HandleMessage(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	reply := channel.OutgoingMessage{
		Conversation: event.Conversation,
	}

	if h.cfg.APIKey == "" || h.cfg.Model == "" || h.client == nil {
		reply.Text = "LLM is not configured."
		return reply, nil
	}

	messages := []ChatMessage{{Role: "developer", Content: h.systemText}}
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

	text, err := h.client.CreateResponse(ctx, ChatRequest{
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

type OpenAIClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewOpenAIClient(cfg Config, httpClient *http.Client) *OpenAIClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIClient{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		httpClient: httpClient,
	}
}

func (c *OpenAIClient) CreateResponse(ctx context.Context, req ChatRequest) (string, error) {
	body := openAIResponseRequest{
		Model: req.Model,
		Input: make([]openAIInputMessage, 0, len(req.Messages)),
	}
	for _, message := range req.Messages {
		body.Input = append(body.Input, openAIInputMessage{
			Type:    "message",
			Role:    message.Role,
			Content: message.Content,
		})
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var decoded openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		if decoded.Error != nil && decoded.Error.Message != "" {
			return "", fmt.Errorf(decoded.Error.Message)
		}
		return "", fmt.Errorf("openai request failed with status %d", resp.StatusCode)
	}
	return decoded.OutputText, nil
}

type openAIResponseRequest struct {
	Model string               `json:"model"`
	Input []openAIInputMessage `json:"input"`
}

type openAIInputMessage struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	OutputText string `json:"output_text"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
