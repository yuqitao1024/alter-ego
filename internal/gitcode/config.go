package gitcode

import (
	"fmt"
	"os"
	"strings"
)

const (
	VerificationModeToken     = "token"
	VerificationModeSignature = "signature"
)

type Config struct {
	Secret           string
	VerificationMode string
	DBPath           string
}

func ConfigFromEnvOptional() (Config, bool, error) {
	return ConfigFromMapOptional(map[string]string{
		"ALTER_EGO_GITCODE_WEBHOOK_SECRET":            os.Getenv("ALTER_EGO_GITCODE_WEBHOOK_SECRET"),
		"ALTER_EGO_GITCODE_WEBHOOK_VERIFICATION_MODE": os.Getenv("ALTER_EGO_GITCODE_WEBHOOK_VERIFICATION_MODE"),
		"ALTER_EGO_GITCODE_DB_PATH":                   os.Getenv("ALTER_EGO_GITCODE_DB_PATH"),
	})
}

func ConfigFromMapOptional(values map[string]string) (Config, bool, error) {
	secret := strings.TrimSpace(values["ALTER_EGO_GITCODE_WEBHOOK_SECRET"])
	mode := strings.TrimSpace(values["ALTER_EGO_GITCODE_WEBHOOK_VERIFICATION_MODE"])
	path := strings.TrimSpace(values["ALTER_EGO_GITCODE_DB_PATH"])

	if secret == "" && mode == "" && path == "" {
		return Config{}, false, nil
	}
	if secret == "" {
		return Config{}, false, fmt.Errorf("ALTER_EGO_GITCODE_WEBHOOK_SECRET is required")
	}
	if mode == "" {
		mode = VerificationModeToken
	}
	switch mode {
	case VerificationModeToken, VerificationModeSignature:
	default:
		return Config{}, false, fmt.Errorf("ALTER_EGO_GITCODE_WEBHOOK_VERIFICATION_MODE must be token or signature")
	}
	if path == "" {
		path = ".alterego/gitcode.db"
	}

	return Config{
		Secret:           secret,
		VerificationMode: mode,
		DBPath:           path,
	}, true, nil
}
