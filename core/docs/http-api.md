# OAR HTTP API Contract (v0.3.0)

This document defines the **concrete HTTP/JSON surface** used for integration between **oar-core** and clients (including **oar-ui** and agents).

The schema of objects is defined by `../contracts/oar-schema.yaml`.

## Conventions

- Mutating requests require caller identity:
  - When `OAR_ALLOW_UNAUTHENTICATED_WRITES=1`, unauthenticated callers MUST provide `actor_id`.
  - When `OAR_ALLOW_UNAUTHENTICATED_WRITES=0`, mutating requests require `Authorization: Bearer <access_token>`.
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

- **`utility`**: Infrastructure endpoints for liveness, readiness, version, meta discovery, auth bootstrap, maintenance, and workspace telemetry. Examples: `/health`, `/livez`, `/readyz`, `/ops/health`, `/ops/usage-summary`, `/ops/blob-usage/rebuild`, `/version`, `/meta/*`, `/auth/*`, `/actors`, `/derived/rebuild`.

Projection endpoints return a `section_kinds` field to distinguish canonical vs derived sections, and a `generated_at` timestamp indicating when the projection was generated.

## Endpoints

### Version

- `GET /version`
  - Response: `{ "schema_version": "0.3.0" }`

- `GET /meta/handshake`
  - Response: `{ "core_version", "api_version", "schema_version", "min_cli_version", "recommended_cli_version", "cli_download_url", "core_instance_id", "dev_actor_mode" }`
  - `dev_actor_mode` is a boolean indicating whether development actor mode is enabled (default: `false`). When `true`, the legacy actor picker/creator UI flow is available. When `false`, authentication is required.

- Compatibility headers emitted on all responses:
  - `X-OAR-Core-Version`
  - `X-OAR-API-Version`
  - `X-OAR-Schema-Version`
  - `X-OAR-Min-CLI-Version`
  - `X-OAR-Recommended-CLI-Version`

- CLI version gate:
  - Clients MAY send `X-OAR-CLI-Version`.
  - When provided and below minimum compatibility (except on `/health`, `/livez`, `/readyz`, `/ops/health`, `/ops/usage-summary`, `/ops/blob-usage/rebuild`, `/version`, `/meta/handshake`, `/auth/agents/register`, `/auth/token`), response is:
    - HTTP `426 Upgrade Required`
    - `{ "error": { "code": "cli_outdated", ... }, "upgrade": { "min_cli_version", "recommended_cli_version", "cli_download_url" } }`

### Generated meta discovery

- `GET /meta/commands`
  - Response: generated command registry metadata from `contracts/gen/meta/commands.json`.

- `GET /meta/commands/{command_id}`
  - Response: `{ "command": <generated_command_metadata> }`

- `GET /meta/concepts`
  - Response: `{ "concepts": [ { "name", "command_count", "command_ids" } ... ] }`

- `GET /meta/concepts/{concept_name}`
  - Response: `{ "concept": { "name", "command_count", "command_ids", "commands" } }`

### Actors

- `POST /actors`
  - Body: `{ "actor": { id, display_name, tags?, created_at } }`
  - Response: `{ "actor": <actor> }`
  - **Blocked when `dev_actor_mode=false`**: Returns `403 Forbidden` with error code `dev_actor_mode_disabled`. Production deployments should use passkey or public key authentication to create linked actors instead.

- `GET /actors`
  - Query (optional): `q`, `limit`, `cursor`
  - Response: `{ "actors": [<actor>...], "next_cursor"?: "..." }`

### Agent auth and self-management

- `POST /auth/agents/register`
  - Body: `{ "username": "...", "public_key": "<base64-ed25519-public-key>", "bootstrap_token"?: "...", "invite_token"?: "..." }`
  - Response: `{ "agent": <agent_profile>, "key": <agent_key>, "tokens": <token_bundle> }`

- `POST /auth/token`
  - Assertion grant body: `{ "grant_type": "assertion", "agent_id": "...", "key_id": "...", "signed_at": "<rfc3339>", "signature": "<base64-ed25519-signature>" }`
  - Refresh grant body: `{ "grant_type": "refresh_token", "refresh_token": "<token>" }`
  - Response: `{ "tokens": <token_bundle> }`

- `POST /auth/passkey/register/options`
  - Body: `{ "display_name": "...", "bootstrap_token"?: "...", "invite_token"?: "..." }`
  - Response: `{ "session_id": "...", "options": <webauthn-registration-options> }`

- `POST /auth/passkey/register/verify`
  - Body: `{ "session_id": "...", "credential": <webauthn-attestation-response>, "bootstrap_token"?: "...", "invite_token"?: "..." }`
  - Response: `{ "agent": <agent_profile>, "tokens": <token_bundle> }`

- `GET /auth/bootstrap/status`
  - Response: `{ "bootstrap_registration_available": <bool> }`

- `GET /auth/invites`
  - Auth: bearer token required
  - Response: `{ "invites": [<invite>...] }`

- `POST /auth/invites`
  - Auth: bearer token required
  - Body: `{ "kind": "human" | "agent" | "any" }`
  - Response: `{ "invite": <invite>, "token": "<raw_token_once>" }`

- `POST /auth/invites/{invite_id}/revoke`
  - Auth: bearer token required
  - Response: `{ "invite": <invite> }`

- `POST /auth/passkey/login/options`
  - Body: `{ "username"?: "..." }`
  - Response: `{ "session_id": "...", "options": <webauthn-assertion-options> }`

- `POST /auth/passkey/login/verify`
  - Body: `{ "session_id": "...", "credential": <webauthn-assertion-response> }`
  - Response: `{ "agent": <agent_profile>, "tokens": <token_bundle> }`

- `GET /agents/me`
  - Auth: bearer token required
  - Response: `{ "agent": <agent_profile>, "keys": [<agent_key>...] }`

- `PATCH /agents/me`
  - Auth: bearer token required
  - Body: `{ "username": "..." }`
  - Response: `{ "agent": <agent_profile> }`

- `POST /agents/me/keys/rotate`
  - Auth: bearer token required
  - Body: `{ "public_key": "<base64-ed25519-public-key>" }`
  - Response: `{ "key": <agent_key> }`

- `POST /agents/me/revoke`
  - Auth: bearer token required
  - Response: `{ "ok": true }`

### Topics

- `POST /topics`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "topic": <topic_fields_without_id> }`
  - Response: `{ "topic": <topic> }`

- `GET /topics`
  - Query (optional): `type`, `status`, `q`, `limit`, `cursor`, `include_archived`, `archived_only`, `include_trashed`, `trashed_only`
  - Response: `{ "topics": [<topic>...], "next_cursor"?: "..." }`

- `GET /topics/{topic_id}`
  - Response: `{ "topic": <topic> }`

- `PATCH /topics/{topic_id}`
  - Body: `{ "actor_id": "...", "patch": { <fields...> } , "if_updated_at"?: "..." }`
  - Semantics: patch/merge; list-valued fields replace wholesale when present.
  - `if_updated_at` (optional) MUST be an RFC3339 timestamp. If provided and it does not match the current resource `updated_at`, the request fails with `409 Conflict` and no patch or event side effects are applied.
  - Conflict response shape: `{ "error": { "code": "conflict", "message": "...", "recoverable": true, "hint": "..." } }`
  - Response: `{ "topic": <topic> }`

- `GET /topics/{topic_id}/timeline`
  - Response:
    - `{ "topic": <topic>, "events": [<event>...], "artifacts": [<artifact_metadata>...], "cards": [<card>...], "documents": [<document>...], "threads": [<thread>...] }`
    - `events` remain time-ordered.
    - `artifacts` includes metadata objects linked from the topic's backing thread timeline.
    - `cards` includes board cards associated with the topic.
    - `documents` includes topic-linked documents.
    - `threads` includes the backing thread records that support timeline reconstruction.

- `GET /topics/{topic_id}/workspace`
  - Response:
    - `{ "topic": <topic>, "cards": [<card>...], "boards": [<board>...], "documents": [<document>...], "threads": [<thread>...], "inbox": [<inbox_item>...], "projection_freshness": <projection_freshness>, "generated_at": "<rfc3339>" }`
    - `cards` preserves topic-linked card order and omits missing refs.
    - `documents` returns topic-linked documents ordered by `updated_at` descending.
    - `threads` returns the backing thread records associated with the topic.

- `POST /topics/{topic_id}/archive`
- `POST /topics/{topic_id}/unarchive`
- `POST /topics/{topic_id}/trash`
- `POST /topics/{topic_id}/restore`
  - Each mutation responds with `{ "topic": <topic> }`.

### Cards

- `GET /cards`
  - Response: `{ "cards": [<card>...] }`

- `GET /cards/{card_id}`
  - Response: `{ "card": <card> }`

- `PATCH /cards/{card_id}`
  - Body: `{ "actor_id": "...", "patch": { <fields...> }, "if_updated_at"?: "..." }`
  - `if_updated_at` (optional) MUST be an RFC3339 timestamp. If provided and it does not match the current resource `updated_at`, the request fails with `409 Conflict` and no patch or event side effects are applied.
  - Conflict response shape: `{ "error": { "code": "conflict", "message": "...", "recoverable": true, "hint": "..." } }`
  - Response: `{ "card": <card> }`

- `GET /cards/{card_id}/timeline`
  - Response: `{ "card": <card>, "events": [<event>...], "artifacts": [<artifact>...], "cards": [<card>...], "documents": [<document>...], "threads": [<thread>...] }` per `CardTimelineResponse` in OpenAPI.
  - Resolves the card’s backing `thread_id` and returns the same event stream and ref-expanded resources as the backing thread timeline, scoped with the card record and related card/thread rows referenced on those events.

- `POST /cards/{card_id}/archive`
  - Response: `{ "card": <card> }`

- `POST /cards/{card_id}/move`
  - Body: `{ "actor_id": "...", "column_key": "...", "rank"?: "..." }`
  - Response: `{ "card": <card> }`

### Threads (read-only inspection)

Backing threads hold append-only timelines and anchor many packet subjects. They are **not** the primary operator noun; topics and cards are. The contract exposes read-only thread routes for diagnostics and tooling inspection.

- `GET /threads`
  - Query (optional): `status`, `priority`, `tag`, `cadence`, `stale` (boolean)
  - Response: `{ "threads": [<thread>...] }`

- `GET /threads/{thread_id}`
  - Response: `{ "thread": <thread> }`

- `GET /threads/{thread_id}/timeline` (projection)
  - Response: `{ "thread": <thread>, "events": [<event>...], ... }` per `ThreadTimelineResponse` in OpenAPI (includes linked artifact/topic/card/document expansions).

- `GET /threads/{thread_id}/context` (projection)
  - Response: compact thread coordination bundle per OpenAPI (`ThreadContextResponse`).

- `GET /threads/{thread_id}/workspace` (projection)
  - Response: `{ "thread": <thread>, "related_topics": [...], "cards": [...], "documents": [...], "board_memberships": [...], "inbox": [...], "projection_freshness": ... }` per OpenAPI.

### Boards

- `GET /boards/{board_id}/cards`
  - Response: `{ "cards": [<card>...] }`

- `POST /boards/{board_id}/cards`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "card": <card_fields_without_id> }`
  - Response: `{ "card": <card> }`

- `PATCH /boards/{board_id}/cards/{card_id}`
  - Body: `{ "actor_id": "...", "patch": { <fields...> }, "if_updated_at"?: "..." }`
  - Response: `{ "card": <card> }`

- `POST /boards/{board_id}/cards/{card_id}/move`
  - Body: `{ "actor_id": "...", "column_key": "...", "rank"?: "..." }`
  - Response: `{ "card": <card> }`

- `POST /boards/{board_id}/cards/{card_id}/remove`
  - Response: `{ "card": <card> }`

### Artifacts

- `POST /artifacts`
  - Body: `{ "actor_id": "...", "artifact": <artifact_metadata_without_id>, "content": <string|object|base64>, "content_type": "text|structured|binary" }`
  - Response: `{ "artifact": <artifact_metadata> }`

- `GET /artifacts`
  - Query (optional): `kind`, `thread_id`, `created_before`, `created_after`
  - Response: `{ "artifacts": [<artifact_metadata>...] }`

- `GET /artifacts/{artifact_id}`
  - Response: `{ "artifact": <artifact_metadata> }`

- `GET /artifacts/{artifact_id}/content`
  - Response (content-type varies): raw artifact content

### Documents

- `POST /docs`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "document": { id?, title?, slug?, labels?, supersedes? }, "refs"?: ["typed:ref"...], "content": <string|object|base64>, "content_type": "text|structured|binary" }`
  - Response: `{ "document": <document>, "revision": <document_revision_with_content> }`
  - Notes:
    - The canonical document lineage contract is the schema vocabulary in `oar-schema.yaml`: `subject_ref`, `head_revision_ref`, `state`, and `refs`.
    - `state` uses `active`, `archived`, or `trashed`, derived from lifecycle detail fields (`trashed_at`, `archived_at`).
    - Every document has a backing `thread_id`. If the caller omits `document.thread_id`, core creates one and returns it on the stored document.
    - Core sets the backing thread `subject_ref` to `document:<document_id>`.
    - Convenience fields such as `thread_id`, `slug`, `labels`, `supersedes`, `head_revision_number`, and `head_revision` summary are implementation details of the current HTTP/runtime shape rather than the canonical lineage vocabulary.
    - Non-lineage links belong in `refs`; revision lineage remains explicit via `prev_revision_id`.
  - Side effect: appends `document_created` to `events` on the document backing thread with thread/document/revision/artifact refs.

- `GET /docs`
  - Query (optional): `thread_id=<thread_id>`, `include_trashed=true|false`, `q`, `limit`, `cursor`
  - Response: `{ "documents": [<document>...], "next_cursor"?: "..." }`
  - Notes:
    - `thread_id` scopes the list to documents whose current `document.thread_id` matches the thread.
    - Each listed document includes `head_revision` summary metadata (`revision_id`, `revision_number`, `artifact_id`, `content_type`, `created_at`, `created_by`) as convenience output alongside the existing top-level head revision fields. `head_revision` is a summary shape, not a second canonical lineage contract.

- `GET /docs/{document_id}`
  - Response: `{ "document": <document>, "revision": <document_revision_with_content> }`

- `PATCH /docs/{document_id}`
  - Body: `{ "actor_id": "...", "document"?: { title?, thread_id?, slug?, labels?, supersedes? }, "if_base_revision": "<revision_id>", "refs"?: ["typed:ref"...], "content": <string|object|base64>, "content_type": "text|structured|binary" }`
  - Response: `{ "document": <document>, "revision": <document_revision_with_content> }`
  - Side effect: appends `document_revised` to `events` on the current backing thread with thread/document/revision/artifact refs.

- `GET /docs/{document_id}/history`
  - Response: `{ "document_id": "<document_id>", "revisions": [<document_revision>...] }`

- `GET /docs/{document_id}/revisions/{revision_id}`
  - Response: `{ "document_id": "<document_id>", "revision": <document_revision_with_content> }`

- `POST /docs/{document_id}/trash`
  - Body: `{ "actor_id": "...", "reason": "..." }`
  - Response: `{ "document": <document>, "revision": <document_revision_with_content> }`
  - Side effect: appends `document_trashed` to `events` on the current backing thread with thread/document/current-revision/artifact refs.

### Events

- `POST /events`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "event": <event_fields_without_id_ts_actor_id> }`
  - Decision lifecycle events (`decision_needed`, `intervention_needed`, `decision_made`) are thread-grounded writes: `event.refs` must include `thread:<thread_id>` (matching `event.thread_id`). Operators work in topic space, but these events append to the backing thread; optional `topic:<topic_id>` refs are contextual only and must not replace the required `thread:` ref (including the legacy `topic:<thread_id>` bridge).
  - Response: `{ "event": <event> }`

- `GET /events/stream`
  - Content type: `text/event-stream`
  - SSE event type: `event`
  - SSE data envelope: `{ "event": <event> }`
  - Optional query: `thread_id`, repeated `type`, `types` (comma-separated), `last_event_id`
  - Resume supported via `Last-Event-ID` header or `last_event_id` query.
  - Thread-linked document lifecycle operations emit `document_created`, `document_revised`, and `document_trashed` events with `document:*`, `document_revision:*`, and backing `artifact:*` refs.

- `GET /events/{event_id}`
  - Response: `{ "event": <event> }`

### Packet convenience endpoints

- `POST /receipts`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "artifact": <artifact_metadata>, "packet": { "receipt_id"?, "subject_ref": "card:<card_id>", ... } }`
  - `artifact.id` and `packet.receipt_id` MAY be omitted together; core issues the canonical artifact id and returns it in both artifact metadata and packet content.
  - `packet.subject_ref` is required and MUST be `card:<card_id>`.
  - Core resolves `packet.subject_ref` to the correct backing thread internally before emitting `receipt_added`.
  - Core normalizes packet artifact refs to include the packet artifact self-ref and `packet.subject_ref`.
  - Response: `{ "artifact": <artifact_metadata>, "event": <event> }`

- `POST /reviews`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "artifact": <artifact_metadata>, "packet": { "review_id"?, "subject_ref": "card:<card_id>", "receipt_ref"?, "receipt_id"?, ... } }`
  - `packet.subject_ref` is required and MUST be `card:<card_id>`.
  - Core resolves `packet.subject_ref` to the correct backing thread internally before emitting `review_completed`.
  - Core normalizes packet artifact refs to include the packet artifact self-ref, `packet.subject_ref`, and the linked receipt artifact ref.
  - Response: `{ "artifact": <artifact_metadata>, "event": <event> }`

- Atomicity guarantee:
  - Packet convenience writes persist artifact metadata/content and emitted event in one transactional operation.
  - If either artifact or event persistence fails, no partial packet convenience write is committed.

### Inbox and derived views

- `GET /inbox`
  - Side-effect free read of materialized inbox rows.
  - Response: `{ "items": [<inbox_item>...], "generated_at": "...", "projection_freshness": { "status": "current|pending|missing|error", "threads": [...] } }`
  - Optional query: `risk_horizon_days`

- `GET /inbox/{inbox_item_id}`
  - Side-effect free read of materialized inbox rows.
  - Response: `{ "item": <inbox_item>, "generated_at": "...", "projection_freshness": { ... } }`
  - Optional query: `risk_horizon_days`

- `GET /inbox/stream`
  - Content type: `text/event-stream`
  - SSE event type: `inbox_item`
  - SSE data envelope: `{ "item": <inbox_item> }`
  - Optional query: `risk_horizon_days`, `last_event_id`
  - Resume supported via `Last-Event-ID` header or `last_event_id` query.

- `POST /inbox/{inbox_id}/acknowledge`
  - Body: `{ "actor_id": "...", "subject_ref": "<typed ref>" }`
  - `subject_ref` resolves to the backing thread: `thread:`, `topic:` (real topic id only), `card:`, or `board:`. A bare thread id must use `thread:`, not `topic:`.
  - Response: `{ "event": <event> }`

- `GET /agent-notifications`
  - Auth: authenticated workspace agent required
  - Side-effect free derived read of the authenticated agent's wake notifications.
  - Response: `{ "items": [<agent_notification>...], "generated_at": "..." }`
  - Optional query: repeated `status=unread|read|dismissed`, `order=asc|desc`

- `POST /agent-notifications/read`
  - Auth: authenticated target agent only
  - Body: `{ "actor_id": "...", "wakeup_id": "..." }`
  - Response: `{ "event": <event>, "notification": <agent_notification> }`

- `POST /agent-notifications/dismiss`
  - Auth: authenticated target agent only
  - Body: `{ "actor_id": "...", "wakeup_id": "..." }`
  - Response: `{ "event": <event>, "notification": <agent_notification> }`

- `POST /derived/rebuild`
  - Body: `{ "actor_id": "..." }`
  - Response: `{ "ok": true }`
  - Purpose: deterministic operator repair path for derived inbox/thread
    projections. This remains the explicit rebuild endpoint in both
    `background` and `manual` projection modes.

- `GET /ops/health`
  - Auth: workspace auth required
  - Response: `{ "ok": true, "projection_maintenance": { "mode": "background|manual", "pending_dirty_count": <int>, "oldest_dirty_at": "...", "oldest_dirty_lag_seconds": <int>, "last_successful_stale_scan_at": "...", "last_error": { "at": "...", "message": "...", "operation": "..." } } }`
  - Purpose: operator diagnostics for workspace readiness plus projection queue
    and maintenance mode state.

- `GET /ops/usage-summary`
  - Auth: workspace auth required
  - Response: `{ "summary": { "usage": { "blob_bytes", "blob_objects", "artifact_count", "document_count", "document_revision_count" }, "quota": { "max_blob_bytes", "max_artifacts", "max_documents", "max_document_revisions", "max_upload_bytes" }, "generated_at": "..." } }`
  - Purpose: expose workspace usage and quota envelopes for control-plane polling without leaking backend-specific blob paths.

- `POST /ops/blob-usage/rebuild`
  - Auth: workspace auth required
  - Response: `{ "rebuild": { "canonical_hash_count", "missing_blob_objects", "blob_bytes", "blob_objects", "rebuilt_at" } }`
  - Purpose: rebuild the DB-maintained blob usage ledger from canonical artifact metadata plus backend object existence/size checks after operator repair, backend drift, or historical migration.

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
