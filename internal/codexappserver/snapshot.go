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
	params, ok := messageParams(msg)
	if !ok {
		return
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
	case "thread/started", "thread/completed":
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
	case "item/started", "item/completed":
		var payload struct {
			Item struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Text     string `json:"text"`
				ThreadID string `json:"thread_id"`
			} `json:"item"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return
		}
		w.snapshot.LastItemID = payload.Item.ID
		text := strings.TrimSpace(payload.Item.Text)
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
	case "thread/started", "thread/completed":
		var payload struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return false
		}
		return payload.Thread.ID == "" || payload.Thread.ID == w.threadID
	case "turn/started", "turn/completed":
		var payload struct {
			Turn struct {
				ThreadID string `json:"thread_id"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return false
		}
		return payload.Turn.ThreadID == "" || payload.Turn.ThreadID == w.threadID
	case "item/started", "item/completed":
		var payload struct {
			Item struct {
				ThreadID string `json:"thread_id"`
			} `json:"item"`
		}
		if err := json.Unmarshal(params, &payload); err != nil {
			return false
		}
		return payload.Item.ThreadID == "" || payload.Item.ThreadID == w.threadID
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
