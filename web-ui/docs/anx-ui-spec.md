# anx-ui — Spec (v0.6.0)

## 0. Purpose

Agent Nexus web UI is the operator interface for Agent Nexus.

Agent Nexus is a manager and executive operating system, not a generic work-management tool. The product foundation and architecture decisions are documented in [docs/architecture/foundation.md](../../docs/architecture/foundation.md). Agent Nexus web UI provides visibility into the workspace maintained by anx-core and a surface for operator intervention: Inbox triage, Tasks (work projection), Docs, Ask PM, and settings. Boards and cards remain the store behind Tasks; threads remain inspection. It is one of many possible clients of anx-core — agents should prefer the CLI and generated clients; operators use this UI.

Agent Nexus web UI does **not**:

- Maintain an independent database of organizational state.
- Perform real-world side effects (sending, spending, posting, deploying).
- Manage or orchestrate agents.

---

## 1. Integration contract with anx-core

### 1.1 Single source of truth

- Agent Nexus web UI MUST treat anx-core as the system of record.
- All persistent changes MUST be executed via anx-core API calls.
- Agent Nexus web UI MAY cache for performance, but caches MUST be invalidatable and MUST NOT create divergent state.

### 1.2 Object compatibility

- Agent Nexus web UI MUST support all primitives and typed conventions defined in `/contracts/anx-schema.yaml`: events, topics, cards, boards, documents, artifacts, packets, and read-only thread timelines.
- Agent Nexus web UI MUST respect **enum policies**: strict enums are closed sets; open enums may contain unknown values.
- Event types and artifact kinds are strict. Contract drift may surface as an API error; unknown fields inside known envelopes must still remain visible and safe.
- Agent Nexus web UI MUST handle unknown fields on any object gracefully — preserve them on round-trip and do not hide them from display.

### 1.3 Typed references

- All ref strings use typed prefixes as defined in `/contracts/anx-schema.yaml` → `ref_format` (e.g., `artifact:<id>`, `topic:<id>`, `card:<id>`, `board:<id>`, `document:<id>`, `event:<id>`, `thread:<id>`, `inbox:<id>`, `url:<url>`).
- Agent Nexus web UI MUST parse ref prefixes to determine link targets and render appropriate navigation (e.g., `artifact:` links navigate to artifact detail, `url:` links open externally, `event:` links scroll to timeline entry or, for `message_posted`, the message item).
- Unknown ref prefixes MUST be rendered as raw text, not hidden or discarded.

### 1.4 Actor identity

- The UI MUST authenticate the current operator as an actor ID from the anx-core actor registry.
- Every write operation MUST include the actor ID.
- The UI displays actor `display_name` wherever `actor_id` appears.
- **Auth-first model**: Production deployments require authenticated principals by default.
  - Passkey registration/login creates a linked actor with `principal_kind=human`, `auth_method=passkey`.
  - Ed25519 key registration creates a linked actor with `principal_kind=agent`, `auth_method=public_key`.
  - When `dev_actor_mode=false` (default), the UI MUST NOT show the legacy actor picker/creator flow.
  - When `dev_actor_mode=true` (development convenience), the legacy actor picker/creator flow MAY be shown, clearly labeled as development-only.
  - Browser session state is cookie-backed and same-origin; refresh tokens MUST NOT be written to script-readable browser storage.

### 1.5 Provenance visibility

- Agent Nexus web UI MUST display provenance using the standardized provenance shape defined in `/contracts/anx-schema.yaml` → `provenance`.
- The `sources` list determines display: if any source is `inferred`, the UI MUST render it visually distinct from evidence-backed provenance (e.g., different color, icon, or label).
- When `by_field` is present (restricted field updates), the UI MUST show per-field provenance on the relevant fields.
- Provenance MUST be displayed on any field flagged as restricted by the schema, plus packet-linked outcomes and other state where evidence vs inference matters.

### 1.6 Topic and card patch semantics

- Agent Nexus web UI MUST use **patch/merge** semantics when updating topics and cards: send only changed fields.
- **List-valued fields** (e.g., `owner_refs`, `document_refs`, `board_refs`, `related_refs`, `card_refs`, `assignee_refs`, `resolution_refs`) are **replaced wholesale** when present in a patch. Absence means no change.
- This ensures unknown fields (added by newer clients or agents) are preserved.
- Agent Nexus web UI MUST NOT write to core-maintained fields.

### 1.7 Reference conventions

- Agent Nexus web UI MUST follow the reference conventions defined in `/contracts/anx-schema.yaml` → `reference_conventions` when creating events.
- Agent Nexus web UI relies on these conventions for deterministic navigation: e.g., a `receipt_added` event's `refs` will include `artifact:<receipt_id>` and the receipt's `card:<card_id>` subject anchor where applicable, enabling the UI to link to evidence and the card scope.

### 1.8 Canonical operator vocabulary

Operator-facing copy MUST use one term per concept. Banned aliases MUST NOT appear in navigation, buttons, banners, or empty states except where noted as technical exceptions.

**Scope.** This is enforced on the surfaces an operator cannot avoid: primary navigation, the onboarding tour, and the page copy of Inbox, Tasks and Docs. Three places are exempt, because their whole job is to expose the core model:

1. **Diagnostics surfaces** (`/events` "Audit", `/threads`), including their nav entries and headings.
2. **Ref-type labels** rendered by `RefLink` / `refLinkModel` — a `card:` chip may read "Card".
3. **Timeline and audit event rows**, which name the core event that occurred.

Outside those three, the core nouns are still present in operator copy in several places (the Tasks board help, the Watching mailbox group headers, the Topic-detail rendering of `/threads/{id}`). Those are known violations, not sanctioned ones; they are tracked as follow-ups rather than silently permitted. Do not add new ones, and do not read the backlog as license — a rule this document does not enforce is a rule the next reader will ignore.

| Concept | Canonical term | Banned UI aliases | Allowed technical exceptions |
| --- | --- | --- | --- |
| Soft-delete lifecycle | Trash, trashed, move to trash, restore | tombstone, tombstoned | HTTP paths and machine identifiers follow `contracts/` (`/trash`, `trashed_at`, `trash_reason`; list endpoints use repeated `state=active|archived|trashed`) |
| Root work item | Task, Tasks | Topic, Topics, Card, Cards, backing thread, Threads (as operator-facing labels) | `card:` refs, `card_id`, the `work.list` / `work.get` command ids, `thread_id`, `thread:` refs, `/threads` diagnostic detail route |
| Document collection | Docs | Documents (as collection label) | `document` for singular resources and API field names |
| Inbox triage action | Acknowledge | Dismiss | — |
| Operator-facing actor in prose | Operator | user, end user | `actor`, `principal` in identity and auth contexts |
| Irreversible removal | Permanently delete | Purge (primary copy) | CLI/command `purge` where it is the API surface name |

`Artifact` remains the umbrella object; `Receipt` and `Review` are artifact kinds only.

**Domain note:** Operator vocabulary and core vocabulary are deliberately different. The boundary between them is the typed ref.

- **Operator-facing nouns are Inbox, Tasks and Docs, and nothing else.** A **Task** is the operator's unit of work (`work.list` / `work.get` projected over cards).
- **Core primitives — topic, board, card, thread, artifact — are not operator nouns.** They are the durable model that agents address by typed ref (`topic:`, `card:`, `board:`, `document:` — the contract's prefixes, per `contracts/anx-schema.yaml` → `ref_format`; `doc:` is a CLI target shorthand only and is rejected inside a ref) through the CLI and generated clients. The UI renders them, but never asks an operator to think in them.
- A **thread** is infrastructure: a durable append-only event timeline that backs topics, cards, boards and documents, and resolves packet subjects. It is never an operator-facing noun.
- A **topic** is the core discussion/context primitive built on a thread. It has **no operator destination**; it appears only as a ref-type label (e.g. in `RefLink` or an Events filter) and as the detail rendering of its backing thread.

The practical rule: if an operator has to learn a word to use the product, it belongs in the first bullet. Everything else is addressed by ref, and the UI resolves the ref to something the operator already understands.

---

## 2. Core UX model: Inbox, Tasks, Docs

### 2.1 Three product primitives

The primary navigation units are **Inbox**, **Tasks**, and **Docs**. Ask PM is
an action in the shell, not a nav category. Settings (Access, Secrets,
Integrations, Audit) live in the sidebar footer and `/more`.

Tasks is the operator projection over work (`work.list` / `work.get`), shown as
table or board. Boards and cards remain the backing store; they are not
separate product destinations. Threads remain the backing timeline for docs,
inbox deep links and audit inspection at `/threads/...`.

`/threads` and `/events` are **Diagnostics**, not product: they expose core
primitives that are deliberately not operator nouns. Both are reachable only
from the sidebar footer / `/more` hub — `/events` under Settings as "Audit",
`/threads` under a "Diagnostics" group. They MUST NOT appear in primary nav.
A diagnostic surface is labelled and grouped rather than merely unlinked: an
orphaned page reachable only by typing its URL is undiscoverable to the
operator who needs it and unexplained to everyone else.

There are no legacy route aliases: `/work`, `/work/{card_ref}`, `/work/new`,
`/decisions` and `/settings` were removed outright rather than left as
redirects. (`/work` is still a core **API** path and is unrelated.)

### 2.2 Topic detail: timeline + workspace

A topic detail view presents two complementary layers:

**Workspace (current state):** The operator-facing topic record plus related cards, boards, documents, and inbox context from projection endpoints where applicable — title, summary, lifecycle state, linked refs, card progress, and linked evidence. This is the "what's true right now" view. Editable in place only where the schema allows it (topics via title/summary patches; cards via their canonical patch and move APIs).

**Timeline (audit trail):** A time-ordered, append-only sequence of all events on the topic's backing thread. Each timeline entry shows type, timestamp, actor, summary, and refs (rendered as navigable typed-ref links). The timeline includes messages, receipt submission, reviews, decisions, exceptions, acknowledgments, and topic/card lifecycle updates.

Mutable topic and card fields are interpretive and versioned through events. The timeline is durable and append-only. The UI MUST make this distinction clear.

### 2.3 Timeline rendering

- Ordering MUST be time-based and stable.
- Different event types SHOULD be visually distinguishable (icons, colors, or labels).
- Typed refs in event entries SHOULD render as navigable links (artifact refs open artifact detail, `topic:` / `card:` refs open topic or card detail, URL refs open externally).
- Artifact-typed events (receipts, reviews) SHOULD be expandable inline or navigable to the artifact detail. The UI uses event `refs` (per reference conventions) to locate the linked artifacts.
- `topic_updated`, topic lifecycle events (`topic_archived`, `topic_trashed`, `topic_restored`, etc.), `card_updated`, and related lifecycle events SHOULD display `changed_fields` (or equivalent change details) from the event payload when available.
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

### 3.0 Workspace root and Events

The workspace root redirects to Inbox. There is no Home unread-feed destination.

Events is the full workspace event browser under settings. It reads `GET /events`,
supports URL/shareable filter intent for type, group, backing scope, topic, actor,
search, time range, and cursor.
Events stays under More on mobile rather than a primary bottom-nav slot.

### 3.1 Inbox

A dedicated surface showing items that need operator attention.

**Display:**

- Inbox items grouped by generic **`kind`**. The current first-class UI affordances are `ask`, `review`, and `escalate`; unknown kinds MUST still appear in their own groups (forward compatibility).
- Within each group, sorted by inferred **urgency** (from kind, optional severity, and trigger/source recency) and then by **source or trigger time**; v0 does not add a separate ranking engine beyond that ordering.
- Each item shows: title, kind, requester context, and a link to the relevant task, document, thread, or artifact.
- Inbox item IDs are deterministic (see schema) and stable across rebuilds.

**Actions:**

- Navigate to the relevant inbox item, task, document, thread inspection route, or artifact.
- Respond to an item → emits a `human_attention_responded` event with `inbox:<inbox_item_id>` in refs. Responded items are suppressed from the inbox unless a new human attention request is created.
- The respond surface shows agent-authored **`response_proposals`** from the backing `human_attention_requested` event: the first entry is the **recommended** response (highlighted); additional entries are optional fill-ins for the freeform response text. **`review`** items also expose local **Approve** / **Reject** actions that submit fixed response text without using those chips.
- Record a response (creates a `human_attention_responded` event for inbox items, or a `message_posted` event for general notes) with notes and typed refs. The write is anchored on the inbox item's backing **thread** (`thread_id` / `thread:` in event refs). The operator may have arrived via a **topic** route, but durable follow-up events still attach to the backing thread; topic refs are optional context when present, not the anchor.

### 3.2 Thread inspection list

`/threads` is a filterable inspection list of backing conversations (docs-as-rooms, inbox deep links, audit). It is not a fourth product primitive and MUST NOT appear in primary nav.

Document and thread list rows SHOULD use compact inline metrics for scanability. Zero values may be shown when the metric set is stable across rows, but list-only API enrichments such as `timeline_message_count`, `revision_count`, and `head_revision_character_count` remain read hints: the UI must tolerate missing fields and degrade them to zero or an unavailable placeholder rather than treating them as durable editable state.

**Filters:** lifecycle `state` (`active`, `archived`, `trashed`), archive/trash visibility flags, and search (`q`).

Each row shows: title, lifecycle state, summary, and last activity timestamp.

### 3.3 Thread / topic inspection detail

Inspection, not the primary working surface. Combines current-state and timeline as in §2.2 when the operator follows an inbox or doc deep link.

**Must support:**

- Viewing and editing topic current-state fields that remain canonical: title and summary. Topic lifecycle state is derived from archive/trash timestamps and is changed through dedicated archive/trash/restore actions, not a mutable state patch.
- Viewing linked evidence with restricted transition enforcement where the schema requires it.
- Viewing the full timeline with navigable typed-ref links.
- Linking artifacts and documents through typed refs where the schema allows.
- Posting messages (creates `message_posted` events on the backing thread).

Card current-state edits (column, assignees, resolution) live on Tasks (`work` + `cards.move` / card patch), not a board card-detail modal.

### 3.4 Receipts and reviews from Inbox and Tasks

Receipt and review authoring is not a card-detail modal on a `/boards` route. Decisions awaiting an answer are Inbox items. Tasks has no card-detail modal. Flows remain grounded in typed refs (`card:`, `inbox:`, `artifact:`) per reference conventions.

**Actions:**

- Open an Inbox item or task and record the decision or review there.

### 3.5 Receipt viewer

A view for inspecting receipt artifacts.

**Must show:** outputs (as navigable typed-ref links), verification evidence (as navigable typed-ref links), changes summary, known gaps.

**Receipt intake:** The UI MUST support at least manual creation of a receipt artifact (fill in fields, attach evidence as typed refs, save). Agents will typically submit receipts via the CLI or generated clients, but operators still need a UI path for manual intake.

**Review action:** From a receipt, the operator can initiate a review — select outcome (accept / revise / escalate), write notes, attach evidence as typed refs. This creates a review artifact + `review_completed` event (with typed refs per reference conventions). If the outcome is `revise`, the UI SHOULD steer the operator back to the topic/card context for follow-up work.

### 3.6 Tasks board and Docs

Docs are a first-class operator surface. Boards are the backing store Tasks writes through, not a separate destination.

**Tasks:**

- The UI MUST present Tasks as the operator projection over work (`work.list` / `work.get`), as table or board.
- Nexus-owned Tasks board drops MUST persist through `cards.move` with public `card:` refs. Source-owned drops MUST file a PM decision rather than silently mutating the source.
- `/boards` is not an operator destination, and neither is `/work` — the operator route is `/tasks`.
- There is no card-detail modal on a board workspace.

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
- Submit the transition through anx-core, which enforces the restriction server-side.
- Display the resulting per-field provenance (from `provenance.by_field`) on the affected field.

### 4.2 Evidence affordances

The UI SHOULD make it easy to:

- Attach typed refs (artifact IDs, external URLs) to any event or topic/card edit.
- View all evidence associated with a packet or other restricted field (linked receipts, reviews, decisions - navigable via typed refs in related events).
- See at a glance whether a restricted field is backed by evidence or inferred (via provenance display).

---

## 5. Messages

### 5.1 Messages as events

Messages posted in the UI MUST be stored as `message_posted` events in anx-core. Messages MUST be associated with a thread.

### 5.2 Replies

Replies SHOULD reference the parent event ID as `event:<parent_event_id>` in the `refs` field (per reference conventions). The timeline renders these in time order within the thread — no separate threading UI is needed for v0.

---

## 6. Concurrency

- Agent Nexus web UI MUST assume multiple writers (operators and agents) may update anx-core concurrently.
- The UI SHOULD poll or subscribe for changes and refresh when canonical state changes.
- For v0, optimistic locking on current-state edits is sufficient: if a view's `updated_at` has changed since the UI loaded it, warn the operator and reload before saving. Patch/merge semantics with wholesale list replacement reduce the risk of accidental field erasure.

---

## 7. Extensibility

- New event types and artifact kinds are added through contract updates to strict enums.
- Unknown fields inside known types MUST render without breaking the UI.
- Unknown fields on any object MUST be preserved on round-trip.
- Unknown ref prefixes MUST be rendered as raw text, not hidden.
- New detail types beyond the current Inbox / Tasks / Docs / thread-inspection surfaces may be added in future versions. The UI SHOULD degrade gracefully if it encounters an unknown type (display raw fields).

---

## 8. v0 release definition

Agent Nexus web UI v0 is complete when it can:

- Display Inbox grouped by category with navigation to relevant tasks, docs, or thread inspection, and support responses that persist across inbox rebuilds.
- Show Tasks as table and board over `work.list`, including Nexus-owned drag via `cards.move`.
- Show Docs list/detail with head vs revision lineage.
- Inspect backing threads at `/threads` without making that a primary nav primitive.
- View receipts and their evidence links as navigable typed refs.
- Perform a lightweight review (outcome + notes + typed evidence refs) from Inbox / artifact detail — not a board card-detail modal.
- Post messages on a thread or topic-backed timeline.
- Render provenance with visual distinction between evidence-backed and inferred sources.
- Parse typed reference strings across all surfaces (`board:` refs may stay inert in the operator UI).
