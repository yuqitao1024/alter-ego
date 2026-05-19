# Codex App-Server WS Subscription Design

Date: 2026-05-19

## Context

The current app-server integration in Alter Ego is sufficient for minimal task orchestration, but it has three structural problems:

- app-server code still lives inside `internal/orchestrator` instead of a dedicated package boundary;
- task state is driven by synchronous polling through `thread/get` and `thread/items/list`;
- runtime transport still depends on SSH-proxied stdio instead of a direct websocket connection to the remote app-server.

The next step is to turn app-server into a first-class subsystem rather than an orchestrator implementation detail.

The target runtime model is:

- remote machines run `codex app-server` as a long-lived machine-managed service;
- Alter Ego connects directly to that service over websocket on the machine network;
- task progress is driven by subscriptions and local thread snapshots instead of polling RPCs;
- SSH is used only for explicit machine initialization, not for runtime task execution.

## Decision

Implement a dedicated package named `internal/codexappserver` and move all app-server transport, client, connection-management, subscription, and machine-init logic there.

The runtime architecture will be:

- one long-lived websocket connection per machine;
- many task threads multiplexed over that shared machine connection;
- one thread watcher per task thread;
- a watcher-maintained thread snapshot consumed by `internal/orchestrator`;
- explicit machine initialization through a user-invoked command;
- no runtime SSH fallback once websocket mode is enabled.

The machine init path will install and configure a machine-local service that starts Codex app-server with:

```text
--dangerously-bypass-approvals-and-sandbox
```

The first version does not add websocket authentication. It assumes an internal network deployment and keeps the configuration model simple.

## Goals

- Split app-server code into a dedicated Go package.
- Replace task runtime polling with subscription-driven state updates.
- Reuse one websocket connection per machine across multiple tasks.
- Keep machine initialization explicit rather than implicit.
- Remove SSH from the task execution critical path.
- Keep current task persistence centered on `thread_id` and `active_turn_id`.

## Non-Goals

- Automatic machine initialization on first use.
- SSH runtime fallback after websocket mode is introduced.
- A machine state table in SQLite.
- Websocket authentication in the first version.
- Full app-server protocol coverage beyond task-runtime needs.

## Package Boundary

Create a package:

```text
internal/codexappserver
```

This package owns five responsibilities.

### 1. Machine Init

Explicit installation and upgrade of the remote Codex app-server service through SSH.

Responsibilities:

- verify `codex` exists on the remote machine;
- write service configuration and systemd unit files;
- enable start-on-boot behavior;
- start or restart the service;
- verify that the service is active after installation.

This is the only place where SSH remains in scope.

### 2. Websocket Transport

Direct websocket connectivity to the machine-managed app-server endpoint.

Responsibilities:

- establish websocket sessions;
- detect disconnects;
- reconnect with backoff;
- expose a framed message stream to higher layers.

The first version assumes internal network access and does not implement websocket auth.

### 3. RPC Client

Structured request/response interaction over the websocket transport.

Responsibilities:

- request ID allocation;
- response correlation;
- safe concurrent in-flight RPC handling;
- typed request and response wrappers for supported app-server methods.

This replaces the current single-flight stdio client.

### 4. Subscription Layer

Machine-scoped event ingestion and thread-scoped watcher registration.

Responsibilities:

- subscribe to the app-server event stream;
- route incoming events to the correct thread watcher;
- support watcher attach, detach, and recovery after reconnect.

### 5. Connection Manager

One shared runtime object per machine.

Responsibilities:

- own the single websocket connection for a machine;
- own reconnect policy;
- own the machine-level subscription session;
- hand out thread watchers to orchestrator consumers.

## Machine Configuration Model

The machine configuration must be extended to support websocket-native runtime access and explicit init.

Required machine-level fields:

- `app_server_listen_host`
- `app_server_listen_port`
- `app_server_service_name`
- `app_server_install_user`

Derived runtime value:

- websocket URL built from host and port

Optional future fields may later add auth, but this version does not require them.

## Machine Init Command

Add an explicit command for operators:

```text
/machine init <machine-id>
```

The command performs:

1. SSH connectivity check.
2. Remote `codex` existence check.
3. Service unit generation.
4. `systemctl daemon-reload`.
5. `systemctl enable`.
6. `systemctl restart`.
7. `systemctl is-enabled`.
8. `systemctl is-active`.

The generated service must run `codex app-server` with:

- `--listen ws://<host>:<port>`
- `--dangerously-bypass-approvals-and-sandbox`

Machine init is explicit only. Task start does not auto-install anything.

## Runtime Data Flow

### Task Start

When `/task start` reaches session startup:

1. orchestrator asks `codexappserver.Manager` for the machine handle;
2. manager returns the shared machine connection or establishes it;
3. orchestrator starts a thread and initial turn through the app-server client;
4. orchestrator registers a thread watcher for that thread;
5. watcher begins maintaining a local thread snapshot for later task ticks.

### Task Progress

Current task progress logic is polling-based:

- `thread/get`
- `thread/items/list`

This must be replaced.

New flow:

1. machine connection receives app-server events continuously;
2. subscription layer routes events to thread watchers;
3. watcher updates the thread snapshot;
4. orchestrator tick reads the latest snapshot and decides what to do.

The key change is that orchestrator no longer uses RPC polling as its remote state source.

### Task Reply

When the user answers a waiting task:

1. orchestrator reads the persisted `thread_id` and current watcher state;
2. if an active turn exists, send input through turn steer;
3. otherwise start a new turn;
4. watcher state continues from the shared machine connection.

### Task Stop

Task stop remains an explicit remote interrupt:

1. orchestrator resolves current `thread_id` and `active_turn_id`;
2. `codexappserver` sends turn interrupt;
3. watcher marks the thread as interrupted or completed based on subsequent events.

## Thread Snapshot Model

Each task thread watcher maintains a snapshot with enough structure for orchestration decisions.

Minimum fields:

- `thread_id`
- `thread_status`
- `active_turn_id`
- `active_turn_status`
- `last_item_id`
- `last_activity_at`
- `latest_agent_message`
- `latest_plan`
- `latest_command`
- `latest_summary`
- `subscription_state`
- `last_subscription_error`

This snapshot is not the SQLite persistence model. It is an in-memory runtime model owned by `codexappserver`.

## Orchestrator Integration

`internal/orchestrator` should depend on a narrower high-level interface and stop carrying transport details.

Expected high-level operations:

- `InitMachine(ctx, machineID)`
- `StartTaskSession(ctx, req)`
- `WatchTaskThread(ctx, machineID, threadID)`
- `SendTaskInput(ctx, machineID, threadID, activeTurnID, input)`
- `InterruptTask(ctx, machineID, threadID, activeTurnID)`
- `Snapshot(machineID, threadID)`

`TickOnce` should read the current thread snapshot instead of calling `CaptureOutput`.

The existing task persistence remains useful:

- `thread_id`
- `active_turn_id`
- `workflow_stage`
- `last_output_summary`

No machine state table is added in this version.

## Detached and Recovery Semantics

Task recovery still exists, but its meaning shifts.

Detached no longer means:

- a local process lost contact with a tmux-like runtime session.

Detached now means:

- the machine connection or thread watcher is temporarily unavailable;
- the thread may still be alive remotely;
- the manager is reconnecting or the watcher needs to be reattached.

Recovery flow:

1. machine websocket disconnects;
2. connection manager enters reconnect mode;
3. affected watchers enter a degraded subscription state;
4. orchestrator may mark tasks detached if no fresh snapshot is available;
5. after reconnect, watchers reattach and resume snapshot updates;
6. orchestrator returns tasks to running once snapshot flow resumes.

## Testing Strategy

### `internal/codexappserver`

Cover:

- websocket connect and reconnect;
- concurrent RPC request/response correlation;
- machine-level shared connection reuse;
- watcher subscription registration and teardown;
- thread snapshot updates from incoming events;
- init command rendering and systemd unit generation.

### `internal/orchestrator`

Cover:

- task startup through the new app-server interface;
- tick decisions based on thread snapshot rather than polled output;
- user reply through steer-or-start-turn behavior;
- detach on watcher loss;
- recovery after watcher reconnection.

### Integration Boundary

Cover:

- machine init command success and failure reporting;
- missing init or unreachable websocket endpoint error surfacing;
- no runtime SSH fallback usage.

## Risks

- The exact app-server subscription event schema may differ from the current hand-written assumptions.
- Shared machine connections increase lifecycle complexity compared with single-task connections.
- Reconnect and watcher reattachment will need careful idempotency handling.
- The websocket runtime path introduces more concurrent state than the current single-flight client.

## Recommendation

Proceed with the dedicated `internal/codexappserver` package and machine-level websocket subscription architecture.

This gives Alter Ego a stable long-term boundary:

- explicit machine bootstrap,
- direct websocket runtime access,
- subscription-driven task state,
- no runtime SSH dependency,
- and no need to keep stretching orchestrator around app-server internals.
