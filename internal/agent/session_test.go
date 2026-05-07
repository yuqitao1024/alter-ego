package agent

import "testing"

func TestSessionStoreAppendTurnAndCount(t *testing.T) {
	store := NewSessionStore(4)

	count := store.AppendTurn("lark:oc_1", "hello", "world")
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	messages := store.Snapshot("lark:oc_1")
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hello" {
		t.Fatalf("user message = %#v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "world" {
		t.Fatalf("assistant message = %#v", messages[1])
	}
	if store.Count("lark:oc_1") != 2 {
		t.Fatalf("Count = %d, want 2", store.Count("lark:oc_1"))
	}
}

func TestSessionStoreSnapshotReturnsCopy(t *testing.T) {
	store := NewSessionStore(4)
	store.AppendTurn("lark:oc_1", "hello", "world")

	messages := store.Snapshot("lark:oc_1")
	messages[0].Content = "mutated"

	current := store.Snapshot("lark:oc_1")
	if current[0].Content != "hello" {
		t.Fatalf("current[0].Content = %q, want hello", current[0].Content)
	}
}

func TestSessionStoreResetClearsOneConversation(t *testing.T) {
	store := NewSessionStore(4)
	store.AppendTurn("lark:oc_1", "hello", "world")
	store.AppendTurn("lark:oc_2", "foo", "bar")

	store.Reset("lark:oc_1")

	if store.Count("lark:oc_1") != 0 {
		t.Fatalf("Count(oc_1) = %d, want 0", store.Count("lark:oc_1"))
	}
	if store.Count("lark:oc_2") != 2 {
		t.Fatalf("Count(oc_2) = %d, want 2", store.Count("lark:oc_2"))
	}
}

func TestSessionStoreTruncatesToConfiguredMaximum(t *testing.T) {
	store := NewSessionStore(4)
	store.AppendTurn("lark:oc_1", "u1", "a1")
	store.AppendTurn("lark:oc_1", "u2", "a2")
	store.AppendTurn("lark:oc_1", "u3", "a3")

	messages := store.Snapshot("lark:oc_1")
	if len(messages) != 4 {
		t.Fatalf("len(messages) = %d, want 4", len(messages))
	}
	if messages[0].Content != "u2" || messages[1].Content != "a2" || messages[2].Content != "u3" || messages[3].Content != "a3" {
		t.Fatalf("unexpected messages after truncation: %#v", messages)
	}
}
