package feishu

import "testing"

func TestConfigFromEnvParsesDefaultsAndAllowlists(t *testing.T) {
	env := map[string]string{
		"ALTER_EGO_FEISHU_APP_ID":       "cli_test",
		"ALTER_EGO_FEISHU_APP_SECRET":   "secret",
		"ALTER_EGO_FEISHU_ALLOW_USERS":  "ou_1, ou_2",
		"ALTER_EGO_FEISHU_ALLOW_GROUPS": "oc_1,oc_2",
	}

	cfg, err := ConfigFromMap(env)
	if err != nil {
		t.Fatalf("ConfigFromMap returned error: %v", err)
	}

	if cfg.AppID != "cli_test" || cfg.AppSecret != "secret" {
		t.Fatalf("credentials were not parsed: %#v", cfg)
	}
	if cfg.Domain != "feishu" {
		t.Fatalf("domain = %q, want feishu", cfg.Domain)
	}
	if !cfg.RequireMention {
		t.Fatal("RequireMention = false, want true by default")
	}
	if !cfg.AllowUsers["ou_1"] || !cfg.AllowUsers["ou_2"] {
		t.Fatalf("allow users not parsed: %#v", cfg.AllowUsers)
	}
	if !cfg.AllowGroups["oc_1"] || !cfg.AllowGroups["oc_2"] {
		t.Fatalf("allow groups not parsed: %#v", cfg.AllowGroups)
	}
}

func TestConfigFromEnvRequiresCredentials(t *testing.T) {
	_, err := ConfigFromMap(map[string]string{})
	if err == nil {
		t.Fatal("ConfigFromMap returned nil error without credentials")
	}
}

func TestConfigFromEnvParsesRequireMentionFalse(t *testing.T) {
	cfg, err := ConfigFromMap(map[string]string{
		"ALTER_EGO_FEISHU_APP_ID":          "cli_test",
		"ALTER_EGO_FEISHU_APP_SECRET":      "secret",
		"ALTER_EGO_FEISHU_REQUIRE_MENTION": "false",
	})
	if err != nil {
		t.Fatalf("ConfigFromMap returned error: %v", err)
	}
	if cfg.RequireMention {
		t.Fatal("RequireMention = true, want false")
	}
}
