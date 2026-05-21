package codexappserver

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeServerRequest(t *testing.T) {
	t.Parallel()

	msg := rpcMessage{
		ID:     "srv-1",
		Method: "item/tool/requestUserInput",
		Params: map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"prompt":   "Choose A or B",
		},
	}

	req, ok, err := DecodeServerRequest(msg)
	if err != nil {
		t.Fatalf("DecodeServerRequest returned error: %v", err)
	}
	if !ok {
		t.Fatal("DecodeServerRequest returned ok=false")
	}
	if req.RequestID != "srv-1" || req.ThreadID != "thread-1" || req.TurnID != "turn-1" {
		t.Fatalf("req = %#v", req)
	}
}

func TestThreadWatcherPublishesServerRequestAndResolvedEvent(t *testing.T) {
	t.Parallel()

	watcher := newThreadWatcher("thread-1")
	watcher.apply(rpcMessage{
		ID:     "srv-1",
		Method: "item/tool/requestUserInput",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"prompt":   "Choose A or B",
		}),
	})

	select {
	case event := <-watcher.Events():
		if event.ServerRequest == nil || event.ServerRequest.RequestID != "srv-1" {
			t.Fatalf("event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("server request event was not published")
	}

	watcher.apply(rpcMessage{
		Method: "serverRequest/resolved",
		Params: mustRawJSON(t, map[string]any{
			"requestId": "srv-1",
		}),
	})

	select {
	case event := <-watcher.Events():
		if event.ResolvedRequestID != "srv-1" {
			t.Fatalf("ResolvedRequestID = %q, want srv-1", event.ResolvedRequestID)
		}
	case <-time.After(time.Second):
		t.Fatal("resolved event was not published")
	}
}

func TestThreadWatcherPublishesGenericApprovalRequestEvent(t *testing.T) {
	t.Parallel()

	watcher := newThreadWatcher("thread-1")
	watcher.apply(rpcMessage{
		ID:     "srv-2",
		Method: "turn/network/requestApproval",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"prompt":   "Allow network access to GitCode?",
		}),
	})

	select {
	case event := <-watcher.Events():
		if event.ServerRequest == nil {
			t.Fatal("ServerRequest = nil")
		}
		if event.ServerRequest.Method != "turn/network/requestApproval" {
			t.Fatalf("Method = %q, want turn/network/requestApproval", event.ServerRequest.Method)
		}
		if event.ServerRequest.RequestID != "srv-2" {
			t.Fatalf("RequestID = %q, want srv-2", event.ServerRequest.RequestID)
		}
	case <-time.After(time.Second):
		t.Fatal("generic approval request event was not published")
	}
}

func TestThreadWatcherAppliesCurrentProtocolNotifications(t *testing.T) {
	t.Parallel()

	watcher := newThreadWatcher("thread-1")

	watcher.apply(rpcMessage{
		Method: "thread/started",
		Params: mustRawJSON(t, map[string]any{
			"thread": map[string]any{
				"id": "thread-1",
				"status": map[string]any{
					"type":        "active",
					"activeFlags": []string{"waitingOnUserInput"},
				},
			},
		}),
	})
	watcher.apply(rpcMessage{
		Method: "turn/started",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turn": map[string]any{
				"id":     "turn-1",
				"status": "inProgress",
			},
		}),
	})
	watcher.apply(rpcMessage{
		Method: "item/completed",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"item": map[string]any{
				"id":   "item-plan",
				"type": "plan",
				"text": "1. Inspect logs\n2. Fix parser",
			},
		}),
	})
	watcher.apply(rpcMessage{
		Method: "item/completed",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"item": map[string]any{
				"id":      "item-cmd",
				"type":    "commandExecution",
				"command": "go test ./...",
			},
		}),
	})
	watcher.apply(rpcMessage{
		Method: "item/completed",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"item": map[string]any{
				"id":   "item-msg",
				"type": "agentMessage",
				"text": "Implemented the websocket parser fix.",
			},
		}),
	})
	watcher.apply(rpcMessage{
		Method: "item/agentMessage/delta",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"itemId":   "item-msg",
			"delta":    " Running verification now.",
		}),
	})
	watcher.apply(rpcMessage{
		Method: "thread/status/changed",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"status": map[string]any{
				"type": "idle",
			},
		}),
	})

	snapshot := watcher.Snapshot()
	if snapshot.ThreadID != "thread-1" {
		t.Fatalf("ThreadID = %q, want thread-1", snapshot.ThreadID)
	}
	if snapshot.ThreadStatus != "idle" {
		t.Fatalf("ThreadStatus = %q, want idle", snapshot.ThreadStatus)
	}
	if snapshot.ActiveTurnID != "turn-1" {
		t.Fatalf("ActiveTurnID = %q, want turn-1", snapshot.ActiveTurnID)
	}
	if snapshot.ActiveTurnStatus != "inProgress" {
		t.Fatalf("ActiveTurnStatus = %q, want inProgress", snapshot.ActiveTurnStatus)
	}
	if snapshot.LastItemID != "item-msg" {
		t.Fatalf("LastItemID = %q, want item-msg", snapshot.LastItemID)
	}
	if snapshot.LatestPlan != "1. Inspect logs\n2. Fix parser" {
		t.Fatalf("LatestPlan = %q", snapshot.LatestPlan)
	}
	if snapshot.LatestCommand != "go test ./..." {
		t.Fatalf("LatestCommand = %q, want go test ./...", snapshot.LatestCommand)
	}
	if snapshot.LatestAgentMessage != "Implemented the websocket parser fix. Running verification now." {
		t.Fatalf("LatestAgentMessage = %q", snapshot.LatestAgentMessage)
	}
	if snapshot.LatestSummary != snapshot.LatestAgentMessage {
		t.Fatalf("LatestSummary = %q, want %q", snapshot.LatestSummary, snapshot.LatestAgentMessage)
	}
	if snapshot.SubscriptionState != SubscriptionStateActive {
		t.Fatalf("SubscriptionState = %q, want %q", snapshot.SubscriptionState, SubscriptionStateActive)
	}
}

func TestThreadWatcherAppliesStreamingProgressNotifications(t *testing.T) {
	t.Parallel()

	watcher := newThreadWatcher("thread-1")

	watcher.apply(rpcMessage{
		Method: "turn/plan/updated",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"plan":     "1. Read papers\n2. Write report",
		}),
	})
	watcher.apply(rpcMessage{
		Method: "item/reasoning/summaryTextDelta",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"itemId":   "reasoning-1",
			"delta":    "正在检索论文和 GitHub 资料。",
		}),
	})
	watcher.apply(rpcMessage{
		Method: "item/commandExecution/outputDelta",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"itemId":   "cmd-1",
			"delta":    "Downloaded 12 references.\n",
		}),
	})
	watcher.apply(rpcMessage{
		Method: "item/plan/delta",
		Params: mustRawJSON(t, map[string]any{
			"threadId": "thread-1",
			"turnId":   "turn-1",
			"itemId":   "plan-1",
			"delta":    "\n3. Generate PPT",
		}),
	})

	snapshot := watcher.Snapshot()
	if snapshot.ActiveTurnID != "turn-1" {
		t.Fatalf("ActiveTurnID = %q, want turn-1", snapshot.ActiveTurnID)
	}
	if snapshot.LatestPlan != "1. Read papers\n2. Write report\n3. Generate PPT" {
		t.Fatalf("LatestPlan = %q", snapshot.LatestPlan)
	}
	if snapshot.LatestAgentMessage != "正在检索论文和 GitHub 资料。" {
		t.Fatalf("LatestAgentMessage = %q", snapshot.LatestAgentMessage)
	}
	if snapshot.LatestCommand != "Downloaded 12 references." {
		t.Fatalf("LatestCommand = %q", snapshot.LatestCommand)
	}
	if snapshot.LatestSummary != "Downloaded 12 references." {
		t.Fatalf("LatestSummary = %q, want command output summary", snapshot.LatestSummary)
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	return json.RawMessage(data)
}
