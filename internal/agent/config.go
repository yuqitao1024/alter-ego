package agent

import (
	"os"
	"strings"
)

type Config struct {
	APIKey  string
	BaseURL string
	Model   string
}

func ConfigFromEnv() Config {
	return ConfigFromMap(map[string]string{
		"ALTER_EGO_OPENAI_API_KEY":  os.Getenv("ALTER_EGO_OPENAI_API_KEY"),
		"ALTER_EGO_OPENAI_BASE_URL": os.Getenv("ALTER_EGO_OPENAI_BASE_URL"),
		"ALTER_EGO_OPENAI_MODEL":    os.Getenv("ALTER_EGO_OPENAI_MODEL"),
	})
}

func ConfigFromMap(values map[string]string) Config {
	cfg := Config{
		APIKey:  strings.TrimSpace(values["ALTER_EGO_OPENAI_API_KEY"]),
		BaseURL: strings.TrimSpace(values["ALTER_EGO_OPENAI_BASE_URL"]),
		Model:   strings.TrimSpace(values["ALTER_EGO_OPENAI_MODEL"]),
	}

	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}

	return cfg
}
