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
	events   chan ThreadEvent
	requests map[string]struct{}
}

func newThreadWatcher(threadID string) *ThreadWatcher {
	return &ThreadWatcher{
		threadID: threadID,
		snapshot: ThreadSnapshot{
			ThreadID:          threadID,
			SubscriptionState: SubscriptionStateConnecting,
		},
		events:   make(chan ThreadEvent, 64),
		requests: make(map[string]struct{}),
	}
}

func (w *ThreadWatcher) Snapshot() ThreadSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshot
}

func (w *ThreadWatcher) Events() <-chan ThreadEvent {
	return w.events
}

func (w *ThreadWatcher) markConnecting() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.snapshot.SubscriptionState = SubscriptionStateConnecting
	w.snapshot.LastSubscriptionError = ""
}

func (w *ThreadWatcher) markError(message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.snapshot.SubscriptionState = SubscriptionStateError
	w.snapshot.LastSubscriptionError = message
	w.snapshot.LastActivityAt = time.Now().UTC()
}

func (w *ThreadWatcher) apply(msg rpcMessage) {
	params, ok := messageParams(msg)
	if !ok {
		return
	}

	if req, matched, err := DecodeServerRequest(msg); err == nil && matched && req.ThreadID == w.threadID {
		w.mu.Lock()
		w.requests[req.RequestID] = struct{}{}
		w.mu.Unlock()
		w.events <- ThreadEvent{
			Message:       msg,
			ServerRequest: &req,
		}
	}
	if requestID, matched, err := DecodeResolvedServerRequest(msg); err == nil && matched {
		w.mu.Lock()
		_, ok := w.requests[requestID]
		if ok {
			delete(w.requests, requestID)
		}
		w.mu.Unlock()
		if !ok {
			return
		}
		w.events <- ThreadEvent{
			Message:           msg,
			ResolvedRequestID: requestID,
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.accepts(msg.Method, params) {
		return
	}

	w.snapshot.LastActivityAt = time.Now().UTC()
	w.snapshot.SubscriptionState = SubscriptionStateActive
	w.snapshot.LastSubscriptionError = ""

	switch msg.Method {
	case "thread/started", "thread/completed", "thread/status/changed", "thread/closed":
		var payload struct {
			Thread struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return
		}
		if payload.Thread.ID != "" {
			w.snapshot.ThreadID = payload.Thread.ID
		}
		w.snapshot.ThreadStatus = payload.Thread.Status
	case "turn/started", "turn/completed":
		var payload struct {
			Turn struct {
				ID       string `json:"id"`
				Status   string `json:"status"`
				ThreadID string `json:"thread_id"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return
		}
		w.snapshot.ActiveTurnID = payload.Turn.ID
		w.snapshot.ActiveTurnStatus = payload.Turn.Status
	case "item/started", "item/completed", "item/agentMessage/delta":
		var payload struct {
			Item struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Text     string `json:"text"`
				ThreadID string `json:"thread_id"`
			} `json:"item"`
			Delta struct {
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return
		}
		w.snapshot.LastItemID = payload.Item.ID
		text := strings.TrimSpace(payload.Item.Text)
		if msg.Method == "item/agentMessage/delta" {
			text = strings.TrimSpace(payload.Delta.Text)
			w.snapshot.LatestAgentMessage = strings.TrimSpace(w.snapshot.LatestAgentMessage + text)
			w.snapshot.LatestSummary = w.snapshot.LatestAgentMessage
			return
		}
		switch payload.Item.Type {
		case "agent_message":
			w.snapshot.LatestAgentMessage = text
			w.snapshot.LatestSummary = text
		case "plan":
			w.snapshot.LatestPlan = text
		case "command":
			w.snapshot.LatestCommand = text
		}
	}
}

func (w *ThreadWatcher) accepts(method string, params json.RawMessage) bool {
	switch method {
	case "thread/started", "thread/completed", "thread/status/changed", "thread/closed":
		var payload struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return false
		}
		return payload.Thread.ID == w.threadID
	case "turn/started", "turn/completed":
		var payload struct {
			Turn struct {
				ThreadID string `json:"thread_id"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return false
		}
		return payload.Turn.ThreadID == w.threadID
	case "item/started", "item/completed", "item/agentMessage/delta":
		var payload struct {
			Item struct {
				ThreadID string `json:"thread_id"`
			} `json:"item"`
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return false
		}
		if payload.Item.ThreadID != "" {
			return payload.Item.ThreadID == w.threadID
		}
		return payload.Thread.ID == w.threadID
	default:
		return false
	}
}

func messageParams(msg rpcMessage) (json.RawMessage, bool) {
	switch params := msg.Params.(type) {
	case json.RawMessage:
		return params, true
	case []byte:
		return json.RawMessage(params), true
	case string:
		return json.RawMessage(params), true
	default:
		if params == nil {
			return nil, false
		}
		payload, err := json.Marshal(params)
		if err != nil {
			return nil, false
		}
		return json.RawMessage(payload), true
	}
}
