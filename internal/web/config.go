package web

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	PublicBaseURL string
	ListenAddr    string
	SessionSecret string
	AllowUsers    map[string]bool
}

func ConfigFromEnv() (Config, error) {
	return ConfigFromMap(map[string]string{
		"ALTER_EGO_WEB_PUBLIC_BASE_URL": os.Getenv("ALTER_EGO_WEB_PUBLIC_BASE_URL"),
		"ALTER_EGO_WEB_LISTEN_ADDR":     os.Getenv("ALTER_EGO_WEB_LISTEN_ADDR"),
		"ALTER_EGO_WEB_SESSION_SECRET":  os.Getenv("ALTER_EGO_WEB_SESSION_SECRET"),
		"ALTER_EGO_LARK_ALLOW_USERS":    os.Getenv("ALTER_EGO_LARK_ALLOW_USERS"),
	})
}

func ConfigFromEnvOptional() (Config, bool, error) {
	return ConfigFromMapOptional(map[string]string{
		"ALTER_EGO_WEB_PUBLIC_BASE_URL": os.Getenv("ALTER_EGO_WEB_PUBLIC_BASE_URL"),
		"ALTER_EGO_WEB_LISTEN_ADDR":     os.Getenv("ALTER_EGO_WEB_LISTEN_ADDR"),
		"ALTER_EGO_WEB_SESSION_SECRET":  os.Getenv("ALTER_EGO_WEB_SESSION_SECRET"),
		"ALTER_EGO_LARK_ALLOW_USERS":    os.Getenv("ALTER_EGO_LARK_ALLOW_USERS"),
	})
}

func ConfigFromMap(values map[string]string) (Config, error) {
	cfg := Config{
		PublicBaseURL: strings.TrimSpace(values["ALTER_EGO_WEB_PUBLIC_BASE_URL"]),
		ListenAddr:    strings.TrimSpace(values["ALTER_EGO_WEB_LISTEN_ADDR"]),
		SessionSecret: strings.TrimSpace(values["ALTER_EGO_WEB_SESSION_SECRET"]),
		AllowUsers:    parseCSVSet(values["ALTER_EGO_LARK_ALLOW_USERS"]),
	}
	if cfg.PublicBaseURL == "" {
		cfg.PublicBaseURL = "https://dashboard.example.com"
	}
	if _, err := url.Parse(cfg.PublicBaseURL); err != nil {
		return Config{}, fmt.Errorf("ALTER_EGO_WEB_PUBLIC_BASE_URL is invalid: %w", err)
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:18080"
	}
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("ALTER_EGO_WEB_SESSION_SECRET is required")
	}
	if len(cfg.AllowUsers) == 0 {
		return Config{}, fmt.Errorf("ALTER_EGO_LARK_ALLOW_USERS is required for web login")
	}
	return cfg, nil
}

func ConfigFromMapOptional(values map[string]string) (Config, bool, error) {
	if strings.TrimSpace(values["ALTER_EGO_WEB_PUBLIC_BASE_URL"]) == "" &&
		strings.TrimSpace(values["ALTER_EGO_WEB_LISTEN_ADDR"]) == "" &&
		strings.TrimSpace(values["ALTER_EGO_WEB_SESSION_SECRET"]) == "" {
		return Config{}, false, nil
	}
	cfg, err := ConfigFromMap(values)
	if err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
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
