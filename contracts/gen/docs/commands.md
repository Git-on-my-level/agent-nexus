# OAR Command Registry

Generated from `contracts/oar-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.3.0`
- Commands: `45`

## `artifacts.create`

- CLI path: `artifacts create`
- HTTP: `POST /artifacts`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Store content-addressed artifact metadata and payload (bytes, text, or structured packet JSON).
- Concepts: `artifacts`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `conflict`
- Output: Returns `{ artifact }`.

## `artifacts.get`

- CLI path: `artifacts get`
- HTTP: `GET /artifacts/{artifact_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve immutable artifact metadata referenced from timelines and packets.
- Concepts: `artifacts`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ artifact }`.

## `artifacts.list`

- CLI path: `artifacts list`
- HTTP: `GET /artifacts`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Search and filter immutable artifacts across the workspace.
- Concepts: `artifacts`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ artifacts }`.

## `boards.cards.create`

- CLI path: `boards cards create`
- HTTP: `POST /boards/{board_id}/cards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a first-class card and attach it to a board.
- Concepts: `boards`, `cards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ card }`.

## `boards.cards.get`

- CLI path: `boards cards get`
- HTTP: `GET /boards/{board_id}/cards/{card_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve a card through its board membership context.
- Concepts: `boards`, `cards`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ card }`.

## `boards.cards.list`

- CLI path: `boards cards list`
- HTTP: `GET /boards/{board_id}/cards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: List cards on one board in canonical order.
- Concepts: `boards`, `cards`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ board_id, cards }`.

## `boards.create`

- CLI path: `boards create`
- HTTP: `POST /boards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a durable board over topics and cards.
- Concepts: `boards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ board }`.

## `boards.get`

- CLI path: `boards get`
- HTTP: `GET /boards/{board_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve canonical board state and summary.
- Concepts: `boards`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ board, summary }`.

## `boards.list`

- CLI path: `boards list`
- HTTP: `GET /boards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Scan durable coordination boards and lightweight summaries.
- Concepts: `boards`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ boards, summaries }`.

## `boards.patch`

- CLI path: `boards patch`
- HTTP: `PATCH /boards/{board_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Update board metadata with optimistic concurrency.
- Concepts: `boards`, `write`, `concurrency`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board }`.

## `boards.workspace`

- CLI path: `boards workspace`
- HTTP: `GET /boards/{board_id}/workspace`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load the operator-facing board workspace with cards, docs, and inbox sections.
- Concepts: `boards`, `workspace`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ board_id, board, primary_topic, cards, documents, inbox, board_summary, projection_freshness, board_summary_freshness, warnings, section_kinds, generated_at }`.

## `cards.archive`

- CLI path: `cards archive`
- HTTP: `POST /cards/{card_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Soft-delete a first-class card by setting archived_at (board concurrency via if_board_updated_at).
- Concepts: `cards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board, card }`.

## `cards.get`

- CLI path: `cards get`
- HTTP: `GET /cards/{card_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve one first-class card by id.
- Concepts: `cards`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ card }`.

## `cards.list`

- CLI path: `cards list`
- HTTP: `GET /cards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Scan first-class card resources across boards.
- Concepts: `cards`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ cards }`.

## `cards.move`

- CLI path: `cards move`
- HTTP: `POST /cards/{card_id}/move`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Reposition a card within a board column using the card's first-class identity.
- Concepts: `cards`, `boards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ card }`.

## `cards.patch`

- CLI path: `cards patch`
- HTTP: `PATCH /cards/{card_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Update card fields, including resolution and resolution refs.
- Concepts: `cards`, `write`, `concurrency`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ card }`.

## `cards.purge`

- CLI path: `cards purge`
- HTTP: `POST /cards/{card_id}/purge`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Permanently delete an archived card (human-gated; requires archived_at).
- Concepts: `cards`, `write`
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ purged, card_id }`.

## `cards.restore`

- CLI path: `cards restore`
- HTTP: `POST /cards/{card_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear archived_at on a soft-deleted card so it reappears on boards.
- Concepts: `cards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board, card }`.

## `docs.create`

- CLI path: `docs create`
- HTTP: `POST /docs`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a canonical document lineage anchored to a typed subject ref.
- Concepts: `docs`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ document, revision }`.

## `docs.get`

- CLI path: `docs get`
- HTTP: `GET /docs/{document_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve a document lineage and its current head revision.
- Concepts: `docs`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ document, revision }`.

## `docs.list`

- CLI path: `docs list`
- HTTP: `GET /docs`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Scan canonical document lineages.
- Concepts: `docs`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ documents }`.

## `docs.revisions.create`

- CLI path: `docs revisions create`
- HTTP: `POST /docs/{document_id}/revisions`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Append a new immutable revision and advance the document head.
- Concepts: `docs`, `revisions`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ document, revision }`.

## `docs.revisions.get`

- CLI path: `docs revisions get`
- HTTP: `GET /docs/{document_id}/revisions/{revision_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve one immutable document revision.
- Concepts: `docs`, `revisions`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ document_id, revision }`.

## `docs.revisions.list`

- CLI path: `docs revisions list`
- HTTP: `GET /docs/{document_id}/revisions`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Enumerate immutable revisions for one document lineage.
- Concepts: `docs`, `revisions`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ document_id, revisions }`.

## `events.create`

- CLI path: `events create`
- HTTP: `POST /events`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Append an event that links first-class resources and evidence through typed refs.
- Concepts: `events`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ event }`.

## `events.list`

- CLI path: `events list`
- HTTP: `GET /events`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Inspect append-only event history across the workspace.
- Concepts: `events`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ events }`.

## `inbox.acknowledge`

- CLI path: `inbox acknowledge`
- HTTP: `POST /inbox/{inbox_id}/acknowledge`
- Stability: `beta`
- Surface: `projection`
- Input mode: `json-body`
- Why: Suppress or clear a derived inbox item via a durable acknowledgment event.
- Concepts: `inbox`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Output: Returns `{ event }`.

## `inbox.list`

- CLI path: `inbox list`
- HTTP: `GET /inbox`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load the derived operator inbox generated from refs and canonical events.
- Concepts: `inbox`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ items }`.

## `meta.health`

- CLI path: `meta health`
- HTTP: `GET /health`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Probe whether the core process is alive.
- Concepts: `health`
- Output: Returns `{ ok: true }`.

## `meta.readyz`

- CLI path: `meta readyz`
- HTTP: `GET /readyz`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Verify storage and projection subsystems are ready for traffic.
- Concepts: `health`, `readiness`
- Error codes: `storage_unavailable`
- Output: Returns `{ ok: true }` when the workspace is ready.

## `meta.version`

- CLI path: `meta version`
- HTTP: `GET /version`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Check compatibility between clients and core before writes.
- Concepts: `compatibility`
- Output: Returns `{ schema_version, command_registry_digest }`.

## `packets.receipts.create`

- CLI path: `packets receipts create`
- HTTP: `POST /packets/receipts`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Record structured delivery evidence anchored by `subject_ref`.
- Concepts: `packets`, `evidence`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ artifact, packet_kind, packet }`.

## `packets.reviews.create`

- CLI path: `packets reviews create`
- HTTP: `POST /packets/reviews`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Record a structured review over a work order and receipt using subject refs.
- Concepts: `packets`, `evidence`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ artifact, packet_kind, packet }`.

## `packets.work-orders.create`

- CLI path: `packets work-orders create`
- HTTP: `POST /packets/work-orders`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Record a structured work-order artifact anchored by `subject_ref`.
- Concepts: `packets`, `evidence`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ artifact, packet_kind, packet }`.

## `threads.context`

- CLI path: `threads context`
- HTTP: `GET /threads/{thread_id}/context`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load a compact coordination bundle (thread, recent events, key artifacts, cards, documents) for inspection and triage.
- Concepts: `threads`, `inspection`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Output: Returns `{ thread, recent_events, key_artifacts, open_cards, documents }` plus forward-compatible fields.

## `threads.inspect`

- CLI path: `threads inspect`
- HTTP: `GET /threads/{thread_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve one backing thread for low-level inspection and diagnostics.
- Concepts: `threads`, `inspection`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ thread }`.

## `threads.list`

- CLI path: `threads list`
- HTTP: `GET /threads`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Inspect backing infrastructure threads without making them the primary planning noun.
- Concepts: `threads`, `inspection`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ threads }`.

## `threads.timeline`

- CLI path: `threads timeline`
- HTTP: `GET /threads/{thread_id}/timeline`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Retrieve event history plus typed-ref expansions for one backing thread.
- Concepts: `threads`, `timeline`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ thread, events, artifacts, topics, cards, documents }`.

## `threads.workspace`

- CLI path: `threads workspace`
- HTTP: `GET /threads/{thread_id}/workspace`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Read-only diagnostic projection that bundles context, inbox, and related-thread signals for one backing thread. Prefer topics.workspace for normal operator coordination when a topic exists.
- Concepts: `threads`, `workspace`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ thread, related_topics, cards, documents, board_memberships, inbox, projection_freshness }`.

## `topics.create`

- CLI path: `topics create`
- HTTP: `POST /topics`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a first-class durable topic before attaching cards, docs, or packets.
- Concepts: `topics`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ topic }`.
- Agent notes: Replay-safe when the same request key and body are reused.

## `topics.get`

- CLI path: `topics get`
- HTTP: `GET /topics/{topic_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve one topic and its canonical durable fields.
- Concepts: `topics`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ topic }`.

## `topics.list`

- CLI path: `topics list`
- HTTP: `GET /topics`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Scan the durable topic inventory.
- Concepts: `topics`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ topics }`.

## `topics.patch`

- CLI path: `topics patch`
- HTTP: `PATCH /topics/{topic_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Update topic state with provenance and optimistic concurrency.
- Concepts: `topics`, `write`, `concurrency`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ topic }`.

## `topics.timeline`

- CLI path: `topics timeline`
- HTTP: `GET /topics/{topic_id}/timeline`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load chronological evidence and related resources for one topic.
- Concepts: `topics`, `timeline`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ topic, events, artifacts, cards, documents, threads }`.

## `topics.workspace`

- CLI path: `topics workspace`
- HTTP: `GET /topics/{topic_id}/workspace`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Primary operator coordination read — load the topic workspace composed from linked cards, docs, backing threads, and inbox items. Prefer this over thread workspace for triage and planning.
- Concepts: `topics`, `workspace`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ topic, cards, boards, documents, threads, inbox, projection_freshness, generated_at }`.

