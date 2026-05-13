# Alter Ego

Alter Ego is an early-stage AI agent project focused on creating a virtual counterpart of the person who builds and uses it. The goal is to build an agent that can assist with day-to-day work, explore topics of interest, and help investigate the practical boundaries of modern AI systems.

## Lark Assistant

The first integration target is a Lark assistant account. The Go service connects to Lark through WebSocket event subscription, receives text messages, and sends text replies back to the same conversation.

Required environment variables:

```sh
export ALTER_EGO_LARK_APP_ID="cli_xxx"
export ALTER_EGO_LARK_APP_SECRET="xxx"
export ALTER_EGO_LARK_ALLOW_USERS="ou_xxx"
```

Optional environment variables:

```sh
export ALTER_EGO_LARK_DOMAIN="lark"
export ALTER_EGO_LARK_ALLOW_GROUPS="oc_xxx"
export ALTER_EGO_LARK_REQUIRE_MENTION="true"
```

To enable real chat replies instead of the stub handler, configure:

```sh
export ALTER_EGO_LLM_PROVIDER="openai"
export ALTER_EGO_LLM_API_KEY="sk-xxx"
export ALTER_EGO_LLM_BASE_URL="https://api.openai.com/v1"
export ALTER_EGO_LLM_MODEL="gpt-5"
```

For DashScope OpenAI-compatible setups with GLM models, use:

```sh
export ALTER_EGO_LLM_PROVIDER="dashscope"
export ALTER_EGO_LLM_API_KEY="your-dashscope-api-key"
export ALTER_EGO_LLM_BASE_URL="https://dashscope.aliyuncs.com/compatible-mode/v1"
export ALTER_EGO_LLM_MODEL="glm-5.1"
```

Legacy `ALTER_EGO_OPENAI_*` variables are still accepted for backward compatibility.

Supported commands:

- `/help`
- `/status`
- `/reset`
- `/task start <template> <requirement text>`
- `/task list`
- `/task status <task-id>`
- `/task reply <task-id> <decision text>`
- `/task stop <task-id>`

## Remote Codex Tasks

Remote Codex orchestration is configured from repository files and persisted in SQLite. Each task runs inside a long-lived remote `tmux` session, and Alter Ego drives Codex by reading the session screen and sending follow-up input back into the same session. If `tmux` survives but the foreground Codex process exits, Alter Ego probes the pane state and issues one controlled `codex resume --last` before falling back to model arbitration.

Unlike the general chat handler, remote task orchestration requires a configured LLM. Deterministic terminal handshakes such as trust prompts and usage-limit prompts are still handled by fixed responders, but every non-deterministic Codex interaction is arbitrated by the configured model. The task subsystem will fail to start if `ALTER_EGO_LLM_API_KEY` or `ALTER_EGO_LLM_MODEL` is missing.

Optional task environment variables:

```sh
export ALTER_EGO_TASK_CONFIG_ROOT="."
export ALTER_EGO_TASK_DB_PATH=".alterego/tasks.db"
```

Configuration layout:

```text
configs/machines/*.yaml
configs/repositories/*.yaml
configs/templates/*.yaml
docs/workflows/*.md
```

Each repository binds to its remote machine pool. Each template binds to one repository and one workflow document. Task state is stored in the SQLite database defined by `ALTER_EGO_TASK_DB_PATH`.

Remote machine prerequisites:

- `ssh` access from the local control node
- `tmux` installed on the remote machine
- `codex` installed and already authenticated on the remote machine
- Git access to the configured `remote_repo_url`

Machine configuration can also define a lightweight shell preamble that is injected into every SSH command:

```yaml
id: machine_a
host: build-a.example.com
user: codex
shell_init:
  - source /opt/codex/env.sh
```

Use `shell_init` only for idempotent environment setup such as exporting `CODEX_HOME`, toolchain paths, or proxy variables. Keep repository-specific preparation in `pre_clone_bootstrap` and `post_clone_bootstrap`.

Repository configuration now uses task-scoped checkout settings instead of a fixed repository path. A repository entry should define:

```yaml
id: repo_backend
display_name: Backend Repo
remote_repo_url: git@github.com:org/repo.git
remote_workspace_root: /srv/codex-tasks
default_branch: main
machine_ids:
  - machine_a
pre_clone_bootstrap:
  - setup-git-auth
post_clone_bootstrap:
  - pnpm install
```

For each new task, Alter Ego will:

1. choose a machine from the repository machine pool;
2. create a task directory under `remote_workspace_root/<task-id>`;
3. run `pre_clone_bootstrap`;
4. clone the repository;
5. checkout `default_branch`;
6. run `post_clone_bootstrap`;
7. create a task-scoped `tmux` session;
8. start `codex` inside that `tmux` session.

Interactive task lifecycle:

1. `pending`
2. `preparing_workspace`
3. `starting_session`
4. `running`
5. `waiting_user_input` when Codex needs clarification, scope confirmation, an implementation choice, or missing context
6. `detached` when the local operator loses attachment but the remote `tmux` session may still exist
7. `completed` when the model arbitrator concludes the requested workflow is finished
8. `failed` when startup, recovery, or remote execution cannot continue
9. `stopped` when the operator explicitly stops the task

Task state and operator audit data are stored in SQLite:

- `tasks`
- `task_events`
- `task_questions`

Replies from `/task reply` are injected back into the live remote session rather than starting a new Codex run.

Task decision flow:

1. probe the current `tmux` pane state and capture the current screen tail;
2. run deterministic responders for known terminal handshakes;
3. if the pane is still alive but Codex has dropped back to the shell, issue one controlled `codex resume --last`;
4. if no responder applies, send the workflow, task context, and terminal excerpt to the configured LLM;
5. the LLM must return one of:
   - `wait`
   - `reply_to_codex`
   - `ask_user`
   - `complete_task`

Run locally:

```sh
CGO_ENABLED=0 go run ./cmd/alterego
```

## Packaging

Generic Linux packaging assets live in [packaging/README.md](/Users/yuqitao/aiagent/alter-ego/packaging/README.md). The committed packaging flow builds a `tar.gz` with:

- the Linux `alterego` binary
- `alteregod.service`
- an empty environment template
- example task configuration

It intentionally excludes any real secrets or real deployment configuration.

## License

Copyright 2026 yuqitao1024.

This project is licensed under the [Apache License 2.0](LICENSE).
