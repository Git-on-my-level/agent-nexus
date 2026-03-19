# oar-core — Spec (v0.2.2)

## 0. Purpose

oar-core is the canonical state and evidence system for Organization Autorunner (OAR).

OAR is a manager and executive operating system, not a generic work-management tool. The product foundation and architecture decisions are documented in [docs/architecture/foundation.md](../../docs/architecture/foundation.md). oar-core implements the canonical runtime truth (SQLite + filesystem blobs) and owns the institutional memory — the durable truth, the evidence trail, the coordination artifacts. It has no opinion on how actors are instantiated, orchestrated, or upgraded. An actor authenticates with an ID, reads state, does work, writes back. Whether that actor is a human, a Claude agent, an open-source agent framework, or something that doesn't exist yet is outside oar-core's scope.

oar-core:
- Maintains durable organizational state (events, snapshots, artifacts, documents, boards).
- Stores and validates structured coordination artifacts (work orders, receipts, reviews).
- Enforces evidence-first grounding rules on restricted state transitions.
- Computes derived views (inbox, staleness) from canonical data.
- Exposes a programmatic API and contract surface that CLI/generated clients can use.

oar-core does **not**:
- Perform real-world side effects (sending, spending, posting, deploying).
- Hold credentials for external systems.
- Orchestrate, dispatch to, or manage agents.
- Rely on chat history as a memory mechanism.

---

## 1. Design constraints

### 1.1 Canonical vs derived
- oar-core MUST separate **canonical truth** (events, snapshots, artifacts) from **derived views** (inbox items, staleness flags).
- Derived views MUST be regenerable from canonical data.
- Canonical data MUST be replayable and auditable.

### 1.2 Evidence-first progress
- Any state change that closes a loop (marking "done", asserting "shipped", claiming "metric improved") MUST be grounded in a receipt artifact or an explicit decision event.

### 1.3 Small, stable primitives
- oar-core centers on three canonical primitives. Everything else is a typed convention over those primitives.

### 1.4 Actor-agnostic
- oar-core treats all actors as opaque IDs. It does not distinguish between humans and agents at the API or storage level. Identity metadata (display name, tags) lives in a lightweight actor registry.

### 1.5 Implementation baseline (v0)
- The reference backend implementation is Go-first.
- Language choice MUST NOT change the external contract: HTTP/JSON API + SQLite/filesystem storage remain the system boundary.

---

## 2. Storage model

### 2.1 SQLite + blob backend seam
- **SQLite** stores events (rows), snapshots (rows), artifact metadata (rows), documents (rows), document_revisions (rows), actor registry (rows), and derived views (rows).
- **Blob storage** stores artifact content. The first backend is the local filesystem, referenced by `content_path` in the artifact metadata row.
- Hosted deployments MUST treat blob storage as a replaceable backend seam rather than a client-visible contract.
- Clients and agents SHOULD prefer the API, CLI, and generated clients over direct filesystem access.

### 2.2 Schema authority
- `oar-schema.yaml` is the authoritative field/type definition shared between oar-core and oar-ui.
- oar-core MUST enforce schema constraints on writes where specified (restricted transitions, required fields, packet validation, typed ref format).
- **Strict enums** (e.g., `thread_status`, `commitment_status`): oar-core MUST reject unknown values.
- **Open enums** (e.g., `event_type`, `artifact_kind`): oar-core MUST accept and store unknown values.
- Unknown fields on any object MUST be preserved and round-tripped.

### 2.3 Snapshot update semantics
- Snapshot updates use **patch/merge** semantics: only specified fields are updated; unspecified fields (including unknown fields) are preserved unchanged.
- **List-valued fields** (e.g., `tags`, `key_artifacts`, `links`) are **replaced wholesale** when present in a patch. Absence means no change.
- This prevents older clients from accidentally erasing fields they don't know about.

---

## 3. Canonical primitives

### 3.1 Event (append-only)
A durable record that something happened or that an actor claims something happened.

**Behavior:**
- Events MUST be append-only. Never edited or deleted.
- Corrections MUST be new events.
- The system MUST accept unknown event types without breaking storage or retrieval.
- All values in `refs` MUST use typed reference strings (see `oar-schema.yaml` → `ref_format`).

**Fields:** per `oar-schema.yaml` → `primitives.event`

**v0 event types:** `message_posted`, `work_order_created`, `receipt_added`, `review_completed`, `decision_needed`, `decision_made`, `snapshot_updated`, `commitment_created`, `commitment_status_changed`, `exception_raised`, `inbox_item_acknowledged`

### 3.2 Snapshot (mutable current state)
The current best-known state of a durable object (thread, commitment).

**Behavior:**
- Snapshots are mutable via patch/merge (see §2.3).
- Every mutation MUST emit a `snapshot_updated` event referencing the snapshot ID via `snapshot:<id>`. The event payload SHOULD include `changed_fields` (list of field names that changed).

**Fields:** per `oar-schema.yaml` → `primitives.snapshot`

Snapshots SHOULD be small and reference larger content via artifacts.

### 3.3 Artifact (immutable content)
An immutable blob representing a specific version of content at a point in time.

**Behavior:**
- Artifacts MUST be immutable. New versions are new artifacts.
- Artifact metadata lives in SQLite. Content lives on the filesystem at `content_path`.
- Artifact content is **generally opaque** to oar-core — it stores and retrieves but does not interpret. **Exception:** packet kinds (`work_order`, `receipt`, `review`) are schema-validated structured content. oar-core MUST validate their required fields, constraints, and ID consistency on write (see §5).
- All values in `refs` MUST use typed reference strings.

**Fields:** per `oar-schema.yaml` → `primitives.artifact`

---

## 4. Typed conventions (v0)

These are schema conventions over the primitives, not new primitive types.

### 4.1 Thread (snapshot)
The unit of ongoing context — a project, incident, relationship, process, or initiative.

**Fields:** per `oar-schema.yaml` → `snapshots.thread`

**Core-maintained fields:**
- `open_commitments`: oar-core MUST automatically update this field when commitments are created, change status, or are deleted. Clients MUST NOT write this field directly; oar-core MUST reject direct writes to it.

### 4.2 Commitment (snapshot)
A trackable obligation: "we owe X by Y."

**Fields:** per `oar-schema.yaml` → `snapshots.commitment`

**Restricted transitions:**
- `status → done` requires a typed reference to a receipt artifact (`artifact:<id>`) or a decision event (`event:<id>`).
- `status → canceled` requires a typed reference to a decision event (`event:<id>`).
- oar-core MUST reject these transitions if the required reference is missing.

When a commitment is created or its status changes, oar-core MUST update the parent thread's `open_commitments` field accordingly.

---

## 5. Artifact packet conventions (v0)

Work orders, receipts, and reviews are stored as artifacts with structured content. They are the primary coordination surface — any actor can create or consume them.

### 5.1 Packet validation rules
- Packet content ID fields (`work_order_id`, `receipt_id`, `review_id`) MUST equal the enclosing artifact's `id`. oar-core MUST reject mismatches.
- All required fields per the packet schema MUST be present. oar-core MUST reject packets missing required fields.
- All packet artifacts MUST include `thread:<thread_id>` in `artifact.refs` (see reference conventions in `oar-schema.yaml`).
- All typed ref fields in packets (e.g., `context_refs`, `outputs`, `verification_evidence`, `evidence_refs`) MUST use typed reference strings.

### 5.2 Work order
A coordination artifact: "here's what needs doing and how to verify it's done." Any actor may create a work order. Any actor may pick one up.

**Fields:** per `oar-schema.yaml` → `packets.work_order`

Work orders MUST be self-contained: an actor picking up a work order should be able to start working without additional context beyond what is referenced.

Creating a work order MUST emit a `work_order_created` event. Per reference conventions, the event `refs` MUST include `artifact:<work_order_artifact_id>`.

### 5.3 Receipt
Structured evidence that work was done.

**Fields:** per `oar-schema.yaml` → `packets.receipt`

**Validation:** Receipts that are primarily prose with no evidence links MUST be rejected. The `outputs` and `verification_evidence` fields MUST each contain at least one typed reference.

Creating a receipt MUST emit a `receipt_added` event. Per reference conventions, the event `refs` MUST include `artifact:<receipt_artifact_id>` and `artifact:<work_order_artifact_id>`.

### 5.4 Review
A lightweight assessment of a receipt against its work order.

**Fields:** per `oar-schema.yaml` → `packets.review`

Creating a review MUST emit a `review_completed` event. Per reference conventions, the event `refs` MUST include `artifact:<review_artifact_id>`, `artifact:<receipt_artifact_id>`, and `artifact:<work_order_artifact_id>`. If the outcome is `revise`, the reviewer SHOULD create a follow-up work order.

### 5.5 Documents (first-class lifecycle)

Documents are a first-class canonical domain with their own API and storage. A document is a long-lived lineage, not just stored text: it has a mutable head pointer for the current version and an ordered immutable revision chain for institutional memory. Documents are distinct from the generic artifact model: artifacts are immutable blobs linked only by refs, while documents provide a canonical lineage interface over those immutable revision artifacts.

**Storage model:**
- `documents` table: document metadata, head_revision_id, tombstone fields.
- `document_revisions` table: revision_id, document_id, revision_number, prev_revision_id, artifact_id, revision_hash.
- Each revision's content is stored in an `artifacts` row with `kind: "doc"`. Content uses content-addressable storage (SHA-256 digest).
- Revisions form a Merkle chain: `revision_hash` incorporates content_hash, prev_revision_hash, document_id, revision_number, created_at, created_by.

**API surface:** `GET /docs`, `POST /docs`, `GET /docs/{document_id}`, `PATCH /docs/{document_id}`, `GET /docs/{document_id}/history`, `GET /docs/{document_id}/revisions/{revision_id}`, `POST /docs/{document_id}/tombstone`.

**Relationship to artifacts:** Document revisions use artifacts internally for content storage. The docs API is the canonical interface for document lineages; clients should not treat documents as `GET /artifacts?kind=doc`. Documents complement canonical threads/events/artifacts rather than replacing them.

### 5.6 Boards (canonical organizing layers)

Boards are first-class canonical coordination resources. A board is not just UI sugar over threads: it is a durable organizational map over work with canonical board metadata plus canonical card membership over threads, and optional canonical links to document lineages.

**Canonical storage:**
- `boards` table: durable board metadata, owners, primary thread, optional primary document, and optimistic concurrency token.
- `board_cards` table: explicit canonical board membership over threads, including column placement, ordering token, and optional pinned document lineage.

**Projection boundary:** `boards.workspace` may hydrate canonical backing resources and derived summaries for operator convenience, but the payload must keep those layers explicit: canonical membership/backing refs stay distinct from derived summary/freshness. Derived board scans remain rebuildable from canonical state.

---

## 6. Actor registry

The actor registry is a lightweight table mapping actor IDs to metadata.
Hosted-v1 workspace principals include both passkey-authenticated humans and
Ed25519 key-pair agents. oar-core does not enforce fine-grained role-based
access in v1 — any authenticated principal can perform any operation. The
registry exists for display, attribution, and future evolution (authority
tiers, reliability scores, invite lifecycle metadata, etc.).

**Fields:** per `oar-schema.yaml` → `actor.registry_fields`

- Actor IDs are referenced by `actor_id` on events, `updated_by` on snapshots, and `created_by` on artifacts.
- oar-core SHOULD reject writes with an unregistered `actor_id` (to prevent typos and orphaned references).

---

## 7. API surface

oar-core MUST expose a programmatic API (protocol: HTTP/JSON for v0).
Hosted-v1 target state requires authentication on all workspace data routes
outside development mode, although some current v0 code paths are still being
aligned to that target. All write operations require an `actor_id` or an
authenticated principal that resolves to one.

### 7.1 Read / query
- Get thread by ID
- List threads (filters: status, priority, tags, staleness)
- Get thread timeline (events + referenced snapshots/artifacts, ordered by time)
- Get snapshot by ID
- List commitments (filters: thread, owner, status, due date range)
- Get artifact by ID (metadata + content path)
- List artifacts (filters: kind, thread, time range)
- List documents, get document head, get document history, get document revision
- Get inbox items (grouped by category, sorted by time/due date)

### 7.2 Write / mutate
- Register actor
- Create thread → emits `snapshot_updated` event with `snapshot:<thread_id>` in refs
- Update thread snapshot fields (patch/merge) → emits `snapshot_updated` event (rejects writes to core-maintained fields)
- Create commitment → emits `commitment_created` event with `snapshot:<commitment_id>` in refs, updates parent thread `open_commitments`
- Update commitment (patch/merge) → emits `commitment_status_changed` or `snapshot_updated` event (enforces restricted transitions with typed refs), updates parent thread `open_commitments` if status changed
- Create artifact (metadata + content) → returns artifact ID; validates packet content for packet kinds; validates typed ref format
- Create document, update document (new immutable revision), tombstone document
- Append event (for messages, decisions, exceptions, acknowledgments) → validates required refs per reference conventions

### 7.3 Convenience operations
- Create work order (validates packet + refs, creates artifact + emits `work_order_created` with required typed refs)
- Submit receipt (validates evidence + packet + refs, creates artifact + emits `receipt_added` with required typed refs)
- Submit review (validates packet + refs, creates artifact + emits `review_completed` with required typed refs)
- Record decision (`decision_needed` or `decision_made` event + optional artifact)
- Acknowledge inbox item (emits `inbox_item_acknowledged` event with `inbox:<inbox_item_id>` in refs)

### 7.4 Derived views
- Derived views are asynchronously materialized from canonical writes; GET endpoints remain side-effect free.
- Canonical writes enqueue affected thread projections for background refresh rather than recomputing projections inline on reads.
- Get inbox items from the materialized projection layer (respects acknowledgment suppression; uses deterministic IDs; surfaces freshness metadata when projections are pending, missing, or errored).
- Board workspace projections may include canonical board membership plus derived scan summaries/freshness in one response, but clients must treat the derived portions as convenience output rather than canonical truth.
- Compute staleness in the projection worker / explicit rebuild path (see §9); reads consume the last materialized state.
- Rebuild all derived views explicitly and idempotently.

Derived/workspace projection reads are convenience read models for CLI and UI
ergonomics. They MUST remain derivable from canonical state and MUST NOT become
the durable automation contract.

---

## 8. Provenance and grounding rules

### 8.1 Provenance shape
Provenance MUST conform to the standardized shape defined in `oar-schema.yaml` → `provenance`:
- `sources`: list of discrete provenance labels (never numeric confidence scores)
- `notes`: optional free-text explanation
- `by_field`: optional per-field provenance map, required when restricted fields are updated

Examples of source labels: `receipt:<artifact_id>`, `decision:<event_id>`, `actor_statement:<event_id>`, `inferred`

The `inferred` label indicates the system generated or updated a value without direct evidence.

### 8.2 Restricted updates
The following MUST NOT be updated without a typed reference to a receipt artifact or decision event:
- `commitment.status → done` (requires `artifact:<receipt_id>` OR `event:<decision_event_id>`)
- `commitment.status → canceled` (requires `event:<decision_event_id>`)
- Any "metric improved" claim
- Any "shipped/sent/deployed" assertion

oar-core MUST enforce these restrictions at the API level — reject the write if the required typed reference is missing. The provenance `by_field` map MUST include the source label for the restricted field being updated.

### 8.3 Interpretive fields
The following MAY be updated without receipts:
- `thread.current_summary`
- `thread.next_actions`

These fields SHOULD include provenance indicating who updated them and on what basis.

---

## 9. Staleness

Threads define cadence; staleness is evaluated against cadence.

- `reactive`: no scheduled check-ins — thread wakes on inbound events only.
- `cron` (5-field expression): thread is stale when `now > next_check_in_at` AND no receipt or decision event has occurred since the previous expected cron run.
- Legacy `daily | weekly | monthly | custom` values remain accepted for compatibility and retain their historical window behavior.

When staleness is detected by background maintenance or a deterministic derived
rebuild, oar-core MUST:
- Emit an `exception_raised` event with subtype `stale_thread`.
- Surface the thread as an inbox item with category `exception`.

Read handlers MUST NOT mint stale-thread exceptions as a side effect of GET
requests.

Staleness computation SHOULD run on a regular interval (implementation-defined)
or be triggerable on demand.

Derived projection refresh SHOULD run asynchronously from a durable dirty queue.
Operational health SHOULD expose queue depth/lag and last successful stale-scan
time so operators can distinguish normal eventual consistency from maintenance
failure.

---

## 10. Reference conventions

oar-core MUST enforce the reference conventions defined in `oar-schema.yaml` → `reference_conventions`.

Key rules:
- All ref strings MUST use typed prefixes (`artifact:`, `snapshot:`, `event:`, `thread:`, `url:`, `inbox:`). Unknown prefixes are preserved, not rejected.
- All thread-scoped events MUST set `thread_id`.
- Each event type has required and optional refs (see schema for full list).
- All packet artifacts MUST include `thread:<thread_id>` in `artifact.refs`.
- `snapshot_updated` events SHOULD include `changed_fields` in their payload.
- `commitment_status_changed` to `done` MUST include either `artifact:<receipt_id>` or `event:<decision_event_id>` in refs (matching the restricted transition rule).

oar-core SHOULD validate required refs on event creation and reject events missing them.

---

## 11. Compatibility and evolution

- Schemas evolve additively. Fields are added, not removed or renamed.
- Unknown fields on any object MUST be preserved and round-tripped (enforced by patch/merge semantics on snapshots).
- Unknown event types and artifact kinds MUST be stored and retrievable (enforced by open enum policy).
- Unknown ref prefixes MUST be preserved and round-tripped.
- oar-core MUST expose a version endpoint (`/version` or equivalent) returning the `oar-schema.yaml` version so clients can adapt.
