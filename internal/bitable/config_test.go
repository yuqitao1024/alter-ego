package bitable

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

func TestConfigFromMapOptionalParsesCredentialsBaseURLAndFieldMapping(t *testing.T) {
	cfg, enabled, err := ConfigFromMapOptional(map[string]string{
		"ALTER_EGO_BITABLE_APP_ID":                   "cli_test",
		"ALTER_EGO_BITABLE_APP_SECRET":               "secret",
		"ALTER_EGO_BITABLE_APP_TOKEN":                "bascn_token",
		"ALTER_EGO_BITABLE_TABLE_ID":                 "tblIssue",
		"ALTER_EGO_BITABLE_FIELD_ISSUE_KEY":          "IssueKey",
		"ALTER_EGO_BITABLE_FIELD_ISSUE_IID":          "IssueIID",
		"ALTER_EGO_BITABLE_FIELD_TITLE":              "Title",
		"ALTER_EGO_BITABLE_FIELD_DESCRIPTION":        "Description",
		"ALTER_EGO_BITABLE_FIELD_STATE":              "State",
		"ALTER_EGO_BITABLE_FIELD_ACTION":             "Action",
		"ALTER_EGO_BITABLE_FIELD_LABELS":              "Labels",
		"ALTER_EGO_BITABLE_FIELD_AUTHOR":             "Author",
		"ALTER_EGO_BITABLE_FIELD_ASSIGNEES":          "Assignees",
		"ALTER_EGO_BITABLE_FIELD_ISSUE_URL":          "IssueURL",
		"ALTER_EGO_BITABLE_FIELD_CREATED_AT":         "CreatedAt",
		"ALTER_EGO_BITABLE_FIELD_UPDATED_AT":         "UpdatedAt",
		"ALTER_EGO_BITABLE_FIELD_LAST_ACTOR":         "LastActor",
		"ALTER_EGO_BITABLE_FIELD_RELATED_PRS":        "RelatedPRs",
		"ALTER_EGO_BITABLE_FIELD_RELATED_PR_URLS":    "RelatedPRURLs",
		"ALTER_EGO_BITABLE_FIELD_RELATED_PR_STATUS":  "RelatedPRStatus",
		"ALTER_EGO_BITABLE_FIELD_LAST_PR_UPDATED_AT": "LastPRUpdatedAt",
	})
	if err != nil {
		t.Fatalf("ConfigFromMapOptional returned error: %v", err)
	}
	if !enabled {
		t.Fatal("enabled = false, want true")
	}
	if cfg.BaseURL != "https://open.feishu.cn" {
		t.Fatalf("BaseURL = %q, want %q", cfg.BaseURL, "https://open.feishu.cn")
	}
	if cfg.AppID != "cli_test" || cfg.AppSecret != "secret" || cfg.AppToken != "bascn_token" || cfg.TableID != "tblIssue" {
		t.Fatalf("credentials were not parsed: %#v", cfg)
	}
	if cfg.Fields.IssueKey != "IssueKey" || cfg.Fields.RelatedPRStatus != "RelatedPRStatus" {
		t.Fatalf("field mapping was not parsed: %#v", cfg.Fields)
	}
}

func TestConfigFromMapOptionalRequiresIssueKeyField(t *testing.T) {
	_, _, err := ConfigFromMapOptional(map[string]string{
		"ALTER_EGO_BITABLE_APP_ID":     "cli_test",
		"ALTER_EGO_BITABLE_APP_SECRET": "secret",
		"ALTER_EGO_BITABLE_APP_TOKEN":  "bascn_token",
		"ALTER_EGO_BITABLE_TABLE_ID":   "tblIssue",
	})
	if err == nil {
		t.Fatal("ConfigFromMapOptional returned nil error, want missing field mapping error")
	}
}
