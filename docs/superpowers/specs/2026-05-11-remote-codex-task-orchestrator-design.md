# Remote Codex Task Orchestrator Design

Date: 2026-05-11

## Context

Alter Ego already has:

- a working Lark transport layer;
- a hybrid command/chat agent handler;
- a real operator-facing bot flow in Lark.

The next capability is operational rather than conversational: the bot should manage development tasks executed by remote `codex` CLI sessions over SSH.

The user does not need full terminal access, but the bot must be able to carry a full remote Codex conversation on the user's behalf, including:

- requirement clarification;
- scope confirmation;
- solution discussion;
- implementation follow-through;
- recovery after local restart.

The earlier `exec/resume` design is no longer the primary path. It does not preserve the interactive behavior needed for `superpowers`-style requirement discussion. The system should instead use long-lived remote interactive sessions backed by `tmux`.

## Decision

Implement a repository-scoped remote task orchestration subsystem with these properties:

- tasks are started and managed through explicit Lark commands;
- repositories define execution locations through bound machine pools;
- templates define workflow behavior and are bound one-to-one with workflow documents;
- the orchestrator persists task state in SQLite;
- each remote task owns one `tmux` session on one remote machine;
- `codex` runs inside that `tmux` session;
- the orchestrator reads remote output with `tmux capture-pane` and sends input with `tmux send-keys`;
- multiple tasks are advanced through a round-robin scheduler;
- user escalation happens when Codex asks for missing information, scope confirmation, or solution choice.

This is a state-machine-driven system with model assistance, not a pure conversational workflow.

## Goals

- Start remote Codex tasks from Lark commands.
- Support multiple concurrent tasks across two machines.
- Keep task distribution balanced across repository-bound machine pools.
- Bind each task template to one repository and one workflow document.
- Persist remote task state locally.
- Recover remote tasks after local process restart or transient SSH disconnect.
- Preserve a continuous interactive Codex session instead of repeatedly spawning non-interactive runs.
- Ask the user in Lark whenever the remote session requires clarification, confirmation, or a solution decision.

## Non-Goals

- Free-form natural language task creation in the first version.
- A general-purpose web console.
- Multi-user authorization and tenant isolation.
- Non-SSH execution transports.
- Raw terminal streaming into Lark.
- Exact terminal rendering fidelity.

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

### 4. Remote Interactive Runner

Connects to remote machines over SSH and manages a long-lived `tmux` session per task.

Primary capabilities:

- prepare task workspace;
- create and name a `tmux` session;
- start `codex` inside that session;
- read recent output with `tmux capture-pane`;
- send follow-up input with `tmux send-keys`;
- detect whether a session still exists with `tmux has-session`;
- stop a task by terminating the `tmux` session or the foreground process.

### 5. Decision Layer

Uses the repository/template workflow document plus runtime context to:

- summarize remote progress for status reporting;
- decide whether Codex is still executing, has completed, or is blocked;
- classify user-facing questions into escalation categories.

The model may suggest escalation, but the orchestrator owns the explicit transition into `waiting_user_input`.

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

`pre_clone_bootstrap` runs after the remote task directory is created but before the repository is cloned.

`post_clone_bootstrap` runs after clone and branch checkout.

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
- `tmux_session_name`
- `remote_codex_session_id` when discoverable
- `last_input`
- `last_output_summary`
- `last_screen_digest`
- `awaiting_question`
- `created_at`
- `updated_at`

### AwaitingQuestion

Needed whenever the task pauses for user input.

Suggested fields:

- `question_text`
- `options_summary`
- `context_excerpt`
- `question_type`
- `asked_at`
- `answered_at`

## Task Lifecycle

Suggested states:

- `pending`
- `preparing_workspace`
- `starting_session`
- `running`
- `waiting_user_input`
- `detached`
- `completed`
- `failed`
- `stopped`

Representative transitions:

- `pending -> preparing_workspace`
- `preparing_workspace -> starting_session`
- `starting_session -> running`
- `running -> waiting_user_input`
- `waiting_user_input -> running`
- `running -> detached`
- `detached -> running`
- `running -> completed`
- `preparing_workspace/starting_session/running/detached -> failed`
- `pending/preparing_workspace/starting_session/running/waiting_user_input/detached -> stopped`

## Remote Startup Model

Task startup is deterministic code, not workflow prose.

For a new task, the runner should:

1. select the target machine;
2. create a task-scoped remote directory under `remote_workspace_root`, for example `<workspace_root>/<task-id>`;
3. run `pre_clone_bootstrap` inside that task directory;
4. clone `remote_repo_url` into a repository subdirectory inside the task directory;
5. checkout `default_branch`;
6. run `post_clone_bootstrap` inside the cloned repository;
7. create a `tmux` session named from the task id, for example `alterego-<task-id>`;
8. start `codex` inside that `tmux` session.

The workflow document does not own clone or environment setup. It only guides how the development task should proceed once the repository is ready.

## Scheduling Model

The orchestrator maintains a round-robin queue of active tasks.

Scheduling rules:

- tasks in `running` or `detached` remain eligible for scheduling;
- tasks in `waiting_user_input` are skipped until the user replies;
- each scheduling turn gives one task one bounded unit of progress:
  - capture recent output from the remote `tmux` session;
  - interpret task state;
  - decide whether to continue, escalate, recover, or finish;
  - optionally send one next input.

## Remote Session Model

The runner must support two top-level entry paths:

- `StartNewInteractiveSession`
- `ReconnectInteractiveSession`

Recovery is based on `tmux`, not `codex exec resume`.

For a detached task:

1. connect to the machine over SSH;
2. check `tmux has-session -t <session-name>`;
3. if the session still exists, keep using that live session;
4. if the session no longer exists, mark the task failed or require explicit operator recovery policy.

The first version does not attempt to reconstruct a lost interactive session from a dead terminal.

### Recovery Inputs

The recovery operation uses:

- `machine_id`
- `remote_workdir`
- `tmux_session_name`

`remote_codex_session_id` may still be persisted when visible, but it is informational in the TTY-first design rather than the primary recovery key.

## Persistence

SQLite is the first-version persistence backend.

Suggested tables:

- `tasks`
- `task_events`
- `task_questions`

`task_events` should capture operator-auditable state changes such as:

- task created
- machine selected
- workspace prepared
- tmux session started
- user question raised
- user answer applied
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

- the coordinator is managing remote interactive Codex sessions;
- it should continue automatically unless Codex asks for user input;
- it should summarize progress concisely;
- it should not invent task state outside the orchestrator's known state.

### Template Workflow Document

This document defines repository-specific operating behavior, for example:

- preferred development sequence;
- validation expectations;
- summary style;
- repository-specific coding norms.

### Runtime Context

Runtime context includes:

- user request;
- task status;
- repository and template identity;
- recent output summary;
- latest captured terminal window when needed;
- last command sent to Codex;
- whether a question is currently pending.

## Human Escalation Policy

The first version should explicitly support at least these categories:

- `requirement_clarification`
- `scope_confirmation`
- `implementation_solution_choice`
- `missing_context`

Anything that looks like a direct question from Codex to the user should pause the task and route the question through Lark.

## Lark Command Semantics

### `/task start <template> <requirement text>`

- validate template existence;
- resolve repository;
- choose machine;
- create persisted `TaskRun`;
- schedule workspace preparation and session start;
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
- `tmux` session name
- latest summary
- pending question if any

### `/task reply <task-id> <decision text>`

- only valid when the task is in `waiting_user_input`;
- persists the user's reply;
- transitions the task back to `running`;
- injects the reply into the live `tmux` session.

### `/task stop <task-id>`

- marks the task as stopping/stopped;
- attempts remote session termination;
- persists final state.

## Error Handling

The first version should handle at least:

- SSH connection failure;
- workspace preparation failure;
- bootstrap command failure;
- repository clone failure;
- `tmux` session creation failure;
- `tmux has-session` failure;
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
- `tmux` startup command construction;
- `tmux capture-pane` parsing and screen normalization;
- `tmux has-session` recovery path;
- round-robin scheduling behavior;
- Lark command parsing and task operation routing;
- decision-layer gating for `waiting_user_input`.

The implementation should isolate remote execution behind interfaces so SSH and `tmux` interactions can be tested with fakes.

## Future Work

- natural-language task creation that compiles down into `/task start`;
- richer escalation categories and better question extraction;
- richer operator view into recent terminal output;
- web dashboard for task inspection;
- cross-process leader election if the orchestrator ever runs in more than one local instance.
