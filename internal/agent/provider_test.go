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

func TestNewProviderReturnsDashScopeProvider(t *testing.T) {
	provider := NewProvider(Config{
		Provider: "dashscope",
		APIKey:   "glm-test",
		BaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:    "glm-5.1",
	}, http.DefaultClient)

	if got := fmt.Sprintf("%T", provider); got != "*agent.DashScopeProvider" {
		t.Fatalf("provider type = %s", got)
	}
}

func TestNewProviderDoesNotTreatGLMAsDashScope(t *testing.T) {
	provider := NewProvider(Config{
		Provider: "glm",
		APIKey:   "glm-test",
		BaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:    "glm-5.1",
	}, http.DefaultClient)

	if got := fmt.Sprintf("%T", provider); got != "*agent.OpenAIProvider" {
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

func TestProviderSystemRoleByProvider(t *testing.T) {
	openai := NewProvider(Config{
		Provider: "openai",
		APIKey:   "sk-test",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-test",
	}, http.DefaultClient)
	if openai.SystemRole() != "developer" {
		t.Fatalf("openai SystemRole = %q", openai.SystemRole())
	}

	dashscope := NewProvider(Config{
		Provider: "dashscope",
		APIKey:   "glm-test",
		BaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:    "glm-5.1",
	}, http.DefaultClient)
	if dashscope.SystemRole() != "system" {
		t.Fatalf("dashscope SystemRole = %q", dashscope.SystemRole())
	}
}
