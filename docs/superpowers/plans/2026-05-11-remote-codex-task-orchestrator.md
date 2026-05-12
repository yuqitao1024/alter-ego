# Remote Codex Task Orchestrator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current non-interactive remote Codex control path with a `SSH + tmux + codex` interactive runner that preserves multi-turn requirement discussion and long-lived remote task sessions.

**Architecture:** Keep Lark as the command gateway and SQLite as the persistence layer. Preserve the existing orchestrator shape, but replace the `exec/resume` runner model with a `tmux`-backed interactive session model. Task startup remains deterministic code; ongoing progress is driven by `tmux capture-pane`, `tmux send-keys`, and `tmux has-session`.

**Tech Stack:** Go 1.23+, standard library, `database/sql` with `modernc.org/sqlite`, existing Lark adapter, SSH transport layer, remote `tmux`, remote `codex`

---

## File Structure

- Modify: `internal/orchestrator/types.go`
  - Replace exec/resume-oriented remote session fields with TTY-oriented session metadata, including terminal responder convergence fields.
- Modify: `internal/orchestrator/store.go`
  - Persist `tmux_session_name`, screen-tracking fields, and responder cooldown metadata.
- Modify: `internal/orchestrator/store_test.go`
  - Update persisted field coverage for the new task model.
- Modify: `internal/orchestrator/runner.go`
  - Replace probe/attach/resume contracts with interactive session contracts.
- Modify: `internal/orchestrator/runner_test.go`
  - Replace recovery-order tests with `tmux` session existence and reconnect tests.
- Modify: `internal/orchestrator/runner_ssh.go`
  - Concrete `tmux`-backed interactive runner implementation, including `machine.shell_init` injection.
- Modify: `internal/orchestrator/runner_ssh_test.go`
  - Tests for startup command shape, `capture-pane`, `send-keys`, `has-session`, and `machine.shell_init`.
- Modify: `internal/orchestrator/decision.go`
  - Expand escalation categories beyond implementation-solution choice.
- Modify: `internal/orchestrator/decision_test.go`
  - Add tests for clarification and scope confirmation detection.
- Modify: `internal/orchestrator/service.go`
  - Replace exec-oriented tick behavior with interactive capture/send behavior.
- Modify: `internal/orchestrator/service_test.go`
  - Update lifecycle tests for `preparing_workspace`, `starting_session`, `waiting_user_input`, and detach/reconnect.
- Modify: `internal/orchestrator/config.go`
  - Add `machine.shell_init` to remote machine configuration.
- Modify: `internal/orchestrator/config_test.go`
  - Verify `machine.shell_init` is loaded and bound into the registry.
- Modify: `README.md`
  - Document remote `tmux` requirement and the new interaction model.

---

### Task 1: Update Persisted Task Model for TTY Sessions

**Files:**
- Modify: `internal/orchestrator/types.go`
- Modify: `internal/orchestrator/store.go`
- Modify: `internal/orchestrator/store_test.go`

- [ ] **Step 1: Write failing store tests for `tmux` session fields**

Add tests covering persisted fields such as:

```go
func TestStorePersistsTMUXSessionFields(t *testing.T)
func TestStoreReloadsTaskWithTTYMetadata(t *testing.T)
```

Assert at least:

- `tmux_session_name`
- `remote_workdir`
- optional `remote_codex_session_id`
- `last_screen_digest`

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because the store schema and task type do not yet match the TTY session model.

- [ ] **Step 3: Implement the minimal persisted field changes**

Update:

- `TaskRun`
- SQLite schema
- scan / marshal helpers

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

---

### Task 2: Replace Runner Contracts with Interactive Session Contracts

**Files:**
- Modify: `internal/orchestrator/runner.go`
- Modify: `internal/orchestrator/runner_test.go`

- [ ] **Step 1: Write failing runner contract tests**

Add tests for:

```go
func TestReconnectUsesTMUXSessionWhenPresent(t *testing.T)
func TestReconnectFailsWhenTMUXSessionMissing(t *testing.T)
```

Use a fake runner that records calls such as:

- `StartInteractiveSession`
- `CaptureOutput`
- `SendInteractiveInput`
- `HasSession`
- `StopSession`

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because the old exec/resume-oriented contracts still exist.

- [ ] **Step 3: Implement the new contracts**

Define value types and interfaces such as:

```go
type RemoteRunner interface {
    StartInteractiveSession(ctx context.Context, req StartRequest) (RemoteSession, error)
    CaptureOutput(ctx context.Context, session RemoteSession) (OutputWindow, error)
    SendInteractiveInput(ctx context.Context, session RemoteSession, input string) error
    HasSession(ctx context.Context, session RemoteSession) (bool, error)
    StopSession(ctx context.Context, session RemoteSession) error
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

---

### Task 3: Implement `tmux` Runner

**Files:**
- Modify: `internal/orchestrator/runner_ssh.go`
- Modify: `internal/orchestrator/runner_ssh_test.go`
- Modify: `internal/orchestrator/config.go`
- Modify: `internal/orchestrator/config_test.go`

- [ ] **Step 1: Write failing `tmux` command-shape tests**

Cover at least:

```go
func TestTMUXRunnerStartCreatesSessionAndLaunchesCodex(t *testing.T)
func TestTMUXRunnerCaptureUsesCapturePane(t *testing.T)
func TestTMUXRunnerSendInputUsesSendKeys(t *testing.T)
func TestTMUXRunnerHasSessionUsesTMUXHasSession(t *testing.T)
func TestTMUXRunnerStopUsesKillSession(t *testing.T)
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because the concrete `tmux` runner does not exist.

- [ ] **Step 3: Implement the runner**

Create command builders for:

- workspace preparation
- `tmux new-session`
- `tmux capture-pane`
- `tmux send-keys`
- `tmux has-session`
- `tmux kill-session`
- `machine.shell_init` injection for every SSH command
- `machine.shell_init` injection inside the `tmux` command that launches `codex`

Ensure `codex` is launched with:

```text
--dangerously-bypass-approvals-and-sandbox
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

---

### Task 4: Add Model Arbitration for Waiting Codex Sessions

**Files:**
- Modify: `internal/orchestrator/decision.go`
- Modify: `internal/orchestrator/decision_test.go`

- [ ] **Step 1: Write failing arbitration tests**

Add tests for:

```go
func TestArbitratorSkipsModelWhenCodexIsStillWorking(t *testing.T)
func TestArbitratorAsksUserWhenModelReturnsAskUser(t *testing.T)
func TestArbitratorRepliesToCodexWhenModelReturnsReplyToCodex(t *testing.T)
func TestArbitratorMarksTaskCompletedWhenModelReturnsCompleteTask(t *testing.T)
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because the decision path still relies on narrow heuristic category matching.

- [ ] **Step 3: Implement the arbitration changes**

Update:

- arbitration result types
- prompt builder and JSON contract
- waiting-vs-working gate helpers
- model-backed arbitrator using the configured LLM provider
- completion signaling when Codex has finished the requested workflow and is only awaiting further operator input

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

---

### Task 5: Rework Service Lifecycle for Interactive Sessions

**Files:**
- Modify: `internal/orchestrator/service.go`
- Modify: `internal/orchestrator/service_test.go`

- [ ] **Step 1: Write failing lifecycle tests**

Add or replace tests for:

```go
func TestTickPreparesWorkspaceAndStartsTMUXSession(t *testing.T)
func TestTickMovesTaskToWaitingUserInputWhenClarificationDetected(t *testing.T)
func TestReplySendsInputIntoLiveSession(t *testing.T)
func TestDetachedTaskReconnectsWhenTMUXSessionExists(t *testing.T)
func TestDetachedTaskFailsWhenTMUXSessionIsGone(t *testing.T)
func TestStopKillsTMUXSessionAndMarksStopped(t *testing.T)
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because the service still assumes exec/resume-style behavior.

- [ ] **Step 3: Implement the lifecycle changes**

Update the service so that:

- `pending` tasks move through `preparing_workspace` and `starting_session`
- `running` tasks are advanced by `CaptureOutput`
- `waiting_user_input` is used for any supported question category
- `/task reply` uses `SendInteractiveInput`
- `detached` recovery uses `HasSession`
- terminal responder escalations persist responder name and screen digest
- `/task reply` records the resolved responder and starts a short cooldown window
- repeated `capture-pane` output for the same responder/screen digest is ignored during cooldown so stale screens do not immediately re-open the same question
- non-responder prompts only invoke model arbitration when Codex appears to be waiting for input
- model arbitration may either ask the user, reply directly to Codex, or mark the task completed

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

---

### Task 6: Update Operator-Facing Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/manual-testing/remote-codex-task-testing.md`

- [ ] **Step 1: Update documentation**

Document:

- remote `tmux` requirement
- task lifecycle with interactive sessions
- `/task reply` semantics for clarification and scope questions
- updated live-testing procedure

- [ ] **Step 2: Run full verification**

Run:

```bash
go test -count=1 ./...
go build ./cmd/alterego
```

Expected: PASS

---

## Self-Review

- Spec coverage:
  - `tmux`-backed interactive runner: covered in Tasks 2 and 3
  - task-scoped checkout and workspace prep: covered in Task 3
  - expanded clarification categories: covered in Task 4
  - service lifecycle changes for interactive sessions: covered in Task 5
  - operator-facing documentation: covered in Task 6
- Placeholder scan:
  - no `TODO` / `TBD` placeholders
  - each task has concrete file ownership and verification commands
- Type consistency:
  - runner model consistently uses `StartInteractiveSession`, `CaptureOutput`, `SendInteractiveInput`, `HasSession`, and `StopSession`
