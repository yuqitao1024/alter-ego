# Codex App-Server Migration Design

Date: 2026-05-19

## Context

The current remote task orchestration design uses:

- SSH to reach remote machines;
- one long-lived `tmux` session per task;
- `tmux capture-pane` to infer Codex state;
- `tmux send-keys` to inject follow-up input.

That design is no longer acceptable as the primary path.

The failure mode is architectural, not incidental:

- terminal UI output is not a reliable state source;
- `Create a plan?` is both a workflow-semantic question and a TUI prompt, and the current system cannot separate those concerns reliably;
- stale or reflowed terminal text causes false state transitions;
- deterministic key injection and model arbitration end up fighting the live Codex UI;
- recovery through `tmux` is fragile and difficult to reason about.

The system should migrate to `codex app-server` as the session and state source.

This migration should preserve the current operator-facing control plane:

- Lark command entrypoints;
- SQLite task persistence;
- repository/template/workflow model;
- scheduler and task lifecycle;
- packaging and deployment layout.

The main replacement is the remote session transport and observation model.

## Decision

Replace the `tmux`-backed remote session path with an app-server-backed path:

- each remote machine hosts a long-lived Codex app-server process;
- Alter Ego connects to that app-server using the structured protocol instead of scraping terminal output;
- task lifecycle is driven by thread, turn, item, and status notifications from app-server;
- SSH remains only for remote bootstrap, deployment, and process supervision;
- `tmux` is frozen immediately, not used for new task execution, and removed after the app-server path is verified.

This is a migration of the execution substrate, not a redesign of the operator workflow.

## Goals

- Remove `tmux capture-pane` and `tmux send-keys` from the remote task critical path.
- Use structured app-server events as the primary task state source.
- Preserve the current Lark `/task` control model.
- Preserve SQLite persistence and task recovery semantics.
- Preserve `superpowers`-style planning and execution behavior without forcing Codex through TUI-specific prompts.
- Allow recovery across Alter Ego restarts by reconnecting to remote app-server threads.

## Non-Goals

- Replacing Lark with another operator channel.
- Eliminating SSH entirely in the first migration.
- Building a GUI or web dashboard.
- Supporting both `tmux` and app-server as equal first-class long-term backends.
- Deleting all `tmux` code in the same change that introduces app-server.

## Why App-Server

`codex app-server` exposes structured protocol objects and notifications for:

- thread start and thread status changes;
- turn start and turn completion;
- agent messages and plan items;
- command execution items and output deltas;
- user input injection through turn start or turn steer requests;
- explicit server requests such as user-input requests and approval requests.

This gives the orchestrator a state source that is:

- semantic rather than screen-based;
- resilient to TUI reflow and terminal rendering;
- compatible with phase-aware workflow policy;
- more recoverable after local process restart.

## Alternatives Considered

### 1. Keep `tmux` and keep patching responders

Rejected.

The problem is not only prompt coverage. The system is trying to infer workflow state from terminal rendering, which is the wrong abstraction boundary.

### 2. Revert to `codex exec/resume`

Rejected as the primary path.

It loses too much interactive session fidelity for `superpowers`-style requirement discussion and staged planning.

### 3. Migrate to app-server while preserving the current control plane

Accepted.

This preserves the stable parts of the current system and replaces the unstable session substrate.

## High-Level Architecture

The feature keeps the current outer layers and swaps the session adapter.

### Preserved Layers

- Lark command gateway
- repository/template/workflow registry
- SQLite task persistence
- scheduler
- notifier
- packaging and deployment skeleton

### Replaced Layer

- `tmux` runner
- terminal responders for TUI-specific prompts
- screen-digest-driven task interpretation

### New Layer

- remote app-server supervisor and client adapter

## Remote Execution Model

Each remote machine runs a single long-lived Codex app-server process.

Alter Ego does not start a separate `codex` TUI process per task. Instead, it:

1. ensures the remote app-server is running;
2. connects to the remote app-server control endpoint;
3. creates or resumes a Codex thread for a task;
4. starts turns within that thread;
5. injects user replies through structured turn input;
6. observes thread and turn notifications to determine current task state.

## Transport Model

The first app-server version should still use SSH for remote reachability and process management.

Recommended first migration model:

- the app-server listens on a remote Unix domain socket;
- Alter Ego reaches it through an SSH process that proxies bytes between local stdio and the remote socket;
- Alter Ego uses `codex app-server proxy --sock <remote-socket>` on the remote host rather than opening raw `tmux` sessions.

This keeps the deployment assumptions close to the current environment:

- no extra public ports need to be exposed;
- no separate service discovery is required;
- existing machine definitions remain usable with small extensions.

Later, this can evolve to a direct websocket model if desired, but that is not required for the first migration.

## Session Model

The task should no longer treat a terminal session as the unit of orchestration.

The unit becomes an app-server thread.

Each task persists:

- `thread_id`
- `active_turn_id` when present
- `last_observed_thread_status`
- `last_observed_turn_status`
- `last_observed_item_id`
- `last_meaningful_summary`
- `last_remote_activity_at`

The existing `tmux_session_name` becomes obsolete and should be replaced after migration.

`remote_codex_session_id` can remain temporarily during migration if needed, but should not be the primary state anchor.

## Task Phase Model

The current `planning` vs `executing` phase boundary remains, but the transition source changes.

The phase must no longer be inferred from:

- terminal prompt text;
- generic keyword matches in injected replies.

Instead, phase progression should be derived from structured app-server context plus workflow-aware model arbitration.

The orchestrator should track a finer workflow stage:

- `requirement_discussion`
- `spec_writing`
- `spec_review`
- `plan_writing`
- `plan_review`
- `implementation`
- `verification`
- `integration`

Mapping:

- `requirement_discussion`, `spec_writing`, `spec_review`, `plan_writing`, `plan_review` => `planning`
- `implementation`, `verification`, `integration` => `executing`

Hard policy:

- `planning -> executing` is allowed when the current workflow stage enters implementation, verification, or integration.
- `executing -> planning` is never automatic.
- If execution needs to reopen planning, the task must transition through `waiting_user_input` and require explicit operator approval in Lark.

## Decision Model

The current deterministic terminal responders must be split into two classes.

### Keep as Deterministic Infrastructure Responders

These remain rule-based because they are environment-level concerns:

- login required
- usage limit
- transport / session unavailable

### Remove as TUI Responders

These should no longer be driven by screen matching:

- `Create a plan?`
- `Esc dismiss`
- follow-up continuation spam

Those concerns are workflow-semantic and should be handled by model arbitration against structured thread state, not terminal UI.

## Model Arbitration Contract

The app-server path should continue to use mandatory model arbitration for non-deterministic workflow decisions.

The arbitrator output should remain structured, but should now include a finer workflow stage:

```json
{
  "action": "wait | ask_user | reply_to_codex | complete_task",
  "decision_type": "none | requirement_clarification | scope_confirmation | implementation_solution_choice | missing_context | progress_blocked",
  "summary": "string",
  "user_question": "string",
  "codex_reply": "string",
  "next_phase": "planning | executing",
  "workflow_stage": "requirement_discussion | spec_writing | spec_review | plan_writing | plan_review | implementation | verification | integration"
}
```

The orchestrator applies hard policy on top:

- workflow stage determines phase mapping;
- execution cannot automatically reopen planning;
- non-deterministic state changes require a model result, not heuristics.

## App-Server Event Handling

The orchestrator should consume app-server notifications and derive task state from them.

The first version only needs a small subset:

- `thread/started`
- `thread/status/changed`
- `turn/started`
- `turn/completed`
- `item/started`
- `item/completed`
- `item/agentMessage/delta`
- `item/plan/delta`
- `item/commandExecution/outputDelta`
- `serverRequest/resolved`

The implementation should aggregate these notifications into:

- operator-facing summary text;
- current workflow stage candidates;
- whether Codex is still actively progressing;
- whether the thread is blocked and waiting for external input;
- whether the task appears completed.

## Human Escalation

Human escalation remains unchanged at the product level:

- Alter Ego asks in Lark when it truly needs operator input.

But the trigger source changes:

- from TUI text heuristics
- to app-server thread state plus workflow-aware arbitration.

This should reduce false escalations caused by TUI prompts that are not actually semantic blockers.

## Persistence Changes

SQLite should evolve from terminal-session metadata to app-server metadata.

Add:

- `thread_id`
- `active_turn_id`
- `workflow_stage`
- `last_thread_status`
- `last_turn_status`
- `last_remote_activity_at`

Deprecate:

- `tmux_session_name`
- `last_screen_digest`
- TUI-responder cooldown fields that only exist to control screen-scraping loops

During migration, both sets of fields may coexist temporarily, but all new tasks should use the app-server-backed fields.

## Recovery Model

Recovery is no longer:

- check `tmux has-session`
- scrape screen
- decide whether to `codex resume --last`

Recovery becomes:

1. reconnect to the remote app-server endpoint;
2. verify that the task's `thread_id` still exists or can be listed;
3. inspect the current thread and active turn state;
4. if there is an active blocked turn, either:
   - inject the pending user reply;
   - or ask the user again if the thread needs clarification;
5. if the thread is complete, mark the task `completed`;
6. if the thread cannot be found, move the task to `failed` or `detached`, depending on recoverability.

## Deployment Model

The current packaging layout can remain:

- `/opt/alterego/bin/alterego`
- `/opt/alterego/config/...`
- `/etc/alterego/alterego.env`
- `alteregod.service`

What changes is the remote-machine requirement.

Instead of requiring `tmux`, remote machines must support:

- `codex app-server` or `codex remote-control`
- a stable runtime environment for the app-server socket

The deployment artifacts should later add example configuration for:

- remote socket path
- remote app-server bootstrap command
- remote app-server health checks

## Migration Strategy

Do not hard-revert the entire `tmux` implementation first.

Recommended sequence:

1. freeze `tmux` as the old backend;
2. stop using it for new tasks once the app-server path is ready;
3. add an app-server-backed runner behind the same service interface;
4. validate a single-task end-to-end flow;
5. validate user clarification and completion;
6. validate restart recovery;
7. only then remove the old `tmux` runner and its docs.

## Validation Plan

The app-server migration is complete only when all of these work:

1. start a task from Lark;
2. write spec and plan inside the same task without false `executing` transition;
3. transition into implementation after plan approval;
4. run tests/build/commit/push/PR tasks without TUI-prompt loops;
5. ask the user for clarification through Lark when needed;
6. survive Alter Ego restart and reconnect to the live remote thread;
7. mark completed tasks correctly from structured app-server state.

## Consequences

Positive:

- removes dependence on screen scraping;
- separates workflow semantics from terminal UI;
- makes `superpowers` planning stages first-class again;
- gives a real recovery path based on thread identity instead of `tmux` survivability.

Negative:

- introduces app-server protocol integration complexity;
- requires a new remote bootstrap and connection model;
- requires migration work in persistence, runner, and decision layers.

This tradeoff is acceptable because the current `tmux` approach has already shown that it cannot reliably preserve the required workflow semantics.
