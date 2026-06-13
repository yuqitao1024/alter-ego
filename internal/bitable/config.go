package bitable

import (
	"fmt"
	"os"
	"strings"
)

type FieldMapping struct {
	IssueKey        string
	IssueIID        string
	Title           string
	Description     string
	State           string
	Action          string
	Labels          string
	Author          string
	Assignees       string
	IssueURL        string
	CreatedAt       string
	UpdatedAt       string
	LastActor       string
	RelatedPRs      string
	RelatedPRURLs   string
	RelatedPRStatus string
	LastPRUpdatedAt string
}

type Config struct {
	AppID     string
	AppSecret string
	AppToken  string
	TableID   string
	BaseURL   string
	Fields    FieldMapping
}

func ConfigFromEnvOptional() (Config, bool, error) {
	return ConfigFromMapOptional(map[string]string{
		"ALTER_EGO_BITABLE_APP_ID":                   os.Getenv("ALTER_EGO_BITABLE_APP_ID"),
		"ALTER_EGO_BITABLE_APP_SECRET":               os.Getenv("ALTER_EGO_BITABLE_APP_SECRET"),
		"ALTER_EGO_BITABLE_APP_TOKEN":                os.Getenv("ALTER_EGO_BITABLE_APP_TOKEN"),
		"ALTER_EGO_BITABLE_TABLE_ID":                 os.Getenv("ALTER_EGO_BITABLE_TABLE_ID"),
		"ALTER_EGO_BITABLE_BASE_URL":                 os.Getenv("ALTER_EGO_BITABLE_BASE_URL"),
		"ALTER_EGO_BITABLE_FIELD_ISSUE_KEY":          os.Getenv("ALTER_EGO_BITABLE_FIELD_ISSUE_KEY"),
		"ALTER_EGO_BITABLE_FIELD_ISSUE_IID":          os.Getenv("ALTER_EGO_BITABLE_FIELD_ISSUE_IID"),
		"ALTER_EGO_BITABLE_FIELD_TITLE":              os.Getenv("ALTER_EGO_BITABLE_FIELD_TITLE"),
		"ALTER_EGO_BITABLE_FIELD_DESCRIPTION":        os.Getenv("ALTER_EGO_BITABLE_FIELD_DESCRIPTION"),
		"ALTER_EGO_BITABLE_FIELD_STATE":              os.Getenv("ALTER_EGO_BITABLE_FIELD_STATE"),
		"ALTER_EGO_BITABLE_FIELD_ACTION":             os.Getenv("ALTER_EGO_BITABLE_FIELD_ACTION"),
		"ALTER_EGO_BITABLE_FIELD_LABELS":             os.Getenv("ALTER_EGO_BITABLE_FIELD_LABELS"),
		"ALTER_EGO_BITABLE_FIELD_AUTHOR":             os.Getenv("ALTER_EGO_BITABLE_FIELD_AUTHOR"),
		"ALTER_EGO_BITABLE_FIELD_ASSIGNEES":          os.Getenv("ALTER_EGO_BITABLE_FIELD_ASSIGNEES"),
		"ALTER_EGO_BITABLE_FIELD_ISSUE_URL":          os.Getenv("ALTER_EGO_BITABLE_FIELD_ISSUE_URL"),
		"ALTER_EGO_BITABLE_FIELD_CREATED_AT":         os.Getenv("ALTER_EGO_BITABLE_FIELD_CREATED_AT"),
		"ALTER_EGO_BITABLE_FIELD_UPDATED_AT":         os.Getenv("ALTER_EGO_BITABLE_FIELD_UPDATED_AT"),
		"ALTER_EGO_BITABLE_FIELD_LAST_ACTOR":         os.Getenv("ALTER_EGO_BITABLE_FIELD_LAST_ACTOR"),
		"ALTER_EGO_BITABLE_FIELD_RELATED_PRS":        os.Getenv("ALTER_EGO_BITABLE_FIELD_RELATED_PRS"),
		"ALTER_EGO_BITABLE_FIELD_RELATED_PR_URLS":    os.Getenv("ALTER_EGO_BITABLE_FIELD_RELATED_PR_URLS"),
		"ALTER_EGO_BITABLE_FIELD_RELATED_PR_STATUS":  os.Getenv("ALTER_EGO_BITABLE_FIELD_RELATED_PR_STATUS"),
		"ALTER_EGO_BITABLE_FIELD_LAST_PR_UPDATED_AT": os.Getenv("ALTER_EGO_BITABLE_FIELD_LAST_PR_UPDATED_AT"),
	})
}

func ConfigFromMapOptional(values map[string]string) (Config, bool, error) {
	appID := strings.TrimSpace(values["ALTER_EGO_BITABLE_APP_ID"])
	appSecret := strings.TrimSpace(values["ALTER_EGO_BITABLE_APP_SECRET"])
	appToken := strings.TrimSpace(values["ALTER_EGO_BITABLE_APP_TOKEN"])
	tableID := strings.TrimSpace(values["ALTER_EGO_BITABLE_TABLE_ID"])
	baseURL := strings.TrimSpace(values["ALTER_EGO_BITABLE_BASE_URL"])

	fields := FieldMapping{
		IssueKey:        strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_ISSUE_KEY"]),
		IssueIID:        strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_ISSUE_IID"]),
		Title:           strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_TITLE"]),
		Description:     strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_DESCRIPTION"]),
		State:           strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_STATE"]),
		Action:          strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_ACTION"]),
		Labels:          strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_LABELS"]),
		Author:          strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_AUTHOR"]),
		Assignees:       strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_ASSIGNEES"]),
		IssueURL:        strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_ISSUE_URL"]),
		CreatedAt:       strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_CREATED_AT"]),
		UpdatedAt:       strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_UPDATED_AT"]),
		LastActor:       strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_LAST_ACTOR"]),
		RelatedPRs:      strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_RELATED_PRS"]),
		RelatedPRURLs:   strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_RELATED_PR_URLS"]),
		RelatedPRStatus: strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_RELATED_PR_STATUS"]),
		LastPRUpdatedAt: strings.TrimSpace(values["ALTER_EGO_BITABLE_FIELD_LAST_PR_UPDATED_AT"]),
	}

	if appID == "" && appSecret == "" && appToken == "" && tableID == "" && baseURL == "" && fields == (FieldMapping{}) {
		return Config{}, false, nil
	}

	if appID == "" || appSecret == "" || appToken == "" || tableID == "" {
		return Config{}, false, fmt.Errorf("ALTER_EGO_BITABLE_APP_ID, ALTER_EGO_BITABLE_APP_SECRET, ALTER_EGO_BITABLE_APP_TOKEN, and ALTER_EGO_BITABLE_TABLE_ID are required")
	}
	if fields.IssueKey == "" {
		return Config{}, false, fmt.Errorf("ALTER_EGO_BITABLE_FIELD_ISSUE_KEY is required")
	}

	if baseURL == "" {
		baseURL = "https://open.feishu.cn"
	}

	return Config{
		AppID:     appID,
		AppSecret: appSecret,
		AppToken:  appToken,
		TableID:   tableID,
		BaseURL:   baseURL,
		Fields:    fields,
	}, true, nil
}
