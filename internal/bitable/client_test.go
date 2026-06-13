package bitable

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientFindCreateAndUpdateIssueRecord(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	tokenRequests := 0
	listRequests := 0
	createRequests := 0
	updateRequests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			tokenRequests++
			if r.Method != http.MethodPost {
				t.Fatalf("token method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("token content-type = %q, want application/json", got)
			}

			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode token body: %v", err)
			}
			if body["app_id"] != "cli_test" {
				t.Fatalf("token app_id = %q", body["app_id"])
			}
			if body["app_secret"] != "secret" {
				t.Fatalf("token app_secret = %q", body["app_secret"])
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			})
		case "/open-apis/bitable/v1/apps/app_token/tables/tbl_issue/records":
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("authorization = %q, want Bearer tenant-token", got)
			}

			if r.Method == http.MethodGet {
				listRequests++
				filter := r.URL.Query().Get("filter")
				wantFilter := `CurrentValue.[IssueKey]="org/repo/issues/8"`
				if decoded, err := url.QueryUnescape(filter); err == nil && decoded != "" {
					filter = decoded
				}
				if filter != wantFilter {
					t.Fatalf("filter = %q, want %q", filter, wantFilter)
				}

				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 0,
					"data": map[string]any{
						"items": []map[string]any{},
					},
				})
				return
			}

			createRequests++
			if r.Method != http.MethodPost {
				t.Fatalf("create method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("create content-type = %q, want application/json", got)
			}

			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			fields, ok := body["fields"].(map[string]any)
			if !ok {
				t.Fatalf("create body fields = %#v", body["fields"])
			}
			if fields["IssueKey"] != "org/repo/issues/8" {
				t.Fatalf("create IssueKey = %#v", fields["IssueKey"])
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"record": map[string]any{"record_id": "rec_1"},
				},
			})
		case "/open-apis/bitable/v1/apps/app_token/tables/tbl_issue/records/batch_update":
			updateRequests++
			if r.Method != http.MethodPost {
				t.Fatalf("update method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("update authorization = %q, want Bearer tenant-token", got)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Fatalf("update content-type = %q, want application/json", got)
			}

			var body struct {
				Records []struct {
					RecordID string         `json:"record_id"`
					Fields   map[string]any `json:"fields"`
				} `json:"records"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			if len(body.Records) != 1 {
				t.Fatalf("update records len = %d, want 1", len(body.Records))
			}
			if body.Records[0].RecordID != "rec_1" {
				t.Fatalf("update record_id = %q", body.Records[0].RecordID)
			}
			if body.Records[0].Fields["State"] != "opened" {
				t.Fatalf("update state = %#v", body.Records[0].Fields["State"])
			}

			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		AppID:     "cli_test",
		AppSecret: "secret",
		AppToken:  "app_token",
		TableID:   "tbl_issue",
		BaseURL:   server.URL,
		Fields: FieldMapping{
			IssueKey: "IssueKey",
		},
	}, server.Client())

	recordID, fields, err := client.FindRecordByIssueKey(context.Background(), "org/repo/issues/8")
	if err != nil {
		t.Fatalf("FindRecordByIssueKey returned error: %v", err)
	}
	if recordID != "" {
		t.Fatalf("FindRecordByIssueKey recordID = %q, want empty", recordID)
	}
	if fields != nil {
		t.Fatalf("FindRecordByIssueKey fields = %#v, want nil", fields)
	}

	createdID, err := client.CreateRecord(context.Background(), map[string]any{"IssueKey": "org/repo/issues/8"})
	if err != nil {
		t.Fatalf("CreateRecord returned error: %v", err)
	}
	if createdID != "rec_1" {
		t.Fatalf("CreateRecord returned %q, want rec_1", createdID)
	}

	if err := client.UpdateRecord(context.Background(), "rec_1", map[string]any{"State": "opened"}); err != nil {
		t.Fatalf("UpdateRecord returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d, want 1", tokenRequests)
	}
	if listRequests != 1 {
		t.Fatalf("list requests = %d, want 1", listRequests)
	}
	if createRequests != 1 {
		t.Fatalf("create requests = %d, want 1", createRequests)
	}
	if updateRequests != 1 {
		t.Fatalf("update requests = %d, want 1", updateRequests)
	}
}

func TestClientFindRecordByIssueKeyReturnsExistingRecord(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			})
		case r.URL.Path == "/open-apis/bitable/v1/apps/app_token/tables/tbl_issue/records" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"items": []map[string]any{
						{
							"record_id": "rec_existing",
							"fields": map[string]any{
								"IssueKey": "org/repo/issues/9",
								"State":    "closed",
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		AppID:     "cli_test",
		AppSecret: "secret",
		AppToken:  "app_token",
		TableID:   "tbl_issue",
		BaseURL:   server.URL,
		Fields: FieldMapping{
			IssueKey: "IssueKey",
		},
	}, server.Client())

	recordID, fields, err := client.FindRecordByIssueKey(context.Background(), "org/repo/issues/9")
	if err != nil {
		t.Fatalf("FindRecordByIssueKey returned error: %v", err)
	}
	if recordID != "rec_existing" {
		t.Fatalf("recordID = %q, want rec_existing", recordID)
	}
	if got := fields["State"]; got != "closed" {
		t.Fatalf("fields[State] = %#v, want closed", got)
	}
}

func TestClientPropagatesBitableErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			})
		case strings.HasSuffix(r.URL.Path, "/records") && r.Method == http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 999, "msg": "create rejected"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		AppID:     "cli_test",
		AppSecret: "secret",
		AppToken:  "app_token",
		TableID:   "tbl_issue",
		BaseURL:   server.URL,
		Fields: FieldMapping{
			IssueKey: "IssueKey",
		},
	}, server.Client())

	_, err := client.CreateRecord(context.Background(), map[string]any{"IssueKey": "org/repo/issues/8"})
	if err == nil {
		t.Fatal("CreateRecord error = nil, want error")
	}
	if !strings.Contains(err.Error(), "bitable create record failed with code 999: create rejected") {
		t.Fatalf("CreateRecord error = %v", err)
	}
}

func TestClientConcurrentCallersShareOneTokenRefresh(t *testing.T) {
	t.Parallel()

	var tokenRequests atomic.Int32
	release := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			tokenRequests.Add(1)
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			})
		case r.URL.Path == "/open-apis/bitable/v1/apps/app_token/tables/tbl_issue/records" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"items": []map[string]any{},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		AppID:     "cli_test",
		AppSecret: "secret",
		AppToken:  "app_token",
		TableID:   "tbl_issue",
		BaseURL:   server.URL,
		Fields: FieldMapping{
			IssueKey: "IssueKey",
		},
	}, server.Client())

	const callers = 8
	started := make(chan struct{}, callers)
	errCh := make(chan error, callers)

	for i := 0; i < callers; i++ {
		go func() {
			started <- struct{}{}
			_, _, err := client.FindRecordByIssueKey(context.Background(), "org/repo/issues/8")
			errCh <- err
		}()
	}

	for i := 0; i < callers; i++ {
		<-started
	}

	time.Sleep(50 * time.Millisecond)
	close(release)

	for i := 0; i < callers; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("FindRecordByIssueKey returned error: %v", err)
		}
	}

	if got := tokenRequests.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
}

func TestClientEscapesFilterValue(t *testing.T) {
	t.Parallel()

	issueKey := `repo/"quoted"\path`
	wantFilter := `CurrentValue.[IssueKey]="repo/\"quoted\"\\path"`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			})
		case r.URL.Path == "/open-apis/bitable/v1/apps/app_token/tables/tbl_issue/records" && r.Method == http.MethodGet:
			filter := r.URL.Query().Get("filter")
			if filter != wantFilter {
				t.Fatalf("filter = %q, want %q", filter, wantFilter)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"items": []map[string]any{},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		AppID:     "cli_test",
		AppSecret: "secret",
		AppToken:  "app_token",
		TableID:   "tbl_issue",
		BaseURL:   server.URL,
		Fields: FieldMapping{
			IssueKey: "IssueKey",
		},
	}, server.Client())

	_, _, err := client.FindRecordByIssueKey(context.Background(), issueKey)
	if err != nil {
		t.Fatalf("FindRecordByIssueKey returned error: %v", err)
	}
}
