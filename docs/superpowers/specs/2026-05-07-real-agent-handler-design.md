# Real Agent Handler Design

Date: 2026-05-07

## Context

Alter Ego already has a working Lark transport layer and a stub handler. The next step is to replace the deterministic echo behavior with a real message handler that can:

- execute a few operational commands for the owner;
- call an LLM for normal free-form messages;
- maintain short-lived conversation context in memory.

The current codebase is intentionally small. The first real handler should preserve that property. It should not introduce tools, persistent memory, or workflow orchestration yet.

The user explicitly asked to skip the normal spec review gate after the document is written and proceed directly to implementation.

## Decision

Implement a hybrid handler:

- messages beginning with `/` are routed to a command handler;
- all other text messages are routed to an LLM-backed chat handler.

The LLM integration will use the OpenAI Responses API over plain HTTP from the Go standard library. This keeps the dependency surface small and avoids binding the project to a separate SDK while the runtime shape is still evolving.

## Goals

- Replace the stub echo handler with a real hybrid handler.
- Support `/help`, `/status`, and `/reset`.
- Support ordinary text chat through an OpenAI-compatible API endpoint.
- Keep a bounded in-memory conversation history per conversation.
- Preserve the existing `channel.Handler` interface so Lark integration code does not need architectural changes.

## Non-Goals

- Tool calling.
- Persistent memory or disk-backed sessions.
- Streaming replies.
- Multi-model routing.
- Rich persona management.
- Image or file inputs.
- Functionally complete agent orchestration.

## Architecture

The first real handler will use five focused units inside `internal/agent`:

1. `config.go`

   Parses OpenAI-related configuration from environment variables:

   - `ALTER_EGO_OPENAI_API_KEY`
   - `ALTER_EGO_OPENAI_BASE_URL`
   - `ALTER_EGO_OPENAI_MODEL`

   `ALTER_EGO_OPENAI_BASE_URL` defaults to `https://api.openai.com/v1`.

2. `session.go`

   Stores short-lived per-conversation message history in memory. The store is keyed by `platform + ":" + conversation.ID` and keeps only the most recent bounded number of turns.

3. `command.go`

   Handles explicit owner and operator commands:

   - `/help`: show supported commands
   - `/status`: show platform, conversation kind, sender ID, model configuration state, and current session history size
   - `/reset`: clear the current conversation history

4. `chat.go`

   Calls the OpenAI Responses API for regular chat messages. It builds a prompt from:

   - one fixed system instruction;
   - recent conversation history from the session store;
   - the current user message.

   The handler returns a normal text reply and appends the user/assistant exchange back to the session store.

5. `router.go`

   Implements `channel.Handler`, dispatching `/command` messages to the command handler and normal text to the chat handler.

## Data Flow

1. Lark receives an incoming text message and normalizes it into `channel.MessageEvent`.
2. `Router.HandleMessage` trims the text and checks whether it begins with `/`.
3. If it is a command:
   - parse the command token;
   - execute `help`, `status`, or `reset`;
   - return a direct text response.
4. If it is normal text:
   - compute the conversation session key;
   - load recent history;
   - call the OpenAI Responses API;
   - persist the new user/assistant turn in memory;
   - return the assistant text.

## OpenAI Integration

The first version will use a minimal HTTP client:

- `POST {base_url}/responses`
- bearer auth with `ALTER_EGO_OPENAI_API_KEY`
- JSON request body containing:
  - `model`
  - `input`

The implementation should parse the response text from the top-level `output_text` field. If the API returns an error or no usable text, the handler should return a user-facing failure message rather than crashing the process.

If the OpenAI config is incomplete, command handling must still work. Normal chat messages should return a clear configuration error message such as "LLM is not configured".

## Session Model

The in-memory session store keeps a bounded slice of messages per conversation. Each stored item needs only:

- role (`user` or `assistant`)
- content

The store will cap history by message count, not token count, to stay simple. The first version should keep the most recent 12 stored messages, which corresponds to roughly 6 user-assistant turns.

`/reset` clears only the current conversation session. It does not affect other chats.

## Command Behavior

- `/help`
  - Returns the supported commands and a one-line explanation for each.

- `/status`
  - Returns:
    - platform
    - conversation kind
    - conversation ID
    - sender ID
    - configured model or "not configured"
    - current history message count

- `/reset`
  - Clears the current conversation session and returns a confirmation message.

Unknown commands should produce a short error message that points users to `/help`.

## Safety and Failure Handling

- Do not log API keys or authorization headers.
- Do not panic on malformed upstream responses.
- If the OpenAI API call fails, return a concise operational error message to the chat.
- Keep command handling available even when the LLM is unavailable.

## Testing

Tests should cover:

- command routing vs chat routing;
- `/help`, `/status`, `/reset`, and unknown command behavior;
- session append, truncation, count, and reset behavior;
- chat handler request/response behavior through a fake client;
- LLM-not-configured behavior;
- preservation of reply conversation targeting.

The implementation should avoid real network calls in unit tests.

## Future Work

- stream LLM replies back to Lark;
- add persistent memory;
- add system prompt configuration;
- add tool invocation and planner/executor structure;
- add richer access-aware operator commands.
