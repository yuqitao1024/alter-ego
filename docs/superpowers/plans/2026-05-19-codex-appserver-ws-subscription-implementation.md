# Codex App-Server WS Subscription Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current SSH-proxied polling runtime with a dedicated `internal/codexappserver` websocket client/manager, add explicit machine initialization, and make orchestrator consume thread snapshots driven by app-server notifications.

**Architecture:** Keep SSH only for workspace preparation and explicit machine initialization. Move all app-server protocol, websocket connection management, notification routing, thread snapshots, and machine-init logic into `internal/codexappserver`, then narrow `internal/orchestrator` to a workspace-prep plus task-lifecycle adapter over that package. Use one long-lived websocket connection per machine, initialize it once, route notifications to thread watchers, and let orchestrator read in-memory snapshots instead of polling `thread/get` and `thread/items/list` on every tick.

**Tech Stack:** Go 1.23, `github.com/gorilla/websocket`, standard library concurrency primitives, standard library tests, existing SQLite-backed orchestrator store, existing SSH command helpers for remote shell execution.

---

## File Structure
- Create: `internal/codexappserver/protocol.go`
  - JSON-RPC request/response/notification envelopes and typed method payloads.
- Create: `internal/codexappserver/transport_ws.go`
  - Websocket dialer, framed read/write loop, close semantics.
- Create: `internal/codexappserver/client.go`
  - Request/response multiplexing, initialize handshake, typed RPC helpers, notification stream.
- Create: `internal/codexappserver/snapshot.go`
  - Thread snapshot model, item summarization helpers, watcher state transitions.
- Create: `internal/codexappserver/manager.go`
  - Machine-scoped shared connection manager, reconnect loop, watcher registration, snapshot access.
- Create: `internal/codexappserver/init.go`
  - Explicit machine initialization over SSH, systemd unit generation, service verification.
- Create: `internal/codexappserver/ssh.go`
  - Package-local SSH helper interfaces used only by machine init.
- Create: `internal/codexappserver/client_test.go`
  - Tests initialize handshake, out-of-order response routing, notification delivery.
- Create: `internal/codexappserver/transport_ws_test.go`
  - Tests websocket transport read/write and disconnect behavior with `httptest`.
- Create: `internal/codexappserver/snapshot_test.go`
  - Tests snapshot updates for `thread/started`, `turn/started`, `item/started`, `item/completed`, `turn/completed`.
- Create: `internal/codexappserver/manager_test.go`
  - Tests one shared connection per machine, watcher attach/detach, reconnect rehydration.
- Create: `internal/codexappserver/init_test.go`
  - Tests generated systemd unit and SSH command sequence.
- Modify: `internal/orchestrator/config.go`
  - Replace socket/bootstrap machine config with websocket/service-init fields.
- Modify: `internal/orchestrator/config_test.go`
  - Update machine YAML fixtures and validation expectations.
- Modify: `internal/orchestrator/runner.go`
  - Keep neutral remote-runner contracts, but make comments and reconnection behavior reflect snapshot-backed app-server runtime.
- Modify: `internal/orchestrator/appserver_runner.go`
  - Replace direct transport/client logic with `codexappserver.Manager`.
- Modify: `internal/orchestrator/appserver_runner_test.go`
  - Rework tests around fake manager and snapshot access instead of fake proxy transport.
- Modify: `internal/orchestrator/service.go`
  - Keep orchestration state machine, but consume `CaptureOutput` from snapshot-backed runner behavior.
- Modify: `internal/orchestrator/service_test.go`
  - Adjust fake runner expectations where turn identity is updated from snapshots.
- Modify: `internal/agent/command.go`
  - Add `/machine init <machine-id>` command and help output.
- Modify: `internal/agent/command_test.go`
  - Test `/machine init` command parsing, usage, and service invocation.
- Modify: `cmd/alterego/main.go`
  - Build `codexappserver.Manager`, wire machine init service, and close manager on shutdown.
- Modify: `cmd/alterego/main_test.go`
  - Update config fixtures for new machine fields and task subsystem construction.
- Delete: `internal/orchestrator/appserver_client.go`
  - Replaced by `internal/codexappserver/client.go`.
- Delete: `internal/orchestrator/appserver_client_test.go`
  - Replaced by package-local client tests in `internal/codexappserver`.
- Delete: `internal/orchestrator/appserver_proxy_ssh.go`
  - Runtime SSH proxy is removed entirely.
- Delete: `internal/orchestrator/appserver_proxy_ssh_test.go`
  - Runtime SSH fallback tests are removed with the proxy.
- Delete: `internal/orchestrator/appserver_types.go`
  - Replaced by `internal/codexappserver/snapshot.go`.

---

### Task 1: Replace Machine Config With Websocket-Service Fields

**Files:**
- Modify: `internal/orchestrator/config.go`
- Modify: `internal/orchestrator/config_test.go`
- Modify: `cmd/alterego/main_test.go`

- [ ] **Step 1: Write the failing config tests**

Add to `internal/orchestrator/config_test.go`:

```go
func TestLoadRegistryRequiresMachineAppServerWebSocketFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeConfigFile(t, root, "configs/machines/machine-a.yaml", `
id: machine_a
host: machine-a.example.com
user: coder
`)
	writeConfigFile(t, root, "configs/repositories/repo.yaml", `
id: repo
remote_repo_url: git@github.com:example/repo.git
remote_workspace_root: /srv/codex-tasks
default_branch: main
machine_ids:
  - machine_a
`)
	writeConfigFile(t, root, "configs/templates/template.yaml", `
id: feature_dev
repository_id: repo
workflow_path: docs/workflows/feature_dev.md
`)
	writeConfigFile(t, root, "docs/workflows/feature_dev.md", "workflow\n")

	_, err := LoadRegistry(root)
	if err == nil {
		t.Fatal("LoadRegistry returned nil error, want missing app-server field error")
	}
	for _, part := range []string{
		"app_server_listen_host",
		"app_server_listen_port",
		"app_server_service_name",
		"app_server_install_user",
	} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("LoadRegistry error = %q, want substring %q", err, part)
		}
	}
}

func TestMachineConfigAppServerWebSocketURL(t *testing.T) {
	t.Parallel()

	machine := MachineConfig{
		ID:                   "machine_a",
		Host:                 "machine-a.example.com",
		User:                 "coder",
		AppServerListenHost:  "0.0.0.0",
		AppServerListenPort:  4317,
		AppServerServiceName: "codex-app-server",
		AppServerInstallUser: "coder",
	}

	if got, want := machine.AppServerWebSocketURL(), "ws://machine-a.example.com:4317"; got != want {
		t.Fatalf("AppServerWebSocketURL() = %q, want %q", got, want)
	}
}
```

Update `cmd/alterego/main_test.go` with a subsystem fixture assertion that uses the new machine fields:

```go
func TestBuildTaskSubsystemRequiresMachineAppServerFields(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTaskConfigFixturesWithoutAppServerFields(t, root)

	_, err := buildTaskSubsystem(context.Background(), taskSubsystemConfig{
		RegistryRoot: root,
		DBPath:       filepath.Join(root, "orchestrator.db"),
		LLMConfig: agent.Config{
			Provider: "dashscope",
			APIKey:   "test-key",
			BaseURL:  "https://example.invalid/v1",
			Model:    "test-model",
		},
	})
	if err == nil {
		t.Fatal("buildTaskSubsystem returned nil error, want missing app-server fields error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/orchestrator ./cmd/alterego -count=1
```

Expected: FAIL because `MachineConfig` still exposes `app_server_socket` and has no `AppServerWebSocketURL` helper.

- [ ] **Step 3: Implement the new machine config schema**

Update `internal/orchestrator/config.go`:

```go
type MachineConfig struct {
	ID                   string   `yaml:"id"`
	DisplayName          string   `yaml:"display_name"`
	Host                 string   `yaml:"host"`
	Port                 int      `yaml:"port"`
	User                 string   `yaml:"user"`
	ShellInit            []string `yaml:"shell_init"`
	AppServerListenHost  string   `yaml:"app_server_listen_host"`
	AppServerListenPort  int      `yaml:"app_server_listen_port"`
	AppServerServiceName string   `yaml:"app_server_service_name"`
	AppServerInstallUser string   `yaml:"app_server_install_user"`
}

func (m *MachineConfig) Validate() error {
	if err := requireFields("machine", m.ID, []requiredField{
		{name: "id", value: m.ID},
		{name: "host", value: m.Host},
		{name: "user", value: m.User},
		{name: "app_server_listen_host", value: m.AppServerListenHost},
		{name: "app_server_service_name", value: m.AppServerServiceName},
		{name: "app_server_install_user", value: m.AppServerInstallUser},
	}); err != nil {
		return err
	}
	if m.AppServerListenPort <= 0 {
		return fmt.Errorf("machine %q is missing required field %q", m.ID, "app_server_listen_port")
	}
	return nil
}

func (m MachineConfig) AppServerWebSocketURL() string {
	host := strings.TrimSpace(m.Host)
	if strings.TrimSpace(m.AppServerListenHost) != "" && m.AppServerListenHost != "0.0.0.0" {
		host = strings.TrimSpace(m.AppServerListenHost)
	}
	return fmt.Sprintf("ws://%s:%d", host, m.AppServerListenPort)
}
```

Update the config fixtures in `internal/orchestrator/config_test.go` and `cmd/alterego/main_test.go` to use:

```yaml
app_server_listen_host: 0.0.0.0
app_server_listen_port: 4317
app_server_service_name: codex-app-server
app_server_install_user: coder
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/orchestrator ./cmd/alterego -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/config.go internal/orchestrator/config_test.go cmd/alterego/main_test.go
git commit -m "refactor: model websocket app-server machine config"
```

---

### Task 2: Build The Websocket Transport And JSON-RPC Client

**Files:**
- Create: `internal/codexappserver/protocol.go`
- Create: `internal/codexappserver/transport_ws.go`
- Create: `internal/codexappserver/client.go`
- Create: `internal/codexappserver/transport_ws_test.go`
- Create: `internal/codexappserver/client_test.go`

- [ ] **Step 1: Write the failing transport and client tests**

Create `internal/codexappserver/transport_ws_test.go`:

```go
package codexappserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocketTransportSendRecv(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error: %v", err)
			return
		}
		defer conn.Close()

		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage() error: %v", err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			t.Errorf("WriteMessage() error: %v", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	transport, err := DialWebSocket(ctx, wsURLFromHTTP(server.URL))
	if err != nil {
		t.Fatalf("DialWebSocket returned error: %v", err)
	}
	defer transport.Close()

	if err := transport.Send(ctx, []byte(`{"method":"ping"}`)); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	got, err := transport.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv returned error: %v", err)
	}
	if string(got) != `{"method":"ping"}` {
		t.Fatalf("Recv payload = %s", string(got))
	}
}

func wsURLFromHTTP(raw string) string {
	return "ws" + strings.TrimPrefix(raw, "http")
}
```

Create `internal/codexappserver/client_test.go`:

```go
package codexappserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestClientInitializesAndRoutesOutOfOrderResponses(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error: %v", err)
			return
		}
		defer conn.Close()

		var requests []rpcMessage
		for len(requests) < 3 {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("ReadMessage() error: %v", err)
				return
			}
			var msg rpcMessage
			if err := json.Unmarshal(payload, &msg); err != nil {
				t.Errorf("Unmarshal() error: %v", err)
				return
			}
			requests = append(requests, msg)
		}

		if requests[0].Method != "initialize" {
			t.Fatalf("first method = %q, want initialize", requests[0].Method)
		}

		if err := conn.WriteJSON(rpcMessage{ID: requests[0].ID, Result: mustJSON(t, map[string]any{"userAgent": "alterego-test"})}); err != nil {
			t.Fatalf("WriteJSON initialize response: %v", err)
		}
		if err := conn.WriteJSON(rpcMessage{ID: requests[2].ID, Result: mustJSON(t, map[string]any{"turn": map[string]any{"id": "turn-2"}})}); err != nil {
			t.Fatalf("WriteJSON second response: %v", err)
		}
		if err := conn.WriteJSON(rpcMessage{ID: requests[1].ID, Result: mustJSON(t, map[string]any{"thread": map[string]any{"id": "thread-1"}})}); err != nil {
			t.Fatalf("WriteJSON first response: %v", err)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientOptions{
		URL: wsURLFromHTTP(server.URL),
		ClientInfo: ClientInfo{
			Name:    "alterego",
			Title:   "Alter Ego",
			Version: "test",
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	defer client.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if _, err := client.StartThread(ctx, ThreadStartRequest{Cwd: "/srv/task/repo"}); err != nil {
			t.Errorf("StartThread returned error: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := client.StartTurn(ctx, TurnStartRequest{ThreadID: "thread-1", Input: "continue"}); err != nil {
			t.Errorf("StartTurn returned error: %v", err)
		}
	}()

	wg.Wait()
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	return data
}

func wsURLFromHTTP(raw string) string {
	return "ws" + strings.TrimPrefix(raw, "http")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/codexappserver -count=1
```

Expected: FAIL because the package and exported symbols do not exist yet.

- [ ] **Step 3: Implement websocket transport and RPC client**

Create `internal/codexappserver/protocol.go`:

```go
package codexappserver

import "encoding/json"

type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type rpcMessage struct {
	ID     string                     `json:"id,omitempty"`
	Method string                     `json:"method,omitempty"`
	Params json.RawMessage            `json:"params,omitempty"`
	Result json.RawMessage            `json:"result,omitempty"`
	Error  *rpcResponseError          `json:"error,omitempty"`
}

type rpcResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type InitializeRequest struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

type InitializeResult struct {
	UserAgent string `json:"userAgent"`
}

type ThreadStartRequest struct {
	Cwd              string `json:"cwd"`
	BaseInstructions string `json:"base_instructions,omitempty"`
}

type TurnStartRequest struct {
	ThreadID string `json:"thread_id"`
	Input    string `json:"input"`
}

type TurnSteerRequest struct {
	TurnID string `json:"turn_id"`
	Input  string `json:"input"`
}

type TurnInterruptRequest struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
}

type ThreadGetRequest struct {
	ThreadID string `json:"thread_id"`
}

type ThreadItemsListRequest struct {
	ThreadID string `json:"thread_id"`
}
```

Create `internal/codexappserver/transport_ws.go`:

```go
package codexappserver

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type WebSocketTransport struct {
	conn   *websocket.Conn
	readCh chan []byte
	errCh  chan error
	sendMu sync.Mutex
	once   sync.Once
}

func DialWebSocket(ctx context.Context, url string) (*WebSocketTransport, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, http.Header{})
	if err != nil {
		return nil, fmt.Errorf("dial websocket %q: %w", url, err)
	}
	transport := &WebSocketTransport{
		conn:   conn,
		readCh: make(chan []byte, 64),
		errCh:  make(chan error, 1),
	}
	go transport.readLoop()
	return transport, nil
}

func (t *WebSocketTransport) Send(_ context.Context, payload []byte) error {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	return t.conn.WriteMessage(websocket.TextMessage, payload)
}

func (t *WebSocketTransport) Recv(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-t.errCh:
		return nil, err
	case payload := <-t.readCh:
		return payload, nil
	}
}

func (t *WebSocketTransport) Close() error {
	var err error
	t.once.Do(func() {
		if t.conn != nil {
			err = t.conn.Close()
		}
	})
	return err
}

func (t *WebSocketTransport) readLoop() {
	defer close(t.readCh)
	for {
		_, payload, err := t.conn.ReadMessage()
		if err != nil {
			select {
			case t.errCh <- err:
			default:
			}
			return
		}
		t.readCh <- payload
	}
}
```

Create `internal/codexappserver/client.go`:

```go
package codexappserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

type ClientOptions struct {
	URL        string
	ClientInfo ClientInfo
}

type Client struct {
	transport     *WebSocketTransport
	notifications chan rpcMessage
	pendingMu     sync.Mutex
	pending       map[string]chan rpcMessage
	nextIDMu      sync.Mutex
	nextID        int
	closeOnce     sync.Once
}

func NewClient(ctx context.Context, opts ClientOptions) (*Client, error) {
	transport, err := DialWebSocket(ctx, opts.URL)
	if err != nil {
		return nil, err
	}
	client := &Client{
		transport:     transport,
		notifications: make(chan rpcMessage, 128),
		pending:       map[string]chan rpcMessage{},
	}
	go client.readLoop()
	if err := client.Initialize(ctx, opts.ClientInfo); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func (c *Client) Initialize(ctx context.Context, info ClientInfo) error {
	var result InitializeResult
	return c.call(ctx, "initialize", InitializeRequest{ClientInfo: info}, &result)
}

func (c *Client) StartThread(ctx context.Context, req ThreadStartRequest) (string, error) {
	var result struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := c.call(ctx, "thread/start", req, &result); err != nil {
		return "", err
	}
	return result.Thread.ID, nil
}

func (c *Client) StartTurn(ctx context.Context, req TurnStartRequest) (string, error) {
	var result struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := c.call(ctx, "turn/start", req, &result); err != nil {
		return "", err
	}
	return result.Turn.ID, nil
}

func (c *Client) SteerTurn(ctx context.Context, req TurnSteerRequest) (string, error) {
	var result struct {
		TurnID string `json:"turnId"`
	}
	if err := c.call(ctx, "turn/steer", req, &result); err != nil {
		return "", err
	}
	return result.TurnID, nil
}

func (c *Client) InterruptTurn(ctx context.Context, req TurnInterruptRequest) error {
	return c.call(ctx, "turn/interrupt", req, nil)
}

func (c *Client) Notifications() <-chan rpcMessage {
	return c.notifications
}

func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = c.transport.Close()
		c.failAll(context.Canceled)
		close(c.notifications)
	})
	return err
}

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	id := c.nextRequestID()
	payload, err := json.Marshal(struct {
		ID     string `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{
		ID:     id,
		Method: method,
		Params: params,
	})
	if err != nil {
		return fmt.Errorf("%s: marshal request: %w", method, err)
	}

	respCh := make(chan rpcMessage, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	if err := c.transport.Send(ctx, payload); err != nil {
		return fmt.Errorf("%s: send request: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-respCh:
		if resp.Error != nil {
			message := strings.TrimSpace(resp.Error.Message)
			if message == "" {
				message = "unknown app-server error"
			}
			return fmt.Errorf("%s: %s", method, message)
		}
		if result == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("%s: decode response: %w", method, err)
		}
		return nil
	}
}

func (c *Client) readLoop() {
	for {
		payload, err := c.transport.Recv(context.Background())
		if err != nil {
			c.failAll(err)
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			c.failAll(fmt.Errorf("decode app-server message: %w", err))
			return
		}
		if msg.ID != "" {
			c.pendingMu.Lock()
			respCh := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.pendingMu.Unlock()
			if respCh != nil {
				respCh <- msg
			}
			continue
		}
		c.notifications <- msg
	}
}

func (c *Client) nextRequestID() string {
	c.nextIDMu.Lock()
	defer c.nextIDMu.Unlock()
	c.nextID++
	return strconv.Itoa(c.nextID)
}

func (c *Client) failAll(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, respCh := range c.pending {
		respCh <- rpcMessage{ID: id, Error: &rpcResponseError{Message: err.Error()}}
		close(respCh)
		delete(c.pending, id)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/codexappserver -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codexappserver/protocol.go internal/codexappserver/transport_ws.go internal/codexappserver/client.go internal/codexappserver/transport_ws_test.go internal/codexappserver/client_test.go
git commit -m "feat: add codex app-server websocket client"
```

---

### Task 3: Add Thread Snapshots And A Shared Machine Connection Manager

**Files:**
- Create: `internal/codexappserver/snapshot.go`
- Create: `internal/codexappserver/manager.go`
- Create: `internal/codexappserver/snapshot_test.go`
- Create: `internal/codexappserver/manager_test.go`

- [ ] **Step 1: Write the failing snapshot and manager tests**

Create `internal/codexappserver/snapshot_test.go`:

```go
package codexappserver

import (
	"encoding/json"
	"testing"
)

func TestThreadWatcherAppliesTurnAndItemNotifications(t *testing.T) {
	t.Parallel()

	watcher := newThreadWatcher("thread-1")

	watcher.apply(notificationEnvelope("thread/started", `{"thread":{"id":"thread-1","status":"running"}}`))
	watcher.apply(notificationEnvelope("turn/started", `{"turn":{"id":"turn-1","status":"running","thread_id":"thread-1"}}`))
	watcher.apply(notificationEnvelope("item/started", `{"item":{"id":"item-1","type":"agent_message","text":"Planning"}}`))
	watcher.apply(notificationEnvelope("item/completed", `{"item":{"id":"item-1","type":"agent_message","text":"Planning complete"}}`))
	watcher.apply(notificationEnvelope("turn/completed", `{"turn":{"id":"turn-1","status":"completed","thread_id":"thread-1"}}`))

	snapshot := watcher.Snapshot()
	if snapshot.ThreadID != "thread-1" {
		t.Fatalf("snapshot.ThreadID = %q", snapshot.ThreadID)
	}
	if snapshot.ActiveTurnID != "turn-1" {
		t.Fatalf("snapshot.ActiveTurnID = %q", snapshot.ActiveTurnID)
	}
	if snapshot.ActiveTurnStatus != "completed" {
		t.Fatalf("snapshot.ActiveTurnStatus = %q", snapshot.ActiveTurnStatus)
	}
	if snapshot.LatestAgentMessage != "Planning complete" {
		t.Fatalf("snapshot.LatestAgentMessage = %q", snapshot.LatestAgentMessage)
	}
}

func notificationEnvelope(method string, payload string) rpcMessage {
	return rpcMessage{
		Method: method,
		Params: json.RawMessage(payload),
	}
}
```

Create `internal/codexappserver/manager_test.go`:

```go
package codexappserver

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestManagerReusesOneConnectionPerMachine(t *testing.T) {
	t.Parallel()

	var dialCount int32
	manager := NewManager(ManagerOptions{
		DialClient: func(ctx context.Context, machine MachineRuntimeConfig) (ClientAPI, error) {
			atomic.AddInt32(&dialCount, 1)
			return newFakeClient(), nil
		},
	})

	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: "ws://machine-a:4317"}

	if _, err := manager.WatchTaskThread(context.Background(), machine, "thread-1"); err != nil {
		t.Fatalf("WatchTaskThread thread-1 error: %v", err)
	}
	if _, err := manager.WatchTaskThread(context.Background(), machine, "thread-2"); err != nil {
		t.Fatalf("WatchTaskThread thread-2 error: %v", err)
	}
	if got := atomic.LoadInt32(&dialCount); got != 1 {
		t.Fatalf("dialCount = %d, want 1", got)
	}
}

type fakeClient struct {
	notifications chan rpcMessage
}

func newFakeClient() *fakeClient {
	return &fakeClient{notifications: make(chan rpcMessage)}
}

func (f *fakeClient) Close() error { return nil }
func (f *fakeClient) Notifications() <-chan rpcMessage { return f.notifications }
func (f *fakeClient) StartThread(context.Context, ThreadStartRequest) (string, error) { return "thread-1", nil }
func (f *fakeClient) StartTurn(context.Context, TurnStartRequest) (string, error) { return "turn-1", nil }
func (f *fakeClient) SteerTurn(context.Context, TurnSteerRequest) (string, error) { return "turn-1", nil }
func (f *fakeClient) InterruptTurn(context.Context, TurnInterruptRequest) error { return nil }
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/codexappserver -count=1
```

Expected: FAIL because the snapshot model and manager do not exist yet.

- [ ] **Step 3: Implement the snapshot model and shared manager**

Create `internal/codexappserver/snapshot.go`:

```go
package codexappserver

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

type SubscriptionState string

const (
	SubscriptionStateConnecting SubscriptionState = "connecting"
	SubscriptionStateActive     SubscriptionState = "active"
	SubscriptionStateError      SubscriptionState = "error"
)

type ThreadSnapshot struct {
	ThreadID              string
	ThreadStatus          string
	ActiveTurnID          string
	ActiveTurnStatus      string
	LastItemID            string
	LastActivityAt        time.Time
	LatestAgentMessage    string
	LatestPlan            string
	LatestCommand         string
	LatestSummary         string
	SubscriptionState     SubscriptionState
	LastSubscriptionError string
}

type ThreadWatcher struct {
	threadID string
	mu       sync.RWMutex
	snapshot ThreadSnapshot
}

func newThreadWatcher(threadID string) *ThreadWatcher {
	return &ThreadWatcher{
		threadID: threadID,
		snapshot: ThreadSnapshot{
			ThreadID:          threadID,
			SubscriptionState: SubscriptionStateConnecting,
		},
	}
}

func (w *ThreadWatcher) Snapshot() ThreadSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshot
}

func (w *ThreadWatcher) apply(msg rpcMessage) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.snapshot.LastActivityAt = time.Now().UTC()
	w.snapshot.SubscriptionState = SubscriptionStateActive

	switch msg.Method {
	case "thread/started":
		var payload struct {
			Thread struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"thread"`
		}
		_ = json.Unmarshal(msg.Params, &payload)
		w.snapshot.ThreadID = payload.Thread.ID
		w.snapshot.ThreadStatus = payload.Thread.Status
	case "turn/started", "turn/completed":
		var payload struct {
			Turn struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(msg.Params, &payload)
		w.snapshot.ActiveTurnID = payload.Turn.ID
		w.snapshot.ActiveTurnStatus = payload.Turn.Status
	case "item/started", "item/completed":
		var payload struct {
			Item struct {
				ID   string `json:"id"`
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
		}
		_ = json.Unmarshal(msg.Params, &payload)
		w.snapshot.LastItemID = payload.Item.ID
		switch payload.Item.Type {
		case "agent_message":
			w.snapshot.LatestAgentMessage = strings.TrimSpace(payload.Item.Text)
			w.snapshot.LatestSummary = strings.TrimSpace(payload.Item.Text)
		case "plan":
			w.snapshot.LatestPlan = strings.TrimSpace(payload.Item.Text)
		case "command":
			w.snapshot.LatestCommand = strings.TrimSpace(payload.Item.Text)
		}
	}
}
```

Create `internal/codexappserver/manager.go`:

```go
package codexappserver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type MachineRuntimeConfig struct {
	MachineID    string
	WebSocketURL string
}

type StartTaskSessionRequest struct {
	Cwd              string
	BaseInstructions string
	Input            string
}

type ClientAPI interface {
	Close() error
	Notifications() <-chan rpcMessage
	StartThread(ctx context.Context, req ThreadStartRequest) (string, error)
	StartTurn(ctx context.Context, req TurnStartRequest) (string, error)
	SteerTurn(ctx context.Context, req TurnSteerRequest) (string, error)
	InterruptTurn(ctx context.Context, req TurnInterruptRequest) error
}

type ManagerOptions struct {
	DialClient func(ctx context.Context, machine MachineRuntimeConfig) (ClientAPI, error)
}

type Manager struct {
	mu         sync.Mutex
	dialClient func(ctx context.Context, machine MachineRuntimeConfig) (ClientAPI, error)
	machines   map[string]*machineRuntime
}

type machineRuntime struct {
	machine  MachineRuntimeConfig
	client   ClientAPI
	watchers map[string]*ThreadWatcher
}

func NewManager(opts ManagerOptions) *Manager {
	return &Manager{
		dialClient: opts.DialClient,
		machines:   map[string]*machineRuntime{},
	}
}

func (m *Manager) StartTaskSession(ctx context.Context, machine MachineRuntimeConfig, req StartTaskSessionRequest) (string, string, error) {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return "", "", err
	}
	threadID, err := runtime.client.StartThread(ctx, ThreadStartRequest{
		Cwd:              req.Cwd,
		BaseInstructions: req.BaseInstructions,
	})
	if err != nil {
		return "", "", fmt.Errorf("start thread: %w", err)
	}
	turnID, err := runtime.client.StartTurn(ctx, TurnStartRequest{
		ThreadID: threadID,
		Input:    req.Input,
	})
	if err != nil {
		return "", "", fmt.Errorf("start turn: %w", err)
	}
	if _, err := m.WatchTaskThread(ctx, machine, threadID); err != nil {
		return "", "", err
	}
	return threadID, turnID, nil
}

func (m *Manager) WatchTaskThread(ctx context.Context, machine MachineRuntimeConfig, threadID string) (*ThreadWatcher, error) {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	watcher := runtime.watchers[threadID]
	if watcher == nil {
		watcher = newThreadWatcher(threadID)
		runtime.watchers[threadID] = watcher
	}
	return watcher, nil
}

func (m *Manager) Snapshot(machineID, threadID string) (ThreadSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.machines[machineID]
	if runtime == nil {
		return ThreadSnapshot{}, false
	}
	watcher := runtime.watchers[threadID]
	if watcher == nil {
		return ThreadSnapshot{}, false
	}
	return watcher.Snapshot(), true
}

func (m *Manager) SendTaskInput(ctx context.Context, machine MachineRuntimeConfig, threadID, activeTurnID, input string) (string, error) {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(activeTurnID) != "" {
		return runtime.client.SteerTurn(ctx, TurnSteerRequest{
			TurnID: activeTurnID,
			Input:  input,
		})
	}
	return runtime.client.StartTurn(ctx, TurnStartRequest{
		ThreadID: threadID,
		Input:    input,
	})
}

func (m *Manager) InterruptTask(ctx context.Context, machine MachineRuntimeConfig, threadID, activeTurnID string) error {
	runtime, err := m.ensureMachine(ctx, machine)
	if err != nil {
		return err
	}
	return runtime.client.InterruptTurn(ctx, TurnInterruptRequest{
		ThreadID: threadID,
		TurnID:   activeTurnID,
	})
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	for _, runtime := range m.machines {
		if err := runtime.client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) ensureMachine(ctx context.Context, machine MachineRuntimeConfig) (*machineRuntime, error) {
	m.mu.Lock()
	runtime := m.machines[machine.MachineID]
	if runtime != nil {
		m.mu.Unlock()
		return runtime, nil
	}
	m.mu.Unlock()

	client, err := m.dialClient(ctx, machine)
	if err != nil {
		return nil, err
	}

	runtime = &machineRuntime{
		machine:  machine,
		client:   client,
		watchers: map[string]*ThreadWatcher{},
	}

	m.mu.Lock()
	m.machines[machine.MachineID] = runtime
	m.mu.Unlock()

	go m.consumeNotifications(machine.MachineID, runtime)
	return runtime, nil
}

func (m *Manager) consumeNotifications(machineID string, runtime *machineRuntime) {
	for msg := range runtime.client.Notifications() {
		m.mu.Lock()
		for _, watcher := range runtime.watchers {
			watcher.apply(msg)
		}
		m.mu.Unlock()
	}
	m.markRuntimeError(machineID, "app-server websocket disconnected")
}

func (m *Manager) markRuntimeError(machineID, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.machines[machineID]
	if runtime == nil {
		return
	}
	for _, watcher := range runtime.watchers {
		watcher.mu.Lock()
		watcher.snapshot.SubscriptionState = SubscriptionStateError
		watcher.snapshot.LastSubscriptionError = message
		watcher.snapshot.LastActivityAt = time.Now().UTC()
		watcher.mu.Unlock()
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/codexappserver -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codexappserver/snapshot.go internal/codexappserver/manager.go internal/codexappserver/snapshot_test.go internal/codexappserver/manager_test.go
git commit -m "feat: add shared codex app-server manager"
```

---

### Task 4: Add Explicit Machine Init And `/machine init`

**Files:**
- Create: `internal/codexappserver/ssh.go`
- Create: `internal/codexappserver/init.go`
- Create: `internal/codexappserver/init_test.go`
- Modify: `internal/agent/command.go`
- Modify: `internal/agent/command_test.go`
- Modify: `cmd/alterego/main.go`

- [ ] **Step 1: Write the failing init and command tests**

Create `internal/codexappserver/init_test.go`:

```go
package codexappserver

import (
	"context"
	"strings"
	"testing"
)

func TestBuildSystemdUnitIncludesDangerousBypassAndWSListen(t *testing.T) {
	t.Parallel()

	unit := buildSystemdUnit(MachineInstallConfig{
		ServiceName: "codex-app-server",
		ListenHost:  "0.0.0.0",
		ListenPort:  4317,
		RunUser:     "coder",
	})

	for _, part := range []string{
		"ExecStart=/usr/bin/env codex app-server --listen ws://0.0.0.0:4317 --dangerously-bypass-approvals-and-sandbox",
		"User=coder",
		"Restart=always",
	} {
		if !strings.Contains(unit, part) {
			t.Fatalf("unit = %q, missing %q", unit, part)
		}
	}
}

func TestInstallerRunsSystemctlSequence(t *testing.T) {
	t.Parallel()

	ssh := &fakeSSHRunner{}
	installer := NewInstaller(ssh, func(machineID string) (MachineInstallConfig, error) {
		return MachineInstallConfig{
			MachineID:    machineID,
			Host:         "machine-a.example.com",
			Port:         22,
			SSHUser:      "ops",
			RunUser:      "coder",
			ListenHost:   "0.0.0.0",
			ListenPort:   4317,
			ServiceName:  "codex-app-server",
			ShellInit:    []string{"source ~/.zshrc"},
		}, nil
	})

	if err := installer.InitMachine(context.Background(), "machine_a"); err != nil {
		t.Fatalf("InitMachine returned error: %v", err)
	}
	if len(ssh.commands) != 1 {
		t.Fatalf("ssh.commands = %d, want 1", len(ssh.commands))
	}
	for _, part := range []string{
		"command -v codex",
		"systemctl daemon-reload",
		"systemctl enable codex-app-server",
		"systemctl restart codex-app-server",
		"systemctl is-enabled codex-app-server",
		"systemctl is-active codex-app-server",
	} {
		if !strings.Contains(ssh.commands[0], part) {
			t.Fatalf("command = %q, missing %q", ssh.commands[0], part)
		}
	}
}

type fakeSSHRunner struct {
	commands []string
}

func (f *fakeSSHRunner) Run(_ context.Context, _ string, _ int, _ string, command string) (string, error) {
	f.commands = append(f.commands, command)
	return "", nil
}
```

Add to `internal/agent/command_test.go`:

```go
func TestCommandHandlerMachineInitInvokesService(t *testing.T) {
	handler := NewCommandHandler(Config{}, NewSessionStore(12), &fakeMachineInitService{})
	event := channel.MessageEvent{
		Text: "/machine init machine_a",
		Conversation: channel.Conversation{
			ID:   "oc_1",
			Kind: channel.ConversationDirect,
		},
	}

	reply, err := handler.HandleCommand(context.Background(), event)
	if err != nil {
		t.Fatalf("HandleCommand returned error: %v", err)
	}
	if reply.Text != "Machine machine_a initialized for Codex App Server." {
		t.Fatalf("reply.Text = %q", reply.Text)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/codexappserver ./internal/agent -count=1
```

Expected: FAIL because installer and command wiring do not exist.

- [ ] **Step 3: Implement machine init and command wiring**

Create `internal/codexappserver/init.go`:

```go
package codexappserver

import (
	"context"
	"fmt"
	"strings"
)

type MachineInstallConfig struct {
	MachineID    string
	Host         string
	Port         int
	SSHUser      string
	RunUser      string
	ListenHost   string
	ListenPort   int
	ServiceName  string
	ShellInit    []string
}

type sshRunner interface {
	Run(ctx context.Context, host string, port int, user string, command string) (string, error)
}

type Installer struct {
	ssh      sshRunner
	resolve  func(machineID string) (MachineInstallConfig, error)
}

func NewInstaller(ssh sshRunner, resolve func(machineID string) (MachineInstallConfig, error)) *Installer {
	return &Installer{ssh: ssh, resolve: resolve}
}

func (i *Installer) InitMachine(ctx context.Context, machineID string) error {
	cfg, err := i.resolve(machineID)
	if err != nil {
		return err
	}
	command := strings.Join([]string{
		"command -v codex >/dev/null 2>&1",
		writeSystemdUnitCommand(cfg.ServiceName, buildSystemdUnit(cfg)),
		"sudo systemctl daemon-reload",
		fmt.Sprintf("sudo systemctl enable %s", shellQuote(cfg.ServiceName)),
		fmt.Sprintf("sudo systemctl restart %s", shellQuote(cfg.ServiceName)),
		fmt.Sprintf("sudo systemctl is-enabled %s", shellQuote(cfg.ServiceName)),
		fmt.Sprintf("sudo systemctl is-active %s", shellQuote(cfg.ServiceName)),
	}, " && ")
	_, err = i.ssh.Run(ctx, cfg.Host, cfg.Port, cfg.SSHUser, command)
	return err
}

func buildSystemdUnit(cfg MachineInstallConfig) string {
	return fmt.Sprintf(`[Unit]
Description=Codex App Server
After=network-online.target

[Service]
Type=simple
User=%s
ExecStart=/usr/bin/env codex app-server --listen ws://%s:%d --dangerously-bypass-approvals-and-sandbox
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
`, cfg.RunUser, cfg.ListenHost, cfg.ListenPort)
}
```

Create `internal/codexappserver/ssh.go`:

```go
package codexappserver

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type ShellSSHRunner struct{}

func (ShellSSHRunner) Run(ctx context.Context, host string, port int, user string, command string) (string, error) {
	args := make([]string, 0, 4)
	if port > 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	args = append(args, user+"@"+host, command)
	output, err := exec.CommandContext(ctx, "ssh", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ssh %s@%s: %w: %s", user, host, err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func writeSystemdUnitCommand(serviceName, unit string) string {
	path := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	return fmt.Sprintf("sudo tee %s >/dev/null <<'EOF'\n%s\nEOF", shellQuote(path), unit)
}
```

Update `internal/agent/command.go`:

```go
type MachineInitService interface {
	InitMachine(ctx context.Context, machineID string) error
}

type CommandHandler struct {
	cfg       Config
	sessions  *SessionStore
	machines  MachineInitService
}

func NewCommandHandler(cfg Config, sessions *SessionStore, machines MachineInitService) *CommandHandler {
	return &CommandHandler{
		cfg:      cfg,
		sessions: sessions,
		machines: machines,
	}
}

func (h *CommandHandler) HandleCommand(ctx context.Context, event channel.MessageEvent) (channel.OutgoingMessage, error) {
	// existing setup...
	switch command {
	case "/help":
		reply.Text = "/help - show supported commands\n/status - show handler status\n/reset - clear current conversation context\n/machine init <machine-id> - install or repair Codex App Server on a machine"
	case "/machine":
		if len(fields) != 3 || fields[1] != "init" {
			reply.Text = "Usage: /machine init <machine-id>"
			return reply, nil
		}
		if h.machines == nil {
			reply.Text = "Machine init service is not configured."
			return reply, nil
		}
		if err := h.machines.InitMachine(ctx, fields[2]); err != nil {
			return reply, err
		}
		reply.Text = fmt.Sprintf("Machine %s initialized for Codex App Server.", fields[2])
	}
}
```

Update `cmd/alterego/main.go` to inject the machine init service:

```go
commandHandler := agent.NewCommandHandler(agentCfg, sessions, taskSubsystem.MachineInstaller)
```

Update `internal/agent/command_test.go` with the fake service:

```go
type fakeMachineInitService struct {
	machineID string
	err       error
}

func (f *fakeMachineInitService) InitMachine(_ context.Context, machineID string) error {
	f.machineID = machineID
	return f.err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/codexappserver ./internal/agent ./cmd/alterego -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codexappserver/ssh.go internal/codexappserver/init.go internal/codexappserver/init_test.go internal/agent/command.go internal/agent/command_test.go cmd/alterego/main.go
git commit -m "feat: add codex app-server machine init command"
```

---

### Task 5: Rewire Orchestrator To Use `codexappserver.Manager`

**Files:**
- Modify: `internal/orchestrator/appserver_runner.go`
- Modify: `internal/orchestrator/appserver_runner_test.go`
- Modify: `internal/orchestrator/runner.go`
- Modify: `internal/orchestrator/service.go`
- Modify: `internal/orchestrator/service_test.go`
- Modify: `cmd/alterego/main.go`

- [ ] **Step 1: Write the failing runner integration tests**

Add to `internal/orchestrator/appserver_runner_test.go`:

```go
func TestAppServerRunnerCaptureOutputReadsSnapshotInsteadOfPolling(t *testing.T) {
	runtime := &fakeCodexRuntime{
		snapshots: map[string]codexappserver.ThreadSnapshot{
			"machine_a/thread-1": {
				ThreadID:           "thread-1",
				ThreadStatus:       "running",
				ActiveTurnID:       "turn-1",
				LatestAgentMessage: "Applied migration and running tests",
				LatestSummary:      "Applied migration and running tests",
			},
		},
	}

	runner := NewAppServerRunner(runtime)
	window, err := runner.CaptureOutput(context.Background(), RemoteSession{
		MachineID: "machine_a",
		ThreadID:  "thread-1",
	})
	if err != nil {
		t.Fatalf("CaptureOutput returned error: %v", err)
	}
	if window.Summary != "Applied migration and running tests" {
		t.Fatalf("window.Summary = %q", window.Summary)
	}
}

func TestAppServerRunnerStartInteractiveSessionStartsWatcher(t *testing.T) {
	runtime := &fakeCodexRuntime{
		startThreadID: "thread-1",
		startTurnID:   "turn-1",
	}
	runner := NewAppServerRunner(runtime)
	runner.machineResolver = func(machineID string) (MachineConfig, error) {
		return MachineConfig{
			ID:                   machineID,
			Host:                 "machine-a.example.com",
			User:                 "coder",
			AppServerListenHost:  "0.0.0.0",
			AppServerListenPort:  4317,
			AppServerServiceName: "codex-app-server",
			AppServerInstallUser: "coder",
		}, nil
	}

	session, err := runner.StartInteractiveSession(context.Background(), StartRequest{
		Machine: MachineConfig{
			ID:                   "machine_a",
			Host:                 "machine-a.example.com",
			User:                 "coder",
			AppServerListenHost:  "0.0.0.0",
			AppServerListenPort:  4317,
			AppServerServiceName: "codex-app-server",
			AppServerInstallUser: "coder",
		},
		RepositoryID:        "repo_backend",
		TaskID:              "task-1",
		RemoteRepoURL:       "git@github.com:example/backend.git",
		RemoteWorkspaceRoot: "/srv/codex-tasks",
		CheckoutBranch:      "main",
		UserRequest:         "Continue implementation",
		WorkflowContent:     "workflow",
	})
	if err != nil {
		t.Fatalf("StartInteractiveSession returned error: %v", err)
	}
	if session.ThreadID != "thread-1" || session.ActiveTurnID != "turn-1" {
		t.Fatalf("session = %#v", session)
	}
	if runtime.watchThreadID != "thread-1" {
		t.Fatalf("watchThreadID = %q, want thread-1", runtime.watchThreadID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/orchestrator -count=1
```

Expected: FAIL because `AppServerRunner` still depends on SSH proxy transport and polling client calls.

- [ ] **Step 3: Replace the runner internals with manager-backed calls**

Update `internal/orchestrator/appserver_runner.go`:

```go
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yuqitao1024/alter-ego/internal/codexappserver"
)

type codexRuntime interface {
	StartTaskSession(ctx context.Context, machine codexappserver.MachineRuntimeConfig, req codexappserver.StartTaskSessionRequest) (string, string, error)
	WatchTaskThread(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID string) (*codexappserver.ThreadWatcher, error)
	SendTaskInput(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID, activeTurnID, input string) (string, error)
	InterruptTask(ctx context.Context, machine codexappserver.MachineRuntimeConfig, threadID, activeTurnID string) error
	Snapshot(machineID, threadID string) (codexappserver.ThreadSnapshot, bool)
}

type AppServerRunner struct {
	transport       sshTransport
	manager         codexRuntime
	machineResolver func(machineID string) (MachineConfig, error)
}

func NewAppServerRunner(manager codexRuntime) *AppServerRunner {
	return &AppServerRunner{
		transport: shellSSHTransport{},
		manager:   manager,
		machineResolver: func(machineID string) (MachineConfig, error) {
			return MachineConfig{}, fmt.Errorf("machine resolver is not configured for %q", machineID)
		},
	}
}

func (r *AppServerRunner) StartInteractiveSession(ctx context.Context, req StartRequest) (RemoteSession, error) {
	repoDir := taskRepoWorkdir(req.RemoteWorkspaceRoot, req.TaskID)
	command := wrapRemoteCommand(req.Machine, buildPrepareWorkspaceCommand(req))
	if _, err := r.runWorkspaceCommand(ctx, req.Machine, "prepare remote workspace", command); err != nil {
		return RemoteSession{}, err
	}

	threadID, turnID, err := r.manager.StartTaskSession(ctx, machineRuntimeConfig(req.Machine), codexappserver.StartTaskSessionRequest{
		Cwd:              repoDir,
		BaseInstructions: strings.TrimSpace(req.WorkflowContent),
		Input:            buildStartInput(req.WorkflowContent, req.UserRequest),
	})
	if err != nil {
		return RemoteSession{}, fmt.Errorf("start app-server session: %w", err)
	}

	return RemoteSession{
		MachineID:    req.Machine.ID,
		Workdir:      repoDir,
		ThreadID:     threadID,
		ActiveTurnID: turnID,
	}, nil
}

func (r *AppServerRunner) CaptureOutput(ctx context.Context, session RemoteSession) (OutputWindow, error) {
	snapshot, ok := r.manager.Snapshot(session.MachineID, session.ThreadID)
	if !ok {
		return OutputWindow{}, ErrAppServerThreadMissing
	}
	summary := firstNonEmpty(snapshot.LatestSummary, snapshot.LatestAgentMessage, snapshot.LatestPlan, snapshot.LatestCommand)
	return OutputWindow{
		RawOutput: summary,
		Summary:   summary,
		SessionState: SessionState{
			ThreadStatus: snapshot.ThreadStatus,
		},
	}, nil
}

func machineRuntimeConfig(machine MachineConfig) codexappserver.MachineRuntimeConfig {
	return codexappserver.MachineRuntimeConfig{
		MachineID:    machine.ID,
		WebSocketURL: machine.AppServerWebSocketURL(),
	}
}
```

Update `cmd/alterego/main.go` to build and inject the manager:

```go
	manager := codexappserver.NewManager(codexappserver.ManagerOptions{
		DialClient: func(ctx context.Context, machine codexappserver.MachineRuntimeConfig) (codexappserver.ClientAPI, error) {
			return codexappserver.NewClient(ctx, codexappserver.ClientOptions{
				URL: machine.WebSocketURL,
				ClientInfo: codexappserver.ClientInfo{
					Name:    "alterego",
					Title:   "Alter Ego",
					Version: "dev",
				},
			})
		},
	})

	runner := orchestrator.NewAppServerRunner(manager)
```

Update the `cmd/alterego/main.go` imports to include:

```go
import "io"
```

Update `buildTaskSubsystem` so it constructs both the manager and installer:

```go
	manager := codexappserver.NewManager(codexappserver.ManagerOptions{
		DialClient: func(ctx context.Context, machine codexappserver.MachineRuntimeConfig) (codexappserver.ClientAPI, error) {
			return codexappserver.NewClient(ctx, codexappserver.ClientOptions{
				URL: machine.WebSocketURL,
				ClientInfo: codexappserver.ClientInfo{
					Name:    "alterego",
					Title:   "Alter Ego",
					Version: "dev",
				},
			})
		},
	})

	installer := codexappserver.NewInstaller(codexappserver.ShellSSHRunner{}, func(machineID string) (codexappserver.MachineInstallConfig, error) {
		machine := registry.Machines[machineID]
		if machine == nil {
			return codexappserver.MachineInstallConfig{}, errors.New("unknown machine: " + machineID)
		}
		return codexappserver.MachineInstallConfig{
			MachineID:   machine.ID,
			Host:        machine.Host,
			Port:        machine.Port,
			SSHUser:     machine.User,
			RunUser:     machine.AppServerInstallUser,
			ListenHost:  machine.AppServerListenHost,
			ListenPort:  machine.AppServerListenPort,
			ServiceName: machine.AppServerServiceName,
			ShellInit:   append([]string(nil), machine.ShellInit...),
		}, nil
	})
```

Then return them from `taskSubsystem`:

```go
	return &taskSubsystem{
		Registry:         registry,
		Store:            store,
		Runner:           runner,
		Service:          service,
		TaskHandler:      agent.NewTaskCommandHandler(service),
		MachineInstaller: installer,
		Manager:          manager,
	}, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/orchestrator ./cmd/alterego -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/appserver_runner.go internal/orchestrator/appserver_runner_test.go internal/orchestrator/runner.go internal/orchestrator/service.go internal/orchestrator/service_test.go cmd/alterego/main.go
git commit -m "refactor: drive orchestrator from codex app-server snapshots"
```

---

### Task 6: Remove Legacy Runtime Files And Run Full Verification

**Files:**
- Delete: `internal/orchestrator/appserver_client.go`
- Delete: `internal/orchestrator/appserver_client_test.go`
- Delete: `internal/orchestrator/appserver_proxy_ssh.go`
- Delete: `internal/orchestrator/appserver_proxy_ssh_test.go`
- Delete: `internal/orchestrator/appserver_types.go`
- Modify: `cmd/alterego/main.go`
- Modify: `go.mod`

- [ ] **Step 1: Write a failing repository-wide build check**

Run:

```bash
go test ./... -count=1
```

Expected: FAIL until all references to `NewAppServerClient`, `AppServerTransport`, `SSHAppServerProxy`, `AppServerState`, and `app_server_socket` are removed.

- [ ] **Step 2: Delete the legacy runtime files and remove stale references**

Delete the old files:

```text
internal/orchestrator/appserver_client.go
internal/orchestrator/appserver_client_test.go
internal/orchestrator/appserver_proxy_ssh.go
internal/orchestrator/appserver_proxy_ssh_test.go
internal/orchestrator/appserver_types.go
```

If `go.mod` still lists `github.com/gorilla/websocket` as indirect, make it direct:

```go
require (
	github.com/gorilla/websocket v1.5.0
	github.com/larksuite/oapi-sdk-go/v3 v3.6.1
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.36.3
)
```

Update `cmd/alterego/main.go` shutdown handling so the manager is closed:

```go
type taskSubsystem struct {
	Registry         *orchestrator.Registry
	Store            *orchestrator.Store
	Runner           orchestrator.RemoteRunner
	Service          *orchestrator.Service
	TaskHandler      *agent.TaskCommandHandler
	MachineInstaller agent.MachineInitService
	Manager          io.Closer
}

func (s *taskSubsystem) Close() error {
	if s == nil {
		return nil
	}
	if s.Manager != nil {
		_ = s.Manager.Close()
	}
	if s.Store != nil {
		return s.Store.Close()
	}
	return nil
}
```

- [ ] **Step 3: Run targeted verification**

Run:

```bash
go test ./internal/codexappserver ./internal/orchestrator ./internal/agent ./cmd/alterego -count=1
go build ./cmd/alterego
```

Expected: both commands PASS.

- [ ] **Step 4: Run full verification**

Run:

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd/alterego/main.go internal/codexappserver internal/orchestrator internal/agent
git rm internal/orchestrator/appserver_client.go internal/orchestrator/appserver_client_test.go internal/orchestrator/appserver_proxy_ssh.go internal/orchestrator/appserver_proxy_ssh_test.go internal/orchestrator/appserver_types.go
git commit -m "refactor: remove legacy app-server runtime path"
```

---

## Spec Coverage Check

- Dedicated package boundary: covered by Tasks 2, 3, and 4 creating `internal/codexappserver`.
- Direct websocket runtime: covered by Tasks 2 and 5 replacing SSH proxy transport with `DialWebSocket` and manager wiring.
- Single shared connection per machine: covered by Task 3 manager tests and implementation.
- Subscription-driven snapshots instead of polling: covered by Tasks 3 and 5 through notification routing and snapshot-backed `CaptureOutput`.
- Explicit machine init command: covered by Task 4 installer plus `/machine init`.
- `--dangerously-bypass-approvals-and-sandbox`: covered by Task 4 systemd unit generation.
- No runtime SSH fallback: covered by Tasks 5 and 6 deleting `appserver_proxy_ssh.go`.
- No machine runtime state table in SQLite: preserved by Tasks 3 and 5 keeping snapshots in memory only.

## Placeholder Scan

- Checked for `TBD`, `TODO`, `implement later`, `add appropriate`, `write tests for the above`, and `similar to Task`.
- No placeholders remain. Every task names concrete files, concrete commands, and concrete test targets.

## Type Consistency Check

- `MachineConfig.AppServerWebSocketURL()` is introduced in Task 1 and reused consistently in Task 5.
- `MachineRuntimeConfig`, `StartTaskSessionRequest`, `ThreadSnapshot`, `ThreadWatcher`, and `ClientAPI` are introduced in Tasks 2 and 3 and referenced with the same names in Tasks 5 and 6.
- `/machine init` uses `MachineInitService.InitMachine(ctx, machineID)` consistently across Task 4 wiring.
