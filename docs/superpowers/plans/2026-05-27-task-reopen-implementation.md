# Task Reopen Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `/task reopen <task-id> <extra requirement>` so a `stopped` or `completed` task can continue on the same task ID and Codex thread with a new follow-up requirement.

**Architecture:** Extend the task command layer with a required-text `reopen` command and add a service-layer `Reopen` method that validates status, clears stale waiting state, optionally resolves persisted pending requests, and injects the new requirement into the existing app-server thread as a fresh turn. Keep `completed` and `stopped` as terminal states until an explicit operator reopen occurs.

**Tech Stack:** Go, SQLite store, Codex app-server runner, Lark task command handler

---

### Task 1: Define reopen behavior in tests

**Files:**
- Modify: `internal/orchestrator/service_test.go`
- Modify: `internal/agent/task_command_test.go`

- [ ] Add failing service tests for reopening a terminal task on the same thread and for rejecting invalid reopen requests.
- [ ] Run targeted tests and confirm they fail for the missing `Reopen` behavior.

### Task 2: Implement reopen command and service flow

**Files:**
- Modify: `internal/agent/task_command.go`
- Modify: `internal/orchestrator/service.go`

- [ ] Add `Reopen(ctx, taskID, extraRequirement string)` to the task service interface and wire `/task reopen <task-id> <extra requirement>` in the command handler.
- [ ] Implement service-level reopen validation:
  - only `stopped` and `completed`
  - non-empty extra requirement
  - same task ID and same thread
  - clear `AwaitingQuestion`
  - resolve persisted `PendingRequestID` if present
  - send new input through `SendInteractiveInput`
  - transition back to `running`
  - append `task_reopened`

### Task 3: Update docs and verify

**Files:**
- Modify: `README.md`

- [ ] Document the new `reopen` command and add the `completed/stopped -> running` reopen transition to the task lifecycle section.
- [ ] Run targeted tests, then `go test ./...`, and confirm all pass.
