# OAR HTTP API Contract (v0)

This document defines the **concrete HTTP/JSON surface** used for integration between **oar-core** and clients (including **oar-ui** and agents).

The schema of objects is defined by `/contracts/oar-schema.yaml`.

## Conventions

- All requests that mutate state MUST include `actor_id`.
- All timestamps are ISO-8601 strings.
- Objects MUST preserve unknown fields (additive evolution).
  - `refs` values MUST be typed ref strings per `ref_format`.

## API Surface Classification

Each endpoint is classified with an `x-oar-surface` extension indicating its role:

- **`canonical`**: CRUD/list/get endpoints over canonical resources (threads, commitments, artifacts, documents, boards, board cards, events, snapshots, packets). These are the durable substrate.

- **`projection`**: Operator convenience surfaces that aggregate multiple canonical resources into workspace-friendly bundles. Examples: `threads.context`, `threads.workspace`, `boards.workspace`, `inbox.list/get/stream/ack`. The web UI intentionally uses projection endpoints for workspace/inbox/operator reads.

- **`utility`**: Infrastructure endpoints for health, version, meta discovery, auth bootstrap, and maintenance. Examples: `/health`, `/version`, `/meta/*`, `/auth/*`, `/actors`, `/derived/rebuild`.

## Endpoints

### Version

- `GET /version`
  - Response: `{ "schema_version": "0.2.3" }`

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
  - `cadence` filter semantics:
    - Preset-based values: `reactive`, `daily`, `weekly`, `monthly`, `custom`.
    - If a thread stores cron cadence directly, canonical daily/weekly/monthly cron expressions are matched to those presets; non-preset cron values are matched as `custom`.
  - Response: `{ "threads": [<thread_snapshot>...] }`

- `GET /threads/{thread_id}`
  - Response: `{ "thread": <thread_snapshot> }`

- `PATCH /threads/{thread_id}`
  - Body: `{ "actor_id": "...", "patch": { <fields...> } , "if_updated_at"?: "..." }`
  - Semantics: patch/merge; list-valued fields replace wholesale when present.
  - `patch.cadence` follows the same `reactive` or 5-field cron rule as create.
  - Response: `{ "thread": <thread_snapshot> }`

- `GET /threads/{thread_id}/timeline`
  - Response: `{ "events": [<event>...] }`

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
  - Response: `{ "commitment": <commitment_snapshot> }`

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
