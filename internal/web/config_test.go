package web

import "testing"

func TestConfigFromMapParsesPhase1Defaults(t *testing.T) {
	t.Parallel()

	cfg, err := ConfigFromMap(map[string]string{
		"ALTER_EGO_WEB_PUBLIC_BASE_URL": "https://dashboard.example.com",
		"ALTER_EGO_WEB_LISTEN_ADDR":     "127.0.0.1:18080",
		"ALTER_EGO_WEB_SESSION_SECRET":  "test-secret",
		"ALTER_EGO_LARK_ALLOW_USERS":    "ou_allowed_1,ou_allowed_2",
	})
	if err != nil {
		t.Fatalf("ConfigFromMap returned error: %v", err)
	}

	if cfg.PublicBaseURL != "https://dashboard.example.com" {
		t.Fatalf("PublicBaseURL = %q", cfg.PublicBaseURL)
	}
	if cfg.ListenAddr != "127.0.0.1:18080" {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.SessionSecret != "test-secret" {
		t.Fatalf("SessionSecret = %q", cfg.SessionSecret)
	}
	if !cfg.AllowUsers["ou_allowed_1"] || !cfg.AllowUsers["ou_allowed_2"] {
		t.Fatalf("AllowUsers = %#v", cfg.AllowUsers)
	}
}

func TestConfigFromMapRequiresSessionSecret(t *testing.T) {
	t.Parallel()

	_, err := ConfigFromMap(map[string]string{
		"ALTER_EGO_WEB_PUBLIC_BASE_URL": "https://dashboard.example.com",
		"ALTER_EGO_LARK_ALLOW_USERS":    "ou_allowed_1",
	})
	if err == nil {
		t.Fatal("ConfigFromMap returned nil error, want missing session secret error")
	}
}

func TestConfigFromMapOptionalDisabledWithoutWebEnv(t *testing.T) {
	t.Parallel()

	_, enabled, err := ConfigFromMapOptional(map[string]string{
		"ALTER_EGO_LARK_ALLOW_USERS": "ou_allowed_1",
	})
	if err != nil {
		t.Fatalf("ConfigFromMapOptional returned error: %v", err)
	}
	if enabled {
		t.Fatal("enabled = true, want false")
	}
}
