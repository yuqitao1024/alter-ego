# Feishu Agent Module Design

Date: 2026-05-05

## Context

Alter Ego will be developed in Go. The first functional module will connect the agent to a Feishu assistant account so the owner can talk to the agent from Feishu and the agent can send results back through the same channel.

The first version should prioritize a working end-to-end Feishu integration over a complete multi-channel architecture. The code should still keep a minimal boundary between Feishu-specific code and the future agent runtime, so later refactoring can add more channels without replacing the whole module.

## Decision

Use Feishu/Lark WebSocket event subscription through the official Go SDK as the first integration path.

This avoids requiring a public webhook URL for local development and matches the near-term goal: run a personal agent service that can receive Feishu messages and respond through the assistant account.

## Goals

- Receive direct messages from a Feishu assistant account.
- Receive group messages when the bot is mentioned.
- Normalize incoming Feishu messages into an internal message event.
- Route each incoming event to a simple internal handler.
- Send handler output back to the same direct chat or group conversation.
- Load app credentials and access-control settings from environment variables.
- Keep app secrets out of source control.

## Non-Goals

- Media messages such as images, files, audio, and video.
- Streaming replies with interactive cards.
- Feishu Drive comment workflows.
- Multiple Feishu accounts.
- Multi-agent routing.
- Pairing approval flows.
- A full plugin system.

## Architecture

The first implementation will use four small internal layers:

1. `cmd/alterego`

   Starts the process, loads configuration, builds the Feishu adapter, attaches a handler, and blocks until shutdown.

2. `internal/channel`

   Defines platform-neutral types and interfaces:

   - `MessageEvent`
   - `Conversation`
   - `Sender`
   - `OutgoingMessage`
   - `Handler`

   This package should not import Feishu SDK packages.

3. `internal/feishu`

   Owns Feishu-specific behavior:

   - Feishu SDK client construction.
   - WebSocket event registration.
   - Conversion from Feishu events to `channel.MessageEvent`.
   - Direct-message and group-message reply delivery.
   - Basic access-control checks.

4. `internal/agent`

   Provides the first stub handler. It can return a simple deterministic response so the Feishu integration can be tested before the real agent runtime exists.

## Configuration

The first version will use environment variables:

- `ALTER_EGO_FEISHU_APP_ID`
- `ALTER_EGO_FEISHU_APP_SECRET`
- `ALTER_EGO_FEISHU_DOMAIN`
- `ALTER_EGO_FEISHU_ALLOW_USERS`
- `ALTER_EGO_FEISHU_ALLOW_GROUPS`
- `ALTER_EGO_FEISHU_REQUIRE_MENTION`

`ALTER_EGO_FEISHU_DOMAIN` defaults to `feishu`. `lark` can be used for global Lark tenants.

`ALTER_EGO_FEISHU_ALLOW_USERS` and `ALTER_EGO_FEISHU_ALLOW_GROUPS` are comma-separated open IDs and chat IDs. Empty allowlists should deny unknown users or groups by default unless the code is explicitly configured otherwise.

`ALTER_EGO_FEISHU_REQUIRE_MENTION` defaults to `true` for group chats.

## Message Flow

1. Feishu sends an `im.message.receive_v1` event over WebSocket.
2. The Feishu adapter validates whether the sender and conversation are allowed.
3. The adapter converts the event into `channel.MessageEvent`.
4. The handler receives the normalized event.
5. The handler returns a `channel.OutgoingMessage`.
6. The Feishu adapter sends the response to the original direct chat or group chat.

## Safety Defaults

- Secrets are read only from environment variables.
- Unknown direct-message senders are ignored unless allowlisted.
- Unknown groups are ignored unless allowlisted.
- Group messages require an explicit bot mention by default.
- Logs must not print app secrets or raw credential values.

## Testing

Initial tests should cover:

- Environment configuration parsing.
- Allowlist decisions for direct messages and group messages.
- Group mention requirement behavior.
- Conversion from simplified Feishu message payloads into internal message events.
- Reply target selection for direct messages and group messages.

The first implementation can use small fixtures or hand-built structs instead of live Feishu API calls.

## Future Work

- Replace the stub handler with the real agent runtime.
- Add message persistence and session state.
- Add media support.
- Add streaming card responses.
- Add pairing instead of static allowlists.
- Extract a more general channel adapter interface after at least one more channel exists.
- Add multi-agent routing only after the single-agent workflow is stable.

## References

- OpenClaw Feishu channel design: https://docs.openclaw.ai/channels/feishu
- OpenClaw channel and gateway overview: https://docs.openclaw.ai/channels
- Feishu Open Platform Go SDK: https://pkg.go.dev/github.com/larksuite/oapi-sdk-go/v3
