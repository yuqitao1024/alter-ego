package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

type AppServerTransport interface {
	Send(ctx context.Context, request []byte) ([]byte, error)
	Recv(ctx context.Context) ([]byte, error)
	Close() error
}

type AppServerClient struct {
	transport AppServerTransport

	// The underlying proxy transport is a single shared byte stream. Until the
	// client grows response demultiplexing, it explicitly allows only one RPC in
	// flight at a time.
	rpcMu         sync.Mutex
	mu            sync.Mutex
	nextRequestID int
}

type ThreadStartRequest struct {
	Cwd              string `json:"cwd"`
	BaseInstructions string `json:"base_instructions"`
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

type AppServerThread struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

type AppServerTurn struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

type AppServerThreadItem struct {
	ID      string          `json:"id"`
	Type    string          `json:"type,omitempty"`
	Status  string          `json:"status,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type appServerRPCRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type appServerRPCResponse struct {
	ID     string                     `json:"id"`
	Result json.RawMessage            `json:"result"`
	Error  *appServerRPCResponseError `json:"error,omitempty"`
}

type appServerRPCResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewAppServerClient(transport AppServerTransport) *AppServerClient {
	return &AppServerClient{transport: transport}
}

func (c *AppServerClient) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

func (c *AppServerClient) StartThread(ctx context.Context, req ThreadStartRequest) (string, error) {
	var result struct {
		Thread AppServerThread `json:"thread"`
	}
	if err := c.call(ctx, "thread/start", req, &result); err != nil {
		return "", err
	}
	return result.Thread.ID, nil
}

func (c *AppServerClient) StartTurn(ctx context.Context, req TurnStartRequest) (string, error) {
	var result struct {
		Turn AppServerTurn `json:"turn"`
	}
	if err := c.call(ctx, "turn/start", req, &result); err != nil {
		return "", err
	}
	return result.Turn.ID, nil
}

func (c *AppServerClient) SteerTurn(ctx context.Context, req TurnSteerRequest) (string, error) {
	var result struct {
		TurnID string `json:"turnId"`
	}
	if err := c.call(ctx, "turn/steer", req, &result); err != nil {
		return "", err
	}
	return result.TurnID, nil
}

func (c *AppServerClient) InterruptTurn(ctx context.Context, req TurnInterruptRequest) error {
	return c.call(ctx, "turn/interrupt", req, nil)
}

func (c *AppServerClient) GetThread(ctx context.Context, req ThreadGetRequest) (AppServerThread, error) {
	var result struct {
		Thread AppServerThread `json:"thread"`
	}
	if err := c.call(ctx, "thread/get", req, &result); err != nil {
		return AppServerThread{}, err
	}
	return result.Thread, nil
}

func (c *AppServerClient) ListThreadItems(ctx context.Context, req ThreadItemsListRequest) ([]AppServerThreadItem, error) {
	var result struct {
		Items []AppServerThreadItem `json:"items"`
	}
	if err := c.call(ctx, "thread/items/list", req, &result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (c *AppServerClient) call(ctx context.Context, method string, params any, result any) error {
	if c == nil || c.transport == nil {
		return fmt.Errorf("app-server transport is not configured")
	}

	c.rpcMu.Lock()
	defer c.rpcMu.Unlock()

	requestID := c.allocateRequestID()
	requestBytes, err := json.Marshal(appServerRPCRequest{
		ID:     requestID,
		Method: method,
		Params: params,
	})
	if err != nil {
		return fmt.Errorf("%s: marshal request: %w", method, err)
	}

	if _, err := c.transport.Send(ctx, requestBytes); err != nil {
		return fmt.Errorf("%s: send request: %w", method, err)
	}

	responseBytes, err := c.transport.Recv(ctx)
	if err != nil {
		return fmt.Errorf("%s: receive response: %w", method, err)
	}
	if len(responseBytes) == 0 {
		return fmt.Errorf("%s: empty response", method)
	}

	var response appServerRPCResponse
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		return fmt.Errorf("%s: decode response: %w", method, err)
	}
	if response.ID != requestID {
		return fmt.Errorf("%s: response id %q does not match request id %q", method, response.ID, requestID)
	}
	if response.Error != nil {
		message := strings.TrimSpace(response.Error.Message)
		if message == "" {
			message = "unknown app-server error"
		}
		return fmt.Errorf("%s: %s", method, message)
	}
	if result == nil || len(response.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("%s: decode result: %w", method, err)
	}
	return nil
}

func (c *AppServerClient) allocateRequestID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextRequestID++
	return strconv.Itoa(c.nextRequestID)
}
