# LLM Provider Abstraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a provider abstraction so the real agent handler can switch between OpenAI and GLM without changing command routing or session behavior.

**Architecture:** Generalize the current OpenAI-specific config into provider-neutral LLM config, introduce a provider factory, keep OpenAI Responses support, and add a GLM provider that uses Zhipu's OpenAI-compatible Chat Completions endpoint.

**Tech Stack:** Go 1.22+, standard library `net/http`, existing handler/session code, fake HTTP servers in tests

---

### Task 1: Generalize LLM Config with Backward Compatibility

**Files:**
- Modify: `internal/agent/config.go`
- Modify: `internal/agent/config_test.go`

- [ ] Write failing tests for:
  - `ALTER_EGO_LLM_*` parsing
  - default provider `openai`
  - GLM default base URL
  - fallback to legacy `ALTER_EGO_OPENAI_*`

- [ ] Run `go test -count=1 ./internal/agent` and verify failure

- [ ] Implement the config changes

- [ ] Run `go test -count=1 ./internal/agent` and verify pass

### Task 2: Introduce Provider Factory

**Files:**
- Create: `internal/agent/provider.go`
- Create: `internal/agent/provider_test.go`

- [ ] Write failing tests for:
  - provider factory returns OpenAI provider
  - provider factory returns GLM provider
  - unknown provider falls back to OpenAI-safe behavior

- [ ] Run `go test -count=1 ./internal/agent` and verify failure

- [ ] Implement provider interface and factory

- [ ] Run `go test -count=1 ./internal/agent` and verify pass

### Task 3: Split OpenAI and GLM Provider Implementations

**Files:**
- Modify: `internal/agent/chat.go`
- Modify: `internal/agent/chat_test.go`
- Create: `internal/agent/provider_openai.go`
- Create: `internal/agent/provider_glm.go`

- [ ] Write or update failing tests for:
  - OpenAI response parsing
  - GLM chat completions parsing
  - chat handler still reports `LLM is not configured` when config is incomplete

- [ ] Run `go test -count=1 ./internal/agent` and verify failure

- [ ] Implement provider-specific clients and wire chat handler to the factory

- [ ] Run `go test -count=1 ./internal/agent` and verify pass

### Task 4: Document and Verify

**Files:**
- Modify: `README.md`

- [ ] Document:
  - `ALTER_EGO_LLM_PROVIDER`
  - `ALTER_EGO_LLM_API_KEY`
  - `ALTER_EGO_LLM_BASE_URL`
  - `ALTER_EGO_LLM_MODEL`
  - note that GLM uses the coding endpoint for Coding Plan setups

- [ ] Run:

```bash
go test -count=1 ./...
go build ./cmd/alterego
```

- [ ] Verify both commands pass
