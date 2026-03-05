# OAR HTTP API Contract (v0)

This document defines the **concrete HTTP/JSON surface** used for integration between **oar-core** and clients (including **oar-ui** and agents).

The schema of objects is defined by `contracts/oar-schema.yaml`.

## Conventions

- All requests that mutate state MUST include `actor_id`.
- All timestamps are ISO-8601 strings.
- Objects MUST preserve unknown fields (additive evolution).
- `refs` values MUST be typed ref strings per `ref_format`.

## Endpoints

### Version

- `GET /version`
  - Response: `{ "schema_version": "0.2.2" }`

### Actors

- `POST /actors`
  - Body: `{ "actor": { id, display_name, tags?, created_at } }`
  - Response: `{ "actor": <actor> }`

- `GET /actors`
  - Response: `{ "actors": [<actor>...] }`

### Threads (thread snapshots)

- `POST /threads`
  - Body: `{ "actor_id": "...", "thread": <thread_snapshot_fields_without_id> }`
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
  - Conflict response shape: `{ "error": { "code": "conflict", "message": "..." } }`
  - Response: `{ "thread": <thread_snapshot> }`

- `GET /threads/{thread_id}/timeline`
  - Response:
    - `{ "events": [<event>...], "snapshots": { "<snapshot_id>": <snapshot> }, "artifacts": { "<artifact_id>": <artifact_metadata> } }`
    - `events` remain time-ordered.
    - `snapshots` includes objects referenced by `snapshot:<id>` refs in returned events when they exist.
    - `artifacts` includes metadata objects referenced by `artifact:<id>` refs in returned events when they exist.
    - Missing referenced IDs are omitted from `snapshots`/`artifacts` (events still keep their original refs).

### Commitments (commitment snapshots)

- `POST /commitments`
  - Body: `{ "actor_id": "...", "commitment": <commitment_snapshot_fields_without_id> }`
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
    - Conflict response shape: `{ "error": { "code": "conflict", "message": "..." } }`
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

### Events

- `POST /events`
  - Body: `{ "actor_id": "...", "event": <event_fields_without_id_ts_actor_id> }`
  - Response: `{ "event": <event> }`

- `GET /events/{event_id}`
  - Response: `{ "event": <event> }`

### Packet convenience endpoints

- `POST /work_orders`
  - Body: `{ "actor_id": "...", "artifact": <artifact_metadata>, "packet": <work_order_packet> }`
  - Response: `{ "artifact": <artifact_metadata>, "event": <event> }`

- `POST /receipts`
  - Body: `{ "actor_id": "...", "artifact": <artifact_metadata>, "packet": <receipt_packet> }`
  - Response: `{ "artifact": <artifact_metadata>, "event": <event> }`

- `POST /reviews`
  - Body: `{ "actor_id": "...", "artifact": <artifact_metadata>, "packet": <review_packet> }`
  - Response: `{ "artifact": <artifact_metadata>, "event": <event> }`

### Inbox and derived views

- `GET /inbox`
  - Response: `{ "items": [<inbox_item>...], "generated_at": "..." }`

- `POST /inbox/ack`
  - Body: `{ "actor_id": "...", "thread_id": "...", "inbox_item_id": "..." }`
  - Response: `{ "event": <event> }`

- `POST /derived/rebuild`
  - Body: `{ "actor_id": "..." }`
  - Response: `{ "ok": true }`
