package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	bodyPreview := diagnosticSnippet(string(rawBody), 400)

	var decoded dashScopeChatCompletionsResponse
	if err := json.Unmarshal(rawBody, &decoded); err != nil {
		return "", fmt.Errorf("decode dashscope response status=%d body=%q: %w", resp.StatusCode, bodyPreview, err)
	}
	if resp.StatusCode >= 400 {
		if decoded.Error != nil && decoded.Error.Message != "" {
			return "", fmt.Errorf("dashscope request failed status=%d message=%q body=%q", resp.StatusCode, decoded.Error.Message, bodyPreview)
		}
		return "", fmt.Errorf("dashscope request failed status=%d body=%q", resp.StatusCode, bodyPreview)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("dashscope response contained no choices status=%d body=%q", resp.StatusCode, bodyPreview)
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		firstMessageRaw := ""
		var envelope struct {
			Choices []struct {
				Message json.RawMessage `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(rawBody, &envelope); err == nil && len(envelope.Choices) > 0 {
			firstMessageRaw = diagnosticSnippet(string(envelope.Choices[0].Message), 1200)
		}
		return "", fmt.Errorf(
			"dashscope response contained empty choice content status=%d choices=%d finish_reason=%q first_message=%q body=%q",
			resp.StatusCode,
			len(decoded.Choices),
			decoded.Choices[0].FinishReason,
			firstMessageRaw,
			bodyPreview,
		)
	}
	return content, nil
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
		FinishReason string `json:"finish_reason"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
