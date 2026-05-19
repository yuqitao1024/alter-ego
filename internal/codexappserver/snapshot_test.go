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
	watcher.apply(notificationEnvelope("item/started", `{"item":{"id":"item-1","type":"agent_message","text":"Planning","thread_id":"thread-1"}}`))
	watcher.apply(notificationEnvelope("item/completed", `{"item":{"id":"item-1","type":"agent_message","text":"Planning complete","thread_id":"thread-1"}}`))
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

func TestThreadWatcherIgnoresItemsWithoutMatchingThreadID(t *testing.T) {
	t.Parallel()

	watcher := newThreadWatcher("thread-1")
	watcher.apply(notificationEnvelope("item/completed", `{"item":{"id":"item-1","type":"agent_message","text":"wrong route"}}`))

	snapshot := watcher.Snapshot()
	if snapshot.LatestAgentMessage != "" {
		t.Fatalf("snapshot.LatestAgentMessage = %q, want empty", snapshot.LatestAgentMessage)
	}
	if snapshot.SubscriptionState != SubscriptionStateConnecting {
		t.Fatalf("snapshot.SubscriptionState = %q, want %q", snapshot.SubscriptionState, SubscriptionStateConnecting)
	}
}

func notificationEnvelope(method string, payload string) rpcMessage {
	var params map[string]any
	if err := json.Unmarshal([]byte(payload), &params); err != nil {
		panic(err)
	}

	return rpcMessage{
		Method: method,
		Params: params,
	}
}
