package bitable

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Client struct {
	cfg        Config
	httpClient *http.Client

	mu         sync.Mutex
	token      string
	tokenUntil time.Time
}

func NewClient(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

func (c *Client) FindRecordByIssueKey(ctx context.Context, issueKey string) (string, map[string]any, error) {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return "", nil, err
	}

	filter := fmt.Sprintf(`CurrentValue.[%s]="%s"`, c.cfg.Fields.IssueKey, issueKey)
	endpoint := fmt.Sprintf(
		"%s/open-apis/bitable/v1/apps/%s/tables/%s/records?filter=%s",
		strings.TrimRight(c.cfg.BaseURL, "/"),
		c.cfg.AppToken,
		c.cfg.TableID,
		url.QueryEscape(filter),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []struct {
				RecordID string         `json:"record_id"`
				Fields   map[string]any `json:"fields"`
			} `json:"items"`
		} `json:"data"`
	}

	if err := c.doJSON(req, &payload); err != nil {
		return "", nil, err
	}
	if payload.Code != 0 {
		return "", nil, fmt.Errorf("bitable list records failed with code %d", payload.Code)
	}
	if len(payload.Data.Items) == 0 {
		return "", nil, nil
	}

	return payload.Data.Items[0].RecordID, payload.Data.Items[0].Fields, nil
}

func (c *Client) CreateRecord(ctx context.Context, fields map[string]any) (string, error) {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(map[string]any{
		"fields": fields,
	})
	if err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf(
		"%s/open-apis/bitable/v1/apps/%s/tables/%s/records",
		strings.TrimRight(c.cfg.BaseURL, "/"),
		c.cfg.AppToken,
		c.cfg.TableID,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Record struct {
				RecordID string `json:"record_id"`
			} `json:"record"`
		} `json:"data"`
	}

	if err := c.doJSON(req, &payload); err != nil {
		return "", err
	}
	if payload.Code != 0 {
		return "", fmt.Errorf("bitable create record failed with code %d", payload.Code)
	}

	return payload.Data.Record.RecordID, nil
}

func (c *Client) UpdateRecord(ctx context.Context, recordID string, fields map[string]any) error {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]any{
		"records": []map[string]any{
			{
				"record_id": recordID,
				"fields":    fields,
			},
		},
	})
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf(
		"%s/open-apis/bitable/v1/apps/%s/tables/%s/records/batch_update",
		strings.TrimRight(c.cfg.BaseURL, "/"),
		c.cfg.AppToken,
		c.cfg.TableID,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}

	if err := c.doJSON(req, &payload); err != nil {
		return err
	}
	if payload.Code != 0 {
		return fmt.Errorf("bitable update record failed with code %d", payload.Code)
	}

	return nil
}

func (c *Client) tenantAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	if c.token != "" && time.Now().Before(c.tokenUntil) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	body, err := json.Marshal(map[string]string{
		"app_id":     c.cfg.AppID,
		"app_secret": c.cfg.AppSecret,
	})
	if err != nil {
		return "", err
	}

	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/open-apis/auth/v3/tenant_access_token/internal"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var payload struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}

	if err := c.doJSON(req, &payload); err != nil {
		return "", err
	}
	if payload.Code != 0 {
		return "", fmt.Errorf("tenant access token request failed with code %d", payload.Code)
	}

	expiresIn := time.Duration(payload.Expire) * time.Second
	if payload.Expire > 60 {
		expiresIn -= 60 * time.Second
	}

	c.mu.Lock()
	c.token = payload.TenantAccessToken
	c.tokenUntil = time.Now().Add(expiresIn)
	c.mu.Unlock()

	return payload.TenantAccessToken, nil
}

func (c *Client) doJSON(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("feishu request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}

	return nil
}
