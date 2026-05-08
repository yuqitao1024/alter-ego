package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type DashScopeProvider struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewDashScopeProvider(cfg Config, httpClient *http.Client) *DashScopeProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &DashScopeProvider{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		httpClient: httpClient,
	}
}

func (p *DashScopeProvider) CreateResponse(ctx context.Context, req ChatRequest) (string, error) {
	body := dashScopeChatCompletionsRequest{
		Model:    req.Model,
		Messages: make([]dashScopeChatMessage, 0, len(req.Messages)),
	}
	for _, message := range req.Messages {
		body.Messages = append(body.Messages, dashScopeChatMessage{
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

	var decoded dashScopeChatCompletionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		if decoded.Error != nil && decoded.Error.Message != "" {
			return "", fmt.Errorf(decoded.Error.Message)
		}
		return "", fmt.Errorf("dashscope request failed with status %d", resp.StatusCode)
	}
	if len(decoded.Choices) == 0 {
		return "", nil
	}
	return decoded.Choices[0].Message.Content, nil
}

func (p *DashScopeProvider) SystemRole() string {
	return "system"
}

type dashScopeChatCompletionsRequest struct {
	Model    string                 `json:"model"`
	Messages []dashScopeChatMessage `json:"messages"`
}

type dashScopeChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type dashScopeChatCompletionsResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
