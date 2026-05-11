# Remote Codex Task Orchestrator Design

Date: 2026-05-11

## Context

Alter Ego already has:

- a working Lark transport layer;
- a hybrid command/chat agent handler;
- short-lived in-memory session state for conversational use;
- a real operator-facing bot flow in Lark.

The next capability is operational rather than conversational: the bot should manage development tasks executed by remote `codex` CLI sessions over SSH. The user does not need full terminal access, but the bot must be able to conduct the full remote CLI interaction on the user's behalf.

The operating constraints established during design are:

- only two remote machines need to be supported in the first version;
- one machine may run multiple Codex tasks concurrently;
- task distribution should be balanced across the two machines;
- only one class of human escalation is required in the first version: implementation-solution choice;
- all other ordinary execution steps should continue automatically;
- tasks and remote Codex session identities must be persisted locally so work can continue after local restart or SSH disconnect;
- restoring a task must prefer reusing a still-running remote Codex process, and only fall back to `codex resume <session_id>` after confirming the old process is gone.

## Decision

Implement a repository-scoped remote task orchestration subsystem with these properties:

- tasks are started and managed through explicit Lark commands;
- repositories define execution locations through bound machine pools;
- templates define workflow behavior and are bound one-to-one with workflow documents;
- the orchestrator persists task state in SQLite;
- remote task execution is session-aware and can resume an existing Codex session by remote session ID;
- multiple tasks are advanced through a round-robin scheduler;
- only implementation-solution-choice nodes pause for a Lark reply from the user.

This is a state-machine-driven system with model assistance, not a pure conversational workflow.

## Goals

- Start remote Codex tasks from Lark commands.
- Support multiple concurrent tasks across two machines.
- Keep task distribution balanced across repository-bound machine pools.
- Bind each task template to one repository and one workflow document.
- Persist remote task state locally.
- Recover remote tasks after local process restart or transient SSH disconnect.
- Reuse a live remote Codex process when possible instead of spawning a duplicate.
- Ask the user in Lark only when the task reaches an implementation-solution-choice decision point.

## Non-Goals

- Free-form natural language task creation in the first version.
- A general-purpose web console.
- Multi-user authorization and tenant isolation.
- Non-SSH execution transports.
- Automatic recovery of arbitrary shell processes outside Codex session semantics.
- Rich terminal mirroring into Lark.
- Automatic human escalation for categories other than implementation-solution choice.

## High-Level Architecture

The feature is organized into six subsystems.

### 1. Lark Command Gateway

Consumes explicit task-management commands from Lark and returns structured status back to the same user conversation.

First-version commands:

- `/task start <template> <requirement text>`
- `/task list`
- `/task status <task-id>`
- `/task reply <task-id> <decision text>`
- `/task stop <task-id>`

### 2. Template Registry

Loads repository and template configuration from the repo.

Proposed directories:

- `configs/machines/*.yaml`
- `configs/repositories/*.yaml`
- `configs/templates/*.yaml`
- `docs/workflows/*.md`

Relationship model:

- a repository defines remote execution scope, including machine pool;
- a template binds to exactly one repository;
- a template binds to exactly one workflow document;
- templates do not define machine pools directly.

### 3. Task Orchestrator

Owns task lifecycle, persistence, scheduling, recovery, and human-escalation state.

This layer is deterministic and stateful. It is not delegated to the LLM.

### 4. Remote Codex Runner

Connects to remote machines over SSH and manages `codex` CLI sessions in a controlled way:

- start new session;
- inspect existing session/process state;
- attach to a still-running session;
- resume an ended session by Codex session ID;
- send task instructions;
- collect output windows;
- stop a task.

### 5. Decision Layer

Uses the repository/template workflow document plus runtime context to:

- generate the next instruction to the remote Codex session;
- summarize remote progress for status reporting;
- decide whether the remote session has reached an implementation-solution-choice node.

The model may suggest escalation, but the orchestrator owns the explicit transition into `waiting_user_decision`.

### 6. Persistence Layer

Stores task state in SQLite so the orchestrator can recover task control after local restart.

## Configuration Model

### MachineConfig

Represents one SSH target.

Suggested fields:

- `id`
- `display_name`
- `host`
- `port`
- `user`
- `auth` reference or credential lookup key
- optional SSH options needed by the runner

### RepositoryConfig

Represents a repository plus its execution boundary and task-scoped checkout rules.

Suggested fields:

- `id`
- `display_name`
- `remote_repo_url`
- `remote_workspace_root`
- `default_branch`
- `machine_ids`
- `pre_clone_bootstrap`
- `post_clone_bootstrap`

`machine_ids` is authoritative. It defines where tasks for this repository may run.

`pre_clone_bootstrap` runs after the remote task directory is created but before the repository is cloned. This is intended for environment preparation needed to make code download succeed.

`post_clone_bootstrap` runs after clone and branch checkout. This is intended for repository-local setup such as dependency installation or submodule initialization.

### TemplateConfig

Represents one task mode for one repository.

Suggested fields:

- `id`
- `repository_id`
- `display_name`
- `description`
- `workflow_path`

Templates do not define `machine_ids`. They inherit execution scope from the bound repository.

## Machine Selection

When a task is created:

1. resolve the template;
2. resolve the bound repository;
3. obtain the repository's `machine_ids`;
4. choose the machine with the fewest active tasks;
5. break ties by config order.

This matches the user's requirement that tasks should be distributed across the two machines rather than selected arbitrarily.

## Task State Model

### TaskRun

Each task instance persists at least:

- `task_id`
- `template_id`
- `repository_id`
- `machine_id`
- `status`
- `user_request`
- `created_by`
- `remote_workdir`
- `remote_codex_session_id`
- `remote_process_identity` when discoverable
- `last_input`
- `last_output_summary`
- `awaiting_question`
- `created_at`
- `updated_at`

### AwaitingQuestion

Only needed when the task reaches an implementation-solution-choice node.

Suggested fields:

- `question_text`
- `options_summary`
- `context_excerpt`
- `asked_at`
- `answered_at`

## Task Lifecycle

Suggested states:

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

Representative transitions:

- `pending -> starting`
- `starting -> running`
- `running -> waiting_user_decision`
- `waiting_user_decision -> running`
- `running -> detached`
- `detached -> probing`
- `probing -> attaching`
- `probing -> resuming`
- `attaching -> running`
- `resuming -> running`
- `running -> completed`
- `starting/running/probing/attaching/resuming -> failed`
- `pending/starting/running/waiting_user_decision/detached -> stopped`

## Remote Startup Model

Task startup is deterministic code, not workflow prose.

For a new task, the runner should:

1. select the target machine;
2. create a task-scoped remote directory under `remote_workspace_root`, for example `<workspace_root>/<task-id>`;
3. run `pre_clone_bootstrap` inside that task directory;
4. clone `remote_repo_url` into a repository subdirectory inside the task directory;
5. checkout `default_branch` (or repository override when added later);
6. run `post_clone_bootstrap` inside the cloned repository;
7. start `codex` inside the cloned repository directory.

The workflow document does not own clone or environment setup. It only guides how the development task should proceed once the repository is ready.

## Scheduling Model

The orchestrator maintains a round-robin queue of active tasks.

Scheduling rules:

- tasks in `running`, `detached`, or recovery-related states remain eligible for scheduling;
- tasks in `waiting_user_decision` are skipped until the user replies;
- each scheduling turn gives one task one bounded unit of progress:
  - observe remote output;
  - interpret task state;
  - decide whether to continue, escalate, recover, or finish;
  - optionally send one next instruction.

This keeps long-running tasks from monopolizing the system and matches the user's requirement for multi-task rotation.

## Remote Codex Session Model

The runner must support two top-level entry paths:

- `StartNewSession`
- `RecoverSession`

`RecoverSession` is not a blind resume. It must follow this strict order:

1. connect to the machine over SSH;
2. inspect whether the old Codex-backed process/session is still alive for the stored task;
3. if still alive, attach or continue that live remote session;
4. only if it is confirmed dead, run the Codex session resume flow using the persisted `remote_codex_session_id`.

This rule prevents duplicate execution branches for the same task.

### Recovery Inputs

The recovery operation uses:

- `machine_id`
- `remote_workdir`
- `remote_codex_session_id`

No attempt is made to recreate the original SSH transport state. The only stable remote identity is the Codex session.

## Persistence

SQLite is the first-version persistence backend.

Reasons:

- task state is more structured than a simple append-only log;
- recovery, filtering, and lifecycle transitions need indexed queries;
- multiple task states, questions, machine assignments, and timestamps fit naturally in relational storage;
- JSON files would become brittle quickly once recovery logic is added.

Suggested tables:

- `machines` if runtime caching of machine config is needed
- `repositories`
- `templates`
- `tasks`
- `task_events`
- `task_questions`

`task_events` should capture operator-auditable state changes such as:

- task created
- machine selected
- session started
- live session attached
- session resumed
- user decision requested
- user decision applied
- task completed
- task failed

## Workflow Document Injection

Each template binds to one workflow document under `docs/workflows/...`.

The decision model context should contain three ordered layers:

1. fixed system rules
2. template-specific workflow document
3. runtime task context

### Fixed System Rules

These rules stay constant:

- the coordinator is managing remote Codex development sessions;
- it should continue automatically unless the task reaches an implementation-solution-choice node;
- it should summarize progress concisely;
- it should not invent task state outside the orchestrator's known state.

### Template Workflow Document

This document defines repository-specific operating behavior, for example:

- preferred development sequence;
- validation expectations;
- summary style;
- signals that count as implementation-solution choice;
- repository-specific coding norms.

### Runtime Context

Runtime context includes:

- user request;
- task status;
- repository and template identity;
- recent output summary;
- latest raw output window when needed;
- last command sent to Codex;
- whether a question is currently pending.

## Human Escalation Policy

Only one category pauses for user input in the first version:

- implementation-solution choice

Everything else should proceed automatically, including:

- ordinary iteration;
- test execution;
- code edits;
- dependency installation;
- commit/push behavior, if later enabled by workflow policy.

The orchestrator must encode this policy explicitly. It should not rely solely on prompt wording.

## Lark Command Semantics

### `/task start <template> <requirement text>`

- validate template existence;
- resolve repository;
- choose machine;
- create persisted `TaskRun`;
- schedule remote session start;
- return task ID, repository, and chosen machine.

### `/task list`

Returns active tasks with:

- task ID
- template
- repository
- machine
- status
- recent summary

### `/task status <task-id>`

Returns detailed state:

- task metadata
- machine
- current state
- remote session identity
- latest summary
- pending question if any

### `/task reply <task-id> <decision text>`

- only valid when the task is in `waiting_user_decision`;
- persists the user's decision;
- transitions the task back to `running`;
- enqueues the task for the next scheduling turn.

### `/task stop <task-id>`

- marks the task as stopping/stopped;
- attempts remote cancellation or termination if applicable;
- persists final state.

## Error Handling

The first version should handle at least:

- SSH connection failure;
- repository path missing on remote machine;
- Codex session start failure;
- live-session probe failure;
- resume failure;
- malformed or unrecognized remote output;
- SQLite open or write failure;
- duplicate Lark delivery, already handled by the current message deduper.

Failures should move tasks into explicit `failed` state with a concise operator-facing reason stored in the task record.

## Testing

Tests should cover:

- repository/template config parsing and binding;
- machine selection balancing across two machines;
- task state transitions;
- SQLite persistence and reload;
- recovery decision order:
  - attach live process first
  - resume only after confirmed exit
- round-robin scheduling behavior;
- Lark command parsing and task operation routing;
- decision-layer gating for `waiting_user_decision`.

The implementation should isolate remote execution behind interfaces so SSH and Codex interactions can be tested with fakes.

## Future Work

- natural-language task creation that compiles down into `/task start`;
- richer escalation categories;
- repository health and capacity scoring;
- web dashboard for task inspection;
- cross-process leader election if the orchestrator ever runs in more than one local instance;
- remote output snapshots and artifact collection.
