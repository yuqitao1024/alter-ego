package codexappserver

import "encoding/json"

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version,omitempty"`
}

type ClientOptions struct {
	URL        string
	ClientInfo ClientInfo
}

type InitializeRequest struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

type InitializeResult struct {
	UserAgent string `json:"userAgent,omitempty"`
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

type Thread struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

type Turn struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
}

type ThreadItem struct {
	ID      string          `json:"id"`
	Type    string          `json:"type,omitempty"`
	Status  string          `json:"status,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}
