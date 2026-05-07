# LLM Provider Abstraction Design

Date: 2026-05-07

## Context

The first real handler already supports:

- command routing;
- in-memory session state;
- ordinary text chat through an OpenAI-backed implementation.

The next requirement is to abstract the model provider layer so Alter Ego can switch between providers without changing the handler flow. The immediate target is Zhipu GLM because the user has GLM Coding Plan access rather than an OpenAI API key.

The user explicitly asked to proceed without waiting for manual review.

## Decision

Introduce a provider abstraction under `internal/agent`:

- keep the existing chat handler contract unchanged at the top;
- replace the OpenAI-specific client dependency with a provider interface;
- support two providers in the first version:
  - `openai`
  - `glm`

`glm` will use Zhipu's official OpenAI-compatible Chat Completions interface.

## Goals

- Support multiple LLM providers behind one handler interface.
- Preserve the current command and session behavior.
- Allow GLM to be configured through environment variables.
- Keep unit tests network-free by using fake HTTP servers.

## Non-Goals

- A plugin system for arbitrary providers.
- Streaming output.
- Tool calling.
- Vendor-specific advanced features such as GLM thinking mode.
- Live GLM integration testing without credentials.

## Configuration

Add provider-neutral environment variables:

- `ALTER_EGO_LLM_PROVIDER`
- `ALTER_EGO_LLM_API_KEY`
- `ALTER_EGO_LLM_BASE_URL`
- `ALTER_EGO_LLM_MODEL`

Compatibility behavior:

- If generic LLM variables are absent, continue to accept the old `ALTER_EGO_OPENAI_*` variables.

Defaults:

- `provider=openai` when unset
- OpenAI base URL defaults to `https://api.openai.com/v1`
- GLM base URL defaults to `https://open.bigmodel.cn/api/coding/paas/v4`

The GLM default is based on Zhipu's Coding Plan documentation for coding-agent style integrations.

## Architecture

1. `config.go`

   Extend `Config` with `Provider`, and parse generic `ALTER_EGO_LLM_*` variables first, then fall back to legacy OpenAI variables.

2. `provider.go`

   Define the provider interface and provider factory:

   - `type Provider interface`
   - `NewProvider(cfg Config, httpClient *http.Client) Provider`

3. `provider_openai.go`

   Keep the current OpenAI Responses implementation with minimal changes.

4. `provider_glm.go`

   Add a GLM provider that calls:

   - `POST {base_url}/chat/completions`

   and parses:

   - `choices[0].message.content`

5. `chat.go`

   Depend on the provider interface instead of directly constructing an OpenAI client.

## Testing

Tests should cover:

- generic LLM config parsing and legacy fallback;
- provider factory selection;
- OpenAI response parsing still works;
- GLM chat completions parsing works through a fake server;
- chat handler still produces correct user-facing behavior with the abstracted provider.

## References

- Zhipu OpenAI-compatible interface:
  https://docs.bigmodel.cn/cn/guide/develop/openai/introduction
- Zhipu GLM Coding Plan coding endpoint:
  https://docs.bigmodel.cn/cn/coding-plan/using5-1
