package agent

import (
	"context"
	"net/http"
)

type Provider interface {
	CreateResponse(ctx context.Context, req ChatRequest) (string, error)
}

func NewProvider(cfg Config, httpClient *http.Client) Provider {
	switch cfg.Provider {
	case "glm":
		return NewGLMProvider(cfg, httpClient)
	default:
		return NewOpenAIProvider(cfg, httpClient)
	}
}
