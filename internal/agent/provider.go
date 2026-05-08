package agent

import (
	"context"
	"net/http"
)

type Provider interface {
	CreateResponse(ctx context.Context, req ChatRequest) (string, error)
	SystemRole() string
}

func NewProvider(cfg Config, httpClient *http.Client) Provider {
	switch cfg.Provider {
	case "dashscope":
		return NewDashScopeProvider(cfg, httpClient)
	default:
		return NewOpenAIProvider(cfg, httpClient)
	}
}
