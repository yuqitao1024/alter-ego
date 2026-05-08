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

Run locally:

```sh
CGO_ENABLED=0 go run ./cmd/alterego
```

## License

Copyright 2026 yuqitao1024.

This project is licensed under the [Apache License 2.0](LICENSE).
