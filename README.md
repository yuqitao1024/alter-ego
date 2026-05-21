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
export ALTER_EGO_LARK_CALLBACK_LISTEN_ADDR=":8080"
export ALTER_EGO_LARK_CALLBACK_PUBLIC_URL="https://callback.example.com"
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
- `/task list -a`
- `/task status <task-id>`
- `/task reply <task-id> <decision text>`
- `/task stop <task-id>`
- `/task delete <task-id>`
- `/task delete -a`

## Remote Codex Tasks

Remote Codex orchestration is configured from repository files and persisted in SQLite. Each task runs against a long-lived remote `codex app-server` thread, and Alter Ego drives Codex through the structured app-server protocol instead of scraping terminal output. SSH is still used to bootstrap and proxy the remote app-server process, but the task state source is the app-server thread and turn model.

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
- `codex app-server` and `codex remote-control` available on the remote machine
- `codex` installed and already authenticated on the remote machine
- Git access to the configured `remote_repo_url`

Machine configuration can also define a lightweight shell preamble that is injected into every SSH command:

```yaml
id: machine_a
host: build-a.example.com
user: codex
app_server_listen_host: 0.0.0.0
app_server_listen_port: 4317
app_server_service_name: codex-app-server
app_server_install_user: codex
app_server_ws_auth_token: change-me-to-a-long-random-token
shell_init:
  - source /opt/codex/env.sh
```

Use `shell_init` only for idempotent environment setup such as exporting `CODEX_HOME`, toolchain paths, or proxy variables. It is injected into SSH commands and the app-server startup command. Keep repository-specific preparation in `pre_clone_bootstrap` and `post_clone_bootstrap`.

Non-loopback Codex app-server websocket listeners also require websocket auth. Set a per-machine `app_server_ws_auth_token`; Alter Ego will install it onto the remote machine as a capability token file and will connect with `Authorization: Bearer <token>`.

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
7. connect to the machine's long-lived Codex app-server websocket endpoint;
8. create a task-scoped app-server thread;
9. start `codex` inside that thread.

Interactive task lifecycle:

1. `pending`
2. `starting`
3. `running`
4. `waiting_user_input` when Codex issues an explicit app-server server request that needs user involvement
5. `recovering` when Alter Ego loses contact with the remote app-server thread and is attempting recovery
6. `completed` when Codex confirms the requested workflow is finished
7. `failed` when startup, recovery, or remote execution cannot continue
8. `stopped` when the operator explicitly stops the task

Task list output now uses Lark interactive cards when sent from the Lark channel. Each task card includes action buttons:

- `status` to fetch the current task summary
- `stop` for `running` or `waiting_user_input`
- `delete` for terminal tasks, with a confirm dialog

To receive card action callbacks, expose a local HTTP listener on `ALTER_EGO_LARK_CALLBACK_LISTEN_ADDR` and point the Lark app's card callback URL at `ALTER_EGO_LARK_CALLBACK_PUBLIC_URL + /lark/card/callback`.

Example:

```text
listen addr: :8080
public url:  https://callback.example.com
callback url: https://callback.example.com/lark/card/callback
```

Task state and operator audit data are stored in SQLite:

- `tasks`
- `task_events`
- `task_questions`
- `task_server_requests`

Replies from `/task reply` are injected back into the live remote session rather than starting a new Codex run.

Task decision flow:

1. subscribe to Codex app-server websocket events and keep the latest thread snapshot in memory;
2. persist each explicit app-server server request and handle it exactly once;
3. only reply to Codex when an explicit server request is pending;
4. use the model to classify whether a pending request can be auto-handled or should be escalated to the user;
5. keep the 2-minute polling loop only for progress reporting and completion-check logic, never for inventing new Codex input;
6. send the one-time completion-check prompt after Codex signals completion, and never send it twice.

Run locally:

```sh
CGO_ENABLED=0 go run ./cmd/alterego
```

## Packaging

Generic Linux packaging assets live in [packaging/README.md](/Users/yuqitao/aiagent/alter-ego/packaging/README.md). The committed packaging flow builds a `tar.gz` with:

- the Linux `alterego` binary
- `alteregod.service`
- an empty environment template
- example task configuration with valid app-server fields

It intentionally excludes any real secrets or real deployment configuration.

## License

Copyright 2026 yuqitao1024.

This project is licensed under the [Apache License 2.0](LICENSE).
