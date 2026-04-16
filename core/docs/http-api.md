# OAR HTTP API Contract (v0.3.0)

This document defines the **concrete HTTP/JSON surface** used for integration between **oar-core** and clients (including **oar-ui** and agents).

The schema of objects is defined by `../contracts/oar-schema.yaml`.

## Conventions

- Mutating requests require caller identity:
  - Mutating requests require `Authorization: Bearer <access_token>`.
  - Authenticated callers MAY omit `actor_id`; core infers it from the bearer token principal.
  - If authenticated callers provide `actor_id`, it MUST match the authenticated principal mapping.
- All timestamps are ISO-8601 strings.
- Objects MUST preserve unknown fields (additive evolution).
- `refs` values MUST be typed ref strings per `ref_format`.
- Error responses use a stable envelope:
  - `{ "error": { "code": "...", "message": "...", "recoverable": <bool>, "hint": "..." } }`
- Request-size, quota, and abuse-control failures use explicit stable codes:
  - `request_too_large` with HTTP `413` and a `request_body.limit_bytes` detail when the request body exceeds the configured limit.
  - `workspace_quota_exceeded` with HTTP `507` and a `quota` detail object containing `metric`, `limit`, `current`, and `projected` when a workspace write would exceed configured storage or count limits.
  - `rate_limited` with HTTP `429`, a `Retry-After` header, and a `rate_limit` detail object containing `bucket` and `retry_after_seconds`.
- Create-heavy write endpoints accept optional `request_key` for replay-safe retries.
  - Reusing the same `request_key` with the same request body replays the original successful response instead of creating duplicates.
  - Reusing the same `request_key` with a different request body returns `409 Conflict`.

### Agent auth conventions

- Access tokens are passed as `Authorization: Bearer <access_token>`.
- First-principal registration is bootstrap-token gated via `POST /auth/agents/register` or the passkey registration endpoints.
- Once the first principal exists, further registration requires a valid invite token.
- `GET /auth/bootstrap/status` exposes whether bootstrap registration is still available.
- Passkey auth is available via:
  - `POST /auth/passkey/register/options`
  - `POST /auth/passkey/register/verify`
  - `POST /auth/passkey/login/options`
  - `POST /auth/passkey/login/verify`
- `POST /auth/token` supports:
  - `grant_type=assertion` using an Ed25519 key assertion
  - `grant_type=refresh_token` using a refresh token
- Refresh tokens are rotated on successful refresh.
- Stable auth error codes include:
  - `username_taken`
  - `auth_required`
  - `invalid_token`
  - `agent_revoked`
  - `key_mismatch`

## API Surface Classification

Each endpoint is classified with an `x-oar-surface` extension indicating its role:

- **`canonical`**: CRUD/list/get endpoints over canonical resources (topics, cards, artifacts, documents, boards, board cards, events, packets), plus **read-only** thread list/inspect routes for backing-thread inspection. These are the durable substrate for automation.

- **`projection`**: Operator convenience surfaces that aggregate multiple canonical resources into workspace-friendly bundles. Examples: `topics.workspace` (primary operator coordination read), `threads.context`, `threads.workspace` (backing-thread diagnostic bundle), `boards.workspace`, `inbox.list/get/stream/ack`. **Do not build durable automation directly on projection payload shapes.** Use canonical APIs or CLI commands for durable substrate.

- **`utility`**: Infrastructure endpoints for liveness, readiness, version, meta discovery, auth bootstrap, maintenance, and workspace telemetry. Examples: `/health`, `/livez`, `/readyz`, `/ops/health`, `/ops/usage-summary`, `/v1/usage/summary`, `/ops/blob-usage/rebuild`, `/version`, `/meta/*`, `/auth/*`, `/actors`, `/derived/rebuild`.

Projection endpoints return a `section_kinds` field to distinguish canonical vs derived sections, and a `generated_at` timestamp indicating when the projection was generated.

## Authoritative HTTP catalog

**Do not treat this file as a per-path API list.** Machine-verifiable workspace HTTP is defined only in:

- [`contracts/oar-openapi.yaml`](../../contracts/oar-openapi.yaml) — paths, methods, request/response schemas, and `x-oar-surface` / `x-oar-command-id`.
- Generated references: [`contracts/gen/meta/commands.json`](../../contracts/gen/meta/commands.json) (structured metadata) and [`contracts/gen/docs/commands.md`](../../contracts/gen/docs/commands.md) (human-oriented command index).

Drift from the live router is gated in CI: `core` runs `TestExactRegisterRoutesCoveredByOpenAPOrExceptions`, which requires every `registerRoute(..., exactRouteAccess(...))` entry in `handler.go` to map to **OpenAPI-derived** commands or to an explicit row in [`contracts/non-openapi-endpoints.yaml`](../../contracts/non-openapi-endpoints.yaml).

### Narrative notes (not exhaustive)

- **CLI version gate**: Clients may send `X-OAR-CLI-Version`. When below minimum compatibility, core responds with `426` and `cli_outdated` except on a small set of public/meta/auth bootstrap routes; see `x-oar-*` and handler logic — exact allowlist is in OpenAPI and code, not duplicated here.
- **Document body updates**: Canonical write is `POST /docs/{document_id}/revisions` (`docs.revisions.create`). There is no `PATCH /docs/{document_id}` on workspace core.
- **Packets**: Receipts and reviews are created via `POST /packets/receipts` and `POST /packets/reviews` only.
- **Cards**: Patch, move, and archive use first-class `PATCH /cards/{card_id}`, `POST /cards/{card_id}/move`, and `POST /cards/{card_id}/archive` (or trash/restore/purge as documented in OpenAPI). Board-scoped duplicate paths have been removed. **Batch card create** is `POST /boards/{board_id}/cards/batch` (`boards.cards.batch_add`): one `if_board_updated_at`, many `items`, single transaction.
- **SSE**: `GET /events/stream` and `GET /inbox/stream` use `text/event-stream`; see OpenAPI `x-oar-input-mode` / streaming metadata.

## Derived projections (materialized views)

- Materialized derived projections used by the common read path:
  - `derived_inbox_items`: asynchronously maintained inbox items keyed by deterministic `inbox_item_id`, with per-thread rows used by `GET /inbox`, `GET /inbox/{id}`, and thread workspace inbox sections.
  - `agent_notification` is a derived per-target-agent view built from canonical `agent_wakeup_requested`, `agent_notification_read`, and `agent_notification_dismissed` events.
  - `derived_topic_views`: asynchronously maintained per-thread stale/workspace summaries used by thread list stale indicators and thread workspace summary surfaces.
  - `topic_projection_refresh_status`: durable per-thread refresh state used to expose `current`, `pending`, `missing`, or `error` freshness metadata without mutating projections inside GET handlers.
- `POST /derived/rebuild` remains the deterministic repair path: it re-emits any missing canonical stale-topic exceptions from canonical state, then rebuilds both projection tables from current topics/events/cards/documents.
  - Standard GET responses never repair or recompute projections inline; they return the best currently materialized data plus freshness metadata.

- Meaningful topic activity for stale-topic clearing:
  - The current activity set is explicit: `actor_statement`, `topic_created`, `topic_updated`, `topic_status_changed`, `card_created`, `card_updated`, `card_moved`, `card_resolved`, `decision_needed`, `intervention_needed`, `decision_made`, `receipt_added`, `review_completed`, `document_created`, `document_revised`, `document_trashed`, `board_created`, `board_updated`, plus any non-create topic/card edits that materially change operator-authored state.
  - Coordination noise does not count as activity: inbox acknowledgments, exception notifications, topic-creation bookkeeping, and derived board/card membership maintenance.
- Topic, board, and card backing-thread linkage is exposed through `thread_id` on the canonical resource shape; keeping those backing links synchronized no longer emits an operator-visible timeline event or bumps the topic’s visible update clock.
