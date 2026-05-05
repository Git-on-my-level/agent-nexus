# anx-core — Spec (v0.3.0)

## 0. Purpose

anx-core is the canonical state and evidence system for Agent Nexus.

Agent Nexus is a manager and executive operating system, not a generic work-management tool. The product foundation and architecture decisions are documented in [docs/architecture/foundation.md](../../docs/architecture/foundation.md). anx-core implements the canonical runtime truth (SQLite plus a blob backend seam) and owns the institutional memory — the durable truth, the evidence trail, the coordination artifacts. It has no opinion on how actors are instantiated, orchestrated, or upgraded. An actor authenticates with an ID, reads state, does work, writes back. Whether that actor is a human, a Claude agent, an open-source agent framework, or something that doesn't exist yet is outside anx-core's scope.

anx-core:
- Maintains durable organizational state (events, topics, artifacts, documents, boards, cards).
- Stores and validates structured coordination artifacts (receipts and reviews).
- Enforces evidence-first grounding rules on restricted state transitions.
- Computes derived views (inbox, staleness) from canonical data.
- Exposes a programmatic API and contract surface that CLI/generated clients can use.

anx-core does **not**:
- Perform real-world side effects (sending, spending, posting, deploying).
- Hold credentials for external systems.
- Orchestrate, dispatch to, or manage agents.
- Rely on chat history as a memory mechanism.

---

## 1. Design constraints

### 1.1 Canonical vs derived
- anx-core MUST separate **canonical truth** (events, topics, cards, boards, documents, artifacts) from **derived views** (inbox items, staleness flags).
- Derived views MUST be regenerable from canonical data.
- Canonical data MUST be replayable and auditable.

### 1.2 Evidence-first progress
- Any state change that closes a loop (marking "done", asserting "shipped", claiming "metric improved") MUST be grounded in a receipt artifact or an explicit decision event.

### 1.3 Small, stable primitives
- anx-core centers on append-only **events**, mutable **resource** records (topics, cards, boards, documents), and **artifacts** (immutable content, including packet kinds). Backing **threads** tie timelines and many packet subjects together. Everything else is a typed convention over those building blocks.

### 1.4 Actor-agnostic
- anx-core treats all actors as opaque IDs. It does not distinguish between humans and agents at the API or storage level. Identity metadata (display name, tags) lives in a lightweight actor registry.

### 1.5 Implementation baseline (v0)
- The reference backend implementation is Go-first.
- Language choice MUST NOT change the external contract: HTTP/JSON API + SQLite-backed canonical state plus a replaceable blob backend remain the system boundary.

---

## 2. Storage model

### 2.1 SQLite + blob backend seam
- **SQLite** stores events (rows), topics (rows), cards (rows), artifact metadata (rows), documents (rows), document_revisions (rows), actor registry (rows), and derived views (rows).
- **Blob storage** stores artifact content. The default backend is the local filesystem; optional deployments can switch to an S3-compatible object store without changing canonical artifact/document identity, which remains content-addressed by `content_hash` and `content_type` rather than a backend-specific path.
- Hosted deployments MUST treat blob storage as a replaceable backend seam rather than a client-visible contract.
- Clients and agents SHOULD prefer the API, CLI, and generated clients over direct filesystem access.

### 2.2 Schema authority
- `anx-schema.yaml` is the authoritative field/type definition shared between anx-core and Agent Nexus web UI.
- anx-core MUST enforce schema constraints on writes where specified (restricted transitions, required fields, packet validation, typed ref format).
- **Strict enums** (including `event_type` and `artifact_kind`): anx-core MUST reject unknown values.
- Open enums, where explicitly marked in the schema, may accept values outside the initial examples.
- Unknown fields on any object MUST be preserved and round-tripped.

### 2.3 Mutable resource update semantics
- Topic and card updates use **patch/merge** semantics: only specified fields are updated; unspecified fields (including unknown fields) are preserved unchanged.
- **List-valued fields** (e.g., `owner_refs`, `document_refs`, `related_refs`) are **replaced wholesale** when present in a patch. Absence means no change.
- This prevents older clients from accidentally erasing fields they don't know about.

---

## 3. Canonical primitives

### 3.1 Event (append-only)
A durable record that something happened or that an actor claims something happened.

**Behavior:**
- Event identity, ordering, refs, and payload content MUST be append-only. Corrections MUST NOT edit existing event content.
- Event lifecycle visibility fields (`archived_at`, `archived_by`, `trashed_at`, `trashed_by`, `trash_reason`) are the bounded mutable exception on event rows. They model list/timeline visibility, not corrections to the event fact.
- Corrections MUST be new events.
- The system MUST reject event types outside the strict `event_type` enum.
- All values in `refs` MUST use typed reference strings (see `anx-schema.yaml` → `ref_format`).

**Fields:** per `anx-schema.yaml` → `primitives.event`

**v0 event types:** per strict `event_type` in `anx-schema.yaml`.

**Home feed projection:** core owns durable per-topic read cursors for Home. The
Home feed is a deterministic projection over Events using the shared
`home_feed` preset, grouped by resolvable topic. Events with only an unmapped
thread or no topic ownership remain visible on Events and are excluded from
Home.

### 3.2 Mutable resources (topic/card/board/document)
Topics, cards, boards, and documents are mutable current-state records.

**Behavior:**
- Mutable resources are updated via patch/merge (see §2.3).
- Resource-specific mutations MUST emit the corresponding event type and reference the resource ID using its typed prefix.
- Topic and card updates SHOULD include `changed_fields` when the implementation can report them.

**Fields:** per `anx-schema.yaml` → the relevant resource schema under `resources.*`

Mutable resources SHOULD be small and reference larger content via artifacts.

### 3.3 Artifact (immutable content)
An immutable blob representing a specific version of content at a point in time.

**Behavior:**
- Artifacts MUST be immutable. New versions are new artifacts.
- Artifact metadata lives in SQLite. Content lives behind the blob backend and is resolved canonically by `content_hash`.
- Artifact content is **generally opaque** to anx-core — it stores and retrieves but does not interpret. **Exception:** packet kinds (`receipt`, `review`) are schema-validated structured content. anx-core MUST validate their required fields, constraints, and ID consistency on write (see §5).
- All values in `refs` MUST use typed reference strings.

**Fields:** per `anx-schema.yaml` → `primitives.artifact`

---

## 4. Typed conventions (v0)

These are schema conventions over the primitives, not new primitive types.

### 4.1 Topic
The unit of ongoing context — a project, incident, relationship, process, or initiative.

**Fields:** per `anx-schema.yaml` → `resources.topic`

Topics MAY point at a backing thread through `thread_id`, but the topic is the canonical subject record.

### 4.2 Card
A first-class board work item anchored to a board and optionally linked to a topic, backing thread, or document lineage.

**Fields:** per `anx-schema.yaml` → `resources.card`

**Restricted transitions:**
- Placing a card in the **done** lane with terminal completion (`resolution → done`) requires a typed reference to a receipt artifact (`artifact:<id>`) or a decision event (`event:<id>`). Operators drive this via `column_key` and `resolution_refs`; abandonment or “won’t do” is modeled with **card trash**, not a second resolution value.
- anx-core MUST reject completion to done if the required reference is missing.

---

## 5. Artifact packet conventions (v0)

Receipts and reviews are stored as artifacts with structured content. They anchor evidence to a first-class card subject.

### 5.1 Packet validation rules
- Packet content ID fields (`receipt_id`, `review_id`) MUST equal the enclosing artifact's `id`. anx-core MUST reject mismatches.
- All required fields per the packet schema MUST be present. anx-core MUST reject packets missing required fields.
- Receipt and review packets MUST use `subject_ref` of the form `card:<card_id>`; anx-core resolves that to the card’s backing thread for event placement.
- All packet artifacts MUST include `subject_ref` in `packet` and a matching `artifact:<packet_id>` ref; additional typed refs MUST satisfy the packet-kind conventions in `anx-schema.yaml` (including the `card:<card_id>` ref and receipt linkage on reviews).
- All typed ref fields in packets (e.g., `outputs`, `verification_evidence`, `evidence_refs`) MUST use typed reference strings.

### 5.2 Receipt
Structured evidence that work was done.

**Fields:** per `anx-schema.yaml` → `packets.receipt`

**Validation:** Receipts that are primarily prose with no evidence links MUST be rejected. The `outputs` and `verification_evidence` fields MUST each contain at least one typed reference.

Creating a receipt MUST emit a `receipt_added` event. Per reference conventions, the event `refs` MUST include `artifact:<receipt_artifact_id>` and `card:<card_id>` (matching `subject_ref`).

### 5.3 Review
A lightweight assessment of a receipt.

**Fields:** per `anx-schema.yaml` → `packets.review`

Creating a review MUST emit a `review_completed` event. Per reference conventions, the event `refs` MUST include `artifact:<review_artifact_id>`, `artifact:<receipt_artifact_id>`, and `card:<card_id>` (matching `subject_ref`).

### 5.4 Documents (first-class lifecycle)

Documents are a first-class canonical domain with their own API and storage. A document is a long-lived lineage, not just stored text: it has a mutable head pointer for the current version and an ordered immutable revision chain for institutional memory. Documents are distinct from the generic artifact model: artifacts are immutable blobs linked only by refs, while documents provide a canonical lineage interface over those immutable revision artifacts.

**Canonical document vocabulary:** The durable contract for documents is the
schema shape in `anx-schema.yaml`: `subject_ref`, `head_revision_ref`, `state`,
and `refs` plus standard lifecycle/provenance fields. Canonical `document.state`
is the sole document lifecycle field and uses `active`, `archived`, or
`trashed`. It is derived from lifecycle detail fields: `trashed_at` present ->
`trashed`, `archived_at` present -> `archived`, otherwise `active`.

**Storage model:**
- `documents` table: document metadata, head_revision_id, trash lifecycle fields (`trashed_at`, `trashed_by`, `trash_reason`), archive lifecycle fields (`archived_at`, `archived_by`).
- `document_revisions` table: revision_id, document_id, revision_number, prev_revision_id, artifact_id, revision_hash.
- Each revision's content is stored in an `artifacts` row with `kind: "doc"`. Content uses content-addressable storage (SHA-256 digest).
- Revisions form a Merkle chain: `revision_hash` incorporates content_hash, prev_revision_hash, document_id, revision_number, created_at, created_by.

**API surface:** `GET /docs`, `POST /docs`, `GET /docs/{document_id}`, `PATCH /docs/{document_id}`, `GET /docs/{document_id}/revisions`, `GET /docs/{document_id}/revisions/{revision_id}`, `POST /docs/{document_id}/trash`, `POST /docs/{document_id}/archive`, `POST /docs/{document_id}/unarchive`, `POST /docs/{document_id}/restore`, `POST /docs/{document_id}/purge`.

**Relationship to artifacts:** Document revisions use artifacts internally for content storage. The docs API is the canonical interface for document lineages; clients should not treat documents as `GET /artifacts?kind=doc`. Documents complement canonical threads/events/artifacts rather than replacing them.

### 5.5 Boards (canonical organizing layers)

Boards are first-class canonical coordination resources. A board is a durable organizational map over work with canonical board metadata, canonical card membership, and optional links to topics and document lineages, all expressed through typed refs.

**Canonical storage:**
- `boards` table: durable board metadata, owners, primary topic ref, backing thread, and optimistic concurrency token.
- `board_cards` table: explicit canonical board membership, including column placement, ordering token, and optional linked document ref.

**Projection boundary:** `boards.workspace` may hydrate canonical backing resources and derived summaries for operator convenience, but the payload must keep those layers explicit: canonical membership/backing refs stay distinct from derived summary/freshness. Derived board scans remain rebuildable from canonical state.

---

## 6. Actor registry

The actor registry is a lightweight table mapping actor IDs to metadata.
Hosted-v1 workspace principals include both passkey-authenticated humans and
Ed25519 key-pair agents. anx-core does not enforce fine-grained role-based
access in v1 — any authenticated principal can perform any operation. The
registry exists for display, attribution, and future evolution (authority
tiers, reliability scores, invite lifecycle metadata, etc.).

**Fields:** per `anx-schema.yaml` → `actor.registry_fields`

- Actor IDs are referenced by `actor_id` on events, `updated_by` on mutable resources, and `created_by` on artifacts.
- anx-core SHOULD reject writes with an unregistered `actor_id` (to prevent typos and orphaned references).

---

## 7. API surface

anx-core MUST expose a programmatic API (protocol: HTTP/JSON for v0).
All workspace data routes require authenticated principals. Writes require an
`actor_id` or an authenticated principal that resolves to one.

### 7.1 Read / query
- Get topic by ID
- List topics (filters: type, derived lifecycle state, archive/trash visibility, search)
- Get topic timeline (events + referenced artifacts/cards/documents/threads, ordered by time)
- Get topic workspace
- Get card by ID
- Get card timeline (events + referenced artifacts/cards/documents/threads for the card’s backing thread)
- List cards (filters: board, topic, resolution, column)
- Get artifact by ID (metadata + content hash)
- List artifacts (filters: kind, topic/thread/time range)
- List documents, get document head, get document history, get document revision
- Get inbox items (grouped by category, sorted by time/due date)
- Get workspace usage summary for operator telemetry and external aggregation (`/ops/usage-summary`, `/v1/usage/summary`)

### 7.2 Write / mutate
- Register actor
- Create topic → emits `topic_created` event with `topic:<topic_id>` in refs
- Update topic fields (patch/merge) → emits `topic_updated`; archive/trash/restore emit dedicated lifecycle events
- Create card → emits `card_created` event with `card:<card_id>` and `board:<board_id>` in refs
- Update card (patch/merge) → emits `card_updated` event
- Move card → emits `card_moved` event
- Resolve card → emits `card_resolved` event
- Create artifact (metadata + content) → returns artifact ID; validates packet content for packet kinds; validates typed ref format
- Create document, update document (new immutable revision), trash document
- Append event (for messages, exceptions, human attention requests/responses, and other audit facts) → validates required refs per reference conventions

### 7.3 Convenience operations
- Submit receipt (validates evidence + packet + refs, creates artifact + emits `receipt_added` with required typed refs)
- Submit review (validates packet + refs, creates artifact + emits `review_completed` with required typed refs)
- Request human attention (emits `human_attention_requested`; prefer `anx human ask|review|escalate` over raw event authoring)
- Respond to inbox item (emits `human_attention_responded` event with `inbox:<inbox_item_id>` in refs)

### 7.4 Derived views
- Derived views are asynchronously materialized from canonical writes; GET endpoints remain side-effect free.
- Canonical writes enqueue affected thread projections for background refresh rather than recomputing projections inline on reads.
- Get inbox items from the materialized projection layer (respects human response suppression; uses deterministic IDs; surfaces freshness metadata when projections are pending, missing, or errored).
- Board workspace projections may include canonical board membership plus derived scan summaries/freshness in one response, but clients must treat the derived portions as convenience output rather than canonical truth.
- Compute staleness in the projection worker / explicit rebuild path (see §9); reads consume the last materialized state.
- Rebuild all derived views explicitly and idempotently.

Derived/workspace projection reads are convenience read models for CLI and UI
ergonomics. They MUST remain derivable from canonical state and MUST NOT become
the durable automation contract.

---

## 8. Provenance and grounding rules

### 8.1 Provenance shape
Provenance MUST conform to the standardized shape defined in `anx-schema.yaml` → `provenance`:
- `sources`: list of discrete provenance labels (never numeric confidence scores)
- `notes`: optional free-text explanation
- `by_field`: optional per-field provenance map, required when restricted fields are updated

Examples of source labels: `receipt:<artifact_id>`, `decision:<event_id>`, `event:<event_id>`, `inferred`

The `inferred` label indicates the system generated or updated a value without direct evidence.

### 8.2 Restricted updates
The following MUST NOT be updated without a typed reference to a receipt artifact or decision event:
- `card.resolution → done` when completing work in the **done** lane (requires `artifact:<receipt_id>` OR `event:<decision_event_id>` via `resolution_refs`)
- Any "metric improved" claim
- Any "shipped/sent/deployed" assertion

anx-core MUST enforce these restrictions at the API level — reject the write if the required typed reference is missing. The provenance `by_field` map MUST include the source label for the restricted field being updated.

### 8.3 Interpretive fields
The following MAY be updated without receipts:
- `topic.summary`
- `topic.title`

### 8.4 Lifecycle state
- Topic, board, document, and card lifecycle state is derived from
  `archived_at` / `trashed_at` timestamps.
- Archive, trash, and restore are explicit lifecycle operations that emit
  dedicated events. Clients MUST NOT patch a mutable topic or board status.
- Card work progress is represented primarily by `column_key`; when a card is
  in the **done** lane, nullable `resolution` is **`done`** only and reflects
  terminal completion backed by `resolution_refs`. Abandonment uses trash
  lifecycle fields, not a `canceled` resolution.

These fields SHOULD include provenance indicating who updated them and on what basis.

---

## 9. Staleness

Launch threads are timeline anchors only: backing-thread JSON does not carry
cadence or next-check-in semantics for core inference.

The core does **not** auto-emit `exception_raised` / `stale_topic` from derived
rebuild or projection maintenance. Operators may still append an
`exception_raised` event with subtype `stale_topic` explicitly; derived inbox then
surfaces `risk_exception` like other exception-backed items.

Read handlers MUST NOT mint stale-topic exceptions as a side effect of GET
requests.

Derived projection refresh SHOULD run asynchronously from a durable dirty queue.
Operational health SHOULD expose queue depth/lag and last successful maintenance
tick so operators can distinguish normal eventual consistency from maintenance
failure.

---

## 10. Reference conventions

anx-core MUST enforce the reference conventions defined in `anx-schema.yaml` → `reference_conventions`.

**Topic vs thread for human attention:** Operators think in topics, cards, boards, and docs. Human-attention events are still grounded by the backing thread for timeline/audit purposes: `human_attention_requested` includes `thread:<thread_id>` plus a typed `subject_ref`, and `human_attention_responded` includes `inbox:<inbox_item_id>` plus response metadata. Do not model operator requests as decision lifecycle events.

Key rules:
- All ref strings MUST use typed prefixes (`artifact:`, `event:`, `thread:`, `topic:`, `document:`, `board:`, `card:`, `url:`, `inbox:`). Unknown prefixes are preserved, not rejected.
- All thread-scoped events MUST set `thread_id`.
- Each event type has required and optional refs (see schema for full list).
- All packet artifacts MUST include `subject_ref` plus the required packet-kind refs in `artifact.refs`; the subject can be a topic, thread, card, board, or document as allowed by the packet schema and core resolver.
- `topic_updated`, `card_updated`, `card_moved`, and `card_resolved` events SHOULD include `changed_fields` or equivalent change details in their payload when applicable.
- `card.resolution -> done` MUST include either `artifact:<receipt_id>` or `event:<decision_event_id>` in refs (matching the restricted transition rule).

anx-core SHOULD validate required refs on event creation and reject events missing them.

---

## 11. Compatibility and evolution

- Schemas evolve additively. Fields are added, not removed or renamed.
- Unknown fields on any object MUST be preserved and round-tripped (enforced by patch/merge semantics on mutable resources).
- Unknown fields on known event and artifact envelopes MUST remain retrievable; unknown event types and artifact kinds are rejected by strict enum policy.
- Unknown ref prefixes MUST be preserved and round-tripped.
- anx-core MUST expose a version endpoint (`/version` or equivalent) returning the `anx-schema.yaml` version so clients can adapt.
