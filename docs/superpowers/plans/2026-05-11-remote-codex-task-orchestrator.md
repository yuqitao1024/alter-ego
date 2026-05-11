# Remote Codex Task Orchestrator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a repository-scoped remote Codex task orchestrator that can start and manage multiple SSH-backed Codex tasks, persist task state in SQLite, recover remote sessions by Codex session ID, and escalate only implementation-solution-choice decisions through Lark.

**Architecture:** Keep Lark as the command gateway, add a deterministic orchestration layer under a new `internal/orchestrator` package, isolate remote SSH/Codex behavior behind a runner interface, and persist task and question state in SQLite. Templates bind to repositories and workflow documents; repositories bind to machine pools, remote repo URLs, task workspace roots, and pre/post clone bootstrap commands; the scheduler balances active tasks across two machines and advances them round-robin.

**Tech Stack:** Go 1.22+, standard library, `database/sql` with `modernc.org/sqlite`, existing Lark adapter, SSH transport library, remote `codex` CLI

---

## File Structure

- Create: `configs/machines/example.yaml`
  - Example machine configuration layout.
- Create: `configs/repositories/example.yaml`
  - Example repository configuration layout with `machine_ids`.
- Create: `configs/templates/example.yaml`
  - Example template configuration layout with `repository_id` and `workflow_path`.
- Create: `docs/workflows/example-feature-dev.md`
  - Example workflow document format.
- Create: `internal/orchestrator/config.go`
  - Loads machine, repository, and template configs from disk.
- Create: `internal/orchestrator/config_test.go`
  - Tests repository/template binding, machine pool loading, and invalid references.
- Create: `internal/orchestrator/types.go`
  - Defines persisted task, question, status, and machine-selection types.
- Create: `internal/orchestrator/store.go`
  - SQLite-backed task persistence and event logging.
- Create: `internal/orchestrator/store_test.go`
  - Tests inserts, updates, reload, question persistence, and active-task queries.
- Create: `internal/orchestrator/scheduler.go`
  - Round-robin scheduler and machine balancing logic.
- Create: `internal/orchestrator/scheduler_test.go`
  - Tests least-loaded machine selection and queue rotation.
- Create: `internal/orchestrator/runner.go`
  - Interfaces for SSH/Codex remote execution, probe, attach, resume, and stop.
- Create: `internal/orchestrator/runner_ssh.go`
  - Concrete SSH/Codex runner implementation.
- Create: `internal/orchestrator/runner_test.go`
  - Tests orchestrator-facing runner logic with fakes.
- Create: `internal/orchestrator/decision.go`
  - Workflow document loading and decision-layer request building.
- Create: `internal/orchestrator/decision_test.go`
  - Tests workflow loading, prompt assembly, and escalation gating.
- Create: `internal/orchestrator/service.go`
  - Main task lifecycle service: create, tick, reply, stop, recover.
- Create: `internal/orchestrator/service_test.go`
  - Tests state transitions and recovery decision order.
- Create: `internal/agent/task_command.go`
  - `/task` command parsing and response formatting.
- Create: `internal/agent/task_command_test.go`
  - Tests `/task start|list|status|reply|stop`.
- Modify: `internal/agent/router.go`
  - Routes `/task` commands to the task command handler.
- Modify: `cmd/alterego/main.go`
  - Loads orchestrator configs, opens SQLite DB, wires services into the bot.
- Modify: `README.md`
  - Documents config files, SQLite DB path, and task commands.

---

### Task 1: Add Orchestrator Config Types and Loaders

**Files:**
- Create: `internal/orchestrator/config.go`
- Create: `internal/orchestrator/config_test.go`
- Create: `configs/machines/example.yaml`
- Create: `configs/repositories/example.yaml`
- Create: `configs/templates/example.yaml`
- Create: `docs/workflows/example-feature-dev.md`

- [ ] **Step 1: Write failing config tests**

Create `internal/orchestrator/config_test.go` covering:

```go
func TestLoadConfigBindsTemplateToRepositoryAndWorkflow(t *testing.T)
func TestLoadConfigRejectsTemplateWithUnknownRepository(t *testing.T)
func TestLoadConfigRejectsRepositoryWithUnknownMachine(t *testing.T)
func TestLoadConfigRejectsTemplateWithMissingWorkflowFile(t *testing.T)
```

Each test should create a temp directory containing minimal YAML files and call a single loader entry point, for example:

```go
cfg, err := LoadRegistry(rootDir)
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because `LoadRegistry` and the config types do not exist.

- [ ] **Step 3: Implement minimal config loader**

Create `internal/orchestrator/config.go` with:

- `MachineConfig`
- `RepositoryConfig`
- `TemplateConfig`
- `Registry`
- `LoadRegistry(root string) (*Registry, error)`

Use a simple YAML layout such as:

```yaml
id: repo_backend
display_name: Backend Repo
remote_path: /srv/backend
default_branch: main
machine_ids:
  - machine_a
  - machine_b
```

and:

```yaml
id: feature_dev
repository_id: repo_backend
display_name: Feature Development
description: Default feature workflow
workflow_path: docs/workflows/example-feature-dev.md
```

- [ ] **Step 4: Add example config files**

Create example files in `configs/...` and `docs/workflows/...` that match the supported schema.

- [ ] **Step 5: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add configs/machines/example.yaml configs/repositories/example.yaml configs/templates/example.yaml docs/workflows/example-feature-dev.md internal/orchestrator/config.go internal/orchestrator/config_test.go
git commit -m "feat: add orchestrator config registry"
```

---

### Task 2: Add Persisted Task and Question Store

**Files:**
- Create: `internal/orchestrator/types.go`
- Create: `internal/orchestrator/store.go`
- Create: `internal/orchestrator/store_test.go`

- [ ] **Step 1: Write failing store tests**

Add tests for:

```go
func TestStoreCreatesTaskAndReloadsIt(t *testing.T)
func TestStoreUpdatesTaskStatusAndSessionFields(t *testing.T)
func TestStorePersistsAwaitingQuestion(t *testing.T)
func TestStoreListsActiveTasksForScheduler(t *testing.T)
```

Use a temp SQLite file path and assert persisted fields for:

- `task_id`
- `template_id`
- `repository_id`
- `machine_id`
- `status`
- `remote_workdir`
- `remote_codex_session_id`
- `awaiting_question`

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because the store and task types do not exist.

- [ ] **Step 3: Implement task types and SQLite store**

Create:

- `TaskStatus` constants:
  - `pending`
  - `starting`
  - `running`
  - `waiting_user_decision`
  - `detached`
  - `probing`
  - `attaching`
  - `resuming`
  - `completed`
  - `failed`
  - `stopped`
- `TaskRun`
- `AwaitingQuestion`
- `TaskEvent`

Implement store methods such as:

```go
func OpenStore(path string) (*Store, error)
func (s *Store) CreateTask(ctx context.Context, task TaskRun) error
func (s *Store) UpdateTask(ctx context.Context, task TaskRun) error
func (s *Store) GetTask(ctx context.Context, taskID string) (TaskRun, error)
func (s *Store) ListActiveTasks(ctx context.Context) ([]TaskRun, error)
func (s *Store) AppendEvent(ctx context.Context, event TaskEvent) error
```

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/types.go internal/orchestrator/store.go internal/orchestrator/store_test.go
git commit -m "feat: add orchestrator sqlite store"
```

---

### Task 3: Add Machine Balancing and Round-Robin Scheduler

**Files:**
- Create: `internal/orchestrator/scheduler.go`
- Create: `internal/orchestrator/scheduler_test.go`

- [ ] **Step 1: Write failing scheduler tests**

Add tests for:

```go
func TestSelectMachineChoosesLeastLoadedMachine(t *testing.T)
func TestSelectMachineBreaksTiesByRepositoryOrder(t *testing.T)
func TestSchedulerSkipsWaitingUserDecisionTasks(t *testing.T)
func TestSchedulerRotatesRunnableTasksRoundRobin(t *testing.T)
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because scheduler logic does not exist.

- [ ] **Step 3: Implement scheduler**

Create:

```go
type Scheduler struct { ... }
func SelectMachine(repo RepositoryConfig, active []TaskRun) (string, error)
func NewScheduler() *Scheduler
func (s *Scheduler) Next(tasks []TaskRun) (TaskRun, bool)
```

Rules:

- choose the repository machine with the fewest active tasks;
- tie-break by `repository.machine_ids` order;
- scheduler only rotates tasks not in `waiting_user_decision`, `completed`, `failed`, or `stopped`.

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/scheduler.go internal/orchestrator/scheduler_test.go
git commit -m "feat: add task scheduler and machine balancing"
```

---

### Task 4: Add Remote Runner Interfaces and Recovery Decision Order

**Files:**
- Create: `internal/orchestrator/runner.go`
- Create: `internal/orchestrator/runner_test.go`

- [ ] **Step 1: Write failing recovery-order tests**

Add tests for:

```go
func TestRecoverPrefersAttachWhenRemoteProcessIsAlive(t *testing.T)
func TestRecoverUsesResumeWhenRemoteProcessIsGone(t *testing.T)
func TestRecoverNeverStartsDuplicateSessionBeforeProbe(t *testing.T)
```

Use a fake runner that records calls like:

- `ProbeSession`
- `AttachLiveSession`
- `ResumeExitedSession`
- `StartNewSession`

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because runner interfaces and recovery orchestration do not exist.

- [ ] **Step 3: Implement runner contracts**

Create interfaces and small value types:

```go
type RemoteRunner interface {
    StartNewSession(ctx context.Context, req StartRequest) (RemoteSession, error)
    ProbeSession(ctx context.Context, req ProbeRequest) (ProbeResult, error)
    AttachLiveSession(ctx context.Context, req AttachRequest) (RemoteSession, error)
    ResumeExitedSession(ctx context.Context, req ResumeRequest) (RemoteSession, error)
    SendInput(ctx context.Context, session RemoteSession, input string) error
    ReadWindow(ctx context.Context, session RemoteSession) (OutputWindow, error)
    StopTask(ctx context.Context, session RemoteSession) error
}
```

Keep this file interface-only for now. The SSH implementation comes later.

- [ ] **Step 4: Add a small recovery helper**

Implement a helper in `runner.go` or `service.go`-local code that enforces:

1. probe
2. attach if alive
3. resume if dead

- [ ] **Step 5: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/runner.go internal/orchestrator/runner_test.go
git commit -m "feat: add remote runner contracts"
```

---

### Task 5: Add Workflow-Driven Decision Layer

**Files:**
- Create: `internal/orchestrator/decision.go`
- Create: `internal/orchestrator/decision_test.go`

- [ ] **Step 1: Write failing decision tests**

Add tests for:

```go
func TestDecisionContextIncludesWorkflowDocument(t *testing.T)
func TestDecisionContextIncludesRuntimeTaskFields(t *testing.T)
func TestEscalationDetectorRecognizesImplementationSolutionChoiceOnly(t *testing.T)
```

The tests should use a small fake workflow file and assert that the decision builder receives:

- fixed system rules
- workflow text
- user request
- latest summary
- last input

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because the decision layer does not exist.

- [ ] **Step 3: Implement minimal decision layer**

Create:

```go
type DecisionContext struct { ... }
type DecisionResult struct { ... }
type DecisionEngine interface {
    DecideNextStep(ctx context.Context, in DecisionContext) (DecisionResult, error)
}
func LoadWorkflow(path string) (string, error)
```

Also add a deterministic helper that marks only `implementation_solution_choice` as escalation-worthy.

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/decision.go internal/orchestrator/decision_test.go
git commit -m "feat: add workflow decision layer"
```

---

### Task 6: Implement Task Lifecycle Service

**Files:**
- Create: `internal/orchestrator/service.go`
- Create: `internal/orchestrator/service_test.go`

- [ ] **Step 1: Write failing lifecycle tests**

Add tests for:

```go
func TestCreateTaskSelectsMachineAndPersistsPendingTask(t *testing.T)
func TestTickStartsPendingTaskAndStoresRemoteSession(t *testing.T)
func TestTickMovesTaskToWaitingUserDecisionWhenDecisionRequiresUser(t *testing.T)
func TestReplyResumesWaitingTask(t *testing.T)
func TestRecoverDetachedTaskAttachesLiveSessionBeforeResume(t *testing.T)
func TestStopMarksTaskStoppedAndCallsRunner(t *testing.T)
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because the lifecycle service does not exist.

- [ ] **Step 3: Implement service**

Create methods such as:

```go
func NewService(store *Store, registry *Registry, scheduler *Scheduler, runner RemoteRunner, decider DecisionEngine) *Service
func (s *Service) StartTask(ctx context.Context, templateID, createdBy, userRequest string) (TaskRun, error)
func (s *Service) TickOnce(ctx context.Context) error
func (s *Service) Reply(ctx context.Context, taskID, text string) error
func (s *Service) Stop(ctx context.Context, taskID string) error
func (s *Service) List(ctx context.Context) ([]TaskRun, error)
func (s *Service) Status(ctx context.Context, taskID string) (TaskRun, error)
```

Implement the minimum state transitions from the spec.

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/service.go internal/orchestrator/service_test.go
git commit -m "feat: add task orchestration service"
```

---

### Task 7: Add SSH Runner Implementation

**Files:**
- Create: `internal/orchestrator/runner_ssh.go`

- [ ] **Step 1: Write a focused failing integration-style test with a fake transport seam**

Add or extend `internal/orchestrator/runner_test.go` so the SSH runner can be exercised through a transport interface instead of real SSH. Cover:

- start request command shape
- probe command shape
- attach path
- resume path
- stop path

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: FAIL because the concrete SSH runner does not exist.

- [ ] **Step 3: Implement SSH runner**

Create `internal/orchestrator/runner_ssh.go` with:

- SSH client abstraction
- remote command builder helpers
- `StartNewSession`
- `ProbeSession`
- `AttachLiveSession`
- `ResumeExitedSession`
- `SendInput`
- `ReadWindow`
- `StopTask`

Keep command construction centralized so future Codex CLI syntax changes are localized.

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/orchestrator
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/runner_ssh.go internal/orchestrator/runner_test.go
git commit -m "feat: add ssh codex runner"
```

---

### Task 8: Add `/task` Agent Commands

**Files:**
- Create: `internal/agent/task_command.go`
- Create: `internal/agent/task_command_test.go`
- Modify: `internal/agent/router.go`

- [ ] **Step 1: Write failing `/task` command tests**

Add tests for:

```go
func TestTaskCommandStartCreatesTask(t *testing.T)
func TestTaskCommandListFormatsActiveTasks(t *testing.T)
func TestTaskCommandStatusFormatsTaskDetails(t *testing.T)
func TestTaskCommandReplyResumesWaitingTask(t *testing.T)
func TestTaskCommandStopStopsTask(t *testing.T)
```

Use a fake task service interface rather than the full orchestrator in these tests.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: FAIL because `/task` command handling does not exist.

- [ ] **Step 3: Implement task command handler and wire router**

Create:

```go
type TaskService interface { ... }
type TaskCommandHandler struct { ... }
```

Support:

- `/task start <template> <requirement text>`
- `/task list`
- `/task status <task-id>`
- `/task reply <task-id> <decision text>`
- `/task stop <task-id>`

Modify `internal/agent/router.go` so `/task` routes to the new command handler.

- [ ] **Step 4: Run test to verify it passes**

Run:

```bash
go test -count=1 ./internal/agent
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/task_command.go internal/agent/task_command_test.go internal/agent/router.go
git commit -m "feat: add remote task bot commands"
```

---

### Task 9: Wire Orchestrator Into Process Startup

**Files:**
- Modify: `cmd/alterego/main.go`
- Modify: `README.md`

- [ ] **Step 1: Write a failing startup test or compile-only seam**

If adding a direct startup test is awkward, add a small constructor function that can be unit-tested, for example:

```go
func buildTaskSubsystem(...) (...)
```

Then write a failing test verifying it errors on missing config directory or DB path.

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test -count=1 ./cmd/alterego
```

Expected: FAIL because the builder seam does not exist.

- [ ] **Step 3: Implement wiring**

Modify `cmd/alterego/main.go` to:

- load registry config from repo paths;
- open SQLite DB;
- construct store, scheduler, runner, decision engine, and service;
- construct `/task` command handler;
- pass both ordinary chat commands and `/task` commands into the router.

Document in `README.md`:

- config directory layout
- SQLite DB file path env var
- task commands

- [ ] **Step 4: Run full verification**

Run:

```bash
go test -count=1 ./...
go build ./cmd/alterego
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/alterego/main.go README.md
git commit -m "feat: wire remote codex task orchestrator"
```

---

## Self-Review

- Spec coverage:
  - repository-bound machines: covered in Task 1 and Task 3
  - template/workflow binding: covered in Task 1 and Task 5
  - SQLite persistence: covered in Task 2
  - round-robin task scheduling: covered in Task 3 and Task 6
  - remote Codex session start/recover: covered in Task 4, Task 6, and Task 7
  - attach-before-resume rule: covered in Task 4 and Task 6
  - Lark `/task` commands: covered in Task 8
  - process wiring: covered in Task 9
- Placeholder scan:
  - no `TODO`/`TBD` placeholders
  - every task includes concrete files, commands, and pass/fail expectations
- Type consistency:
  - `Registry`, `TaskRun`, `AwaitingQuestion`, `RemoteRunner`, `Service`, and `TaskCommandHandler` naming is consistent across tasks
