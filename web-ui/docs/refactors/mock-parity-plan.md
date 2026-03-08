# Web UI Mock/Proxy/Client Parity Plan

## Scope
This plan defines a contract-aware cleanup for web-ui parity across:

- `src/lib/oarCoreClient.js` (client wrapper),
- `src/hooks.server.js` (core proxy),
- `src/routes/**/+server.js` + `src/lib/mockCoreData.js` (mock API).

Goal: reduce correctness drift and future maintenance drift without implementing every core endpoint in mocks.

## Mismatch Matrix

| Area | Current web-ui behavior | Core/contract behavior | Risk | Planned change |
|---|---|---|---|---|
| Inbox ack semantics | Mock ack removes item from in-memory `inboxItems` (`ackMockInboxItem` splices array). Route returns `200`. | Core appends `inbox_item_acknowledged` event (`201`) and inbox suppression is derived from events, not destructive delete. | Mock UI behavior diverges from real-core derivation/suppression lifecycle. | Make mock ack event-driven + non-destructive, return `201`, and filter derived inbox list by ack refs/timestamp rules. |
| Thread create + core-maintained fields | `createMockThread(...)` accepts caller-provided `thread.open_commitments`. | Core rejects `thread.open_commitments` on create/patch as core-maintained (`400 invalid_request`). | Mock accepts writes that real core rejects; tests can pass with invalid payloads. | Enforce rejection in mock thread create/patch path and align error shape/message. |
| Thread timeline for unknown thread | `GET /threads/{id}/timeline` always returns `{ events: [] }` (200 when unknown). | Core checks thread existence first and returns `404 not_found`; contract response includes `{ events, snapshots, artifacts }`. | Mock misses not-found paths and response envelope parity. | Return `404` for unknown thread; return full timeline envelope keys in success payload. |
| Proxy allowlist drift | `hooks.server.js` hardcodes pathname checks; misses `/health`, `/inbox/stream`, `/auth/*`, `/agents/me*` from contract. | Contract surface is defined by generated command metadata. | New endpoints can be forgotten; proxy fails only in integration environments. | Replace manual allowlist with contract-driven path matcher built from generated command registry. |
| Client vs mock surface gaps | `oarCoreClient` exposes commands without matching mock routes (`snapshots.get`, `events.get`, `artifacts.create`). | In same-origin mock mode these calls can 404 or behave inconsistently. | Hidden route gaps in local/mock workflows. | Introduce explicit `mock-supported` vs `proxy-only` command classification; add missing mock routes only for UI-needed paths (at minimum `snapshots.get`). |
| Manual endpoint duplication | Endpoint knowledge is duplicated across: command IDs in client, path checks in hooks, route files/mocks. | Contract metadata already exists (`contracts/gen/meta/commands.json`, generated TS registry). | High maintenance burden; drift risk increases every time contract changes. | Centralize endpoint metadata usage in shared web-ui helper(s), then test parity against generated contract metadata. |

Notes from inventory:

- Contract commands: `39`.
- `hooks.server.js` currently misses `7` contract paths: `/health`, `/inbox/stream`, `/auth/agents/register`, `/auth/token`, `/agents/me`, `/agents/me/keys/rotate`, `/agents/me/revoke`.
- `oarCoreClient` currently invokes `25` command IDs.

## Chosen Architecture (Smallest Durable)

Use a **contract-driven route catalog + explicit mock parity profile**.

### 1) Contract route catalog (single source for paths)

Add a shared helper (new module) that reads generated command metadata and exposes:

- `isContractPath(pathname)` for proxy routing,
- `command metadata by id` for parity tests,
- path-template matcher utilities.

`hooks.server.js` uses this instead of hand-maintained path chains.

### 2) Explicit mock parity profile (small surface, intentionally bounded)

Define a manifest of command IDs with categories:

- `mock_supported`: must be implemented core-faithfully in local mock mode.
- `proxy_only`: intentionally not mocked; expected to work only with `OAR_CORE_BASE_URL` proxy mode.

This avoids pretending full mock coverage while preventing accidental silent drift.

### 3) Core-faithful behavior for high-risk mocked commands

For `mock_supported` commands that are mutation- or timeline-sensitive, align semantics with core:

- status codes (`201` for create/ack flows),
- error semantics for core-maintained fields,
- existence checks and response envelope shape.

## Mock Behaviors: Must Match vs Narrow

### Must become core-faithful

- `POST /inbox/ack`: event-driven ack semantics + `201` response.
- `POST /threads` and `PATCH /threads/{thread_id}`: reject `open_commitments` writes.
- `GET /threads/{thread_id}/timeline`: unknown-thread `404` + full response envelope keys.
- `POST /events`, `POST /work_orders`, `POST /receipts`, `POST /reviews`: status code parity (`201`) and consistent error envelope conventions.

### Should be narrowed or marked proxy-only

- Auth/agent endpoints (`/auth/*`, `/agents/me*`).
- Streaming endpoints (`/events/stream`, `/inbox/stream`).
- Operational endpoints not used by mock-only UI workflows (`/health`, meta command/concept discovery) unless explicitly required for local tooling.

## Migration Order

1. Add contract-aware route catalog + matcher tests.
2. Switch `hooks.server.js` to contract matcher for proxy decisions.
3. Add mock parity profile (mock-supported vs proxy-only) + coverage test against client command IDs.
4. Implement high-risk mock behavior parity fixes:
   - inbox ack semantics,
   - thread create/patch protected fields,
   - timeline 404 + response shape,
   - snapshot mock route for `snapshots.get`.
5. Update docs/runbook with mock-surface guarantees and proxy-only operations.

## Exact Files To Touch (Implementation Ticket)

Must touch:

- `web-ui/src/hooks.server.js`
- `web-ui/src/lib/oarCoreClient.js`
- `web-ui/src/lib/mockCoreData.js`
- `web-ui/src/routes/inbox/ack/+server.js`
- `web-ui/src/routes/threads/+server.js`
- `web-ui/src/routes/threads/[threadId]/timeline/+server.js`
- `web-ui/src/routes/snapshots/[snapshotId]/+server.js` (new)
- `web-ui/tests/unit/oarCoreClient.test.js`
- `web-ui/tests/unit/mockCoreData.test.js` (new)
- `web-ui/tests/unit/proxyContractParity.test.js` (new)
- `web-ui/tests/e2e/inbox.spec.js`
- `web-ui/tests/e2e/thread-detail.spec.js`

Likely touch:

- `web-ui/src/lib/coreRouteCatalog.js` (new contract-aware helper)
- `web-ui/src/lib/mockParityProfile.js` (new)
- `web-ui/docs/runbook.md`

## Parity Tests To Add

- Unit: contract-vs-proxy parity test
  - assert every contract path is proxy-routable by `hooks.server.js` matcher.
- Unit: client-vs-mock profile test
  - assert every `oarCoreClient` command ID is classified (`mock_supported` or `proxy_only`).
- Unit: mock behavior parity
  - inbox ack is non-destructive and suppression-driven,
  - thread create/patch rejects `open_commitments`,
  - timeline unknown-thread returns `404` and success payload includes `{ events, snapshots, artifacts }`.
- E2E: inbox/thread-detail parity paths
  - inbox item remains suppressed after reload following ack,
  - timeline unknown-thread path surfaces not-found correctly in mock mode.

## Explicit Non-Goals

- Building full mock implementations for all `39` contract commands.
- Building auth UI/streaming UI features in this ticket.
- Replacing generated TS client or redesigning API abstractions broadly.
- Core/backend contract changes.
