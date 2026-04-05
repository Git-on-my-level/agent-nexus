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

- **`canonical`**: CRUD/list/get endpoints over canonical resources (topics, cards, boards, documents, artifacts, events, packets, plus read-only thread compatibility endpoints). These are the durable substrate.

- **`projection`**: Operator convenience surfaces that aggregate multiple canonical resources into workspace-friendly bundles. Examples: `topics.workspace` (primary coordination), `threads.context`, `threads.workspace` (backing-thread diagnostics), `boards.workspace`, `inbox.list/get/stream/ack`. The web UI intentionally uses projection endpoints for workspace/inbox/operator reads.

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

### Topics (canonical subject state)

- `POST /topics`
  - Body: `{ "actor_id": "...", "topic": <topic_fields_without_id> }`
  - Response: `{ "topic": <topic> }`

- `GET /topics`
  - Query (optional): `type`, `status`, `q`, `limit`, `cursor`
  - Response: `{ "topics": [<topic>...] }`

- `GET /topics/{topic_id}`
  - Response: `{ "topic": <topic> }`

- `PATCH /topics/{topic_id}`
  - Body: `{ "actor_id": "...", "patch": { <fields...> }, "if_updated_at"?: "..." }`
  - Notes:
    - Patch/merge semantics apply; list-valued fields replace wholesale.
  - Response: `{ "topic": <topic> }`

### Cards (canonical board work items)

- `GET /cards`
  - Response: `{ "cards": [<card>...] }`

- `GET /cards/{card_id}`
  - Response: `{ "card": <card> }`

- `PATCH /cards/{card_id}`
  - Body: `{ "actor_id": "...", "patch": { <fields...> }, "if_updated_at"?: "..." }`
  - Response: `{ "card": <card> }`

- Board-scoped card lifecycle (create, patch, move, remove) is exposed under `POST|PATCH /boards/{board_id}/cards` and related paths; see `/contracts/oar-openapi.yaml`.

### Threads (read-only backing inspection)

Threads are backing infrastructure for timelines and packet subjects. The workspace contract exposes **GET-only** thread routes for inspection and projections; prefer **topics** and **cards** for operator mutations.

- `GET /threads`
  - Query (optional): `status`, `priority`, `tag`, `cadence`, `stale` (boolean)
  - Response: `{ "threads": [<thread>...] }` (each `thread` matches the schema’s thread resource shape)

- `GET /threads/{thread_id}`
  - Response: `{ "thread": <thread> }`

- `GET /threads/{thread_id}/timeline` (projection)
  - Response: thread timeline envelope including `events` and related expansions per OpenAPI (`ThreadTimelineResponse`).

- `GET /threads/{thread_id}/context` (projection)
  - Response: compact coordination bundle for triage per OpenAPI.

- `GET /threads/{thread_id}/workspace` (projection)
  - Response: related topics, cards, documents, board memberships, inbox section, and freshness metadata per OpenAPI.

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
