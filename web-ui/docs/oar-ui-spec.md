# oar-ui — Spec (v0.2.2)

## 0. Purpose

oar-ui is the human-facing interface for Organization Autorunner.

It provides visibility into the shared workspace maintained by oar-core and a surface for human intervention: decisions, reviews, snapshot edits, and message posting. It is one of many possible clients of oar-core — agents interact through the API directly; humans interact through this UI.

oar-ui does **not**:

- Maintain an independent database of organizational state.
- Perform real-world side effects (sending, spending, posting, deploying).
- Manage or orchestrate agents.

---

## 1. Integration contract with oar-core

### 1.1 Single source of truth

- oar-ui MUST treat oar-core as the system of record.
- All persistent changes MUST be executed via oar-core API calls.
- oar-ui MAY cache for performance, but caches MUST be invalidatable and MUST NOT create divergent state.

### 1.2 Object compatibility

- oar-ui MUST support all primitives and typed conventions defined in `/contracts/oar-schema.yaml`: events, snapshots, artifacts, threads, commitments, work orders, receipts, reviews.
- oar-ui MUST respect **enum policies**: strict enums are closed sets; open enums may contain unknown values.
- oar-ui MUST handle unknown event types and artifact kinds gracefully — render them as "unknown type" with raw payload, summary, and refs visible.
- oar-ui MUST handle unknown fields on any object gracefully — preserve them on round-trip and do not hide them from display.

### 1.3 Typed references

- All ref strings use typed prefixes as defined in `/contracts/oar-schema.yaml` → `ref_format` (e.g., `artifact:<id>`, `snapshot:<id>`, `event:<id>`, `thread:<id>`, `inbox:<id>`, `url:<url>`).
- oar-ui MUST parse ref prefixes to determine link targets and render appropriate navigation (e.g., `artifact:` links navigate to artifact detail, `url:` links open externally, `event:` links scroll to timeline entry).
- Unknown ref prefixes MUST be rendered as raw text, not hidden or discarded.

### 1.4 Actor identity

- The UI MUST authenticate the current user as an actor ID from the oar-core actor registry.
- Every write operation MUST include the actor ID.
- The UI displays actor `display_name` wherever `actor_id` appears.

### 1.5 Provenance visibility

- oar-ui MUST display provenance using the standardized provenance shape defined in `/contracts/oar-schema.yaml` → `provenance`.
- The `sources` list determines display: if any source is `inferred`, the UI MUST render it visually distinct from evidence-backed provenance (e.g., different color, icon, or label).
- When `by_field` is present (restricted field updates), the UI MUST show per-field provenance on the relevant fields.
- Provenance MUST be displayed on: commitment status, and any field flagged as restricted by the schema.

### 1.6 Snapshot update semantics

- oar-ui MUST use **patch/merge** semantics when updating snapshots: send only changed fields.
- **List-valued fields** (e.g., `tags`, `key_artifacts`, `links`) are **replaced wholesale** when present in a patch. Absence means no change.
- This ensures unknown fields (added by newer clients or agents) are preserved.
- oar-ui MUST NOT write to core-maintained fields (e.g., `thread.open_commitments`).

### 1.7 Reference conventions

- oar-ui MUST follow the reference conventions defined in `/contracts/oar-schema.yaml` → `reference_conventions` when creating events.
- oar-ui relies on these conventions for deterministic navigation: e.g., a `receipt_added` event's `refs` will always contain `artifact:<receipt_id>` and `artifact:<work_order_id>`, enabling the UI to link directly to both.

---

## 2. Core UX model: thread-first timelines

### 2.1 Threads as navigation backbone

The primary navigation unit is the **thread**. All other objects (events, commitments, artifacts, work orders, receipts) are accessed through or in relation to threads.

### 2.2 Thread detail: snapshot + timeline

A thread detail view presents two complementary layers:

**Snapshot (current state):** The thread's `current_summary`, `next_actions`, status, priority, schedule (`cadence`), and linked commitments. This is the "what's true right now" view. Editable in place (except core-maintained fields like `open_commitments`, which are read-only in the UI).

**Timeline (audit trail):** A time-ordered, append-only sequence of all events associated with the thread. Each timeline entry shows type, timestamp, actor, summary, and refs (rendered as navigable typed-ref links). The timeline includes messages, work order creation, receipt submission, reviews, decisions, exceptions, acknowledgments, and snapshot updates.

The snapshot is interpretive and mutable. The timeline is durable and immutable. The UI MUST make this distinction clear.

### 2.3 Timeline rendering

- Ordering MUST be time-based and stable.
- Different event types SHOULD be visually distinguishable (icons, colors, or labels).
- Typed refs in event entries SHOULD render as navigable links (artifact refs open artifact detail, snapshot refs scroll to the snapshot, URL refs open externally).
- Artifact-typed events (work orders, receipts, reviews) SHOULD be expandable inline or navigable to the artifact detail. The UI uses event `refs` (per reference conventions) to locate the linked artifacts.
- `snapshot_updated` events SHOULD display `changed_fields` from the event payload when available.
- Unknown event types MUST render without breaking the timeline.

---

## 3. Required UI surfaces (v0)

### 3.1 Inbox

A dedicated surface showing items that need human attention.

**Display:**

- Inbox items grouped by category: `decision_needed`, `exception`, `commitment_risk`.
- Within each category, sorted by time or due date (no ranking engine in v0).
- Each item shows: title, category, recommended action, and a link to the relevant thread/commitment.
- Inbox item IDs are deterministic (see schema) and stable across rebuilds.

**Actions:**

- Navigate to relevant thread, commitment, or artifact.
- Acknowledge/dismiss an item → emits an `inbox_item_acknowledged` event with `inbox:<inbox_item_id>` in refs. Acknowledged items are suppressed from the inbox unless a new triggering event occurs after the acknowledgment.
- Record a decision (creates a `decision_made` event with notes and typed refs).

### 3.2 Thread list

A filterable list of all threads.

**Filters:** status, priority, tags, cadence preset/staleness.

Cadence filter presets are `Reactive`, `Daily`, `Weekly`, `Monthly`, and `Custom`.
Storage is `reactive` or cron; preset filters map cron values to these categories.

Each row shows: title, status, priority, cadence indicator, staleness indicator, last activity timestamp.

### 3.3 Thread detail

The primary working surface. Combines the snapshot view and timeline described in §2.

**Must support:**

- Viewing and editing thread snapshot fields: title, status, priority, type, cadence schedule (preset or custom cron), next check-in, tags, current summary, next actions. (`open_commitments` is displayed but not directly editable.)
- Viewing open commitments. Creating and editing commitments (with restricted transition enforcement — see §4).
- Viewing the full timeline with navigable typed-ref links.
- Linking artifacts to the thread (adds typed refs to `key_artifacts`).
- Posting messages (creates `message_posted` events on the thread).

### 3.4 Work order composer

A form for creating a work order artifact within a thread context.

**Fields:** objective, constraints, context refs (link existing artifacts as `artifact:<id>` or paste external URLs as `url:<url>`), acceptance criteria, definition of done.

**Actions:**

- Save (creates work order artifact + `work_order_created` event via oar-core, with typed refs per reference conventions).

The composer SHOULD pre-populate `thread_id` and suggest relevant context refs from the thread's existing artifacts.

### 3.5 Receipt viewer

A view for inspecting receipt artifacts.

**Must show:** outputs (as navigable typed-ref links), verification evidence (as navigable typed-ref links), changes summary, known gaps.

**Receipt intake:** The UI MUST support at least manual creation of a receipt artifact (fill in fields, attach evidence as typed refs, save). Agents will typically submit receipts via the API directly, but humans need a UI path too.

**Review action:** From a receipt, the user can initiate a review — select outcome (accept / revise / escalate), write notes, attach evidence as typed refs. This creates a review artifact + `review_completed` event (with typed refs per reference conventions). If the outcome is `revise`, the UI SHOULD prompt creation of a follow-up work order.

---

## 4. Grounding and restricted updates

### 4.1 Restricted transition enforcement

When a user attempts to set `commitment.status → done` or `→ canceled`, the UI MUST:

- Require the user to attach a receipt artifact reference as `artifact:<id>` (for `done`) or record an explicit decision event as `event:<id>` (for either `done` or `canceled`).
- Block the save until the typed reference is provided.
- Submit the transition through oar-core, which enforces the restriction server-side.
- Display the resulting per-field provenance (from `provenance.by_field`) on the commitment status.

### 4.2 Evidence affordances

The UI SHOULD make it easy to:

- Attach typed refs (artifact IDs, external URLs) to any event or snapshot edit.
- View all evidence associated with a commitment (linked receipts, reviews, decisions — navigable via typed refs in related events).
- See at a glance whether a commitment's status is backed by evidence or inferred (via provenance display).

---

## 5. Messages

### 5.1 Messages as events

Messages posted in the UI MUST be stored as `message_posted` events in oar-core. Messages MUST be associated with a thread.

### 5.2 Replies

Replies SHOULD reference the parent event ID as `event:<parent_event_id>` in the `refs` field (per reference conventions). The timeline renders these in time order within the thread — no separate threading UI is needed for v0.

---

## 6. Concurrency

- oar-ui MUST assume multiple writers (humans and agents) may update oar-core concurrently.
- The UI SHOULD poll or subscribe for changes and refresh when canonical state changes.
- For v0, optimistic locking on snapshot edits is sufficient: if a snapshot's `updated_at` has changed since the UI loaded it, warn the user and reload before saving. Patch/merge semantics with wholesale list replacement reduce the risk of accidental field erasure.

---

## 7. Extensibility

- New event types and artifact kinds will be added over time (open enums).
- Unknown types MUST render in the timeline without breaking the UI.
- Unknown fields on any object MUST be preserved on round-trip.
- Unknown ref prefixes MUST be rendered as raw text, not hidden.
- New snapshot types beyond thread and commitment may be added in future versions. The UI SHOULD degrade gracefully if it encounters an unknown snapshot type (display raw fields).

---

## 8. v0 release definition

oar-ui v0 is complete when it can:

- Display the inbox grouped by category with navigation to relevant threads, and support acknowledgment that persists across inbox rebuilds.
- List and filter threads.
- Show thread detail with editable snapshot (patch/merge, respecting core-maintained fields) and full timeline with navigable typed-ref links.
- Create and edit commitments with restricted transition enforcement and per-field provenance display.
- Create work orders within a thread with typed context refs.
- View receipts and their evidence links as navigable typed refs.
- Perform a lightweight review (outcome + notes + typed evidence refs).
- Post messages on a thread.
- Render provenance with visual distinction between evidence-backed and inferred sources.
- Parse and navigate typed reference strings across all surfaces.
