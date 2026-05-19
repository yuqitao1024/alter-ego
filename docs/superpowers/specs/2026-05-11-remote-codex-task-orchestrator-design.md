# Remote Codex Task Orchestrator Design

Date: 2026-05-11

Status: superseded

This design has been retired. The implemented task lifecycle now uses the app-server-backed design documented in `docs/superpowers/specs/2026-05-19-codex-app-server-migration-design.md`.

The retained historical intent is:

- `/task` remains the operator control surface
- repository/template/workflow binding remains the orchestration model
- SQLite persistence, scheduler, and notifier remain part of the control plane

Do not use this file as an implementation reference.
