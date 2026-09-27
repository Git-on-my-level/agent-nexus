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
- Agent Nexus web UI MUST parse ref prefixes to determine link targets and render appropriate navigation (`card:` opens the Task, `document:` opens the Doc, `thread:` opens thread inspection, `url:` links open externally, `event:` links scroll to the timeline entry or, for `message_posted`, the message item). `topic:`, `board:` and `artifact:` have no operator destination and render as inert labels.
- Unknown ref prefixes MUST be rendered as raw text, not hidden or discarded.

### 1.4 Actor identity

- The UI MUST authenticate the current operator as an actor ID from the anx-core actor registry.
- Every write operation MUST include the actor ID.
- The UI displays actor `display_name` wherever `actor_id` appears.
- Machine identifiers are not labels. Actor ids, machine-minted principal handles (`passkey.<slug>.<hex>`, `external.<hash>`), connection ids, thread refs and raw error payloads are never printed as the name of something. The UI shows the name (or a person-chosen handle) and keeps the identifier behind a copy affordance where an operator may need it for the CLI or a bug report: "Copy actor id" on `/more`, "Copy connection id" and "Copy error" on Integrations (connections read "GitHub · main" when a tool has more than one), "Copy ref" on the Threads list, task and doc pages and in ⌘K.
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
2. **Ref-type labels**, wherever they are rendered — `RefLink` / `refLinkModel` chips and Inbox subject lines (`getInboxSubjectLabel`) — because their job is to name the *ref type*. These still use the operator noun where one exists: a `card:` subject reads "Task", a `topic:` subject reads "Project". Types with no operator equivalent (`thread:`, `artifact:`, `board:`) keep the core name. The exemption is the label's job, not the module it lives in: any new surface that names a ref type follows the same rule, and must not introduce its own noun map.
3. **Timeline and audit event rows**, which name the core event that occurred.

Those three exemptions are the remaining places "card" (and other core nouns) may appear in operator-visible copy. Inbox, Tasks, Docs, onboarding, keyboard help, and compact `RefLink` chips use Task / Project. Do not add new leaks, and do not read an exemption as license to teach core nouns on product surfaces. Task creation asks the operator to choose a Board only when more than one board already exists, because `work.create` defaults the backing board.

The Tasks table and board cards name the board only when more than one board is in view; with one board there is no choice to make, so there is no Board column and no board name on a card.

| Concept | Canonical term | Banned UI aliases | Allowed technical exceptions |
| --- | --- | --- | --- |
| Soft-delete lifecycle | Trash, trashed, move to trash, restore | tombstone, tombstoned | HTTP paths and machine identifiers follow `contracts/` (`/trash`, `trashed_at`, `trash_reason`; list endpoints use repeated `state=active|archived|trashed`) |
| Root work item | Task, Tasks | Topic, Topics, Card, Cards, backing thread, Threads (as operator-facing labels) | `card:` refs, `card_id`, the `work.list` / `work.get` command ids, `thread_id`, `thread:` refs, `/threads` diagnostic detail route |
| Project / work grouping | Project | Topic, Topics (as operator-facing labels) | `topic:` refs; core reports `"projects": "topics"` in `work.capabilities` |
| Backing board | Board — only where the operator has a real choice among boards | Board as a required step, label or column when the workspace has one board | `board:` refs, `board_id`, the `boards.*` command family, diagnostics surfaces |
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
an action in the shell, not a nav category. The account menu in the sidebar
footer and the `/more` hub group the secondary destinations under two labels:
**Settings** (Access, Secrets, Integrations) and **Diagnostics** (Audit,
Threads). Sign out is the last item of the account menu.

Tasks is the operator projection over work (`work.list` / `work.get`), shown as
table or board. Boards and cards remain the backing store; they are not
separate product destinations. Threads remain the backing timeline for docs,
inbox deep links and audit inspection at `/threads/...`.

`/threads` and `/events` are **Diagnostics**, not product: they expose core
primitives that are deliberately not operator nouns. Both are reachable only
from the account menu / `/more` hub under the "Diagnostics" group (`/events`
as "Audit", `/threads` as "Threads"), and from the ⌘K palette's "Go to" list.
They MUST NOT appear in primary nav.
A diagnostic surface is labelled and grouped rather than merely unlinked: an
orphaned page reachable only by typing its URL is undiscoverable to the
operator who needs it and unexplained to everyone else.

There are no legacy route aliases: `/work`, `/work/{card_ref}`, `/work/new`,
`/decisions` and `/settings` were removed outright rather than left as
redirects. (`/work` is still a core **API** path and is unrelated.)

### 2.2 Topic detail: timeline + workspace

There is no topic detail destination. `/threads/{threadId}` is a **read-only diagnostic** view of one backing thread, reached from a typed ref, an inbox deep link, or the `/threads` Diagnostics list. It presents two complementary layers:

**Workspace (current state):** the thread's record and related cards, documents and inbox context from projection endpoints — title, summary, lifecycle state, linked refs and linked evidence. This is the "what's true right now" view.

**Timeline (audit trail):** a time-ordered, append-only sequence of all events on the thread. Each entry shows type, timestamp, actor, summary and refs. The timeline includes messages, receipt submission, reviews, decisions, exceptions, acknowledgments and lifecycle updates.

Mutable fields are interpretive and versioned through events. The timeline is durable and append-only. The UI MUST make this distinction clear.

**Lifecycle is not editable here.** Archive, trash and restore are core operations on the topic behind the thread; this surface MUST direct the operator to the CLI rather than offering controls it cannot honor. It MUST NOT reference a "topic route" — none exists.

### 2.3 Timeline rendering

- Ordering MUST be time-based and stable.
- Different event types SHOULD be visually distinguishable (icons, colors, or labels).
- Typed refs in event entries render per `refLinkModel`: `card:` opens the Task, `document:` opens the Doc, `thread:` / `event:` open thread inspection, `url:` opens externally. `topic:`, `board:` and `artifact:` have **no operator destination** and render as inert labels — that is deliberate, not a gap.
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

Events ("Audit") is the full workspace event browser under Diagnostics. It reads `GET /events`,
supports URL/shareable filter intent for type, group, backing scope, topic, actor,
search, time range, and cursor.
Events stays under More on mobile rather than a primary bottom-nav slot.

### 3.1 Inbox

A dedicated surface showing items that need operator attention.

**Display:**

- Inbox items are grouped into three **mailboxes** — **Needs you**, **Watching**, **Handled** (`lib/inboxMailbox.js`). Item `kind` (`ask`, `review`, `escalate`) drives affordances within an item, not the grouping; unknown kinds MUST still appear rather than being dropped.
- Within each group, sorted by inferred **urgency** (from kind, optional severity, and trigger/source recency) and then by **source or trigger time**; v0 does not add a separate ranking engine beyond that ordering.
- Each item shows: title, kind, requester context, and a link to the relevant task, document, thread, or artifact.
- Inbox item IDs are deterministic (see schema) and stable across rebuilds.

**Actions:**

- Navigate to the relevant inbox item, task, document, thread inspection route, or artifact.
- Respond to an item → emits a `human_attention_responded` event with `inbox:<inbox_item_id>` in refs. Responded items are suppressed from the inbox unless a new human attention request is created.
- The respond surface shows agent-authored **`response_proposals`** from the backing `human_attention_requested` event: the first entry is the **recommended** response (highlighted); additional entries are optional fill-ins for the freeform response text. **`review`** items also expose local **Approve** / **Reject** actions that submit fixed response text without using those chips.
- Record a response (creates a `human_attention_responded` event for inbox items, or a `message_posted` event for general notes) with notes and typed refs. The write is anchored on the inbox item's backing **thread** (`thread_id` / `thread:` in event refs). Topic refs are optional context when present, never the anchor.

### 3.2 Thread inspection list

`/threads` is a filterable inspection list of backing conversations (docs-as-rooms, inbox deep links, audit). It is not a fourth product primitive and MUST NOT appear in primary nav. It is listed under the **Diagnostics** group in the sidebar footer / `/more` hub, so it is discoverable by link rather than only by URL.

**Filters:** lifecycle `state` (`active`, `archived`, `trashed`), archive/trash visibility flags, and search (`q`).

Each row shows: title, lifecycle state (when not active), last activity timestamp, and whether a topic is linked. The thread ref is not printed; a "Copy ref" icon button carries it for CLI use.

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
- **Attention order.** The table lists open work by what needs a look first: Blocked, In progress, In review, Ready, Backlog, then phases Nexus has no name for; within a phase, `work.list` order (most recently updated first). Done and Cancelled fold into one quiet toggle under the table ("3 done"); `?closed=1` shows them (URL-backed) and filtering by Done or Cancelled shows them regardless. On the board, columns keep workflow order and cards keep their rank (rank is what a drag writes); the Done and Cancelled columns collapse to their count with the same toggle and stay drop targets.
- **One row per source item.** Tasks whose `source.authority` and `source.native_id` match (one GitHub issue read through two connections) show as one row: the one whose reader is not failing, then the most recently read, then the oldest. The folded tasks are listed on the kept task's page under "Also tracked through another connection", with their read state. Nexus-owned tasks never fold.
- **Columns earn their place.** Board appears only when more than one board is in view. "Last checked" means the last read from the source, so it appears only when a source-backed task is in view, and a Nexus-owned row leaves it empty rather than repeating "created here". The next-actor line under a title shows the actor's name, never the actor id.
- **Evidence on the task page is grouped by source.** One line per source ("GitHub #208 · 4 observations · last 1m ago") with the latest read's claim badge (Reported claim / Uncertain report / Verified evidence; "verified" is trusted only from `verification`), the latest read's uncertainty, then each distinct evidence link once, named by kind (Issue #208, Comment 1, Review · APPROVED, Check · build: completed / success); long lists show six and a "N more" toggle. A failed latest read is one warn line with the source's message (instants humanized). The read-by-read history, with consecutive identical reads folded ("Reported claim × 18 between 47m ago and 14m ago"), is an "Observation history" disclosure; raw reader payloads stay under Details.
- **Live.** The Tasks list and the task page follow the workspace event stream (card and board events); there is no Reload button on the list. The task page keeps Reload for evidence, which arrives as observations rather than events.

**Docs:**

- The UI MUST present docs as canonical long-lived lineages with a mutable head and explicit revision history, not generic stored text blobs.
- Doc create/edit workflows SHOULD use searchable thread-link pickers for common linkage flows, with manual raw-ID entry hidden behind an advanced path.
- Doc detail SHOULD make the current head revision versus prior lineage history legible at a glance.
- Docs list rows show the title, the head version chip (`v3`), tags and source, the last comment preview when it says more than the title, and the update time. There is no comment count or version count: the chip already says the version, and without per-reader unread state a comment total is the same noise on every row. The list follows the event stream (document events and comments on a listed doc) with no Reload button.

### 3.6a Command palette (⌘K) and keyboard

The palette (`CommandPalette.svelte`, model in `lib/commandPaletteModel.js`) is keyboard-first and takes actions, not only searches. Rows are grouped, in this order:

1. **Actions on the task or doc in view.** Task: Move to… (M), Assign to… (A, Nexus-owned tasks only; source-owned assignment belongs to the source), Open in <source> (O, when the source has a URL), Copy link, Copy ref, Ask PM about this task. Doc: Edit doc (E), Copy link, Copy ref. "Move to…" and "Assign to…" open a sub-list; with a query, their leaves ("Move to In review", "Assign to Leo Park") match directly. A source-owned task's moves read "Request move to … at GitHub" and file a PM decision exactly like a board drop. Done is not offered: completion needs an evidence ref, which the board's evidence form collects.
2. **Go to:** Inbox (G I), Tasks (G T), Docs (G D), Ask PM (⌘J), then every Settings and Diagnostics destination.
3. **Search results:** tasks (one row per source item) and docs, from two characters on.

Matching is fuzzy (subsequence, word starts and runs rank higher; spaces are ignored, so "assign leo" finds "Assign to Leo Park"). Arrow keys or Ctrl+N/P move, Enter runs, Esc backs out of a sub-list and then closes, Backspace on an empty sub-list query goes back. Actions use existing calls only (`cards.move` through `applyTaskPhaseMove`, `cards.patch` for `assignee_refs` fenced on the card's current `updated_at`, PM decisions) and report the outcome in a transient notice (with "Open in Inbox" for a filed request).

Every shortcut the palette shows is bound, by the palette itself, in a capture-phase window listener: G then I/T/D anywhere; M, A, O on a task page; E on a doc page. None fire while focus is in an input, textarea, select or contenteditable, or while a dialog is open. The Tasks `?` overlay lists G I/T/D and ⌘K alongside the page's own keys.

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
- The UI SHOULD subscribe for changes and refresh when canonical state changes. List pages use `liveWorkspaceEvents` (`lib/liveWorkspaceEvents.js`) over `GET /stream/events`: it starts after the newest matching event (no history replay), filters by type and an optional predicate, coalesces bursts into one re-read, resumes with `last_event_id` after a drop, backs off while core is unreachable, and stops on 401/403. Tasks, Docs and the task page use it; thread detail keeps its own thread-scoped stream. A live re-read keeps the rows on screen and, on failure, says they may be stale.
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
