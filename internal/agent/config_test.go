package agent

import "testing"

func TestConfigFromMapParsesDefaults(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{})
	if cfg.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("BaseURL = %q, want https://api.openai.com/v1", cfg.BaseURL)
	}
	if cfg.Model != "" {
		t.Fatalf("Model = %q, want empty", cfg.Model)
	}
}

func TestConfigFromMapParsesValues(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		"ALTER_EGO_OPENAI_API_KEY":  "sk-test",
		"ALTER_EGO_OPENAI_BASE_URL": "https://example.com/v1",
		"ALTER_EGO_OPENAI_MODEL":    "gpt-test",
	})

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
