# OAR HTTP API Contract (v0)

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
- Create-heavy write endpoints accept optional `request_key` for replay-safe retries.
  - Reusing the same `request_key` with the same request body replays the original successful response instead of creating duplicates.
  - Reusing the same `request_key` with a different request body returns `409 Conflict`.

### Agent auth conventions

- Access tokens are passed as `Authorization: Bearer <access_token>`.
- Registration is open in v0 via `POST /auth/agents/register`.
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

## Endpoints

### Version

- `GET /version`
  - Response: `{ "schema_version": "0.2.2" }`

- `GET /meta/handshake`
  - Response: `{ "core_version", "api_version", "schema_version", "min_cli_version", "recommended_cli_version", "cli_download_url", "core_instance_id" }`

- Compatibility headers emitted on all responses:
  - `X-OAR-Core-Version`
  - `X-OAR-API-Version`
  - `X-OAR-Schema-Version`
  - `X-OAR-Min-CLI-Version`
  - `X-OAR-Recommended-CLI-Version`

- CLI version gate:
  - Clients MAY send `X-OAR-CLI-Version`.
  - When provided and below minimum compatibility (except on `/health`, `/version`, `/meta/handshake`, `/auth/agents/register`, `/auth/token`), response is:
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

- `GET /actors`
  - Response: `{ "actors": [<actor>...] }`

### Agent auth and self-management

- `POST /auth/agents/register`
  - Body: `{ "username": "...", "public_key": "<base64-ed25519-public-key>" }`
  - Response: `{ "agent": <agent_profile>, "key": <agent_key>, "tokens": <token_bundle> }`

- `POST /auth/token`
  - Assertion grant body: `{ "grant_type": "assertion", "agent_id": "...", "key_id": "...", "signed_at": "<rfc3339>", "signature": "<base64-ed25519-signature>" }`
  - Refresh grant body: `{ "grant_type": "refresh_token", "refresh_token": "<token>" }`
  - Response: `{ "tokens": <token_bundle> }`

- `POST /auth/passkey/register/options`
  - Body: `{ "display_name": "..." }`
  - Response: `{ "session_id": "...", "options": <webauthn-registration-options> }`

- `POST /auth/passkey/register/verify`
  - Body: `{ "session_id": "...", "credential": <webauthn-attestation-response> }`
  - Response: `{ "agent": <agent_profile>, "tokens": <token_bundle> }`

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

### Threads (thread snapshots)

- `POST /threads`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "thread": <thread_snapshot_fields_without_id> }`
  - `thread.cadence`:
    - MUST be either literal `reactive` or a 5-field cron expression.
    - Legacy values `daily`, `weekly`, `monthly`, `custom` MAY be accepted for backward compatibility.
  - Response: `{ "thread": <thread_snapshot> }`

- `GET /threads`
  - Query (optional): `status`, `priority`, `tag`, `cadence`, `stale` (boolean)
  - `tag` MAY be repeated (for example `?tag=ops&tag=backend`). Repeated tags use AND semantics: returned threads MUST contain all provided tags.
  - `cadence` MAY be repeated (for example `?cadence=daily&cadence=weekly`). Repeated cadence values use OR semantics: returned threads MAY match any provided cadence.
  - `cadence` filter values are preset-oriented (`reactive`, `daily`, `weekly`, `monthly`, `custom`).
  - Canonical preset cron expressions (for example `0 9 * * *`) are treated as their preset aliases.
  - Non-preset cron expressions match by exact cadence string.
  - When both `tag` and `cadence` filters are present, both filters apply.
  - Response: `{ "threads": [<thread_snapshot>...] }`

- `GET /threads/{thread_id}`
  - Response: `{ "thread": <thread_snapshot> }`

- `PATCH /threads/{thread_id}`
  - Body: `{ "actor_id": "...", "patch": { <fields...> } , "if_updated_at"?: "..." }`
  - Semantics: patch/merge; list-valued fields replace wholesale when present.
  - `patch.cadence` follows the same `reactive` or 5-field cron rule as create.
  - `if_updated_at` (optional) MUST be an RFC3339 timestamp. If provided and it does not match the current snapshot `updated_at`, the request fails with `409 Conflict` and no patch or event side effects are applied.
  - Conflict response shape: `{ "error": { "code": "conflict", "message": "...", "recoverable": true, "hint": "..." } }`
  - Response: `{ "thread": <thread_snapshot> }`

- `GET /threads/{thread_id}/timeline`
  - Response:
    - `{ "events": [<event>...], "snapshots": { "<snapshot_id>": <snapshot> }, "artifacts": { "<artifact_id>": <artifact_metadata> }, "documents": { "<document_id>": <document> }, "document_revisions": { "<revision_id>": <document_revision> } }`
    - `events` remain time-ordered.
    - `snapshots` includes objects referenced by `snapshot:<id>` refs in returned events when they exist.
    - `artifacts` includes metadata objects referenced by `artifact:<id>` refs in returned events when they exist.
    - `documents` includes objects referenced by `document:<id>` refs in returned events when they exist.
    - `document_revisions` includes objects referenced by `document_revision:<id>` refs in returned events when they exist.
    - Missing referenced IDs are omitted from expansion maps (events still keep their original refs).

- `GET /threads/{thread_id}/context`
  - Query (optional):
    - `max_events` (non-negative integer, default `20`)
    - `include_artifact_content` (`true|false`, default `false`)
  - Response:
    - `{ "thread": <thread_snapshot>, "recent_events": [<event>...], "key_artifacts": [ { "ref": "artifact:<id>", "artifact": <artifact_metadata>, "content_preview"?: "<string>" } ... ], "open_commitments": [<commitment_snapshot>...], "documents": [<document>...] }`
    - `recent_events` contains at most `max_events` newest events for the thread.
    - `key_artifacts` preserves `thread.key_artifacts` order and omits missing refs.
    - `content_preview` is included only when `include_artifact_content=true`.
    - `open_commitments` expands `thread.open_commitments` IDs into full commitment snapshots (missing IDs are omitted).
    - `documents` returns thread-linked documents ordered by `updated_at` descending, each with a `head_revision` summary for current revision metadata.

### Commitments (commitment snapshots)

- `POST /commitments`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "commitment": <commitment_snapshot_fields_without_id> }`
  - Response: `{ "commitment": <commitment_snapshot> }`

- `GET /commitments`
  - Query (optional): `thread_id`, `owner`, `status`, `due_before`, `due_after`
  - Response: `{ "commitments": [<commitment_snapshot>...] }`

- `GET /commitments/{commitment_id}`
  - Response: `{ "commitment": <commitment_snapshot> }`

- `PATCH /commitments/{commitment_id}`
  - Body: `{ "actor_id": "...", "patch": { <fields...> }, "refs"?: ["typed:ref"...], "if_updated_at"?: "..." }`
  - Notes:
    - Restricted transitions (e.g. `status -> done`) require `refs` per schema.
    - `refs` are used to populate provenance for restricted fields.
    - `if_updated_at` (optional) MUST be an RFC3339 timestamp. If provided and it does not match the current snapshot `updated_at`, the request fails with `409 Conflict` and no patch or event side effects are applied.
    - Conflict response shape: `{ "error": { "code": "conflict", "message": "...", "recoverable": true, "hint": "..." } }`
  - Response: `{ "commitment": <commitment_snapshot> }`

### Artifacts

- `POST /artifacts`
  - Body: `{ "actor_id": "...", "artifact": <artifact_metadata_without_id_and_content_path>, "content": <string|object|base64>, "content_type": "text|structured|binary" }`
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
  - Body: `{ "actor_id": "...", "request_key"?: "...", "document": { id?, thread_id?, title?, slug?, status?, labels?, supersedes? }, "refs"?: ["typed:ref"...], "content": <string|object|base64>, "content_type": "text|structured|binary" }`
  - Response: `{ "document": <document>, "revision": <document_revision_with_content> }`
  - Side effect: appends `document_created` to `events` with thread/document/revision/artifact refs when the document is thread-linked.

- `GET /docs`
  - Query (optional): `thread_id=<thread_id>`, `include_tombstoned=true|false`
  - Response: `{ "documents": [<document>...] }`
  - Notes:
    - `thread_id` scopes the list to documents whose current `document.thread_id` matches the thread.
    - Each listed document includes `head_revision` summary metadata (`revision_id`, `revision_number`, `artifact_id`, `content_type`, `created_at`, `created_by`) alongside the existing top-level head revision fields.

- `GET /docs/{document_id}`
  - Response: `{ "document": <document>, "revision": <document_revision_with_content> }`

- `PATCH /docs/{document_id}`
  - Body: `{ "actor_id": "...", "document"?: { title?, thread_id?, slug?, status?, labels?, supersedes? }, "if_base_revision": "<revision_id>", "refs"?: ["typed:ref"...], "content": <string|object|base64>, "content_type": "text|structured|binary" }`
  - Response: `{ "document": <document>, "revision": <document_revision_with_content> }`
  - Side effect: appends `document_updated` to `events` with thread/document/revision/artifact refs when the document is thread-linked.

- `GET /docs/{document_id}/history`
  - Response: `{ "document_id": "<document_id>", "revisions": [<document_revision>...] }`

- `GET /docs/{document_id}/revisions/{revision_id}`
  - Response: `{ "document_id": "<document_id>", "revision": <document_revision_with_content> }`

- `POST /docs/{document_id}/tombstone`
  - Body: `{ "actor_id": "...", "reason": "..." }`
  - Response: `{ "document": <document>, "revision": <document_revision_with_content> }`
  - Side effect: appends `document_tombstoned` to `events` with thread/document/current-revision/artifact refs when the document is thread-linked.

### Events

- `POST /events`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "event": <event_fields_without_id_ts_actor_id> }`
  - Response: `{ "event": <event> }`

- `GET /events/stream`
  - Content type: `text/event-stream`
  - SSE event type: `event`
  - SSE data envelope: `{ "event": <event> }`
  - Optional query: `thread_id`, repeated `type`, `types` (comma-separated), `last_event_id`
  - Resume supported via `Last-Event-ID` header or `last_event_id` query.
  - Thread-linked document lifecycle operations emit `document_created`, `document_updated`, and `document_tombstoned` events with `document:*`, `document_revision:*`, and backing `artifact:*` refs.

- `GET /events/{event_id}`
  - Response: `{ "event": <event> }`

### Packet convenience endpoints

- `POST /work_orders`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "artifact": <artifact_metadata>, "packet": <work_order_packet> }`
  - `artifact.id` and `packet.work_order_id` MAY be omitted together; core issues the canonical artifact id and returns it in both artifact metadata and packet content.
  - Response: `{ "artifact": <artifact_metadata>, "event": <event> }`

- `POST /receipts`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "artifact": <artifact_metadata>, "packet": <receipt_packet> }`
  - `artifact.id` and `packet.receipt_id` MAY be omitted together; core issues the canonical artifact id and returns it in both artifact metadata and packet content.
  - Response: `{ "artifact": <artifact_metadata>, "event": <event> }`

- `POST /reviews`
  - Body: `{ "actor_id": "...", "request_key"?: "...", "artifact": <artifact_metadata>, "packet": <review_packet> }`
  - Response: `{ "artifact": <artifact_metadata>, "event": <event> }`

- Atomicity guarantee:
  - Packet convenience writes persist artifact metadata/content and emitted event in one transactional operation.
  - If either artifact or event persistence fails, no partial packet convenience write is committed.

### Inbox and derived views

- `GET /inbox`
  - Response: `{ "items": [<inbox_item>...], "generated_at": "..." }`
  - Optional query: `risk_horizon_days`

- `GET /inbox/{inbox_item_id}`
  - Response: `{ "item": <inbox_item>, "generated_at": "..." }`
  - Optional query: `risk_horizon_days`

- `GET /inbox/stream`
  - Content type: `text/event-stream`
  - SSE event type: `inbox_item`
  - SSE data envelope: `{ "item": <inbox_item> }`
  - Optional query: `risk_horizon_days`, `last_event_id`
  - Resume supported via `Last-Event-ID` header or `last_event_id` query.

- `POST /inbox/ack`
  - Body: `{ "actor_id": "...", "thread_id": "...", "inbox_item_id": "..." }`
  - Response: `{ "event": <event> }`

- `POST /derived/rebuild`
  - Body: `{ "actor_id": "..." }`
  - Response: `{ "ok": true }`

- Materialized derived projections used by the common read path:
  - `derived_inbox_items`: incrementally maintained inbox items keyed by deterministic `inbox_item_id`, with per-thread rows used by `GET /inbox`, `GET /inbox/{id}`, and thread workspace inbox sections.
  - `derived_thread_views`: incrementally maintained per-thread stale/workspace summaries used by thread list stale indicators and thread workspace summary surfaces.
  - `POST /derived/rebuild` remains the deterministic repair path: it re-emits any missing canonical stale-thread exceptions from canonical state, then rebuilds both projection tables from current threads/events/commitments/documents.

- Meaningful thread activity for stale-thread clearing:
  - The current activity set is explicit: `actor_statement`, `decision_needed`, `decision_made`, `work_order_created`, `receipt_added`, `review_completed`, `document_created`, `document_updated`, `document_tombstoned`, `commitment_created`, `commitment_status_changed`, plus non-create `snapshot_updated` events from direct user-authored snapshot edits.
  - Coordination noise does not count as activity: inbox acknowledgments, exception notifications, thread-creation bookkeeping, and derived `open_commitments` maintenance.
  - `thread.open_commitments` remains present on thread reads for compatibility, but keeping that field synchronized no longer emits a user-visible timeline event or bumps the thread’s visible update clock.
