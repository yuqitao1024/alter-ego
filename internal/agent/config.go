package agent

import (
	"os"
	"strings"
)

type Config struct {
	Provider string
	APIKey  string
	BaseURL string
	Model   string
}

func ConfigFromEnv() Config {
	return ConfigFromMap(map[string]string{
		"ALTER_EGO_LLM_PROVIDER":    os.Getenv("ALTER_EGO_LLM_PROVIDER"),
		"ALTER_EGO_LLM_API_KEY":     os.Getenv("ALTER_EGO_LLM_API_KEY"),
		"ALTER_EGO_LLM_BASE_URL":    os.Getenv("ALTER_EGO_LLM_BASE_URL"),
		"ALTER_EGO_LLM_MODEL":       os.Getenv("ALTER_EGO_LLM_MODEL"),
		"ALTER_EGO_OPENAI_API_KEY":  os.Getenv("ALTER_EGO_OPENAI_API_KEY"),
		"ALTER_EGO_OPENAI_BASE_URL": os.Getenv("ALTER_EGO_OPENAI_BASE_URL"),
		"ALTER_EGO_OPENAI_MODEL":    os.Getenv("ALTER_EGO_OPENAI_MODEL"),
	})
}

func ConfigFromMap(values map[string]string) Config {
	cfg := Config{
		Provider: strings.TrimSpace(values["ALTER_EGO_LLM_PROVIDER"]),
		APIKey:   strings.TrimSpace(values["ALTER_EGO_LLM_API_KEY"]),
		BaseURL:  strings.TrimSpace(values["ALTER_EGO_LLM_BASE_URL"]),
		Model:    strings.TrimSpace(values["ALTER_EGO_LLM_MODEL"]),
	}

	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.APIKey == "" {
		cfg.APIKey = strings.TrimSpace(values["ALTER_EGO_OPENAI_API_KEY"])
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = strings.TrimSpace(values["ALTER_EGO_OPENAI_BASE_URL"])
	}
	if cfg.Model == "" {
		cfg.Model = strings.TrimSpace(values["ALTER_EGO_OPENAI_MODEL"])
	}

	if cfg.BaseURL == "" {
		switch cfg.Provider {
		case "glm":
			cfg.BaseURL = "https://open.bigmodel.cn/api/coding/paas/v4"
		default:
			cfg.BaseURL = "https://api.openai.com/v1"
		}
	}

	return cfg
}
