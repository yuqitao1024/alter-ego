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
	ThreadActiveFlags     []string
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

func (w *ThreadWatcher) hydrate(thread Thread) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if strings.TrimSpace(thread.ID) != "" {
		w.snapshot.ThreadID = thread.ID
	}
	if status, flags := decodeStatusDetails(thread.Status); status != "" || len(flags) > 0 {
		w.snapshot.ThreadStatus = status
		w.snapshot.ThreadActiveFlags = append([]string(nil), flags...)
	}
	for _, turn := range thread.Turns {
		if strings.TrimSpace(turn.ID) != "" {
			w.snapshot.ActiveTurnID = turn.ID
		}
		if status := decodeStatus(turn.Status); status != "" {
			w.snapshot.ActiveTurnStatus = status
		}
		for _, item := range turn.Items {
			if strings.TrimSpace(item.ID) != "" {
				w.snapshot.LastItemID = item.ID
			}
			switch item.Type {
			case "agent_message", "agentMessage":
				text := strings.TrimSpace(item.Text)
				if text != "" {
					w.snapshot.LatestAgentMessage = text
					w.snapshot.LatestSummary = text
				}
			case "plan":
				if text := strings.TrimSpace(item.Text); text != "" {
					w.snapshot.LatestPlan = text
				}
			case "command", "commandExecution":
				if command := strings.TrimSpace(firstNonEmpty(item.Command, item.Text)); command != "" {
					w.snapshot.LatestCommand = command
				}
			}
		}
	}
	w.snapshot.LastActivityAt = time.Now().UTC()
	w.snapshot.SubscriptionState = SubscriptionStateActive
	w.snapshot.LastSubscriptionError = ""
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
		threadID, status, flags, ok := decodeThreadNotification(params)
		if !ok {
			return
		}
		if threadID != "" {
			w.snapshot.ThreadID = threadID
		}
		if status != "" {
			w.snapshot.ThreadStatus = status
		}
		w.snapshot.ThreadActiveFlags = append([]string(nil), flags...)
	case "turn/started", "turn/completed":
		turnID, status, ok := decodeTurnNotification(params)
		if !ok {
			return
		}
		if turnID != "" {
			w.snapshot.ActiveTurnID = turnID
		}
		if status != "" {
			w.snapshot.ActiveTurnStatus = status
		}
	case "item/started", "item/completed":
		item, ok := decodeItemNotification(params)
		if !ok {
			return
		}
		w.snapshot.LastItemID = item.ID
		switch item.Type {
		case "agent_message", "agentMessage":
			text := strings.TrimSpace(item.Text)
			w.snapshot.LatestAgentMessage = text
			w.snapshot.LatestSummary = text
		case "plan":
			w.snapshot.LatestPlan = strings.TrimSpace(item.Text)
		case "command", "commandExecution":
			w.snapshot.LatestCommand = strings.TrimSpace(firstNonEmpty(item.Command, item.Text))
		}
	case "item/agentMessage/delta", "item/reasoning/summaryTextDelta":
		itemID, turnID, text, ok := decodeTextDelta(params)
		if !ok {
			return
		}
		w.snapshot.LastItemID = itemID
		if turnID != "" {
			w.snapshot.ActiveTurnID = turnID
		}
		w.snapshot.LatestAgentMessage = strings.TrimSpace(w.snapshot.LatestAgentMessage + text)
		w.snapshot.LatestSummary = w.snapshot.LatestAgentMessage
	case "item/plan/delta":
		itemID, turnID, text, ok := decodeTextDelta(params)
		if !ok {
			return
		}
		w.snapshot.LastItemID = itemID
		if turnID != "" {
			w.snapshot.ActiveTurnID = turnID
		}
		w.snapshot.LatestPlan = strings.TrimSpace(w.snapshot.LatestPlan + text)
		if w.snapshot.LatestSummary == "" {
			w.snapshot.LatestSummary = w.snapshot.LatestPlan
		}
	case "item/commandExecution/outputDelta":
		itemID, turnID, text, ok := decodeTextDelta(params)
		if !ok {
			return
		}
		w.snapshot.LastItemID = itemID
		if turnID != "" {
			w.snapshot.ActiveTurnID = turnID
		}
		w.snapshot.LatestCommand = strings.TrimSpace(w.snapshot.LatestCommand + text)
		w.snapshot.LatestSummary = w.snapshot.LatestCommand
	case "turn/plan/updated":
		turnID, text, ok := decodeTurnPlanUpdated(params)
		if !ok {
			return
		}
		if turnID != "" {
			w.snapshot.ActiveTurnID = turnID
		}
		w.snapshot.LatestPlan = text
		if w.snapshot.LatestSummary == "" {
			w.snapshot.LatestSummary = text
		}
	}
}

func (w *ThreadWatcher) accepts(method string, params json.RawMessage) bool {
	switch method {
	case "thread/started", "thread/completed", "thread/status/changed", "thread/closed":
		threadID, _, _, ok := decodeThreadNotification(params)
		return ok && threadID == w.threadID
	case "turn/started", "turn/completed":
		threadID, _, _, ok := decodeTurnScope(params)
		return ok && threadID == w.threadID
	case "item/started", "item/completed", "item/agentMessage/delta", "item/reasoning/summaryTextDelta", "item/plan/delta", "item/commandExecution/outputDelta":
		threadID, _, ok := decodeItemScope(params)
		return ok && threadID == w.threadID
	case "turn/plan/updated":
		threadID, _, ok := decodeTurnPlanScope(params)
		return ok && threadID == w.threadID
	default:
		return false
	}
}

type threadNotification struct {
	Thread struct {
		ID     string          `json:"id"`
		Status json.RawMessage `json:"status"`
	} `json:"thread"`
	ThreadID string          `json:"threadId"`
	Status   json.RawMessage `json:"status"`
}

type turnNotification struct {
	ThreadID string `json:"threadId"`
	Turn     struct {
		ID     string          `json:"id"`
		Status json.RawMessage `json:"status"`
	} `json:"turn"`
}

type itemNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Item     struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Text    string `json:"text"`
		Command string `json:"command"`
	} `json:"item"`
	ItemID string `json:"itemId"`
	Delta  string `json:"delta"`
}

type turnPlanNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	Plan     string `json:"plan"`
}

type statusEnvelope struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags"`
}

func decodeThreadNotification(params json.RawMessage) (string, string, []string, bool) {
	var payload threadNotification
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", "", nil, false
	}
	threadID := firstNonEmpty(payload.Thread.ID, payload.ThreadID)
	status, flags := decodeStatusDetails(payload.Thread.Status)
	if status == "" {
		status, flags = decodeStatusDetails(payload.Status)
	}
	return threadID, status, flags, threadID != "" || status != ""
}

func decodeTurnNotification(params json.RawMessage) (string, string, bool) {
	var payload turnNotification
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", "", false
	}
	return payload.Turn.ID, decodeStatus(payload.Turn.Status), payload.Turn.ID != ""
}

func decodeTurnScope(params json.RawMessage) (string, string, string, bool) {
	var payload turnNotification
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", "", "", false
	}
	return payload.ThreadID, payload.Turn.ID, decodeStatus(payload.Turn.Status), payload.ThreadID != ""
}

func decodeItemNotification(params json.RawMessage) (struct {
	ID      string
	Type    string
	Text    string
	Command string
}, bool) {
	var payload itemNotification
	if err := json.Unmarshal(params, &payload); err != nil {
		return struct {
			ID      string
			Type    string
			Text    string
			Command string
		}{}, false
	}
	return struct {
		ID      string
		Type    string
		Text    string
		Command string
	}{
		ID:      payload.Item.ID,
		Type:    payload.Item.Type,
		Text:    payload.Item.Text,
		Command: payload.Item.Command,
	}, payload.Item.ID != ""
}

func decodeAgentMessageDelta(params json.RawMessage) (string, string, bool) {
	itemID, _, text, ok := decodeTextDelta(params)
	return itemID, text, ok
}

func decodeTextDelta(params json.RawMessage) (string, string, string, bool) {
	var payload itemNotification
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", "", "", false
	}
	return payload.ItemID, payload.TurnID, payload.Delta, payload.ItemID != ""
}

func decodeItemScope(params json.RawMessage) (string, string, bool) {
	var payload itemNotification
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", "", false
	}
	return payload.ThreadID, firstNonEmpty(payload.Item.ID, payload.ItemID), payload.ThreadID != ""
}

func decodeTurnPlanUpdated(params json.RawMessage) (string, string, bool) {
	var payload turnPlanNotification
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", "", false
	}
	text := strings.TrimSpace(payload.Plan)
	return payload.TurnID, text, text != ""
}

func decodeTurnPlanScope(params json.RawMessage) (string, string, bool) {
	var payload turnPlanNotification
	if err := json.Unmarshal(params, &payload); err != nil {
		return "", "", false
	}
	return payload.ThreadID, payload.TurnID, payload.ThreadID != ""
}

func decodeStatus(raw json.RawMessage) string {
	status, _ := decodeStatusDetails(raw)
	return status
}

func decodeStatusDetails(raw json.RawMessage) (string, []string) {
	if len(raw) == 0 {
		return "", nil
	}

	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return strings.TrimSpace(plain), nil
	}

	var status statusEnvelope
	if err := json.Unmarshal(raw, &status); err == nil {
		return strings.TrimSpace(status.Type), append([]string(nil), status.ActiveFlags...)
	}
	return "", nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
