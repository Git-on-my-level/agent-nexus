# Agent Nexus HTTP API Contract (v0.3.0)

This document defines the **concrete HTTP/JSON surface** used for integration between **anx-core** and clients (including **the web UI** and agents).

The schema of objects is defined by `../contracts/anx-schema.yaml`.

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
- Core conservatively normalizes documented user-visible markdown/prose fields before storage, including event summaries/message text, topic/board/card/document summaries, document markdown content, card revision summaries, and inbox response text. Mutation responses include `markdown_hygiene` only when normalization produced warnings; core does not recursively rewrite arbitrary JSON strings. Hard markdown hygiene failures use `invalid_markdown` only for unsupported control characters or extremely long single lines.
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

#### Workspace service JWT assertions

When a hosted **workspace service** (anx-core) calls the **control plane** (e.g. heartbeat telemetry, account status), it may authenticate with a short-lived **Ed25519 (EdDSA) JWT** in `Authorization: Bearer <jwt>`. The signing implementation lives in `core/internal/wsservicejwt` (single source of truth in this repo). **The control plane’s verifier for these assertions must track the same claim set and time windows** (`iss`/`sub`, `aud`, `iat`/`nbf`/`exp`, `workspace_id`, `purpose`); that code lives in the control plane repository, so any contract change (TTL, skew, new claims) requires coordinated updates in both places.

## API Surface Classification

Each endpoint is classified with an `x-anx-surface` extension indicating its role:

- **`canonical`**: CRUD/list/get endpoints over canonical resources (topics, cards, artifacts, documents, boards, board cards, events, packets), plus **read-only** thread list/inspect routes for backing-thread inspection. These are the durable substrate for automation.

- **`projection`**: Operator convenience surfaces that aggregate multiple canonical resources into workspace-friendly bundles. Examples: `topics.workspace` (primary operator coordination read), `threads.context`, `threads.workspace` (backing-thread diagnostic bundle), `boards.workspace`, `inbox.list/get/stream/ack`. **Do not build durable automation directly on projection payload shapes.** Use canonical APIs or CLI commands for durable substrate.

- **`utility`**: Infrastructure endpoints for liveness, readiness, version, meta discovery, auth bootstrap, maintenance, and workspace telemetry. Examples: `/health`, `/livez`, `/readyz`, `/ops/health`, `/ops/usage-summary`, `/v1/usage/summary`, `/ops/blob-usage/rebuild`, `/version`, `/meta/*`, `/auth/*`, `/actors`, `/derived/rebuild`.

Projection endpoints return a `section_kinds` field to distinguish canonical vs derived sections, and a `generated_at` timestamp indicating when the projection was generated.

## Authoritative HTTP catalog

**Do not treat this file as a per-path API list.** Machine-verifiable workspace HTTP is defined only in:

- [`contracts/anx-openapi.yaml`](../../contracts/anx-openapi.yaml) — paths, methods, request/response schemas, and `x-anx-surface` / `x-anx-command-id`.
- Generated references: [`contracts/gen/meta/commands.json`](../../contracts/gen/meta/commands.json) (structured metadata) and [`contracts/gen/docs/commands.md`](../../contracts/gen/docs/commands.md) (human-oriented command index).

Drift from the live router is gated in CI: `core` runs `TestExactRegisterRoutesCoveredByOpenAPOrExceptions`, which requires every `registerRoute(..., exactRouteAccess(...))` entry in `handler.go` to map to **OpenAPI-derived** commands or to an explicit row in [`contracts/non-openapi-endpoints.yaml`](../../contracts/non-openapi-endpoints.yaml).

### Narrative notes (not exhaustive)

- **Home unread feed**: `GET /home/unread` returns high-signal `home_feed`
  events grouped by topic for the authenticated principal. Read state is durable
  in core as per-topic cursors keyed by reader identity. `POST /home/read`
  advances one topic cursor (`topic_id`) or every currently visible topic cursor
  (`topic_ids`). Opening a topic may mark that topic read from the UI after a
  successful topic load; cursor writes do not emit synthetic events.
- **Events history**: `GET /events` is the complete event browser API. It
  supports the shared `preset=home_feed`, `event_group`, `backing_scope`,
  type/topic/thread/actor/search/time filters, and cursor pagination. Event
  types are strict contract values.
- **CLI version gate**: Clients may send `X-ANX-CLI-Version`. When below minimum compatibility, core responds with `426` and `cli_outdated` except on a small set of public/meta/auth bootstrap routes; see `x-anx-*` and handler logic — exact allowlist is in OpenAPI and code, not duplicated here.
- **Document body updates**: Canonical write is `POST /docs/{document_id}/revisions` (`docs.revisions.create`). There is no `PATCH /docs/{document_id}` on workspace core.
- **List-only canonical enrichments**: `GET /topics` may include `timeline_message_count`; `GET /documents` may include `revision_count`, `timeline_message_count`, and `head_revision_character_count`. These are derived read hints for list rows, not writable fields. Message counts include non-trashed `message_posted` events on the backing `thread_id`. Character counts are best-effort UTF-8 rune counts of the decoded head revision body and may be omitted for large or unavailable blobs.
- **Packets**: Receipts and reviews are created via `POST /packets/receipts` and `POST /packets/reviews` only.
- **Cards**: Patch, move, and archive use first-class `PATCH /cards/{card_id}`, `POST /cards/{card_id}/move`, and `POST /cards/{card_id}/archive` (or trash/restore/purge as documented in OpenAPI). Board-scoped duplicate paths have been removed. **Batch card create** is `POST /boards/{board_id}/cards/batch` (`boards.cards.batch_add`): one `if_board_updated_at`, many `items`, single transaction. Assigning a registered **agent** as the card assignee (via `assignee_refs`) enqueues an **agent wakeup**, visible to that agent as an **agent notification** (`GET /agent-notifications`).
- **Card timeline vs. Discussion (intentional split)**: `GET /cards/{card_id}/timeline` (`cards.timeline`) is the card's **lifecycle/audit log** — it returns only `card_*` events for the card and intentionally omits `message_posted`, even when a message carries a `card:<id>` ref. A card's **Discussion** (messages) lives on the card's backing thread and is served by `GET /threads/{thread_id}/timeline` using the card's `thread_id`. This mirrors the unified message-on-thread model used by boards, topics, and documents: every primitive's Discussion is `message_posted` on its backing thread; the per-primitive timeline endpoints expose lifecycle/audit, not the conversation.
- **SSE**: `GET /stream/events`, `GET /stream/inbox`, and `GET /stream/agent-notification-receipts` use `text/event-stream`; see OpenAPI `x-anx-input-mode` / streaming metadata.

## Derived projections (materialized views)

- Materialized derived projections used by the common read path:
  - `derived_inbox_items`: asynchronously maintained inbox items keyed by deterministic `inbox_item_id`, with per-thread rows used by `GET /inbox`, `GET /inbox/{id}`, `GET /stream/inbox`, and thread workspace inbox sections. The deprecated `risk_horizon_days` query parameter is validated for compatibility but does not trigger read-time recomputation; risk horizon semantics are defined by the configured projection maintainer.
  - `agent_notification` is a derived per-target-agent view built from the `agent_wakeups` queue table and per-wakeup notification status.
  - `derived_topic_views`: asynchronously maintained per-thread stale/workspace summaries used by thread list stale indicators and thread workspace summary surfaces.
  - `topic_projection_refresh_status`: durable per-thread refresh state used to expose `current`, `pending`, `missing`, or `error` freshness metadata without mutating projections inside GET handlers.
- `POST /derived/rebuild` remains the deterministic repair path: it rebuilds projection tables from current topics/events/cards/documents without inventing lifecycle or staleness events.
  - Standard GET responses never repair or recompute projections inline; they return the best currently materialized data plus freshness metadata.

- Meaningful topic activity for stale-topic clearing:
  - The current activity set is explicit: topic/card/document/board lifecycle events, `message_posted`, `receipt_added`, `review_completed`, `human_attention_requested`, `human_attention_responded`, and `exception_raised`, plus non-create topic/card edits that materially change operator-authored state.
  - Coordination noise does not count as activity: agent notification read/dismissal, topic-creation bookkeeping, and derived projection maintenance.
- Topic, board, and card backing-thread linkage is exposed through `thread_id` on the canonical resource shape; keeping those backing links synchronized no longer emits an operator-visible timeline event or bumps the topic’s visible update clock.
