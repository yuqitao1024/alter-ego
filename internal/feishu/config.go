package feishu

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	AppID          string
	AppSecret      string
	Domain         string
	AllowUsers     map[string]bool
	AllowGroups    map[string]bool
	RequireMention bool
}

func ConfigFromEnv() (Config, error) {
	return ConfigFromMap(map[string]string{
		"ALTER_EGO_FEISHU_APP_ID":          os.Getenv("ALTER_EGO_FEISHU_APP_ID"),
		"ALTER_EGO_FEISHU_APP_SECRET":      os.Getenv("ALTER_EGO_FEISHU_APP_SECRET"),
		"ALTER_EGO_FEISHU_DOMAIN":          os.Getenv("ALTER_EGO_FEISHU_DOMAIN"),
		"ALTER_EGO_FEISHU_ALLOW_USERS":     os.Getenv("ALTER_EGO_FEISHU_ALLOW_USERS"),
		"ALTER_EGO_FEISHU_ALLOW_GROUPS":    os.Getenv("ALTER_EGO_FEISHU_ALLOW_GROUPS"),
		"ALTER_EGO_FEISHU_REQUIRE_MENTION": os.Getenv("ALTER_EGO_FEISHU_REQUIRE_MENTION"),
	})
}

func ConfigFromMap(values map[string]string) (Config, error) {
	cfg := Config{
		AppID:          strings.TrimSpace(values["ALTER_EGO_FEISHU_APP_ID"]),
		AppSecret:      strings.TrimSpace(values["ALTER_EGO_FEISHU_APP_SECRET"]),
		Domain:         strings.TrimSpace(values["ALTER_EGO_FEISHU_DOMAIN"]),
		AllowUsers:     parseCSVSet(values["ALTER_EGO_FEISHU_ALLOW_USERS"]),
		AllowGroups:    parseCSVSet(values["ALTER_EGO_FEISHU_ALLOW_GROUPS"]),
		RequireMention: true,
	}

	if cfg.AppID == "" {
		return Config{}, fmt.Errorf("ALTER_EGO_FEISHU_APP_ID is required")
	}
	if cfg.AppSecret == "" {
		return Config{}, fmt.Errorf("ALTER_EGO_FEISHU_APP_SECRET is required")
	}
	if cfg.Domain == "" {
		cfg.Domain = "feishu"
	}

	switch strings.ToLower(strings.TrimSpace(values["ALTER_EGO_FEISHU_REQUIRE_MENTION"])) {
	case "", "true", "1", "yes":
		cfg.RequireMention = true
	case "false", "0", "no":
		cfg.RequireMention = false
	default:
		return Config{}, fmt.Errorf("ALTER_EGO_FEISHU_REQUIRE_MENTION must be true or false")
	}

	return cfg, nil
}

func parseCSVSet(raw string) map[string]bool {
	set := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value != "" {
			set[value] = true
		}
	}
	return set
}
