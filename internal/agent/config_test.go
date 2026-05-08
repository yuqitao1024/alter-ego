package agent

import "testing"

func TestConfigFromMapParsesDefaults(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{})
	if cfg.Provider != "openai" {
		t.Fatalf("Provider = %q, want openai", cfg.Provider)
	}
	if cfg.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("BaseURL = %q, want https://api.openai.com/v1", cfg.BaseURL)
	}
	if cfg.Model != "" {
		t.Fatalf("Model = %q, want empty", cfg.Model)
	}
}

func TestConfigFromMapParsesValues(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		"ALTER_EGO_LLM_PROVIDER": "dashscope",
		"ALTER_EGO_LLM_API_KEY":  "sk-test",
		"ALTER_EGO_LLM_BASE_URL": "https://example.com/v1",
		"ALTER_EGO_LLM_MODEL":    "gpt-test",
	})

	if cfg.Provider != "dashscope" {
		t.Fatalf("Provider = %q", cfg.Provider)
	}
	if cfg.APIKey != "sk-test" {
		t.Fatalf("APIKey = %q", cfg.APIKey)
	}
	if cfg.BaseURL != "https://example.com/v1" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Model != "gpt-test" {
		t.Fatalf("Model = %q", cfg.Model)
	}
}

func TestConfigFromMapFallsBackToLegacyOpenAIVariables(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		"ALTER_EGO_OPENAI_API_KEY":  "sk-legacy",
		"ALTER_EGO_OPENAI_BASE_URL": "https://legacy.example.com/v1",
		"ALTER_EGO_OPENAI_MODEL":    "gpt-legacy",
	})

	if cfg.Provider != "openai" {
		t.Fatalf("Provider = %q", cfg.Provider)
	}
	if cfg.APIKey != "sk-legacy" || cfg.BaseURL != "https://legacy.example.com/v1" || cfg.Model != "gpt-legacy" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestConfigFromMapUsesDashScopeDefaultBaseURL(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		"ALTER_EGO_LLM_PROVIDER": "dashscope",
	})

	if cfg.BaseURL != "https://dashscope.aliyuncs.com/compatible-mode/v1" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
}
