package codexappserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

var errClientClosed = errors.New("codex app-server client closed")

type Client struct {
	transport Transport

	mu            sync.Mutex
	nextID        int
	pending       map[string]chan callResult
	notifications chan rpcMessage

	closeOnce  sync.Once
	notifyOnce sync.Once
	readDone   chan struct{}
}

type callResult struct {
	message rpcMessage
	err     error
}

func NewClient(ctx context.Context, opts ClientOptions) (*Client, error) {
	transport, err := DialWebSocket(ctx, opts.URL)
	if err != nil {
		return nil, err
	}

	client := &Client{
		transport:     transport,
		pending:       make(map[string]chan callResult),
		notifications: make(chan rpcMessage, 16),
		readDone:      make(chan struct{}),
	}
	go client.readLoop()

	go func() {
		_, _ = client.Initialize(ctx, InitializeRequest{ClientInfo: opts.ClientInfo})
	}()

	return client, nil
}

func (c *Client) Initialize(ctx context.Context, req InitializeRequest) (InitializeResult, error) {
	var result InitializeResult
	if err := c.call(ctx, "initialize", req, &result); err != nil {
		return InitializeResult{}, err
	}
	return result, nil
}

func (c *Client) StartThread(ctx context.Context, req ThreadStartRequest) (string, error) {
	var result struct {
		Thread Thread `json:"thread"`
	}
	if err := c.call(ctx, "thread/start", req, &result); err != nil {
		return "", err
	}
	return result.Thread.ID, nil
}

func (c *Client) StartTurn(ctx context.Context, req TurnStartRequest) (string, error) {
	var result struct {
		Turn Turn `json:"turn"`
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
	if c == nil {
		return nil
	}

	var err error
	c.closeOnce.Do(func() {
		c.failAll(errClientClosed)
		if c.transport != nil {
			err = c.transport.Close()
		}
		<-c.readDone
		c.closeNotifications()
	})
	return err
}

func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	if c == nil || c.transport == nil {
		return errors.New("codex app-server transport is not configured")
	}

	requestID := c.nextRequestID()
	responseCh := make(chan callResult, 1)

	c.mu.Lock()
	c.pending[requestID] = responseCh
	c.mu.Unlock()

	requestBytes, err := json.Marshal(rpcMessage{
		ID:     requestID,
		Method: method,
		Params: params,
	})
	if err != nil {
		c.removePending(requestID)
		return fmt.Errorf("%s: marshal request: %w", method, err)
	}

	if err := c.transport.Send(ctx, requestBytes); err != nil {
		c.removePending(requestID)
		return fmt.Errorf("%s: send request: %w", method, err)
	}

	select {
	case response := <-responseCh:
		if response.err != nil {
			return fmt.Errorf("%s: receive response: %w", method, response.err)
		}
		if response.message.Error != nil {
			message := strings.TrimSpace(response.message.Error.Message)
			if message == "" {
				message = "unknown app-server error"
			}
			return fmt.Errorf("%s: %s", method, message)
		}
		if result == nil || len(response.message.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(response.message.Result, result); err != nil {
			return fmt.Errorf("%s: decode result: %w", method, err)
		}
		return nil
	case <-ctx.Done():
		c.removePending(requestID)
		return fmt.Errorf("%s: waiting for response: %w", method, ctx.Err())
	}
}

func (c *Client) readLoop() {
	defer close(c.readDone)
	defer c.closeNotifications()

	for {
		payload, err := c.transport.Recv(context.Background())
		if err != nil {
			c.failAll(err)
			return
		}

		var message rpcMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			c.failAll(fmt.Errorf("decode response: %w", err))
			return
		}

		if message.ID != "" {
			if c.routeResponse(message) {
				continue
			}
		}

		select {
		case c.notifications <- message:
		default:
		}
	}
}

func (c *Client) routeResponse(message rpcMessage) bool {
	c.mu.Lock()
	responseCh, ok := c.pending[message.ID]
	if ok {
		delete(c.pending, message.ID)
	}
	c.mu.Unlock()

	if !ok {
		return false
	}

	responseCh <- callResult{message: message}
	return true
}

func (c *Client) nextRequestID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	return strconv.Itoa(c.nextID)
}

func (c *Client) failAll(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan callResult)
	c.mu.Unlock()

	for _, responseCh := range pending {
		responseCh <- callResult{err: err}
	}
}

func (c *Client) removePending(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) closeNotifications() {
	c.notifyOnce.Do(func() {
		close(c.notifications)
	})
}
