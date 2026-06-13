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

	mu          sync.Mutex
	token       string
	tokenUntil  time.Time
	refreshing  bool
	refreshDone chan struct{}
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

	filter := fmt.Sprintf(`CurrentValue.[%s]="%s"`, c.cfg.Fields.IssueKey, escapeFilterValue(issueKey))
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
		return "", nil, feishuCodeError("bitable list records failed", payload.Code, payload.Msg)
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
		return "", feishuCodeError("bitable create record failed", payload.Code, payload.Msg)
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
		return feishuCodeError("bitable update record failed", payload.Code, payload.Msg)
	}

	return nil
}

func (c *Client) tenantAccessToken(ctx context.Context) (string, error) {
	for {
		c.mu.Lock()
		if c.token != "" && time.Now().Before(c.tokenUntil) {
			token := c.token
			c.mu.Unlock()
			return token, nil
		}
		if c.refreshing {
			done := c.refreshDone
			c.mu.Unlock()

			select {
			case <-done:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			continue
		}

		c.refreshing = true
		c.refreshDone = make(chan struct{})
		c.mu.Unlock()

		token, tokenUntil, err := c.requestTenantAccessToken(ctx)

		c.mu.Lock()
		if err == nil {
			c.token = token
			c.tokenUntil = tokenUntil
		}
		c.refreshing = false
		close(c.refreshDone)
		c.refreshDone = nil
		c.mu.Unlock()

		if err != nil {
			return "", err
		}
		return token, nil
	}
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

func (c *Client) requestTenantAccessToken(ctx context.Context) (string, time.Time, error) {
	body, err := json.Marshal(map[string]string{
		"app_id":     c.cfg.AppID,
		"app_secret": c.cfg.AppSecret,
	})
	if err != nil {
		return "", time.Time{}, err
	}

	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + "/open-apis/auth/v3/tenant_access_token/internal"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	var payload struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}

	if err := c.doJSON(req, &payload); err != nil {
		return "", time.Time{}, err
	}
	if payload.Code != 0 {
		return "", time.Time{}, feishuCodeError("tenant access token request failed", payload.Code, payload.Msg)
	}

	expiresIn := time.Duration(payload.Expire) * time.Second
	if payload.Expire > 60 {
		expiresIn -= 60 * time.Second
	}

	return payload.TenantAccessToken, time.Now().Add(expiresIn), nil
}

func escapeFilterValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return value
}

func feishuCodeError(prefix string, code int, msg string) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fmt.Errorf("%s with code %d", prefix, code)
	}
	return fmt.Errorf("%s with code %d: %s", prefix, code, msg)
}
