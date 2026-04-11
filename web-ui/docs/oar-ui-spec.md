# oar-ui — Spec (v0.2.3)

## 0. Purpose

oar-ui is the operator interface for Organization Autorunner.

OAR is a manager and executive operating system, not a generic work-management tool. The product foundation and architecture decisions are documented in [docs/architecture/foundation.md](../../docs/architecture/foundation.md). oar-ui provides visibility into the workspace maintained by oar-core and a surface for operator intervention: topics, boards, cards, documents, packets, and message posting. It is one of many possible clients of oar-core — agents should prefer the CLI and generated clients; operators use this UI.

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

- oar-ui MUST support all primitives and typed conventions defined in `/contracts/oar-schema.yaml`: events, topics, cards, boards, documents, artifacts, packets, and read-only thread timelines.
- oar-ui MUST respect **enum policies**: strict enums are closed sets; open enums may contain unknown values.
- oar-ui MUST handle unknown event types and artifact kinds gracefully — render them as "unknown type" with raw payload, summary, and refs visible.
- oar-ui MUST handle unknown fields on any object gracefully — preserve them on round-trip and do not hide them from display.

### 1.3 Typed references

- All ref strings use typed prefixes as defined in `/contracts/oar-schema.yaml` → `ref_format` (e.g., `artifact:<id>`, `topic:<id>`, `card:<id>`, `board:<id>`, `document:<id>`, `event:<id>`, `thread:<id>`, `inbox:<id>`, `url:<url>`).
- oar-ui MUST parse ref prefixes to determine link targets and render appropriate navigation (e.g., `artifact:` links navigate to artifact detail, `url:` links open externally, `event:` links scroll to timeline entry).
- Unknown ref prefixes MUST be rendered as raw text, not hidden or discarded.

### 1.4 Actor identity

- The UI MUST authenticate the current operator as an actor ID from the oar-core actor registry.
- Every write operation MUST include the actor ID.
- The UI displays actor `display_name` wherever `actor_id` appears.
- **Auth-first model**: Production deployments require authenticated principals by default.
  - Passkey registration/login creates a linked actor with `principal_kind=human`, `auth_method=passkey`.
  - Ed25519 key registration creates a linked actor with `principal_kind=agent`, `auth_method=public_key`.
  - When `dev_actor_mode=false` (default), the UI MUST NOT show the legacy actor picker/creator flow.
  - When `dev_actor_mode=true` (development convenience), the legacy actor picker/creator flow MAY be shown, clearly labeled as development-only.
  - Browser session state is cookie-backed and same-origin; refresh tokens MUST NOT be written to script-readable browser storage.

### 1.5 Provenance visibility

- oar-ui MUST display provenance using the standardized provenance shape defined in `/contracts/oar-schema.yaml` → `provenance`.
- The `sources` list determines display: if any source is `inferred`, the UI MUST render it visually distinct from evidence-backed provenance (e.g., different color, icon, or label).
- When `by_field` is present (restricted field updates), the UI MUST show per-field provenance on the relevant fields.
- Provenance MUST be displayed on any field flagged as restricted by the schema, plus packet-linked outcomes and other state where evidence vs inference matters.

### 1.6 Topic and card patch semantics

- oar-ui MUST use **patch/merge** semantics when updating topics and cards: send only changed fields.
- **List-valued fields** (e.g., `tags`, `key_artifacts`, `links`) are **replaced wholesale** when present in a patch. Absence means no change.
- This ensures unknown fields (added by newer clients or agents) are preserved.
- oar-ui MUST NOT write to core-maintained fields.

### 1.7 Reference conventions

- oar-ui MUST follow the reference conventions defined in `/contracts/oar-schema.yaml` → `reference_conventions` when creating events.
- oar-ui relies on these conventions for deterministic navigation: e.g., a `receipt_added` event's `refs` will include `artifact:<receipt_id>` and the receipt's `card:<card_id>` subject anchor where applicable, enabling the UI to link to evidence and the card scope.

### 1.8 Canonical operator vocabulary

Operator-facing copy MUST use one term per concept. Banned aliases MUST NOT appear in navigation, buttons, banners, or empty states except where noted as technical exceptions.

| Concept | Canonical term | Banned UI aliases | Allowed technical exceptions |
| --- | --- | --- | --- |
| Soft-delete lifecycle | Trash, trashed, move to trash, restore | tombstone, tombstoned | HTTP paths and machine identifiers follow `contracts/` (`/trash`, `trashed_at`, `trash_reason`, `include_trashed`, `trashed_only`) |
| Root work item | Topic, Topics | backing thread, Threads (as operator-facing label) | `thread_id`, `thread:` refs, `/threads` diagnostic routes |
| Document collection | Docs | Documents (as collection label) | `document` for singular resources and API field names |
| Inbox triage action | Acknowledge | Dismiss | — |
| Operator-facing actor in prose | Operator | user, end user | `actor`, `principal` in identity and auth contexts |
| Irreversible removal | Permanently delete | Purge (primary copy) | CLI/command `purge` where it is the API surface name |

`Artifact` remains the umbrella object; `Receipt` and `Review` are artifact kinds only.

**Domain note:** A **thread** is a core primitive (durable event timeline, `thread_id`, backing streams). A **topic** is the operator-facing work item implemented on top of a thread. Cards, documents, and boards may also reference backing threads that are not the same as a navigable topic row; use **topic** in operator copy when the UI means the organizational unit, and **thread** when the meaning is the timeline primitive, a `thread:` ref, or a read-only inspection route.

---

## 2. Core UX model: topic-and-board workflows

### 2.1 Topics and boards as navigation backbone

The primary navigation unit is the **topic**, with boards and cards for execution. Threads remain the read-only backing timeline for evidence and audit navigation.

### 2.2 Topic detail: timeline + workspace

A topic detail view presents two complementary layers:

**Workspace (current state):** The operator-facing topic record plus related cards, boards, documents, and inbox context from projection endpoints where applicable — title, status, priority, schedule (`cadence`), summary, next actions, and linked evidence. This is the "what's true right now" view. Editable in place only where the schema allows it (topics and cards via their canonical patch APIs).

**Timeline (audit trail):** A time-ordered, append-only sequence of all events on the topic's backing thread. Each timeline entry shows type, timestamp, actor, summary, and refs (rendered as navigable typed-ref links). The timeline includes messages, receipt submission, reviews, decisions, exceptions, acknowledgments, and topic/card lifecycle updates.

Mutable topic and card fields are interpretive and versioned through events. The timeline is durable and append-only. The UI MUST make this distinction clear.

### 2.3 Timeline rendering

- Ordering MUST be time-based and stable.
- Different event types SHOULD be visually distinguishable (icons, colors, or labels).
- Typed refs in event entries SHOULD render as navigable links (artifact refs open artifact detail, `topic:` / `card:` refs open topic or card detail, URL refs open externally).
- Artifact-typed events (receipts, reviews) SHOULD be expandable inline or navigable to the artifact detail. The UI uses event `refs` (per reference conventions) to locate the linked artifacts.
- `topic_updated`, `topic_status_changed`, `card_updated`, and related lifecycle events SHOULD display `changed_fields` (or equivalent change details) from the event payload when available.
- Unknown event types MUST render without breaking the timeline.

### 2.4 URL-backed view state

- Operator-visible state that materially changes which content is shown SHOULD
  be URL-backed when practical, so refresh, share, and back/forward navigation
  restore the same view.
- Examples include selected detail tabs, active filters, revision selectors,
  and composer modes that change the operator's working context.
- Transient form drafts and purely presentational preferences MAY stay outside
  the URL.

---

## 3. Required UI surfaces (v0)

### 3.1 Inbox

A dedicated surface showing items that need operator attention.

**Display:**

- Inbox items grouped by **`category`** using `enums.inbox_category`: `action_needed`, `risk_exception`, and `attention` (aligned with core and OpenAPI). Core v0 derivation mainly emits the first two; `attention` is the canonical bucket for document-review-style items when surfaced. Unknown categories MUST still appear in their own groups (forward compatibility).
- Within each group, sorted by inferred **urgency** (from category plus trigger/source recency) and then by **source or trigger time**; v0 does not add a separate ranking engine beyond that ordering.
- Each item shows: title, category, and a link to the relevant topic/board/card/thread.
- Inbox item IDs are deterministic (see schema) and stable across rebuilds.

**Actions:**

- Navigate to the relevant topic, board, card, thread, or artifact.
- Acknowledge an item → emits an `inbox_item_acknowledged` event with `inbox:<inbox_item_id>` in refs. Acknowledged items are suppressed from the inbox unless a new triggering event occurs after the acknowledgment.
- Record a decision (creates a `decision_made` event with notes and typed refs). The write is anchored on the inbox item's backing **thread** (`thread_id` / `thread:` in event refs). The operator may have arrived via a **topic** route, but durable decision lifecycle events still attach to the backing thread; topic refs are optional context when present, not the anchor.

### 3.2 Topic list

A filterable list of topics (the UI may still expose thread-indexed routes for inspection; the operator-facing noun is **topic**).

**Filters:** status, priority, tags, cadence preset/staleness.

Cadence filter presets are `Reactive`, `Daily`, `Weekly`, `Monthly`, and `Custom`.
Storage is `reactive` or cron; preset filters map cron values to these categories.

Each row shows: title, status, priority, cadence indicator, staleness indicator, last activity timestamp.

### 3.3 Topic detail

The primary working surface. Combines the workspace-style current-state view and timeline described in §2.

**Must support:**

- Viewing and editing topic and card current-state fields: title, status, priority, type, cadence schedule (preset or custom cron), next check-in, tags, current summary, next actions (within schema and core-maintained rules).
- Viewing linked evidence and packet outcomes with restricted transition enforcement where the schema requires it.
- Viewing the full timeline with navigable typed-ref links.
- Linking artifacts to the topic/thread context (adds typed refs to `key_artifacts` where the schema allows).
- Posting messages (creates `message_posted` events on the backing thread).

### 3.4 Receipts and reviews from boards (no thread detail Work tab)

Receipt and review authoring is not a separate thread/topic detail tab. Operators create receipts and reviews from **card detail modals on boards**, where flows remain grounded in `card:<card_id>` subjects (see receipt and review packet contracts) and typed refs per reference conventions.

**Actions:**

- Open a card from a board, use the card detail modal to author receipts and reviews against that card.

### 3.5 Receipt viewer

A view for inspecting receipt artifacts.

**Must show:** outputs (as navigable typed-ref links), verification evidence (as navigable typed-ref links), changes summary, known gaps.

**Receipt intake:** The UI MUST support at least manual creation of a receipt artifact (fill in fields, attach evidence as typed refs, save). Agents will typically submit receipts via the CLI or generated clients, but operators still need a UI path for manual intake.

**Review action:** From a receipt, the operator can initiate a review — select outcome (accept / revise / escalate), write notes, attach evidence as typed refs. This creates a review artifact + `review_completed` event (with typed refs per reference conventions). If the outcome is `revise`, the UI SHOULD steer the operator back to the topic/card context for follow-up work.

### 3.6 Boards and docs as canonical operator surfaces

Boards and docs are first-class operator surfaces, but they remain grounded in canonical core state.

**Boards:**

- The UI MUST present boards as canonical organizing layers over work, not disposable kanban widgets.
- Board detail MUST distinguish canonical board facts (board metadata, card membership, backing thread/doc refs) from derived scan data (counts, inbox aggregates, freshness badges).
- When board projections are pending, missing, or errored, the UI MUST keep canonical board membership visible while clearly downgrading trust in derived summaries.
- Primary board workflows (create board, edit board metadata, add card, update pinned document) SHOULD use searchable pickers backed by canonical list endpoints. Manual raw-ID entry MAY exist only as an advanced escape hatch.

**Docs:**

- The UI MUST present docs as canonical long-lived lineages with a mutable head and explicit revision history, not generic stored text blobs.
- Doc create/edit workflows SHOULD use searchable thread-link pickers for common linkage flows, with manual raw-ID entry hidden behind an advanced path.
- Doc detail SHOULD make the current head revision versus prior lineage history legible at a glance.

### 3.7 Access management

The Access page provides workspace-local operator visibility and intervention for principals and invites.

**Principal management:**

- The UI MUST display current principals with their agent ID, kind (human/agent), auth method, revocation status, joined time, and last-seen time.
- Any authenticated principal MAY view the principal list.
- An operator MAY revoke another principal through the UI using the `auth principals revoke` API path, which creates an audit trail.
- The UI MUST prevent self-revocation (the calling principal cannot revoke itself through the Access page).
- The UI MUST enforce break-glass protection for the last active human principal:
  - Revoking the last active human requires explicit confirmation, including typing the target agent ID and providing a human-lockout reason.
  - The break-glass flow uses the `allow_human_lockout` and `human_lockout_reason` parameters.

**Invite management:**

- The UI MUST display pending and revoked invites with their invite ID, kind, and creation time.
- Any authenticated principal MAY create and revoke invites.
- The UI SHOULD display created invite tokens with a copy-to-clipboard affordance and a clear warning that tokens are shown only once.

**Audit trail:**

- The Access page SHOULD surface recent auth audit events for operator visibility.

---

## 4. Grounding and restricted updates

### 4.1 Restricted transition enforcement

When an operator attempts to set a restricted state transition on a card or packet-backed field, the UI MUST:

- Require the operator to attach a receipt artifact reference as `artifact:<id>` or record an explicit decision event as `event:<id>` when the schema requires evidence.
- Block the save until the typed reference is provided.
- Submit the transition through oar-core, which enforces the restriction server-side.
- Display the resulting per-field provenance (from `provenance.by_field`) on the affected field.

### 4.2 Evidence affordances

The UI SHOULD make it easy to:

- Attach typed refs (artifact IDs, external URLs) to any event or topic/card edit.
- View all evidence associated with a packet or other restricted field (linked receipts, reviews, decisions - navigable via typed refs in related events).
- See at a glance whether a restricted field is backed by evidence or inferred (via provenance display).

---

## 5. Messages

### 5.1 Messages as events

Messages posted in the UI MUST be stored as `message_posted` events in oar-core. Messages MUST be associated with a thread.

### 5.2 Replies

Replies SHOULD reference the parent event ID as `event:<parent_event_id>` in the `refs` field (per reference conventions). The timeline renders these in time order within the thread — no separate threading UI is needed for v0.

---

## 6. Concurrency

- oar-ui MUST assume multiple writers (operators and agents) may update oar-core concurrently.
- The UI SHOULD poll or subscribe for changes and refresh when canonical state changes.
- For v0, optimistic locking on current-state edits is sufficient: if a view's `updated_at` has changed since the UI loaded it, warn the operator and reload before saving. Patch/merge semantics with wholesale list replacement reduce the risk of accidental field erasure.

---

## 7. Extensibility

- New event types and artifact kinds will be added over time (open enums).
- Unknown types MUST render in the timeline without breaking the UI.
- Unknown fields on any object MUST be preserved on round-trip.
- Unknown ref prefixes MUST be rendered as raw text, not hidden.
- New detail types beyond the current topic/board/card/thread surfaces may be added in future versions. The UI SHOULD degrade gracefully if it encounters an unknown type (display raw fields).

---

## 8. v0 release definition

oar-ui v0 is complete when it can:

- Display the inbox grouped by category with navigation to relevant topics, boards, cards, or threads, and support acknowledgment that persists across inbox rebuilds.
- List and filter topics.
- Show topic and board detail with editable current state (patch/merge, respecting core-maintained fields) and full timeline with navigable typed-ref links.
- View receipts and their evidence links as navigable typed refs.
- Perform a lightweight review (outcome + notes + typed evidence refs).
- Post messages on a thread or topic-backed timeline.
- Render provenance with visual distinction between evidence-backed and inferred sources.
- Parse and navigate typed reference strings across all surfaces.
