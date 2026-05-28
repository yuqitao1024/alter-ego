package web

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

type OAuthToken struct {
	AccessToken string
}

type OAuthUser struct {
	OpenID string
	Name   string
}

type OAuthClient interface {
	BuildAuthorizeURL(state string) (string, error)
	ExchangeCode(ctx context.Context, code string) (OAuthToken, error)
	FetchUser(ctx context.Context, accessToken string) (OAuthUser, error)
}

type LarkOAuthClient struct {
	AppID       string
	AppSecret   string
	BaseURL     string
	RedirectURI string
	HTTPClient  *http.Client
}

func (c LarkOAuthClient) BuildAuthorizeURL(state string) (string, error) {
	u, err := url.Parse(c.authorizeBaseURL() + "/open-apis/authen/v1/authorize")
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("app_id", c.AppID)
	query.Set("redirect_uri", c.RedirectURI)
	query.Set("response_type", "code")
	query.Set("scope", "contact:user.base:readonly")
	query.Set("state", state)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (c LarkOAuthClient) ExchangeCode(ctx context.Context, code string) (OAuthToken, error) {
	requestBody, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"client_id":     c.AppID,
		"client_secret": c.AppSecret,
		"redirect_uri":  c.RedirectURI,
	})
	if err != nil {
		return OAuthToken{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL()+"/open-apis/authen/v2/oauth/token", bytes.NewReader(requestBody))
	if err != nil {
		return OAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return OAuthToken{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OAuthToken{}, err
	}
	if resp.StatusCode/100 != 2 {
		return OAuthToken{}, fmt.Errorf("lark token exchange failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return OAuthToken{}, err
	}
	if payload.Code != 0 || strings.TrimSpace(payload.Data.AccessToken) == "" {
		return OAuthToken{}, fmt.Errorf("lark token exchange rejected: code=%d msg=%s", payload.Code, payload.Msg)
	}
	return OAuthToken{AccessToken: payload.Data.AccessToken}, nil
}

func (c LarkOAuthClient) FetchUser(ctx context.Context, accessToken string) (OAuthUser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBaseURL()+"/open-apis/authen/v1/user_info", nil)
	if err != nil {
		return OAuthUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return OAuthUser{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return OAuthUser{}, err
	}
	if resp.StatusCode/100 != 2 {
		return OAuthUser{}, fmt.Errorf("lark user info failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OpenID string `json:"open_id"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return OAuthUser{}, err
	}
	if payload.Code != 0 || strings.TrimSpace(payload.Data.OpenID) == "" {
		return OAuthUser{}, fmt.Errorf("lark user info rejected: code=%d msg=%s", payload.Code, payload.Msg)
	}
	return OAuthUser{
		OpenID: payload.Data.OpenID,
		Name:   payload.Data.Name,
	}, nil
}

type StateStore struct {
	ttl   time.Duration
	now   func() time.Time
	mu    sync.Mutex
	items map[string]stateValue
}

type stateValue struct {
	redirectTo string
	expiresAt  time.Time
}

func NewStateStore(ttl time.Duration) *StateStore {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &StateStore{
		ttl:   ttl,
		now:   func() time.Time { return time.Now().UTC() },
		items: map[string]stateValue{},
	}
}

func (s *StateStore) Issue(redirectTo string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := fmt.Sprintf("%d", s.now().UnixNano())
	s.items[state] = stateValue{
		redirectTo: redirectTo,
		expiresAt:  s.now().Add(s.ttl),
	}
	return state
}

func (s *StateStore) Consume(state string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, ok := s.items[state]
	if !ok {
		return "", false
	}
	delete(s.items, state)
	if s.now().After(value.expiresAt) {
		return "", false
	}
	return value.redirectTo, true
}

func stringsTrimDefault(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

func (c LarkOAuthClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c LarkOAuthClient) apiBaseURL() string {
	return stringsTrimDefault(c.BaseURL, "https://open.feishu.cn")
}

func (c LarkOAuthClient) authorizeBaseURL() string {
	base := c.apiBaseURL()
	if strings.Contains(base, "open.feishu.cn") {
		return "https://accounts.feishu.cn"
	}
	return base
}
