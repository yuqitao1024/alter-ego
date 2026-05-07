# Lark Agent Module Design

Date: 2026-05-05

## Context

Alter Ego will be developed in Go. The first functional module will connect the agent to a Lark assistant account so the owner can talk to the agent from Lark and the agent can send results back through the same channel.

The first version should prioritize a working end-to-end Lark integration over a complete multi-channel architecture. The code should still keep a minimal boundary between Lark-specific code and the future agent runtime, so later refactoring can add more channels without replacing the whole module.

## Decision

Use Lark/Lark WebSocket event subscription through the official Go SDK as the first integration path.

This avoids requiring a public webhook URL for local development and matches the near-term goal: run a personal agent service that can receive Lark messages and respond through the assistant account.

## Goals

- Receive direct messages from a Lark assistant account.
- Receive group messages when the bot is mentioned.
- Normalize incoming Lark messages into an internal message event.
- Route each incoming event to a simple internal handler.
- Send handler output back to the same direct chat or group conversation.
- Load app credentials and access-control settings from environment variables.
- Keep app secrets out of source control.

## Non-Goals

- Media messages such as images, files, audio, and video.
- Streaming replies with interactive cards.
- Lark Drive comment workflows.
- Multiple Lark accounts.
- Multi-agent routing.
- Pairing approval flows.
- A full plugin system.

## Architecture

The first implementation will use four small internal layers:

1. `cmd/alterego`

   Starts the process, loads configuration, builds the Lark adapter, attaches a handler, and blocks until shutdown.

2. `internal/channel`

   Defines platform-neutral types and interfaces:

   - `MessageEvent`
   - `Conversation`
   - `Sender`
   - `OutgoingMessage`
   - `Handler`

   This package should not import Lark SDK packages.

3. `internal/lark`

   Owns Lark-specific behavior:

   - Lark SDK client construction.
   - WebSocket event registration.
   - Conversion from Lark events to `channel.MessageEvent`.
   - Direct-message and group-message reply delivery.
   - Basic access-control checks.

4. `internal/agent`

   Provides the first stub handler. It can return a simple deterministic response so the Lark integration can be tested before the real agent runtime exists.

## Configuration

The first version will use environment variables:

- `ALTER_EGO_LARK_APP_ID`
- `ALTER_EGO_LARK_APP_SECRET`
- `ALTER_EGO_LARK_DOMAIN`
- `ALTER_EGO_LARK_ALLOW_USERS`
- `ALTER_EGO_LARK_ALLOW_GROUPS`
- `ALTER_EGO_LARK_REQUIRE_MENTION`

`ALTER_EGO_LARK_DOMAIN` defaults to `lark`. `lark` can be used for global Lark tenants.

`ALTER_EGO_LARK_ALLOW_USERS` and `ALTER_EGO_LARK_ALLOW_GROUPS` are comma-separated open IDs and chat IDs. Empty allowlists should deny unknown users or groups by default unless the code is explicitly configured otherwise.

`ALTER_EGO_LARK_REQUIRE_MENTION` defaults to `true` for group chats.

## Message Flow

1. Lark sends an `im.message.receive_v1` event over WebSocket.
2. The Lark adapter validates whether the sender and conversation are allowed.
3. The adapter converts the event into `channel.MessageEvent`.
4. The handler receives the normalized event.
5. The handler returns a `channel.OutgoingMessage`.
6. The Lark adapter sends the response to the original direct chat or group chat.

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
- Conversion from simplified Lark message payloads into internal message events.
- Reply target selection for direct messages and group messages.

The first implementation can use small fixtures or hand-built structs instead of live Lark API calls.

## Future Work

- Replace the stub handler with the real agent runtime.
- Add message persistence and session state.
- Add media support.
- Add streaming card responses.
- Add pairing instead of static allowlists.
- Extract a more general channel adapter interface after at least one more channel exists.
- Add multi-agent routing only after the single-agent workflow is stable.

## References

- OpenClaw Lark channel design: https://docs.openclaw.ai/channels/lark
- OpenClaw channel and gateway overview: https://docs.openclaw.ai/channels
- Lark Open Platform Go SDK: https://pkg.go.dev/github.com/larksuite/oapi-sdk-go/v3
