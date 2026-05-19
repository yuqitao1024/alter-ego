# Codex App-Server Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current `tmux`-backed remote Codex task path with a `codex app-server`-backed path while preserving the existing Lark, SQLite, scheduler, workflow, and deployment control plane.

**Architecture:** Keep the current orchestrator outer shell and swap the remote session substrate. SSH remains the bootstrap and proxy mechanism, but remote task state comes from `codex app-server` thread, turn, item, and status notifications instead of scraped terminal UI. The migration is phased: add app-server persistence and runner support first, switch orchestration to the new state source, then freeze and remove `tmux`-specific logic.

**Tech Stack:** Go, `database/sql` with `modernc.org/sqlite`, existing Lark adapter, SSH subprocess transport, `codex app-server`, `codex app-server proxy`, remote Unix sockets

---

## File Structure

### Existing files to modify

- `internal/orchestrator/types.go`
  - Replace `tmux`-specific persisted fields with app-server thread metadata while keeping temporary compatibility where needed.
- `internal/orchestrator/store.go`
  - Add SQLite schema support for app-server thread state and stop depending on `tmux`-only fields for new tasks.
- `internal/orchestrator/config.go`
  - Extend machine configuration with app-server bootstrap and socket settings.
- `internal/orchestrator/config_test.go`
  - Cover new machine settings and repository/template compatibility.
- `internal/orchestrator/runner.go`
  - Replace the `tmux`-shaped runner contract with an app-server session contract.
- `internal/orchestrator/service.go`
  - Switch lifecycle, recovery, and phase handling to app-server thread/turn state.
- `internal/orchestrator/decision.go`
  - Add workflow-stage-aware model arbitration and remove TUI-specific planning/executing shortcuts.
- `internal/orchestrator/decision_test.go`
  - Cover workflow stage mapping, phase hard gates, and app-server-driven completion/wait behavior.
- `internal/orchestrator/service_test.go`
  - Cover thread-backed start, reply, completion, and restart recovery.
- `cmd/alterego/main.go`
  - Wire the new app-server runner and fail fast if required machine app-server settings are missing.
- `README.md`
  - Replace `tmux` operating model with the app-server operating model.
- `packaging/templates/alterego.env.example`
  - Document any new config knobs that belong in env rather than YAML.

### Existing files expected to be deleted after cutover

- `internal/orchestrator/responder.go`
- `internal/orchestrator/responder_test.go`
- `internal/orchestrator/runner_ssh.go`
- `internal/orchestrator/runner_ssh_test.go`

These should not be deleted until the app-server path is working and selected as the default path.

### New files to create

- `internal/orchestrator/appserver_types.go`
  - Local protocol structs needed by the orchestrator for thread, turn, item, and notification handling.
- `internal/orchestrator/appserver_client.go`
  - JSON-RPC-ish client for app-server stdio/proxy transport.
- `internal/orchestrator/appserver_client_test.go`
  - Fake transport coverage for request/response and notification handling.
- `internal/orchestrator/appserver_runner.go`
  - Concrete `RemoteRunner` implementation backed by app-server threads rather than `tmux`.
- `internal/orchestrator/appserver_runner_test.go`
  - Runner-level behavior tests for thread start, turn start, turn steer, reconnection, and completion.
- `internal/orchestrator/appserver_proxy_ssh.go`
  - SSH bootstrap and remote `codex app-server proxy` process management.
- `internal/orchestrator/appserver_proxy_ssh_test.go`
  - Command-shape tests for remote bootstrap and proxy startup.

## Task 1: Add app-server state to persistence and configuration

**Files:**
- Create: `internal/orchestrator/appserver_types.go`
- Modify: `internal/orchestrator/types.go`
- Modify: `internal/orchestrator/store.go`
- Modify: `internal/orchestrator/config.go`
- Test: `internal/orchestrator/store_test.go`
- Test: `internal/orchestrator/config_test.go`

- [ ] **Step 1: Write failing store tests for app-server task metadata**

```go
func TestStorePersistsAppServerThreadState(t *testing.T) {
	store := openTestStore(t)
	now := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)

	task := TaskRun{
		TaskID:            "task-appserver",
		TemplateID:        "simt-stl-dev",
		RepositoryID:      "simt-stl",
		MachineID:         "A5-82",
		Status:            StatusRunning,
		Phase:             TaskPhasePlanning,
		WorkflowStage:     WorkflowStagePlanWriting,
		ThreadID:          "thread_123",
		ActiveTurnID:      "turn_456",
		LastThreadStatus:  "running",
		LastTurnStatus:    "running",
		LastObservedItemID:"item_789",
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}

	persisted, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}

	if persisted.ThreadID != "thread_123" || persisted.ActiveTurnID != "turn_456" {
		t.Fatalf("persisted thread state = %#v", persisted)
	}
	if persisted.WorkflowStage != WorkflowStagePlanWriting {
		t.Fatalf("WorkflowStage = %q, want %q", persisted.WorkflowStage, WorkflowStagePlanWriting)
	}
}
```

- [ ] **Step 2: Run store tests to verify they fail**

Run: `go test -count=1 ./internal/orchestrator -run 'TestStorePersistsAppServerThreadState'`
Expected: FAIL because `TaskRun` and the schema do not yet have `ThreadID`, `ActiveTurnID`, and `WorkflowStage`.

- [ ] **Step 3: Add new task fields and workflow-stage enum**

```go
type WorkflowStage string

const (
	WorkflowStageRequirementDiscussion WorkflowStage = "requirement_discussion"
	WorkflowStageSpecWriting           WorkflowStage = "spec_writing"
	WorkflowStageSpecReview            WorkflowStage = "spec_review"
	WorkflowStagePlanWriting           WorkflowStage = "plan_writing"
	WorkflowStagePlanReview            WorkflowStage = "plan_review"
	WorkflowStageImplementation        WorkflowStage = "implementation"
	WorkflowStageVerification          WorkflowStage = "verification"
	WorkflowStageIntegration           WorkflowStage = "integration"
)

type TaskRun struct {
	// existing fields...
	WorkflowStage       WorkflowStage
	ThreadID            string
	ActiveTurnID        string
	LastThreadStatus    string
	LastTurnStatus      string
	LastObservedItemID  string
	LastRemoteActivityAt *time.Time
}
```

- [ ] **Step 4: Extend SQLite schema and scans for app-server fields**

```go
const tasksSchema = `
ALTER TABLE tasks ADD COLUMN workflow_stage TEXT NOT NULL DEFAULT 'requirement_discussion';
ALTER TABLE tasks ADD COLUMN thread_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN active_turn_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN last_thread_status TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN last_turn_status TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN last_observed_item_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN last_remote_activity_at TEXT NULL;
`
```

Also update:

- `CreateTask`
- `UpdateTask`
- `scanTask`
- `ListActiveTasks`

to read and write the new columns.

- [ ] **Step 5: Add failing config tests for app-server machine settings**

```go
func TestLoadMachineConfigWithAppServerFields(t *testing.T) {
	root := writeConfigFixture(t, `
id: A5-82
display_name: NPU2
host: 127.0.0.1
port: 20002
user: root
shell_init:
  - source /home/y00621698/env.sh
app_server_socket: /home/y00621698/.codex/app-server.sock
app_server_bootstrap:
  - codex remote-control -c model=\"gpt-5.4\"
`)

	machines, err := LoadMachineConfigs(root)
	if err != nil {
		t.Fatalf("LoadMachineConfigs returned error: %v", err)
	}

	got := machines["A5-82"]
	if got.AppServerSocket != "/home/y00621698/.codex/app-server.sock" {
		t.Fatalf("AppServerSocket = %q", got.AppServerSocket)
	}
}
```

- [ ] **Step 6: Run config tests to verify they fail**

Run: `go test -count=1 ./internal/orchestrator -run 'TestLoadMachineConfigWithAppServerFields'`
Expected: FAIL because `MachineConfig` does not yet include `app_server_socket` and `app_server_bootstrap`.

- [ ] **Step 7: Add app-server settings to machine config**

```go
type MachineConfig struct {
	ID                 string   `yaml:"id"`
	DisplayName        string   `yaml:"display_name"`
	Host               string   `yaml:"host"`
	Port               int      `yaml:"port"`
	User               string   `yaml:"user"`
	ShellInit          []string `yaml:"shell_init"`
	AppServerSocket    string   `yaml:"app_server_socket"`
	AppServerBootstrap []string `yaml:"app_server_bootstrap"`
}
```

- [ ] **Step 8: Run focused orchestrator tests**

Run: `go test -count=1 ./internal/orchestrator -run 'Test(StorePersistsAppServerThreadState|LoadMachineConfigWithAppServerFields)'`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/orchestrator/types.go \
  internal/orchestrator/store.go \
  internal/orchestrator/store_test.go \
  internal/orchestrator/config.go \
  internal/orchestrator/config_test.go \
  internal/orchestrator/appserver_types.go
git commit -m "feat: persist app-server task metadata"
```

### Task 2: Build an app-server client and SSH-backed proxy transport

**Files:**
- Create: `internal/orchestrator/appserver_client.go`
- Create: `internal/orchestrator/appserver_client_test.go`
- Create: `internal/orchestrator/appserver_proxy_ssh.go`
- Create: `internal/orchestrator/appserver_proxy_ssh_test.go`
- Modify: `internal/orchestrator/runner.go`

- [ ] **Step 1: Write failing client tests for thread start and turn start**

```go
func TestAppServerClientStartsThreadAndTurn(t *testing.T) {
	transport := newFakeAppServerTransport()
	transport.enqueueResponse(`{"id":"1","result":{"thread":{"id":"thread_123"}}}`)
	transport.enqueueResponse(`{"id":"2","result":{"turn":{"id":"turn_456"}}}`)

	client := NewAppServerClient(transport)

	threadID, err := client.StartThread(context.Background(), ThreadStartRequest{
		Cwd: "/srv/tasks/task-1/repo",
		BaseInstructions: "workflow",
	})
	if err != nil {
		t.Fatalf("StartThread returned error: %v", err)
	}

	if threadID != "thread_123" {
		t.Fatalf("threadID = %q, want %q", threadID, "thread_123")
	}
}
```

- [ ] **Step 2: Run client tests to verify they fail**

Run: `go test -count=1 ./internal/orchestrator -run 'TestAppServerClientStartsThreadAndTurn'`
Expected: FAIL because `NewAppServerClient` and the transport contract do not exist.

- [ ] **Step 3: Define a minimal app-server transport interface**

```go
type AppServerTransport interface {
	Send(ctx context.Context, request []byte) ([]byte, error)
	Recv(ctx context.Context) ([]byte, error)
	Close() error
}

type AppServerClient struct {
	transport AppServerTransport
}
```

- [ ] **Step 4: Implement minimal request methods**

Implement request wrappers for:

- `thread/start`
- `turn/start`
- `turn/steer`
- `thread/get`
- `thread/items/list`

using small internal structs instead of generated TypeScript.

- [ ] **Step 5: Write failing SSH proxy command-shape tests**

```go
func TestSSHAppServerProxyStartsRemoteProxyCommand(t *testing.T) {
	transport := &fakeSSHCommandTransport{}
	proxy := NewSSHAppServerProxy(transport)

	_, err := proxy.Connect(context.Background(), MachineConfig{
		ID:              "A5-82",
		Host:            "127.0.0.1",
		Port:            20002,
		User:            "root",
		ShellInit:       []string{"source /home/y00621698/env.sh"},
		AppServerSocket: "/home/y00621698/.codex/app-server.sock",
	})
	if err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}

	if !strings.Contains(transport.lastCommand, "codex app-server proxy --sock '/home/y00621698/.codex/app-server.sock'") {
		t.Fatalf("command = %q", transport.lastCommand)
	}
}
```

- [ ] **Step 6: Run proxy tests to verify they fail**

Run: `go test -count=1 ./internal/orchestrator -run 'TestSSHAppServerProxyStartsRemoteProxyCommand'`
Expected: FAIL because the proxy transport does not exist.

- [ ] **Step 7: Implement SSH bootstrap and proxy transport**

Build the remote startup flow in this order:

```sh
source /home/y00621698/env.sh
test -S /home/y00621698/.codex/app-server.sock || codex remote-control >/tmp/alterego-app-server.log 2>&1 &
codex app-server proxy --sock /home/y00621698/.codex/app-server.sock
```

The Go wrapper should:

- inject `machine.shell_init`;
- run `machine.app_server_bootstrap` only if the socket is absent;
- then attach proxy stdio to the client transport.

- [ ] **Step 8: Run focused client and proxy tests**

Run: `go test -count=1 ./internal/orchestrator -run 'Test(AppServerClientStartsThreadAndTurn|SSHAppServerProxyStartsRemoteProxyCommand)'`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add internal/orchestrator/runner.go \
  internal/orchestrator/appserver_client.go \
  internal/orchestrator/appserver_client_test.go \
  internal/orchestrator/appserver_proxy_ssh.go \
  internal/orchestrator/appserver_proxy_ssh_test.go
git commit -m "feat: add codex app-server client transport"
```

### Task 3: Add an app-server-backed remote runner

**Files:**
- Create: `internal/orchestrator/appserver_runner.go`
- Create: `internal/orchestrator/appserver_runner_test.go`
- Modify: `internal/orchestrator/runner.go`
- Modify: `internal/orchestrator/runner_test.go`

- [ ] **Step 1: Write failing runner tests for thread-backed sessions**

```go
func TestAppServerRunnerStartsThreadBackedSession(t *testing.T) {
	client := newFakeAppServerClient()
	runner := NewAppServerRunner(client)

	session, err := runner.StartInteractiveSession(context.Background(), StartRequest{
		TaskID:              "task-1",
		RepositoryID:        "simt-stl",
		RemoteRepoURL:       "git@gitcode.com:cann-sigs/simt-stl.git",
		RemoteWorkspaceRoot: "/srv/codex-tasks",
		CheckoutBranch:      "main",
		UserRequest:         "Implement issue #20",
		WorkflowContent:     "workflow",
	})
	if err != nil {
		t.Fatalf("StartInteractiveSession returned error: %v", err)
	}

	if session.ThreadID == "" {
		t.Fatal("ThreadID is empty")
	}
}
```

- [ ] **Step 2: Run runner tests to verify they fail**

Run: `go test -count=1 ./internal/orchestrator -run 'TestAppServerRunnerStartsThreadBackedSession'`
Expected: FAIL because the app-server runner does not exist.

- [ ] **Step 3: Replace session identity with thread identity**

```go
type RemoteSession struct {
	MachineID        string
	Workdir          string
	ThreadID         string
	ActiveTurnID     string
	LastOutputWindow OutputWindow
}
```

Keep `TMUXSessionName` temporarily only if older code still compiles during this commit. Do not keep using it for new runner logic.

- [ ] **Step 4: Implement app-server-backed session methods**

`StartInteractiveSession` should:

1. prepare remote workspace with the existing deterministic startup flow;
2. connect to app-server through the SSH proxy transport;
3. start a thread with base instructions and cwd;
4. start a turn with the initial workflow and user request;
5. return `RemoteSession` populated with `ThreadID`, `ActiveTurnID`, and `Workdir`.

`CaptureOutput` should:

- fetch current thread state and items;
- aggregate the most recent meaningful `agentMessage`, `plan`, and `commandExecution` items;
- return summary text and machine-readable thread status instead of terminal state.

`SendInteractiveInput` should:

- call `turn/steer` when there is an active turn;
- otherwise start a new turn for the existing thread.

`HasSession` should:

- verify the thread still exists on the app-server side.

`ResumeLastCodexSession` should not exist in the new path as a `tmux` recovery hack. Replace it with thread reconnect semantics.

- [ ] **Step 5: Run focused runner tests**

Run: `go test -count=1 ./internal/orchestrator -run 'TestAppServerRunner'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/appserver_runner.go \
  internal/orchestrator/appserver_runner_test.go \
  internal/orchestrator/runner.go \
  internal/orchestrator/runner_test.go
git commit -m "feat: add app-server-backed remote runner"
```

### Task 4: Replace TUI-driven orchestration with thread/turn-driven orchestration

**Files:**
- Modify: `internal/orchestrator/service.go`
- Modify: `internal/orchestrator/service_test.go`
- Modify: `internal/orchestrator/decision.go`
- Modify: `internal/orchestrator/decision_test.go`

- [ ] **Step 1: Write failing service tests for workflow-stage-driven phase changes**

```go
func TestServicePromotesToExecutingOnlyFromImplementationStage(t *testing.T) {
	service, store, runner, decider := newServiceHarness(t)
	task := seedRunningTask(t, store, TaskRun{
		TaskID:        "task-phase",
		Status:        StatusRunning,
		Phase:         TaskPhasePlanning,
		WorkflowStage: WorkflowStagePlanWriting,
		ThreadID:      "thread_123",
	})

	runner.outputWindow = OutputWindow{
		Summary: "Spec approved. Writing implementation plan now.",
	}
	decider.result = DecisionResult{
		Action:        DecisionActionWait,
		NextPhase:     TaskPhasePlanning,
		WorkflowStage: WorkflowStagePlanWriting,
		Summary:       "Codex is writing the implementation plan.",
	}

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, _ := store.GetTask(context.Background(), task.TaskID)
	if persisted.Phase != TaskPhasePlanning {
		t.Fatalf("Phase = %q, want %q", persisted.Phase, TaskPhasePlanning)
	}
}
```

- [ ] **Step 2: Run service tests to verify they fail**

Run: `go test -count=1 ./internal/orchestrator -run 'TestServicePromotesToExecutingOnlyFromImplementationStage'`
Expected: FAIL because the service still promotes based on generic execution keywords.

- [ ] **Step 3: Remove `wantsExecutionPhase`-based promotion**

Delete this behavior:

```go
if wantsExecutionPhase(text) {
	task.Phase = TaskPhaseExecuting
}
```

and replace it with workflow-stage mapping.

- [ ] **Step 4: Extend decision results with workflow stage**

```go
type DecisionResult struct {
	Action        string
	DecisionType  string
	NextPhase     TaskPhase
	WorkflowStage WorkflowStage
	NextInput     string
	CodexReply    string
	Summary       string
	Question      *AwaitingQuestion
}
```

The model prompt must require:

- `workflow_stage`
- `next_phase`

and the service must treat `workflow_stage` as the real source of phase mapping.

- [ ] **Step 5: Remove `tmux` responders and screen-digest-only planning hacks from the new path**

Delete or stop using:

- `plan_prompt_dismiss`
- `plan_prompt_continued`
- `LastContinuationScreenDigest`
- `PendingPostResponderAction`

for app-server-backed tasks.

Keep only deterministic infrastructure blockers:

- login required
- usage limit
- transport unavailable

- [ ] **Step 6: Replace output interpretation with thread-item interpretation**

Use recent app-server items to decide:

- still working;
- waiting for user;
- completed;
- blocked;
- ready for a direct Codex reply.

The summary source should come from structured item content, not terminal screen tails.

- [ ] **Step 7: Run focused service and decision tests**

Run: `go test -count=1 ./internal/orchestrator -run 'Test(Service|Decision)'`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/orchestrator/service.go \
  internal/orchestrator/service_test.go \
  internal/orchestrator/decision.go \
  internal/orchestrator/decision_test.go
git commit -m "feat: drive task phases from app-server state"
```

### Task 5: Add restart recovery and cut new tasks over to app-server

**Files:**
- Modify: `internal/orchestrator/service.go`
- Modify: `internal/orchestrator/runner.go`
- Modify: `cmd/alterego/main.go`
- Test: `internal/orchestrator/service_test.go`
- Test: `cmd/alterego/main_test.go`

- [ ] **Step 1: Write failing recovery tests for thread reconnect**

```go
func TestRecoverDetachedTaskReconnectsByThreadID(t *testing.T) {
	service, store, runner, _ := newServiceHarness(t)
	task := seedRunningTask(t, store, TaskRun{
		TaskID:        "task-recover",
		Status:        StatusDetached,
		ThreadID:      "thread_123",
		ActiveTurnID:  "turn_456",
		Phase:         TaskPhaseExecuting,
		WorkflowStage: WorkflowStageImplementation,
	})

	runner.reconnectSession = RemoteSession{
		MachineID:    "A5-82",
		Workdir:      "/srv/tasks/task-recover/repo",
		ThreadID:     "thread_123",
		ActiveTurnID: "turn_456",
	}

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}

	persisted, _ := store.GetTask(context.Background(), task.TaskID)
	if persisted.Status != StatusRunning {
		t.Fatalf("Status = %q, want %q", persisted.Status, StatusRunning)
	}
}
```

- [ ] **Step 2: Run recovery tests to verify they fail**

Run: `go test -count=1 ./internal/orchestrator -run 'TestRecoverDetachedTaskReconnectsByThreadID'`
Expected: FAIL because reconnect still assumes `tmux` session identity.

- [ ] **Step 3: Rework detached recovery to use thread existence**

Recovery should:

1. reconnect to remote app-server;
2. verify `thread_id` exists;
3. refresh active turn state;
4. update task status back to `running` if the thread is valid;
5. otherwise move the task to `failed` or keep it `detached` with a clear event.

- [ ] **Step 4: Fail fast in main if app-server machine settings are incomplete**

```go
for _, machine := range machines {
	if machine.AppServerSocket == "" {
		return fmt.Errorf("machine %q missing app_server_socket", machine.ID)
	}
}
```

- [ ] **Step 5: Run orchestrator and main focused tests**

Run: `go test -count=1 ./internal/orchestrator ./cmd/alterego`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/service.go \
  internal/orchestrator/service_test.go \
  internal/orchestrator/runner.go \
  cmd/alterego/main.go \
  cmd/alterego/main_test.go
git commit -m "feat: recover remote tasks through app-server threads"
```

### Task 6: Update docs, packaging, and freeze the tmux path

**Files:**
- Modify: `README.md`
- Modify: `packaging/README.md`
- Modify: `packaging/templates/alterego.env.example`
- Modify: `docs/superpowers/specs/2026-05-11-remote-codex-task-orchestrator-design.md`
- Modify: `docs/superpowers/plans/2026-05-11-remote-codex-task-orchestrator.md`
- Optionally delete: `internal/orchestrator/responder.go`
- Optionally delete: `internal/orchestrator/responder_test.go`
- Optionally delete: `internal/orchestrator/runner_ssh.go`
- Optionally delete: `internal/orchestrator/runner_ssh_test.go`

- [ ] **Step 1: Update user-facing docs for app-server**

Document:

- remote machine requirements:
  - `codex app-server` / `codex remote-control`
  - remote socket path
- no `tmux` requirement for new tasks
- new recovery model based on `thread_id`

- [ ] **Step 2: Update packaging templates**

Add commented examples for:

```env
# ALTER_EGO_REMOTE_APP_SERVER_MODEL=gpt-5.4
# ALTER_EGO_REMOTE_APP_SERVER_SOCKET=/home/<user>/.codex/app-server.sock
```

and describe that machine YAML now owns:

- `app_server_socket`
- `app_server_bootstrap`

- [ ] **Step 3: Freeze the old tmux spec/plan docs**

Add a note near the top of the May 11 spec and plan:

```md
> Superseded by `2026-05-19-codex-app-server-migration-design.md` once the app-server runner lands.
```

- [ ] **Step 4: Delete tmux-specific implementation files only after all app-server tests pass**

Delete:

```bash
internal/orchestrator/responder.go
internal/orchestrator/responder_test.go
internal/orchestrator/runner_ssh.go
internal/orchestrator/runner_ssh_test.go
```

only after:

```bash
go test -count=1 ./...
go build ./cmd/alterego
```

pass with the app-server path as the default path.

- [ ] **Step 5: Run full verification**

Run: `go test -count=1 ./...`
Expected: PASS

Run: `go build ./cmd/alterego`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add README.md \
  packaging/README.md \
  packaging/templates/alterego.env.example \
  docs/superpowers/specs/2026-05-11-remote-codex-task-orchestrator-design.md \
  docs/superpowers/plans/2026-05-11-remote-codex-task-orchestrator.md \
  docs/superpowers/specs/2026-05-19-codex-app-server-migration-design.md
git rm internal/orchestrator/responder.go \
  internal/orchestrator/responder_test.go \
  internal/orchestrator/runner_ssh.go \
  internal/orchestrator/runner_ssh_test.go
git commit -m "refactor: switch remote tasks to codex app-server"
```

## Self-Review

- **Spec coverage:** This plan covers transport replacement, persistence changes, workflow-stage-aware phase control, restart recovery, docs, and the final `tmux` freeze/removal. No spec requirement is intentionally omitted.
- **Placeholder scan:** No `TODO`, `TBD`, or “implement later” placeholders remain in tasks.
- **Type consistency:** The plan consistently uses `ThreadID`, `ActiveTurnID`, `WorkflowStage`, `TaskPhase`, and app-server `thread/turn/item` concepts. It does not mix those back with `tmux_session_name` for new-path logic.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-05-19-codex-app-server-migration.md`.

Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
