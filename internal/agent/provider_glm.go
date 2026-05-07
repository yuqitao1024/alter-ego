package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type GLMProvider struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewGLMProvider(cfg Config, httpClient *http.Client) *GLMProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &GLMProvider{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		httpClient: httpClient,
	}
}

func (p *GLMProvider) CreateResponse(ctx context.Context, req ChatRequest) (string, error) {
	body := glmChatCompletionsRequest{
		Model:    req.Model,
		Messages: make([]glmChatMessage, 0, len(req.Messages)),
	}
	for _, message := range req.Messages {
		body.Messages = append(body.Messages, glmChatMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var decoded glmChatCompletionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		if decoded.Error != nil && decoded.Error.Message != "" {
			return "", fmt.Errorf(decoded.Error.Message)
		}
		return "", fmt.Errorf("glm request failed with status %d", resp.StatusCode)
	}
	if len(decoded.Choices) == 0 {
		return "", nil
	}
	return decoded.Choices[0].Message.Content, nil
}

type glmChatCompletionsRequest struct {
	Model    string           `json:"model"`
	Messages []glmChatMessage `json:"messages"`
}

type glmChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type glmChatCompletionsResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
