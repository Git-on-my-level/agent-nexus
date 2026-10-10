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
- Machine identifiers are not labels. Actor ids, machine-minted principal handles (`passkey.<slug>.<hex>`, `external.<hash>`), connection ids, thread refs and raw error payloads are never printed as the name of something. The UI shows the name (or a person-chosen handle) and keeps the identifier behind a copy affordance where an operator may need it for the CLI or a bug report: "Copy actor id" on `/more`, "Copy connection id" and "Copy error" on Integrations (connections read "GitHub · main" when a tool has more than one), "Copy ref" on the Threads list, task and doc pages and in ⌘K. Access and agent pages show principal, host key and run ids the same way (`CopyableId`).
- **Who wrote something.** A host-derived agent is named by its host relation, "codex on workstation-a", wherever the UI names an author or requester (task and doc messages, Inbox requesters, timelines, ⌘K). The shell loads the roster (`GET /agents`) once and `buildActorNameMap` prefers the agent's `display_name` over the actor record's name (`lib/actorSession.js` → `agentRegistry`). An event written inside a launcher run carries `run_attribution`; message items show a quiet "via run exec-…" beside the time that opens that run on the agent's page (`components/agents/RunAttribution.svelte`).
- **Auth-first model**: Production deployments require authenticated principals by default.
  - Passkey registration/login creates a linked actor with `principal_kind=human`, `auth_method=passkey`.
  - Agents are not registered one by one. A machine is enrolled once as a **host** (`anx host enroll`, approved in Access, or a headless token); every agent on it is derived from the host as `<name>.<host>` (`principal_kind=agent`, `auth_method=host_assertion`) the first time it uses `anx`. Agent invites and public-key self-registration do not exist.
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

**Scope.** This is enforced on the surfaces an operator cannot avoid: primary navigation, the onboarding tour, and the page copy of Inbox, Agents, Tasks and Docs. Three places are exempt, because their whole job is to expose the core model:

1. **Diagnostics surfaces** (`/events` "Audit", `/threads`), including their nav entries and headings.
2. **Ref-type labels**, wherever they are rendered — `RefLink` / `refLinkModel` chips and Inbox subject lines (`inboxSubjectNoun`) — because their job is to name the _ref type_. These still use the operator noun where one exists: a `card:` subject reads "Task", a `topic:` subject reads "Project". Types with no operator equivalent (`thread:`, `artifact:`, `board:`) keep the core name. The exemption is the label's job, not the module it lives in: any new surface that names a ref type follows the same rule, and must not introduce its own noun map.
3. **Timeline and audit event rows**, which name the core event that occurred.

Those three exemptions are the remaining places "card" (and other core nouns) may appear in operator-visible copy. Inbox, Tasks, Docs, onboarding, keyboard help, and compact `RefLink` chips use Task / Project. Do not add new leaks, and do not read an exemption as license to teach core nouns on product surfaces. Task creation asks the operator to choose a Board only when more than one board already exists, because `work.create` defaults the backing board.

The Tasks table and board cards name the board only when more than one board is in view; with one board there is no choice to make, so there is no Board column and no board name on a card.

| Concept                        | Canonical term                                                 | Banned UI aliases                                                               | Allowed technical exceptions                                                                                                              |
| ------------------------------ | -------------------------------------------------------------- | ------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | -------- | --------- |
| Soft-delete lifecycle          | Trash, trashed, move to trash, restore                         | tombstone, tombstoned                                                           | HTTP paths and machine identifiers follow `contracts/` (`/trash`, `trashed_at`, `trash_reason`; list endpoints use repeated `state=active | archived | trashed`) |
| Root work item                 | Task, Tasks                                                    | Topic, Topics, Card, Cards, backing thread, Threads (as operator-facing labels) | `card:` refs, `card_id`, the `work.list` / `work.get` command ids, `thread_id`, `thread:` refs, `/threads` diagnostic detail route        |
| Project / work grouping        | Project                                                        | Topic, Topics (as operator-facing labels)                                       | `topic:` refs; core reports `"projects": "topics"` in `work.capabilities`                                                                 |
| Backing board                  | Board — only where the operator has a real choice among boards | Board as a required step, label or column when the workspace has one board      | `board:` refs, `board_id`, the `boards.*` command family, diagnostics surfaces                                                            |
| Document collection            | Docs                                                           | Documents (as collection label)                                                 | `document` for singular resources and API field names                                                                                     |
| Inbox triage action            | Acknowledge                                                    | Dismiss                                                                         | —                                                                                                                                         |
| Operator-facing actor in prose | Operator                                                       | user, end user                                                                  | `actor`, `principal` in identity and auth contexts                                                                                        |
| Enrolled machine               | Host (Access), machine (prose)                                 | device, node, runner (as the concept)                                           | `host:` refs, `host_id`, the `hosts.*` command family                                                                                     |
| Human with access              | People (Access section), person                                | principal, user (as operator labels)                                            | `principal` in audit copy ids and API paths                                                                                               |
| Irreversible removal           | Permanently delete                                             | Purge (primary copy)                                                            | CLI/command `purge` where it is the API surface name                                                                                      |

`Artifact` remains the umbrella object; `Receipt` and `Review` are artifact kinds only.

**Domain note:** Operator vocabulary and core vocabulary are deliberately different. The boundary between them is the typed ref.

- **Operator-facing nouns are Overview, Inbox, Agents, Tasks and Docs, plus Hosts and People inside Access.** **Overview** is the workspace home: it summarizes what needs the operator, work in flight, agent presence, and visual reports, and it links into those surfaces. It is not an attention surface and it introduces no actions. A **Task** is the operator's unit of work (`work.list` / `work.get` projected over cards). An **Agent** is a derived principal on a host, shown by what it is doing; the Agents view is presence, not a second attention queue.
- **Core primitives — topic, board, card, thread, artifact — are not operator nouns.** They are the durable model that agents address by typed ref (`topic:`, `card:`, `board:`, `document:` — the contract's prefixes, per `contracts/anx-schema.yaml` → `ref_format`; `doc:` is a CLI target shorthand only and is rejected inside a ref) through the CLI and generated clients. The UI renders them, but never asks an operator to think in them.
- A **thread** is infrastructure: a durable append-only event timeline that backs topics, cards, boards and documents, and resolves packet subjects. It is never an operator-facing noun.
- A **topic** is the core discussion/context primitive built on a thread. It has **no operator destination**; it appears only as a ref-type label (e.g. in `RefLink` or an Events filter) and as the detail rendering of its backing thread.

The practical rule: if an operator has to learn a word to use the product, it belongs in the first bullet. Everything else is addressed by ref, and the UI resolves the ref to something the operator already understands.

---

## 2. Core UX model: Overview, Inbox, Agents, Tasks, Docs

### 2.1 Primary navigation

The primary navigation units are **Overview**, **Inbox**, **Agents**,
**Tasks**, and **Docs**, in that order, in the sidebar and in the mobile
bottom bar. Overview is first because it is the workspace home. Inbox
shows its Needs you count; Agents shows how many agents are working (nothing at
zero). Agents is presence: who is working, waiting on a human, idle or stale.
It never answers an ask; a waiting agent's row links into the Inbox. Ask PM is
an action in the shell, not a nav category; see 3.9 for what that slot shows
when no PM agent is onboarded. The account menu in the sidebar
footer and the `/more` hub group the secondary destinations under two labels:
**Settings** (Archive, Access, Secrets, Integrations) and **Diagnostics**
(Audit, Threads). Sign out is the last item of the account menu.

Access carries the one count that lives behind the account menu: how many
access requests wait for a decision, covering both agents asking for a grant
and machines asking to enroll. It counts what the reader can still decide:
undecided access requests plus pending, unexpired host enrollments. An
approved enrollment is waiting on its machine, so it is not in the number and
the section heading reads **Enrollment in progress** instead. Core's
`GET /auth/access/summary` is the cheaper read but its `pending_count`
includes those approved ceremonies, so the shell counts from the two lists and
the badge and the page cannot disagree. The badge runs only for a person:
deciding a grant is human-only, so an agent principal would only ever be
refused. Because the Access item is out of sight
until the menu opens, the same number also sits on the menu trigger (the
avatar and name button), and on the Access row in `/more`. One number, the
detail on hover, and nothing at zero — or for a reader without administration
authority, who could not act on it.

Overview (`/overview`) summarizes four existing reads and links into them. It
does not answer an ask, move a task, or edit a document. Its own sections are
**Initiatives**, **Recent changes** and the pinned **Dashboard**; everything
else answers a different question and sits one fold down under **More detail**,
with links to the surface that owns it. **Needs you** is the
Inbox count and its first rows, and lives in the Inbox and its badge. **Work at a glance** counts `GET /work` by
phase and source, plus blocked tasks, tasks whose next actor is a person, and
freshness (stale, unknown, error); each count links to Tasks with the matching
query (`source`, `phase`, `freshness`, and `human=1` for a person as next
actor). **Agents** counts working, waiting, and stale and links to the roster.
**Reports** lists documents whose current content is a valid visual report
(`parseVisualReport`), newest first, and renders the selected one. A document
whose title starts with `Dashboard` or `Fleet Dashboard` is preferred. A failed
read is unavailable; it is never shown as zero or healthy.

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

The workspace root redirects to Overview. There is no Home unread-feed destination. Inbox remains the only attention surface. Agents (§3.8) is a presence view, not a home.

Events ("Audit") is the full workspace event browser under Diagnostics. It reads `GET /events`,
supports URL/shareable filter intent for type, group, backing scope, topic, actor,
search, time range, and cursor.
Events stays under More on mobile rather than a primary bottom-nav slot.

### 3.1 Inbox

A dedicated surface showing items that need operator attention. It is the only attention surface; other views (Tasks, the Agents roster) link into it rather than handling items themselves.

**Mailboxes and order:**

- Inbox items are grouped into three **mailboxes** — **Needs you**, **Watching**, **Handled** (`lib/inboxMailbox.js`). Item `kind` (`ask`, `review`, `escalate`) drives affordances within an item, not the grouping; unknown kinds MUST still appear rather than being dropped.
- **Needs you** is ordered by how long the requester has been blocked (the age of the open request, compared at minute resolution), then by severity (critical, high, …), then by kind. Blocked tasks use their last update as the start of the wait, because core does not record when a task entered Blocked. **Watching** and **Handled** are newest first.
- A `?work_ref=<card ref>` link ("Inbox for this task") narrows every mailbox to rows about that task and shows a removable "Only <task>" chip. Counts on the mailbox tabs follow the filter; the sidebar count does not.

**Rows:**

- An ask, review or escalation row reads: title and loud severity badge; then the requester by name and the subject ("Omar Reed · Task: Lock hub quest path"); on the right, in Needs you, how long they have waited ("3h 12m", amber from one hour). The subject prefers the task an item names over the project it was filed on (`inboxItemSubject`).
- Names, not identifiers: requesters and responders resolve through the actor registry. When only an id exists the UI shows a short stand-in (`agent 6400c2d2`) with the full id behind a copy button; a raw UUID is never the label.
- Watching **update** rows (`home.unread` groups) are digests in operator language built from the group's events — "Leo moved 2 tasks to review · Nina commented", "You updated 2 tasks" — never core nouns ("Board updates") or a bare count badge (`lib/inboxDigest.js`).
- Handled rows drop core's "Human response recorded:" title prefix; the mailbox already says the item was answered.
- Inbox item IDs are deterministic (see schema) and stable across rebuilds.

**Detail pane and standalone item page:**

- The first row of the current mailbox is selected automatically; the pane is never an empty placeholder while rows exist. The automatic selection stays on its row while the list refreshes, so a live update never swaps the item being answered. Below `lg` the list and the detail are separate screens and only an explicit choice (`?item=`) opens the detail.
- The pane header says who is blocked and for how long ("Omar Reed has been blocked for 3h 12m").
- A **context strip** shows what the item blocks — the subject task (title, phase) or doc (title, version) — and the latest progress note around it (author, age, excerpt), preferring the requester's own latest message. It reads `docs.get` and `events.list` (message events on the task's thread and the item's threads). When the requester is an agent with a presence note (`anx work note`), the strip also shows that note with its age and the agent's state dot, linked to the agent page (`components/agents/AgentPresenceLine.svelte`, read from the shell roster). Threads and boards are not operator subjects and get no subject line.
- The respond surface shows agent-authored **`response_proposals`** from the backing `human_attention_requested` event, numbered 1–5. The first is marked **Recommended**. Choosing a proposal selects it and sends it; **`review`** items also offer **Approve** / **Reject**, which send fixed text. A freeform reply and **Acknowledge** complete the surface. The Inbox pane and the standalone item route (`/inbox/{id}`) share this component (`InboxRespondPanel`) and behave the same; the standalone page adds the notify-target and attachment controls.
- Both surfaces notify the original requester by default when core can reach them (`notification_target_status.resolvable`), and nobody otherwise. Acknowledge never notifies.
- Links from the standalone page back into the Inbox use the Inbox's own parameters (`?mailbox=handled&item=…`); `?status=` is not an Inbox parameter.

**Undo:**

- Sending a proposal, a reply, Approve/Reject or Acknowledge does not call core immediately. The response waits about five seconds behind an undo toast ("Sent to Omar Reed · Undo"); Undo or ⌘Z takes it back and restores the selection and draft. When the window closes the UI commits exactly the `inbox.respond` request built at send time. Queuing a second response commits the first at once; closing the tab commits a waiting response (with the browser's leave prompt to give it time). A refused commit returns the item to Needs you with a "Not sent" toast and Retry (`lib/inboxResponseQueue.js`).
- While a response waits or has just committed, the item is filed under Handled locally so the list and the sidebar count move at once; core's projection takes over within a minute.
- The standalone page returns to the Inbox after sending; undoing a response sent there goes back to that page with the draft, notify target and attachments restored.

**Keyboard:**

- `J` / `K` next and previous row, `1`–`5` send that suggested response, `R` focus the reply, `⌘Enter` send the reply, `E` acknowledge (or mark an update read, or acknowledge a failed delivery), `O` open the subject task or doc, `⌘Z` undo the waiting response, `?` the shortcut list, `Esc` close it. Single keys never fire while typing in a field, with a modifier held, or under another dialog. The pane footer shows the keys that apply to the selected row (`lib/inboxShortcuts.js`).

**Live updates:**

- The Inbox follows the workspace event stream through `liveWorkspaceEvents` (§6) and reloads quietly, coalescing bursts. There is no Reload button. A quiet reload never blanks the list and clears a load error once the load succeeds again.
- The sidebar Inbox item shows the Needs you count (nothing at zero). While the Inbox page is open it publishes its own count; elsewhere the shell loads the Needs you sources itself and refreshes on the same shared stream (`lib/inboxCount.js`).

**Writes:**

- Respond to an item → emits a `human_attention_responded` event with `inbox:<inbox_item_id>` in refs. Responded items are suppressed from the inbox unless a new human attention request is created.
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
2. **Go to:** Overview (G O), Inbox (G I), Agents (G A), Tasks (G T), Docs (G D), Ask PM (⌘J), then every Settings and Diagnostics destination. With no PM agent onboarded, "Ask PM about this task" and "Ask PM" are absent and "Set up your PM" takes their place (3.9).
3. **Search results:** tasks (one row per source item) and docs, from two characters on.

Matching is fuzzy (subsequence, word starts and runs rank higher; spaces are ignored, so "assign leo" finds "Assign to Leo Park"). Arrow keys or Ctrl+N/P move, Enter runs, Esc backs out of a sub-list and then closes, Backspace on an empty sub-list query goes back. Actions use existing calls only (`cards.move` through `applyTaskPhaseMove`, `cards.patch` for `assignee_refs` fenced on the card's current `updated_at`, PM decisions) and report the outcome in a transient notice (with "Open in Inbox" for a filed request).

Every shortcut the palette shows is bound, by the palette itself, in a capture-phase window listener: G then O/I/A/T/D anywhere; M, A, O on a task page; E on a doc page. None fire while focus is in an input, textarea, select or contenteditable, or while a dialog is open. The Tasks `?` overlay lists G O/I/A/T/D and ⌘K alongside the page's own keys.

### 3.7 Access management

Access (`/access`, under Settings) is where machines and people get and lose access. Sections, top to bottom:

**Waiting for you** (only while anything is waiting; **Enrollment in progress**
when the only rows left are approved ceremonies waiting on their machines).
Two kinds share the section, agents first, because a person decides those
while an enrolling machine is still polling:

- **Access requests** (`GET /auth/access-requests`, human only): the agent's
  name and handle, the reason it gave, and how long it has waited.
  **Approve…** opens an inline confirmation naming what administration allows
  and what granting an agent on a host trusts, then calls
  `POST /auth/access-requests/{id}/approve`, which grants `auth-admin` and
  resolves the Inbox item in one transaction. **Deny** is one click and
  changes nothing. After either, the Administrators list is re-read.
- Lists pending host enrollments (`GET /auth/hosts/enrollments/pending`): requested host name, `os_user@hostname`, requesting IP, agents found on the machine, adopted agent names, age and expiry, and the user code the machine printed, set large.
- Approval is deliberate: **Approve…** opens an inline confirmation that repeats the code ("Approve only if J6FA-N4XI is the code printed on workstation-a.local") and says every agent running there can act in the workspace; only **Codes match, approve** calls `POST …/approve`. **Deny** is one click. An expired request cannot be approved. After a decision the page says what happens next (an approved host appears under Hosts once the machine finishes).
- The list polls every few seconds while the page is visible, so a request shows up while the operator is looking. Host cards re-read on the roster stream (`liveAgentChanges`). Core may return a full workspace web UI verification URL ending in `/access/hosts/enroll`; that route redirects here (`#host-requests`). Without a configured URL, the CLI tells the operator to open Access → Hosts.

**Hosts:**

- One card per active host (`GET /hosts`): name, `os_user@hostname`, enrolled age, host key id behind a copy button, bridge online/offline, and its agents. Agent rows show the state dot and derived state from the host read itself, name, handle and whether it is an adapter, persona or adopted agent, and link to the agent page. Adapters found on the machine but not yet used are listed as "Also installed".
- **Exclusions** are edited in place (`PATCH /hosts/{id}` `excluded_names`): add a name (validated against the agent-name pattern) or remove one. An excluded name cannot act from that machine; its open sessions end and its history stays.
- **Revoke host…** opens an inline confirmation inside the card that names every agent that loses access and requires typing the host name before `DELETE /hosts/{id}`. Revoked hosts collapse behind "Show N revoked hosts".
- **Enroll a machine** shows the command (`anx --base-url <core> host enroll`, with a copy button) and the headless option: create a one-time token with a label and lifetime (10 minutes to 24 hours); the token and the full `host enroll --token` command are shown once with copy buttons. Unused tokens can be revoked; used and expired ones collapse. With no hosts, this panel is open by default.

**Administrators:**

- Who can administer this workspace, people and agents in one list, because a
  list of only the explicit grants would read as if nobody else could act. A
  person holds administration implicitly from the moment they join; an agent
  holds it only from an explicit grant (`GET /auth/admins`). Rows carry the
  display name, the kind (Person or Agent), an agent's handle and host, and
  the date administration started. The principal id sits behind a copy button.
- An agent's grant date comes from the `auth_admin_granted` audit events the
  page already loaded, because `AuthAdmin` carries no grant timestamp. Outside
  that window the date is omitted rather than guessed at.
- **Grant administration** takes an agent username or principal id and
  confirms what the grant allows, naming the principal and the host whose
  shared key can request it. **Revoke administration** confirms the same way.
  Both are offered only to a person (`POST /auth/admins/{id}/grant|revoke` is
  human-only); an agent administrator sees the list and no controls. A person
  is never offered a grant — remove their authority by revoking their access
  under People.
- `listPrincipals` returns one page, so when it does not reach every person
  the section says how many more administer the workspace without being shown.

**People:**

- Humans only, by name (actor display name, then username), with joined and last-seen age and the principal id behind a copy button. Agents are never listed here; they appear under their hosts.
- **Invite a person** (self-hosted) creates a human invite and shows the one-time token with a copy button; open invites list with Revoke. Hosted workspaces invite people from Organizations instead, and the section says so.
- Revoking a person asks for confirmation. The last active human requires break-glass: typing the principal id and a lockout reason (`allow_human_lockout`, `human_lockout_reason`). The signed-in principal cannot revoke itself.

**Standalone agents** (only when present): agents registered before hosts and not adopted. They keep working until revoked here.

**Refused reads:** every read on this page needs administration authority and
they are refused together, so a principal without it gets one line ("Only
workspace administrators can manage access.") instead of seven sections each
describing an empty workspace it was not allowed to see. A 403 is that answer;
a 401 is an expired session and stays on the error path.

**Recent access events:** auth audit events as sentences with names and host names ("Maya Chen approved workstation-a", "codex on workstation-a used anx for the first time"); event ids sit behind a copy button on hover. Eight show first, then more, then older pages.

Agent identity is managed through host enrollment and host-level exclusions; there are no per-agent enrollment or wake controls. There is no Refresh button: actions re-read what they change.

### 3.8 Agents

`/agents` is the roster of every agent in the workspace (`GET /agents`), grouped by the state core derives: **Waiting on you**, **Working**, **Idle**, **Stale**, in that order, each with its state dot and count. The subtitle counts the roster ("6 agents · 1 working · 2 waiting on you · 1 idle · 2 stale").

Each row answers who, where, doing what, and for how long:

- **Who:** display name ("codex on workstation-a"), `@handle`, runtime (adapter and model of the active run, or the adapter when the agent is one), and the bridge indicator (a secondary icon; online means tagging wakes it now). Bridge state is never the row's state.
- **Waiting on you:** the oldest open ask (title linked to the Inbox item, kind, severity, the task it is about, "N more open"), how long it has waited, and **Answer in Inbox**. Waiting rows sort by longest wait. The roster never answers an ask.
- **Working:** the current task (linked), the last progress note quoted with its age, and run time (or time since the last update without a run). Longest-running first.
- **Idle:** current task or "No current task", last signal age.
- **Stale:** "No signal for 2d" or "Never checked in", dimmed.

Waiting rows read everything from the roster: `waiting_ask` carries the ask's title, kind, severity, subject, Inbox item id (the link) and `created_at` (the wait start). While core's inbox projection has not caught up (`inbox_item_id` null) the link opens Needs you.

**Keyboard:** `J`/`K` (or arrows) move the selection, `Enter` or `O` opens the agent, `I` opens the selected agent's ask in the Inbox, `T` its current task, `?` the shortcut list (`KeyboardShortcutsDialog`), `Esc` closes it. Keys never fire while typing, with a modifier held, or under a dialog. A footer shows the keys (`lib/agentShortcuts.js`).

**Live updates:** the shell keeps one roster loaded (`lib/agentRoster.js`) for the nav badge, names and this page. It re-reads `GET /agents` on every `agents_changed` notification from core's roster stream (`GET /stream/agents` via `liveAgentChanges`, §6), which arrives on connect, after run, presence, host and bridge changes, and after a reconnect; on ask events from the workspace stream (`human_attention_requested`/`responded`); when the tab becomes visible; and on a two-minute fallback timer. Durations tick every 15 seconds; run time counts forward from core's `duration_seconds` between reads. The agent page re-reads its history when the roster moves (at most every 3 seconds) and takes `open_asks` (with `inbox_item_id` and `created_at`) straight from `GET /agents/{id}`.

**Agent page** (`/agents/{handle}`):

- Header: state dot, display name, state label, `@handle` (copy), host (links to its Access card), adapter or persona and runtime, bridge state.
- **Waiting on you** (when there are open asks), each linked to its Inbox item with wait time. **Now:** current task, last note with age, active run, last signal.
- **Recent runs:** launcher id (`exec-…`, copyable), state, adapter and model, duration, whether the result was collected ("Not collected" when a finished run's result was never read), and the task. `?run=<id>` highlights a run (the "via run" link lands here).
- **Notes and messages:** progress notes and the agent's messages (`events.list` by actor), newest first, with task and "via run" attribution; six first, then the rest.
- **Recent tasks** with phase and age.
- **Identity** (secondary, aside): host and machine, name and kind, derived/adopted/standalone, created, agent and actor ids behind copy buttons. Auth admins can exclude the agent's name on its host (reversible; inline confirmation) or revoke the agent (permanent: for a derived agent the host can never use that name again, and the confirmation says so). Revoking every agent on a machine is done on the host in Access.

---

### 3.8a Ask PM

The heading is the conversation when the conversation has a name of its own,
with "Ask PM" as the eyebrow above it. Core names a conversation
`text.slice(0, 100)` of its first question, so a title the first question
already starts with is an echo, not a title: the page stays named "Ask PM"
and the first bubble is the question. A title that is the heading is clamped
to two lines, with the whole of it in the tooltip.

PM presence is a dot and one word on the heading's baseline — Connected or
Offline — with the runner, the host and the last-seen age in its tooltip, and
Manage as a small link beside it. The sentence about a proposal going to Inbox
for approval appears only on a new, empty conversation.

A turn's run reads as one line. While it is working that line is
`Working · <elapsed>` (never the last step's own label, which the steps below
it already carry) and the steps are collapsed behind it; once it is answered
the line is `Finished · N steps`, below the answer and quieter than it. The
wait note appears only past ninety seconds, when the wait is genuinely
unusual. A turn's time is the group header's; a bubble does not repeat it.

Evidence under an answer is a bounded "Sources" row: three chips and the rest
behind a `+N`. The thread re-pins to the bottom when it grows under a reader
who is already there, so a row that wraps late cannot end up behind the
composer.

### 3.9 PM onboarding and gating

A PM agent runs on the reader's own computer, never on the server, so a
workspace can have none. `GET /pm/presence` answers
`{ state, last_seen, runner, host }` — `not_onboarded` until a PM has
connected at least once, then `connected` or `offline` by heartbeat freshness
— read in `lib/pm/onboardingState.js` and held for the shell by
`lib/pm/presence.js`, which shows a PM surface only on a state core has
positively reported. The response's older `configured` / `connected`
booleans are used only when `state` is absent, so this UI still works against
a core that predates it; there `configured` alone decides, and a missing
`last_seen_at` is deliberately not read as "never connected", because that
core's presence projection is newer than the PM itself and a workspace that
has used its PM for months reports no last-seen on its first run.

**The gate is on affordances, never on reads.** A PM runs on the reader's own
computer, so one can be absent while proposals it filed earlier still wait for
a yes — and core still lists them and still accepts an answer. The Inbox and
Tasks therefore always read the PM decision and receipt feeds, whatever the
state says; hiding an obligation the Inbox is the only place to answer would
be worse than two already-bounded reads. When core answers the reserved
`pm_not_onboarded` code, the UI reads that as an empty feed rather than an
error, so the Inbox does not look broken for a feature the workspace does not
have. Core does not emit that code yet: today a missing PM identity and a PM
bridge that is merely down both answer 503 `unavailable`, and those cannot be
told apart from the response — so a bare 503 keeps its error rather than being
read as "install a PM", which would throw away the reader's draft.

- **`not_onboarded`:** every PM surface is **absent, not disabled** — the
  sidebar and bottom-bar Ask PM action, the PM conversation, "Ask PM about
  this" on Inbox rows and tasks, the palette's PM rows, and PM hints in copy
  elsewhere. ⌘J is swallowed rather than left to the browser. `/pm`
  redirects to `/pm/setup`; a conversation already in core stays there and is
  readable again once a PM is set up. What stays named is a PM proposal the
  reader has in front of them: a row the PM filed earlier is still a PM
  proposal, and describing it without the word would be a worse sentence than
  an accurate one. Moving work a source owns is also gone: that move
  is a PM proposal the PM carries out at the source, so with no PM
  `applyTaskPhaseMove` refuses before writing and the surfaces say where to
  set one up, rather than filing a proposal nobody would carry out. A move on
  Nexus-owned work is unaffected.
- **Setup:** exactly one calm entry point, in the slot Ask PM occupies, leading
  to `/pm/setup`. That page says in two sentences that the PM runs on the
  reader's computer through their own agent, gives the copyable
  `anx pm install` command for this workspace, and shows a live "Waiting for
  your PM to connect…" state that flips to "Connected" in place on the first
  heartbeat. PM surfaces appear without a reload. The live watch is bounded:
  after a few minutes it stops and names the `anx pm status` check instead of
  waiting silently forever, and it spends no requests while the tab is hidden.
- **`offline`:** PM features stay visible with a quiet "PM offline, last seen
  X" status and the `anx pm status` hint. Sending stays open; the answer waits
  for the machine to come back.
- **Manage:** `/pm/setup` doubles as the status page once a PM exists (state,
  last seen, and the runner and host the PM registered with, plus the
  `anx pm status` and `anx pm uninstall` commands), reached from the "Manage"
  link beside the PM status.
- **An unknown state shows nothing.** Before the first read returns, after a
  read fails, and against a core with no `/pm/presence` at all, no PM
  affordance is rendered anywhere the reader did not ask for one — the shell
  slot, the palette, the Inbox and Tasks entry points, the ⌘J shortcut: the
  slot holds its space and commits to a label only once core answers.
  `/pm/setup` stays reachable by typing the URL, and says it is still checking
  rather than claiming either answer. Showing a guess made the row flash in and out
  on every load, and a read that never succeeded left a button that could not
  work. Answering an existing proposal is never gated this way (see above), so
  nothing already asked of the reader becomes unreachable. A core old enough
  to lack the route cannot serve this UI anyway: `pm.presence` is in the
  command registry the shell checks against core's handshake at startup.
- **Refusing a PM write needs a confirmed absence**, not an unproven one.
  `pmFeaturesVisible` and `pmKnownAbsent` are mirror images rather than
  negations of each other — both false while the state is unknown — so a slow
  or failed read defers the source-owned move to core instead of telling a
  workspace that has a PM that it has none.
- **Server-side gating** is core's, not the UI's: the PM routes answer
  `pm_not_onboarded` where there is no PM. The UI reads that refusal as "no
  PM" wherever it can arrive. A bare 503 `unavailable` is not read that way:
  core answers it both for a missing PM identity and for a PM bridge that is
  merely down, and swallowing the second would throw away the reader's draft.

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
- The UI SHOULD subscribe for changes and refresh when canonical state changes. List pages use `liveWorkspaceEvents` (`lib/liveWorkspaceEvents.js`) over `GET /stream/events`: it starts after the newest event core has (no history replay), filters by type and an optional predicate, coalesces bursts into one re-read, resumes with `last_event_id` after a drop, backs off while core is unreachable, and stops on 401/403. Subscriptions for the whole workspace share one connection per client (types and predicates filter per subscriber), so the Inbox, the sidebar count, the agent roster and a list page never open parallel streams; a thread-scoped subscription gets its own. Tasks, Docs, the task page, the Inbox (§3.1), the Inbox count and the agent roster (§3.8) use it; thread detail keeps its own thread-scoped stream. The same module's `liveAgentChanges` follows `GET /stream/agents`, core's ephemeral roster invalidation (not in the event log, no cursor): one shared connection per client, a callback on connect, on every `agents_changed` and after reconnects. The roster, the agent page and Access host cards use it. A live re-read keeps the rows on screen and, on failure, says they may be stale.
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
- Show every agent's derived state, current work, asks and runs on Agents, and enroll, exclude and revoke hosts in Access.
- Show Tasks as table and board over `work.list`, including Nexus-owned drag via `cards.move`.
- Show Docs list/detail with head vs revision lineage.
- Inspect backing threads at `/threads` without making that a primary nav primitive.
- View receipts and their evidence links as navigable typed refs.
- Perform a lightweight review (outcome + notes + typed evidence refs) from Inbox / artifact detail — not a board card-detail modal.
- Post messages on a thread or topic-backed timeline.
- Render provenance with visual distinction between evidence-backed and inferred sources.
- Parse typed reference strings across all surfaces (`board:` refs may stay inert in the operator UI).

### Executive Overview

Overview reads the core `/overview` projection shared with agents: Initiatives,
Recent changes and Dashboard, then a collapsed More detail fold (the morning
brief, what is waiting on you, work counts, freshness and agent presence). The
cross-workspace ask read behind that fold runs when the fold is opened, not on
every visit. Initiatives use the same live-initiatives renderer and projection
as document reports (`progress.done/total`, `needs[]`). Rows show the first summary line, Markdown checklist
progress, priority and all human asks outside fenced examples. One control chooses the
report, pins it as the persistent workspace dashboard and resets to
newest-report selection; beside it, one link opens the document. Inline
reports show the panels, and the report title only where the surrounding page
does not already print it; project/freshness filters, panel counts and
provenance inspection remain in Open document. Archive in the
secondary navigation contains archived boards, cards, topics and documents.
Watching aggregates routine agent edits by board and distinct card, prioritizes
asks/answers and done/blocked transitions, keeps every material clause visible in the digest, and excludes archived subjects including cards on archived project topics. Report choices load when the report control opens, and the list grows in place rather than replacing itself under an open popup.
