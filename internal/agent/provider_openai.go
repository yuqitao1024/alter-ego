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
	messages := req.Messages
	if len(messages) > 0 {
		leading := messages[0]
		role := strings.ToLower(strings.TrimSpace(leading.Role))
		if (role == "developer" || role == "system") && strings.TrimSpace(leading.Content) != "" {
			body.Instructions = leading.Content
			messages = messages[1:]
		}
	}
	for _, message := range messages {
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

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	bodyPreview := diagnosticSnippet(string(rawBody), 400)

	var decoded openAIResponse
	if err := json.Unmarshal(rawBody, &decoded); err != nil {
		return "", fmt.Errorf("decode openai response status=%d body=%q: %w", resp.StatusCode, bodyPreview, err)
	}
	if resp.StatusCode >= 400 {
		if decoded.Error != nil && decoded.Error.Message != "" {
			return "", fmt.Errorf("openai request failed status=%d message=%q body=%q", resp.StatusCode, decoded.Error.Message, bodyPreview)
		}
		return "", fmt.Errorf("openai request failed status=%d body=%q", resp.StatusCode, bodyPreview)
	}
	output := strings.TrimSpace(decoded.OutputText)
	if output == "" {
		output = strings.TrimSpace(extractOpenAIOutputText(decoded.Output))
	}
	if output == "" {
		firstOutputRaw := ""
		var envelope struct {
			Status string            `json:"status"`
			Output []json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(rawBody, &envelope); err == nil && len(envelope.Output) > 0 {
			firstOutputRaw = diagnosticSnippet(string(envelope.Output[0]), 1200)
			if decoded.Status == "" {
				decoded.Status = envelope.Status
			}
		}
		return "", fmt.Errorf(
			"openai response contained empty output_text status=%d response_status=%q output_items=%d first_output=%q body=%q",
			resp.StatusCode,
			decoded.Status,
			len(decoded.Output),
			firstOutputRaw,
			bodyPreview,
		)
	}
	return output, nil
}

func (p *OpenAIProvider) SystemRole() string {
	return "developer"
}

type openAIResponseRequest struct {
	Model        string               `json:"model"`
	Instructions string               `json:"instructions,omitempty"`
	Input        []openAIInputMessage `json:"input"`
}

type openAIInputMessage struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	OutputText string `json:"output_text"`
	Status     string `json:"status"`
	Output     []openAIOutputItem `json:"output"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type openAIOutputItem struct {
	Type    string                    `json:"type"`
	Role    string                    `json:"role"`
	Content []openAIOutputContentItem `json:"content"`
}

type openAIOutputContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func extractOpenAIOutputText(items []openAIOutputItem) string {
	for _, item := range items {
		for _, content := range item.Content {
			if strings.EqualFold(strings.TrimSpace(content.Type), "output_text") {
				text := strings.TrimSpace(content.Text)
				if text != "" {
					return text
				}
			}
		}
	}
	return ""
}
