# GitCode Issue To Feishu Bitable Sync Design

Date: 2026-06-13

## Context

Alter Ego already runs as a single Go service with existing HTTP routing, Lark integration, and a browser dashboard. The new requirement is to ingest GitCode webhook events for a single repository, project current issue state into a Feishu Bitable table, and keep related pull request information attached to the corresponding issue row.

The first version should optimize for a reliable weekly-report data source, not for audit logging or a general multi-repository integration platform. The table model is already conceptually fixed by the user:

- one Bitable row per issue;
- issue path is the stable row key;
- later issue events update the same row;
- pull requests do not get their own rows;
- pull request data is stored only in fields on the issue row;
- pull request to issue linkage comes only from the webhook `issues[]` association array.

## Decision

Extend the existing Go process with a new GitCode webhook handler and a new Feishu Bitable client module.

The implementation should accept GitCode `issue` and `merge_request` webhook events, verify authenticity, normalize the payload, deduplicate repeated deliveries, then upsert a single issue row in Feishu Bitable. Issue events own the base issue fields. Pull request events only update pull-request-related aggregate fields on already-existing issue rows.

## Goals

- Reuse the existing Alter Ego HTTP service instead of deploying a separate webhook service.
- Accept GitCode issue and pull request webhook events for one repository.
- Verify incoming webhook authenticity before processing.
- Deduplicate repeated webhook deliveries.
- Create one Bitable row per issue.
- Use the issue path as the stable row key.
- Update the existing row when the issue changes state or content.
- Update issue rows with related pull request summary fields when pull request webhook payloads include associated issues.
- Keep the implementation compatible with the user’s existing Bitable schema through configurable field mapping.
- Keep enough logs and local state to debug sync failures without building a full event-history subsystem.

## Non-Goals

- Multi-project or multi-repository routing.
- Separate Bitable tables for pull requests.
- Full event-history persistence for every webhook payload.
- Backfilling old issues or pull requests from GitCode APIs.
- Auto-creating issue rows from pull request events when the issue row does not already exist.
- Downstream weekly-report rendering or report generation logic.
- Generic support for every GitCode webhook event type.

## Architecture

The design should add four small boundaries:

1. `cmd/alterego`

   Loads GitCode and Feishu Bitable configuration, constructs the webhook handler, and mounts it on the existing HTTP server.

2. `internal/gitcode`

   Owns GitCode-specific behavior:

   - webhook header parsing;
   - token or signature verification;
   - request-body decoding;
   - event normalization for `issue` and `merge_request`;
   - short-term deduplication metadata.

3. `internal/bitable`

   Owns Feishu Bitable-specific behavior:

   - app token acquisition;
   - field mapping;
   - row lookup by issue key;
   - record creation;
   - record update.

4. `internal/issuesync` or equivalent service layer

   Owns product logic:

   - convert normalized GitCode issue payloads into issue-row mutations;
   - convert normalized pull request payloads into pull-request aggregate field mutations;
   - decide whether to create, update, skip, or warn;
   - isolate Bitable schema details from the raw webhook handler.

This keeps GitCode parsing, Feishu transport, and issue synchronization logic independently testable.

## HTTP Integration

The new webhook endpoint should be mounted on the existing Go HTTP mux, for example:

- `POST /gitcode/webhook`

This endpoint should live beside the existing Lark callback routes and browser routes. It should not require the browser dashboard to be enabled.

If the web server is disabled and only callback-style HTTP serving is active, the GitCode webhook route should still be available in the same HTTP process, subject to the same listen address constraints already used by the project.

## Configuration

The first version should use environment variables consistent with the current project style. Exact names can be finalized during implementation, but the design requires configuration for:

- GitCode webhook verification mode:
  - shared token header verification, or
  - HMAC signature verification
- GitCode webhook secret material
- GitCode webhook public path or listen enablement if needed
- Feishu app ID
- Feishu app secret
- Feishu Bitable app token
- Feishu Bitable table ID
- Bitable field mapping for the user’s actual schema

The field mapping should not assume a hard-coded table schema in code. The service should map internal logical fields such as `IssueKey`, `Title`, `State`, and `RelatedPRs` onto user-provided Bitable field names or IDs.

## Data Model

The normalized internal issue model should include, at minimum:

- stable issue key derived from issue path, with issue URL only as a fallback when the path is not available in the normalized payload;
- issue IID;
- title;
- description;
- state;
- action;
- labels;
- author;
- assignees;
- issue URL;
- created time;
- updated time;
- last actor.

The normalized internal pull request projection should include, at minimum:

- pull request ID or IID;
- title;
- state;
- action;
- URL;
- source branch;
- target branch;
- updated time;
- associated issue keys from `issues[]`.

The Bitable row should be treated as the current issue snapshot, not as an append-only history log.

## Sync Rules

### Issue Events

- `open`:
  - create a row if none exists for the issue key;
  - otherwise update the row in place.
- `update`:
  - update the matching row in place.
- `close`:
  - update the matching row in place and set the current issue state.
- `reopen`:
  - update the matching row in place and restore the issue state.

Issue events own the canonical issue fields. They must not overwrite pull-request-only aggregate fields with empty values unless the issue payload explicitly carries meaningful changes for those fields.

### Pull Request Events

- Only process pull request events whose payload contains one or more associated issues in `issues[]`.
- For each associated issue:
  - derive the issue key from the associated issue path, using the associated issue URL only as a fallback when needed;
  - look up the matching Bitable row;
  - if the row exists, update only the pull-request-related aggregate fields;
  - if the row does not exist, log a warning and skip the update.

Pull request events should not create issue rows on their own in the first version. This keeps row ownership simple and avoids accidental rows from incomplete association payloads.

### Pull Request Aggregate Fields

The implementation should support aggregate fields that summarize the current related pull request state on the issue row. The exact Bitable columns will be aligned later with the user’s actual schema, but the product rule is:

- one pull request should appear once in the aggregate representation;
- a later event for the same pull request should replace the prior projection for that pull request;
- the aggregate should expose enough information for weekly-report consumers to understand active or merged pull requests linked to the issue.

The simplest acceptable first-version representation is a deterministic text aggregation keyed by pull request IID, plus optional parallel fields for URLs, statuses, and last update time.

## Deduplication

The service must be idempotent for repeated deliveries.

At minimum, the implementation should persist a small local deduplication record containing enough identifiers to reject duplicate webhook requests, such as:

- `X-GitCode-Delivery`;
- payload `uuid`;
- event type;
- receive time.

The dedupe store can live in SQLite alongside the rest of the service state, or in another small local persistent store already consistent with project patterns. The key requirement is that a retried webhook delivery does not cause duplicate Bitable mutations.

## Error Handling

The handler should clearly separate invalid requests from temporary downstream failures.

- Return `4xx` for:
  - missing or invalid verification headers;
  - unsupported event types;
  - malformed JSON payloads.
- Return `2xx` for:
  - successfully processed events;
  - deduplicated replayed events;
  - intentionally skipped pull request events with no associated issues.
- Return `5xx` for:
  - Feishu token acquisition failures;
  - Bitable API failures;
  - local persistence failures that prevent safe idempotent processing.

If a pull request references an associated issue but the issue row is missing in Bitable, the handler should log a warning and still return success for the webhook after safely skipping that association update. This is a business skip, not a transport failure.

## Feishu Bitable Integration

The Bitable client should support:

- acquiring and caching the Feishu tenant access token;
- querying table fields when needed to validate mapping;
- looking up existing records by issue key;
- creating a record when an issue row does not exist;
- updating a record when an issue row already exists.

The implementation should be built around upsert-like behavior at the service layer:

1. derive issue key;
2. query Bitable for the row using that key;
3. if present, update the existing record;
4. otherwise create a new record.

This avoids hard-coding record IDs outside the Bitable layer.

## Observability

The first version should emit concise logs for:

- webhook receive start;
- verification failure;
- duplicate delivery skip;
- issue row create;
- issue row update;
- pull request aggregate update;
- skipped pull request because no associated issue row exists;
- Feishu API failure.

Logs must not print secrets or full raw payloads by default. If payload excerpts are logged for debugging, they should be bounded and scrubbed.

## Testing

Tests should cover:

- GitCode verification logic for token and, if implemented, signature modes;
- request decoding for issue and merge request webhook payloads;
- issue event normalization for `open`, `update`, `close`, and `reopen`;
- merge request normalization using the `issues[]` association array;
- duplicate delivery detection;
- issue create-vs-update decisions;
- pull request aggregate updates on an existing issue row;
- skip behavior when a pull request points to an issue row that does not exist;
- Feishu token and Bitable client error handling;
- HTTP route wiring into the existing service.

The implementation should use unit tests and small handler-level integration tests with mocked Bitable transport rather than live GitCode or Feishu calls.

## Rollout Notes

Before enabling the webhook in production, the operator will need:

- a GitCode webhook configured for issue and pull request events;
- matching verification secret settings on both sides;
- a Feishu app with permission to read and write the target Bitable table;
- the final Bitable field mapping aligned to the user’s actual table schema.

The first production validation should use a small sequence:

1. create an issue and confirm row creation;
2. edit or close the issue and confirm row update;
3. open a pull request linked through `issues[]` and confirm related pull request fields update on the issue row;
4. update or merge that pull request and confirm aggregate field replacement.

## Future Work

- Support multiple repositories or repository-specific routing.
- Persist richer event audit history for replay and backfill.
- Add optional issue auto-backfill when pull request events arrive first.
- Expose sync status in the browser dashboard.
- Add metrics for webhook volume, dedupe hit rate, and Bitable failure rate.
