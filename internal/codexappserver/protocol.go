package codexappserver

import (
	"encoding/json"
	"strings"
)

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
	URL         string
	BearerToken string
	ClientInfo  ClientInfo
}

type InitializeRequest struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

type InitializeResult struct {
	UserAgent string `json:"userAgent,omitempty"`
}

type InitializedNotification struct{}

type InputItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type SandboxPolicy struct {
	Type          string   `json:"type"`
	WritableRoots []string `json:"writableRoots,omitempty"`
	NetworkAccess bool     `json:"networkAccess,omitempty"`
}

func WorkspaceWriteSandboxPolicy(root string) SandboxPolicy {
	policy := SandboxPolicy{
		Type:          "workspaceWrite",
		NetworkAccess: true,
	}
	if strings.TrimSpace(root) != "" {
		policy.WritableRoots = []string{root}
	}
	return policy
}

type ThreadStartRequest struct {
	Cwd              string `json:"cwd"`
	BaseInstructions string `json:"baseInstructions,omitempty"`
	ApprovalPolicy   string `json:"approvalPolicy,omitempty"`
	Sandbox          string `json:"sandbox,omitempty"`
}

type TurnStartRequest struct {
	ThreadID       string        `json:"threadId"`
	ExpectedTurnID string        `json:"expectedTurnId,omitempty"`
	Cwd            string        `json:"cwd,omitempty"`
	ApprovalPolicy string        `json:"approvalPolicy,omitempty"`
	SandboxPolicy  SandboxPolicy `json:"sandboxPolicy,omitempty"`
	Input          []InputItem   `json:"input"`
}

type TurnSteerRequest struct {
	ThreadID       string        `json:"threadId"`
	ExpectedTurnID string        `json:"expectedTurnId,omitempty"`
	ApprovalPolicy string        `json:"approvalPolicy,omitempty"`
	SandboxPolicy  SandboxPolicy `json:"sandboxPolicy,omitempty"`
	Input          []InputItem   `json:"input"`
}

type TurnInterruptRequest struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type ThreadResumeRequest struct {
	ThreadID string `json:"threadId"`
}

type Thread struct {
	ID     string          `json:"id"`
	Status json.RawMessage `json:"status,omitempty"`
	Turns  []Turn          `json:"turns,omitempty"`
}

type Turn struct {
	ID     string          `json:"id"`
	Status json.RawMessage `json:"status,omitempty"`
	Items  []ThreadItem    `json:"items,omitempty"`
}

type ThreadItem struct {
	ID      string          `json:"id"`
	Type    string          `json:"type,omitempty"`
	Status  string          `json:"status,omitempty"`
	Text    string          `json:"text,omitempty"`
	Command string          `json:"command,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type ServerRequest struct {
	RequestID string
	Method    string
	ThreadID  string
	TurnID    string
	Prompt    string
	RawParams json.RawMessage
}

type ThreadEvent struct {
	Message           rpcMessage
	ServerRequest     *ServerRequest
	ResolvedRequestID string
}

func DecodeServerRequest(msg rpcMessage) (ServerRequest, bool, error) {
	if msg.ID == "" || msg.Method == "" {
		return ServerRequest{}, false, nil
	}

	if !strings.HasSuffix(msg.Method, "/requestApproval") && !strings.HasSuffix(msg.Method, "/requestUserInput") {
		return ServerRequest{}, false, nil
	}

	params, ok := messageParams(msg)
	if !ok {
		return ServerRequest{}, false, nil
	}

	var payload struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Prompt   string `json:"prompt"`
		Thread   struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return ServerRequest{}, false, err
	}
	threadID := payload.ThreadID
	if threadID == "" {
		threadID = payload.Thread.ID
	}
	return ServerRequest{
		RequestID: msg.ID,
		Method:    msg.Method,
		ThreadID:  threadID,
		TurnID:    payload.TurnID,
		Prompt:    payload.Prompt,
		RawParams: params,
	}, true, nil
}

func DecodeResolvedServerRequest(msg rpcMessage) (string, bool, error) {
	if msg.Method != "serverRequest/resolved" {
		return "", false, nil
	}
	params, ok := messageParams(msg)
	if !ok {
		return "", false, nil
	}

	var payload struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", false, err
	}
	if payload.RequestID == "" {
		return "", false, nil
	}
	return payload.RequestID, true, nil
}
