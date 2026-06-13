package gitcode

import "testing"

func TestConfigFromMapOptionalDisabledWhenUnset(t *testing.T) {
	_, enabled, err := ConfigFromMapOptional(map[string]string{})
	if err != nil {
		t.Fatalf("ConfigFromMapOptional returned error: %v", err)
	}
	if enabled {
		t.Fatal("enabled = true, want false")
	}
}

func TestConfigFromMapOptionalParsesDefaults(t *testing.T) {
	cfg, enabled, err := ConfigFromMapOptional(map[string]string{
		"ALTER_EGO_GITCODE_WEBHOOK_SECRET": "top-secret",
	})
	if err != nil {
		t.Fatalf("ConfigFromMapOptional returned error: %v", err)
	}
	if !enabled {
		t.Fatal("enabled = false, want true")
	}
	if cfg.VerificationMode != VerificationModeToken {
		t.Fatalf("VerificationMode = %q, want %q", cfg.VerificationMode, VerificationModeToken)
	}
	if cfg.DBPath != ".alterego/gitcode.db" {
		t.Fatalf("DBPath = %q, want %q", cfg.DBPath, ".alterego/gitcode.db")
	}
}

func TestConfigFromMapOptionalRejectsUnknownVerificationMode(t *testing.T) {
	_, _, err := ConfigFromMapOptional(map[string]string{
		"ALTER_EGO_GITCODE_WEBHOOK_SECRET":            "top-secret",
		"ALTER_EGO_GITCODE_WEBHOOK_VERIFICATION_MODE": "bogus",
	})
	if err == nil {
		t.Fatal("ConfigFromMapOptional returned nil error, want validation error")
	}
}
