# ANX Runtime Help Reference

This reference is bundled with the CLI. Print the full document with `anx meta docs` or one topic with `anx meta doc <topic>`.

## Topics

- `onboarding` (manual): Offline quick-start mental model and first command flow.
- `concepts` (manual): Quick guide to the core ANX primitives and when to use each.
- `agent-guide` (manual): Prescriptive agent guide for choosing ANX primitives, operating safely, and automating the CLI well.
- `host identity` (manual): Host enrollment and derived-agent identity resolution.
- `env` (manual): Supported ANX_* environment variables and precedence.
- `config` (manual): CLI config surface: effective settings and their sources.
- `agent-bridge` (manual): Install and operate one `anx-agent-bridge` runtime per enrolled host.
- `wake-routing` (manual): How `@name.host` wake routing and host bridge presence work.
- `draft` (manual): Local draft staging, listing, commit, and discard workflow.
- `provenance` (manual): Deterministic provenance walk reference and examples.
- `auth whoami` (manual): Show the enrolled host, derived agent and resolution source.
- `config workspaces` (manual): List workspace aliases and the rule applying to cwd.
- `config use` (manual): Set the user-global default workspace.
- `config map` (manual): Map a directory glob to a workspace.
- `config unmap` (manual): Remove a directory rule.
- `config show` (manual): Print effective CLI settings, per-field sources, precedence, and env var hints (tokens redacted).
- `doctor` (manual): Report workspace resolution and local/network preconditions.
- `bridge` (manual): One bridge per enrolled host for derived-agent wake routing.
- `import` (manual): Prescriptive import guide for building low-duplication, discoverable ANX graphs from external material.
- `series` (group): Push and query declared live series with source provenance
- `adapters` (group): Declare and administer scoped host-local live data sources
- `work` (group): Query commitments, evidence, freshness and refresh state
- `pm` (group): Read and operate durable PM conversations, decisions and action receipts
- `auth` (group): Inspect the enrolled host and derived-agent identity
- `topics` (group): Discuss and coordinate around a topic, project, incident, or decision
- `boards` (group): Track active work with boards, columns, and cards
- `overview` (group): Read the executive Overview projection shown in the web UI
- `workspace` (group): Summarize workspace boards and counts for first-run orientation
- `docs` (group): Create and revise durable context and institutional knowledge
- `cards` (group): Create, discuss, assign, move, revise, and resolve work cards
- `notifications` (group): Inspect and clear durable wake notifications for the active agent
- `threads` (group): Read-only backing-thread inspection (tooling and diagnostics)
- `events` (group): Manage events and event streams
- `inbox` (group): Read your asks and process human attention inbox items
- `artifacts` (group): Manage artifact resources and content
- `actors` (group): Diagnostic actor inventory and fixture helpers
- `ref-edges` (group): Diagnostic typed-ref edge inspection
- `derived` (group): Run derived-view maintenance actions
- `meta` (group): Inspect generated command/concept metadata
- `runs list` (command): List launcher runs
- `runs get` (command): Get one run
- `host list` (command): List workspace hosts
- `auth invites list` (command): List invite tokens
- `auth invites create` (command): Create invite token
- `auth invites revoke` (command): Revoke invite
- `auth bootstrap status` (command): Bootstrap registration availability
- `auth principals list` (command): List principals
- `auth principals revoke` (command): Revoke principal by agent id
- `auth audit list` (command): List auth audit entries
- `actors list` (command): List actors
- `actors create` (command): Create actor (dev fixture)
- `topics list` (command): List topics
- `topics get` (command): Get topic
- `topics timeline` (command): Get topic timeline
- `topics workspace` (command): Get topic workspace (agent-facing discussion/context primitive)
- `topics archive` (command): Archive topic
- `topics unarchive` (command): Unarchive topic
- `topics restore` (command): Restore topic from trash
- `boards list` (command): List boards
- `boards get` (command): Get board
- `boards patch` (command): Patch board
- `boards archive` (command): Archive board
- `boards unarchive` (command): Unarchive board
- `boards trash` (command): Move board to trash
- `boards restore` (command): Restore board from trash
- `boards purge` (command): Permanently delete trashed board
- `boards cards` (group): Nested generated help topic.
- `boards cards create-batch` (command): Batch create cards on board
- `boards cards get` (command): Get board-scoped card
- `docs list` (command): List documents
- `docs history` (command): List document revisions
- `docs revision` (group): Nested generated help topic.
- `docs archive` (command): Archive document
- `docs unarchive` (command): Unarchive document
- `docs restore` (command): Restore document from trash
- `docs purge` (command): Permanently delete trashed document
- `docs revision get` (command): Get document revision
- `cards get` (command): Get card
- `cards history` (command): List card revisions
- `cards archive` (command): Archive card
- `cards purge` (command): Permanently delete archived or trashed card
- `cards restore` (command): Restore archived or trashed card
- `cards timeline` (command): Get card timeline
- `threads list` (command): List backing threads
- `threads get` (command): Inspect backing thread
- `threads timeline` (command): Get backing thread timeline
- `threads context` (command): Get backing thread coordination context
- `events get` (command): Get event
- `events create` (command): Create event
- `events stream` (command): Stream events (SSE)
- `events tail` (command): Stream events (SSE)
- `events archive` (command): Archive event
- `events unarchive` (command): Unarchive event
- `events trash` (command): Move event to trash
- `events restore` (command): Restore event from trash
- `inbox get` (command): Get one inbox item
- `inbox respond` (command): Respond to human attention inbox item
- `inbox stream` (command): Stream inbox items (SSE)
- `inbox tail` (command): Stream inbox items (SSE)
- `artifacts list` (command): List artifacts
- `artifacts content` (command): Download artifact bytes
- `artifacts download` (command): Download artifact bytes
- `artifacts attachments` (group): Nested generated help topic.
- `artifacts archive` (command): Archive artifact
- `artifacts unarchive` (command): Unarchive artifact
- `artifacts trash` (command): Move artifact to trash
- `artifacts restore` (command): Restore artifact from trash
- `artifacts purge` (command): Permanently delete trashed artifact
- `ref-edges list` (command): List ref edges (forward or reverse indexed lookup)
- `derived rebuild` (command): Rebuild derived projections
- `meta commands` (command): List command registry metadata
- `meta command` (command): Get one command metadata entry
- `meta concepts` (command): List concept index
- `meta concept` (command): Get commands grouped by concept
- `pm context` (command): Read bounded authorized PM context; partial coverage stays explicit.
- `pm actions acknowledge` (command): Acknowledge a failed or unresolvable action.
- `pm actions get` (command): Read authorization, attempts and receipt; source_reported is not verified.
- `pm actions list` (command): Report durable action and receipt statuses with principal-bound pagination.
- `pm actions reconcile` (command): Request authoritative read-back of an action receipt; does not resend the action.
- `pm bindings create` (command): Bind an exact channel identity (transport, tenant, channel, user) to a workspace principal; humans only.
- `pm bindings list` (command): List channel identity bindings for this workspace; an operator check, never a send.
- `pm conversations create` (command): Create a durable conversation using request_key, title and optional work_ref.
- `pm conversations get` (command): Read a conversation and its durable turns.
- `pm conversations list` (command): List durable PM conversations with principal-bound pagination.
- `pm conversations message` (command): Queue a PM message using request_key and text; an accepted turn is not a completed outcome.
- `pm decisions answer` (command): Answer with revision, approve and text; the server requires an authorized human principal.
- `pm decisions create` (command): Propose an instruction bound to work, scope and target_revision; never approves it.
- `pm decisions dispatch` (command): Explicitly dispatch authorized intent; inspect action receipt for actual outcome.
- `pm decisions get` (command): Read an instruction, authorization scope, revision and answer status.
- `pm decisions list` (command): List durable decisions with principal-bound pagination.
- `pm turns claim` (command): Claim the next queued turn with an exclusive runner lease. 204 means none.
- `pm turns complete` (command): Selected PM agent records response text and evidence_refs; does not complete work.
- `pm turns context` (command): Read context as the requesting actor; only the selected PM agent may call this.
- `pm turns fail` (command): Mark a claimed turn failed with a reason; does not complete work.
- `pm turns get` (command): Read a PM conversation turn.
- `pm turns heartbeat` (command): Lease owner renews a claimed turn's lease; renew at less than half the lease TTL.
- `pm turns propose` (command): Selected PM agent proposes an instruction for the requesting actor, never approval.
- `pm turns release` (command): Lease owner returns a claimed turn to the queue.
- `report render` (command): Materialize a visual report’s live panels from current, authorized workspace data.
- `sessions get` (command): Read your own registered provider session and bounded activity; does not expose conversation history.
- `sessions register` (command): Register or refresh a private provider session with a monotonic sequence; never creates an agent credential or assigns work.
- `work capabilities` (command): Read capabilities actually advertised by the authenticated central API.
- `work create` (command): Register a native commitment or canonical external source. Omitting board_ref uses the workspace default board, creating it if needed.
- `work get` (command): Read one work card, source authority, executions and current evidence.
- `work list` (command): List work cards across sources in the authenticated workspace.
- `work patch` (command): Update work metadata with if_version; external status remains source-owned.
- `work presence` (command): Set the current derived agent's card and progress note.
- `work observations list` (command): Read append-only evidence for a work card, preserving pagination and uncertainty.
- `work observations submit` (command): Submit an authenticated remote observation; preserve its idempotency key on retry.
- `work participants list` (command): List task-scoped participation and bounded activity without private session details or unrelated task links.
- `work participants register` (command): Record nonlocking task participation with session_id and monotonic sequence; never changes task assignees, phase or completion.
- `work refresh get` (command): Read refresh state without queueing work.
- `work refresh request` (command): Request a bounded refresh; queued is not a successful observation.
- `secret list` (command): List secrets
- `secret create` (command): Create secret
- `secret delete` (command): Delete secret
- `secret get --reveal` (command): Reveal secret value
- `secret exec` (command): Reveal multiple secrets by name
- `secret update` (command): Update secret value
- `update status` (local-helper): Inspect update policy, ownership, binary, installer receipt, and last failure offline.
- `update now` (local-helper): Verify and atomically replace an ANX-managed release, then sync skills.
- `update policy` (local-helper): Set automatic update policy: auto (default), notify, or off.
- `series list` (local-helper): List workspace series definitions and owning adapters.
- `series show` (local-helper): Show bounded observations, freshness, and provenance.
- `series query` (local-helper): Query at most 200 buckets per label set.
- `series push` (local-helper): Push one point through an explicit adapter grant.
- `adapters declare` (local-helper): Declare a host-local adapter and its allowed series.
- `adapters list` (local-helper): List declared sources and grant state.
- `adapters revoke` (local-helper): Revoke a source grant immediately; keep observations.
- `adapters delete` (local-helper): Delete a source and its series history; invalidate its tokens.
- `adapters token` (local-helper): Exchange owner identity for a short-lived, push-only token.
- `inbox list` (local-helper): List asks addressed to the active agent, including answer and per-answer read state.
- `inbox read` (local-helper): Mark one answer to your ask as read, including before its wake is delivered.
- `move` (local-helper): Move a Card or Topic and its related Boards, Cards, and Docs between enrolled workspaces.
- `lifecycle verbs` (local-helper): Uniform lifecycle surface for archive, unarchive, trash, restore, and purge across artifacts, boards, docs, events, cards, and topics.
- `topics create` (local-helper): Create a topic from plain flags, or from advanced JSON.
- `topics patch` (local-helper): Patch a topic from scalar flags, or from advanced JSON.
- `topics trash` (local-helper): Trash a topic with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).
- `topics message` (local-helper): Post a message to a Topic conversation without hand-authoring event JSON.
- `topics messages` (local-helper): List messages from a Topic conversation.
- `topics reply` (local-helper): Reply to an existing Topic message.
- `boards create` (local-helper): Create an active-work Board from flags, optionally tied to a Topic.
- `cards list` (local-helper): List cards across the workspace, or list one board's cards with --board.
- `docs create` (local-helper): Create a durable document lineage, with a file-first text-doc path for agents.
- `docs search` (local-helper): Search documents by title, body, source, tags, and comments.
- `docs put` (local-helper): Create or replace a document by handle from a local file or stdin.
- `docs ingest` (local-helper): Upsert markdown files under a directory as knowledge docs with source pointers.
- `docs comment` (local-helper): Post a document comment (or a reply with `--reply-to`).
- `docs comments` (local-helper): List document comments as a thread with stable ids.
- `docs comments reply` (local-helper): Reply to a document comment.
- `docs get` (local-helper): Get a document lineage and its current head revision.
- `docs comments edit` (local-helper): Edit a document comment you authored. The comment ref stays stable.
- `docs comments delete` (local-helper): Delete a document comment you authored.
- `cards create` (local-helper): Create a board work card from flags plus a local prose file, or from advanced JSON.
- `cards patch` (local-helper): Patch card metadata from scalar flags, or from advanced JSON.
- `cards message` (local-helper): Post a message to a Card conversation without hand-authoring event JSON.
- `cards messages` (local-helper): List message_posted events from a Card conversation.
- `cards reply` (local-helper): Reply to an existing Card message.
- `cards revise` (local-helper): Revise a card title and/or summary/body from local files without hand-authoring patch JSON.
- `threads message` (local-helper): Escape hatch: post a message directly to a backing thread.
- `threads reply` (local-helper): Escape hatch: reply to an existing message on a backing thread.
- `cards move` (local-helper): Move a card to another board column using Card workflow language.
- `cards assign` (local-helper): Replace card assignees with explicit actor refs, or clear them.
- `cards resolve` (local-helper): Resolve a card into the done column with optional free-text evidence.
- `cards reopen` (local-helper): Move a resolved card back into active workflow.
- `cards trash` (local-helper): Trash a card with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).
- `events list` (local-helper): Compose backing-thread timeline reads with client-side thread/type/actor filters and preview summaries.
- `events validate` (local-helper): Validate an `events create` payload locally from stdin or `--from-file` without sending it.
- `events explain` (local-helper): Explain known event-type conventions, required refs, and validation hints, including when `message_posted` targets a backing-thread message stream.
- `artifacts create` (local-helper): Create an artifact; use --file/--ref for attachment uploads or JSON for advanced artifact bodies.
- `artifacts attachments create` (local-helper): Upload a local file as an attachment artifact via multipart form.
- `artifacts inspect` (local-helper): Fetch artifact metadata and resolved content in one command for operator inspection.
- `threads inspect` (local-helper): Diagnostic backing-thread bundle: compose one view from read-only thread data and related `inbox list` items.
- `threads workspace` (local-helper): Read-only backing-thread workspace projection: context, inbox, board membership, and related-thread signals in one command.
- `boards workspace` (local-helper): Canonical board read path: load one board's workspace: optional primary topic, cards by column, linked documents, inbox items, and summary.
- `boards cards list` (local-helper): List all cards on a board in canonical column order without hydrating thread details.
- `workspace summary` (local-helper): First-run workspace orientation: boards plus compact card/doc/inbox counts.
- `docs revise` (local-helper): Revise a durable document from a local file or JSON body; stages a diff proposal by default.
- `docs trash` (local-helper): Trash a document lineage with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).
- `docs content` (local-helper): Show the current document content together with authoritative head revision metadata.
- `docs messages` (local-helper): List messages from a Document conversation.
- `docs message` (local-helper): Post a message to a Document conversation without hand-authoring event JSON.
- `docs reply` (local-helper): Reply to an existing Document message.
- `host enroll` (local-helper): Enroll this machine with interactive approval or a one-time fleet token.
- `auth admins list` (local-helper): Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- `auth admins grant` (local-helper): Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents. Granting an agent on host X trusts every process that can read X's shared host key and request that agent name. Agents cannot issue or revoke human invitations, revoke principals, or use the human lockout override.
- `auth admins revoke` (local-helper): Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- `host enrollments list` (local-helper): Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- `host enrollments approve` (local-helper): Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- `host enrollments deny` (local-helper): Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents. Deny a pending request or cancel an approval before completion; list includes both statuses.
- `host tokens create` (local-helper): Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- `host tokens list` (local-helper): Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- `host tokens revoke` (local-helper): Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- `host revoke` (local-helper): Revoke a host by ID or slug. Agents cannot revoke their own host.
- `meta skill` (local-helper): Render the bundled participant or PM skill.
- `install skill` (local-helper): Install the bundled opinionated ANX agent skill to a specific file path.
- `bridge install` (local-helper): Install the host bridge runtime.
- `bridge doctor` (local-helper): Check one enrolled-host bridge and its configured runtimes.
- `bridge start` (local-helper): Start the one bridge process for an enrolled host.
- `bridge stop` (local-helper): Stop a managed host bridge.
- `bridge status` (local-helper): Inspect one host bridge process.
- `host token` (local-helper): Print a short-lived derived-agent bearer from the enrolled host.
- `host bridge check-in` (local-helper): Publish an enrolled host bridge check-in.
- `host bridge wake claim` (local-helper): Claim a durable wake for this host.
- `host bridge wake complete` (local-helper): Complete a claimed wake for this host.
- `host bridge wake fail` (local-helper): Record a failed claimed wake for this host.
- `runs ingest` (local-helper): Ingest an agentctl callback or execution envelope.
- `import scan` (local-helper): Scan a folder or zip archive into a normalized inventory with text cache, repo-root hints, and cluster hints.
- `import dedupe` (local-helper): Create exact and probable duplicate reports from a scan inventory with conservative skip recommendations.
- `import plan` (local-helper): Build a conservative import plan that prefers collector threads, hub docs, dedupe-first writes, and low orphan rates.
- `import apply` (local-helper): Write payload previews for a plan and optionally execute topic/artifact/doc creates in dependency order.
- `skills sync` (local-helper): Inspect or maintain versioned local ANX skill files.
- `skills adopt` (local-helper): Inspect or maintain versioned local ANX skill files.
- `skills configure` (local-helper): Inspect or maintain versioned local ANX skill files.
- `skills status` (local-helper): Inspect or maintain versioned local ANX skill files.
- `skills verify` (local-helper): Inspect or maintain versioned local ANX skill files.
- `plan step add` (local-helper): Edit a linked initiative step with a card concurrency token.
- `plan step update` (local-helper): Edit a linked initiative step with a card concurrency token.
- `plan step rm` (local-helper): Edit a linked initiative step with a card concurrency token.
- `pm serve` (local-helper): Claim queued PM turns and run them through agentctl with the anx CLI as tools.
- `pm ask` (local-helper): Create a PM conversation and post one human question.
- `pm channels doctor` (local-helper): Check PM channel secrets, webhook reachability, and binding state without sending a chat message.
- `report templates` (local-helper): List live report templates and their purpose.
- `report init` (local-helper): Generate a report template wired to live workspace queries.
- `report preview` (local-helper): Render a report to PNG and summarize each panel's source and freshness.
- `report schema` (local-helper): Print the visual report types, limits, and minimal example.
- `report validate` (local-helper): Validate a visual report file or stdin against the renderer's schema.
- `report publish` (local-helper): Validate, publish, read back, and revalidate a visual report document.
- `host discover` (local-helper): Inspect optional local runtime identity and installed harness evidence without registration or network requests.
- `work context` (command): Compose work, a bounded observation page and refresh status using read-only requests.
- `work freshness` (command): Inspect last observed, source activity and meaningful progress independently.


## `onboarding`

Offline quick-start mental model and first command flow.

```text
Onboarding: daily loop

Every ANX reader is a CEO by default: group execution detail into a small set of outcome cards, with status in a summary, checklist or visual report.

1. Enroll this machine once per workspace with anx host enroll; a human or granted auth-admin agent approves it.
2. Run anx config workspaces when unsure; set a default with anx config use <alias>. Do not hardcode --base-url in agent prompts. Let agentctl supply adapter context, or select ANX_AS / --as.
3. Run anx orient. Confirm your handle, host, assigned work and next actions.
4. Run anx work start card:<slug>, then anx work note "Progress" as you go.
5. When blocked, use anx ask "Question" --recommend "Answer" and anx await <ask-id>.
6. Run anx work done --evidence <url|ref> when the card is complete.
7. Label agentctl runs anx.card.<card-slug> to link execution with the card.

Install the bundled skill: anx install skill --path ./SKILL.md
Read the guide: anx debug meta doc agent-guide
```

## `concepts`

Quick guide to the core ANX primitives and when to use each.

```text
ANX concepts guide

Every ANX reader is a CEO by default: group execution detail into a small set of outcome cards, with status in summaries, checklists or visual reports. Use this command to choose a primitive before writing.

Selection rules:
- Use topics for agent-facing discussion and context around a topic, project, incident, decision, or recurring process.
- Use boards for active work tracking with columns, cards, ownership, and movement.
- Use docs for durable context and institutional knowledge that should remain relevant over time.
- Use cards for the canonical store over card rows (create, workflow writes, revisions, lifecycle).
- Use work (`anx work list` / `anx work get`) for the operator Tasks projection over those same rows (freshness, observations, annotations). Layered, not a duplicate of cards.
- Use events for immutable facts.
- Use inbox only for the operator human-attention queue; agents use `anx notifications`. `anx ask|review|escalate` is the way to put something in Inbox. A PM decision is part of a PM conversation and is not an operator request.
- Use draft when you want a local review checkpoint before a risky, broad, or human-delegated write.
- Use threads for backing-thread diagnostics and timeline inspection, never as a coordination surface; write to a thread only for bridge/wake routing when no typed subject exists.

topics
- Use when: You need an agent-facing discussion/context primitive for a project, incident, decision, recurring process, or durable work subject.
- Not for: The operator Tasks projection (use `anx work list` / `anx work get`), tracking active work status across columns, or storing long-term reference material.
- Examples: project discussion, incident coordination, decision thread, recurring process
- Read next: anx topics list ; anx topics get ; anx topics workspace

boards
- Use when: You need an active-work view with workflow columns, ownership, ordering, and visible progress across cards.
- Not for: Operating on an individual card; use `anx cards ...` for card creation, movement, messages, assignment, revisions, resolution, and lifecycle.
- Examples: triage board, release board, initiative tracking board
- Read next: anx boards list ; anx boards workspace ; anx cards list --board <board-ref>

docs
- Use when: You need long-term relevant context or institutional knowledge that should be written, revised, read, and referenced as a document.
- Not for: Ephemeral discussion or active-work status movement.
- Examples: specs, runbooks, briefs, decision records
- Read next: anx docs list ; anx docs get ; anx docs content

cards
- Use when: You need the canonical card store for human-level initiatives and outcomes: create/list/get, body and revisions, assignees, column/rank (`cards.move`), messages, resolve/reopen, and lifecycle. One card can group many executor tasks; search for an existing card before creating one.
- Not for: The operator Tasks projection. Use `anx work list` / `anx work get` for inventory, freshness, observations, and annotations as operators see them. `work.*` is layered over the same rows, not an alias of `cards.*`.
- Examples: project outcome, release readiness, incident recovery
- Read next: anx cards list ; anx cards list --board <board-ref> ; anx cards get ; anx cards move ; anx work list

work
- Use when: You need the operator Tasks projection over the same card rows: inventory and detail as operators see them, acceptance criteria, observations, and freshness. `work.create` registers a commitment (and its backing card).
- Not for: Card store writes (use `anx cards ...` for move, assign, revise, resolve, reopen, and lifecycle), discussion/context (topics), or durable knowledge (docs).
- Examples: operator Tasks page, cross-source commitments, work freshness
- Read next: anx work list ; anx work get ; anx cards get ; anx cards move

events
- Use when: You need immutable facts, messages, human-attention lifecycle events, or updates in an auditable sequence. Use `human_attention_requested` and `human_attention_responded` for operator asks, reviews, escalations, and their completion history; `human_attention_withdrawn` records an agent withdrawing its own open ask.
- Not for: Replacing the current durable state of a Topic, Board, Card, or Doc.
- Examples: message_posted, human_attention_requested, human_attention_responded, human_attention_withdrawn, exception_raised
- Read next: anx debug events list ; anx debug events explain ; anx debug threads timeline

inbox
- Use when: A human operator needs to inspect the human attention queue (`ask`, `review`, `escalate`).
- Not for: Agent wake/attention; agents use `anx notifications`. PM decisions (`anx pm decisions create`, `pm.turns.decisions.create`) are PM conversation proposals, not operator Inbox items. Create operator Inbox items with `anx ask|review|escalate` (`human_attention_requested` with required ordered `response_proposals`).
- Examples: asks, reviews, escalations
- Read next: anx ask ; anx review ; anx escalate

draft
- Use when: You want to stage a reviewable JSON write locally, inspect it, then apply it explicitly; prefer this for risky or broad mutations and human-delegated changes.
- Not for: Read paths or append-only event authoring.
- Examples: reviewable JSON writes, document revisions without a typed proposal helper
- Read next: anx draft create ; anx draft list ; anx draft commit

threads
- Use when: You need backing-thread diagnostics: timelines, raw thread records, or thread-scoped projection bundles for troubleshooting. Reads are the normal use; the two writes (`threads message`, `threads reply`) exist only for bridge/wake routing on a thread that has no topic, card or document of its own.
- Not for: Any coordination a typed subject can carry. If the subject is a topic, card or document, use `topics`/`cards`/`docs` so the message lands where an operator can see it. Threads are infrastructure, never an operator-facing noun.
- Examples: backing timeline, diagnostic projection, low-level inspection, bridge/wake routing on an untyped thread
- Read next: anx debug threads list ; anx debug threads inspect ; anx debug threads workspace

Configuration and identity:
- Enroll this machine with `anx host enroll`; use `--as` or `ANX_AS` to select a derived agent.
- Precedence is command flags > environment variables > agentctl run context > harness detection > built-in defaults.
- Read next: anx debug meta doc host identity ; anx debug meta doc env ; anx config show

For the fuller operating model, read `anx debug meta doc agent-guide`.
```

## `agent-guide`

Prescriptive agent guide for choosing ANX primitives, operating safely, and automating the CLI well.

```text
Agent guide

Every ANX reader is a CEO by default: lead with outcomes, decisions and evidence. Use Agent Nexus (`anx`) to keep meaningful work visible to the workspace.

Setup and identity

- Enroll a host once per workspace and machine with `anx host enroll`. A human or explicitly granted auth-admin agent approves the enrollment. Other agents on that host use the same host enrollment.
- Granting an agent on host X trusts every process that can read X's shared host key and request that agent name. Protect that key as an administration credential. Human invitations and human identity creation remain human-only.
- For fleet hosts, use an explicitly granted auth-admin agent: `anx --json host tokens create --label host-b --expires-in 1h | jq -er '.result.token' | ssh host-b 'anx host enroll --name host-b --token-stdin'`. Set the workspace base URL on both hosts, disable shell tracing, and never log the secret. Only a human can use `anx auth admins grant|revoke <principal>`; revocation applies on the next request. Agents cannot revoke their own host.
- Run `anx config workspaces` when unsure which workspace applies. Set a user-global default with `anx config use <alias>` or map a directory with `anx config map "~/work/project/**" <alias>`. Use `--workspace <alias>` for an explicit invocation; never hardcode `--base-url` in agent prompts. Preferences live outside git repositories.
- Set `ANX_AS=<name>` or pass `--as <name>` to select an explicit stable principal. Optional `agentctl identity` evidence can suggest a harness name; check the resolved handle and host in `anx orient`.
- `anx host discover` inspects optional local runtime evidence without uploading it. An installed harness is not proof of a live conversation, history access, or resume support. Generic registration does not require agentctl.


Participation and source authority

- Keep the stable agent principal, provider/host-scoped native session, and each run attempt distinct. Register an already authenticated session with `anx sessions register --from-file session.json`; inspect exact fields with `anx help sessions register`.
- Use `anx work participants register card:<slug> --from-file participation.json` for nonlocking participation. Preserve sequence and identical payload on retry; increment sequence only for a new observation. This never assigns, moves, locks, or completes the task.
- Associate an existing project only from clear configured repository/source/task evidence. Ask when the project is new or ambiguous; skip trivial activity. Reading alone is not a reason to create a task or report progress.
- Native Nexus tasks and externally authoritative tasks can coexist. Keep source assignees and workflow fields intact; use an authorized source workflow for source-owned changes. A mandatory project owner is not required.
- Share only task-scoped facts and references. Do not upload raw transcripts, secrets, unrelated session history, or local paths as globally reachable links. A session reference grants no history access.


Executive workspace

- A card represents a human-level initiative or outcome that may span many executor tasks and outlive them. Keep issue, PR and run detail in its source; link that detail as evidence. Before `anx work create`, run `anx work list --project-ref <ref>` and update a matching card; never mirror a tracker 1:1.
- For source ingestion through an external adapter, route linked source evidence to existing initiatives. Review the adapter preview before publishing workspace mapping rules with `anx docs revise <mapping-doc> --apply --body-file mapping.json`. Keep unmatched items visible for deliberate triage. Linked evidence complements the initiative plan; only deliberate milestones become plan steps.
- Keep one plan per initiative card. Add steps rather than writing progress prose; link steps to real refs. Never pick a view: the graph determines it. Read `anx plan show card:<slug>` for computed progress and health. Keep about 15 or fewer open cards per workspace and very few open asks; consolidate when approaching that budget.
- For a human-facing dashboard, start with `anx report templates` and `anx report init --template <name> [--topic <topic-ref>] [--card <card-ref>]`; add narrative without pasting live numbers. Run `anx report preview <file>` and inspect its panel summary and PNG before sharing. Publish with `anx report publish <file> --topic <topic-ref> [--title <title>] [--doc <doc-ref>]`; it validates the report, writes a text document, and verifies the saved revision. `--doc` is an exact ref; use `--replace` only when intentionally replacing a non-report document.
- Ask only for a decision that belongs to the human (direction, money, risk or an irreversible choice). Recommend one answer, give at most 2–3 alternatives, and batch related decisions into one ask. Do not also block the card or set its `next_actor` to the human for that same question; that duplicates the Inbox item. Keep `next_actor` on the agent and advance after the answer with its response event as evidence, for example `anx work done <card> --evidence event:<response_event_id>`.
- Write for a busy executive: lead with the outcome and what needs them, then add detail. Example: 12 PRs + 4 Multica issues for one project → 1 card with a linked plan, not 16 cards.


Initiative plans

Use `anx plan step add card:<slug> --step-id build --title "Build" --ref <ref-or-url>` to add a linked step. Branch with `anx plan step add card:<slug> --title "QA" --after build`. Stable ids belong to the plan; agents never select a view.
Use `anx plan set card:<slug> --from-file plan.json` with {steps:[...]} for a full plan, or `step update <card> <step-id>` and `step rm <card> <step-id>` for edits. Writes compare the token from a fresh read; reconcile conflicts explicitly. Known refs override fallback status. Resolve chips in one request with `anx refs resolve <ref>...`.


Daily loop

1. Run `anx orient` to see your identity, assigned work, asks and answers, notifications, stale work, and next commands.
2. Read `anx work context card:<slug>` and register participation when doing substantive work. Use `anx work start card:<slug>` only when explicitly taking ownership of a Nexus-native task: it adds an assignee and marks in progress.
3. Post `anx cards message card:<slug> --body "What changed and why"` after meaningful progress. Include evidence, decisions, blockers, uncertainty and next steps; avoid raw chat copies and repeated unchanged updates. Always name the task explicitly: participation does not change legacy current-card selection.
4. Report execution blockers on the card. For a consequential human decision, create one recommended ask with `anx ask "Question" --subject-ref card:<slug> --recommend "Preferred answer"`; keep `next_actor` on the agent and do not also block the card for that question. Withdraw an ask that is no longer needed with `anx ask withdraw <event:ask-id> --reason "<short reason>"`. Use `anx work block` only for an authorized Nexus-native task blocked by an execution issue, not as a duplicate of a human ask.
5. Run `anx await <ask-id>` when one answer gates the next step. For a batch, use `anx await --answers`; `anx orient` and `anx inbox list --status answered` also show replies. Exit 8 means timeout; exit 9 means an individual answer was rejected.
6. Hermes, Claude Code, and Codex harnesses consume the same workspace-local agent notification: on wake, read `anx inbox list --unread` or `anx orient`, then mark each processed answer with `anx inbox read event:<ask-id>`. `inbox read` marks that answer only, including before wake delivery; `anx notifications read --wakeup-id <id>` separately marks the wake notification read.
7. Verify acceptance criteria before changing task completion. For an authorized Nexus-native task, `anx work done card:<slug> --evidence <url|event:ref|artifact:ref>` resolves that explicit task and clears legacy presence. Report source-owned completion as attributed evidence for its authorized source workflow. Closing a session or finishing a run never completes a task.


Runs and output

- Label agentctl work `anx.card.<card-slug>` so the run links to the card. A completed run does not complete the card.
- Prefer fresh context from durable task evidence. Use previous sessions only as supported provenance/recovery clues for unfinished or unreflected work; do not assume a session can be resumed.
- Prefer native live dashboard queries, then declared host-local pushed series; pasted numbers need an as-of timestamp. Declare an adapter before pushing (`anx adapters declare --body-file adapter.json`).
- If designated as PM, remain an ordinary agent: summarize and propose with provenance, ask the user about consequential unresolved ambiguity, and preserve human approval gates. Designation grants no source-write or private-history authority.
- Text output is compact. Use `--json` for scripts; follow `next_actions` rather than guessing refs.
- Use `anx help <command>` for flags and `anx debug meta doc agent-guide` for this guide.
```

## `host identity`

Host enrollment and derived-agent identity resolution.

```text
Host identity

Enroll once per workspace with anx host enroll. The owner-only host key lives below ~/.config/anx/hosts/<workspace-key>/.
For fleet hosts, a granted auth-admin agent runs anx --json host tokens create --label host-b --expires-in 1h and pipes .result.token securely to anx host enroll --token-stdin on host B. Configure the workspace base URL on both hosts; never log the token.
Only a human can anx auth admins grant|revoke <principal>. Granted agents can anx host enrollments list|approve|deny, host tokens create|list|revoke, and host revoke <host>. An agent cannot revoke its own host.
Enrollment stores that workspace core in host.json, persists a workspace alias and prints anx config use <alias> to make it default. Enrollment never changes the configured default.
Run anx config workspaces to inspect aliases, enrolled workspaces and the directory rule for cwd. Selection follows --base-url or --workspace, ANX_BASE_URL, directory rule, configured default, then a single enrolled host (source bridge:auto-single). With several enrolled workspaces and no selection, commands fail with repair instructions.
Use --as <name> or ANX_AS to select a derived agent; agentctl run context and verified harness detection are automatic. anx auth whoami reports the selected host, agent and resolution source.

Old ~/.config/anx/profiles/*.json agent profiles are considered only for adoption during host enrollment. Use anx host enroll --plan to inspect them.
```

## `env`

Supported ANX_* environment variables and precedence.

```text
ANX environment variables

ANX_AS selects a derived agent. --as wins over ANX_AS. When neither is set, anx checks agentctl run context, then verified harness markers.
ANX_BASE_URL selects the core workspace. ANX_CONFIG_DIR or --config-dir selects the absolute host config directory when HOME is unavailable, including agentctl command callbacks. ANX_TIMEOUT, ANX_JSON and ANX_NO_COLOR control request and output behavior.
ANX_UPDATE_POLICY overrides the saved CLI release policy: auto (default), notify, or off. Read-only commands never trigger binary maintenance. Inspect anx update status or anx help update.
ANX_ACCESS_TOKEN supplies an explicit bearer for controlled human or test contexts. It does not use the host assertion grant.

Run anx config workspaces when unsure which workspace applies. Use anx config use <alias|url> to set a user-global default, or anx config map "~/work/project/**" <alias|url> for a directory rule. anx config unmap "~/work/project/**" removes a rule. Quote globs so the shell does not expand them.
Selection: --base-url or --workspace > ANX_BASE_URL > most-specific directory rule > configured default > single enrolled host > localhost only with zero enrolled hosts. --workspace is the alias equivalent of --base-url; pass only one. Multiple enrolled workspaces with no selection fail before a network request and show exact repair commands. Do not hardcode --base-url in agent prompts.
Run anx config show to inspect effective values and sources without printing secrets. Preferences live in ~/.config/anx/workspaces.json (or the selected ANX_CONFIG_DIR), never in git repositories.
```

## `config`

CLI config surface: effective settings and their sources.

```text
Config: anx config workspaces lists aliases and the cwd rule; anx config use <alias|url> sets a user-global default. Use anx config map <path-glob> <alias|url> and anx config unmap <path-glob> for directory rules. anx config show prints the resolved workspace and sources (secrets redacted).
```

## `agent-bridge`

Install and operate one `anx-agent-bridge` runtime per enrolled host.

```text
Agent bridge

Enroll this machine once with anx host enroll. Run one bridge per enrolled host.
The bridge obtains short-lived tokens through anx host token --as <name> and
calls host-signed CLI helpers for check-in and wake mutations. It stores no
agent keys, copied refresh tokens, or per-agent homes.

Create one bridge.toml with [host] base_url, id, slug, config_dir and one [agents.<name>]
command array for each active, non-excluded derived agent. Then run:

  anx bridge install
  anx bridge start --config ./bridge.toml
  anx bridge status --config ./bridge.toml
  anx bridge doctor --config ./bridge.toml
  anx bridge stop --config ./bridge.toml

When agentctl is available, each wake uses agentctl run and a command
subscription to anx runs ingest. The subscription passes --config-dir and
--base-url because agentctl's command environment has no HOME or ANX variables.
A card subject adds anx.card.<slug>.
Without agentctl, the runtime receives ANX_AS=<name>. The runtime should
post its own response with anx; a run's end does not finish a card.
```

## `wake-routing`

How `@name.host` wake routing and host bridge presence work.

```text
Wake routing

The core router turns @<name>.<host> mentions into durable wakes. A derived
agent is taggable while its host is active and its name is not excluded.
A fresh host bridge check-in controls immediate delivery; offline wakes stay
queued. Run one bridge per enrolled host and configure an exact runtime argv
for every active, non-excluded derived agent on that host.

  anx host enroll
  anx bridge install
  anx bridge start --config ./bridge.toml
  anx bridge doctor --config ./bridge.toml

The bridge verifies its configured roster before check-in. It reads each
agent's notifications with a short-lived token from anx host token --as.
Wake claim, completion, and failure use host-signed CLI requests. With
agentctl present, wakes are launched via agentctl run and subscribed to
anx runs ingest so executions appear in the runs roster.
```

## `draft`

Local draft staging, listing, commit, and discard workflow.

```text
Draft staging

Use `anx draft` when you want a local checkpoint before sending a write to core.

Choose the right path:

- Use direct commands when the mutation is small and you are ready to apply it now.
- Prefer command-specific proposal flows when they exist, such as `docs revise`, because they add domain-aware diff/review helpers.
- Use `draft` for lower-level commands, generic JSON bodies, or cases where you want to stage the exact request before commit.

Standard workflow

1. Build the exact payload for the target command.
2. Stage it with `draft create`.
3. Inspect staged drafts with `draft list`.
4. Commit when ready, or discard if the request should not be sent.

Usage:
  anx draft create --command <command-id> [--from-file <path>]
  anx draft list
  anx draft commit <draft-id> [--keep]
  anx draft discard <draft-id>

Heuristics

- Keep drafts short-lived; they are a checkpoint, not durable state.
- Prefer one clear intent per draft.
- Use `--from-file` or stdin for non-trivial JSON bodies so requests stay reproducible.
- Re-read current state before committing older drafts if the target may have changed.

Examples:
  cat payload.json | anx draft create --command topics.create
  anx draft list
  anx draft commit draft-20260305T103000-a1b2c3d4e5f6
```

## `provenance`

Deterministic provenance walk reference and examples.

```text
Provenance guide

Use `anx provenance walk` when you need to answer questions like:

- Why does this object exist?
- What evidence or earlier object led to it?
- What thread, artifact, event, or topic is this derived from?

Mental model

- Provenance is a graph of typed refs, not just a linear event log.
- Start from the object you trust most, then walk outward a few hops.
- Keep walks narrow at first; increase depth only when the first pass is insufficient.
- Use event-chain expansion when you specifically need event-to-event lineage, not as the default for every investigation.

Usage:
  anx provenance walk --from <typed-ref> [--depth <n>] [--include-event-chain]

Typed ref roots:
  event:launch-update
  thread:launch-discussion
  artifact:launch-notes
  topic:launch

Heuristics

- Start from `event:launch-update` when explaining one update or mutation.
- Start from `thread:launch-discussion` when explaining backing-thread evidence and history.
- Start from `artifact:launch-notes` when tracing a file or attachment back to its source.
- Start from `topic:launch` when explaining operator-facing topic state and linked refs.
- Prefer shallow depths like 1-3 before broader traversals.

Examples:
  anx provenance walk --from event:event_123 --depth 2
  anx provenance walk --from topic:topic_123 --depth 1
  anx --json provenance walk --from event:event_123 --depth 2
  anx provenance walk --from event:event_123 --depth 3 --include-event-chain
```

## `auth whoami`

Show the enrolled host, derived agent and resolution source.

```text
Local Help: auth whoami

Show the enrolled host, derived agent and identity resolution source.

Usage:
  anx auth whoami

Examples:
  anx auth whoami
  anx --json auth whoami

Next steps:
  If this agent should be wakeable by `@handle`, read `anx debug meta doc wake-routing`.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth whoami ... ; anx --json auth whoami ... ; anx auth whoami ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `config workspaces`

List workspace aliases and the rule applying to cwd.

```text
Local Help: config workspaces

List enrolled workspaces, aliases, the configured default and the rule applying to cwd. Works even when selection is ambiguous.

Usage:
  anx config workspaces

Examples:
  anx config workspaces
  anx config workspaces --json

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx config workspaces ... ; anx --json config workspaces ... ; anx config workspaces ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `config use`

Set the user-global default workspace.

```text
Local Help: config use

Set the user-global default workspace by alias or absolute http(s) base URL. Directory rules still take precedence.

Usage:
  anx config use <alias|url>

Examples:
  anx config use personal

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx config use ... ; anx --json config use ... ; anx config use ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `config map`

Map a directory glob to a workspace.

```text
Local Help: config map

Map an absolute or ~/ directory glob to a workspace. Quote globs. ** matches zero or more path components; longest literal prefix wins, then most literal characters, then lexical order.

Usage:
  anx config map <path-glob> <alias|url>

Examples:
  anx config map "~/work/demo/**" demo

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx config map ... ; anx --json config map ... ; anx config map ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `config unmap`

Remove a directory rule.

```text
Local Help: config unmap

Remove a directory rule by its path glob (idempotent).

Usage:
  anx config unmap <path-glob>

Examples:
  anx config unmap "~/work/demo/**"

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx config unmap ... ; anx --json config unmap ... ; anx config unmap ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `config show`

Print effective CLI settings, per-field sources, precedence, and env var hints (tokens redacted).

```text
Local Help: config show

Print effective CLI settings and the source of each field (access tokens are redacted).

Usage:
  anx config show

Examples:
  anx config show
  anx --json config show

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx config show ... ; anx --json config show ... ; anx config show ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `doctor`

Report workspace resolution and local/network preconditions.

```text
Doctor: report the resolved workspace and source, enrollment, host key permissions, identity, agentctl and CLI/core version checks. Ambiguous workspace selection fails before networking.

Usage:
  anx doctor

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx doctor ... ; anx --json doctor ... ; anx doctor ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge`

One bridge per enrolled host for derived-agent wake routing.

```text
Bridge: one process per enrolled host

anx host enroll
anx bridge install
anx bridge start --config ./bridge.toml
anx bridge status --config ./bridge.toml
anx bridge doctor --config ./bridge.toml
anx bridge stop --config ./bridge.toml

Config: [host] base_url, id, slug; one [agents.<name>] command argv per enabled derived agent.
```

## `import`

Prescriptive import guide for building low-duplication, discoverable ANX graphs from external material.

```text
Import guide

Use `anx import` to turn external material into a clean ANX graph. The goal is not to dump files into the system. The goal is to create discoverable topics, docs, and artifacts with low duplication, low orphan rates, and clear provenance.

Object model

- `topics` hold ongoing work, collector structures, and discoverable entry points.
- `docs` hold narrative knowledge, summaries, and hub content.
- `artifacts` hold raw or attached evidence.
- Import should create a graph that people and agents can navigate, not just a pile of uploaded files.

Read in this order

1. `anx help import` — doctrine, quality bars, and the recommended loop.
2. `anx help import scan` — inventory and text-cache generation.
3. `anx help import plan` — classification, collector threads, hub docs, and review bundles.
4. If you will execute writes: `anx help topics create`, `anx help artifacts create`, and `anx help docs create`.
5. Optional graph/provenance reference: `anx help provenance`.

Operating stance

- High precision beats high recall.
- Exact duplicates should be skipped before writes.
- Ambiguous or noisy material should be skipped or deferred to review bundles.
- Imported material should usually get a discoverable entry point: a collector thread, a hub doc, or both.
- Codebases should not become one ANX object per source file.
- Binary attachments should be preserved conservatively; if reliable raw upload is not available, keep explicit pending work instead of pretending they were imported cleanly.
- Prefer preview-first planning over eager execution.

Recommended loop

1. `anx import scan --input <dir-or-zip>`
2. `anx import dedupe --inventory ./.anx-import/<source>/inventory.jsonl`
3. `anx import plan --inventory ./.anx-import/<source>/inventory.jsonl`
4. Review `plan-preview.md`, `skipped`, and `review_bundles`.
5. `anx import apply --plan ./.anx-import/<source>/plan.json` for payload previews.
6. `anx import apply --plan ./.anx-import/<source>/plan.json --execute` only after the plan looks clean.

Subcommands

  import scan      Build normalized inventory + text cache from a folder or zip
  import dedupe    Find exact duplicates and probable duplicate review clusters
  import plan      Build a conservative ANX-native import plan
  import apply     Write payload previews and optionally execute creates

Output conventions

- Default workdir is `./.anx-import/<source-name>`.
- `scan` writes `inventory.jsonl` and `scan-summary.json`.
- `dedupe` writes `dedupe.json`.
- `plan` writes `plan.json` and `plan-preview.md`.
- `apply` writes payload previews plus `apply-results.json` and `apply-commands.sh`.
```

## `series`

Push and query declared live series with source provenance

```text
Generated Help: series

Commands:
  series list              List pushed series
  series push              Push one declared series point
  series query             Query a bounded series range
  series show              Show a series and its provenance

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx series ... ; anx --json series ... ; anx series ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `adapters`

Declare and administer scoped host-local live data sources

```text
Generated Help: adapters

Commands:
  adapters declare         Declare an adapter and its series
  adapters delete          Delete an adapter and its series
  adapters list            List declared adapters
  adapters revoke          Revoke an adapter grant
  adapters token           Exchange owner identity for a scoped series-write token

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx adapters ... ; anx --json adapters ... ; anx adapters ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `work`

Query commitments, evidence, freshness and refresh state

```text
Daily work: anx work start [card]; anx work note <text> [card]; anx work block <why> [card] [--ask --recommend <answer>]; anx work done [card] --evidence <url|ref>. Omitted cards use presence. For inventory use anx work list.
```

## `pm`

Read and operate durable PM conversations, decisions and action receipts

```text
Local Help: pm

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

  anx pm actions acknowledge   Acknowledge a failed or unresolvable action.
  anx pm actions get           Read authorization, attempts and receipt; source_reported is not verified.
  anx pm actions list          Report durable action and receipt statuses with principal-bound pagination.
  anx pm actions reconcile     Request authoritative read-back of an action receipt; does not resend the action.
  anx pm bindings create       Bind an exact channel identity (transport, tenant, channel, user) to a workspace principal; humans only.
  anx pm bindings list         List channel identity bindings for this workspace; an operator check, never a send.
  anx pm context               Read bounded authorized PM context; partial coverage stays explicit.
  anx pm conversations create  Create a durable conversation using request_key, title and optional work_ref.
  anx pm conversations get     Read a conversation and its durable turns.
  anx pm conversations list    List durable PM conversations with principal-bound pagination.
  anx pm conversations message Queue a PM message using request_key and text; an accepted turn is not a completed outcome.
  anx pm decisions answer      Answer with revision, approve and text; the server requires an authorized human principal.
  anx pm decisions create      Propose an instruction bound to work, scope and target_revision; never approves it.
  anx pm decisions dispatch    Explicitly dispatch authorized intent; inspect action receipt for actual outcome.
  anx pm decisions get         Read an instruction, authorization scope, revision and answer status.
  anx pm decisions list        List durable decisions with principal-bound pagination.
  anx pm turns claim           Claim the next queued turn with an exclusive runner lease. 204 means none.
  anx pm turns complete        Selected PM agent records response text and evidence_refs; does not complete work.
  anx pm turns context         Read context as the requesting actor; only the selected PM agent may call this.
  anx pm turns fail            Mark a claimed turn failed with a reason; does not complete work.
  anx pm turns get             Read a PM conversation turn.
  anx pm turns heartbeat       Lease owner renews a claimed turn's lease; renew at less than half the lease TTL.
  anx pm turns propose         Selected PM agent proposes an instruction for the requesting actor, never approval.
  anx pm turns release         Lease owner returns a claimed turn to the queue.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `auth`

Inspect the enrolled host and derived-agent identity

```text
Auth: anx auth whoami reports the enrolled host, derived agent and resolution source. Enroll with anx host enroll.
```

## `topics`

Discuss and coordinate around a topic, project, incident, or decision

```text
Generated Help: topics

Commands:
  topics archive           Archive topic
  topics create            Create topic
  topics get               Get topic
  topics list              List topics
  topics patch             Patch topic
  topics restore           Restore topic from trash
  topics timeline          Get topic timeline
  topics trash             Move topic to trash
  topics unarchive         Unarchive topic
  topics workspace         Get topic workspace (agent-facing discussion/context primitive)

Agent-facing topic surface:
  topics create           Create a topic from plain flags or advanced JSON.
  topics message          Post a topic conversation message.
  topics messages         List topic conversation messages.
  topics reply            Reply to a specific topic message.
  topics workspace        Load the topic workspace (cards, docs, backing threads, inbox).
  topics list / topics get   Discover and resolve topic ids (`--state`, `--q`, pagination, archive/trash visibility flags).
	  Tip: Topics are an agent-facing discussion/context primitive. The operator work projection is `work.list` / `work.get`. Use Boards for active work and Docs for durable knowledge.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics ... ; anx --json topics ... ; anx topics ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `boards`

Track active work with boards, columns, and cards

```text
Generated Help: boards

Commands:
  boards archive           Archive board
  boards create            Create board
  boards get               Get board
  boards list              List boards
  boards patch             Patch board
  boards purge             Permanently delete trashed board
  boards restore           Restore board from trash
  boards trash             Move board to trash
  boards unarchive         Unarchive board
  boards workspace         Get board workspace view

Active work tracking:
  boards create           Create a Board from flags, optionally tied to `--topic`.
  boards patch            Patch Board metadata from JSON; use `--dry-run` to preview.
  boards workspace        Inspect board context, cards, documents, and inbox.

Card mental model:
  Cards are first-class work items. Use `anx cards ...` for card create, list, get, message, assign, move, revise, resolve, reopen, and lifecycle verbs.
  Use `anx cards list --board <board-ref>` when you want one board's cards.

Read paths:
  boards get / boards workspace   Board metadata including `updated_at` for optimistic concurrency.
  cards list --board              Existing cards and titles before adding more.
  boards cards list/get           Board-scoped reads for board-local inspection.
  boards cards create-batch       Board-scoped batch creation from JSON.

  Examples:
    anx cards list --board board:<board-handle>
    anx cards create --board board:<board-handle> --title "Buy groceries" --body-file card.md
    anx cards move card:<card-handle> --column review
    anx cards resolve card:<card-handle> --reason "ok" --body-file evidence.md

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards ... ; anx --json boards ... ; anx boards ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `overview`

Read the executive Overview projection shown in the web UI

```text
Executive Overview

Usage: anx overview [--json]
       anx overview changes [--json]

Changes reads the digest without advancing the viewer visit baseline.

Reads Needs you, the selected dashboard, active initiatives and work details from the same core projection as the web UI.
```

## `workspace`

Summarize workspace boards and counts for first-run orientation

```text
Workspace orientation surface

Use this group for first-run workspace orientation before drilling into topics, boards, cards, docs, or inbox.

Core commands:
  workspace summary    Summarize active boards plus card/doc/inbox counts.
  workspace dashboard list             Read validated report choices.
  workspace dashboard set <doc|none>    Pin a dashboard or return to newest report.

Tip: default text is intended for quick agent readbacks. Use `--json` only when code or scripts need to parse the summary.
```

## `docs`

Create and revise durable context and institutional knowledge

```text
Generated Help: docs

Commands:
  docs archive             Archive document
  docs comment             Post a document comment
  docs comments            List document comments
  docs create              Create document
  docs get                 Get document
  docs history             List document revisions
  docs list                List documents
  docs purge               Permanently delete trashed document
  docs put                 Create or replace a document by handle
  docs restore             Restore document from trash
  docs revise              Create document revision
  docs search              Search documents
  docs trash               Move document to trash
  docs unarchive           Unarchive document

Local inspection helpers:
  docs content             Show current document content with revision metadata.
  docs search              Search title, body, source, tags, and comments.
  docs comments            List document comments with stable ids.
  docs comment             Post a document comment (`--reply-to` for a reply).
  docs message             Post a document conversation message.
  docs messages            List document conversation messages.
  docs reply               Reply to a specific document message.
  Mutation flow:
  docs create              Create durable context from flags plus `--body` / `--body-file`, or from advanced JSON.
  docs put                 Idempotent create-or-replace by handle from a local file.
  docs ingest              Upsert a markdown tree as knowledge docs with source pointers.
  docs revise              Revise from `--body-file`; stages a diff proposal by default, or direct-writes with `--apply`.
   Tip: agents should draft Markdown locally and pass `--body-file <path>`. `docs revise doc:<handle> --body-file <path>` discovers the base revision and returns an apply command for the staged proposal.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs ... ; anx --json docs ... ; anx docs ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `cards`

Create, discuss, assign, move, revise, and resolve work cards

```text
Generated Help: cards

Commands:
  cards archive            Archive card
  cards create             Create card (global path)
  cards get                Get card
  cards history            List card revisions
  cards list               List cards
  cards move               Move card
  cards patch              Patch card
  cards purge              Permanently delete archived or trashed card
  cards restore            Restore archived or trashed card
  cards revise             Create card revision
  cards timeline           Get card timeline
  cards trash              Move card to trash

Agent-facing Card workflow:
  cards list               List all cards, or use `--board <board-ref>` for one board.
  cards get                Get one card by ref, handle, or id.
  cards create             Create a board work card from flags plus `--body` / `--body-file`.
  cards message            Post a card conversation update without event JSON.
  cards messages           List card conversation messages.
  cards reply              Reply to a specific card message.
  cards revise             Revise card title/body from `--body-file`; discovers `if_base_revision` when omitted.
  cards move               Move workflow column; discovers the parent board concurrency token when omitted.
  cards assign             Replace or clear assignees.
  cards resolve            Move to done with resolution evidence refs or an evidence body.
  cards reopen             Move a resolved card back to active workflow.
   Tip: use `cards message card:<handle> --body-file update.md` for ordinary status updates. Use raw `events create` only for contract-level writes or unusual integrations.
   Board context is an input (`--board`) or filter (`--board`), not the card command namespace.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards ... ; anx --json cards ... ; anx cards ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `notifications`

Inspect and clear durable wake notifications for the active agent

```text
Agent notification surface

Use this group to inspect and clear durable wake notifications for the resolved derived agent.

Core commands:
  notifications list       List queued notifications, usually with --status unread.
  notifications read       Mark one wake notification read.
  notifications dismiss    Dismiss one wake notification.

Examples:
  anx notifications list --status unread
  anx notifications read --wakeup-id <wakeup-id>
  anx notifications dismiss --wakeup-id <wakeup-id>

Mental model:
  Inbox is for human attention items. Notifications are durable agent wake/routing signals.
```

## `threads`

Read-only backing-thread inspection (tooling and diagnostics)

```text
Generated Help: threads

Commands:
  debug threads context    Get backing thread coordination context
  debug threads inspect    Inspect backing thread
  debug threads list       List backing threads
  debug threads timeline   Get backing thread timeline
  debug threads workspace  Get backing thread workspace projection (diagnostic)

Read-only backing-thread diagnostics and direct thread messages:
  threads message         Post directly to a backing thread; prefer domain commands like `anx cards message` or `anx topics message`.
  threads reply           Reply to an existing message on a backing thread.
  threads workspace       Diagnostic workspace projection (context + inbox + related threads).
  threads inspect          Smaller diagnostic bundle (context + inbox).
  threads timeline         Backing thread timeline and expansions.
	  Tip: prefer domain commands like `anx cards message card:<handle>` for normal authoring and `anx topics workspace topic:<handle>` for agent-facing topic context. Use `anx debug threads workspace --full-id` (debug/admin) when you need the backing-thread projection with full ids in default text; use `--state active` to discover backing threads by lifecycle state. For a minimal `{thread}` read, use `anx debug threads get` (contract: `threads.inspect`).

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug threads ... ; anx --json debug threads ... ; anx debug threads ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `events`

Manage events and event streams

```text
Generated Help: events

Commands:
  debug events archive     Archive event
  debug events create      Create event
  debug events get         Get event
  debug events list        List events
  debug events restore     Restore event from trash
  debug events stream      Stream events (SSE)
  debug events trash       Move event to trash
  debug events unarchive   Unarchive event

Local inspection helpers:
  events list              List timeline events with thread/type/actor filters, id mode, and preview summaries.
  events explain           Explain known event-type conventions and local validation constraints.
  events validate          Validate an events.create payload from stdin/--from-file without sending a request.
	  Tip: use `--mine` or `--actor-id <id>` to audit one actor; add `--full-id` (debug/admin) for copy/paste IDs.
	  Raw `events create` is a contract escape hatch. For ordinary discussion, use `anx topics message topic:<handle>`, `anx docs message doc:<handle>`, or `anx cards message card:<handle>` instead of hand-authoring a `message_posted` event.
  For details: `anx debug events explain <event-type>`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events ... ; anx --json debug events ... ; anx debug events ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `inbox`

Read your asks and process human attention inbox items

```text
Generated Help: inbox

Commands:
  inbox get                Get one inbox item
  inbox list               List inbox items
  inbox respond            Respond to human attention inbox item
  inbox stream             Stream inbox items (SSE)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox ... ; anx --json inbox ... ; anx inbox ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `artifacts`

Manage artifact resources and content

```text
Generated Help: artifacts

Commands:
  artifacts archive        Archive artifact
  artifacts content        Download artifact bytes
  artifacts create         Create artifact
  artifacts list           List artifacts
  artifacts purge          Permanently delete trashed artifact
  artifacts restore        Restore artifact from trash
  artifacts trash          Move artifact to trash
  artifacts unarchive      Unarchive artifact

Common attachment flow:
  artifacts create --file <path> --ref <typed-ref>
                            Upload a file attachment with repeatable typed refs.
  artifacts content <id>   With no --output, streams raw artifact bytes to stdout.
  artifacts content <id> --output <path>
                            Download raw artifact bytes to a file (`--output -` is an explicit alias for stdout).
  artifacts content <id> --output .
                            Download using the server-provided filename.
  artifacts download       Alias for raw byte download (same as artifacts content).

Aliases and inspection:
  artifacts get            Compatibility alias for artifacts inspect.
  artifacts inspect        Fetch artifact metadata and content in one call.

Lower-level helpers:
  artifacts attachments create
                            Explicit multipart attachment upload path.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts ... ; anx --json artifacts ... ; anx artifacts ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `actors`

Diagnostic actor inventory and fixture helpers

```text
Generated Help: actors

Commands:
  debug actors create      Create actor (dev fixture)
  debug actors list        List actors

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug actors ... ; anx --json debug actors ... ; anx debug actors ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `ref-edges`

Diagnostic typed-ref edge inspection

```text
Generated Help: ref-edges

Commands:
  debug ref-edges list     List ref edges (forward or reverse indexed lookup)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug ref-edges ... ; anx --json debug ref-edges ... ; anx debug ref-edges ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `derived`

Run derived-view maintenance actions

```text
Derived maintenance surface

Use this group to refresh or inspect derived views that are computed from canonical state.

Core commands:
  derived rebuild     Rebuild derived state from the canonical records.

Tip: derived commands are operational helpers, not the source of truth.
```

## `meta`

Inspect generated command/concept metadata

```text
Metadata and shipped reference surface

Use this group to inspect CLI/runtime metadata and to print the bundled runtime reference docs.

Core commands:
  meta health     Inspect overall CLI/runtime health.
  meta livez      Check liveness.
  meta readyz     Check readiness.
  meta version    Print version information.

Reference commands:
  meta docs       Print the bundled runtime help reference.
  meta doc        Print one bundled runtime help topic.
  meta skill      Render the bundled participant or PM skill.
  meta commands   Inspect generated command metadata.
  meta concepts   Inspect generated concepts metadata.
  meta ops        Inspect operational metadata helpers.
```

## `runs list`

List launcher runs

```text
Generated Help: runs list

- Command ID: `runs.list`
- CLI path: `runs list`
- HTTP: `GET /runs`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: See current and recent agent executions.
- Output: Returns `{ runs, next_cursor? }`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`
- Concepts: `runs`, `agents`, `cards`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `runs get`, `runs ingest`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx runs list ... ; anx --json runs list ... ; anx runs list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `runs get`

Get one run

```text
Generated Help: runs get

- Command ID: `runs.get`
- CLI path: `runs get`
- HTTP: `GET /runs/{run_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect execution attribution and outcome.
- Output: Returns `{ run }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `runs`, `agents`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `runs ingest`, `runs list`

Inputs:
  Required:
  - path `run_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx runs get ... ; anx --json runs get ... ; anx runs get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host list`

List workspace hosts

```text
Generated Help: host list

- Command ID: `hosts.list`
- CLI path: `host list`
- HTTP: `GET /hosts`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect enrolled machines.
- Output: Returns `{ hosts }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `hosts`, `agents`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `host bridge check-in`, `host enroll complete`, `host enroll headless`, `host enroll poll`, `host enroll start`, `host enrollments approve`, `host enrollments deny`, `host enrollments list`, `host get`, `host patch`, `host revoke`, `host tokens create`, `host tokens list`, `host tokens revoke`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host list ... ; anx --json host list ... ; anx host list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth invites list`

List invite tokens

```text
Generated Help: auth invites list

- Command ID: `auth.invites.list`
- CLI path: `auth invites list`
- HTTP: `GET /auth/invites`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Operator listing of outstanding invites.
- Output: Returns `{ invites }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `auth`
- Adjacent commands: `auth admins grant`, `auth admins list`, `auth admins revoke`, `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth invites list ... ; anx --json auth invites list ... ; anx auth invites list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth invites create`

Create invite token

```text
Generated Help: auth invites create

- Command ID: `auth.invites.create`
- CLI path: `auth invites create`
- HTTP: `POST /auth/invites`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Issue a one-time invite for a human principal; kind must be human.
- Output: Returns `{ invite, token }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `human_required`
- Concepts: `auth`
- Adjacent commands: `auth admins grant`, `auth admins list`, `auth admins revoke`, `auth audit list`, `auth bootstrap status`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`

Inputs:
  Required:
  - body `kind` (string)
  Optional:
  - body `expires_at` (datetime)
  Enum values: kind: human

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth invites create ... ; anx --json auth invites create ... ; anx auth invites create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth invites revoke`

Revoke invite

```text
Generated Help: auth invites revoke

- Command ID: `auth.invites.revoke`
- CLI path: `auth invites revoke`
- HTTP: `POST /auth/invites/{invite_id}/revoke`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Invalidate an outstanding invite by id.
- Output: Returns `{ invite }`.
- Error codes: `human_required`, `auth_required`, `invalid_request`, `not_found`, `invalid_token`
- Concepts: `auth`
- Adjacent commands: `auth admins grant`, `auth admins list`, `auth admins revoke`, `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`

Inputs:
  Required:
  - path `invite_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth invites revoke ... ; anx --json auth invites revoke ... ; anx auth invites revoke ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth bootstrap status`

Bootstrap registration availability

```text
Generated Help: auth bootstrap status

- Command ID: `auth.bootstrap.status`
- CLI path: `auth bootstrap status`
- HTTP: `GET /auth/bootstrap/status`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Report whether first-human passkey bootstrap registration is still available; hosts cannot bootstrap.
- Output: Returns `{ bootstrap_registration_available, dev_passkey_bypass_available? }`, where the dev bypass field reflects the effective local-only passkey bypass capability.
- Concepts: `auth`
- Adjacent commands: `auth admins grant`, `auth admins list`, `auth admins revoke`, `auth audit list`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth bootstrap status ... ; anx --json auth bootstrap status ... ; anx auth bootstrap status ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth principals list`

List principals

```text
Generated Help: auth principals list

- Command ID: `auth.principals.list`
- CLI path: `auth principals list`
- HTTP: `GET /auth/principals`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Operator visibility into registered principals for the workspace.
- Output: Returns principal list JSON.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `auth`
- Adjacent commands: `auth admins grant`, `auth admins list`, `auth admins revoke`, `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals revoke`, `auth token`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth principals list ... ; anx --json auth principals list ... ; anx auth principals list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth principals revoke`

Revoke principal by agent id

```text
Generated Help: auth principals revoke

- Command ID: `auth.principals.revoke`
- CLI path: `auth principals revoke`
- HTTP: `POST /auth/principals/{principal_id}/revoke`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Administrative revocation of a principal linkage.
- Output: Returns result JSON.
- Error codes: `human_required`, `auth_required`, `invalid_request`, `not_found`, `invalid_token`, `conflict`
- Concepts: `auth`
- Adjacent commands: `auth admins grant`, `auth admins list`, `auth admins revoke`, `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth token`

Inputs:
  Required:
  - path `principal_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth principals revoke ... ; anx --json auth principals revoke ... ; anx auth principals revoke ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth audit list`

List auth audit entries

```text
Generated Help: auth audit list

- Command ID: `auth.audit.list`
- CLI path: `auth audit list`
- HTTP: `GET /auth/audit`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Operator audit trail for auth-sensitive actions.
- Output: Returns audit list JSON.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `auth`, `audit`
- Adjacent commands: `auth admins grant`, `auth admins list`, `auth admins revoke`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth audit list ... ; anx --json auth audit list ... ; anx auth audit list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `actors list`

List actors

```text
Generated Help: actors list

- Command ID: `actors.list`
- CLI path: `debug actors list`
- HTTP: `GET /actors`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Enumerate durable actor records for operator UI and dev fixtures.
- Output: Returns `{ actors, next_cursor? }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `actors`, `auth`
- Adjacent commands: `debug actors create`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug actors list ... ; anx --json debug actors list ... ; anx debug actors list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `actors create`

Create actor (dev fixture)

```text
Generated Help: actors create

- Command ID: `actors.create`
- CLI path: `debug actors create`
- HTTP: `POST /actors`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Dev-only actor registration when dev_actor_mode is enabled.
- Output: Returns `{ actor }`.
- Error codes: `auth_required`, `invalid_request`, `dev_actor_mode_disabled`
- Concepts: `actors`, `auth`
- Adjacent commands: `debug actors list`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug actors create ... ; anx --json debug actors create ... ; anx debug actors create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics list`

List topics

```text
Generated Help: topics list

- Command ID: `topics.list`
- CLI path: `topics list`
- HTTP: `GET /topics`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Scan the durable topic inventory.
- Output: Returns `{ topics }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `topics`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics list ... ; anx --json topics list ... ; anx topics list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics get`

Get topic

```text
Generated Help: topics get

- Command ID: `topics.get`
- CLI path: `topics get`
- HTTP: `GET /topics/{topic_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one topic and its canonical durable fields.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `topics`
- Adjacent commands: `topics archive`, `topics create`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics get ... ; anx --json topics get ... ; anx topics get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics timeline`

Get topic timeline

```text
Generated Help: topics timeline

- Command ID: `topics.timeline`
- CLI path: `topics timeline`
- HTTP: `GET /topics/{topic_id}/timeline`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Load chronological evidence and related resources for one topic.
- Output: Returns `{ topic, events, artifacts, cards, documents, threads, notification_receipts }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `topics`, `timeline`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics timeline ... ; anx --json topics timeline ... ; anx topics timeline ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics workspace`

Get topic workspace (agent-facing discussion/context primitive)

```text
Generated Help: topics workspace

- Command ID: `topics.workspace`
- CLI path: `topics workspace`
- HTTP: `GET /topics/{topic_id}/workspace`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Agent-facing discussion/context primitive. Load the topic workspace composed from linked cards, docs, backing threads, and inbox items. The operator work projection is `work.list` / `work.get`.
- Output: Returns `{ topic, cards, boards, documents, threads, inbox, projection_freshness, generated_at }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `topics`, `workspace`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`

Inputs:
  Required:
  - path `topic_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics workspace ... ; anx --json topics workspace ... ; anx topics workspace ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics archive`

Archive topic

```text
Generated Help: topics archive

- Command ID: `topics.archive`
- CLI path: `topics archive`
- HTTP: `POST /topics/{topic_id}/archive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Soft-archive a topic and derive its lifecycle state from archived_at.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`
- Adjacent commands: `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

CLI input:
  - JSON body is optional; `--from-file` remains available for advanced request bodies.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics archive ... ; anx --json topics archive ... ; anx topics archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics unarchive`

Unarchive topic

```text
Generated Help: topics unarchive

- Command ID: `topics.unarchive`
- CLI path: `topics unarchive`
- HTTP: `POST /topics/{topic_id}/unarchive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Clear archived_at on a topic (restore default list visibility).
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

CLI input:
  - JSON body is optional; `--from-file` remains available for advanced request bodies.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics unarchive ... ; anx --json topics unarchive ... ; anx topics unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics restore`

Restore topic from trash

```text
Generated Help: topics restore

- Command ID: `topics.restore`
- CLI path: `topics restore`
- HTTP: `POST /topics/{topic_id}/restore`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Clear trash lifecycle fields on a topic after an explicit restore action.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

CLI input:
  - JSON body is optional; `--from-file` remains available for advanced request bodies.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics restore ... ; anx --json topics restore ... ; anx topics restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards list`

List boards

```text
Generated Help: boards list

- Command ID: `boards.list`
- CLI path: `boards list`
- HTTP: `GET /boards`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Scan durable coordination boards and lightweight summaries.
- Output: Returns `{ boards, next_cursor? }` (each `boards[]` item is `{ board, summary }` with `summary` a `BoardSummary` projection, not the board's text blurb).
- Error codes: `auth_required`, `invalid_token`
- Concepts: `boards`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards list ... ; anx --json boards list ... ; anx boards list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards get`

Get board

```text
Generated Help: boards get

- Command ID: `boards.get`
- CLI path: `boards get`
- HTTP: `GET /boards/{board_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve canonical board state and summary.
- Output: Returns `{ board, summary }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `boards`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards get ... ; anx --json boards get ... ; anx boards get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards patch`

Patch board

```text
Generated Help: boards patch

- Command ID: `boards.patch`
- CLI path: `boards patch`
- HTTP: `PATCH /boards/{board_id}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Update board metadata with optimistic concurrency.
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`, `concurrency`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.
  Optional:
  - body `patch.document_refs` (list<any>)
  - body `patch.pinned_refs` (list<any>)
  - body `patch.primary_topic_ref` (string)
  - body `patch.provenance.by_field` (object)
  - body `patch.provenance.notes` (string)
  - body `patch.provenance.sources` (list<string>)
  - body `patch.summary` (string)
  - body `patch.title` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards patch ... ; anx --json boards patch ... ; anx boards patch ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards archive`

Archive board

```text
Generated Help: boards archive

- Command ID: `boards.archive`
- CLI path: `boards archive`
- HTTP: `POST /boards/{board_id}/archive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Soft-archive a board and derive its lifecycle state from archived_at.
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards archive ... ; anx --json boards archive ... ; anx boards archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards unarchive`

Unarchive board

```text
Generated Help: boards unarchive

- Command ID: `boards.unarchive`
- CLI path: `boards unarchive`
- HTTP: `POST /boards/{board_id}/unarchive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archived_at on a board (restore default list visibility).
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards unarchive ... ; anx --json boards unarchive ... ; anx boards unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards trash`

Move board to trash

```text
Generated Help: boards trash

- Command ID: `boards.trash`
- CLI path: `boards trash`
- HTTP: `POST /boards/{board_id}/trash`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move board to trash with an explicit operator reason.
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards trash ... ; anx --json boards trash ... ; anx boards trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards restore`

Restore board from trash

```text
Generated Help: boards restore

- Command ID: `boards.restore`
- CLI path: `boards restore`
- HTTP: `POST /boards/{board_id}/restore`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear trash lifecycle fields on a board after an explicit restore action.
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards restore ... ; anx --json boards restore ... ; anx boards restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards purge`

Permanently delete trashed board

```text
Generated Help: boards purge

- Command ID: `boards.purge`
- CLI path: `boards purge`
- HTTP: `POST /boards/{board_id}/purge`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Permanently delete a trashed board (human-gated).
- Output: Returns `{ purged, board_ref, board_handle }`; internal board_id may appear for admin/debug compatibility.
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards purge ... ; anx --json boards purge ... ; anx boards purge ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards`

Nested generated help topic.

```text
Generated Help: boards cards

Commands:
  boards cards create-batch Batch create cards on board
  boards cards get         Get board-scoped card
  boards cards list        List board cards

Board-scoped card read and batch surface:
  boards cards list          List cards on one board.
  boards cards get           Get a card through its board context.
  boards cards create-batch  Create many cards on one board from JSON.

Canonical card workflow:
  Use `anx cards` for normal card operations. Cards are first-class work items, not subordinate board commands.
  - `anx cards list --board <board-ref>` lists one board's cards.
  - `anx cards create --board <board-ref>` creates one card.
  - `anx cards get|message|assign|move|revise|resolve|reopen|archive|trash|restore|purge` operate on the card ref directly.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards cards ... ; anx --json boards cards ... ; anx boards cards ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `boards cards create-batch`

Batch create cards on board

```text
Generated Help: boards cards create-batch

- Command ID: `boards.cards.batch_add`
- CLI path: `boards cards create-batch`
- HTTP: `POST /boards/{board_id}/cards/batch`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create multiple cards in one transaction using a single board concurrency token.
- Output: Returns `{ board, cards }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `boards`, `cards`, `write`
- Adjacent commands: `boards archive`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - body `items` (list<any>)
  Optional:
  - body `actor_id` (string): Defaults from the resolved derived agent when omitted. Non-empty `--actor-id` overrides `actor_id` in the JSON body.
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get <board-ref-or-handle>`, `anx boards workspace <board-ref-or-handle>`, or the latest board mutation response. You may pass `--if-board-updated-at` instead of embedding it in JSON.
  - body `request_key` (string): Idempotency key for the whole batch. Non-empty `--request-key` overrides `request_key` in the JSON body.

CLI input:
  - Provide a JSON object on stdin or via `--from-file`; it must include `items` (array of card create payloads).
  - Board target: a single positional `<board-ref-or-handle>` before flags (preferred), or `--board-id <board-ref-or-handle>` for compatibility.
  - `actor_id` defaults from the resolved agent when omitted from JSON; `--actor-id` sets or overrides it.
  - `--request-key` and `--if-board-updated-at`, when non-empty, override the same keys in the JSON body.

Agent tip: run `anx boards get <board-ref-or-handle> --json` (or `boards workspace`) first, copy `board.updated_at` into `if_board_updated_at`, or pass `--if-board-updated-at` from that value. Each item's `related_refs` must reference source threads not already backing another card on this board, or the server returns `conflict`.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards cards create-batch ... ; anx --json boards cards create-batch ... ; anx boards cards create-batch ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards get`

Get board-scoped card

```text
Generated Help: boards cards get

- Command ID: `boards.cards.get`
- CLI path: `boards cards get`
- HTTP: `GET /boards/{board_id}/cards/{card_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve a card through its board membership context.
- Output: Returns `{ card }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `boards`, `cards`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`
  - path `card_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards cards get ... ; anx --json boards cards get ... ; anx boards cards get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs list`

List documents

```text
Generated Help: docs list

- Command ID: `docs.list`
- CLI path: `docs list`
- HTTP: `GET /docs`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Scan canonical document lineages.
- Output: Returns `{ documents }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `docs`
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - List knowledge docs: `anx docs list --knowledge`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs list ... ; anx --json docs list ... ; anx docs list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs history`

List document revisions

```text
Generated Help: docs history

- Command ID: `docs.revisions.list`
- CLI path: `docs history`
- HTTP: `GET /docs/{document_id}/revisions`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Enumerate immutable revisions for one document lineage.
- Output: Returns `{ document_ref, document_handle, revisions }`; internal document_id may appear for admin/debug compatibility.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `docs`, `revisions`
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - List revision history: `anx docs history doc:runbook`

Inputs:
  Required:
  - path `document_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs history ... ; anx --json docs history ... ; anx docs history ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs revision`

Nested generated help topic.

```text
Generated Help: docs revision

Commands:
  docs revision get        Get document revision

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs revision ... ; anx --json docs revision ... ; anx docs revision ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `docs archive`

Archive document

```text
Generated Help: docs archive

- Command ID: `docs.archive`
- CLI path: `docs archive`
- HTTP: `POST /docs/{document_id}/archive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Soft-archive a document lineage (orthogonal to head revision content).
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Archive a document: `anx docs archive doc:runbook --reason "superseded"`

Inputs:
  Required:
  - path `document_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs archive ... ; anx --json docs archive ... ; anx docs archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs unarchive`

Unarchive document

```text
Generated Help: docs unarchive

- Command ID: `docs.unarchive`
- CLI path: `docs unarchive`
- HTTP: `POST /docs/{document_id}/unarchive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archived_at on a document so it returns to default visibility.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`
- Examples:
  - Unarchive a document: `anx docs unarchive doc:runbook`

Inputs:
  Required:
  - path `document_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs unarchive ... ; anx --json docs unarchive ... ; anx docs unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs restore`

Restore document from trash

```text
Generated Help: docs restore

- Command ID: `docs.restore`
- CLI path: `docs restore`
- HTTP: `POST /docs/{document_id}/restore`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear trash state on a document after an explicit restore action.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Restore a trashed document: `anx docs restore doc:runbook`

Inputs:
  Required:
  - path `document_id`
  Optional:
  - body `actor_id` (string)
  - body `reason` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs restore ... ; anx --json docs restore ... ; anx docs restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs purge`

Permanently delete trashed document

```text
Generated Help: docs purge

- Command ID: `docs.purge`
- CLI path: `docs purge`
- HTTP: `POST /docs/{document_id}/purge`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Permanently delete a trashed document (human-gated).
- Output: Returns `{ purged, document_ref, document_handle }`; internal document_id may appear for admin/debug compatibility.
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Purge a trashed document: `anx docs purge doc:runbook`

Inputs:
  Required:
  - path `document_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs purge ... ; anx --json docs purge ... ; anx docs purge ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs revision get`

Get document revision

```text
Generated Help: docs revision get

- Command ID: `docs.revisions.get`
- CLI path: `docs revision get`
- HTTP: `GET /docs/{document_id}/revisions/{revision_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one immutable document revision.
- Output: Returns `{ document_ref, document_handle, revision }`; internal document_id may appear for admin/debug compatibility.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `docs`, `revisions`
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs search`, `docs trash`, `docs unarchive`

Inputs:
  Required:
  - path `document_id`
  - path `revision_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs revision get ... ; anx --json docs revision get ... ; anx docs revision get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards get`

Get card

```text
Generated Help: cards get

- Command ID: `cards.get`
- CLI path: `cards get`
- HTTP: `GET /cards/{card_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one first-class card by public ref or handle.
- Output: Returns `{ card }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `cards`
- Adjacent commands: `cards archive`, `cards create`, `cards history`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards revise`, `cards revision get`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards get ... ; anx --json cards get ... ; anx cards get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards history`

List card revisions

```text
Generated Help: cards history

- Command ID: `cards.revisions.list`
- CLI path: `cards history`
- HTTP: `GET /cards/{card_id}/revisions`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Enumerate immutable content revisions for one card lineage.
- Output: Returns `{ card_ref, card_handle, revisions }`; internal card_id may appear for admin/debug compatibility.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `cards`, `revisions`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards revise`, `cards revision get`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards history ... ; anx --json cards history ... ; anx cards history ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards archive`

Archive card

```text
Generated Help: cards archive

- Command ID: `cards.archive`
- CLI path: `cards archive`
- HTTP: `POST /cards/{card_id}/archive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Soft-delete a first-class card by setting archived_at (board concurrency via if_board_updated_at).
- Output: Returns `{ board, card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`, `already_trashed`
- Concepts: `cards`, `write`
- Adjacent commands: `cards create`, `cards get`, `cards history`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards revise`, `cards revision get`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  Optional:
  - body `actor_id` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get <board-ref-or-handle>`, `anx boards workspace <board-ref-or-handle>`, or the latest board mutation response.
  - body `if_latest_observation_id` (string): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.
  - body `if_version` (integer): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards archive ... ; anx --json cards archive ... ; anx cards archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards purge`

Permanently delete archived or trashed card

```text
Generated Help: cards purge

- Command ID: `cards.purge`
- CLI path: `cards purge`
- HTTP: `POST /cards/{card_id}/purge`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Permanently delete an archived or trashed card (human-gated).
- Output: Returns `{ purged, card_ref, card_handle }`; internal card_id may appear for admin/debug compatibility.
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `write`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards history`, `cards list`, `cards move`, `cards patch`, `cards restore`, `cards revise`, `cards revision get`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards purge ... ; anx --json cards purge ... ; anx cards purge ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards restore`

Restore archived or trashed card

```text
Generated Help: cards restore

- Command ID: `cards.restore`
- CLI path: `cards restore`
- HTTP: `POST /cards/{card_id}/restore`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archive or trash lifecycle fields on a card so it reappears on boards.
- Output: Returns `{ board, card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `write`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards history`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards revise`, `cards revision get`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  Optional:
  - body `actor_id` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get <board-ref-or-handle>`, `anx boards workspace <board-ref-or-handle>`, or the latest board mutation response.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards restore ... ; anx --json cards restore ... ; anx cards restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards timeline`

Get card timeline

```text
Generated Help: cards timeline

- Command ID: `cards.timeline`
- CLI path: `cards timeline`
- HTTP: `GET /cards/{card_id}/timeline`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Load chronological evidence and related resources for one card.
- Output: Returns `{ card, events, artifacts, cards, documents, threads }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `cards`, `timeline`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards history`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards revise`, `cards revision get`, `cards trash`

Inputs:
  Required:
  - path `card_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards timeline ... ; anx --json cards timeline ... ; anx cards timeline ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads list`

List backing threads

```text
Generated Help: threads list

- Command ID: `threads.list`
- CLI path: `debug threads list`
- HTTP: `GET /threads`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect backing infrastructure threads without making them the primary planning noun.
- Output: Returns `{ threads }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `threads`, `inspection`
- Adjacent commands: `debug threads context`, `debug threads inspect`, `debug threads timeline`, `debug threads workspace`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug threads list ... ; anx --json debug threads list ... ; anx debug threads list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads get`

Inspect backing thread

```text
Generated Help: threads get

- Command ID: `threads.inspect`
- CLI path: `debug threads inspect`
- HTTP: `GET /threads/{thread_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one backing thread for low-level inspection and diagnostics.
- Output: Returns `{ thread }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `threads`, `inspection`
- Adjacent commands: `debug threads context`, `debug threads list`, `debug threads timeline`, `debug threads workspace`

Inputs:
  Required:
  - path `thread_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug threads get ... ; anx --json debug threads get ... ; anx debug threads get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads timeline`

Get backing thread timeline

```text
Generated Help: threads timeline

- Command ID: `threads.timeline`
- CLI path: `debug threads timeline`
- HTTP: `GET /threads/{thread_id}/timeline`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Retrieve event history plus typed-ref expansions for one backing thread.
- Output: Returns `{ thread, events, artifacts, topics, cards, documents, notification_receipts }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `threads`, `timeline`
- Adjacent commands: `debug threads context`, `debug threads inspect`, `debug threads list`, `debug threads workspace`

Inputs:
  Required:
  - path `thread_id`

Local CLI flags:
  --include-archived        Include archived events in the timeline.
  --archived-only           Show only archived events.
  --include-trashed      Include trashed events in the timeline.
  --trashed-only         Show only trashed events in the timeline.

Note: by default, archived and trashed events are excluded from the timeline output.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug threads timeline ... ; anx --json debug threads timeline ... ; anx debug threads timeline ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads context`

Get backing thread coordination context

```text
Generated Help: threads context

- Command ID: `threads.context`
- CLI path: `debug threads context`
- HTTP: `GET /threads/{thread_id}/context`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Load a compact coordination bundle (thread, recent events, key artifacts, cards, documents) for inspection and triage.
- Output: Returns `{ thread, recent_events, key_artifacts, open_cards, documents }` plus forward-compatible fields.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `threads`, `inspection`
- Adjacent commands: `debug threads inspect`, `debug threads list`, `debug threads timeline`, `debug threads workspace`

Inputs:
  Required:
  - path `thread_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug threads context ... ; anx --json debug threads context ... ; anx debug threads context ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events get`

Get event

```text
Generated Help: events get

- Command ID: `events.get`
- CLI path: `debug events get`
- HTTP: `GET /events/{event_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Fetch one append-only event record by public ref or handle.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `events`
- Adjacent commands: `debug events archive`, `debug events create`, `debug events list`, `debug events restore`, `debug events stream`, `debug events trash`, `debug events unarchive`

Inputs:
  Required:
  - path `event_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events get ... ; anx --json debug events get ... ; anx debug events get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events create`

Create event

```text
Generated Help: events create

- Command ID: `events.create`
- CLI path: `debug events create`
- HTTP: `POST /events`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Append an event that links first-class resources and evidence through typed refs.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `events`, `write`
- Adjacent commands: `debug events archive`, `debug events get`, `debug events list`, `debug events restore`, `debug events stream`, `debug events trash`, `debug events unarchive`

Inputs:
  Required:
  - body `event.actor_id` (string)
  - body `event.provenance.sources` (list<string>)
  - body `event.refs` (list<any>)
  - body `event.summary` (string)
  - body `event.type` (string)
  Optional:
  - body `event.handle` (string)
  - body `event.payload` (object)
  - body `event.provenance.by_field` (object)
  - body `event.provenance.notes` (string)
  - body `event.ref` (typed_ref)
  - body `event.thread_ref` (string)
  Enum values: event.type (strict): agent_notification_dismissed, agent_notification_read, board_created, board_updated, card_archived, card_created, card_moved, card_resolved, card_trashed, card_updated, document_created, document_restored, document_revised, document_trashed, exception_raised, human_attention_requested, human_attention_responded, human_attention_withdrawn, message_posted, receipt_added, review_completed, topic_archived, topic_created, topic_restored, topic_trashed, topic_updated

Common authoring types:
  Communication: direct communication or important non-structured information
  - `message_posted`
  Human attention: request, answer, or withdraw operator attention
  - `human_attention_requested`
  - `human_attention_responded`
  - `human_attention_withdrawn`
  Topics and documents: durable subject and document lifecycle signals
  - `topic_created`, `topic_updated`, `topic_archived`, `topic_trashed`
  - `document_created`, `document_revised`, `document_trashed`
  Boards and cards: workflow placement and movement
  - `board_created`, `board_updated`
  - `card_created`, `card_updated`, `card_moved`, `card_resolved`
  Exceptions: surface problems, risks, or escalations
  - `exception_raised`

Usually emitted by higher-level commands:
  - `human_attention_requested`: prefer `anx ask|review|escalate`
  - `human_attention_withdrawn`: prefer `anx ask withdraw`

Local CLI notes:
  - Prefer higher-level commands for topic, board, card, doc, and human-attention lifecycle writes.
  - Use `--dry-run` with `--from-file` to validate and preview the request without sending the mutation.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events create ... ; anx --json debug events create ... ; anx debug events create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events stream`

Stream events (SSE)

```text
Generated Help: events stream

- Command ID: `events.stream`
- CLI path: `debug events stream`
- HTTP: `GET /stream/events`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Long-lived SSE feed of workspace events with optional thread/type filters and Last-Event-ID resume.
- Output: Each SSE message is `event: …` with JSON data `{ "event": <event> }` (see core/docs/http-api.md).
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `events`
- Adjacent commands: `debug events archive`, `debug events create`, `debug events get`, `debug events list`, `debug events restore`, `debug events trash`, `debug events unarchive`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events stream ... ; anx --json debug events stream ... ; anx debug events stream ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events tail`

Stream events (SSE)

```text
Generated Help: events tail

- Command ID: `events.stream`
- CLI path: `debug events stream`
- HTTP: `GET /stream/events`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Long-lived SSE feed of workspace events with optional thread/type filters and Last-Event-ID resume.
- Output: Each SSE message is `event: …` with JSON data `{ "event": <event> }` (see core/docs/http-api.md).
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `events`
- Adjacent commands: `debug events archive`, `debug events create`, `debug events get`, `debug events list`, `debug events restore`, `debug events trash`, `debug events unarchive`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events tail ... ; anx --json debug events tail ... ; anx debug events tail ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events archive`

Archive event

```text
Generated Help: events archive

- Command ID: `events.archive`
- CLI path: `debug events archive`
- HTTP: `POST /events/{event_id}/archive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Set archived_at on an append-only event record for filtered views.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `events`, `write`
- Adjacent commands: `debug events create`, `debug events get`, `debug events list`, `debug events restore`, `debug events stream`, `debug events trash`, `debug events unarchive`

Inputs:
  Required:
  - path `event_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events archive ... ; anx --json debug events archive ... ; anx debug events archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events unarchive`

Unarchive event

```text
Generated Help: events unarchive

- Command ID: `events.unarchive`
- CLI path: `debug events unarchive`
- HTTP: `POST /events/{event_id}/unarchive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archived_at on an event.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `events`, `write`
- Adjacent commands: `debug events archive`, `debug events create`, `debug events get`, `debug events list`, `debug events restore`, `debug events stream`, `debug events trash`

Inputs:
  Required:
  - path `event_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events unarchive ... ; anx --json debug events unarchive ... ; anx debug events unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events trash`

Move event to trash

```text
Generated Help: events trash

- Command ID: `events.trash`
- CLI path: `debug events trash`
- HTTP: `POST /events/{event_id}/trash`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move event to trash with an explicit operator reason.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `events`, `write`
- Adjacent commands: `debug events archive`, `debug events create`, `debug events get`, `debug events list`, `debug events restore`, `debug events stream`, `debug events unarchive`

Inputs:
  Required:
  - path `event_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events trash ... ; anx --json debug events trash ... ; anx debug events trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events restore`

Restore event from trash

```text
Generated Help: events restore

- Command ID: `events.restore`
- CLI path: `debug events restore`
- HTTP: `POST /events/{event_id}/restore`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear trash state on an event after an explicit restore action.
- Output: Returns `{ event }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `events`, `write`
- Adjacent commands: `debug events archive`, `debug events create`, `debug events get`, `debug events list`, `debug events stream`, `debug events trash`, `debug events unarchive`

Inputs:
  Required:
  - path `event_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events restore ... ; anx --json debug events restore ... ; anx debug events restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox get`

Get one inbox item

```text
Generated Help: inbox get

- Command ID: `inbox.get`
- CLI path: `inbox get`
- HTTP: `GET /inbox/{inbox_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Side-effect free read of one materialized inbox row.
- Output: Returns `{ item, generated_at, projection_freshness }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `inbox`
- Adjacent commands: `inbox list`, `inbox respond`, `inbox stream`

Inputs:
  Required:
  - path `inbox_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox get ... ; anx --json inbox get ... ; anx inbox get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox respond`

Respond to human attention inbox item

```text
Generated Help: inbox respond

- Command ID: `inbox.respond`
- CLI path: `inbox respond`
- HTTP: `POST /inbox/{inbox_id}/respond`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: A human principal records one response per request, closes the human attention item, and optionally notifies the selected requester/replacement agent.
- Output: Returns `{ event, notify }`.
- Error codes: `auth_required`, `human_required`, `invalid_request`, `invalid_token`, `notification_target_required`, `not_found`, `conflict`, `idempotency_conflict`
- Concepts: `inbox`, `write`
- Adjacent commands: `inbox get`, `inbox list`, `inbox stream`

Inputs:
  Required:
  - path `inbox_id`
  - body `outcome` (string)
  - body `response_text` (string)
  Optional:
  - body `actor_id` (string)
  - body `idempotency_key` (string)
  - body `inbox_item_id` (string)
  - body `notify_mode` (string)
  - body `notify_target_actor_id` (string)
  - body `notify_target_agent_id` (string)
  - body `related_refs` (list<any>)
  Enum values: notify_mode: none, original, replacement; outcome: acknowledged, answered, approved, rejected

CLI flags (`inbox respond`):
  --inbox-item-id <id>    Inbox item id or list alias (see `debug inbox list`).
  --response-text <text>  Freeform response text.
  --outcome <value>       answered, approved, rejected, or acknowledged (required).
  --notify-mode <mode>    original, target, or none.
  --actor-id <id>         Actor id (`me` uses the resolved agent's actor when configured).
  --from-file <path>      JSON body file (API request shape).
  Positional: inbox item id when not given via `--inbox-item-id`.
  Otherwise: JSON object on stdin (`inbox_item_id`, `response_text`, `outcome`, optional fields).

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox respond ... ; anx --json inbox respond ... ; anx inbox respond ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox stream`

Stream inbox items (SSE)

```text
Generated Help: inbox stream

- Command ID: `inbox.stream`
- CLI path: `inbox stream`
- HTTP: `GET /stream/inbox`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Server-sent events feed of inbox projection updates.
- Output: SSE `inbox_item` events with JSON payloads.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `inbox`
- Adjacent commands: `inbox get`, `inbox list`, `inbox respond`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox stream ... ; anx --json inbox stream ... ; anx inbox stream ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox tail`

Stream inbox items (SSE)

```text
Generated Help: inbox tail

- Command ID: `inbox.stream`
- CLI path: `inbox stream`
- HTTP: `GET /stream/inbox`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Server-sent events feed of inbox projection updates.
- Output: SSE `inbox_item` events with JSON payloads.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `inbox`
- Adjacent commands: `inbox get`, `inbox list`, `inbox respond`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox tail ... ; anx --json inbox tail ... ; anx inbox tail ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts list`

List artifacts

```text
Generated Help: artifacts list

- Command ID: `artifacts.list`
- CLI path: `artifacts list`
- HTTP: `GET /artifacts`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Search and filter immutable artifacts across the workspace.
- Output: Returns `{ artifacts }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `artifacts`
- Adjacent commands: `artifacts archive`, `artifacts attachments create`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts list ... ; anx --json artifacts list ... ; anx artifacts list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts content`

Download artifact bytes

```text
Generated Help: artifacts content

- Command ID: `artifacts.content`
- CLI path: `artifacts content`
- HTTP: `GET /artifacts/{artifact_id}/content`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Return raw artifact bytes with accurate Content-Type, Content-Disposition, ETag, and Last-Modified for attachments.
- Output: Raw bytes or JSON/text depending on artifact.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `artifacts`
- Adjacent commands: `artifacts archive`, `artifacts attachments create`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts content ... ; anx --json artifacts content ... ; anx artifacts content ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts download`

Download artifact bytes

```text
Generated Help: artifacts download

- Command ID: `artifacts.content`
- CLI path: `artifacts content`
- HTTP: `GET /artifacts/{artifact_id}/content`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Return raw artifact bytes with accurate Content-Type, Content-Disposition, ETag, and Last-Modified for attachments.
- Output: Raw bytes or JSON/text depending on artifact.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `artifacts`
- Adjacent commands: `artifacts archive`, `artifacts attachments create`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts download ... ; anx --json artifacts download ... ; anx artifacts download ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts attachments`

Nested generated help topic.

```text
Generated Help: artifacts attachments

Commands:
  artifacts attachments create Upload a file attachment

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts attachments ... ; anx --json artifacts attachments ... ; anx artifacts attachments ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `anx help <command path>` for full command-level generated details.
```

## `artifacts archive`

Archive artifact

```text
Generated Help: artifacts archive

- Command ID: `artifacts.archive`
- CLI path: `artifacts archive`
- HTTP: `POST /artifacts/{artifact_id}/archive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Set archived_at on artifact metadata (orthogonal to trash lifecycle).
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts attachments create`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts archive ... ; anx --json artifacts archive ... ; anx artifacts archive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts unarchive`

Unarchive artifact

```text
Generated Help: artifacts unarchive

- Command ID: `artifacts.unarchive`
- CLI path: `artifacts unarchive`
- HTTP: `POST /artifacts/{artifact_id}/unarchive`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear archived_at on artifact metadata.
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts attachments create`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`

Inputs:
  Required:
  - path `artifact_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts unarchive ... ; anx --json artifacts unarchive ... ; anx artifacts unarchive ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts trash`

Move artifact to trash

```text
Generated Help: artifacts trash

- Command ID: `artifacts.trash`
- CLI path: `artifacts trash`
- HTTP: `POST /artifacts/{artifact_id}/trash`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move artifact metadata to trash with an explicit operator reason.
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts attachments create`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts trash ... ; anx --json artifacts trash ... ; anx artifacts trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts restore`

Restore artifact from trash

```text
Generated Help: artifacts restore

- Command ID: `artifacts.restore`
- CLI path: `artifacts restore`
- HTTP: `POST /artifacts/{artifact_id}/restore`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Clear trash lifecycle fields on an artifact after an explicit restore action.
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts attachments create`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`
  Optional:
  - body `actor_id` (string)
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts restore ... ; anx --json artifacts restore ... ; anx artifacts restore ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts purge`

Permanently delete trashed artifact

```text
Generated Help: artifacts purge

- Command ID: `artifacts.purge`
- CLI path: `artifacts purge`
- HTTP: `POST /artifacts/{artifact_id}/purge`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Permanently delete a trashed artifact (human-gated).
- Output: Returns `{ purged, artifact_ref, artifact_handle }`; internal artifact_id may appear for admin/debug compatibility.
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts attachments create`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - path `artifact_id`
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts purge ... ; anx --json artifacts purge ... ; anx artifacts purge ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `ref-edges list`

List ref edges (forward or reverse indexed lookup)

```text
Generated Help: ref-edges list

- Command ID: `ref_edges.list`
- CLI path: `debug ref-edges list`
- HTTP: `GET /ref-edges`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `query`
- Why: Query the write-through ref index by source or target typed ref (mutually exclusive); reverse lookup uses target_ref.
- Output: Returns `{ ref_edges }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `refs`, `inspection`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug ref-edges list ... ; anx --json debug ref-edges list ... ; anx debug ref-edges list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `derived rebuild`

Rebuild derived projections

```text
Generated Help: derived rebuild

- Command ID: `derived.rebuild`
- CLI path: `debug derived rebuild`
- HTTP: `POST /derived/rebuild`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Deterministic operator repair for inbox/thread projections.
- Output: Returns `{ ok: true }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `projections`, `maintenance`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug derived rebuild ... ; anx --json debug derived rebuild ... ; anx debug derived rebuild ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta commands`

List command registry metadata

```text
Generated Help: meta commands

- Command ID: `meta.commands.list`
- CLI path: `debug meta commands`
- HTTP: `GET /meta/commands`
- Side effect class: `read_only`
- Stability: `stable`
- Input mode: `none`
- Why: Expose embedded Agent Nexus command metadata for discovery and codegen parity.
- Output: Returns generated command registry JSON.
- Error codes: `meta_unavailable`
- Concepts: `compatibility`
- Adjacent commands: `debug meta command`, `debug meta concept`, `debug meta concepts`, `debug meta handshake`, `debug meta health`, `debug meta livez`, `debug meta readyz`, `debug meta version`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug meta commands ... ; anx --json debug meta commands ... ; anx debug meta commands ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta command`

Get one command metadata entry

```text
Generated Help: meta command

- Command ID: `meta.commands.get`
- CLI path: `debug meta command`
- HTTP: `GET /meta/commands/{command_id}`
- Side effect class: `read_only`
- Stability: `stable`
- Input mode: `none`
- Why: Resolve command metadata by stable command id.
- Output: Returns `{ command }`.
- Error codes: `meta_unavailable`, `not_found`
- Concepts: `compatibility`
- Adjacent commands: `debug meta commands`, `debug meta concept`, `debug meta concepts`, `debug meta handshake`, `debug meta health`, `debug meta livez`, `debug meta readyz`, `debug meta version`

Inputs:
  Required:
  - path `command_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug meta command ... ; anx --json debug meta command ... ; anx debug meta command ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta concepts`

List concept index

```text
Generated Help: meta concepts

- Command ID: `meta.concepts.list`
- CLI path: `debug meta concepts`
- HTTP: `GET /meta/concepts`
- Side effect class: `read_only`
- Stability: `stable`
- Input mode: `none`
- Why: Group command metadata by concept tags.
- Output: Returns `{ concepts: [...] }`.
- Error codes: `meta_unavailable`
- Concepts: `compatibility`
- Adjacent commands: `debug meta command`, `debug meta commands`, `debug meta concept`, `debug meta handshake`, `debug meta health`, `debug meta livez`, `debug meta readyz`, `debug meta version`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug meta concepts ... ; anx --json debug meta concepts ... ; anx debug meta concepts ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta concept`

Get commands grouped by concept

```text
Generated Help: meta concept

- Command ID: `meta.concepts.get`
- CLI path: `debug meta concept`
- HTTP: `GET /meta/concepts/{concept_name}`
- Side effect class: `read_only`
- Stability: `stable`
- Input mode: `none`
- Why: Expand one concept into related commands.
- Output: Returns `{ concept: {...} }`.
- Error codes: `meta_unavailable`, `not_found`
- Concepts: `compatibility`
- Adjacent commands: `debug meta command`, `debug meta commands`, `debug meta concepts`, `debug meta handshake`, `debug meta health`, `debug meta livez`, `debug meta readyz`, `debug meta version`

Inputs:
  Required:
  - path `concept_name`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug meta concept ... ; anx --json debug meta concept ... ; anx debug meta concept ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `pm context`

Read bounded authorized PM context; partial coverage stays explicit.

```text
Generated Help: pm context

- Command ID: `pm.context`
- CLI path: `pm context`
- HTTP: `GET /pm/context`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read bounded authorized PM context.
- Output: Returns `PMContextResponse`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read bounded authorized PM context; partial coverage stays explicit.

Usage: anx pm context
  --work-ref <value>
  --query <value>
  --limit <value>
  --cursor <value>

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm actions acknowledge`

Acknowledge a failed or unresolvable action.

```text
Generated Help: pm actions acknowledge

- Command ID: `pm.actions.acknowledge`
- CLI path: `pm actions acknowledge`
- HTTP: `POST /pm/actions/{action_id}/acknowledge`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Acknowledge a failed, unresolvable, or undeliverable action.
- Output: Returns `PMAction`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `action_id`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Acknowledge a failed or unresolvable action.

Usage: anx pm actions acknowledge <ref> (or --action-id <ref>)

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm actions get`

Read authorization, attempts and receipt; source_reported is not verified.

```text
Generated Help: pm actions get

- Command ID: `pm.actions.get`
- CLI path: `pm actions get`
- HTTP: `GET /pm/actions/{action_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read an action and its receipts.
- Output: Returns `PMAction`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `action_id`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read authorization, attempts and receipt; source_reported is not verified.

Usage: anx pm actions get <ref> (or --action-id <ref>)

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm actions list`

Report durable action and receipt statuses with principal-bound pagination.

```text
Generated Help: pm actions list

- Command ID: `pm.actions.list`
- CLI path: `pm actions list`
- HTTP: `GET /pm/actions`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List action receipts and attempts.
- Output: Returns `PMActionListResponse`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Report durable action and receipt statuses with principal-bound pagination.

Usage: anx pm actions list
  --limit <value>
  --cursor <value>

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm actions reconcile`

Request authoritative read-back of an action receipt; does not resend the action.

```text
Generated Help: pm actions reconcile

- Command ID: `pm.actions.reconcile`
- CLI path: `pm actions reconcile`
- HTTP: `POST /pm/actions/{action_id}/reconcile`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Read back an action outcome without resending.
- Output: Returns `PMAction`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `action_id`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Request authoritative read-back of an action receipt; does not resend the action.

Usage: anx pm actions reconcile <ref> (or --action-id <ref>)

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm bindings create`

Bind an exact channel identity (transport, tenant, channel, user) to a workspace principal; humans only.

```text
Generated Help: pm bindings create

- Command ID: `pm.bindings.create`
- CLI path: `pm bindings create`
- HTTP: `POST /pm/bindings`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Bind an exact channel identity to a workspace principal.
- Output: Returns `PMBinding`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Optional:
  - body `actor_id` (string)
  - body `can_approve` (boolean)
  - body `created_at` (string)
  - body `enabled` (boolean)
  - body `id` (string)
  - body `origin.channel_id` (string)
  - body `origin.external_user_id` (string)
  - body `origin.tenant_id` (string)
  - body `origin.thread_id` (string)
  - body `origin.transport` (string)
  - body `revision` (integer)
  - body `work_ref` (string)
  - body `workspace_id` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Bind an exact channel identity (transport, tenant, channel, user) to a workspace principal; humans only.

Usage: anx pm bindings create --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm bindings list`

List channel identity bindings for this workspace; an operator check, never a send.

```text
Generated Help: pm bindings list

- Command ID: `pm.bindings.list`
- CLI path: `pm bindings list`
- HTTP: `GET /pm/bindings`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Show which exact channel identities may talk to the PM, and with what authority, without sending anything.
- Output: Returns `PMBindingListResponse`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. A binding is an operator mapping, not proof that the channel is configured or reachable; `anx pm channels doctor` checks configuration without sending.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

List channel identity bindings for this workspace; an operator check, never a send.

Usage: anx pm bindings list
  --limit <value>
  --cursor <value>

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm conversations create`

Create a durable conversation using request_key, title and optional work_ref.

```text
Generated Help: pm conversations create

- Command ID: `pm.conversations.create`
- CLI path: `pm conversations create`
- HTTP: `POST /pm/conversations`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a durable PM conversation.
- Output: Returns `PMConversation`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - body `request_key` (string)
  - body `title` (string)
  Optional:
  - body `work_ref` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Create a durable conversation using request_key, title and optional work_ref.

Usage: anx pm conversations create --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm conversations get`

Read a conversation and its durable turns.

```text
Generated Help: pm conversations get

- Command ID: `pm.conversations.get`
- CLI path: `pm conversations get`
- HTTP: `GET /pm/conversations/{conversation_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read PM conversation and turns.
- Output: Returns `PMConversationDetailResponse`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `conversation_id`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read a conversation and its durable turns.

Usage: anx pm conversations get <ref> (or --conversation-id <ref>)

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm conversations list`

List durable PM conversations with principal-bound pagination.

```text
Generated Help: pm conversations list

- Command ID: `pm.conversations.list`
- CLI path: `pm conversations list`
- HTTP: `GET /pm/conversations`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List PM conversations.
- Output: Returns `PMConversationListResponse`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

List durable PM conversations with principal-bound pagination.

Usage: anx pm conversations list
  --limit <value>
  --cursor <value>

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm conversations message`

Queue a PM message using request_key and text; an accepted turn is not a completed outcome.

```text
Generated Help: pm conversations message

- Command ID: `pm.conversations.messages.create`
- CLI path: `pm conversations message`
- HTTP: `POST /pm/conversations/{conversation_id}/messages`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Queue a contextual PM turn.
- Output: Returns `PMTurn`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Channel ingress uses this same turn pipeline; Telegram and Discord messages become turns with `origin` set. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `conversation_id`
  - body `request_key` (string)
  - body `text` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Queue a PM message using request_key and text; an accepted turn is not a completed outcome.

Usage: anx pm conversations message <ref> (or --conversation-id <ref>) --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm decisions answer`

Answer with revision, approve and text; the server requires an authorized human principal.

```text
Generated Help: pm decisions answer

- Command ID: `pm.decisions.answer`
- CLI path: `pm decisions answer`
- HTTP: `POST /pm/decisions/{decision_id}/answer`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Answer and authorize a scoped decision.
- Output: Returns `PMDecision`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `decision_id`
  - body `approve` (boolean)
  - body `revision` (integer)
  - body `text` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Answer with revision, approve and text; the server requires an authorized human principal.

Usage: anx pm decisions answer <ref> (or --decision-id <ref>) --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm decisions create`

Propose an instruction bound to work, scope and target_revision; never approves it.

```text
Generated Help: pm decisions create

- Command ID: `pm.decisions.create`
- CLI path: `pm decisions create`
- HTTP: `POST /pm/decisions`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Propose a scoped PM decision.
- Output: Returns `PMDecision`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `human_proposal_pending`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - body `instruction` (string)
  - body `request_key` (string)
  - body `scope` (string)
  - body `target_revision` (string)
  - body `work_ref` (string)
  Optional:
  - body `payload.phase` (string)
  - body `payload.resolution_refs` (list<string>)
  Enum values: payload.phase: backlog, blocked, done, in_progress, ready, review

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Propose an instruction bound to work, scope and target_revision; never approves it.

Usage: anx pm decisions create --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm decisions dispatch`

Explicitly dispatch authorized intent; inspect action receipt for actual outcome.

```text
Generated Help: pm decisions dispatch

- Command ID: `pm.decisions.dispatch`
- CLI path: `pm decisions dispatch`
- HTTP: `POST /pm/decisions/{decision_id}/dispatch`
- Side effect class: `external_side_effect`
- Stability: `beta`
- Input mode: `json-body`
- Why: Hand off an authorized source action.
- Output: Returns `PMAction`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `decision_id`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Explicitly dispatch authorized intent; inspect action receipt for actual outcome.

Usage: anx pm decisions dispatch <ref> (or --decision-id <ref>)

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm decisions get`

Read an instruction, authorization scope, revision and answer status.

```text
Generated Help: pm decisions get

- Command ID: `pm.decisions.get`
- CLI path: `pm decisions get`
- HTTP: `GET /pm/decisions/{decision_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read a PM decision.
- Output: Returns `PMDecision`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `decision_id`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read an instruction, authorization scope, revision and answer status.

Usage: anx pm decisions get <ref> (or --decision-id <ref>)

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm decisions list`

List durable decisions with principal-bound pagination.

```text
Generated Help: pm decisions list

- Command ID: `pm.decisions.list`
- CLI path: `pm decisions list`
- HTTP: `GET /pm/decisions`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List durable PM decisions.
- Output: Returns `PMDecisionListResponse`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

List durable decisions with principal-bound pagination.

Usage: anx pm decisions list
  --limit <value>
  --cursor <value>

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm turns claim`

Claim the next queued turn with an exclusive runner lease. 204 means none.

```text
Generated Help: pm turns claim

- Command ID: `pm.turns.claim`
- CLI path: `pm turns claim`
- HTTP: `POST /pm/turns/claim`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Claim one queued turn for the selected PM agent so two runners never answer it.
- Output: Returns `PMClaimedTurn`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Selected PM agent only. Empty body is allowed. 204 means no waiting turn; 429 busy with reason capacity means waiting work is blocked by the lease cap and should be retried after one poll interval. Claims recover the same runner_id lease first, or allocate a fresh lease. Past-deadline open turns are expired to `failed` on reads, claims, and periodic maintenance. Lease expiry is bounded by the turn deadline and ANX_PM_LEASE_TTL (default 60s). Channel-origin turns use this same claim/complete/fail pipeline.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Optional:
  - body `runner_id` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Claim the next queued turn with an exclusive runner lease. 204 means none.

Usage: anx pm turns claim [--from-file <path|->] [--runner-id <id>]
  --runner-id <id> (defaults to the authenticated actor id; claims are idempotent for the same runner_id)

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm turns complete`

Selected PM agent records response text and evidence_refs; does not complete work.

```text
Generated Help: pm turns complete

- Command ID: `pm.turns.complete`
- CLI path: `pm turns complete`
- HTTP: `POST /pm/turns/{turn_id}/complete`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Record a selected PM agent response.
- Output: Returns `PMTurn`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `turn_closed`, `lease_required`, `lease_mismatch`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `turn_id`
  - body `lease_token` (string)
  - body `text` (string)
  Optional:
  - body `evidence_refs` (list<string>)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Selected PM agent records response text and evidence_refs; does not complete work.

Usage: anx pm turns complete <ref> (or --turn-id <ref>) --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm turns context`

Read context as the requesting actor; only the selected PM agent may call this.

```text
Generated Help: pm turns context

- Command ID: `pm.turns.context`
- CLI path: `pm turns context`
- HTTP: `POST /pm/turns/{turn_id}/context`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `json-body`
- Why: Read requesting principal context as selected PM agent.
- Output: Returns `PMContextResponse`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `turn_closed`, `lease_required`, `lease_mismatch`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `turn_id`
  - body `lease_token` (string)
  Optional:
  - body `cursor` (string)
  - body `limit` (integer)
  - body `query` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read context as the requesting actor; only the selected PM agent may call this.

Usage: anx pm turns context <ref> (or --turn-id <ref>) [--lease-token <token>]
  --query <value>
  --limit <value>
  --cursor <value>
  --lease-token <token> (or ANX_PM_LEASE_TOKEN from `anx pm serve`)

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm turns fail`

Mark a claimed turn failed with a reason; does not complete work.

```text
Generated Help: pm turns fail

- Command ID: `pm.turns.fail`
- CLI path: `pm turns fail`
- HTTP: `POST /pm/turns/{turn_id}/fail`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Record a selected PM agent failure reason without inventing a reply.
- Output: Returns `PMTurn`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `turn_closed`, `lease_required`, `lease_mismatch`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Selected PM agent only. An active lease is required and lease_token must match. Missing tokens return 409 lease_required; stale or expired lease tokens return 409 lease_mismatch and require claiming again. Identical terminal failure replays with the token that failed the turn return 200 without mutation, including after the deadline; a different reason returns 409 turn_closed; a stale or missing replay token returns 409 lease_mismatch explaining that the turn is already failed and no retry is needed.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `turn_id`
  - body `lease_token` (string)
  - body `reason` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Mark a claimed turn failed with a reason; does not complete work.

Usage: anx pm turns fail <ref> (or --turn-id <ref>) --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm turns get`

Read a PM conversation turn.

```text
Generated Help: pm turns get

- Command ID: `pm.turns.get`
- CLI path: `pm turns get`
- HTTP: `GET /pm/turns/{turn_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read a PM conversation turn.
- Output: Returns `PMTurn`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `source_revision_changed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Only the requesting conversation actor can read this turn. Past-deadline open turns are failed before returning.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `turn_id`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read a PM conversation turn.

Usage: anx pm turns get <ref> (or --turn-id <ref>)

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm turns heartbeat`

Lease owner renews a claimed turn's lease; renew at less than half the lease TTL.

```text
Generated Help: pm turns heartbeat

- Command ID: `pm.turns.heartbeat`
- CLI path: `pm turns heartbeat`
- HTTP: `POST /pm/turns/{turn_id}/heartbeat`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Keep an active runner lease alive during execution.
- Output: Returns `PMHeartbeatTurn`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `lease_mismatch`, `lease_required`, `turn_closed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Renew at a cadence strictly less than TTL/2; stop execution if ownership is lost.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns release`

Inputs:
  Required:
  - path `turn_id`
  - body `lease_token` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Lease owner renews a claimed turn's lease; renew at less than half the lease TTL.

Usage: anx pm turns heartbeat <ref> (or --turn-id <ref>) --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm turns propose`

Selected PM agent proposes an instruction for the requesting actor, never approval.

```text
Generated Help: pm turns propose

- Command ID: `pm.turns.decisions.create`
- CLI path: `pm turns propose`
- HTTP: `POST /pm/turns/{turn_id}/decisions`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Record a selected PM agent proposal.
- Output: Returns `PMDecision`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `human_proposal_pending`, `turn_closed`, `lease_required`, `lease_mismatch`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`, `pm turns release`

Inputs:
  Required:
  - path `turn_id`
  - body `instruction` (string)
  - body `lease_token` (string)
  - body `request_key` (string)
  - body `scope` (string)
  - body `target_revision` (string)
  - body `work_ref` (string)
  Optional:
  - body `payload.phase` (string)
  - body `payload.resolution_refs` (list<string>)
  Enum values: payload.phase: backlog, blocked, done, in_progress, ready, review

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Selected PM agent proposes an instruction for the requesting actor, never approval.

Usage: anx pm turns propose <ref> (or --turn-id <ref>) --from-file <path|-> [--lease-token <token>]
  --lease-token <token> (or ANX_PM_LEASE_TOKEN from `anx pm serve`)

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `pm turns release`

Lease owner returns a claimed turn to the queue.

```text
Generated Help: pm turns release

- Command ID: `pm.turns.release`
- CLI path: `pm turns release`
- HTTP: `POST /pm/turns/{turn_id}/release`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Return interrupted work to the queue for another claim.
- Output: Returns `PMTurn`.
- Error codes: `auth_required`, `invalid_token`, `invalid_request`, `forbidden`, `not_found`, `conflict`, `lease_mismatch`, `turn_not_claimed`, `turn_closed`, `busy`, `unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Selected PM agent only; runner_id and lease_token must match the unexpired lease owner.
- Adjacent commands: `pm actions acknowledge`, `pm actions get`, `pm actions list`, `pm actions reconcile`, `pm bindings create`, `pm bindings list`, `pm context`, `pm conversations create`, `pm conversations get`, `pm conversations list`, `pm conversations message`, `pm decisions answer`, `pm decisions create`, `pm decisions dispatch`, `pm decisions get`, `pm decisions list`, `pm turns claim`, `pm turns complete`, `pm turns context`, `pm turns propose`, `pm turns fail`, `pm turns get`, `pm turns heartbeat`

Inputs:
  Required:
  - path `turn_id`
  - body `lease_token` (string)
  - body `runner_id` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Lease owner returns a claimed turn to the queue.

Usage: anx pm turns release <ref> (or --turn-id <ref>) --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

PM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.

Use --json for one machine-readable envelope.
```

## `report render`

Materialize a visual report’s live panels from current, authorized workspace data.

```text
Generated Help: report render

- Command ID: `report.render`
- CLI path: `report render`
- HTTP: `GET /docs/{document_id}/report`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read the same live dashboard data shown to a workspace reader.
- Output: Returns `{ document_ref, revision_ref, observed_at, panels }`; static panels are omitted.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `not_found`, `invalid_request`, `unavailable`
- Concepts: `docs`, `cards`, `evidence`
- Agent notes: Read-only. Both text and structured version 1 visual reports are supported. Each live or series-bound panel is independently materialized with status ok, stale or unavailable, observation time, data and an explicit truncated flag. Never infer zero work from an unavailable or truncated panel. Queries are bounded to 2000 source rows. Archived boards and their work are excluded. Private PM events remain private.
- Adjacent commands: `report preview`

Inputs:
  Required:
  - path `document_id`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Materialize a visual report’s live panels from current, authorized workspace data.

Usage: anx report render <ref> (or --document-id <ref>)

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `sessions get`

Read your own registered provider session and bounded activity; does not expose conversation history.

```text
Generated Help: sessions get

- Command ID: `sessions.get`
- CLI path: `sessions get`
- HTTP: `GET /sessions/{session_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read your private native session without assigning, moving, or completing work.
- Output: Returns `SessionResponse`.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `sessions_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Existing bearer authentication is required, including in development mode. Registration and participation writes require an agent principal and never mint credentials. Sessions are private to that principal. Task reads expose only explicitly shared participation metadata; native session identifiers and other task links are never shared. Upserts refresh server-clock activity leases of 120 seconds. A session heartbeat does not refresh task participation. Session closure is terminal and never changes work state, assignees, source authority, or ownership. Session identity is independent of per-attempt /runs. Capabilities are caller-reported, not server-verified support.
- Adjacent commands: `sessions register`

Inputs:
  Required:
  - path `session_id`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read your own registered provider session and bounded activity; does not expose conversation history.

Usage: anx sessions get <ref> (or --session-id <ref>)

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `sessions register`

Register or refresh a private provider session with a monotonic sequence; never creates an agent credential or assigns work.

```text
Generated Help: sessions register

- Command ID: `sessions.register`
- CLI path: `sessions register`
- HTTP: `POST /sessions`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Register or refresh a private native session without assigning, moving, or completing work.
- Output: Returns `SessionResponse`.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `sessions_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Existing bearer authentication is required, including in development mode. Registration and participation writes require an agent principal and never mint credentials. Sessions are private to that principal. Task reads expose only explicitly shared participation metadata; native session identifiers and other task links are never shared. Upserts refresh server-clock activity leases of 120 seconds. A session heartbeat does not refresh task participation. Session closure is terminal and never changes work state, assignees, source authority, or ownership. Session identity is independent of per-attempt /runs. Capabilities are caller-reported, not server-verified support.
- Adjacent commands: `sessions get`

Inputs:
  Required:
  - body `activity` (string)
  - body `capabilities.history` (string)
  - body `capabilities.logs` (string)
  - body `capabilities.resume` (string)
  - body `native_session_id` (string)
  - body `provider` (string)
  - body `sequence` (integer)
  Optional:
  - body `host_scope` (string)
  - body `native_session_id_kind` (string)
  Enum values: activity: active, closed, idle; capabilities.history: supported, unknown, unsupported; capabilities.logs: supported, unknown, unsupported; capabilities.resume: supported, unknown, unsupported; native_session_id_kind: opaque, provider_session_sha256

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Register or refresh a private provider session with a monotonic sequence; never creates an agent credential or assigns work.

Usage: anx sessions register --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work capabilities`

Read capabilities actually advertised by the authenticated central API.

```text
Generated Help: work capabilities

- Command ID: `work.capabilities`
- CLI path: `work capabilities`
- HTTP: `GET /work/capabilities`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect work tracking capabilities. `canonical_entity` is `card`; work is a projection over cards, not a second store.
- Output: Returns `WorkCapabilitiesResponse`.
- Error codes: `invalid_request`, `not_found`, `conflict`, `work_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.
- Adjacent commands: `work create`, `work get`, `work list`, `work observations list`, `work observations submit`, `work participants list`, `work participants register`, `work patch`, `work presence`, `work refresh get`, `work refresh request`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read capabilities actually advertised by the authenticated central API.

Usage: anx work capabilities

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work create`

Register a native commitment or canonical external source. Omitting board_ref uses the workspace default board, creating it if needed.

```text
Generated Help: work create

- Command ID: `work.create`
- CLI path: `work create`
- HTTP: `POST /work`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Register a card-backed commitment. board_ref is optional; omitted uses the workspace default board.
- Output: Returns `WorkResponse`.
- Error codes: `invalid_request`, `not_found`, `conflict`, `work_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace authenticated. When board_ref is omitted, the server places the card on the workspace's oldest active board, creating a default Tasks board if none exists. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.
- Adjacent commands: `work capabilities`, `work get`, `work list`, `work observations list`, `work observations submit`, `work participants list`, `work participants register`, `work patch`, `work presence`, `work refresh get`, `work refresh request`

Inputs:
  Required:
  - body `title` (string)
  Optional:
  - body `actor_id` (string)
  - body `blockers` (list<string>)
  - body `board_ref` (string)
  - body `definition_of_done` (list<string>)
  - body `document_ref` (string)
  - body `due_at` (string)
  - body `executions` (list<object>)
  - body `id` (string)
  - body `next_action` (string)
  - body `next_actor` (string)
  - body `owner` (string)
  - body `phase` (string)
  - body `plan` (any)
  - body `priority` (string)
  - body `project_ref` (string)
  - body `related_refs` (list<any>)
  - body `relations` (list<object>)
  - body `risk` (string)
  - body `source.authority` (string)
  - body `source.connection_id` (string)
  - body `source.native_id` (string)
  - body `source.native_status` (string)
  - body `source.revision` (string)
  - body `source.url` (string)
  - body `start_at` (string)
  - body `summary` (string)
  - body `topic_ref` (string)
  - body `wake_condition` (string)
  - body `workspace_move` (object)
  Enum values: risk: critical, high, low, medium

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Register a native commitment or canonical external source. Omitting board_ref uses the workspace default board, creating it if needed.

Usage: anx work create --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work get`

Read one work card, source authority, executions and current evidence.

```text
Generated Help: work get

- Command ID: `work.get`
- CLI path: `work get`
- HTTP: `GET /work/{card_ref}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read a commitment and its evidence. Same card row as `cards.get`, with projection fields (freshness, observations, annotations).
- Output: Returns `WorkResponse`.
- Error codes: `invalid_request`, `not_found`, `conflict`, `work_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.
- Adjacent commands: `work capabilities`, `work create`, `work list`, `work observations list`, `work observations submit`, `work participants list`, `work participants register`, `work patch`, `work presence`, `work refresh get`, `work refresh request`

Inputs:
  Required:
  - path `card_ref`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read one work card, source authority, executions and current evidence.

Usage: anx work get <ref> (or --work-id <ref>)

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work list`

List work cards across sources in the authenticated workspace.

```text
Generated Help: work list

- Command ID: `work.list`
- CLI path: `work list`
- HTTP: `GET /work`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List the operator Tasks projection over cards. `work.*` adds acceptance criteria, observations, and freshness on the same rows as `cards.*`; use `cards.*` for the canonical store and card workflow writes.
- Output: Returns `WorkListResponse`.
- Error codes: `invalid_request`, `not_found`, `conflict`, `work_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.
- Adjacent commands: `work capabilities`, `work create`, `work get`, `work observations list`, `work observations submit`, `work participants list`, `work participants register`, `work patch`, `work presence`, `work refresh get`, `work refresh request`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

List work cards across sources in the authenticated workspace.

Usage: anx work list
  --project-ref <value>
  --source <value>
  --owner <value>
  --phase <value>
  --freshness <value>
  --q <value>
  --limit <value>
  --cursor <value>

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work patch`

Update work metadata with if_version; external status remains source-owned.

```text
Generated Help: work patch

- Command ID: `work.patch`
- CLI path: `work patch`
- HTTP: `PATCH /work/{card_ref}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Update local commitment annotations.
- Output: Returns `WorkResponse`.
- Error codes: `invalid_request`, `not_found`, `conflict`, `work_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.
- Adjacent commands: `work capabilities`, `work create`, `work get`, `work list`, `work observations list`, `work observations submit`, `work participants list`, `work participants register`, `work presence`, `work refresh get`, `work refresh request`

Inputs:
  Required:
  - path `card_ref`
  - body `if_version` (integer): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.
  Optional:
  - body `actor_id` (string)
  - body `patch.blockers` (list<string>)
  - body `patch.due_at` (string)
  - body `patch.executions` (list<object>)
  - body `patch.next_action` (string)
  - body `patch.next_actor` (string)
  - body `patch.plan` (any)
  - body `patch.priority` (string)
  - body `patch.project_ref` (string)
  - body `patch.relations` (list<object>)
  - body `patch.start_at` (string)
  - body `patch.wake_condition` (string)
  - body `patch.workspace_move` (object)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Update work metadata with if_version; external status remains source-owned.

Usage: anx work patch <ref> (or --work-id <ref>) --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work presence`

Set the current derived agent's card and progress note.

```text
Generated Help: work presence

- Command ID: `agents.me.presence`
- CLI path: `work presence`
- HTTP: `PATCH /agents/me/presence`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Report current card and progress.
- Output: Returns `{ presence }`.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `run_attribution_invalid`
- Concepts: `agents`, `cards`, `runs`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `work capabilities`, `work create`, `work get`, `work list`, `work observations list`, `work observations submit`, `work participants list`, `work participants register`, `work patch`, `work refresh get`, `work refresh request`

Inputs:
  Optional:
  - body `current_card_ref` (string)
  - body `note` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Set the current derived agent's card and progress note.

Usage: anx work presence --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work observations list`

Read append-only evidence for a work card, preserving pagination and uncertainty.

```text
Generated Help: work observations list

- Command ID: `work.observations.list`
- CLI path: `work observations list`
- HTTP: `GET /work/{card_ref}/observations`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List append-only work observations.
- Output: Returns `WorkObservationListResponse`.
- Error codes: `invalid_request`, `not_found`, `conflict`, `work_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.
- Adjacent commands: `work capabilities`, `work create`, `work get`, `work list`, `work observations submit`, `work participants list`, `work participants register`, `work patch`, `work presence`, `work refresh get`, `work refresh request`

Inputs:
  Required:
  - path `card_ref`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read append-only evidence for a work card, preserving pagination and uncertainty.

Usage: anx work observations list <ref> (or --work-id <ref>)
  --limit <value>
  --cursor <value>

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work observations submit`

Submit an authenticated remote observation; preserve its idempotency key on retry.

```text
Generated Help: work observations submit

- Command ID: `work.observations.submit`
- CLI path: `work observations submit`
- HTTP: `POST /work/{card_ref}/observations`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Submit an attributed source observation.
- Output: Returns `WorkObservationResponse`.
- Error codes: `invalid_request`, `not_found`, `conflict`, `work_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.
- Adjacent commands: `work capabilities`, `work create`, `work get`, `work list`, `work observations list`, `work participants list`, `work participants register`, `work patch`, `work presence`, `work refresh get`, `work refresh request`

Inputs:
  Required:
  - path `card_ref`
  - body `observation.idempotency_key` (string)
  - body `observation.observed_at` (string)
  - body `observation.reader_id` (string)
  - body `observation.reader_revision` (string)
  - body `observation.status` (string)
  Optional:
  - body `actor_id` (string)
  - body `observation.actor_id` (string)
  - body `observation.coverage` (object)
  - body `observation.error.code` (string)
  - body `observation.error.message` (string)
  - body `observation.evidence` (list<object>)
  - body `observation.facts` (object)
  - body `observation.id` (string)
  - body `observation.meaningful_progress_at` (string)
  - body `observation.received_at` (string)
  - body `observation.source_activity_at` (string)
  - body `observation.source_revision` (string)
  - body `observation.source_sequence` (integer)
  - body `observation.stale_after_seconds` (integer)
  - body `observation.uncertainty` (list<string>)
  - body `observation.verification` (string)
  - body `observation.work_ref` (string)
  Enum values: observation.status: error, reported, uncertain, verified; observation.verification: reported

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Submit an authenticated remote observation; preserve its idempotency key on retry.

Usage: anx work observations submit <ref> (or --work-id <ref>) --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.
Body: {"observation":{"idempotency_key":"stable-report-key","reader_id":"reader","reader_revision":"v1","observed_at":"RFC3339 timestamp","status":"reported","facts":{},"evidence":[]}}
Preserve source_sequence and idempotency_key on retry; received_at and actor_id are server-owned. Remote verified labels remain claims.

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work participants list`

List task-scoped participation and bounded activity without private session details or unrelated task links.

```text
Generated Help: work participants list

- Command ID: `work.participants.list`
- CLI path: `work participants list`
- HTTP: `GET /work/{card_ref}/participants`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List task-scoped participation without assigning, moving, or completing work.
- Output: Returns `WorkParticipantListResponse`.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `sessions_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Existing bearer authentication is required, including in development mode. Registration and participation writes require an agent principal and never mint credentials. Sessions are private to that principal. Task reads expose only explicitly shared participation metadata; native session identifiers and other task links are never shared. Upserts refresh server-clock activity leases of 120 seconds. A session heartbeat does not refresh task participation. Session closure is terminal and never changes work state, assignees, source authority, or ownership. Session identity is independent of per-attempt /runs. Capabilities are caller-reported, not server-verified support.
- Adjacent commands: `work capabilities`, `work create`, `work get`, `work list`, `work observations list`, `work observations submit`, `work participants register`, `work patch`, `work presence`, `work refresh get`, `work refresh request`

Inputs:
  Required:
  - path `card_ref`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

List task-scoped participation and bounded activity without private session details or unrelated task links.

Usage: anx work participants list <ref> (or --work-id <ref>)
  --limit <value>
  --cursor <value>

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work participants register`

Record nonlocking task participation with session_id and monotonic sequence; never changes task assignees, phase or completion.

```text
Generated Help: work participants register

- Command ID: `work.participants.register`
- CLI path: `work participants register`
- HTTP: `POST /work/{card_ref}/participants`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Register or refresh nonlocking task participation without assigning, moving, or completing work.
- Output: Returns `WorkParticipantResponse`.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `sessions_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Existing bearer authentication is required, including in development mode. Registration and participation writes require an agent principal and never mint credentials. Sessions are private to that principal. Task reads expose only explicitly shared participation metadata; native session identifiers and other task links are never shared. Upserts refresh server-clock activity leases of 120 seconds. A session heartbeat does not refresh task participation. Session closure is terminal and never changes work state, assignees, source authority, or ownership. Session identity is independent of per-attempt /runs. Capabilities are caller-reported, not server-verified support.
- Adjacent commands: `work capabilities`, `work create`, `work get`, `work list`, `work observations list`, `work observations submit`, `work participants list`, `work patch`, `work presence`, `work refresh get`, `work refresh request`

Inputs:
  Required:
  - path `card_ref`
  - body `activity` (string)
  - body `sequence` (integer)
  - body `session_id` (string)
  Enum values: activity: active, idle, left

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Record nonlocking task participation with session_id and monotonic sequence; never changes task assignees, phase or completion.

Usage: anx work participants register <ref> (or --work-id <ref>) --from-file <path|->

JSON body follows the central API contract; use anx debug meta commands for generated schemas. Server validates scope, versions and evidence.

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work refresh get`

Read refresh state without queueing work.

```text
Generated Help: work refresh get

- Command ID: `work.refresh.get`
- CLI path: `work refresh get`
- HTTP: `GET /work/{card_ref}/refresh`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect durable refresh lifecycle.
- Output: Returns `WorkRefreshResponse`.
- Error codes: `invalid_request`, `not_found`, `conflict`, `work_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.
- Adjacent commands: `work capabilities`, `work create`, `work get`, `work list`, `work observations list`, `work observations submit`, `work participants list`, `work participants register`, `work patch`, `work presence`, `work refresh request`

Inputs:
  Required:
  - path `card_ref`

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Read refresh state without queueing work.

Usage: anx work refresh get <ref> (or --work-id <ref>)

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work refresh request`

Request a bounded refresh; queued is not a successful observation.

```text
Generated Help: work refresh request

- Command ID: `work.refresh.request`
- CLI path: `work refresh request`
- HTTP: `POST /work/{card_ref}/refresh`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Queue or coalesce a read-only refresh.
- Output: Returns `WorkRefreshResponse`.
- Error codes: `invalid_request`, `not_found`, `conflict`, `work_unavailable`
- Concepts: `cards`, `evidence`
- Agent notes: Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.
- Adjacent commands: `work capabilities`, `work create`, `work get`, `work list`, `work observations list`, `work observations submit`, `work participants list`, `work participants register`, `work patch`, `work presence`, `work refresh get`

Inputs:
  Required:
  - path `card_ref`
  Optional:
  - body `actor_id` (string)

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Request a bounded refresh; queued is not a successful observation.

Usage: anx work refresh request <ref> (or --work-id <ref>)

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `secret list`

List secrets

```text
Generated Help: secret list

- Command ID: `secrets.list`
- CLI path: `secret list`
- HTTP: `GET /secrets`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List workspace secret metadata without exposing values.
- Output: Returns `{ secrets }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `secrets`
- Adjacent commands: `secret create`, `secret delete`, `secret exec`, `secret get --reveal`, `secret update`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret list ... ; anx --json secret list ... ; anx secret list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret create`

Create secret

```text
Generated Help: secret create

- Command ID: `secrets.create`
- CLI path: `secret create`
- HTTP: `POST /secrets`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Store an encrypted workspace credential with metadata.
- Output: Returns `{ secret }` (metadata only, value is not echoed).
- Error codes: `auth_required`, `invalid_token`, `human_only`, `invalid_request`, `resource_exists`, `secrets_not_configured`
- Concepts: `secrets`, `write`
- Agent notes: Only human principals may create secrets.
- Adjacent commands: `secret delete`, `secret exec`, `secret get --reveal`, `secret list`, `secret update`

Inputs:
  Required:
  - body `name` (string)
  - body `value` (string)
  Optional:
  - body `description` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret create ... ; anx --json secret create ... ; anx secret create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret delete`

Delete secret

```text
Generated Help: secret delete

- Command ID: `secrets.delete`
- CLI path: `secret delete`
- HTTP: `DELETE /secrets/{secret_id}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Permanently remove a secret and its encrypted value.
- Output: Returns `{ deleted: true, secret_id }`.
- Error codes: `auth_required`, `invalid_token`, `human_only`, `not_found`, `secrets_not_configured`
- Concepts: `secrets`, `write`
- Agent notes: Only human principals may delete secrets.
- Adjacent commands: `secret create`, `secret exec`, `secret get --reveal`, `secret list`, `secret update`

Inputs:
  Required:
  - path `secret_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret delete ... ; anx --json secret delete ... ; anx secret delete ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret get --reveal`

Reveal secret value

```text
Generated Help: secret get --reveal

- Command ID: `secrets.reveal`
- CLI path: `secret get --reveal`
- HTTP: `POST /secrets/{secret_id}/reveal`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Decrypt and return a secret value. Logged in audit.
- Output: Returns `{ name, value }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`, `secrets_not_configured`
- Concepts: `secrets`
- Agent notes: Every reveal is logged in auth audit. POST (not GET) to prevent caching.
- Adjacent commands: `secret create`, `secret delete`, `secret exec`, `secret list`, `secret update`

Inputs:
  Required:
  - path `secret_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret get --reveal ... ; anx --json secret get --reveal ... ; anx secret get --reveal ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret exec`

Reveal multiple secrets by name

```text
Generated Help: secret exec

- Command ID: `secrets.reveal-batch`
- CLI path: `secret exec`
- HTTP: `POST /secrets/reveal-batch`
- Side effect class: `external_side_effect`
- Stability: `beta`
- Input mode: `json-body`
- Why: Batch-fetch secrets for env injection. Each reveal is audited.
- Output: Returns `{ secrets: [{ name, value }] }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`, `invalid_request`, `secrets_not_configured`
- Concepts: `secrets`
- Agent notes: Each resolved secret generates an audit event. Missing names return not_found.
- Adjacent commands: `secret create`, `secret delete`, `secret get --reveal`, `secret list`, `secret update`

Inputs:
  Required:
  - body `names` (list<string>)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret exec ... ; anx --json secret exec ... ; anx secret exec ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `secret update`

Update secret value

```text
Generated Help: secret update

- Command ID: `secrets.update`
- CLI path: `secret update`
- HTTP: `PUT /secrets/{secret_id}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Replace an encrypted secret value.
- Output: Returns `{ secret }` (metadata only).
- Error codes: `auth_required`, `invalid_token`, `human_only`, `not_found`, `invalid_request`, `secrets_not_configured`
- Concepts: `secrets`, `write`
- Agent notes: Only human principals may update secrets.
- Adjacent commands: `secret create`, `secret delete`, `secret exec`, `secret get --reveal`, `secret list`

Inputs:
  Required:
  - path `secret_id`
  - body `value` (string)
  Optional:
  - body `description` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx secret update ... ; anx --json secret update ... ; anx secret update ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `update status`

Inspect update policy, ownership, binary, installer receipt, and last failure offline.

```text
Update the installed anx CLI binary in place.

Usage:
  anx update status|now|policy auto|notify|off
  anx update [--check] [--version <tag>]

Options:
  --check                 report the selected target version without changing the binary
  --version <tag>         install a specific release tag instead of the recommended/latest version

Behavior:
  - auto (default) checks on the first successful work write per UTC day in a detached two-minute worker
  - notify checks without installing and emits one daily warning when a newer release is known
  - off disables automatic checks; ANX_UPDATE_POLICY overrides the saved policy
  - status is offline and separates the observed binary from its installer receipt
  - updates only digest-matching ANX installer-managed releases; rerun scripts/install-anx.sh for old installs
  - verifies the release checksum and executable version; rolls back on replacement verification failure
  - runs the new binary's managed skills sync after replacement
  - read-only commands, help, local maintenance and dry runs never trigger binary updates
  - resolves the latest GitHub release, falling back to its public redirect when the API is rate-limited
  - downloads the matching release archive for the current OS/arch and replaces the current binary
  - reminds managed bridge users to rerun anx bridge install

Examples:
  anx update --check
  anx update
  anx update --version v1.2.3
  anx update
```

## `update now`

Verify and atomically replace an ANX-managed release, then sync skills.

```text
Update the installed anx CLI binary in place.

Usage:
  anx update status|now|policy auto|notify|off
  anx update [--check] [--version <tag>]

Options:
  --check                 report the selected target version without changing the binary
  --version <tag>         install a specific release tag instead of the recommended/latest version

Behavior:
  - auto (default) checks on the first successful work write per UTC day in a detached two-minute worker
  - notify checks without installing and emits one daily warning when a newer release is known
  - off disables automatic checks; ANX_UPDATE_POLICY overrides the saved policy
  - status is offline and separates the observed binary from its installer receipt
  - updates only digest-matching ANX installer-managed releases; rerun scripts/install-anx.sh for old installs
  - verifies the release checksum and executable version; rolls back on replacement verification failure
  - runs the new binary's managed skills sync after replacement
  - read-only commands, help, local maintenance and dry runs never trigger binary updates
  - resolves the latest GitHub release, falling back to its public redirect when the API is rate-limited
  - downloads the matching release archive for the current OS/arch and replaces the current binary
  - reminds managed bridge users to rerun anx bridge install

Examples:
  anx update --check
  anx update
  anx update --version v1.2.3
  anx update
```

## `update policy`

Set automatic update policy: auto (default), notify, or off.

```text
Update the installed anx CLI binary in place.

Usage:
  anx update status|now|policy auto|notify|off
  anx update [--check] [--version <tag>]

Options:
  --check                 report the selected target version without changing the binary
  --version <tag>         install a specific release tag instead of the recommended/latest version

Behavior:
  - auto (default) checks on the first successful work write per UTC day in a detached two-minute worker
  - notify checks without installing and emits one daily warning when a newer release is known
  - off disables automatic checks; ANX_UPDATE_POLICY overrides the saved policy
  - status is offline and separates the observed binary from its installer receipt
  - updates only digest-matching ANX installer-managed releases; rerun scripts/install-anx.sh for old installs
  - verifies the release checksum and executable version; rolls back on replacement verification failure
  - runs the new binary's managed skills sync after replacement
  - read-only commands, help, local maintenance and dry runs never trigger binary updates
  - resolves the latest GitHub release, falling back to its public redirect when the API is rate-limited
  - downloads the matching release archive for the current OS/arch and replaces the current binary
  - reminds managed bridge users to rerun anx bridge install

Examples:
  anx update --check
  anx update
  anx update --version v1.2.3
  anx update
```

## `series list`

List workspace series definitions and owning adapters.

```text
Generated Help: series list

- Command ID: `series.list`
- CLI path: `series list`
- HTTP: `GET /series`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List pushed series.
- Output: Returns JSON with provenance and explicit freshness.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `series_capacity`
- Concepts: `documents`
- Agent notes: Explicit declared push source; core never fetches external data.
- Adjacent commands: `series push`, `series query`, `series show`

Local Help: series list

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: List workspace series definitions and owning adapters.
- Quick start: anx series list
- Examples:
  - `anx series list`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx series list ... ; anx --json series list ... ; anx series list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `series show`

Show bounded observations, freshness, and provenance.

```text
Generated Help: series show

- Command ID: `series.show`
- CLI path: `series show`
- HTTP: `GET /series/{name}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Show a series and its provenance.
- Output: Returns JSON with provenance and explicit freshness.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `series_capacity`
- Concepts: `documents`
- Agent notes: Explicit declared push source; core never fetches external data.
- Adjacent commands: `series list`, `series push`, `series query`

Inputs:
  Required:
  - path `name`

Local Help: series show

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Show bounded observations, freshness, and provenance.
- Quick start: anx series show <name> [--range 24h] [--step 1h] [--agg last] [--label k=v]
- Examples:
  - `anx series show builds`

Flags:
  <name>                       Accepted by series show.
  --range <duration>           Accepted by series show.
  --step <duration>            Accepted by series show.
  --agg <aggregation>          Accepted by series show.
  --label k=v                  Accepted by series show.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx series show ... ; anx --json series show ... ; anx series show ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `series query`

Query at most 200 buckets per label set.

```text
Generated Help: series query

- Command ID: `series.query`
- CLI path: `series query`
- HTTP: `GET /series/{name}/query`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Query a bounded series range.
- Output: Returns JSON with provenance and explicit freshness.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `series_capacity`
- Concepts: `documents`
- Agent notes: Explicit declared push source; core never fetches external data.
- Adjacent commands: `series list`, `series push`, `series show`

Inputs:
  Required:
  - path `name`

Local Help: series query

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Query at most 200 buckets per label set.
- Quick start: anx series query <name> --range <duration> --step <duration> [--agg last|avg|sum|min|max|count] [--label k=v]
- Examples:
  - `anx series query builds --range 7d --step 1h --agg sum`

Flags:
  <name>                       Accepted by series query.
  --range <duration>           Accepted by series query.
  --step <duration>            Accepted by series query.
  --agg <aggregation>          Accepted by series query.
  --label k=v                  Accepted by series query.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx series query ... ; anx --json series query ... ; anx series query ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `series push`

Push one point through an explicit adapter grant.

```text
Generated Help: series push

- Command ID: `series.push`
- CLI path: `series push`
- HTTP: `POST /series/{name}/points`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `flags`
- Why: Push one declared series point.
- Output: Returns JSON with provenance and explicit freshness.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `series_capacity`, `series_rate_limited`
- Concepts: `documents`
- Agent notes: Explicit declared push source; core never fetches external data.
- Adjacent commands: `series list`, `series query`, `series show`
- Examples:
  - Push a number: `anx series push builds 12 --label initiative=launch`
  - Push command output: `anx series push builds --from-command -- ./count-builds`

Inputs:
  Required:
  - path `name`
  Optional:
  - body `labels` (object)
  - body `state` (string)
  - body `ts` (datetime)
  - body `value` (number)

CLI input:
  Flags:
  - `--adapter`: Declared adapter; omitted means resolve from series inventory.
  - `--label`: Repeated exact k=v labels.
  - `--ts`: RFC3339 observation timestamp.
  - `--from-command`: Read a number or JSON point from the argv after --.
  - `--series`: Series name when from-command stdout is a number.

Local Help: series push

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Push one point through an explicit adapter grant.
- Quick start: anx series push <name> <value-or-state> [--adapter <name>] [--label k=v] [--ts RFC3339]; or anx series push [<name>] --from-command -- <cmd> [args...]
- Examples:
  - `anx series push builds 12 --label initiative=launch`
  - `anx series push --series builds --from-command -- ./count-builds`

Flags:
  <name>                       Accepted by series push.
  <value-or-state>             Accepted by series push.
  --adapter <name>             Accepted by series push.
  --label k=v                  Accepted by series push.
  --ts RFC3339                 Accepted by series push.
  --from-command               Accepted by series push.
  --series <name>              Accepted by series push.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx series push ... ; anx --json series push ... ; anx series push ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `adapters declare`

Declare a host-local adapter and its allowed series.

```text
Generated Help: adapters declare

- Command ID: `adapters.declare`
- CLI path: `adapters declare`
- HTTP: `POST /adapters`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `file-and-body`
- Why: Declare an adapter and its series.
- Output: Returns JSON with provenance and explicit freshness.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `series_capacity`
- Concepts: `documents`
- Agent notes: Explicit declared push source; core never fetches external data.
- Adjacent commands: `adapters delete`, `adapters list`, `adapters revoke`, `adapters token`
- Examples:
  - Declare before pushing: `anx adapters declare --body-file adapter.json`

Inputs:
  Required:
  - body `agent_id` (string)
  - body `description` (string)
  - body `expected_interval` (string)
  - body `name` (string)
  - body `series` (list<any>)

CLI input:
  Flags:
  - `--body-file` required: UTF-8 adapter declaration JSON file or stdin (-).

Local Help: adapters declare

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Declare a host-local adapter and its allowed series.
- Quick start: anx adapters declare --body-file <path|->
- Examples:
  - `anx adapters declare --body-file adapter.json`

Flags:
  --body-file <path|->         Accepted by adapters declare.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx adapters declare ... ; anx --json adapters declare ... ; anx adapters declare ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `adapters list`

List declared sources and grant state.

```text
Generated Help: adapters list

- Command ID: `adapters.list`
- CLI path: `adapters list`
- HTTP: `GET /adapters`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List declared adapters.
- Output: Returns JSON with provenance and explicit freshness.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `series_capacity`
- Concepts: `documents`
- Agent notes: Explicit declared push source; core never fetches external data.
- Adjacent commands: `adapters declare`, `adapters delete`, `adapters revoke`, `adapters token`

Local Help: adapters list

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: List declared sources and grant state.
- Quick start: anx adapters list
- Examples:
  - `anx adapters list`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx adapters list ... ; anx --json adapters list ... ; anx adapters list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `adapters revoke`

Revoke a source grant immediately; keep observations.

```text
Generated Help: adapters revoke

- Command ID: `adapters.revoke`
- CLI path: `adapters revoke`
- HTTP: `POST /adapters/{name}/revoke`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Revoke an adapter grant.
- Output: Returns JSON with provenance and explicit freshness.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `series_capacity`
- Concepts: `documents`
- Agent notes: Explicit declared push source; core never fetches external data.
- Adjacent commands: `adapters declare`, `adapters delete`, `adapters list`, `adapters token`

Inputs:
  Required:
  - path `name`

Local Help: adapters revoke

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Revoke a source grant immediately; keep observations.
- Quick start: anx adapters revoke <name>
- Examples:
  - `anx adapters revoke github`

Flags:
  <name>                       Accepted by adapters revoke.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx adapters revoke ... ; anx --json adapters revoke ... ; anx adapters revoke ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `adapters delete`

Delete a source and its series history; invalidate its tokens.

```text
Generated Help: adapters delete

- Command ID: `adapters.delete`
- CLI path: `adapters delete`
- HTTP: `DELETE /adapters/{name}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Delete an adapter and its series.
- Output: Returns JSON with provenance and explicit freshness.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `series_capacity`
- Concepts: `documents`
- Agent notes: Explicit declared push source; core never fetches external data.
- Adjacent commands: `adapters declare`, `adapters list`, `adapters revoke`, `adapters token`

Inputs:
  Required:
  - path `name`

Local Help: adapters delete

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Delete a source and its series history; invalidate its tokens.
- Quick start: anx adapters delete <name>
- Examples:
  - `anx adapters delete github`

Flags:
  <name>                       Accepted by adapters delete.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx adapters delete ... ; anx --json adapters delete ... ; anx adapters delete ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `adapters token`

Exchange owner identity for a short-lived, push-only token.

```text
Generated Help: adapters token

- Command ID: `adapters.token`
- CLI path: `adapters token`
- HTTP: `POST /adapters/{name}/token`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Exchange owner identity for a scoped series-write token.
- Output: Returns JSON with provenance and explicit freshness.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `not_found`, `conflict`, `series_capacity`
- Concepts: `documents`
- Agent notes: Explicit declared push source; core never fetches external data.
- Adjacent commands: `adapters declare`, `adapters delete`, `adapters list`, `adapters revoke`

Inputs:
  Required:
  - path `name`

Local Help: adapters token

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Exchange owner identity for a short-lived, push-only token.
- Quick start: anx adapters token <name>
- Examples:
  - `anx --json adapters token github`

Flags:
  <name>                       Accepted by adapters token.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx adapters token ... ; anx --json adapters token ... ; anx adapters token ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox list`

List asks addressed to the active agent, including answer and per-answer read state.

```text
Generated Help: inbox list

- Command ID: `inbox.list`
- CLI path: `inbox list`
- HTTP: `GET /inbox`
- Side effect class: `read_only`
- Input mode: `none`
- Why: Project human_attention_requested events into a queryable inbox view.
- Output: Returns `{ status, items, generated_at }`; completed adds `{ next_cursor }`; open projection adds `{ projection_freshness }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Adjacent commands: `inbox get`, `inbox respond`, `inbox stream`


View scoping:
  - `anx inbox list` lists your own open asks. Use `--status answered` to read replies, or `--status all` for both.
  - Add `--unread` to show answers not yet individually marked read; it implies answered unless `--status` is explicit.
  - `anx inbox read event:<ask-id>` marks only that answer read, even during the quiet window.
  - `anx notifications read --wakeup-id <id>` marks a wake notification read separately.
  - Human attention triage remains available as `anx debug inbox list`; use `anx inbox respond` to answer an inbox item.
  - Select an agent with `--as <name>` or `ANX_AS`.

Inbox kinds:
  - `ask`: A requesting agent needs an answer, judgment, or missing context.
  - `review`: A requesting agent wants review of generated work or a proposed action.
  - `escalate`: A requesting agent surfaced a risk or abnormal condition.

Local Help: inbox list

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: List asks addressed to the active agent, including answer and per-answer read state.
- Quick start: Use `--unread` to focus on unprocessed answers; it implies `--status answered` unless status is explicit.
- Composition: Reads the authenticated agent's requester-scoped, paginated ask projection. Answer read state is per response and independent of wake notifications. Use `anx debug inbox list` for operator inbox diagnostics.
- Examples:
  - `anx inbox list`
  - `anx inbox list --status answered`
  - `anx inbox list --unread`

Flags:
  --status <open|answered|all> Filter your asks; default is open.
  --unread                     Show answers not individually marked read; implies answered unless --status is explicit.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox list ... ; anx --json inbox list ... ; anx inbox list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox read`

Mark one answer to your ask as read, including before its wake is delivered.

```text
Local Help: inbox read

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Mark one answer to your ask as read, including before its wake is delivered.
- Quick start: Pass the `event:<ask-id>` returned by `anx ask`.
- Composition: Persists read state for that answer only. Wake notification read state remains separate and is managed with `anx notifications read`.
- Examples:
  - `anx inbox read event:<ask-id>`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx inbox read ... ; anx --json inbox read ... ; anx inbox read ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `move`

Move a Card or Topic and its related Boards, Cards, and Docs between enrolled workspaces.

```text
anx move card <ref> --to <workspace-alias> [--connection-map <source-id>=<destination-id>] [--dry-run]; anx move topic <ref> --to <workspace-alias> [--connection-map <source-id>=<destination-id>] [--dry-run]. Topic moves journal source revisions, rewrite refs, verify every destination resource, and only then archive or tombstone the source.
```

## `lifecycle verbs`

Uniform lifecycle surface for archive, unarchive, trash, restore, and purge across artifacts, boards, docs, events, cards, and topics.

```text
Local Help: lifecycle verbs

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Uniform lifecycle surface for archive, unarchive, trash, restore, and purge across artifacts, boards, docs, events, cards, and topics.
- Composition: Canonical resources and verbs are listed in `internal/app/lifecycle_spec.go`. When a flag replaces a non-empty JSON field, `--json --dry-run` includes `_overrides` and `anx_cli_recovery.kind=json_flag_overlay`.
- JSON body: Optional `--from-file` JSON object body; `--reason`, `--actor-id` (except purge), and `--dry-run` augment or replace JSON fields.
- Examples:
  - `anx artifacts archive artifact:notes --reason "obsolete"`
  - `anx boards trash board:launch --reason "merged elsewhere" --dry-run --json`
  - `anx cards archive card:foo --from-file lifecycle.json --actor-id actor:agent-beta`

Flags:
  --reason <text>              Short audit string stamped on the lifecycle event.
  --from-file <path>           Advanced JSON body from file or stdin (`-`).
  --actor-id <actor-id>        Actor id (every verb except purge); overlays JSON `actor_id`.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx lifecycle verbs ... ; anx --json lifecycle verbs ... ; anx lifecycle verbs ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics create`

Create a topic from plain flags, or from advanced JSON.

```text
Generated Help: topics create

- Command ID: `topics.create`
- CLI path: `topics create`
- HTTP: `POST /topics`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a first-class durable topic before attaching cards, docs, or artifacts.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `topics`, `write`
- Agent notes: Replay-safe when the same request key and body are reused.
- Adjacent commands: `topics archive`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - body `topic.board_refs` (list<any>)
  - body `topic.document_refs` (list<any>)
  - body `topic.owner_refs` (list<any>)
  - body `topic.provenance.sources` (list<string>)
  - body `topic.related_refs` (list<any>)
  - body `topic.summary` (string)
  - body `topic.title` (string)
  Optional:
  - body `request_key` (string)
  - body `topic.id` (string)
  - body `topic.provenance.by_field` (object)
  - body `topic.provenance.notes` (string)
  - body `topic.thread_id` (string)
  - body `topic.workspace_move` (object)

Local Help: topics create

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Create a topic from plain flags, or from advanced JSON.
- Composition: Builds the `topics.create` request. Use Topics for discussion and current context around a project, incident, decision, or recurring process.
- JSON body: Either flags building `{ topic }`, or advanced JSON body `{ topic }` from stdin/--from-file.
- Examples:
  - `anx topics create --title "Launch" --summary "Coordinate launch work"`
  - `anx topics create --title "Incident 42" --summary "Triage checkout failures" --owner-ref actor:<actor-id>`
  - `cat topic.json | anx topics create`

Flags:
  --title <text>               Topic title.
  --summary <text>             Topic summary.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --owner-ref <typed-ref>      Owner typed ref, repeatable.
  --document-ref <typed-ref>   Linked document typed ref, repeatable.
  --board-ref <typed-ref>      Linked board typed ref, repeatable.
  --ref <typed-ref>            Additional related typed ref, repeatable.
  --from-file <path>           Advanced JSON request body from file.
  --dry-run                    Validate and render the request without sending it.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics create ... ; anx --json topics create ... ; anx topics create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics patch`

Patch a topic from scalar flags, or from advanced JSON.

```text
Generated Help: topics patch

- Command ID: `topics.patch`
- CLI path: `topics patch`
- HTTP: `PATCH /topics/{topic_id}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Update topic state with provenance and optimistic concurrency.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`, `concurrency`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics restore`, `topics timeline`, `topics trash`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.
  Optional:
  - body `patch.board_refs` (list<any>)
  - body `patch.document_refs` (list<any>)
  - body `patch.owner_refs` (list<any>)
  - body `patch.provenance.by_field` (object)
  - body `patch.provenance.notes` (string)
  - body `patch.provenance.sources` (list<string>)
  - body `patch.related_refs` (list<any>)
  - body `patch.summary` (string)
  - body `patch.title` (string)
  - body `patch.workspace_move` (object)

Local Help: topics patch

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Patch a topic from scalar flags, or from advanced JSON.
- Composition: Fetches the Topic to discover `updated_at` when `--if-updated-at` is omitted.
- JSON body: Either flags building `{ patch, if_updated_at }`, or advanced JSON body from stdin/--from-file.
- Examples:
  - `anx topics patch <topic-id> --title "Launch plan"`
  - `anx topics patch <topic-id> --summary "Updated coordination notes" --if-updated-at <updated_at>`
  - `cat topic-patch.json | anx topics patch <topic-id>`

Flags:
  <topic-id>                   Topic id or unique prefix to patch.
  --title <text>               Topic title.
  --summary <text>             Topic summary.
  --if-updated-at <timestamp>  Optimistic concurrency token; discovered from topics get when omitted.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --from-file <path>           Advanced JSON request body from file.
  --dry-run                    Validate and render the request without sending it.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics patch ... ; anx --json topics patch ... ; anx topics patch ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics trash`

Trash a topic with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).

```text
Generated Help: topics trash

- Command ID: `topics.trash`
- CLI path: `topics trash`
- HTTP: `POST /topics/{topic_id}/trash`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `flags`
- Why: Move topic to trash with an explicit operator reason.
- Output: Returns `{ topic }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `topics`, `write`
- Adjacent commands: `topics archive`, `topics create`, `topics get`, `topics list`, `topics patch`, `topics restore`, `topics timeline`, `topics unarchive`, `topics workspace`

Inputs:
  Required:
  - path `topic_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

CLI input:
  Flags:
  - `--reason` required -> body `reason`: Operator-visible trash reason.

Local Help: topics trash

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Trash a topic with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).
- Composition: Uses the shared lifecycle parser (`lifecycle_spec.go`). Routine trashing prefers `--reason`; `--from-file` remains the advanced compatibility path.
- JSON body: `{ reason, actor_id?, ... }` from `--from-file`, merged with `--reason` / `--actor-id` flags.
- Examples:
  - `anx topics trash topic:launch --reason "test artifact"`
  - `cat trash.json | anx topics trash topic:launch --from-file=-`

Flags:
  <topic-id>                   Topic id or unique prefix to trash.
  --reason <text>              Reason for trashing the topic.
  --from-file <path>           Advanced JSON request body from file or stdin (`-`).
  --actor-id <actor-id>        Actor id; overlays JSON and defaults from resolved agent when omitted.
  --dry-run                    Validate and render the request without sending it.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics trash ... ; anx --json topics trash ... ; anx topics trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics message`

Post a message to a Topic conversation without hand-authoring event JSON.

```text
Local Help: topics message

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Post a message to a Topic conversation without hand-authoring event JSON.
- Composition: Fetches the Topic to discover its backing thread, then writes a visible `message_posted` event to that thread.
- JSON body: Builds an `events.create` body with `event.type=message_posted`, topic/thread refs, and payload text.
- Examples:
  - `anx topics message topic:launch --body-file message.md`
  - `anx topics message topic:launch --body "Decision context"`

Flags:
  <ref>                        Topic ref, handle, or id to message.
  --thread <thread-id>         Backing thread id for thread-scoped message fallback.
  --thread-id <thread-id>      Backing thread id for thread-scoped message fallback.
  --body <text>                Message body text.
  --body-file <path>           Load message body text from a local file.
  --summary <text>             Optional short event summary.
  --ref <typed-ref>            Additional typed ref, repeatable.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics message ... ; anx --json topics message ... ; anx topics message ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics messages`

List messages from a Topic conversation.

```text
Local Help: topics messages

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: List messages from a Topic conversation.
- Composition: Fetches the Topic, then reads its backing thread timeline and filters to messages attached to that topic.
- JSON body: Fetches the Topic backing thread and returns an `events.list`-style filtered timeline slice with topic metadata.
- Examples:
  - `anx topics messages topic:launch`
  - `anx topics messages topic:launch --max-events 5 --mine`

Flags:
  <ref>                        Topic ref, handle, or id whose messages should be listed.
  --max-events <n>             Return at most N most-recent matching messages.
  --mine                       Filter to messages authored by the resolved agent actor_id.
  --actor-id <actor-id>        Filter to one actor id.
  --full-id                    (debug/admin) Render full event ids in default text output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics messages ... ; anx --json topics messages ... ; anx topics messages ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `topics reply`

Reply to an existing Topic message.

```text
Local Help: topics reply

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Reply to an existing Topic message.
- Composition: Fetches the Topic and validates the target message exists on its backing thread before posting the reply.
- JSON body: Builds an `events.create` body like `topics message` and adds `payload.reply_to_event_id` plus an `event:launch-update` ref.
- Examples:
  - `anx topics reply topic:launch --to <message-id> --body "Confirmed"`
  - `anx topics reply topic:launch --to <message-id> --body-file reply.md`

Flags:
  <ref>                        Topic ref, handle, or id to reply on.
  --thread <thread-id>         Backing thread id for thread-scoped reply fallback.
  --thread-id <thread-id>      Backing thread id for thread-scoped reply fallback.
  --to <message-id>            Message/event id, typed ref, or handle being replied to.
  --body <text>                Reply body text.
  --body-file <path>           Load reply body text from a local file.
  --summary <text>             Optional short event summary.
  --ref <typed-ref>            Additional typed ref, repeatable.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx topics reply ... ; anx --json topics reply ... ; anx topics reply ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards create`

Create an active-work Board from flags, optionally tied to a Topic.

```text
Generated Help: boards create

- Command ID: `boards.create`
- CLI path: `boards create`
- HTTP: `POST /boards`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a durable board over topics and cards.
- Output: Returns `{ board }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `boards`, `write`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - body `board.document_refs` (list<any>)
  - body `board.pinned_refs` (list<any>)
  - body `board.provenance.sources` (list<string>)
  - body `board.title` (string)
  Optional:
  - body `board.column_schema` (object)
  - body `board.id` (string)
  - body `board.primary_topic_ref` (string)
  - body `board.provenance.by_field` (object)
  - body `board.provenance.notes` (string)
  - body `board.summary` (string)
  - body `board.thread_id` (string)
  - body `board.workspace_move` (object)

Local Help: boards create

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Create an active-work Board from flags, optionally tied to a Topic.
- Composition: Builds the `boards.create` request. Use Boards for active work tracking, ownership, columns, and Card movement.
- JSON body: Either flags building `{ board }`, or advanced JSON body `{ board }` from stdin/--from-file.
- Examples:
  - `anx boards create --topic topic:<topic-handle> --title "Launch board"`
  - `anx boards create --title "Launch board" --summary "Active launch work" --document-ref document:<document-handle>`
  - `cat board.json | anx boards create`

Flags:
  --title <text>               Board title.
  --summary <text>             Optional board summary.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --topic <topic-ref-or-handle> Primary topic typed ref or handle.
  --document-ref <typed-ref>   Linked document typed ref, repeatable.
  --ref <typed-ref>            Pinned/related typed ref, repeatable.
  --from-file <path>           Advanced JSON request body from file.
  --dry-run                    Validate and render the request without sending it.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards create ... ; anx --json boards create ... ; anx boards create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards list`

List cards across the workspace, or list one board's cards with --board.

```text
Generated Help: cards list

- Command ID: `cards.list`
- CLI path: `cards list`
- HTTP: `GET /cards`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Scan the canonical card store. `cards.*` is the store API; `work.*` is the operator Tasks projection over the same rows. Use this family for card workflow writes.
- Output: Returns `{ cards }`.
- Error codes: `auth_required`, `invalid_token`
- Concepts: `cards`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards history`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards revise`, `cards revision get`, `cards timeline`, `cards trash`

Local Help: cards list

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: List cards across the workspace, or list one board's cards with --board.
- Composition: Uses `cards.list` for workspace-wide reads. With `--board`, resolves the board ref/handle/id and uses the board-scoped card list while keeping the canonical CLI path `cards list`.
- JSON body: Global list returns `{ cards }`; `--board` composes the board-scoped card list and returns `{ board_ref, board_handle, cards }`.
- Examples:
  - `anx cards list`
  - `anx cards list --board board:<board-handle>`
  - `anx cards list --include-archived`

Flags:
  --board <board-ref-or-handle> List cards on one board.
  --board-id <board-id>        Board id/ref/handle; equivalent to --board for scripts.
  --include-archived           Include archived cards in global list output.
  --archived-only              Show only archived cards in global list output.
  --include-trashed            Include trashed cards in global list output.
  --trashed-only               Show only trashed cards in global list output.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards list ... ; anx --json cards list ... ; anx cards list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs create`

Create a durable document lineage, with a file-first text-doc path for agents.

```text
Local Help: docs create

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Create a durable document lineage, with a file-first text-doc path for agents.
- Quick start: Flags: `--topic <topic-ref-or-handle> --title <text> --body-file <path>`; use `--body <text>` for short inline content or `--from-file <path>` for advanced JSON.
- Composition: Builds the same `docs.create` request as the generated command. For ordinary text docs, prefer flags so agents can draft Markdown locally without hand-authoring JSON.
- JSON body: Either flags plus `--body-file`, or advanced JSON body `{ document, content, content_type }` from stdin/--from-file.
- Examples:
  - `anx docs create --topic topic:<topic-handle> --title "Runbook" --body-file runbook.md`
  - `anx docs create --topic topic:<topic-handle> --title "Note" --body "Short update"`
  - `anx docs create --subject-ref topic:<topic-handle> --title "Runbook" --summary "Durable context" --body-file runbook.md`
  - `cat doc-create.json | anx docs create`

Flags:
  --topic <topic-ref-or-handle> Anchor the document to a topic typed ref or handle.
  --subject-ref <typed-ref>    Explicit document subject ref when not using --topic.
  --title <text>               Document title for flag-built text docs.
  --summary <text>             Optional document summary for list/detail headers.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --ref <typed-ref>            Additional typed ref (repeatable).
  --body-file <path>           Load Markdown/text content from a local file, or stdin with `-`.
  --body <text>                Inline document body text (Markdown/text) when not using --body-file.
  --from-file <path>           Advanced JSON request body from file.
  --dry-run                    Validate and render the request without sending it.

Generated Help: docs create

- Command ID: `docs.create`
- CLI path: `docs create`
- HTTP: `POST /docs`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a canonical document lineage anchored to a typed subject ref.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Create from a local file: `anx docs create --topic topic:launch --title "Runbook" --body-file runbook.md`

Inputs:
  Required:
  - body `content_type` (string)
  - body `document.title` (string)
  Optional:
  - body `actor_id` (string)
  - body `content` (any)
  - body `content_base64` (string)
  - body `document.document_id` (string)
  - body `document.handle` (string)
  - body `document.hosts` (list<string>)
  - body `document.provenance.by_field` (object)
  - body `document.provenance.notes` (string)
  - body `document.provenance.sources` (list<string>)
  - body `document.refs` (list<any>)
  - body `document.source` (string)
  - body `document.subject_ref` (string)
  - body `document.summary` (string)
  - body `document.tags` (list<string>)
  - body `document.thread_id` (string)
  - body `document.verified_at` (datetime)
  - body `document.workspace_move` (object)
  - body `refs` (list<any>)
  - body `request_key` (string)
  Enum values: content_type: binary, structured, text

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs create ... ; anx --json docs create ... ; anx docs create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs search`

Search documents by title, body, source, tags, and comments.

```text
Local Help: docs search

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Search documents by title, body, source, tags, and comments.
- Composition: SQLite FTS5 over title, body, summary, source, tags, and comments. Use `--knowledge` for agent-facing docs tagged `knowledge`. `--host` filters knowledge facts that apply to that machine.
- JSON body: GET `/docs/search?q=` returning `{ documents, next_cursor? }` with optional `search_rank`.
- Examples:
  - `anx docs search "runbook" --knowledge --host laptop-a`
  - `anx docs search "alphawhiz" --knowledge --host laptop-a --limit 20`

Flags:
  <q>                          Search query; also accepted as `--q`.
  --q <text>                   Search query over title, body, and comments.
  --knowledge                  Only documents tagged knowledge.
  --tag <tag>                  Restrict results to one tag.
  --host <name>                Restrict results to documents whose hosts list includes this name.
  --limit <n>                  Page size; omit to return up to 50 hits.
  --cursor <cursor>            Pagination cursor from a previous search response.

Generated Help: docs search

- Command ID: `docs.search`
- CLI path: `docs search`
- HTTP: `GET /docs/search`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Full-text search over document title, body, and comments so agents can find knowledge another host wrote.
- Output: Returns `{ documents, next_cursor? }`. Each document may include `search_rank` (higher is better).
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `docs`
- Agent notes: SQLite FTS5 over title, body, summary, source, tags, and backing-thread comments. Query terms are AND-matched; punctuation is tokenized. Prefer this over `docs.list?q=` when matching body or comments. `search_rank` is higher for stronger matches (title weighted above body, then summary/source/tags, then comments).
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs trash`, `docs unarchive`
- Examples:
  - Search knowledge: `anx docs search "runbook" --knowledge --limit 20`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs search ... ; anx --json docs search ... ; anx docs search ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs put`

Create or replace a document by handle from a local file or stdin.

```text
Local Help: docs put

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Create or replace a document by handle from a local file or stdin.
- Composition: Idempotent by handle: missing handles create, existing handles append a revision and update title/source/tags/hosts/verified_at.
- JSON body: PUT `/docs/{document_id}` with `{ document, content, content_type }`. Handle is `--handle`, filename stem, or title slug.
- Examples:
  - `anx docs put runbook.md --title "Runbook" --tags knowledge --source https://example.invalid/runbook.md --hosts laptop-a --verified-at 2026-09-08T12:00:00Z`
  - `anx docs put - --handle kb-shared --title "Note" --tags knowledge`

Flags:
  <path>                       Markdown/text file, or `-` for stdin.
  --title <text>               Document title.
  --source <url-or-ref>        Canonical source URL or ref when this doc aggregates.
  --tags <tag>                 Tags, repeatable or comma-separated. Use `knowledge` for agent-facing docs.
  --hosts <name>               Host names this knowledge fact applies to.
  --verified-at <rfc3339>      When this knowledge fact was last verified.
  --handle <handle>            Public handle used as the idempotency key.
  --body <text>                Inline body when not passing a path.
  --body-file <path>           Load body from a file or stdin with `-`.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.

Generated Help: docs put

- Command ID: `docs.put`
- CLI path: `docs put`
- HTTP: `PUT /docs/{document_id}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Idempotent write of document body and metadata keyed by handle, so agents can republish knowledge without duplicating lineages.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `conflict`
- Concepts: `docs`, `write`
- Agent notes: Path `{document_id}` is the public handle (or `document:<handle>`). If that handle exists, a new revision is appended and metadata (`title`, `source`, `tags`, `hosts`, `verified_at`) is updated. If it does not exist, the document is created with that handle. Visibility/lifecycle is unchanged. CLI `anx docs put -` reads the body from stdin.
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Publish from stdin: `anx docs put - --handle kb-shared --title "Note" --tags knowledge --source https://example.invalid/note.md --hosts laptop-a --verified-at 2026-09-08T12:00:00Z`

Inputs:
  Required:
  - path `document_id`
  - body `content` (any)
  - body `content_type` (string)
  Optional:
  - body `actor_id` (string)
  - body `document.hosts` (list<string>)
  - body `document.provenance.by_field` (object)
  - body `document.provenance.notes` (string)
  - body `document.provenance.sources` (list<string>)
  - body `document.refs` (list<any>)
  - body `document.source` (string)
  - body `document.subject_ref` (string)
  - body `document.summary` (string)
  - body `document.tags` (list<string>)
  - body `document.title` (string)
  - body `document.verified_at` (datetime)
  - body `refs` (list<any>)
  Enum values: content_type: binary, structured, text

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs put ... ; anx --json docs put ... ; anx docs put ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs ingest`

Upsert markdown files under a directory as knowledge docs with source pointers.

```text
Local Help: docs ingest

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Upsert markdown files under a directory as knowledge docs with source pointers.
- Composition: Walks `.md` / `.markdown` files, tags them `knowledge`, sets `source` to `--source` plus the relative path, and skips a put when title, source, tags, and body are unchanged so a second run creates no new revisions.
- JSON body: Local summary `{ created, updated, unchanged, skipped, failed, documents[] }`. Each file is `docs.put` by a handle derived from its relative path.
- Examples:
  - `anx docs ingest ./kb --source https://example.invalid/kb`

Flags:
  <path>                       Directory of markdown files, or a single markdown file.
  --source <url-prefix>        Required. Joined with each relative path as the canonical source pointer.
  --tags <tag>                 Extra tags. `knowledge` is always applied.
  --hosts <name>               Host names this knowledge tree applies to.
  --verified-at <rfc3339>      When this knowledge tree was last verified.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs ingest ... ; anx --json docs ingest ... ; anx docs ingest ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs comment`

Post a document comment (or a reply with `--reply-to`).

```text
Local Help: docs comment

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Post a document comment (or a reply with `--reply-to`).
- Composition: Writes a `message_posted` event on the document backing thread. Comment ids are stable event ids.
- JSON body: POST `/docs/{document_id}/comments` with `{ text, parent_id? }`.
- Examples:
  - `anx docs comment doc:runbook "Host B found this"`
  - `anx docs comment doc:runbook --body "Acknowledged" --reply-to <comment-id>`

Flags:
  <ref>                        Document ref, handle, or id.
  <text>                       Comment body; also accepted as `--body`.
  --body <text>                Comment text.
  --reply-to <comment-id>      Parent comment id for a reply.
  --document-id <id>           Document id when not using the positional.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.

Generated Help: docs comment

- Command ID: `docs.comments.create`
- CLI path: `docs comment`
- HTTP: `POST /docs/{document_id}/comments`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Post a comment on a document so another agent can read it later.
- Output: Returns `{ comment }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `docs`, `write`
- Agent notes: Posts a `message_posted` event on the document backing thread. Optional `reply_to` or `parent_id` creates a reply. Comment refs (`event:<handle>`) are stable across document revisions and are the deep-link identity.
- Adjacent commands: `docs archive`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Post a comment: `anx docs comment doc:runbook "Host B found this"`

Inputs:
  Required:
  - path `document_id`
  - body `text` (string)
  Optional:
  - body `actor_id` (string)
  - body `parent_id` (string)
  - body `reply_to` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs comment ... ; anx --json docs comment ... ; anx docs comment ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs comments`

List document comments as a thread with stable ids.

```text
Local Help: docs comments

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: List document comments as a thread with stable ids.
- Composition: Reads `message_posted` events on the document backing thread.
- JSON body: GET `/docs/{document_id}/comments` returning `{ comments, next_cursor? }`.
- Examples:
  - `anx docs comments doc:runbook`
  - `anx docs comments doc:runbook --limit 20`

Flags:
  <ref>                        Document ref, handle, or id.
  --document-id <id>           Document id when not using the positional.
  --limit <n>                  Page size.
  --cursor <cursor>            Pagination cursor from a previous comments response.

Generated Help: docs comments

- Command ID: `docs.comments.list`
- CLI path: `docs comments`
- HTTP: `GET /docs/{document_id}/comments`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read the document discussion thread with stable comment ids.
- Output: Returns `{ comments, next_cursor? }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `docs`
- Agent notes: Comments are the document backing-thread `message_posted` events, projected with stable `event:<handle>` refs that survive document revisions. `reply_to` is the parent comment ref for threaded replies; `parent_id` is the same parent as an internal id.
- Adjacent commands: `docs archive`, `docs comment`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - List comments: `anx docs comments doc:runbook`

Inputs:
  Required:
  - path `document_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs comments ... ; anx --json docs comments ... ; anx docs comments ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs comments reply`

Reply to a document comment.

```text
Local Help: docs comments reply

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Reply to a document comment.
- Composition: Writes a `message_posted` reply with `reply_to` set to the parent comment ref.
- JSON body: POST `/docs/{document_id}/comments/{comment_id}/replies` with `{ text }`.
- Examples:
  - `anx docs comments reply doc:runbook event:note --body "Acknowledged"`

Flags:
  <doc>                        Document ref, handle, or id.
  <comment>                    Parent comment ref (`event:<handle>`) or id.
  --body <text>                Reply text.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.

Generated Help: docs comments reply

- Command ID: `docs.comments.reply`
- CLI path: `docs comments reply`
- HTTP: `POST /docs/{document_id}/comments/{comment_id}/replies`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Reply in a document comment thread without leaving the docs surface.
- Output: Returns `{ comment }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Concepts: `docs`, `write`
- Agent notes: Posts a reply `message_posted` event with `reply_to` set to `{comment_id}`. Prefer `docs comment --reply-to` from the CLI.
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Reply in thread: `anx docs comments reply doc:runbook event:note --body "Acknowledged"`

Inputs:
  Required:
  - path `document_id`
  - path `comment_id`
  - body `text` (string)
  Optional:
  - body `actor_id` (string)
  - body `parent_id` (string)
  - body `reply_to` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs comments reply ... ; anx --json docs comments reply ... ; anx docs comments reply ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs get`

Get a document lineage and its current head revision.

```text
Local Help: docs get

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Get a document lineage and its current head revision.
- Composition: `--format md` prints only the markdown body, suitable for piping.
- JSON body: GET `/docs/{document_id}` returning `{ document, revision }`.
- Examples:
  - `anx docs get kb-shared --format md`

Flags:
  <ref>                        Document ref, handle, or id.
  --document-id <id>           Document id when not using the positional.
  --format md                  Print only the current revision body.

Generated Help: docs get

- Command ID: `docs.get`
- CLI path: `docs get`
- HTTP: `GET /docs/{document_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve a document lineage and its current head revision.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `docs`
- Agent notes: Returns `{ document, revision }` including the head revision body. CLI `--format md` prints only the markdown body. Knowledge docs expose `source`, `hosts`, and `verified_at`.
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Print markdown body: `anx docs get kb-shared --format md`

Inputs:
  Required:
  - path `document_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs get ... ; anx --json docs get ... ; anx docs get ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs comments edit`

Edit a document comment you authored. The comment ref stays stable.

```text
Local Help: docs comments edit

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Edit a document comment you authored. The comment ref stays stable.
- Composition: Only the original author may edit. Deep-links keep working because `ref` does not change.
- JSON body: PATCH `/docs/{document_id}/comments/{comment_id}` with `{ text }`.
- Examples:
  - `anx docs comments edit doc:runbook event:note --body "Corrected"`

Flags:
  <doc>                        Document ref, handle, or id.
  <comment>                    Comment ref (`event:<handle>`) or id.
  --body <text>                Replacement comment text.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent.

Generated Help: docs comments edit

- Command ID: `docs.comments.update`
- CLI path: `docs comments edit`
- HTTP: `PATCH /docs/{document_id}/comments/{comment_id}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Edit a comment you authored without changing its stable ref.
- Output: Returns `{ comment }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `forbidden`
- Concepts: `docs`, `write`
- Agent notes: Updates the comment body in place. Only the original author may edit. The comment `ref`/`handle` stay the same so UI deep-links remain valid across edits and document revisions.
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Edit own comment: `anx docs comments edit doc:runbook event:note --body "Corrected"`

Inputs:
  Required:
  - path `document_id`
  - path `comment_id`
  - body `text` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs comments edit ... ; anx --json docs comments edit ... ; anx docs comments edit ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs comments delete`

Delete a document comment you authored.

```text
Local Help: docs comments delete

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Delete a document comment you authored.
- Composition: Only the original author may delete. The comment is trashed on the backing thread.
- JSON body: DELETE `/docs/{document_id}/comments/{comment_id}`.
- Examples:
  - `anx docs comments delete doc:runbook event:note`

Flags:
  <doc>                        Document ref, handle, or id.
  <comment>                    Comment ref (`event:<handle>`) or id.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent.

Generated Help: docs comments delete

- Command ID: `docs.comments.delete`
- CLI path: `docs comments delete`
- HTTP: `DELETE /docs/{document_id}/comments/{comment_id}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Remove a comment you authored from the document discussion.
- Output: Returns `{ comment }` with the trashed comment.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `forbidden`
- Concepts: `docs`, `write`
- Agent notes: Trashes the backing `message_posted` event. Only the original author may delete. The comment ref stays stable; list omits trashed comments.
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Delete own comment: `anx docs comments delete doc:runbook event:note`

Inputs:
  Required:
  - path `document_id`
  - path `comment_id`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs comments delete ... ; anx --json docs comments delete ... ; anx docs comments delete ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards create`

Create a board work card from flags plus a local prose file, or from advanced JSON.

```text
Generated Help: cards create

- Command ID: `cards.create`
- CLI path: `cards create`
- HTTP: `POST /cards`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Create a card by supplying board_ref or board_handle in the request body. Use this canonical card workflow path for single-card creation.
- Output: Returns `{ board, card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `boards`, `write`
- Adjacent commands: `cards archive`, `cards get`, `cards history`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards revise`, `cards revision get`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - body `card.title` (string)
  Optional:
  - body `actor_id` (string)
  - body `board_handle` (string)
  - body `board_id` (string)
  - body `board_ref` (any)
  - body `card.after_card_id` (string)
  - body `card.assignee_refs` (list<any>)
  - body `card.before_card_id` (string)
  - body `card.column_key` (string)
  - body `card.definition_of_done` (list<string>)
  - body `card.document_ref` (string)
  - body `card.due_at` (datetime)
  - body `card.handle` (string)
  - body `card.id` (string)
  - body `card.provenance.by_field` (object)
  - body `card.provenance.notes` (string)
  - body `card.provenance.sources` (list<string>)
  - body `card.related_refs` (list<any>)
  - body `card.resolution` (string)
  - body `card.resolution_refs` (list<any>)
  - body `card.risk` (string)
  - body `card.summary` (string)
  - body `card.topic_ref` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get <board-ref-or-handle>`, `anx boards workspace <board-ref-or-handle>`, or the latest board mutation response.
  - body `request_key` (string)
  Enum values: card.column_key: backlog, blocked, done, in_progress, ready, review; card.resolution: done; card.risk: critical, high, low, medium

Local Help: cards create

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Create a board work card from flags plus a local prose file, or from advanced JSON.
- Composition: Builds the `cards.create` request. For normal agent work, draft the card summary/body locally and pass `--body-file` so the CLI can fill the stable Card envelope.
- JSON body: Either flags plus `--body`/`--body-file`, or advanced JSON body `{ board_id, card }` from stdin/--from-file.
- Examples:
  - `anx cards create --board board:<board-handle> --topic topic:<topic-handle> --title "Implement login" --body-file card.md`
  - `anx cards create --board board:<board-handle> --title "Implement login" --body "Inline summary"`
  - `anx cards create --board board:<board-handle> --title "Implement login" --body-file card.md --assignee-ref actor:<actor-handle>`
  - `cat card-create.json | anx cards create`

Flags:
  --board <board-ref-or-handle> Board typed ref or handle for the new work card.
  --title <text>               Card title.
  --body <text>                Inline card summary/body text.
  --body-file <path>           Load card summary/body text from a local file, or stdin with `-`.
  --topic <topic-ref-or-handle> Related topic typed ref or handle.
  --column <key>               Initial board column; defaults to backlog.
  --assignee-ref <typed-ref>   Assignee actor ref, repeatable.
  --document-ref <typed-ref>   Pinned document ref for the card.
  --ref <typed-ref>            Additional related typed ref, repeatable.
  --done <text>                Definition-of-done checklist item, repeatable.
  --from-file <path>           Advanced JSON request body from file.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards create ... ; anx --json cards create ... ; anx cards create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards patch`

Patch card metadata from scalar flags, or from advanced JSON.

```text
Generated Help: cards patch

- Command ID: `cards.patch`
- CLI path: `cards patch`
- HTTP: `PATCH /cards/{card_id}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Update card fields, including resolution and resolution refs.
- Output: Returns `{ card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `write`, `concurrency`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards history`, `cards list`, `cards move`, `cards purge`, `cards restore`, `cards revise`, `cards revision get`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  - body `if_updated_at` (datetime): Optimistic concurrency token. Read the latest value from the corresponding read command before mutating.
  Optional:
  - body `actor_id` (string)
  - body `patch.assignee_refs` (list<any>)
  - body `patch.document_ref` (string)
  - body `patch.due_at` (datetime)
  - body `patch.provenance.by_field` (object)
  - body `patch.provenance.notes` (string)
  - body `patch.provenance.sources` (list<string>)
  - body `patch.related_refs` (list<any>)
  - body `patch.resolution` (string)
  - body `patch.resolution_refs` (list<any>)
  - body `patch.risk` (string)
  - body `patch.topic_ref` (string)
  Enum values: patch.resolution: done; patch.risk: critical, high, low, medium

Local Help: cards patch

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Patch card metadata from scalar flags, or from advanced JSON.
- Composition: Fetches the Card to discover `updated_at` when `--if-updated-at` is omitted. Use `cards move` for board placement changes.
- JSON body: Either flags building `{ patch, if_updated_at }`, or advanced JSON body from stdin/--from-file.
- Examples:
  - `anx cards patch <card-id> --title "Implement login"`
  - `anx cards patch <card-id> --summary "Updated scope" --if-updated-at <updated_at>`
  - `cat card-patch.json | anx cards patch <card-id>`

Flags:
  <card-id>                    Card id or unique prefix to patch.
  --title <text>               Card title.
  --summary <text>             Card summary/body.
  --column-key <key>           Accepted for guidance only; use `anx cards move --column <key>` for placement.
  --if-updated-at <timestamp>  Optimistic concurrency token; discovered from cards get when omitted.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --from-file <path>           Advanced JSON request body from file.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards patch ... ; anx --json cards patch ... ; anx cards patch ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards message`

Post a message to a Card conversation without hand-authoring event JSON.

```text
Local Help: cards message

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Post a message to a Card conversation without hand-authoring event JSON.
- Composition: Fetches the Card to discover its backing thread and board, then writes a visible `message_posted` event. Use this for card status updates, implementation notes, and ordinary discussion.
- JSON body: Builds an `events.create` body with `event.type=message_posted`, card/thread/board refs, derived agent actor, and payload text.
- Examples:
  - `anx cards message card:implement-login --body "Implemented in 0729e75"`
  - `anx cards message card:implement-login --body-file update.md`
  - `cat update.md | anx cards message card:implement-login`

Flags:
  <ref>                        Card ref, handle, or id to message.
  --body <text>                Message body text.
  --body-file <path>           Load message body text from a local file.
  --summary <text>             Optional short event summary.
  --ref <typed-ref>            Additional typed ref, repeatable.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards message ... ; anx --json cards message ... ; anx cards message ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards messages`

List message_posted events from a Card conversation.

```text
Local Help: cards messages

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: List message_posted events from a Card conversation.
- Composition: Fetches the Card, then reads its backing thread timeline and filters to ordinary messages. Use `cards timeline` when you need lifecycle events too.
- JSON body: Fetches the Card backing thread and returns an `events.list`-style filtered timeline slice with card metadata.
- Examples:
  - `anx cards messages card:implement-login`
  - `anx cards messages card:implement-login --max-events 5 --mine`
  - `anx cards messages card:implement-login --full-id`

Flags:
  <ref>                        Card ref, handle, or id whose messages should be listed.
  --max-events <n>             Return at most N most-recent matching messages.
  --mine                       Filter to messages authored by the resolved agent actor_id.
  --actor-id <actor-id>        Filter to one actor id.
  --full-id                    (debug/admin) Render full event ids in default text output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards messages ... ; anx --json cards messages ... ; anx cards messages ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards reply`

Reply to an existing Card message.

```text
Local Help: cards reply

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Reply to an existing Card message.
- Composition: Fetches the Card and validates the target message exists on its backing thread before posting the reply.
- JSON body: Builds an `events.create` body like `cards message` and adds `payload.reply_to_event_id` plus an `event:launch-update` ref.
- Examples:
  - `anx cards reply card:implement-login --to <message-id> --body "Confirmed"`
  - `anx cards reply card:implement-login --to <message-id> --body-file reply.md`

Flags:
  <ref>                        Card ref, handle, or id to reply on.
  --to <message-id>            Message/event id, typed ref, or handle being replied to.
  --body <text>                Reply body text.
  --body-file <path>           Load reply body text from a local file.
  --summary <text>             Optional short event summary.
  --ref <typed-ref>            Additional typed ref, repeatable.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards reply ... ; anx --json cards reply ... ; anx cards reply ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards revise`

Revise a card title and/or summary/body from local files without hand-authoring patch JSON.

```text
Generated Help: cards revise

- Command ID: `cards.revisions.create`
- CLI path: `cards revise`
- HTTP: `POST /cards/{card_id}/revisions`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Append a new immutable card content revision and advance the card head.
- Output: Returns `{ card, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `revisions`, `write`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards history`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards revision get`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  - body `if_base_revision` (string): Optimistic concurrency token. Copy the current head revision ref from `anx cards get <card-ref-or-handle>` before updating.
  - body `revision.summary` (string)
  - body `revision.title` (string)
  Optional:
  - body `actor_id` (string)
  - body `revision.definition_of_done` (list<string>)
  - body `revision.provenance.by_field` (object)
  - body `revision.provenance.notes` (string)
  - body `revision.provenance.sources` (list<string>)
  - body `revision.refs` (list<any>)

Local Help: cards revise

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Revise a card title and/or summary/body from local files without hand-authoring patch JSON.
- Composition: Fetches the card when needed for optimistic concurrency, then sends `cards.revisions.create` with `summary` from `--body-file` and optional `title`.
- JSON body: `{ if_base_revision, revision: { title?, summary?, definition_of_done? }, actor_id? }`; discovers `if_base_revision` from `cards get` when omitted.
- Examples:
  - `anx cards revise card:implement-login --body-file card.md`
  - `anx cards revise card:implement-login --title "Updated title" --body-file card.md`
  - `cat card.md | anx cards revise card:implement-login --body-file -`
  - `anx cards revise card:implement-login --from-file card-revision.json`

Flags:
  <ref>                        Card ref, handle, or id to revise.
  --body-file <path>           Load revised card summary/body text from a local file or stdin with `-`.
  --title <text>               Optional revised card title.
  --if-base-revision <revision-id> Base card revision id; discovered when omitted.
  --from-file <path>           Advanced JSON revision request body from file.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards revise ... ; anx --json cards revise ... ; anx cards revise ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads message`

Escape hatch: post a message directly to a backing thread.

```text
Local Help: threads message

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Escape hatch: post a message directly to a backing thread.
- Composition: Writes directly to a backing thread. Prefer domain commands such as `cards message`, `topics message`, or `docs message` when you are working from a Card, Topic, or Doc.
- JSON body: Builds an `events.create` body with `event.type=message_posted`, `event.thread_id`, thread ref, derived agent actor, and payload text.
- Examples:
  - `anx debug threads message <thread-id> --body-file note.md`
  - `anx debug threads message <thread-id> --body "Diagnostic note"`

Flags:
  <thread-id>                  Thread id, typed ref, or handle to message.
  --body <text>                Message body text.
  --body-file <path>           Load message body text from a local file.
  --summary <text>             Optional short event summary.
  --ref <typed-ref>            Additional typed ref, repeatable.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug threads message ... ; anx --json debug threads message ... ; anx debug threads message ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads reply`

Escape hatch: reply to an existing message on a backing thread.

```text
Local Help: threads reply

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Escape hatch: reply to an existing message on a backing thread.
- Composition: Validates the target message exists on the thread before posting the reply.
- JSON body: Builds an `events.create` body like `threads message` and adds `payload.reply_to_event_id` plus an `event:launch-update` ref.
- Examples:
  - `anx debug threads reply <thread-id> --to <message-id> --body "Confirmed"`
  - `anx debug threads reply <thread-id> --to <message-id> --body-file reply.md`

Flags:
  <thread-id>                  Thread id, typed ref, or handle to reply on.
  --to <message-id>            Message/event id, typed ref, or handle being replied to.
  --body <text>                Reply body text.
  --body-file <path>           Load reply body text from a local file.
  --summary <text>             Optional short event summary.
  --ref <typed-ref>            Additional typed ref, repeatable.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug threads reply ... ; anx --json debug threads reply ... ; anx debug threads reply ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards move`

Move a card to another board column using Card workflow language.

```text
Generated Help: cards move

- Command ID: `cards.move`
- CLI path: `cards move`
- HTTP: `POST /cards/{card_id}/move`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Reposition a card within a board column using the card's first-class identity.
- Output: Returns `{ card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `boards`, `write`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards history`, `cards list`, `cards patch`, `cards purge`, `cards restore`, `cards revise`, `cards revision get`, `cards timeline`, `cards trash`

Inputs:
  Required:
  - path `card_id`
  - body `column_key` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get <board-ref-or-handle>`, `anx boards workspace <board-ref-or-handle>`, or the latest board mutation response.
  Optional:
  - body `actor_id` (string)
  - body `after_card_id` (string)
  - body `before_card_id` (string)
  - body `resolution` (string)
  - body `resolution_refs` (list<any>)
  Enum values: column_key: backlog, blocked, done, in_progress, ready, review; resolution: done

Local Help: cards move

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Move a card to another board column using Card workflow language.
- Composition: Fetches the card and parent board when needed for optimistic concurrency, then sends `cards.move`.
- JSON body: `{ column_key, if_board_updated_at, actor_id? }`; discovers the board concurrency token when omitted.
- Examples:
  - `anx cards move card:implement-login --column review`
  - `anx cards move card:implement-login --column blocked --if-board-updated-at <updated-at>`

Flags:
  <ref>                        Card ref, handle, or id to move.
  --column <key>               Target board column.
  --if-board-updated-at <timestamp> Board optimistic concurrency token; discovered when omitted.
  --from-file <path>           Advanced JSON move request body from file.
  --dry-run                    Validate and render the request without sending it.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards move ... ; anx --json cards move ... ; anx cards move ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards assign`

Replace card assignees with explicit actor refs, or clear them.

```text
Local Help: cards assign

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Replace card assignees with explicit actor refs, or clear them.
- Composition: Builds a focused `cards.patch` request for the Card ownership field.
- JSON body: `{ patch: { assignee_refs }, if_updated_at, actor_id? }`; discovers `if_updated_at` from `cards get` when omitted.
- Examples:
  - `anx cards assign card:implement-login --assignee-ref actor:agent-alpha`
  - `anx cards assign card:implement-login --clear`

Flags:
  <ref>                        Card ref, handle, or id to assign.
  --assignee-ref <typed-ref>   Assignee actor typed ref, repeatable.
  --clear                      Clear all assignees.
  --if-updated-at <timestamp>  Card optimistic concurrency token; discovered when omitted.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards assign ... ; anx --json cards assign ... ; anx cards assign ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards resolve`

Resolve a card into the done column with optional free-text evidence.

```text
Local Help: cards resolve

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Resolve a card into the done column with optional free-text evidence.
- Composition: With evidence from `--body`, `--body-file`, or `--resolution-ref`, posts a card message first when a body is supplied, then passes resolution refs (including the new `event:<id>` when posted) into `cards.move`. `--reason` stamps the lifecycle audit only.
- JSON body: `{ column_key: "done", resolution, resolution_refs, if_board_updated_at, actor_id? }`; discovers the board concurrency token when omitted.
- Examples:
  - `anx cards resolve card:implement-login --reason "ok" --body "Validated in staging"`
  - `anx cards resolve card:implement-login --resolution-ref event:<event-id>`
  - `anx cards resolve card:implement-login --column done --body-file evidence.md`
  - `cat note.md | anx cards resolve card:implement-login --body-file -`

Flags:
  <ref>                        Card ref, handle, or id to resolve.
  --column <key>               Target board column, default done.
  --reason <text>              Short audit string stamped on the resolve request (not posted to the backing thread).
  --resolution-ref <typed-ref> Evidence event/artifact typed ref, repeatable.
  --body <text>                Post inline evidence to the card thread before resolving.
  --body-file <path>           Load evidence text from a file before resolving.
  --summary <text>             Optional short evidence event summary.
  --resolution <value>         Resolution value, default done.
  --if-board-updated-at <timestamp> Board optimistic concurrency token; discovered when omitted.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --from-file <path>           Advanced JSON move request body from file.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards resolve ... ; anx --json cards resolve ... ; anx cards resolve ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards reopen`

Move a resolved card back into active workflow.

```text
Local Help: cards reopen

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Move a resolved card back into active workflow.
- Composition: Builds a focused `cards.move` request. The default reopened column is `ready`.
- JSON body: `{ column_key, if_board_updated_at, actor_id? }`; discovers the board concurrency token when omitted.
- Examples:
  - `anx cards reopen card:implement-login`
  - `anx cards reopen card:implement-login --column backlog`

Flags:
  <ref>                        Card ref, handle, or id to reopen.
  --column <key>               Target reopened column; defaults to ready.
  --if-board-updated-at <timestamp> Board optimistic concurrency token; discovered when omitted.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards reopen ... ; anx --json cards reopen ... ; anx cards reopen ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `cards trash`

Trash a card with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).

```text
Generated Help: cards trash

- Command ID: `cards.trash`
- CLI path: `cards trash`
- HTTP: `POST /cards/{card_id}/trash`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move a card to trash with an explicit operator reason while keeping archive lifecycle distinct.
- Output: Returns `{ board, card }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `cards`, `write`
- Adjacent commands: `cards archive`, `cards create`, `cards get`, `cards history`, `cards list`, `cards move`, `cards patch`, `cards purge`, `cards restore`, `cards revise`, `cards revision get`, `cards timeline`

Inputs:
  Required:
  - path `card_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)
  - body `if_board_updated_at` (datetime): Optimistic concurrency token. Copy `board.updated_at` from `anx boards get <board-ref-or-handle>`, `anx boards workspace <board-ref-or-handle>`, or the latest board mutation response.

Local Help: cards trash

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Trash a card with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).
- Composition: Uses the shared lifecycle parser (`lifecycle_spec.go`). Routine trashing prefers `--reason`; `--from-file` remains the advanced compatibility path.
- JSON body: `{ reason, actor_id?, ... }` from `--from-file`, merged with `--reason` / `--actor-id` flags.
- Examples:
  - `anx cards trash card:implement-login --reason "duplicate"`
  - `cat trash.json | anx cards trash card:implement-login --from-file=-`

Flags:
  <ref>                        Card ref, handle, or id to trash.
  --reason <text>              Reason for trashing the card.
  --from-file <path>           Advanced JSON request body from file or stdin (`-`).
  --actor-id <actor-id>        Actor id; overlays JSON and defaults from resolved agent when omitted.
  --dry-run                    Validate and render the request without sending it.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx cards trash ... ; anx --json cards trash ... ; anx cards trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events list`

Compose backing-thread timeline reads with client-side thread/type/actor filters and preview summaries.

```text
Generated Help: events list

- Command ID: `events.list`
- CLI path: `debug events list`
- HTTP: `GET /events`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect append-only event history across the workspace.
- Output: Returns `{ events, page_info }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Concepts: `events`
- Adjacent commands: `debug events archive`, `debug events create`, `debug events get`, `debug events restore`, `debug events stream`, `debug events trash`, `debug events unarchive`

Local Help: events list

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Compose backing-thread timeline reads with client-side thread/type/actor filters and preview summaries.
- Composition: Fetches one or more backing-thread timelines locally, then filters and summarizes the events without changing contracts or core behavior. Use it as a diagnostic read; prefer `topics workspace` and card/board reads for normal coordination.
- JSON body: `thread_id`, `thread_ids`, `events`, `total_events`, `returned_events`
- Examples:
  - `anx debug events list --thread-id <thread-id> --type message_posted --mine --full-id`
  - `anx debug events list --thread-id <thread-id> --max-events 10`

Flags:
  --thread-id <thread-id>      Thread id to inspect (repeatable).
  --type <event-type>          Repeatable event type filter.
  --types <csv>                Comma-separated event types.
  --actor-id <actor-id>        Filter to one actor id.
  --mine                       Resolve to the resolved agent actor_id.
  --max-events <n>             Keep the most recent matching events.
  --max <n>                    Alias for --max-events.
  --full-id                    (debug/admin) Render full event ids in default text output (non-JSON).
  --include-archived           Include archived events in results.
  --archived-only              Show only archived events.
  --include-trashed            Include trashed events in results.
  --trashed-only               Show only trashed events.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events list ... ; anx --json debug events list ... ; anx debug events list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events validate`

Validate an `events create` payload locally from stdin or `--from-file` without sending it.

```text
Local Help: events validate

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Validate an `events create` payload locally from stdin or `--from-file` without sending it.
- Composition: Parses the same JSON body accepted by `events create`, runs local validation rules, and returns a validation preview envelope without contacting core.
- JSON body: `command`, `command_id`, `path_params`, `query`, `body`, `valid`
- Examples:
  - `cat event.json | anx debug events validate`
  - `anx debug events validate --from-file event.json`

Flags:
  --from-file <path>           Load the request body from a JSON file instead of stdin.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events validate ... ; anx --json debug events validate ... ; anx debug events validate ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events explain`

Explain known event-type conventions, required refs, and validation hints, including when `message_posted` targets a backing-thread message stream.

```text
Local Help: events explain

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Explain known event-type conventions, required refs, and validation hints, including when `message_posted` targets a backing-thread message stream.
- Composition: Formats the embedded event reference and validation guidance into a plain-text reference without sending a request. Use it to confirm when `message_posted` is required for a visible backing-thread message in the web UI Messages tab.
- JSON body: `event_type`, `known`, `required_refs`, `payload_requirements`, `examples`, `hint`
- Examples:
  - `anx debug events explain`
  - `anx debug events explain message_posted`

Flags:
  <event-type>                 Optional event type to focus on; omit it to list known event types.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug events explain ... ; anx --json debug events explain ... ; anx debug events explain ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts create`

Create an artifact; use --file/--ref for attachment uploads or JSON for advanced artifact bodies.

```text
Generated Help: artifacts create

- Command ID: `artifacts.create`
- CLI path: `artifacts create`
- HTTP: `POST /artifacts`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Store content-addressed artifact metadata and payload (bytes, text, or structured JSON).
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `conflict`
- Concepts: `artifacts`, `write`
- Adjacent commands: `artifacts archive`, `artifacts attachments create`, `artifacts content`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Inputs:
  Required:
  - body `artifact` (object)
  - body `content_type` (string)
  Optional:
  - body `actor_id` (string)
  - body `content` (any)

Local Help: artifacts create

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Create an artifact; use --file/--ref for attachment uploads or JSON for advanced artifact bodies.
- Composition: Routes the common file attachment path through the multipart attachment endpoint while preserving the contract-level JSON create path for text, structured, or binary artifact bodies.
- JSON body: With --file, posts multipart attachment data to `artifacts.attachments.create`; without --file, sends advanced JSON `{ artifact, content_type, content }` to `artifacts.create`.
- Examples:
  - `anx artifacts create --file ./photo.jpg --ref topic:launch`
  - `anx artifacts create --file ./evidence.pdf --ref card:launch-evidence --summary "Evidence"`
  - `cat artifact.json | anx artifacts create`

Flags:
  --file <path>                Upload a local file as kind=attachment via multipart form.
  --ref <typed-ref>            Typed ref to attach to, repeatable.
  --refs <json>                Compatibility form: JSON array of typed refs.
  --summary <text>             Optional attachment summary.
  --artifact <json>            Optional JSON object merged into attachment metadata; refs and kind are ignored by the server.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --from-file <path>           Advanced JSON artifact create body from file; cannot be combined with --file.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts create ... ; anx --json artifacts create ... ; anx artifacts create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts attachments create`

Upload a local file as an attachment artifact via multipart form.

```text
Generated Help: artifacts attachments create

- Command ID: `artifacts.attachments.create`
- CLI path: `artifacts attachments create`
- HTTP: `POST /artifacts/attachments`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `multipart-form`
- Why: Create kind=attachment via multipart form (efficient binary upload; previews use GET /artifacts/{artifact_id}/content, where the path segment accepts artifact ref or handle).
- Output: Returns `{ artifact }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `conflict`, `unsupported_mime`, `payload_too_large`
- Concepts: `artifacts`, `write`
- Agent notes: Multipart/form-data upload; generated HTTP Invoke helpers JSON-encode bodies and are not suitable—use UI, curl -F, or a multipart-aware client.
- Adjacent commands: `artifacts archive`, `artifacts content`, `artifacts create`, `artifacts get`, `artifacts list`, `artifacts purge`, `artifacts restore`, `artifacts trash`, `artifacts unarchive`

Local Help: artifacts attachments create

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Upload a local file as an attachment artifact via multipart form.
- Composition: Posts to `POST /artifacts/attachments`, which stores the file bytes and creates kind=attachment artifact metadata.
- JSON body: Multipart form fields: `refs`, `file`, optional `summary`, `artifact`, and `actor_id`.
- Examples:
  - `anx artifacts attachments create --file ./notes.md --ref thread:<thread-id>`
  - `anx artifacts attachments create --file ./photo.jpg --refs '["topic:launch"]'`

Flags:
  --file <path>                Path to the file to upload.
  --ref <typed-ref>            Typed ref to attach to, repeatable.
  --refs <json>                Compatibility form: JSON array of typed refs.
  --summary <text>             Optional attachment summary.
  --artifact <json>            Optional JSON object merged into attachment metadata; refs and kind are ignored by the server.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts attachments create ... ; anx --json artifacts attachments create ... ; anx artifacts attachments create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts inspect`

Fetch artifact metadata and resolved content in one command for operator inspection.

```text
Local Help: artifacts inspect

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Fetch artifact metadata and resolved content in one command for operator inspection.
- Composition: Loads artifact metadata, then fetches content with `artifacts content` using the resolved artifact id.
- JSON body: `artifact`, `content`, `content_headers`, `content_text`, `content_base64`
- Examples:
  - `anx artifacts inspect artifact:notes`
  - `anx artifacts inspect <artifact-ref-or-alias>`

Flags:
  --artifact-id <ref>          Artifact ref or handle to inspect.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx artifacts inspect ... ; anx --json artifacts inspect ... ; anx artifacts inspect ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads inspect`

Diagnostic backing-thread bundle: compose one view from read-only thread data and related `inbox list` items.

```text
Generated Help: threads inspect

- Command ID: `threads.inspect`
- CLI path: `debug threads inspect`
- HTTP: `GET /threads/{thread_id}`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one backing thread for low-level inspection and diagnostics.
- Output: Returns `{ thread }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `threads`, `inspection`
- Adjacent commands: `debug threads context`, `debug threads list`, `debug threads timeline`, `debug threads workspace`

Inputs:
  Required:
  - path `thread_id`

Local Help: threads inspect

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Diagnostic backing-thread bundle: compose one view from read-only thread data and related `inbox list` items.
- Composition: Resolves one thread by id or discovery filters, loads read-only thread projections, then filters inbox items client-side by `thread_id`. Prefer `topics workspace` for agent-facing topic context when you have a topic id. The operator work projection is `work.list` / `work.get`.
- JSON body: `thread`, `context`, `collaboration`, `inbox`
- Examples:
  - `anx debug threads inspect --thread-id <thread-id>`
  - `anx debug threads inspect --state active --full-id`

Flags:
  --thread-id <thread-id>      Thread id to inspect.
  --state <state>              Discover one thread by lifecycle state (active, archived, trashed).
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from the underlying read-only thread views.
  --full-id                    (debug/admin) Render full event and inbox ids in default text output (non-JSON).

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug threads inspect ... ; anx --json debug threads inspect ... ; anx debug threads inspect ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads workspace`

Read-only backing-thread workspace projection: context, inbox, board membership, and related-thread signals in one command.

```text
Generated Help: threads workspace

- Command ID: `threads.workspace`
- CLI path: `debug threads workspace`
- HTTP: `GET /threads/{thread_id}/workspace`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Read-only diagnostic projection that bundles context, inbox, and related-thread signals for one backing thread. Prefer topics.workspace for normal operator coordination when a topic exists.
- Output: Returns `{ thread, related_topics, cards, documents, board_memberships, inbox, projection_freshness }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `threads`, `workspace`
- Adjacent commands: `debug threads context`, `debug threads inspect`, `debug threads list`, `debug threads timeline`

Inputs:
  Required:
  - path `thread_id`

Local Help: threads workspace

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Read-only backing-thread workspace projection: context, inbox, board membership, and related-thread signals in one command.
- Composition: Resolves one thread by id or discovery filters, loads read-only thread projections, adds thread-scoped inbox items, and follows related thread refs for diagnostic review. Prefer `topics workspace` for agent-facing topic context. The operator work projection is `work.list` / `work.get`.
- JSON body: `thread`, `context`, `collaboration`, `inbox`, `pending_attention`, `related_threads`, `follow_up`
- Examples:
  - `anx debug threads workspace --thread-id <thread-id> --full-id`
  - `anx debug threads workspace --state active`

Flags:
  --thread-id <thread-id>      Thread id to inspect.
  --state <state>              Discover one thread by lifecycle state (active, archived, trashed).
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from the underlying read-only thread views.
  --full-id                    (debug/admin) Render full event and inbox ids in default text output (non-JSON).

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug threads workspace ... ; anx --json debug threads workspace ... ; anx debug threads workspace ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards workspace`

Canonical board read path: load one board's workspace: optional primary topic, cards by column, linked documents, inbox items, and summary.

```text
Generated Help: boards workspace

- Command ID: `boards.workspace`
- CLI path: `boards workspace`
- HTTP: `GET /boards/{board_id}/workspace`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Load the operator-facing board workspace with cards, docs, and inbox sections.
- Output: Returns `{ board, primary_topic, cards, documents, inbox, board_summary, projection_freshness, board_summary_freshness, warnings, section_kinds, generated_at }`.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `boards`, `workspace`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards cards list`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`

Inputs:
  Required:
  - path `board_id`

Local Help: boards workspace

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Canonical board read path: load one board's workspace: optional primary topic, cards by column, linked documents, inbox items, and summary.
- Composition: Resolves a board by typed ref or handle, fetches the projection workspace with per-card thread backing, and renders cards grouped by canonical column order (backlog, ready, in_progress, blocked, review, done).
- JSON body: `board_id`, `board`, `primary_topic`, `cards`, `documents`, `inbox`, `board_summary`, `projection_freshness`, `board_summary_freshness`, `warnings`, `section_kinds`, `generated_at`
- Examples:
  - `anx boards workspace board:<board-handle>`
  - `anx boards workspace board_product_launch`

Flags:
  <board-ref-or-handle>        Board typed ref or handle to load.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards workspace ... ; anx --json boards workspace ... ; anx boards workspace ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `boards cards list`

List all cards on a board in canonical column order without hydrating thread details.

```text
Generated Help: boards cards list

- Command ID: `boards.cards.list`
- CLI path: `boards cards list`
- HTTP: `GET /boards/{board_id}/cards`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: List cards on one board in canonical order.
- Output: Returns `{ board_ref, board_handle, cards }`; internal board_id may appear for admin/debug compatibility.
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Concepts: `boards`, `cards`
- Adjacent commands: `boards archive`, `boards cards create-batch`, `boards cards get`, `boards create`, `boards get`, `boards list`, `boards patch`, `boards purge`, `boards restore`, `boards trash`, `boards unarchive`, `boards workspace`

Inputs:
  Required:
  - path `board_id`

Local Help: boards cards list

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: List all cards on a board in canonical column order without hydrating thread details.
- Composition: Fetches the raw card list for a board ordered by canonical column sequence and per-column rank. Default text leads with card refs and titles; thread refs are secondary context.
- JSON body: `board_id`, `cards`
- Examples:
  - `anx boards cards list board:<board-handle>`
  - `anx boards cards list board:<board-handle> --full-id`

Flags:
  <board-ref-or-handle>        Board typed ref or handle to list cards for.
  --full-id                    (debug/admin) Render full card ids in default text output.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx boards cards list ... ; anx --json boards cards list ... ; anx boards cards list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `workspace summary`

First-run workspace orientation: boards plus compact card/doc/inbox counts.

```text
Local Help: workspace summary

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: First-run workspace orientation: boards plus compact card/doc/inbox counts.
- Composition: Local CLI helper that composes existing list reads without changing the core contract. Boards are required; card, document, and inbox counts are best-effort and surface warnings on partial read failures.
- JSON body: `boards`, `counts`, `generated_at`, optional `warnings`
- Examples:
  - `anx workspace summary`
  - `anx --json workspace summary`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx workspace summary ... ; anx --json workspace summary ... ; anx workspace summary ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs revise`

Revise a durable document from a local file or JSON body; stages a diff proposal by default.

```text
Local Help: docs revise

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Revise a durable document from a local file or JSON body; stages a diff proposal by default.
- Quick start: Flags: `docs revise <doc-ref> --body-file <path>` stages a proposal; add `--apply` to write immediately.
- Composition: Fetches the current document revision, discovers the base revision when omitted, computes a local diff, and stages a proposal. Add `--apply` to direct-write the revision; use `--apply --proposal-id <id>` to apply a staged proposal.
- JSON body: Proposal mode returns `proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`; `--apply` sends the revision immediately or applies a staged proposal.
- Examples:
  - `anx docs revise doc:runbook --body-file notes.md`
  - `anx docs revise --apply --proposal-id <proposal-id>`
  - `anx docs revise doc:runbook --apply --body-file notes.md`
  - `cat revision.json | anx docs revise doc:runbook`

Flags:
  <ref>                        Document ref, alias, or id to revise.
  --body-file <path>           Load revised Markdown/text content from a local file or stdin with `-`.
  --from-file <path>           Advanced JSON revision body from a file.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --apply                      Apply immediately, or apply a staged proposal when combined with --proposal-id.
  --proposal-id <proposal-id>  Staged proposal id to apply; must be combined with --apply.
  --propose                    Stage a proposal (default; included for explicitness).

Generated Help: docs revise

- Command ID: `docs.revisions.create`
- CLI path: `docs revise`
- HTTP: `POST /docs/{document_id}/revisions`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Append a new immutable revision and advance the document head.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `revisions`, `write`
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revision get`, `docs search`, `docs trash`, `docs unarchive`
- Examples:
  - Revise from a local file: `anx docs revise doc:runbook --body-file runbook.md`

Inputs:
  Required:
  - path `document_id`
  - body `content` (any)
  - body `content_type` (string)
  - body `if_base_revision` (string): Optimistic concurrency token. Copy the current head revision ref from `anx docs get <doc-ref-or-handle>` before updating.
  Optional:
  - body `actor_id` (string)
  - body `document` (object)
  - body `provenance.by_field` (object)
  - body `provenance.notes` (string)
  - body `provenance.sources` (list<string>)
  - body `refs` (list<any>)
  Enum values: content_type: binary, structured, text

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs revise ... ; anx --json docs revise ... ; anx docs revise ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs trash`

Trash a document lineage with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).

```text
Local Help: docs trash

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Trash a document lineage with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).
- Quick start: Flags: `docs trash <doc-ref> --reason <text>`; `--from-file <path>` remains the advanced JSON compatibility path.
- Composition: Uses the shared lifecycle parser (`lifecycle_spec.go`). Routine trashing prefers `--reason`; JSON input remains supported for existing automation.
- JSON body: `{ reason, actor_id?, ... }` from `--from-file`, merged with `--reason` / `--actor-id` flags.
- Examples:
  - `anx docs trash doc:runbook --reason "superseded"`
  - `cat trash.json | anx docs trash doc:runbook --from-file=-`

Flags:
  <ref>                        Document ref, handle, or id to trash.
  --reason <text>              Reason for trashing the document.
  --from-file <path>           Advanced JSON request body from file or stdin (`-`).
  --actor-id <actor-id>        Actor id; overlays JSON and defaults from resolved agent when omitted.
  --dry-run                    Validate and render the request without sending it.

Generated Help: docs trash

- Command ID: `docs.trash`
- CLI path: `docs trash`
- HTTP: `POST /docs/{document_id}/trash`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Move a document lineage to trash with an explicit operator reason.
- Output: Returns `{ document, revision }`.
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Concepts: `docs`, `write`
- Adjacent commands: `docs archive`, `docs comment`, `docs comments`, `docs comments delete`, `docs comments edit`, `docs comments reply`, `docs create`, `docs get`, `docs history`, `docs list`, `docs patch`, `docs purge`, `docs put`, `docs restore`, `docs revise`, `docs revision get`, `docs search`, `docs unarchive`
- Examples:
  - Trash a document: `anx docs trash doc:runbook --reason "obsolete"`

Inputs:
  Required:
  - path `document_id`
  - body `reason` (string)
  Optional:
  - body `actor_id` (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs trash ... ; anx --json docs trash ... ; anx docs trash ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs content`

Show the current document content together with authoritative head revision metadata.

```text
Local Help: docs content

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Show the current document content together with authoritative head revision metadata.
- Composition: Loads `docs get`, then renders the current revision content and metadata in one operator-friendly response.
- JSON body: `document`, `revision`, `content`, `status_code`, `headers`
- Examples:
  - `anx docs content doc:runbook`

Flags:
  <ref>                        Document ref, alias, or id to inspect.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs content ... ; anx --json docs content ... ; anx docs content ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs messages`

List messages from a Document conversation.

```text
Local Help: docs messages

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: List messages from a Document conversation.
- Composition: Fetches the Document, then reads its backing thread timeline and filters to messages attached to that document.
- JSON body: Fetches the Document backing thread and returns an `events.list`-style filtered timeline slice with document metadata.
- Examples:
  - `anx docs messages doc:runbook`
  - `anx docs messages doc:runbook --max-events 5 --mine`

Flags:
  <ref>                        Document ref, alias, or id.
  --max-events <n>             Return at most N most-recent matching messages.
  --mine                       Filter to messages authored by the resolved agent actor_id.
  --actor-id <actor-id>        Filter to one actor id.
  --full-id                    (debug/admin) Render full event ids in default text output.
  --include-archived           Include archived message events.
  --archived-only              Show only archived message events.
  --include-trashed            Include trashed message events.
  --trashed-only               Show only trashed message events.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs messages ... ; anx --json docs messages ... ; anx docs messages ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs message`

Post a message to a Document conversation without hand-authoring event JSON.

```text
Local Help: docs message

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Post a message to a Document conversation without hand-authoring event JSON.
- Quick start: Flags: `docs message <doc-ref> --body-file <path>` or `--body <text>` for short updates.
- Composition: Fetches the Document to discover its backing thread, then writes a visible `message_posted` event attached to that document.
- JSON body: Builds an `events.create` body with `event.type=message_posted`, document/thread refs, derived agent actor, and payload text.
- Examples:
  - `anx docs message doc:runbook --body-file note.md`
  - `anx docs message doc:runbook --body "Reviewed the current revision"`

Flags:
  <ref>                        Document ref, alias, or id to message.
  --body <text>                Message body text.
  --body-file <path>           Load message body text from a local file.
  --summary <text>             Optional short event summary.
  --ref <typed-ref>            Additional typed ref, repeatable.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs message ... ; anx --json docs message ... ; anx docs message ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs reply`

Reply to an existing Document message.

```text
Local Help: docs reply

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Reply to an existing Document message.
- Quick start: Flags: `docs reply <doc-ref> --to <message-id> --body-file <path>` or `--body <text>`.
- Composition: Fetches the Document and validates the target message exists on its backing thread before posting the reply.
- JSON body: Builds an `events.create` body like `docs message` and adds `payload.reply_to_event_id` plus an `event:launch-update` ref.
- Examples:
  - `anx docs reply doc:runbook --to <message-id> --body "Confirmed"`
  - `anx docs reply doc:runbook --to <message-id> --body-file reply.md`

Flags:
  <ref>                        Document ref, alias, or id to reply on.
  --to <message-id>            Message/event id, typed ref, or handle being replied to.
  --body <text>                Reply body text.
  --body-file <path>           Load reply body text from a local file.
  --summary <text>             Optional short event summary.
  --ref <typed-ref>            Additional typed ref, repeatable.
  --actor-id <actor-id>        Actor id; defaults from the resolved agent when available.
  --dry-run                    Validate and render the request without sending it.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx docs reply ... ; anx --json docs reply ... ; anx docs reply ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host enroll`

Enroll this machine with interactive approval or a one-time fleet token.

```text
Local Help: host enroll

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Enroll this machine with interactive approval or a one-time fleet token.
- Examples:
  - `anx host enroll --name host-b`
  - `anx --json host tokens create --label host-b --expires-in 1h | jq -er '.result.token' | ssh host-b 'anx host enroll --name host-b --token-stdin'`

Flags:
  --name <slug>                Workspace-local host slug.
  --token-stdin                Read the one-time enrollment token from piped stdin; mutually exclusive with --token and --plan.
  --token <secret>             Headless token (prefer --token-stdin to keep secrets out of argv).
  --exclude <name>             Leave this legacy profile standalone; repeatable.
  --plan                       Show the adoption plan without enrolling.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host enroll ... ; anx --json host enroll ... ; anx host enroll ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth admins list`

Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.

```text
Generated Help: auth admins list

- Command ID: `auth.admins.list`
- CLI path: `auth admins list`
- HTTP: `GET /auth/admins`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Manage explicit workspace administration authority for agents.
- Output: Returns `AuthAdminsResponse`.
- Error codes: `auth_required`, `invalid_token`, `auth_admin_required`
- Concepts: `auth`
- Agent notes: Only a human can change a grant. No default grant is assigned to agents or hosts.
- Adjacent commands: `auth admins grant`, `auth admins revoke`, `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`

Local Help: auth admins list

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- Examples:
  - `anx auth admins list`
  - `anx auth admins grant codex.host-a`
  - `anx auth admins revoke codex.host-a`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth admins list ... ; anx --json auth admins list ... ; anx auth admins list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth admins grant`

Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents. Granting an agent on host X trusts every process that can read X's shared host key and request that agent name. Agents cannot issue or revoke human invitations, revoke principals, or use the human lockout override.

```text
Generated Help: auth admins grant

- Command ID: `auth.admins.grant`
- CLI path: `auth admins grant`
- HTTP: `POST /auth/admins/{principal_id}/grant`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Manage explicit workspace administration authority for agents.
- Output: Returns `AuthAdminResponse`.
- Error codes: `auth_required`, `invalid_token`, `human_required`, `invalid_request`, `not_found`
- Concepts: `auth`
- Agent notes: Only a human can change a grant. No default grant is assigned to agents or hosts.
- Adjacent commands: `auth admins list`, `auth admins revoke`, `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`

Inputs:
  Required:
  - path `principal_id`

Local Help: auth admins grant

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents. Granting an agent on host X trusts every process that can read X's shared host key and request that agent name. Agents cannot issue or revoke human invitations, revoke principals, or use the human lockout override.
- Examples:
  - `anx auth admins list`
  - `anx auth admins grant codex.host-a`
  - `anx auth admins revoke codex.host-a`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth admins grant ... ; anx --json auth admins grant ... ; anx auth admins grant ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `auth admins revoke`

Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.

```text
Generated Help: auth admins revoke

- Command ID: `auth.admins.revoke`
- CLI path: `auth admins revoke`
- HTTP: `POST /auth/admins/{principal_id}/revoke`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Manage explicit workspace administration authority for agents.
- Output: Returns `AuthAdminResponse`.
- Error codes: `auth_required`, `invalid_token`, `human_required`, `invalid_request`, `not_found`
- Concepts: `auth`
- Agent notes: Only a human can change a grant. No default grant is assigned to agents or hosts.
- Adjacent commands: `auth admins grant`, `auth admins list`, `auth audit list`, `auth bootstrap status`, `auth invites create`, `auth invites list`, `auth invites revoke`, `auth passkey dev login`, `auth passkey dev register`, `auth passkey login options`, `auth passkey login verify`, `auth passkey register options`, `auth passkey register verify`, `auth principals list`, `auth principals revoke`, `auth token`

Inputs:
  Required:
  - path `principal_id`

Local Help: auth admins revoke

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- Examples:
  - `anx auth admins list`
  - `anx auth admins grant codex.host-a`
  - `anx auth admins revoke codex.host-a`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx auth admins revoke ... ; anx --json auth admins revoke ... ; anx auth admins revoke ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host enrollments list`

Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.

```text
Generated Help: host enrollments list

- Command ID: `hosts.enroll.pending`
- CLI path: `host enrollments list`
- HTTP: `GET /auth/hosts/enrollments/pending`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Review host enrollment requests.
- Output: Returns `{ enrollments }`.
- Error codes: `auth_required`, `invalid_token`, `auth_admin_required`
- Concepts: `auth`, `hosts`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `host bridge check-in`, `host enroll complete`, `host enroll headless`, `host enroll poll`, `host enroll start`, `host enrollments approve`, `host enrollments deny`, `host get`, `host list`, `host patch`, `host revoke`, `host tokens create`, `host tokens list`, `host tokens revoke`

Local Help: host enrollments list

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- Examples:
  - `anx host enrollments list`
  - `anx host enrollments approve ABCD-EFGH`
  - `anx host enrollments deny ABCD-EFGH`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host enrollments list ... ; anx --json host enrollments list ... ; anx host enrollments list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host enrollments approve`

Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.

```text
Generated Help: host enrollments approve

- Command ID: `hosts.enroll.approve`
- CLI path: `host enrollments approve`
- HTTP: `POST /auth/hosts/enrollments/{enrollment_id}/approve`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Approve a verified machine.
- Output: Returns `HostEnrollmentStatusResponse`.
- Error codes: `auth_required`, `invalid_token`, `auth_admin_required`, `not_found`, `enrollment_expired`, `enrollment_consumed`, `host_slug_taken`
- Concepts: `auth`, `hosts`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `host bridge check-in`, `host enroll complete`, `host enroll headless`, `host enroll poll`, `host enroll start`, `host enrollments deny`, `host enrollments list`, `host get`, `host list`, `host patch`, `host revoke`, `host tokens create`, `host tokens list`, `host tokens revoke`

Inputs:
  Required:
  - path `enrollment_id`

Local Help: host enrollments approve

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- Examples:
  - `anx host enrollments list`
  - `anx host enrollments approve ABCD-EFGH`
  - `anx host enrollments deny ABCD-EFGH`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host enrollments approve ... ; anx --json host enrollments approve ... ; anx host enrollments approve ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host enrollments deny`

Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents. Deny a pending request or cancel an approval before completion; list includes both statuses.

```text
Generated Help: host enrollments deny

- Command ID: `hosts.enroll.deny`
- CLI path: `host enrollments deny`
- HTTP: `POST /auth/hosts/enrollments/{enrollment_id}/deny`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Reject an untrusted machine.
- Output: Returns `HostEnrollmentStatusResponse`.
- Error codes: `auth_required`, `invalid_token`, `auth_admin_required`, `not_found`, `enrollment_expired`, `enrollment_consumed`
- Concepts: `auth`, `hosts`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `host bridge check-in`, `host enroll complete`, `host enroll headless`, `host enroll poll`, `host enroll start`, `host enrollments approve`, `host enrollments list`, `host get`, `host list`, `host patch`, `host revoke`, `host tokens create`, `host tokens list`, `host tokens revoke`

Inputs:
  Required:
  - path `enrollment_id`

Local Help: host enrollments deny

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents. Deny a pending request or cancel an approval before completion; list includes both statuses.
- Examples:
  - `anx host enrollments list`
  - `anx host enrollments approve ABCD-EFGH`
  - `anx host enrollments deny ABCD-EFGH`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host enrollments deny ... ; anx --json host enrollments deny ... ; anx host enrollments deny ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host tokens create`

Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.

```text
Generated Help: host tokens create

- Command ID: `hosts.tokens.create`
- CLI path: `host tokens create`
- HTTP: `POST /auth/hosts/enrollment-tokens`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Authorize one headless host enrollment.
- Output: Returns `{ enrollment_token, token }` once.
- Error codes: `auth_required`, `invalid_token`, `auth_admin_required`, `invalid_request`
- Concepts: `auth`, `hosts`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `host bridge check-in`, `host enroll complete`, `host enroll headless`, `host enroll poll`, `host enroll start`, `host enrollments approve`, `host enrollments deny`, `host enrollments list`, `host get`, `host list`, `host patch`, `host revoke`, `host tokens list`, `host tokens revoke`

Inputs:
  Required:
  - body `label` (string)
  Optional:
  - body `expires_at` (datetime)
  - body `expires_in_seconds` (integer)

Local Help: host tokens create

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- Examples:
  - `anx host tokens create --label fleet --expires-in 1h`
  - `anx host tokens list`
  - `anx host tokens revoke <token-id>`

Flags:
  --label <label>              Audit label for this one-time token.
  --expires-in <duration>      Lifetime from 10m to 24h; default 1h.

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host tokens create ... ; anx --json host tokens create ... ; anx host tokens create ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host tokens list`

Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.

```text
Generated Help: host tokens list

- Command ID: `hosts.tokens.list`
- CLI path: `host tokens list`
- HTTP: `GET /auth/hosts/enrollment-tokens`
- Side effect class: `read_only`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect headless host enrollment grants.
- Output: Returns `{ enrollment_tokens }` without secrets.
- Error codes: `auth_required`, `invalid_token`, `auth_admin_required`
- Concepts: `auth`, `hosts`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `host bridge check-in`, `host enroll complete`, `host enroll headless`, `host enroll poll`, `host enroll start`, `host enrollments approve`, `host enrollments deny`, `host enrollments list`, `host get`, `host list`, `host patch`, `host revoke`, `host tokens create`, `host tokens revoke`

Local Help: host tokens list

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- Examples:
  - `anx host tokens create --label fleet --expires-in 1h`
  - `anx host tokens list`
  - `anx host tokens revoke <token-id>`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host tokens list ... ; anx --json host tokens list ... ; anx host tokens list ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host tokens revoke`

Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.

```text
Generated Help: host tokens revoke

- Command ID: `hosts.tokens.revoke`
- CLI path: `host tokens revoke`
- HTTP: `POST /auth/hosts/enrollment-tokens/{token_id}/revoke`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Invalidate an unused headless grant.
- Output: Returns `{ enrollment_token }` without secret.
- Error codes: `auth_required`, `invalid_token`, `auth_admin_required`, `not_found`
- Concepts: `auth`, `hosts`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `host bridge check-in`, `host enroll complete`, `host enroll headless`, `host enroll poll`, `host enroll start`, `host enrollments approve`, `host enrollments deny`, `host enrollments list`, `host get`, `host list`, `host patch`, `host revoke`, `host tokens create`, `host tokens list`

Inputs:
  Required:
  - path `token_id`

Local Help: host tokens revoke

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Explicit workspace administration. Grants and revocations of auth-admin require a human; host administration accepts granted agents.
- Examples:
  - `anx host tokens create --label fleet --expires-in 1h`
  - `anx host tokens list`
  - `anx host tokens revoke <token-id>`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host tokens revoke ... ; anx --json host tokens revoke ... ; anx host tokens revoke ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host revoke`

Revoke a host by ID or slug. Agents cannot revoke their own host.

```text
Generated Help: host revoke

- Command ID: `hosts.revoke`
- CLI path: `host revoke`
- HTTP: `DELETE /hosts/{host_id}`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `none`
- Why: Cut off a compromised machine.
- Output: Returns `{ host }`.
- Error codes: `auth_required`, `invalid_token`, `auth_admin_required`, `host_self_revoke`, `not_found`
- Concepts: `hosts`, `auth`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `host bridge check-in`, `host enroll complete`, `host enroll headless`, `host enroll poll`, `host enroll start`, `host enrollments approve`, `host enrollments deny`, `host enrollments list`, `host get`, `host list`, `host patch`, `host tokens create`, `host tokens list`, `host tokens revoke`

Inputs:
  Required:
  - path `host_id`

Local Help: host revoke

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Revoke a host by ID or slug. Agents cannot revoke their own host.
- Examples:
  - `anx host revoke host-b`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host revoke ... ; anx --json host revoke ... ; anx host revoke ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta skill`

Render the bundled participant or PM skill.

```text
Local Help: meta skill

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Render the bundled participant or PM skill.
- Composition: Pure local helper. Renders a canonical portable participant or PM skill and optionally writes an unmanaged export.
- JSON body: `target`, `content`, `default_file`, `written_files`, `guide_topic`, `skill_name`
- Examples:
  - `anx debug meta skill anx`
  - `anx debug meta skill anx --write-file ./SKILL.md`
  - `anx debug meta skill --target cursor --write-file ./SKILL.md`

Flags:
  <target>                     Skill target to render. Use `participant` or `pm`; `anx` and legacy `cursor` export the participant skill.
  --target <target>            Flag form of the skill target.
  --write-file <path>          Write the rendered skill to this exact path.
  --write-dir <dir>            Write the rendered skill into this directory using its default filename.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx debug meta skill ... ; anx --json debug meta skill ... ; anx debug meta skill ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `install skill`

Install the bundled opinionated ANX agent skill to a specific file path.

```text
Install the bundled opinionated ANX agent skill

Usage:
  anx install skill --path <file>
  anx install skill <file>
  anx install skill --path <file> --force

Writes a generic `SKILL.md`-compatible Markdown file that teaches source-aware ANX participation. This legacy export has no managed ownership marker.
For safe ongoing refresh use `anx skills configure`; --force here explicitly replaces the selected file only.

Options:
  --path <file>          Destination file path.
  --write-file <file>    Compatibility spelling for --path.
  --force                Overwrite an existing destination file.

Examples:
  anx install skill --path ./SKILL.md
  anx install skill ./anx-participant/SKILL.md
```

## `bridge install`

Install the host bridge runtime.

```text
Local Help: bridge install

- Kind: `local helper`
- Side effect class: `local_operational_write`
- Summary: Install the host bridge runtime.
- Composition: Install a managed Python virtualenv and wrapper.
- JSON body: `install_dir`, `bin_dir`, `wrapper_path`, `python`, `bridge_binary`, `package_ref`
- Examples:
  - `anx bridge install`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge install ... ; anx --json bridge install ... ; anx bridge install ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge doctor`

Check one enrolled-host bridge and its configured runtimes.

```text
Local Help: bridge doctor

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Check one enrolled-host bridge and its configured runtimes.
- Composition: Invoke the bridge's host roster validation.
- JSON body: `host`, `agents`, `agentctl`
- Examples:
  - `anx bridge doctor --config ./bridge.toml`

Flags:
  --config <path>              One host bridge config.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge doctor ... ; anx --json bridge doctor ... ; anx bridge doctor ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge start`

Start the one bridge process for an enrolled host.

```text
Local Help: bridge start

- Kind: `local helper`
- Side effect class: `external_side_effect`
- Summary: Start the one bridge process for an enrolled host.
- Composition: Start a managed host bridge daemon.
- JSON body: `config_path`, `pid`, `log_path`
- Examples:
  - `anx bridge start --config ./bridge.toml`

Flags:
  --config <path>              Host bridge config.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge start ... ; anx --json bridge start ... ; anx bridge start ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge stop`

Stop a managed host bridge.

```text
Local Help: bridge stop

- Kind: `local helper`
- Side effect class: `local_operational_write`
- Summary: Stop a managed host bridge.
- Composition: Stop the host bridge process.
- JSON body: `config_path`, `pid`, `stopped_at`
- Examples:
  - `anx bridge stop --config ./bridge.toml`

Flags:
  --config <path>              Host bridge config.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge stop ... ; anx --json bridge stop ... ; anx bridge stop ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `bridge status`

Inspect one host bridge process.

```text
Local Help: bridge status

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Inspect one host bridge process.
- Composition: Read managed host bridge process state.
- JSON body: `config_path`, `running`, `pid`, `log_path`
- Examples:
  - `anx bridge status --config ./bridge.toml`

Flags:
  --config <path>              Host bridge config.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx bridge status ... ; anx --json bridge status ... ; anx bridge status ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host token`

Print a short-lived derived-agent bearer from the enrolled host.

```text
Local Help: host token

- Kind: `local helper`
- Side effect class: `local_operational_write`
- Summary: Print a short-lived derived-agent bearer from the enrolled host.
- Composition: Host assertion grant; text mode prints only the token.
- JSON body: `token`, `expires_at`, `agent: {id, handle}`
- Examples:
  - `anx --json host token --as codex`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host token ... ; anx --json host token ... ; anx host token ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host bridge check-in`

Publish an enrolled host bridge check-in.

```text
Local Help: host bridge check-in

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Publish an enrolled host bridge check-in.
- Composition: Signs the exact request body with the owner-only host key.
- JSON body: Core bridge check-in result
- Examples:
  - `anx host bridge check-in --host-id <id> --instance-id <id> --ttl-seconds 180`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host bridge check-in ... ; anx --json host bridge check-in ... ; anx host bridge check-in ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host bridge wake claim`

Claim a durable wake for this host.

```text
Local Help: host bridge wake claim

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Claim a durable wake for this host.
- Composition: Uses a host-signed proof; complete or fail after handling.
- JSON body: Core wake mutation result
- Examples:
  - `anx host bridge wake claim --host-id <id> --wakeup-id <id> --instance-id <id>`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host bridge wake claim ... ; anx --json host bridge wake claim ... ; anx host bridge wake claim ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host bridge wake complete`

Complete a claimed wake for this host.

```text
Local Help: host bridge wake complete

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Complete a claimed wake for this host.
- Composition: Uses a host-signed proof.
- JSON body: Core wake mutation result
- Examples:
  - `anx host bridge wake complete --host-id <id> --wakeup-id <id> --instance-id <id>`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host bridge wake complete ... ; anx --json host bridge wake complete ... ; anx host bridge wake complete ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host bridge wake fail`

Record a failed claimed wake for this host.

```text
Local Help: host bridge wake fail

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Record a failed claimed wake for this host.
- Composition: Uses a host-signed proof and an error summary.
- JSON body: Core wake mutation result
- Examples:
  - `anx host bridge wake fail --host-id <id> --wakeup-id <id> --instance-id <id> --error <text>`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host bridge wake fail ... ; anx --json host bridge wake fail ... ; anx host bridge wake fail ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `runs ingest`

Ingest an agentctl callback or execution envelope.

```text
Generated Help: runs ingest

- Command ID: `runs.upsert`
- CLI path: `runs ingest`
- HTTP: `POST /runs`
- Side effect class: `remote_coordination_write`
- Stability: `beta`
- Input mode: `json-body`
- Why: Map one agentctl execution envelope to a durable run.
- Output: Returns `{ run, created, replayed }`.
- Error codes: `auth_required`, `invalid_token`, `forbidden`, `invalid_request`, `host_revoked`, `run_identity_conflict`, `run_state_regression`
- Concepts: `runs`, `agents`, `cards`
- Agent notes: Validate workspace identity and route-specific proof before mutation; error codes are stable.
- Adjacent commands: `runs get`, `runs list`

Inputs:
  Required:
  - body `adapter` (string)
  - body `agent_id` (string)
  - body `external_id` (string)
  - body `host_id` (string)
  - body `labels` (list<string>)
  - body `last_observed_at` (datetime)
  - body `launcher` (string)
  - body `liveness` (string)
  - body `result_collected` (boolean)
  - body `state` (string)
  Optional:
  - body `branch` (string)
  - body `card_ref` (string)
  - body `ended_at` (datetime)
  - body `model` (string)
  - body `repository` (string)
  - body `started_at` (datetime)
  Enum values: launcher: agentctl; liveness: alive, stale, unknown; state: cancelled, completed, failed, running, starting, unknown

Local Help: runs ingest

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Ingest an agentctl callback or execution envelope.
- Composition: agentctl command appends an owner-only JSON event path. Its child has only PATH and LANG; pass --config-dir and --base-url explicitly. Failures are logged without secrets under <config-dir>/logs/runs-ingest.log.
- JSON body: Idempotent run upsert result
- Examples:
  - `anx --config-dir /absolute/anx --base-url https://anx.example.com runs ingest /absolute/event.json`

Global flags:
  Global flags can appear before or after the command path.
  Examples: anx runs ingest ... ; anx --json runs ingest ... ; anx runs ingest ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `import scan`

Scan a folder or zip archive into a normalized inventory with text cache, repo-root hints, and cluster hints.

```text
Local Help: import scan

- Kind: `local helper`
- Side effect class: `local_operational_write`
- Summary: Scan a folder or zip archive into a normalized inventory with text cache, repo-root hints, and cluster hints.
- Composition: Pure local filesystem helper. Expands `.zip` inputs, ignores obvious generated junk, fingerprints files, caches readable text, and emits `inventory.jsonl` plus `scan-summary.json`.
- JSON body: `input`, `scan_root`, `extracted_root`, `inventory`, `file_count`, `counts_by_category`, `counts_by_cluster_hint`, `repo_roots`
- Examples:
  - `anx import scan --input ./workspace.zip`
  - `anx import scan --input ./vault --out ./.anx-import/vault`

Flags:
  --input <path>               Directory or `.zip` archive to scan.
  --out <dir>                  Output directory. Defaults to `./.anx-import/<source-name>`.
  --max-preview-bytes <n>      Maximum bytes to keep for preview extraction.
  --max-text-cache-bytes <n>   Maximum text-file size cached verbatim for later doc creation.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx import scan ... ; anx --json import scan ... ; anx import scan ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `import dedupe`

Create exact and probable duplicate reports from a scan inventory with conservative skip recommendations.

```text
Local Help: import dedupe

- Kind: `local helper`
- Side effect class: `local_operational_write`
- Summary: Create exact and probable duplicate reports from a scan inventory with conservative skip recommendations.
- Composition: Pure local helper. Uses normalized text hashes for readable content and raw SHA-256 for everything else; exact drops are recommended, probable duplicates are review-only.
- JSON body: `inventory`, `exact_duplicates`, `probable_duplicates`, `recommended_skip_ids`
- Examples:
  - `anx import dedupe --inventory ./.anx-import/workspace/inventory.jsonl`
  - `anx import dedupe ./.anx-import/workspace/inventory.jsonl --out ./.anx-import/workspace`

Flags:
  --inventory <path>           Inventory produced by `anx import scan`. Positional form also supported.
  --out <dir>                  Output directory. Defaults to the inventory directory.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx import dedupe ... ; anx --json import dedupe ... ; anx import dedupe ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `import plan`

Build a conservative import plan that prefers collector threads, hub docs, dedupe-first writes, and low orphan rates.

```text
Local Help: import plan

- Kind: `local helper`
- Side effect class: `local_operational_write`
- Summary: Build a conservative import plan that prefers collector threads, hub docs, dedupe-first writes, and low orphan rates.
- Composition: Pure local helper. Classifies inventory items into docs, artifacts, repo bundles, review bundles, and collector/hub structures. It writes `plan.json` plus `plan-preview.md` without sending requests.
- JSON body: `source_name`, `inventory`, `dedupe`, `principles`, `objects`, `skipped`, `review_bundles`, `notes`
- Examples:
  - `anx import plan --inventory ./.anx-import/workspace/inventory.jsonl`
  - `anx import plan --inventory ./.anx-import/workspace/inventory.jsonl --dedupe ./.anx-import/workspace/dedupe.json --source-name 'workspace export'`

Flags:
  --inventory <path>           Inventory produced by `anx import scan`. Positional form also supported.
  --dedupe <path>              Dedupe report. Defaults to sibling `dedupe.json`.
  --out <dir>                  Output directory. Defaults to the inventory directory.
  --source-name <name>         High-signal display name used in titles, tags, and provenance. Defaults from the inventory directory.
  --collector-threshold <n>    Minimum cluster size that triggers a collector thread.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx import plan ... ; anx --json import plan ... ; anx import plan ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `import apply`

Write payload previews for a plan and optionally execute topic/artifact/doc creates in dependency order.

```text
Local Help: import apply

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Write payload previews for a plan and optionally execute topic/artifact/doc creates in dependency order.
- Composition: Local helper with optional network writes. Always writes payload previews first; when `--execute` is set it creates topics, then artifacts, then docs, substituting `$REF:<key>` placeholders after upstream IDs are known.
- JSON body: `plan`, `execute`, `results`, `refs`
- Examples:
  - `anx import apply --plan ./.anx-import/workspace/plan.json`
  - `anx --as importer import apply --plan ./.anx-import/workspace/plan.json --execute`

Flags:
  --plan <path>                Plan produced by `anx import plan`. Positional form also supported.
  --out <dir>                  Output directory for payload previews and apply results. Defaults to `<plan-dir>/apply`.
  --execute                    Actually call `topics create`, `artifacts create`, and `docs create`. Default is preview-only.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx import apply ... ; anx --json import apply ... ; anx import apply ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `skills sync`

Inspect or maintain versioned local ANX skill files.

```text
Install and maintain ANX skills across detected harnesses, or inspect one explicit copy.

Usage:
  anx skills sync [--dry-run] [--pm|--no-pm] [--auto-sync|--no-auto-sync] [--home <dir>]
  anx skills status [--home <dir>]
  anx skills adopt <path> [--expected-digest sha256:<digest>] [--role participant|pm]
  anx skills configure --path <skill-directory> --role participant|pm [--dry-run]
  anx skills status --path <skill-directory> --role participant|pm
  anx skills verify --path <skill-directory> --role participant|pm

Sync detects installed harnesses and installs or refreshes only clean ANX-owned
copies. It preserves unmanaged or edited files. --pm opts into the additional PM
skill and remembers that choice. Automatic refresh is enabled by default and can
be disabled with --no-auto-sync or ANX_SKILLS_AUTO_SYNC=0. --dry-run performs no
writes. Agentctl-owned copies are reported and left for agentctl to manage.

Legacy skills are reported with their digest. Adoption is read-only until the
printed digest is passed back with --expected-digest; adoption migrates the
skill to the canonical harness path and keeps the old SKILL.md as a timestamped
backup, leaving the legacy directory without a loadable skill. Status reports state per harness. Explicit
configure/verify remain available for arbitrary providers and paths. Harness
activation remains unknown even when a managed copy is current.

Managed copies record CLI version, source revision, skill version and content
digest. Custom files, unrelated instructions and credentials are preserved.
```

## `skills adopt`

Inspect or maintain versioned local ANX skill files.

```text
Install and maintain ANX skills across detected harnesses, or inspect one explicit copy.

Usage:
  anx skills sync [--dry-run] [--pm|--no-pm] [--auto-sync|--no-auto-sync] [--home <dir>]
  anx skills status [--home <dir>]
  anx skills adopt <path> [--expected-digest sha256:<digest>] [--role participant|pm]
  anx skills configure --path <skill-directory> --role participant|pm [--dry-run]
  anx skills status --path <skill-directory> --role participant|pm
  anx skills verify --path <skill-directory> --role participant|pm

Sync detects installed harnesses and installs or refreshes only clean ANX-owned
copies. It preserves unmanaged or edited files. --pm opts into the additional PM
skill and remembers that choice. Automatic refresh is enabled by default and can
be disabled with --no-auto-sync or ANX_SKILLS_AUTO_SYNC=0. --dry-run performs no
writes. Agentctl-owned copies are reported and left for agentctl to manage.

Legacy skills are reported with their digest. Adoption is read-only until the
printed digest is passed back with --expected-digest; adoption migrates the
skill to the canonical harness path and keeps the old SKILL.md as a timestamped
backup, leaving the legacy directory without a loadable skill. Status reports state per harness. Explicit
configure/verify remain available for arbitrary providers and paths. Harness
activation remains unknown even when a managed copy is current.

Managed copies record CLI version, source revision, skill version and content
digest. Custom files, unrelated instructions and credentials are preserved.
```

## `skills configure`

Inspect or maintain versioned local ANX skill files.

```text
Install and maintain ANX skills across detected harnesses, or inspect one explicit copy.

Usage:
  anx skills sync [--dry-run] [--pm|--no-pm] [--auto-sync|--no-auto-sync] [--home <dir>]
  anx skills status [--home <dir>]
  anx skills adopt <path> [--expected-digest sha256:<digest>] [--role participant|pm]
  anx skills configure --path <skill-directory> --role participant|pm [--dry-run]
  anx skills status --path <skill-directory> --role participant|pm
  anx skills verify --path <skill-directory> --role participant|pm

Sync detects installed harnesses and installs or refreshes only clean ANX-owned
copies. It preserves unmanaged or edited files. --pm opts into the additional PM
skill and remembers that choice. Automatic refresh is enabled by default and can
be disabled with --no-auto-sync or ANX_SKILLS_AUTO_SYNC=0. --dry-run performs no
writes. Agentctl-owned copies are reported and left for agentctl to manage.

Legacy skills are reported with their digest. Adoption is read-only until the
printed digest is passed back with --expected-digest; adoption migrates the
skill to the canonical harness path and keeps the old SKILL.md as a timestamped
backup, leaving the legacy directory without a loadable skill. Status reports state per harness. Explicit
configure/verify remain available for arbitrary providers and paths. Harness
activation remains unknown even when a managed copy is current.

Managed copies record CLI version, source revision, skill version and content
digest. Custom files, unrelated instructions and credentials are preserved.
```

## `skills status`

Inspect or maintain versioned local ANX skill files.

```text
Install and maintain ANX skills across detected harnesses, or inspect one explicit copy.

Usage:
  anx skills sync [--dry-run] [--pm|--no-pm] [--auto-sync|--no-auto-sync] [--home <dir>]
  anx skills status [--home <dir>]
  anx skills adopt <path> [--expected-digest sha256:<digest>] [--role participant|pm]
  anx skills configure --path <skill-directory> --role participant|pm [--dry-run]
  anx skills status --path <skill-directory> --role participant|pm
  anx skills verify --path <skill-directory> --role participant|pm

Sync detects installed harnesses and installs or refreshes only clean ANX-owned
copies. It preserves unmanaged or edited files. --pm opts into the additional PM
skill and remembers that choice. Automatic refresh is enabled by default and can
be disabled with --no-auto-sync or ANX_SKILLS_AUTO_SYNC=0. --dry-run performs no
writes. Agentctl-owned copies are reported and left for agentctl to manage.

Legacy skills are reported with their digest. Adoption is read-only until the
printed digest is passed back with --expected-digest; adoption migrates the
skill to the canonical harness path and keeps the old SKILL.md as a timestamped
backup, leaving the legacy directory without a loadable skill. Status reports state per harness. Explicit
configure/verify remain available for arbitrary providers and paths. Harness
activation remains unknown even when a managed copy is current.

Managed copies record CLI version, source revision, skill version and content
digest. Custom files, unrelated instructions and credentials are preserved.
```

## `skills verify`

Inspect or maintain versioned local ANX skill files.

```text
Install and maintain ANX skills across detected harnesses, or inspect one explicit copy.

Usage:
  anx skills sync [--dry-run] [--pm|--no-pm] [--auto-sync|--no-auto-sync] [--home <dir>]
  anx skills status [--home <dir>]
  anx skills adopt <path> [--expected-digest sha256:<digest>] [--role participant|pm]
  anx skills configure --path <skill-directory> --role participant|pm [--dry-run]
  anx skills status --path <skill-directory> --role participant|pm
  anx skills verify --path <skill-directory> --role participant|pm

Sync detects installed harnesses and installs or refreshes only clean ANX-owned
copies. It preserves unmanaged or edited files. --pm opts into the additional PM
skill and remembers that choice. Automatic refresh is enabled by default and can
be disabled with --no-auto-sync or ANX_SKILLS_AUTO_SYNC=0. --dry-run performs no
writes. Agentctl-owned copies are reported and left for agentctl to manage.

Legacy skills are reported with their digest. Adoption is read-only until the
printed digest is passed back with --expected-digest; adoption migrates the
skill to the canonical harness path and keeps the old SKILL.md as a timestamped
backup, leaving the legacy directory without a loadable skill. Status reports state per harness. Explicit
configure/verify remain available for arbitrary providers and paths. Harness
activation remains unknown even when a managed copy is current.

Managed copies record CLI version, source revision, skill version and content
digest. Custom files, unrelated instructions and credentials are preserved.
```

## `plan step add`

Edit a linked initiative step with a card concurrency token.

```text
One plan per initiative card. Add linked steps; never select a view.
anx plan show <card>
anx plan set <card> --from-file <path|-> [--if-updated-at <timestamp>] (JSON {steps:[]})
anx plan step add <card> --title <title> [--step-id <slug>] [--ref <ref|url>] [--after a,b] [--due <date>] [--status done|active|blocked|not_started]
anx plan step update <card> <step-id> [--title ...] [--ref ...] [--after a,b] [--due ...] [--status ...]
anx plan step rm <card> <step-id>
Writes read the latest plan then use its concurrency token. Conflicts require an explicit retry. Removing a depended-on step is rejected; update dependencies first.
```

## `plan step update`

Edit a linked initiative step with a card concurrency token.

```text
One plan per initiative card. Add linked steps; never select a view.
anx plan show <card>
anx plan set <card> --from-file <path|-> [--if-updated-at <timestamp>] (JSON {steps:[]})
anx plan step add <card> --title <title> [--step-id <slug>] [--ref <ref|url>] [--after a,b] [--due <date>] [--status done|active|blocked|not_started]
anx plan step update <card> <step-id> [--title ...] [--ref ...] [--after a,b] [--due ...] [--status ...]
anx plan step rm <card> <step-id>
Writes read the latest plan then use its concurrency token. Conflicts require an explicit retry. Removing a depended-on step is rejected; update dependencies first.
```

## `plan step rm`

Edit a linked initiative step with a card concurrency token.

```text
One plan per initiative card. Add linked steps; never select a view.
anx plan show <card>
anx plan set <card> --from-file <path|-> [--if-updated-at <timestamp>] (JSON {steps:[]})
anx plan step add <card> --title <title> [--step-id <slug>] [--ref <ref|url>] [--after a,b] [--due <date>] [--status done|active|blocked|not_started]
anx plan step update <card> <step-id> [--title ...] [--ref ...] [--after a,b] [--due ...] [--status ...]
anx plan step rm <card> <step-id>
Writes read the latest plan then use its concurrency token. Conflicts require an explicit retry. Removing a depended-on step is rejected; update dependencies first.
```

## `pm serve`

Claim queued PM turns and run them through agentctl with the anx CLI as tools.

```text
Local Help: pm serve

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Claim queued PM turns and run them through agentctl with the anx CLI as tools.
- Composition: Local runner. Claims one leased turn, writes a small prompt file, launches the configured harness through agentctl, then completes or fails the turn. Does not call a model in-process.
- JSON body: `turn_id`, `execution_id`, `status`, `provider`, `model`
- Examples:
  - `anx --as pm pm serve --runner 'omp -p --mode json --model zai/glm-5.3 --auto-approve'`
  - `anx --as pm pm serve --runner 'hermes -p --provider zai --model glm-5.3 -- {prompt}'`

Flags:
  --runner <argv>              Harness argv. Without {prompt}, this is passed to `agentctl run --`. With {prompt}, argv is executed directly after substituting the prompt file path. Evidence refs come from a trailing ---evidence--- block or a JSON evidence_refs array on the reply object (the same object assistant text is read from), never from prose or nested tool output. Topic and document refs are verified like card/work/artifact/event/decision. Replies over the turn's max_output_bytes (default 64000, core's turn-text ceiling) are stored with a visible truncation marker.
  --work-dir <dir>             Directory for prompt files and the runner id (default .tmp/pm-runner). Must be the agentctl working root when agentctl is used.
  --poll-interval <duration>   Sleep between empty claims and after a released turn (default 2s).
  --max-concurrent <n>         In-process cap on turns this runner executes at once (default 1). Each worker claims with a distinct runner id (<id>-<slot>). Core also bounds workspace sending turns.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx pm serve ... ; anx --json pm serve ... ; anx pm serve ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `pm ask`

Create a PM conversation and post one human question.

```text
Local Help: pm ask

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Create a PM conversation and post one human question.
- Composition: Local helper over `pm conversations create` and `pm conversations message`. A queued turn is not an assistant reply; run `anx pm serve` for that.
- JSON body: `conversation`, `turn`
- Examples:
  - `anx --as maya pm ask "What needs my decision?"`
  - `anx --as maya pm ask --wait "What needs my decision?"`

Flags:
  --wait                       Poll until the turn has a response, fails, or the deadline passes.
  --work-ref <ref>             Optional work/card ref to attach to the conversation.
  --title <text>               Conversation title (defaults to a prefix of the question).
  --request-key <key>          Stable request key for create+message replay.
  --conversation-id <id>       Post into an existing conversation instead of creating one.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx pm ask ... ; anx --json pm ask ... ; anx pm ask ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `pm channels doctor`

Check PM channel secrets, webhook reachability, and binding state without sending a chat message.

```text
Local Help: pm channels doctor

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Check PM channel secrets, webhook reachability, and binding state without sending a chat message.
- Composition: Local diagnostic. Reads env, probes webhook URLs with GET, and lists bindings. Does not send Telegram or Discord messages.
- JSON body: `checks`, `ok`
- Examples:
  - `anx pm channels doctor`
  - `anx pm channels doctor --telegram-webhook-url http://127.0.0.1:8000/pm/ingress/telegram --discord-webhook-url http://127.0.0.1:8000/pm/ingress/discord`

Flags:
  --telegram-webhook-url <url> Telegram ingress URL to probe with GET (fake or core). Does not POST an update.
  --discord-webhook-url <url>  Discord interactions URL to probe with GET (fake or core). Does not POST an interaction.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx pm channels doctor ... ; anx --json pm channels doctor ... ; anx pm channels doctor ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `report templates`

List live report templates and their purpose.

```text
Local Help: report templates

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: List live report templates and their purpose.
- Composition: Pure local helper; no credentials or network required.
- JSON body: `templates[]` with `name` and `purpose`
- Examples:
  - `anx report templates`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx report templates ... ; anx --json report templates ... ; anx report templates ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `report init`

Generate a report template wired to live workspace queries.

```text
Local Help: report init

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Generate a report template wired to live workspace queries.
- Composition: Pure local helper. Start with a template, add narrative, then preview before sharing.
- JSON body: A version 1 `anx.visual-report` document on stdout.
- Examples:
  - `anx report init --template workspace-overview > dashboard.json`
  - `anx report init --template initiative --card card:launch > initiative.json`

Flags:
  --template <name>            One of workspace-overview, initiative, weekly-review, release-readiness, incident-review, fleet-health.
  --topic <ref>                Scope project-based live queries to a topic.
  --card <ref>                 Scope a card-centered template to an initiative card.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx report init ... ; anx --json report init ... ; anx report init ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `report preview`

Render a report to PNG and summarize each panel's source and freshness.

```text
Local Help: report preview

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Render a report to PNG and summarize each panel's source and freshness.
- Composition: Read-only. A document uses current authorized live data; a file is materialized in memory without publishing it.
- JSON body: `png`, `rendered`, `panels[]` with `what`, `source`, and `freshness`
- Examples:
  - `anx report preview ./dashboard.json`
  - `anx report preview doc:dashboard --output /tmp/dashboard.png`

Flags:
  <doc|file>                   Saved report document ref, or local visual report JSON path.
  --output <png>               PNG destination; defaults to report-preview.png.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx report preview ... ; anx --json report preview ... ; anx report preview ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `report schema`

Print the visual report types, limits, and minimal example.

```text
Local Help: report schema

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Print the visual report types, limits, and minimal example.
- Composition: Pure local helper; no credentials or network required.
- JSON body: `kind`, `schema_version`, `panel_types`, `limits`, `example`
- Examples:
  - `anx report schema`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx report schema ... ; anx --json report schema ... ; anx report schema ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `report validate`

Validate a visual report file or stdin against the renderer's schema.

```text
Local Help: report validate

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Validate a visual report file or stdin against the renderer's schema.
- Composition: Pure local helper using the same report contract as the web renderer.
- JSON body: `recognized`, `valid`, `errors`, `panel_count`
- Examples:
  - `anx report validate ./dashboard.json`
  - `cat dashboard.json | anx report validate -`

Flags:
  <file|->                     Report JSON path, or - to read stdin.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx report validate ... ; anx --json report validate ... ; anx report validate ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `report publish`

Validate, publish, read back, and revalidate a visual report document.

```text
Local Help: report publish

- Kind: `local helper`
- Side effect class: `remote_coordination_write`
- Summary: Validate, publish, read back, and revalidate a visual report document.
- Composition: Creates a topic-linked document or revises a matching visual report. An explicit --doc resolves exactly; replacing another document requires --replace.
- JSON body: `doc_ref`, optional `web_url`, `title`, `action`, `panel_count`, `validated`
- Examples:
  - `anx report publish ./dashboard.json --topic topic:launch`
  - `anx report publish ./dashboard.json --topic topic:launch --title "Fleet Dashboard" --doc doc:fleet-dashboard --replace`

Flags:
  <file>                       Visual report JSON path.
  --topic <ref>                Existing topic to anchor the report.
  --title <text>               Document title; defaults to the report title.
  --doc <ref>                  Existing document in the topic to revise.
  --replace                    Allow replacing an explicitly selected non-report document.


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx report publish ... ; anx --json report publish ... ; anx report publish ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `host discover`

Inspect optional local runtime identity and installed harness evidence without registration or network requests.

```text
Local Help: host discover

- Kind: `local helper`
- Side effect class: `read_only`
- Summary: Inspect optional local runtime identity and installed harness evidence without registration or network requests.
- Composition: Read-only local agentctl identity v1 evidence. No principal grant, task assignment, transcript read, or automatic upload. Installed availability is not a live-session capability.
- JSON body: `provider`, `available`, `registration_requires_provider`, optional `identity` and `adapters`
- Examples:
  - `anx host discover`
  - `anx host discover --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: anx host discover ... ; anx --json host discover ... ; anx host discover ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --workspace <alias>, --as <name>, --config-dir <absolute-path>, --no-color, --verbose, --headers, --timeout <duration>
```

## `work context`

Compose work, a bounded observation page and refresh status using read-only requests.

```text
Local Help: work context

Side effect class: read_only

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Compose work, a bounded observation page and refresh status using read-only requests.

Usage: anx work context <ref> (or --work-id <ref>)
  --limit <value>
  --cursor <value>

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```

## `work freshness`

Inspect last observed, source activity and meaningful progress independently.

```text
Local Help: work freshness

Side effect class: read_only

Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.

Inspect last observed, source activity and meaningful progress independently.

Usage: anx work freshness <ref> (or --work-id <ref>)

Lists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.

Use --json for one machine-readable envelope.
```
