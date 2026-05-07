# Real Agent Handler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the stub Lark message handler with a hybrid command-plus-LLM handler that supports basic operator commands and normal text chat with bounded in-memory context.

**Architecture:** Keep the existing `channel.Handler` boundary intact and build the real handler entirely inside `internal/agent`. Add a small OpenAI config parser, an in-memory session store, a command handler, a chat handler backed by the Responses API, and a router that dispatches between them.

**Tech Stack:** Go 1.22+, standard library `net/http`, standard library tests, existing Lark adapter

---

## File Structure

- Create: `internal/agent/config.go`
  - Parses OpenAI environment variables.
- Create: `internal/agent/session.go`
  - Stores bounded in-memory conversation history.
- Create: `internal/agent/session_test.go`
  - Tests append, truncation, count, and reset behavior.
- Create: `internal/agent/command.go`
  - Implements `/help`, `/status`, `/reset`, and unknown command handling.
- Create: `internal/agent/command_test.go`
  - Tests command outputs and reset behavior.
- Create: `internal/agent/chat.go`
  - Implements OpenAI-backed chat handling through an abstract client.
- Create: `internal/agent/chat_test.go`
  - Tests chat request composition, missing config behavior, and response handling with a fake client.
- Create: `internal/agent/router.go`
  - Implements the hybrid `channel.Handler`.
- Create: `internal/agent/router_test.go`
  - Tests command routing vs normal message routing.
- Modify: `cmd/alterego/main.go`
  - Wires the real handler into the process.
- Modify: `README.md`
  - Documents the OpenAI configuration needed for real chat mode.

---

### Task 1: Add OpenAI Config Parsing

**Files:**
- Create: `internal/agent/config.go`

- [ ] **Step 1: Write the failing config test**

Create `internal/agent/config_test.go`:

```go
package agent

import "testing"

func TestConfigFromMapParsesDefaults(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{})
	if cfg.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Model != "" {
		t.Fatalf("Model = %q, want empty", cfg.Model)
	}
}

func TestConfigFromMapParsesValues(t *testing.T) {
	cfg := ConfigFromMap(map[string]string{
		"ALTER_EGO_OPENAI_API_KEY":   "sk-test",
		"ALTER_EGO_OPENAI_BASE_URL":  "https://example.com/v1",
		"ALTER_EGO_OPENAI_MODEL":     "gpt-test",
	})
	if cfg.APIKey != "sk-test" || cfg.BaseURL != "https://example.com/v1" || cfg.Model != "gpt-test" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: FAIL because `ConfigFromMap` and `Config` do not exist.

- [ ] **Step 3: Implement config parsing**

Create `internal/agent/config.go` with `Config`, `ConfigFromEnv`, and `ConfigFromMap`.

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: PASS

---

### Task 2: Add In-Memory Session Store

**Files:**
- Create: `internal/agent/session.go`
- Create: `internal/agent/session_test.go`

- [ ] **Step 1: Write failing session tests**

Add tests for:

- `AppendTurn` stores user and assistant messages;
- `Snapshot` returns a copy;
- `Reset` clears one conversation;
- history truncates to the configured maximum.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: FAIL because `SessionStore` and related methods do not exist.

- [ ] **Step 3: Implement session store**

Create `internal/agent/session.go` with:

- `SessionMessage`
- `SessionStore`
- `NewSessionStore(maxMessages int)`
- `Snapshot(key string) []SessionMessage`
- `AppendTurn(key, userText, assistantText string) int`
- `Reset(key string)`
- `Count(key string) int`

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: PASS

---

### Task 3: Add Command Handler

**Files:**
- Create: `internal/agent/command.go`
- Create: `internal/agent/command_test.go`

- [ ] **Step 1: Write failing command tests**

Add tests for:

- `/help` includes `status` and `reset`;
- `/status` reports model configuration and history count;
- `/reset` clears the current session;
- unknown commands point to `/help`.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: FAIL because `CommandHandler` does not exist.

- [ ] **Step 3: Implement command handler**

Create `internal/agent/command.go` with:

- `CommandHandler`
- `NewCommandHandler`
- `HandleCommand`

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: PASS

---

### Task 4: Add Chat Handler with Fake Client TDD

**Files:**
- Create: `internal/agent/chat.go`
- Create: `internal/agent/chat_test.go`

- [ ] **Step 1: Write failing chat tests**

Add tests for:

- missing API key or model returns `LLM is not configured`;
- request includes system prompt, prior history, and current user message;
- successful response appends a turn to the session store;
- empty response text returns a graceful failure message.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: FAIL because `ChatHandler` and `ChatClient` do not exist.

- [ ] **Step 3: Implement chat handler**

Create `internal/agent/chat.go` with:

- `ChatClient` interface
- `OpenAIClient`
- `ChatHandler`
- `NewChatHandler`
- request/response structs for the Responses API

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: PASS

---

### Task 5: Add Router and Replace Stub Wiring

**Files:**
- Create: `internal/agent/router.go`
- Create: `internal/agent/router_test.go`
- Modify: `cmd/alterego/main.go`

- [ ] **Step 1: Write failing router tests**

Add tests for:

- `/help` routes to command handling;
- normal text routes to chat handling;
- returned conversation target remains unchanged.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: FAIL because `Router` does not exist.

- [ ] **Step 3: Implement router and main wiring**

Create `internal/agent/router.go` and update `cmd/alterego/main.go` to build:

- agent config
- session store
- command handler
- chat handler
- router

- [ ] **Step 4: Run package tests to verify it passes**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: PASS

---

### Task 6: Document and Verify

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Document OpenAI environment variables**

Add a short section covering:

- `ALTER_EGO_OPENAI_API_KEY`
- `ALTER_EGO_OPENAI_BASE_URL`
- `ALTER_EGO_OPENAI_MODEL`
- commands `/help`, `/status`, `/reset`

- [ ] **Step 2: Run full verification**

Run:

```bash
go test -count=1 ./...
go build ./cmd/alterego
```

Expected: PASS
