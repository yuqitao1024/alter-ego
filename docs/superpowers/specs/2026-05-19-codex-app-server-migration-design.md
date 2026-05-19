# Codex App-Server Migration Design

Date: 2026-05-19

Status: implemented

## Summary

Alter Ego now runs remote task orchestration through Codex app-server threads and turns.

The operator-facing control plane is unchanged:

- Lark `/task` commands remain the entrypoint
- repository/template/workflow binding remains the orchestration model
- SQLite persists task lifecycle and question history
- the scheduler and notifier continue to own task advancement and escalation

## Implemented Runtime Model

- SSH is used for remote bootstrap and proxy transport
- each task is tracked by `thread_id` and `active_turn_id`
- task state is derived from structured app-server thread and item data
- user replies are injected through app-server turn start or steer operations
- restart recovery reconnects through persisted thread identity

## Notes

- This document is retained as a concise record of the app-server cutover
- The repository should treat app-server as the only supported task execution substrate
