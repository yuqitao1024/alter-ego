package agent

import (
	"fmt"
	"net/http"
	"testing"
)

func TestNewProviderReturnsOpenAIProvider(t *testing.T) {
	provider := NewProvider(Config{
		Provider: "openai",
		APIKey:   "sk-test",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-test",
	}, http.DefaultClient)

	if got := fmt.Sprintf("%T", provider); got != "*agent.OpenAIProvider" {
		t.Fatalf("provider type = %s", got)
	}
}

func TestNewProviderReturnsGLMProvider(t *testing.T) {
	provider := NewProvider(Config{
		Provider: "glm",
		APIKey:   "glm-test",
		BaseURL:  "https://open.bigmodel.cn/api/coding/paas/v4",
		Model:    "GLM-5.1",
	}, http.DefaultClient)

	if got := fmt.Sprintf("%T", provider); got != "*agent.GLMProvider" {
		t.Fatalf("provider type = %s", got)
	}
}

func TestNewProviderFallsBackToOpenAIProvider(t *testing.T) {
	provider := NewProvider(Config{
		Provider: "unknown",
		APIKey:   "sk-test",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-test",
	}, http.DefaultClient)

	if got := fmt.Sprintf("%T", provider); got != "*agent.OpenAIProvider" {
		t.Fatalf("provider type = %s", got)
	}
}
