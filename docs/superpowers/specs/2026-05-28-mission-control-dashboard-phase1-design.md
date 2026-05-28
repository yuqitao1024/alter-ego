# Mission Control Dashboard Phase 1 Design

## Scope

Phase 1 introduces a browser dashboard for Alter Ego with a Mission Control inspired shell. This phase is intentionally narrow:

- reuse Mission Control frontend code and visual patterns only
- keep the existing Go service as the source of truth
- gate browser access with Lark OAuth
- expose only mock dashboard data in the browser
- deploy behind Caddy on a configurable public origin such as `https://dashboard.example.com`

This phase does not replace the Lark bot workflow, does not expose real task APIs yet, and does not move orchestration logic out of the Go service.

## Routes

Public browser routes:

- `GET /`
- `GET /login`

Backend-owned routes:

- `GET /auth/lark/start`
- `GET /auth/lark/callback`
- `POST /auth/logout`
- `GET /api/web/session`
- `GET /api/web/mock/tasks`
- `POST /lark/card/callback`

## Runtime Topology

Phase 1 runs three processes:

1. `alteregod` for Lark bot handling, task orchestration, browser session management, Lark OAuth, and mock dashboard APIs
2. `alterego-web` for the vendored Next.js dashboard shell
3. `caddy` as the public reverse proxy on a configurable browser listener

Caddy proxies `/api/*`, `/auth/*`, and `/lark/*` to the Go service. All other browser paths go to the frontend.

## Auth Model

Browser login uses the Lark OAuth authorization code flow:

1. the frontend sends the operator to `/auth/lark/start`
2. the Go service issues a short-lived one-time `state`
3. Lark redirects back to `/auth/lark/callback`
4. the Go service exchanges `code` for an access token
5. the Go service fetches the Lark user profile
6. only configured allowed `open_id` values may create a session
7. the Go service stores the authenticated browser session in a signed cookie

The OAuth `state` is server-issued and one-time consumed. The browser never holds app secrets.

## Frontend Shape

The frontend stays intentionally thin in phase 1:

- `/login` renders a Lark sign-in page
- `/` renders a protected dashboard shell
- the shell reads `/api/web/session`
- the shell reads `/api/web/mock/tasks`
- task cards, task table, and detail panel are rendered from the mock payload only

The frontend should not implement its own auth backend, task backend, or websocket orchestration logic in phase 1.

## Testing

Verification for this phase:

- Go unit tests for auth/session/web handlers
- Go startup tests for unified HTTP route wiring
- frontend production build succeeds
- package build includes the Go binary, frontend runtime assets, and reverse proxy templates

## Follow-up

Phase 2 will replace mock dashboard endpoints with real task APIs and prune unused vendored frontend modules once the browser workflow is proven.
