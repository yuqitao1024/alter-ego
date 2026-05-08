package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type OpenAIProvider struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewOpenAIProvider(cfg Config, httpClient *http.Client) *OpenAIProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OpenAIProvider{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:     cfg.APIKey,
		httpClient: httpClient,
	}
}

func (p *OpenAIProvider) CreateResponse(ctx context.Context, req ChatRequest) (string, error) {
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(payload))
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

func (p *OpenAIProvider) SystemRole() string {
	return "developer"
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
