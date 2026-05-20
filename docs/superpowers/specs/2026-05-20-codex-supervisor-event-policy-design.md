# Codex Supervisor Event Policy Design

Date: 2026-05-20

## Context

Alter Ego has already moved task runtime onto Codex app-server websocket subscriptions, but task supervision still carries two behaviors from the earlier polling-oriented design:

- task control still relies on periodic model evaluation of `summary` text to decide whether Codex needs help;
- task lifecycle still stores phase-oriented orchestration concepts (`phase`, `workflowStage`) that do not map cleanly to Codex protocol semantics, user-authored workflow markdown stages, or Superpowers planning stages.

This creates three practical problems:

- Alter Ego can interfere with Codex even when Codex has not explicitly asked for input;
- task stage semantics are ambiguous and can drift, for example when plan-writing is misclassified as execution;
- restart and resume safety is underspecified for reply-once behavior and task-completion verification.

The target model is a narrower supervisory role:

- Codex app-server is the source of truth for when client action is required;
- Alter Ego updates state continuously from subscriptions and only replies to Codex when the protocol explicitly asks for a reply;
- model judgment is used for semantic classification and user-facing communication, not for inventing new Codex inputs;
- every replyable app-server request is persisted and handled exactly once;
- every task completion verification prompt is sent at most once.

## Protocol Facts

This design depends on official Codex app-server behavior:

- app-server can issue server-initiated requests such as `item/tool/requestUserInput`, `item/commandExecution/requestApproval`, and `item/fileChange/requestApproval`;
- app-server emits `serverRequest/resolved` after a server request is resolved or cleared;
- `thread/resume` is the recovery entry point after reconnect or supervisor restart.

These protocol features are sufficient to remove summary-based guesswork for deciding when Codex needs an answer.

## Decision

Alter Ego will switch from summary-driven intervention to explicit request-driven supervision.

The new policy is:

- if Codex app-server has not issued an explicit server request, Alter Ego must never send an unsolicited reply to Codex;
- if Codex app-server has issued an explicit server request, Alter Ego may classify it, persist it, decide whether it can be auto-handled, and either reply once or escalate once;
- periodic polling remains only for progress reporting and task liveness backstop, not for input-generation decisions.

Task lifecycle will also be simplified:

- remove `phase`;
- remove `workflowStage`;
- rename `detached` to `recovering`;
- merge `preparing_workspace` and `starting_session` into `starting`.

The resulting task state machine is:

- `pending`
- `starting`
- `running`
- `waiting_user_input`
- `recovering`
- `completed`
- `failed`
- `stopped`

## Goals

- Make Codex app-server explicit server requests the only trigger for replying to Codex.
- Remove summary-based "does Codex need input?" inference.
- Persist request-processing lifecycle so the same app-server request is answered at most once.
- Persist task-completion verification state so Codex is asked to verify completion at most once.
- Keep a light polling loop only for progress reporting and recovery backstop.
- Let the model perform semantic classification within a strict structured-output contract.

## Non-Goals

- Reconstructing or preserving Superpowers stage names in task persistence.
- Mapping user-authored workflow markdown stages into fixed stored lifecycle states.
- Letting Alter Ego autonomously generate new work directions when Codex has not asked for input.
- Making task completion entirely event-driven without a liveness backstop.

## Supervisor Boundary

Alter Ego is a supervisor, not a co-worker embedded in the turn loop.

Allowed Codex-facing actions:

- answer an explicit app-server request;
- send one completion-verification prompt after Codex indicates task completion;
- interrupt or stop tasks when explicitly requested by operators.

Forbidden Codex-facing actions:

- sending any input when no explicit request is pending;
- repeatedly re-answering the same app-server request;
- repeatedly re-running completion verification after it has already been attempted once.

## Event Model

The runtime event layer must distinguish between two categories.

### 1. Informational Subscription Events

Examples:

- thread status changes;
- turn status changes;
- agent message deltas;
- plan and command output summaries.

These events update in-memory thread snapshots and persisted task summaries only.

They do not authorize Codex replies.

### 2. Actionable Server Requests

Examples:

- `item/tool/requestUserInput`
- `item/commandExecution/requestApproval`
- `item/fileChange/requestApproval`

These events create persisted request records and move a task into a request-handling flow.

The actionable unit is the server request identity, not the summary text.

## Model Responsibilities

Semantic interpretation should be handled by the model through structured outputs.

Required model judgments:

- classify a server request as plan-decision-like or execution-approval-like;
- decide whether a request should be auto-handled or escalated to the user;
- decide whether a two-minute snapshot change is large enough to notify the user;
- decide whether Codex has clearly signaled task completion;
- decide whether Codex's response to the completion check means "all done" or "still remaining work".

The model must not be allowed to improvise authority. All model outputs are advisory inside hard-coded policy gates.

## Hard Policy Gates

The following rules are enforced without model discretion.

### No Unsolicited Codex Input

If there is no persisted unresolved app-server server request for the task, Alter Ego must not send any Codex reply.

### Reply Exactly Once Per Server Request

For a given app-server `request_id`:

- if already `replied`, `resolved`, or `ignored`, no second reply may be sent;
- if `pending` or `replying`, recovery logic may resume work but may not duplicate a sent reply;
- `serverRequest/resolved` closes the lifecycle regardless of whether the resolution came from Alter Ego, Codex, or protocol cleanup.

### Completion Verification Exactly Once

For a given task:

- once the completion verification prompt has been sent, it must never be sent again;
- after completion verification reaches either "confirmed done" or "remaining work reported", no further automatic completion nudges are allowed.

### Structured Output Only

If a model response does not conform to the expected schema, Alter Ego must not execute the suggested Codex reply.

## Request Classification Policy

The model will classify each explicit server request under a strict schema, but execution policy is fixed.

### Plan-Decision-Like Requests

Examples:

- scope decisions;
- architecture trade-offs;
- prioritization decisions;
- requests that materially change agreed work.

Default behavior:

- prefer user escalation through Feishu;
- do not auto-reply unless policy explicitly allows a safe default.

### Execution-Approval-Like Requests

Examples:

- "continue", "resume", "proceed", or similar execution confirmations;
- routine execution-stage approvals that do not alter agreed scope.

Default behavior:

- allow model-guided auto-reply;
- do not escalate unless the model indicates uncertainty or the request implies scope change.

The distinction is semantic and should not be encoded by brittle keyword lists.

## Progress Reporting Loop

Polling remains as a separate concern from request handling.

Policy:

- run every two minutes instead of every ten seconds;
- read the latest persisted or snapshot summary;
- ask the model only whether progress has materially advanced enough to notify the user;
- never let this loop send input to Codex.

This keeps users informed without turning progress polling into a control path.

## Completion Verification Flow

When Codex clearly signals task completion:

1. if completion verification has not been sent yet, Alter Ego sends one fixed verification prompt asking Codex to confirm whether the full task scope is complete;
2. Alter Ego marks completion verification as sent before waiting for the answer;
3. if Codex confirms completion, the task is closed;
4. if Codex reports remaining work, Alter Ego asks the user through Feishu whether to continue;
5. regardless of outcome, the verification prompt is never sent again.

This prevents endless "please double-check" loops.

## Persistence Model

Two persistence layers are needed.

### Task Table Additions

Store only supervisory control state on the task row:

- `status`
- `thread_id`
- `active_turn_id`
- `last_output_summary`
- `pending_request_id`
- `completion_check_status`
- `completion_check_sent_at`
- `completion_check_completed_at`

`phase` and `workflowStage` should be removed from persistence.

### Server Request Table

Store one row per explicit app-server request:

- `request_id`
- `task_id`
- `thread_id`
- `turn_id`
- `request_type`
- `request_payload`
- `status`
- `decision_source`
- `reply_content`
- `created_at`
- `reply_started_at`
- `replied_at`
- `resolved_at`

This table is the source of truth for exactly-once request handling.

## Recovery Semantics

Recovery must be restart-safe and resume-safe.

On Alter Ego restart:

1. reconnect machine websocket sessions;
2. call `thread/resume` for active task threads;
3. restore snapshots from resumed events;
4. load unresolved persisted server requests;
5. continue only unfinished request lifecycles;
6. never re-send a reply whose request row is already marked `replied`, `resolved`, or `ignored`.

Repeated delivery of old events is acceptable as long as persisted request state absorbs duplicates idempotently.

## User Escalation Policy

Feishu escalation should be used when:

- the request is classified as a plan decision;
- Codex reports remaining work after the single completion verification prompt;
- model classification fails schema validation or policy validation.

Feishu escalation should be avoided when:

- Codex is only asking for routine execution continuation;
- progress reporting would be pure noise;
- the task is already in a resolved completion-check terminal state.

## Consequences

Positive:

- far less unwanted interference with Codex;
- stage semantics become simpler and less brittle;
- restart safety improves substantially through persisted request lifecycles;
- user notifications become intentional instead of reactive noise.

Trade-offs:

- store schema grows to include request lifecycle state;
- request-handling code becomes more explicit and stateful;
- summary-based orchestration shortcuts must be removed and replaced with protocol-aware logic.

## Acceptance Criteria

- Alter Ego never replies to Codex without an explicit unresolved app-server request or the single completion-verification prompt.
- A repeated or replayed app-server request with the same `request_id` is never replied to twice.
- Completion verification is never sent more than once per task.
- Progress polling cannot send Codex replies.
- `phase` and `workflowStage` no longer exist in task persistence.
- `recovering` replaces `detached`.
- `starting` replaces separate workspace/session startup states.
