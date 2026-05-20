# Codex Supervisor Event Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace summary-driven Codex supervision with explicit app-server request handling, persist exactly-once request/completion state, simplify the task state machine, and keep polling only for progress reporting.

**Architecture:** Treat Codex app-server server requests as the only authority for Codex-facing replies. Persist request lifecycle rows and task-level completion-check control flags in SQLite. Remove `phase` and `workflowStage`, merge startup states, rename `detached` to `recovering`, and split the supervisor logic into protocol ingestion, model classification, hard policy gates, and progress reporting.

**Tech Stack:** Go 1.23, existing SQLite store, `internal/codexappserver`, `internal/orchestrator`, standard library tests, existing OpenAI-backed decision model adapter.

---

## File Structure
- Modify: `internal/codexappserver/protocol.go`
  - Add typed server-request envelopes and resolved notifications.
- Modify: `internal/codexappserver/client.go`
  - Expose server-request notifications in the runtime stream.
- Modify: `internal/codexappserver/snapshot.go`
  - Keep snapshot focused on status/summary only; do not make it the source of truth for reply state.
- Modify: `internal/codexappserver/manager.go`
  - Surface actionable server requests alongside snapshots and preserve `thread/resume` semantics.
- Modify: `internal/codexappserver/client_test.go`
  - Verify protocol decoding for server requests and `serverRequest/resolved`.
- Modify: `internal/codexappserver/manager_test.go`
  - Verify reconnect preserves unresolved request delivery without duplicate reply emission.
- Modify: `internal/orchestrator/types.go`
  - Delete `phase` and `workflowStage`; rename status values; add persisted control fields.
- Modify: `internal/orchestrator/store.go`
  - Remove deleted columns from `tasks`; add request/completion columns and a new `task_server_requests` table.
- Modify: `internal/orchestrator/store_test.go`
  - Update schema expectations and add request lifecycle persistence tests.
- Create: `internal/orchestrator/supervisor_policy.go`
  - Define structured model schema and hard policy gate entrypoints.
- Create: `internal/orchestrator/supervisor_policy_test.go`
  - Test no-unsolicited-reply, reply-once, and completion-check-once rules.
- Modify: `internal/orchestrator/decision.go`
  - Replace phase/stage-oriented prompt with narrow supervisor classification prompt and structured output parsing.
- Modify: `internal/orchestrator/decision_test.go`
  - Test classification schema for plan decision, execution approval, progress update, completion signal, and ignore.
- Modify: `internal/orchestrator/service.go`
  - Remove summary-driven ask-user logic; add request-driven control flow and 2-minute progress reporting.
- Modify: `internal/orchestrator/service_test.go`
  - Rework lifecycle tests around `starting`, `recovering`, persisted requests, completion-check-once, and no unsolicited Codex replies.
- Modify: `internal/orchestrator/scheduler.go`
  - Replace `detached`/startup status handling with `recovering`/`starting`.
- Modify: `cmd/alterego/main.go`
  - Change polling interval from 10 seconds to 2 minutes and wire request-aware supervisor service.

---

### Task 1: Simplify Task State And Persistence Schema

**Files:**
- Modify: `internal/orchestrator/types.go`
- Modify: `internal/orchestrator/store.go`
- Modify: `internal/orchestrator/store_test.go`

- [ ] **Step 1: Write failing store tests for the simplified task state**

Add to `internal/orchestrator/store_test.go`:

```go
func TestStorePersistsSupervisorControlFields(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	task := TaskRun{
		TaskID:                 "task-supervisor",
		TemplateID:             "feature_dev",
		RepositoryID:           "repo_backend",
		MachineID:              "machine_a",
		Status:                 StatusRecovering,
		UserRequest:            "continue",
		CreatedBy:              "ou_1",
		ThreadID:               "thread-1",
		ActiveTurnID:           "turn-1",
		LastOutputSummary:      "running tests",
		PendingRequestID:       "req-1",
		CompletionCheckStatus:  CompletionCheckStatusSent,
		CompletionCheckSentAt:  ptrTime(now.Add(time.Minute)),
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	if err := store.CreateTask(context.Background(), task); err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}

	got, err := store.GetTask(context.Background(), task.TaskID)
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}

	if got.Status != StatusRecovering {
		t.Fatalf("Status = %q, want %q", got.Status, StatusRecovering)
	}
	if got.PendingRequestID != "req-1" {
		t.Fatalf("PendingRequestID = %q", got.PendingRequestID)
	}
	if got.CompletionCheckStatus != CompletionCheckStatusSent {
		t.Fatalf("CompletionCheckStatus = %q", got.CompletionCheckStatus)
	}
}
```

Add:

```go
func TestStorePersistsTaskServerRequestLifecycle(t *testing.T) {
	t.Parallel()

	store := openTestStore(t)
	defer store.Close()

	req := TaskServerRequest{
		RequestID:    "req-1",
		TaskID:       "task-1",
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		RequestType:  ServerRequestTypeUserInput,
		RequestPayload: `{"prompt":"choose"}`,
		Status:       ServerRequestStatusPending,
		CreatedAt:    time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	}

	if err := store.UpsertTaskServerRequest(context.Background(), req); err != nil {
		t.Fatalf("UpsertTaskServerRequest returned error: %v", err)
	}

	if err := store.MarkTaskServerRequestReplying(context.Background(), "req-1", req.CreatedAt.Add(time.Minute)); err != nil {
		t.Fatalf("MarkTaskServerRequestReplying returned error: %v", err)
	}

	if err := store.MarkTaskServerRequestReplied(context.Background(), "req-1", "continue", req.CreatedAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkTaskServerRequestReplied returned error: %v", err)
	}

	got, err := store.GetTaskServerRequest(context.Background(), "req-1")
	if err != nil {
		t.Fatalf("GetTaskServerRequest returned error: %v", err)
	}
	if got.Status != ServerRequestStatusReplied {
		t.Fatalf("Status = %q, want %q", got.Status, ServerRequestStatusReplied)
	}
	if got.ReplyContent != "continue" {
		t.Fatalf("ReplyContent = %q", got.ReplyContent)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/orchestrator -run 'TestStorePersistsSupervisorControlFields|TestStorePersistsTaskServerRequestLifecycle' -count=1
```

Expected: FAIL because `TaskRun` has no supervisor control fields and the store has no `task_server_requests` table helpers.

- [ ] **Step 3: Delete old lifecycle fields and add new persisted fields**

Update `internal/orchestrator/types.go`:

```go
const (
	StatusPending          TaskStatus = "pending"
	StatusStarting         TaskStatus = "starting"
	StatusRunning          TaskStatus = "running"
	StatusWaitingUserInput TaskStatus = "waiting_user_input"
	StatusRecovering       TaskStatus = "recovering"
	StatusCompleted        TaskStatus = "completed"
	StatusFailed           TaskStatus = "failed"
	StatusStopped          TaskStatus = "stopped"
)

type CompletionCheckStatus string

const (
	CompletionCheckStatusNotStarted      CompletionCheckStatus = "not_started"
	CompletionCheckStatusSent            CompletionCheckStatus = "sent"
	CompletionCheckStatusConfirmedDone   CompletionCheckStatus = "confirmed_done"
	CompletionCheckStatusReportedPending CompletionCheckStatus = "reported_remaining"
)

type TaskRun struct {
	TaskID                  string
	TemplateID              string
	RepositoryID            string
	MachineID               string
	Status                  TaskStatus
	UserRequest             string
	CreatedBy               string
	RemoteWorkdir           string
	ThreadID                string
	ActiveTurnID            string
	LastInput               string
	LastOutputSummary       string
	LastDecisionAction      string
	PendingRequestID        string
	CompletionCheckStatus   CompletionCheckStatus
	CompletionCheckSentAt   *time.Time
	CompletionCheckDoneAt   *time.Time
	AwaitingQuestion        *AwaitingQuestion
	CreatedAt               time.Time
	UpdatedAt               time.Time
}
```

Add new request lifecycle types in the same file:

```go
type ServerRequestType string
type ServerRequestStatus string

const (
	ServerRequestTypeUserInput      ServerRequestType = "request_user_input"
	ServerRequestTypeCommandApproval ServerRequestType = "command_approval"
	ServerRequestTypeFileApproval    ServerRequestType = "file_change_approval"
)

const (
	ServerRequestStatusPending  ServerRequestStatus = "pending"
	ServerRequestStatusReplying ServerRequestStatus = "replying"
	ServerRequestStatusReplied  ServerRequestStatus = "replied"
	ServerRequestStatusResolved ServerRequestStatus = "resolved"
	ServerRequestStatusIgnored  ServerRequestStatus = "ignored"
)

type TaskServerRequest struct {
	RequestID      string
	TaskID         string
	ThreadID       string
	TurnID         string
	RequestType    ServerRequestType
	RequestPayload string
	Status         ServerRequestStatus
	DecisionSource string
	ReplyContent   string
	CreatedAt      time.Time
	ReplyStartedAt *time.Time
	RepliedAt      *time.Time
	ResolvedAt     *time.Time
}
```

- [ ] **Step 4: Replace the `tasks` schema and add the request table**

Update `internal/orchestrator/store.go`:

```go
`CREATE TABLE IF NOT EXISTS tasks (
	task_id TEXT PRIMARY KEY,
	template_id TEXT NOT NULL,
	repository_id TEXT NOT NULL,
	machine_id TEXT NOT NULL,
	status TEXT NOT NULL,
	user_request TEXT NOT NULL,
	created_by TEXT NOT NULL,
	remote_workdir TEXT NOT NULL,
	thread_id TEXT NOT NULL DEFAULT '',
	active_turn_id TEXT NOT NULL DEFAULT '',
	last_input TEXT NOT NULL,
	last_output_summary TEXT NOT NULL,
	last_decision_action TEXT NOT NULL DEFAULT '',
	pending_request_id TEXT NOT NULL DEFAULT '',
	completion_check_status TEXT NOT NULL DEFAULT 'not_started',
	completion_check_sent_at TEXT,
	completion_check_done_at TEXT,
	awaiting_question TEXT,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`

`CREATE TABLE IF NOT EXISTS task_server_requests (
	request_id TEXT PRIMARY KEY,
	task_id TEXT NOT NULL,
	thread_id TEXT NOT NULL,
	turn_id TEXT NOT NULL,
	request_type TEXT NOT NULL,
	request_payload TEXT NOT NULL,
	status TEXT NOT NULL,
	decision_source TEXT NOT NULL DEFAULT '',
	reply_content TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	reply_started_at TEXT,
	replied_at TEXT,
	resolved_at TEXT
)`
```

Add helpers:

```go
func (s *Store) UpsertTaskServerRequest(ctx context.Context, req TaskServerRequest) error
func (s *Store) GetTaskServerRequest(ctx context.Context, requestID string) (TaskServerRequest, error)
func (s *Store) ListOpenTaskServerRequests(ctx context.Context, taskID string) ([]TaskServerRequest, error)
func (s *Store) MarkTaskServerRequestReplying(ctx context.Context, requestID string, at time.Time) error
func (s *Store) MarkTaskServerRequestReplied(ctx context.Context, requestID, reply string, at time.Time) error
func (s *Store) MarkTaskServerRequestResolved(ctx context.Context, requestID string, at time.Time) error
func (s *Store) MarkTaskServerRequestIgnored(ctx context.Context, requestID string, at time.Time) error
```

Delete all `phase` / `workflow_stage` columns and all references to them instead of preserving compatibility.

- [ ] **Step 5: Run tests to verify they pass**

Run:

```bash
go test ./internal/orchestrator -run 'TestStorePersistsSupervisorControlFields|TestStorePersistsTaskServerRequestLifecycle|TestStoreCreatesTaskAndReloadsIt|TestStoreUpdatesTaskStatusAndSessionFields' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/types.go internal/orchestrator/store.go internal/orchestrator/store_test.go
git commit -m "refactor: simplify supervisor task state schema"
```

---

### Task 2: Surface Explicit App-Server Server Requests

**Files:**
- Modify: `internal/codexappserver/protocol.go`
- Modify: `internal/codexappserver/client.go`
- Modify: `internal/codexappserver/client_test.go`
- Modify: `internal/codexappserver/manager.go`
- Modify: `internal/codexappserver/manager_test.go`

- [ ] **Step 1: Write failing tests for server-request decoding**

Add to `internal/codexappserver/client_test.go`:

```go
func TestClientPublishesServerInitiatedRequestNotifications(t *testing.T) {
	t.Parallel()

	transport := &stubTransport{recvCh: make(chan recvResult, 2)}
	client := newTestClient(transport)
	defer client.Close()

	transport.recvCh <- recvResult{payload: mustJSON(t, rpcMessage{
		ID:     "srv-1",
		Method: "item/tool/requestUserInput",
		Params: map[string]any{"threadId": "thread-1", "turnId": "turn-1", "prompt": "Choose A or B"},
	})}

	select {
	case msg := <-client.Notifications():
		if msg.Method != "item/tool/requestUserInput" {
			t.Fatalf("Method = %q", msg.Method)
		}
		if msg.ID != "srv-1" {
			t.Fatalf("ID = %q", msg.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("server request notification was not published")
	}
}
```

Add to `internal/codexappserver/manager_test.go`:

```go
func TestManagerPreservesRequestNotificationAcrossReconnect(t *testing.T) {
	t.Parallel()

	first := newFakeClient()
	second := newFakeClient()
	manager := NewManager(ManagerOptions{
		DialClient: sequenceDialer(first, second),
	})

	machine := MachineRuntimeConfig{MachineID: "machine_a", WebSocketURL: "ws://machine-a:4317"}
	if _, err := manager.WatchTaskThread(context.Background(), machine, "thread-1"); err != nil {
		t.Fatalf("WatchTaskThread error: %v", err)
	}

	first.notifications <- rpcMessage{
		ID:     "srv-1",
		Method: "item/tool/requestUserInput",
		Params: mustJSON(t, map[string]any{"thread": map[string]any{"id": "thread-1"}}),
	}
	close(first.notifications)

	if _, err := manager.WatchTaskThread(context.Background(), machine, "thread-1"); err != nil {
		t.Fatalf("WatchTaskThread redial error: %v", err)
	}
	if len(second.resumeThreadIDs) != 1 || second.resumeThreadIDs[0] != "thread-1" {
		t.Fatalf("resumeThreadIDs = %#v", second.resumeThreadIDs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/codexappserver -run 'TestClientPublishesServerInitiatedRequestNotifications|TestManagerPreservesRequestNotificationAcrossReconnect' -count=1
```

Expected: FAIL because the runtime does not yet normalize or preserve explicit server request lifecycle semantics.

- [ ] **Step 3: Add normalized server-request payload types**

Extend `internal/codexappserver/protocol.go`:

```go
type ServerRequest struct {
	RequestID string
	Method    string
	ThreadID  string
	TurnID    string
	Prompt    string
	RawParams json.RawMessage
}

func DecodeServerRequest(msg rpcMessage) (ServerRequest, bool, error) {
	if msg.ID == "" || msg.Method == "" {
		return ServerRequest{}, false, nil
	}
	switch msg.Method {
	case "item/tool/requestUserInput", "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
	default:
		return ServerRequest{}, false, nil
	}
	params, ok := messageParams(msg)
	if !ok {
		return ServerRequest{}, false, nil
	}
	var payload struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Prompt   string `json:"prompt"`
		Thread   struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(params, &payload); err != nil {
		return ServerRequest{}, false, err
	}
	threadID := payload.ThreadID
	if threadID == "" {
		threadID = payload.Thread.ID
	}
	return ServerRequest{
		RequestID: msg.ID,
		Method:    msg.Method,
		ThreadID:  threadID,
		TurnID:    payload.TurnID,
		Prompt:    payload.Prompt,
		RawParams: params,
	}, true, nil
}
```

- [ ] **Step 4: Preserve explicit server requests in the manager**

Add to `internal/codexappserver/manager.go`:

```go
type ThreadEvent struct {
	Message       rpcMessage
	ServerRequest *ServerRequest
}
```

Extend `ThreadWatcher` to keep a buffered event channel:

```go
events chan ThreadEvent
```

and expose:

```go
func (w *ThreadWatcher) Events() <-chan ThreadEvent
```

When `consumeNotifications` sees a normalized server request, publish it to the matching watcher's event stream without mutating reply state inside `codexappserver`.

- [ ] **Step 5: Run tests to verify they pass**

Run:

```bash
go test ./internal/codexappserver -count=1
go test -race ./internal/codexappserver -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/codexappserver/protocol.go internal/codexappserver/client.go internal/codexappserver/client_test.go internal/codexappserver/manager.go internal/codexappserver/manager_test.go
git commit -m "feat: surface codex app-server server requests"
```

---

### Task 3: Replace Phase Logic With Supervisor Policy Gates

**Files:**
- Create: `internal/orchestrator/supervisor_policy.go`
- Create: `internal/orchestrator/supervisor_policy_test.go`
- Modify: `internal/orchestrator/decision.go`
- Modify: `internal/orchestrator/decision_test.go`

- [ ] **Step 1: Write failing policy tests**

Create `internal/orchestrator/supervisor_policy_test.go`:

```go
package orchestrator

import "testing"

func TestPolicyForbidsReplyWithoutExplicitRequest(t *testing.T) {
	t.Parallel()

	decision := SupervisorDecision{
		Classification:    ClassificationExecutionApproval,
		ShouldReplyCodex:  true,
		ReplyPolicy:       ReplyPolicyAutoContinue,
		CodexReply:        "continue",
	}

	if got := ApplySupervisorPolicy(TaskRun{}, nil, decision); got.AllowReply {
		t.Fatal("AllowReply = true, want false")
	}
}

func TestPolicyAllowsAutoReplyForPendingExecutionApproval(t *testing.T) {
	t.Parallel()

	task := TaskRun{PendingRequestID: "req-1"}
	req := &TaskServerRequest{
		RequestID:   "req-1",
		RequestType: ServerRequestTypeUserInput,
		Status:      ServerRequestStatusPending,
	}
	decision := SupervisorDecision{
		Classification:   ClassificationExecutionApproval,
		ShouldReplyCodex: true,
		ReplyPolicy:      ReplyPolicyAutoContinue,
		CodexReply:       "continue",
	}

	got := ApplySupervisorPolicy(task, req, decision)
	if !got.AllowReply {
		t.Fatal("AllowReply = false, want true")
	}
}
```

Add to `internal/orchestrator/decision_test.go`:

```go
func TestModelDecisionEngineParsesSupervisorClassificationSchema(t *testing.T) {
	t.Parallel()

	engine := NewModelDecisionEngine(&fakeDecisionModel{
		response: `{"classification":"execution_approval","should_reply_codex":true,"should_notify_user":false,"reply_policy":"auto_continue","codex_reply":"continue","reason":"routine execution resume"}`,
	})

	result, err := engine.ClassifySupervisorEvent(t.Context(), SupervisorContext{
		Task: TaskRun{TaskID: "task-1"},
		Request: TaskServerRequest{
			RequestID:   "req-1",
			RequestType: ServerRequestTypeUserInput,
			RequestPayload: `{"prompt":"continue?"}`,
		},
	})
	if err != nil {
		t.Fatalf("ClassifySupervisorEvent returned error: %v", err)
	}
	if result.Classification != ClassificationExecutionApproval {
		t.Fatalf("Classification = %q", result.Classification)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/orchestrator -run 'TestPolicyForbidsReplyWithoutExplicitRequest|TestPolicyAllowsAutoReplyForPendingExecutionApproval|TestModelDecisionEngineParsesSupervisorClassificationSchema' -count=1
```

Expected: FAIL because no supervisor policy types or classification entrypoint exist.

- [ ] **Step 3: Add structured classification types and hard gates**

Create `internal/orchestrator/supervisor_policy.go`:

```go
package orchestrator

type SupervisorClassification string
type ReplyPolicy string

const (
	ClassificationPlanDecision      SupervisorClassification = "plan_decision"
	ClassificationExecutionApproval SupervisorClassification = "execution_approval"
	ClassificationProgressUpdate    SupervisorClassification = "progress_update"
	ClassificationCompletionSignal  SupervisorClassification = "completion_signal"
	ClassificationIgnore            SupervisorClassification = "ignore"
)

const (
	ReplyPolicyNoReply      ReplyPolicy = "no_reply"
	ReplyPolicyAutoContinue ReplyPolicy = "auto_continue"
	ReplyPolicyAskUser      ReplyPolicy = "ask_user"
)

type SupervisorDecision struct {
	Classification   SupervisorClassification `json:"classification"`
	ShouldReplyCodex bool                     `json:"should_reply_codex"`
	ShouldNotifyUser bool                     `json:"should_notify_user"`
	ReplyPolicy      ReplyPolicy              `json:"reply_policy"`
	Reason           string                   `json:"reason"`
	UserUpdate       string                   `json:"user_update"`
	CodexReply       string                   `json:"codex_reply"`
}

type PolicyResult struct {
	AllowReply      bool
	EscalateToUser  bool
	NotifyUser      bool
	ReplyContent    string
}

func ApplySupervisorPolicy(task TaskRun, req *TaskServerRequest, decision SupervisorDecision) PolicyResult {
	if req == nil || task.PendingRequestID == "" || req.RequestID != task.PendingRequestID {
		return PolicyResult{}
	}
	if req.Status != ServerRequestStatusPending {
		return PolicyResult{}
	}
	switch decision.Classification {
	case ClassificationExecutionApproval:
		if decision.ShouldReplyCodex && decision.ReplyPolicy == ReplyPolicyAutoContinue {
			return PolicyResult{AllowReply: true, ReplyContent: decision.CodexReply}
		}
	case ClassificationPlanDecision:
		return PolicyResult{EscalateToUser: true}
	}
	if decision.ShouldNotifyUser {
		return PolicyResult{NotifyUser: true}
	}
	return PolicyResult{}
}
```

- [ ] **Step 4: Replace phase/stage prompt semantics with narrow supervisor schemas**

Refactor `internal/orchestrator/decision.go`:

```go
type SupervisorContext struct {
	Task    TaskRun
	Request TaskServerRequest
	Summary string
}

func (e *ModelDecisionEngine) ClassifySupervisorEvent(ctx context.Context, in SupervisorContext) (SupervisorDecision, error)
func (e *ModelDecisionEngine) EvaluateProgressUpdate(ctx context.Context, task TaskRun, summary string) (SupervisorDecision, error)
func (e *ModelDecisionEngine) EvaluateCompletionSignal(ctx context.Context, task TaskRun, summary string) (SupervisorDecision, error)
```

Delete prompt language that references `phase`, `workflowStage`, or asks the model to opportunistically drive Codex without an explicit request.

- [ ] **Step 5: Run tests to verify they pass**

Run:

```bash
go test ./internal/orchestrator -run 'TestPolicyForbidsReplyWithoutExplicitRequest|TestPolicyAllowsAutoReplyForPendingExecutionApproval|TestModelDecisionEngineParsesSupervisorClassificationSchema' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/supervisor_policy.go internal/orchestrator/supervisor_policy_test.go internal/orchestrator/decision.go internal/orchestrator/decision_test.go
git commit -m "feat: add codex supervisor policy gates"
```

---

### Task 4: Make Service Request-Driven And Completion-Check-Once

**Files:**
- Modify: `internal/orchestrator/service.go`
- Modify: `internal/orchestrator/service_test.go`
- Modify: `internal/orchestrator/appserver_runner.go`
- Modify: `internal/orchestrator/appserver_runner_test.go`
- Modify: `internal/orchestrator/scheduler.go`

- [ ] **Step 1: Write failing lifecycle tests**

Add to `internal/orchestrator/service_test.go`:

```go
func TestTickDoesNotReplyToCodexWithoutExplicitPendingRequest(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{
			Summary: "Need clarification about architecture",
			SessionState: SessionState{ThreadStatus: "running"},
		},
	}
	service, store := newTestService(t, runner, &fakeDecisionEngine{
		supervisorDecision: SupervisorDecision{
			Classification:   ClassificationExecutionApproval,
			ShouldReplyCodex: true,
			ReplyPolicy:      ReplyPolicyAutoContinue,
			CodexReply:       "continue",
		},
	})
	defer store.Close()

	task := sampleTaskRun("task-no-request", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	mustCreateTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 0 {
		t.Fatalf("sentInputs = %#v, want none", runner.sentInputs)
	}
}

func TestTickRepliesOnceForPendingExecutionApprovalRequest(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{Summary: "continue?", SessionState: SessionState{ThreadStatus: "running"}},
	}
	service, store := newTestService(t, runner, &fakeDecisionEngine{
		supervisorDecision: SupervisorDecision{
			Classification:   ClassificationExecutionApproval,
			ShouldReplyCodex: true,
			ReplyPolicy:      ReplyPolicyAutoContinue,
			CodexReply:       "continue",
		},
	})
	defer store.Close()

	task := sampleTaskRun("task-request", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	task.PendingRequestID = "req-1"
	mustCreateTask(t, store, task)
	mustUpsertRequest(t, store, TaskServerRequest{
		RequestID:   "req-1",
		TaskID:      task.TaskID,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		RequestType: ServerRequestTypeUserInput,
		Status:      ServerRequestStatusPending,
		CreatedAt:   task.CreatedAt,
	})

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 1 || runner.sentInputs[0] != "continue" {
		t.Fatalf("sentInputs = %#v", runner.sentInputs)
	}
	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("second TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 1 {
		t.Fatalf("sentInputs after second tick = %#v, want one reply", runner.sentInputs)
	}
}

func TestTickSendsCompletionCheckOnlyOnce(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{Summary: "Task complete.", SessionState: SessionState{ThreadStatus: "completed"}},
	}
	service, store := newTestService(t, runner, &fakeDecisionEngine{
		completionDecision: SupervisorDecision{
			Classification:   ClassificationCompletionSignal,
			ShouldReplyCodex: false,
		},
	})
	defer store.Close()

	task := sampleTaskRun("task-complete-once", StatusRunning)
	task.ThreadID = "thread-1"
	task.ActiveTurnID = "turn-1"
	mustCreateTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 1 {
		t.Fatalf("sentInputs = %#v, want one completion check", runner.sentInputs)
	}
	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("second TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 1 {
		t.Fatalf("sentInputs after second tick = %#v, want still one completion check", runner.sentInputs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/orchestrator -run 'TestTickDoesNotReplyToCodexWithoutExplicitPendingRequest|TestTickRepliesOnceForPendingExecutionApprovalRequest|TestTickSendsCompletionCheckOnlyOnce' -count=1
```

Expected: FAIL because service still uses summary-driven intervention and has no persisted request lifecycle handling.

- [ ] **Step 3: Replace summary-driven intervention with request-driven handling**

Refactor `internal/orchestrator/service.go`:

```go
func (s *Service) advanceRunningTask(ctx context.Context, task TaskRun) error {
	window, err := s.runner.CaptureOutput(ctx, sessionFromTask(task))
	if err != nil {
		...
	}
	if strings.TrimSpace(window.Summary) != "" {
		task.LastOutputSummary = window.Summary
	}

	req, err := s.loadPendingRequest(ctx, task)
	if err != nil {
		return err
	}
	if req != nil {
		decision, err := s.decider.ClassifySupervisorEvent(ctx, SupervisorContext{
			Task:    task,
			Request: *req,
			Summary: window.Summary,
		})
		if err != nil {
			return fmt.Errorf("classify supervisor request for task %q: %w", task.TaskID, err)
		}
		return s.handleSupervisorRequest(ctx, task, window, req, decision)
	}

	return s.handleProgressAndCompletionOnly(ctx, task, window)
}
```

Delete:

```go
shouldSkipDecisionForWorkingWindow
normalizeWorkflowStage
taskPhaseForWorkflowStage
enforcePhasePolicy
```

and all call sites instead of keeping compatibility wrappers.

- [ ] **Step 4: Implement completion-check-once and renamed statuses**

Add:

```go
const completionCheckPrompt = "Please verify once, against the confirmed task scope only, whether all requested work is complete. Reply with either: 1) all requested work is complete, or 2) remaining work still exists."
```

Implement:

```go
func (s *Service) maybeSendCompletionCheck(ctx context.Context, task TaskRun, summary string) (TaskRun, bool, error)
func (s *Service) handleSupervisorRequest(ctx context.Context, task TaskRun, window OutputWindow, req *TaskServerRequest, decision SupervisorDecision) error
func (s *Service) handleProgressAndCompletionOnly(ctx context.Context, task TaskRun, window OutputWindow) error
func (s *Service) loadPendingRequest(ctx context.Context, task TaskRun) (*TaskServerRequest, error)
```

Update task status transitions:

```go
StatusPending -> StatusStarting
StatusRecovering instead of StatusDetached
```

- [ ] **Step 5: Run focused tests to verify they pass**

Run:

```bash
go test ./internal/orchestrator -run 'TestTickDoesNotReplyToCodexWithoutExplicitPendingRequest|TestTickRepliesOnceForPendingExecutionApprovalRequest|TestTickSendsCompletionCheckOnlyOnce|TestRecoverDetachedTaskReconnectsByThreadID' -count=1
```

Expected: PASS after the reconnect test is renamed to the new `recovering` semantics.

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/service.go internal/orchestrator/service_test.go internal/orchestrator/appserver_runner.go internal/orchestrator/appserver_runner_test.go internal/orchestrator/scheduler.go
git commit -m "refactor: make codex supervision request-driven"
```

---

### Task 5: Add 2-Minute Progress Reporting And Full Cleanup

**Files:**
- Modify: `cmd/alterego/main.go`
- Modify: `internal/orchestrator/service.go`
- Modify: `internal/orchestrator/service_test.go`
- Modify: `internal/orchestrator/scheduler.go`
- Modify: `internal/orchestrator/runner_test.go`

- [ ] **Step 1: Write failing tests for progress polling and renamed state**

Add to `internal/orchestrator/service_test.go`:

```go
func TestTickProgressPollingOnlyNotifiesUserAndNeverRepliesCodex(t *testing.T) {
	t.Parallel()

	runner := &fakeServiceRunner{
		outputWindow: OutputWindow{Summary: "Completed migration and all tests passed.", SessionState: SessionState{ThreadStatus: "running"}},
	}
	notifier := &fakeTaskNotifier{}
	service, store := newTestServiceWithNotifier(t, runner, &fakeDecisionEngine{
		progressDecision: SupervisorDecision{
			Classification:   ClassificationProgressUpdate,
			ShouldNotifyUser: true,
			UserUpdate:       "Codex completed migration and passed tests.",
		},
	}, notifier)
	defer store.Close()

	task := sampleTaskRun("task-progress", StatusRunning)
	task.ThreadID = "thread-1"
	mustCreateTask(t, store, task)

	if err := service.TickOnce(context.Background()); err != nil {
		t.Fatalf("TickOnce returned error: %v", err)
	}
	if len(runner.sentInputs) != 0 {
		t.Fatalf("sentInputs = %#v, want none", runner.sentInputs)
	}
	if len(notifier.messages) != 1 {
		t.Fatalf("messages = %#v, want one user update", notifier.messages)
	}
}
```

Add to `cmd/alterego/main.go` expectations by asserting:

```go
const taskTickInterval = 2 * time.Minute
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/orchestrator ./cmd/alterego -run 'TestTickProgressPollingOnlyNotifiesUserAndNeverRepliesCodex|TestBuildTaskSubsystemBuildsService' -count=1
```

Expected: FAIL because the notifier/progress-only path and two-minute tick interval are not yet wired.

- [ ] **Step 3: Implement the progress-only polling path**

Add in `internal/orchestrator/service.go`:

```go
func (s *Service) maybeNotifyProgress(ctx context.Context, task TaskRun, summary string) error {
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	decision, err := s.decider.EvaluateProgressUpdate(ctx, task, summary)
	if err != nil {
		return err
	}
	if !decision.ShouldNotifyUser || strings.TrimSpace(decision.UserUpdate) == "" || s.notifier == nil {
		return nil
	}
	progressTask := task
	progressTask.LastOutputSummary = decision.UserUpdate
	return s.notifier.NotifyTaskQuestion(ctx, progressTask)
}
```

Keep this path separate from Codex replies.

- [ ] **Step 4: Change the polling interval and remove legacy status references**

Update `cmd/alterego/main.go`:

```go
const taskTickInterval = 2 * time.Minute
...
ticker := time.NewTicker(taskTickInterval)
```

Update `internal/orchestrator/scheduler.go` and tests so only the new statuses remain runnable/load-bearing.

- [ ] **Step 5: Run full targeted verification**

Run:

```bash
go test ./internal/codexappserver ./internal/orchestrator ./internal/agent ./cmd/alterego -count=1
go test -race ./internal/codexappserver ./internal/orchestrator ./cmd/alterego -count=1
go build ./cmd/alterego
```

Expected: PASS.

- [ ] **Step 6: Run repository-wide verification**

Run:

```bash
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/alterego/main.go internal/orchestrator internal/codexappserver
git commit -m "feat: enforce codex supervisor event policy"
```

---

## Spec Coverage Check

- Explicit server-request-driven replies: covered by Tasks 2, 3, and 4.
- No summary-based ask-user inference: covered by Task 4 deleting the old decision path.
- Simplified statuses with `starting` and `recovering`: covered by Tasks 1 and 4.
- Remove `phase` and `workflowStage`: covered by Task 1 and Task 4.
- Two-minute progress-only polling: covered by Task 5.
- Reply exactly once per request: covered by Tasks 1, 3, and 4.
- Completion check exactly once: covered by Tasks 1 and 4.
- No compatibility retention for removed fields/states: covered by Task 1 and Task 4 explicitly deleting the old schema and logic.

## Placeholder Scan

- Checked for `TODO`, `TBD`, `implement later`, `similar to Task`, and vague compatibility language.
- No compatibility-preserving steps remain; all removed fields and states are deleted directly.

## Type Consistency Check

- Request lifecycle names are consistent: `TaskServerRequest`, `ServerRequestStatus*`, `ServerRequestType*`.
- Completion-check names are consistent: `CompletionCheckStatus*`, `CompletionCheckSentAt`, `CompletionCheckDoneAt`.
- Supervisor schema names are consistent: `SupervisorDecision`, `SupervisorClassification`, `ReplyPolicy`.
