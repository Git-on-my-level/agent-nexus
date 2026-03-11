# OAR Runtime Help Reference

This reference is bundled with the CLI. Print the full document with `oar meta docs` or one topic with `oar meta doc <topic>`.

## Topics

- `onboarding` (manual): Offline quick-start mental model and first command flow.
- `draft` (manual): Local draft staging, listing, commit, and discard workflow.
- `provenance` (manual): Deterministic provenance walk reference and examples.
- `threads` (group): Manage thread resources
- `commitments` (group): Manage commitment resources
- `artifacts` (group): Manage artifact resources and content
- `docs` (group): Manage long-lived docs and revisions
- `events` (group): Manage events and event streams
- `inbox` (group): List/get/ack/stream inbox items
- `work-orders` (group): Create work-order packets
- `receipts` (group): Create receipt packets
- `reviews` (group): Create review packets
- `derived` (group): Run derived-view maintenance actions
- `meta` (group): Inspect generated command/concept metadata
- `threads list` (command): List thread snapshots
- `threads get` (command): Get thread snapshot by id
- `threads create` (command): Create thread snapshot
- `threads timeline` (command): Get thread timeline events and referenced entities
- `threads context` (command): Get bundled thread context for agent callers
- `commitments list` (command): List commitments
- `commitments get` (command): Get commitment by id
- `commitments create` (command): Create commitment snapshot
- `artifacts list` (command): List artifact metadata
- `artifacts get` (command): Get artifact metadata by id
- `artifacts create` (command): Create artifact
- `artifacts content` (command): Get artifact raw content
- `artifacts tombstone` (command): Tombstone an artifact (soft-delete)
- `docs list` (command): List documents and their current head metadata
- `docs create` (command): Create document with initial immutable revision
- `docs get` (command): Get document and authoritative head revision
- `docs history` (command): List ordered immutable revisions for a document
- `docs revision` (group): Nested generated help topic.
- `docs tombstone` (command): Tombstone a document (soft-delete)
- `docs revision get` (command): Get one immutable document revision
- `events get` (command): Get event by id
- `events create` (command): Append event
- `events stream` (command): Stream events via Server-Sent Events (SSE)
- `events tail` (command): Stream events via Server-Sent Events (SSE)
- `inbox list` (command): List derived inbox items
- `inbox get` (command): Get derived inbox item detail
- `inbox ack` (command): Acknowledge an inbox item
- `inbox stream` (command): Stream derived inbox items via SSE
- `inbox tail` (command): Stream derived inbox items via SSE
- `derived rebuild` (command): Rebuild derived views
- `meta commands` (command): List generated command metadata
- `meta command` (command): Get generated metadata for a command id
- `meta concepts` (command): List generated concept metadata
- `meta concept` (command): Get generated metadata for one concept
- `work-orders create` (command): Create work-order packet artifact
- `receipts create` (command): Create receipt packet artifact
- `reviews create` (command): Create review packet artifact
- `events list` (local-helper): Compose `threads timeline` responses with client-side thread/type/actor filters and preview summaries.
- `events validate` (local-helper): Validate an `events create` payload locally from stdin or `--from-file` without sending it.
- `events explain` (local-helper): Explain known event-type conventions, required refs, and validation hints for one type or the full catalog.
- `artifacts inspect` (local-helper): Fetch artifact metadata and resolved content in one command for operator inspection.
- `threads inspect` (local-helper): Canonical thread coordination read path: compose one view from `threads context` and related `inbox list` items.
- `threads workspace` (local-helper): Single holistic thread coordination read: combine context, inbox, recommendation review, and related-thread signals in one command.
- `threads recommendations` (local-helper): Review one thread's recommendation/decision inputs plus related-thread signals with provenance and follow-up hints.
- `threads patch` (local-helper): Stage a thread patch proposal locally and show the diff before applying it.
- `threads apply` (local-helper): Apply a previously staged thread patch proposal.
- `commitments update` (local-helper): Stage a commitment update proposal locally and show the diff before applying it.
- `commitments apply` (local-helper): Apply a previously staged commitment update proposal.
- `docs update` (local-helper): Stage a document update proposal locally and show the content diff before applying it.
- `docs content` (local-helper): Show the current document content together with authoritative head revision metadata.
- `docs validate-update` (local-helper): Validate a `docs update` payload locally from stdin or file without sending the mutation.
- `docs apply` (local-helper): Apply a previously staged document update proposal.


## `onboarding`

Offline quick-start mental model and first command flow.

```text
Onboarding: mental model

1. `oar` is a non-interactive CLI that maps stable command paths to core HTTP endpoints and emits plain text or a single JSON envelope.
2. Each command should be safe for automation, so defaults, errors, and output shapes are designed for scripts first.
3. Profiles (`--agent`) hold reusable auth and base URL settings so repeated commands stay short and consistent.
4. Typed commands (`threads`, `events`, `inbox`, and packet creators) are the primary surface, while `api call` is the escape hatch.
5. The fastest way to stay aligned is to run health/auth checks first, then execute the work-order loop one step at a time.

Work-order loop

1. Inspect inbound work and context: `oar inbox list` or `oar inbox stream --max-events 1`.
2. Read current state before mutating it: `oar threads workspace --thread-id <thread-id>`.
   Use `oar threads context` for cross-thread aggregates and `oar threads get` for raw snapshot-only reads.
3. Stage a mutation proposal when you need reviewable intent: `oar docs update`, `oar threads patch`, `oar commitments update`, or `oar draft create --command <command-id>`.
4. Apply the staged proposal (or commit a draft for lower-level commands) and capture returned IDs.
5. Confirm outcomes in timeline/events and ack inbox items to close the loop.

First 5 commands to run

  oar --base-url http://127.0.0.1:8000 --agent <agent> doctor
  oar --base-url http://127.0.0.1:8000 --agent <agent> auth register --username <username>
  oar --agent <agent> auth whoami
  oar --agent <agent> threads list
  oar --agent <agent> inbox stream --max-events 1

Optional full runbook (local, offline)

  cli/docs/runbook.md
```

## `draft`

Local draft staging, listing, commit, and discard workflow.

```text
Draft commands stage write requests locally before commit.

Usage:
  oar draft create --command <command-id> [--from-file <path>]
  oar draft list
  oar draft commit <draft-id> [--keep]
  oar draft discard <draft-id>

Examples:
  cat payload.json | oar draft create --command threads.create
  oar draft commit draft-20260305T103000-a1b2c3d4e5f6
```

## `provenance`

Deterministic provenance walk reference and examples.

```text
Provenance navigation

Usage:
  oar provenance walk --from <typed-ref> [--depth <n>] [--include-event-chain]

Typed ref roots:
  event:<id>
  thread:<id>
  artifact:<id>
  snapshot:<id>

Examples:
  oar --json provenance walk --from event:event_123 --depth 2
  oar --json provenance walk --from snapshot:snapshot_123 --depth 1
  oar provenance walk --from event:event_123 --depth 3 --include-event-chain
```

## `threads`

Manage thread resources

```text
Generated Help: threads

Commands:
  threads context          Get bundled thread context for agent callers
  threads create           Create thread snapshot
  threads get              Get thread snapshot by id
  threads list             List thread snapshots
  threads patch            Patch thread snapshot
  threads timeline         Get thread timeline events and referenced entities

Canonical coordination read path:
  threads workspace           Compose one holistic thread workspace from context + inbox + related-thread review.
  threads inspect             Compose one thread coordination view from context + inbox in one command.
  threads recommendations     Focus recommendation/decision review with actor+timestamp provenance.
  Mutation flow:
  threads patch               Stage a thread patch proposal and inspect the diff before applying.
  threads apply               Apply a staged thread patch proposal.
  Tip: start with `oar threads workspace` for one initiative/thread, use `--status/--tag/--type initiative` to discover one thread, use `oar threads context` for cross-thread aggregates, and `oar threads get` for raw snapshot-only reads. Add `--full-id` for copy/paste ids.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads ... ; oar threads ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `commitments`

Manage commitment resources

```text
Generated Help: commitments

Commands:
  commitments create       Create commitment snapshot
  commitments get          Get commitment by id
  commitments list         List commitments
  commitments update       Patch commitment snapshot

Mutation flow:
  commitments update         Stage a commitment update proposal and inspect the diff before applying.
  commitments apply          Apply a staged commitment update proposal.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments ... ; oar commitments ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `artifacts`

Manage artifact resources and content

```text
Generated Help: artifacts

Commands:
  artifacts create         Create artifact
  artifacts get            Get artifact metadata by id
  artifacts list           List artifact metadata
  artifacts tombstone      Tombstone an artifact (soft-delete)

Local inspection helper:
  artifacts inspect        Fetch artifact metadata and content in one call.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts ... ; oar artifacts ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `docs`

Manage long-lived docs and revisions

```text
Generated Help: docs

Commands:
  docs create              Create document with initial immutable revision
  docs get                 Get document and authoritative head revision
  docs history             List ordered immutable revisions for a document
  docs list                List documents and their current head metadata
  docs tombstone           Tombstone a document (soft-delete)
  docs update              Create a new immutable revision for an existing document

Local inspection helpers:
  docs content             Show current document content with revision metadata.
  Mutation flow:
  docs update              Stage an update proposal and inspect its diff before applying it.
  docs apply               Apply a staged document update proposal.
  docs validate-update     Validate a docs.update payload from stdin/--from-file.
  Tip: add `--content-file <path>` to avoid hand-escaping multiline content.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs ... ; oar docs ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `events`

Manage events and event streams

```text
Generated Help: events

Commands:
  events create            Append event
  events get               Get event by id
  events stream            Stream events via Server-Sent Events (SSE)

Local inspection helpers:
  events list              List timeline events with thread/type/actor filters, id mode, and preview summaries.
  events explain           Explain known event-type conventions and local validation constraints.
  events validate          Validate an events.create payload from stdin/--from-file without sending a request.
  Tip: use `--mine` or `--actor-id <id>` to audit one actor; add `--full-id` for copy/paste IDs.
  For details: `oar events explain <event-type>`

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events ... ; oar events ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `inbox`

List/get/ack/stream inbox items

```text
Generated Help: inbox

Commands:
  inbox ack                Acknowledge an inbox item
  inbox get                Get derived inbox item detail
  inbox list               List derived inbox items
  inbox stream             Stream derived inbox items via SSE

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox ... ; oar inbox ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `work-orders`

Create work-order packets

```text
Generated Help: work-orders

Commands:
  work-orders create       Create work-order packet artifact

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json work-orders ... ; oar work-orders ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `receipts`

Create receipt packets

```text
Generated Help: receipts

Commands:
  receipts create          Create receipt packet artifact

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json receipts ... ; oar receipts ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `reviews`

Create review packets

```text
Generated Help: reviews

Commands:
  reviews create           Create review packet artifact

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json reviews ... ; oar reviews ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `derived`

Run derived-view maintenance actions

```text
Generated Help: derived

Commands:
  derived rebuild          Rebuild derived views

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json derived ... ; oar derived ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `meta`

Inspect generated command/concept metadata

```text
Generated Help: meta

Commands:
  meta command             Get generated metadata for a command id
  meta commands            List generated command metadata
  meta concept             Get generated metadata for one concept
  meta concepts            List generated concept metadata

Shipped reference docs:
  meta docs               Print the bundled Markdown runtime reference.
  meta doc                Print one bundled Markdown topic, for example `oar meta doc threads`.
  Tip: use `oar help meta` for the short runtime surface and `oar meta docs` for the full shipped reference.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta ... ; oar meta ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `threads list`

List thread snapshots

```text
Generated Help: threads list

- Command ID: `threads.list`
- CLI path: `threads list`
- HTTP: `GET /threads`
- Stability: `stable`
- Input mode: `none`
- Why: Retrieve current thread state for triage and scheduling decisions.
- Output: Returns `{ threads }`; query filters are additive.
- Error codes: `invalid_request`
- Concepts: `threads`, `filtering`
- Agent notes: Safe and idempotent.
- Adjacent commands: `threads context`, `threads create`, `threads get`, `threads patch`, `threads timeline`
- Examples:
  - List active p1 threads: `oar threads list --status active --priority p1 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads list ... ; oar threads list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads get`

Get thread snapshot by id

```text
Generated Help: threads get

- Command ID: `threads.get`
- CLI path: `threads get`
- HTTP: `GET /threads/{thread_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Resolve a raw authoritative thread snapshot for low-level reads before patching or composing packets.
- Output: Returns `{ thread }`.
- Error codes: `not_found`
- Concepts: `threads`
- Agent notes: Safe and idempotent. Prefer `oar threads inspect` for operator coordination reads.
- Adjacent commands: `threads context`, `threads create`, `threads list`, `threads patch`, `threads timeline`
- Examples:
  - Read thread: `oar threads get --thread-id thread_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads get ... ; oar threads get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads create`

Create thread snapshot

```text
Generated Help: threads create

- Command ID: `threads.create`
- CLI path: `threads create`
- HTTP: `POST /threads`
- Stability: `stable`
- Input mode: `json-body`
- Why: Open a new thread for tracking ongoing organizational work.
- Output: Returns `{ thread }` including generated id and audit fields.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `threads`, `snapshots`
- Agent notes: Non-idempotent unless caller enforces a deterministic id strategy externally.
- Adjacent commands: `threads context`, `threads get`, `threads list`, `threads patch`, `threads timeline`
- Examples:
  - Create thread: `oar threads create --from-file thread.json --json`

Body schema:
  Required: thread.cadence (string), thread.current_summary (string), thread.key_artifacts (list<typed_ref>), thread.next_actions (list<string>), thread.priority (string), thread.provenance.sources (list<string>), thread.status (string), thread.tags (list<string>), thread.title (string), thread.type (string)
  Optional: actor_id (string), thread.next_check_in_at (datetime), thread.provenance.by_field (map<string, list<string>>), thread.provenance.notes (string)
  Enum values: thread.priority (strict): p0, p1, p2, p3; thread.status (strict): active, closed, paused; thread.type (strict): case, incident, initiative, other, process, relationship

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads create ... ; oar threads create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads timeline`

Get thread timeline events and referenced entities

```text
Generated Help: threads timeline

- Command ID: `threads.timeline`
- CLI path: `threads timeline`
- HTTP: `GET /threads/{thread_id}/timeline`
- Stability: `stable`
- Input mode: `none`
- Why: Retrieve narrative event history plus referenced snapshots/artifacts in one call.
- Output: Returns `{ events, snapshots, artifacts }` where snapshot/artifact maps are sparse.
- Error codes: `not_found`
- Concepts: `threads`, `events`, `provenance`
- Agent notes: Events stay time ordered; missing refs are omitted from expansion maps.
- Adjacent commands: `threads context`, `threads create`, `threads get`, `threads list`, `threads patch`
- Examples:
  - Timeline: `oar threads timeline --thread-id thread_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads timeline ... ; oar threads timeline ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads context`

Get bundled thread context for agent callers

```text
Generated Help: threads context

- Command ID: `threads.context`
- CLI path: `threads context`
- HTTP: `GET /threads/{thread_id}/context`
- Stability: `beta`
- Input mode: `none`
- Why: Load one thread's state, recent events, key artifacts, open commitments, and linked documents in a single round-trip; CLI `oar threads context` can aggregate across threads by composing multiple calls.
- Output: Returns `{ thread, recent_events, key_artifacts, open_commitments, documents }`.
- Error codes: `invalid_request`, `not_found`
- Concepts: `threads`, `events`, `artifacts`, `commitments`, `docs`
- Agent notes: Use include_artifact_content for prompt-ready previews; default mode keeps payloads lighter. Prefer `oar threads inspect` as the first single-thread coordination read.
- Adjacent commands: `threads create`, `threads get`, `threads list`, `threads patch`, `threads timeline`
- Examples:
  - Context with defaults: `oar threads context --thread-id thread_123 --json`
  - Context with artifact previews: `oar threads context --thread-id thread_123 --include-artifact-content --max-events 50 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads context ... ; oar threads context ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments list`

List commitments

```text
Generated Help: commitments list

- Command ID: `commitments.list`
- CLI path: `commitments list`
- HTTP: `GET /commitments`
- Stability: `stable`
- Input mode: `none`
- Why: Monitor open/blocked work and due windows.
- Output: Returns `{ commitments }`.
- Error codes: `invalid_request`
- Concepts: `commitments`, `filtering`
- Agent notes: Safe and idempotent.
- Adjacent commands: `commitments create`, `commitments get`, `commitments update`
- Examples:
  - List open commitments for a thread: `oar commitments list --thread-id thread_123 --status open --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments list ... ; oar commitments list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments get`

Get commitment by id

```text
Generated Help: commitments get

- Command ID: `commitments.get`
- CLI path: `commitments get`
- HTTP: `GET /commitments/{commitment_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Read commitment status/details before status transitions.
- Output: Returns `{ commitment }`.
- Error codes: `not_found`
- Concepts: `commitments`
- Agent notes: Safe and idempotent.
- Adjacent commands: `commitments create`, `commitments list`, `commitments update`
- Examples:
  - Get commitment: `oar commitments get --commitment-id commitment_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments get ... ; oar commitments get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments create`

Create commitment snapshot

```text
Generated Help: commitments create

- Command ID: `commitments.create`
- CLI path: `commitments create`
- HTTP: `POST /commitments`
- Stability: `stable`
- Input mode: `json-body`
- Why: Track accountable work items tied to a thread.
- Output: Returns `{ commitment }` with generated id.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `commitments`
- Agent notes: Non-idempotent unless caller controls external dedupe.
- Adjacent commands: `commitments get`, `commitments list`, `commitments update`
- Examples:
  - Create commitment: `oar commitments create --from-file commitment.json --json`

Body schema:
  Required: commitment.definition_of_done (list<string>), commitment.due_at (datetime), commitment.links (list<typed_ref>), commitment.owner (string), commitment.provenance.sources (list<string>), commitment.status (string), commitment.thread_id (string), commitment.title (string)
  Optional: actor_id (string), commitment.provenance.by_field (map<string, list<string>>), commitment.provenance.notes (string)
  Enum values: commitment.status (strict): blocked, canceled, done, open

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments create ... ; oar commitments create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts list`

List artifact metadata

```text
Generated Help: artifacts list

- Command ID: `artifacts.list`
- CLI path: `artifacts list`
- HTTP: `GET /artifacts`
- Stability: `stable`
- Input mode: `none`
- Why: Discover evidence and packets attached to threads.
- Output: Returns `{ artifacts }` metadata only.
- Error codes: `invalid_request`
- Concepts: `artifacts`, `filtering`
- Agent notes: Safe and idempotent.
- Adjacent commands: `artifacts content get`, `artifacts create`, `artifacts get`, `artifacts tombstone`
- Examples:
  - List work orders for a thread: `oar artifacts list --kind work_order --thread-id thread_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts list ... ; oar artifacts list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts get`

Get artifact metadata by id

```text
Generated Help: artifacts get

- Command ID: `artifacts.get`
- CLI path: `artifacts get`
- HTTP: `GET /artifacts/{artifact_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Resolve artifact refs before downloading or rendering content.
- Output: Returns `{ artifact }` metadata.
- Error codes: `not_found`
- Concepts: `artifacts`
- Agent notes: Safe and idempotent.
- Adjacent commands: `artifacts content get`, `artifacts create`, `artifacts list`, `artifacts tombstone`
- Examples:
  - Get artifact: `oar artifacts get --artifact-id artifact_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts get ... ; oar artifacts get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts create`

Create artifact

```text
Generated Help: artifacts create

- Command ID: `artifacts.create`
- CLI path: `artifacts create`
- HTTP: `POST /artifacts`
- Stability: `stable`
- Input mode: `file-and-body`
- Why: Persist immutable evidence blobs and metadata for references and review.
- Output: Returns `{ artifact }` metadata after content write.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `artifacts`, `evidence`
- Agent notes: Treat as non-idempotent unless caller controls artifact id collisions.
- Adjacent commands: `artifacts content get`, `artifacts get`, `artifacts list`, `artifacts tombstone`
- Examples:
  - Create structured artifact: `oar artifacts create --from-file artifact-create.json --json`

Body schema:
  Required: artifact (object), content (object|string), content_type (string)
  Optional: actor_id (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts create ... ; oar artifacts create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts content`

Get artifact raw content

```text
Generated Help: artifacts content

- Command ID: `artifacts.content.get`
- CLI path: `artifacts content get`
- HTTP: `GET /artifacts/{artifact_id}/content`
- Stability: `stable`
- Input mode: `none`
- Why: Fetch opaque artifact bytes for downstream processors.
- Output: Raw bytes; content type mirrors stored artifact media.
- Error codes: `not_found`
- Concepts: `artifacts`, `content`
- Agent notes: Stream to file for large payloads.
- Adjacent commands: `artifacts create`, `artifacts get`, `artifacts list`, `artifacts tombstone`
- Examples:
  - Download content: `oar artifacts content get --artifact-id artifact_123 > artifact.bin`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts content ... ; oar artifacts content ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts tombstone`

Tombstone an artifact (soft-delete)

```text
Generated Help: artifacts tombstone

- Command ID: `artifacts.tombstone`
- CLI path: `artifacts tombstone`
- HTTP: `POST /artifacts/{artifact_id}/tombstone`
- Stability: `beta`
- Input mode: `json-body`
- Why: Mark an artifact as inactive while preserving provenance; tombstoned artifacts are excluded from list by default.
- Output: Returns `{ artifact }` with updated tombstone metadata.
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Concepts: `artifacts`, `lifecycle`
- Agent notes: Idempotent; repeated tombstone calls on the same artifact are safe.
- Adjacent commands: `artifacts content get`, `artifacts create`, `artifacts get`, `artifacts list`
- Examples:
  - Tombstone artifact: `oar artifacts tombstone --artifact-id artifact_123 --reason "superseded by newer version" --json`

Body schema:
  Required: actor_id (string)
  Optional: reason (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts tombstone ... ; oar artifacts tombstone ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs list`

List documents and their current head metadata

```text
Generated Help: docs list

- Command ID: `docs.list`
- CLI path: `docs list`
- HTTP: `GET /docs`
- Stability: `beta`
- Input mode: `none`
- Why: Discover available documents without resolving each head individually, optionally scoped to a single thread.
- Output: Returns `{ documents }` ordered by `updated_at` descending.
- Error codes: `invalid_request`
- Concepts: `docs`, `revisions`
- Agent notes: Safe and idempotent. Use `thread_id` to focus on one thread's docs and `include_tombstoned=true` when auditing superseded documents.
- Adjacent commands: `docs create`, `docs get`, `docs history`, `docs revision get`, `docs tombstone`, `docs update`
- Examples:
  - List documents: `oar docs list --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs list ... ; oar docs list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs create`

Create document with initial immutable revision

```text
Generated Help: docs create

- Command ID: `docs.create`
- CLI path: `docs create`
- HTTP: `POST /docs`
- Stability: `beta`
- Input mode: `json-body`
- Why: Bootstrap a first-class document identity and initial revision without manual head-pointer management.
- Output: Returns `{ document, revision }` where `revision` is the new head.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`
- Concepts: `docs`, `revisions`
- Agent notes: Non-idempotent unless caller provides a deterministic document id and dedupes retries.
- Adjacent commands: `docs get`, `docs history`, `docs list`, `docs revision get`, `docs tombstone`, `docs update`
- Examples:
  - Create document: `oar docs create --from-file doc-create.json --json`

Body schema:
  Required: content (object|string), content_type (string), document (object)
  Optional: actor_id (string), refs (list<string>)
  Enum values: content_type: binary, structured, text

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs create ... ; oar docs create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs get`

Get document and authoritative head revision

```text
Generated Help: docs get

- Command ID: `docs.get`
- CLI path: `docs get`
- HTTP: `GET /docs/{document_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve the current authoritative document head without client-side lineage traversal.
- Output: Returns `{ document, revision }` where `revision` is the current head.
- Error codes: `invalid_request`, `not_found`
- Concepts: `docs`, `revisions`
- Agent notes: Safe and idempotent.
- Adjacent commands: `docs create`, `docs history`, `docs list`, `docs revision get`, `docs tombstone`, `docs update`
- Examples:
  - Get document head: `oar docs get --document-id product-constitution --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs get ... ; oar docs get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs history`

List ordered immutable revisions for a document

```text
Generated Help: docs history

- Command ID: `docs.history`
- CLI path: `docs history`
- HTTP: `GET /docs/{document_id}/history`
- Stability: `beta`
- Input mode: `none`
- Why: Traverse full document lineage in canonical revision-number order.
- Output: Returns `{ document_id, revisions }` ordered by ascending `revision_number`.
- Error codes: `invalid_request`, `not_found`
- Concepts: `docs`, `revisions`, `lineage`
- Agent notes: Safe and idempotent.
- Adjacent commands: `docs create`, `docs get`, `docs list`, `docs revision get`, `docs tombstone`, `docs update`
- Examples:
  - List document history: `oar docs history --document-id product-constitution --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs history ... ; oar docs history ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs revision`

Nested generated help topic.

```text
Generated Help: docs revision

Commands:
  docs revision get        Get one immutable document revision

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs revision ... ; oar docs revision ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>

Tip: `oar help <command path>` for full command-level generated details.
```

## `docs tombstone`

Tombstone a document (soft-delete)

```text
Generated Help: docs tombstone

- Command ID: `docs.tombstone`
- CLI path: `docs tombstone`
- HTTP: `POST /docs/{document_id}/tombstone`
- Stability: `beta`
- Input mode: `json-body`
- Why: Mark a document as inactive while preserving revision history and provenance.
- Output: Returns `{ document, revision }` with updated tombstone metadata.
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Concepts: `docs`, `lifecycle`
- Agent notes: Idempotent; repeated tombstone calls on the same document are safe.
- Adjacent commands: `docs create`, `docs get`, `docs history`, `docs list`, `docs revision get`, `docs update`
- Examples:
  - Tombstone document: `oar docs tombstone --document-id product-constitution --reason "replaced by v2" --json`

Body schema:
  Required: actor_id (string)
  Optional: reason (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs tombstone ... ; oar docs tombstone ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs revision get`

Get one immutable document revision

```text
Generated Help: docs revision get

- Command ID: `docs.revision.get`
- CLI path: `docs revision get`
- HTTP: `GET /docs/{document_id}/revisions/{revision_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Read a specific historical revision payload without mutating document head.
- Output: Returns `{ revision }` including metadata and revision content.
- Error codes: `invalid_request`, `not_found`
- Concepts: `docs`, `revisions`
- Agent notes: Safe and idempotent.
- Adjacent commands: `docs create`, `docs get`, `docs history`, `docs list`, `docs tombstone`, `docs update`
- Examples:
  - Get revision: `oar docs revision get --document-id product-constitution --revision-id 019f... --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs revision get ... ; oar docs revision get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events get`

Get event by id

```text
Generated Help: events get

- Command ID: `events.get`
- CLI path: `events get`
- HTTP: `GET /events/{event_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Resolve event references and evidence links.
- Output: Returns `{ event }`.
- Error codes: `not_found`
- Concepts: `events`
- Agent notes: Safe and idempotent.
- Adjacent commands: `events create`, `events stream`
- Examples:
  - Get event: `oar events get --event-id event_123 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events get ... ; oar events get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events create`

Append event

```text
Generated Help: events create

- Command ID: `events.create`
- CLI path: `events create`
- HTTP: `POST /events`
- Stability: `stable`
- Input mode: `json-body`
- Why: Record append-only narrative or protocol state changes that complement snapshots.
- Output: Returns `{ event }` with generated id and timestamp.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `events`, `append-only`
- Agent notes: Non-idempotent unless external dedupe keying is used.
- Adjacent commands: `events get`, `events stream`
- Examples:
  - Append event: `oar events create --from-file event.json --json`

Body schema:
  Required: event.provenance.sources (list<string>), event.refs (list<typed_ref>), event.summary (string), event.type (string)
  Optional: actor_id (string), event.actor_id (string), event.payload (object), event.provenance.by_field (map<string, list<string>>), event.provenance.notes (string), event.thread_id (string)
  Enum values: event.type (open): commitment_created, commitment_status_changed, decision_made, decision_needed, document_created, document_tombstoned, document_updated, exception_raised, inbox_item_acknowledged, message_posted, receipt_added, review_completed, snapshot_updated, work_order_claimed, work_order_created

Local CLI notes:
  - Common open `event.type` values include `actor_statement`; the enum list above is illustrative, not exhaustive.
  - Use `--dry-run` with `--from-file` to validate and preview the request without sending the mutation.

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events create ... ; oar events create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events stream`

Stream events via Server-Sent Events (SSE)

```text
Generated Help: events stream

- Command ID: `events.stream`
- CLI path: `events stream`
- HTTP: `GET /events/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Follow live event updates with resumable SSE semantics.
- Output: SSE stream where each event carries `{ event }` and uses event id for resume.
- Error codes: `internal_error`, `cli_outdated`
- Concepts: `events`, `streaming`
- Agent notes: Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Adjacent commands: `events create`, `events get`
- Examples:
  - Stream all events: `oar events tail --json`
  - Resume by id: `oar events tail --last-event-id <event_id> --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events stream ... ; oar events stream ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events tail`

Stream events via Server-Sent Events (SSE)

```text
Generated Help: events tail

- Command ID: `events.stream`
- CLI path: `events stream`
- HTTP: `GET /events/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Follow live event updates with resumable SSE semantics.
- Output: SSE stream where each event carries `{ event }` and uses event id for resume.
- Error codes: `internal_error`, `cli_outdated`
- Concepts: `events`, `streaming`
- Agent notes: Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Adjacent commands: `events create`, `events get`
- Examples:
  - Stream all events: `oar events tail --json`
  - Resume by id: `oar events tail --last-event-id <event_id> --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events tail ... ; oar events tail ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox list`

List derived inbox items

```text
Generated Help: inbox list

- Command ID: `inbox.list`
- CLI path: `inbox list`
- HTTP: `GET /inbox`
- Stability: `stable`
- Input mode: `none`
- Why: Surface derived actionable risk and decision signals.
- Output: Returns `{ items, generated_at }`.
- Concepts: `inbox`, `derived-views`
- Agent notes: Safe and idempotent.
- Adjacent commands: `inbox ack`, `inbox get`, `inbox stream`
- Examples:
  - List inbox: `oar inbox list --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox list ... ; oar inbox list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox get`

Get derived inbox item detail

```text
Generated Help: inbox get

- Command ID: `inbox.get`
- CLI path: `inbox get`
- HTTP: `GET /inbox/{inbox_item_id}`
- Stability: `stable`
- Input mode: `none`
- Why: Inspect one inbox item in detail before acting on it.
- Output: Returns `{ item, generated_at }` for the requested inbox item.
- Error codes: `not_found`
- Concepts: `inbox`, `derived-views`
- Agent notes: CLI supports canonical ids, aliases, and unique prefixes.
- Adjacent commands: `inbox ack`, `inbox list`, `inbox stream`
- Examples:
  - Get inbox item by canonical id: `oar inbox get --id inbox:decision_needed:thread_123:none:event_123 --json`
  - Get inbox item by alias: `oar inbox get --id ibx_abcd1234ef56 --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox get ... ; oar inbox get ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox ack`

Acknowledge an inbox item

```text
Generated Help: inbox ack

- Command ID: `inbox.ack`
- CLI path: `inbox ack`
- HTTP: `POST /inbox/ack`
- Stability: `stable`
- Input mode: `json-body`
- Why: Suppress already-acted-on derived inbox signals.
- Output: Returns `{ event }` representing acknowledgment.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `inbox`, `events`
- Agent notes: Idempotent at semantic level; repeated acks should not duplicate active inbox items.
- Adjacent commands: `inbox get`, `inbox list`, `inbox stream`
- Examples:
  - Ack inbox item: `oar inbox ack --thread-id thread_123 --inbox-item-id inbox:item-1 --json`
  - Ack inbox item by id: `oar inbox ack inbox:decision_needed:thread_123:none:event_1 --json`

Body schema:
  Required: inbox_item_id (string), thread_id (string)
  Optional: actor_id (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox ack ... ; oar inbox ack ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox stream`

Stream derived inbox items via SSE

```text
Generated Help: inbox stream

- Command ID: `inbox.stream`
- CLI path: `inbox stream`
- HTTP: `GET /inbox/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Follow live derived inbox updates without repeated polling.
- Output: SSE stream where each event carries `{ item }` derived inbox metadata.
- Error codes: `internal_error`, `cli_outdated`
- Concepts: `inbox`, `derived-views`, `streaming`
- Agent notes: Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Adjacent commands: `inbox ack`, `inbox get`, `inbox list`
- Examples:
  - Stream inbox updates: `oar inbox tail --json`
  - Resume inbox stream: `oar inbox tail --last-event-id <id> --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox stream ... ; oar inbox stream ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `inbox tail`

Stream derived inbox items via SSE

```text
Generated Help: inbox tail

- Command ID: `inbox.stream`
- CLI path: `inbox stream`
- HTTP: `GET /inbox/stream`
- Stability: `beta`
- Input mode: `none`
- Why: Follow live derived inbox updates without repeated polling.
- Output: SSE stream where each event carries `{ item }` derived inbox metadata.
- Error codes: `internal_error`, `cli_outdated`
- Concepts: `inbox`, `derived-views`, `streaming`
- Agent notes: Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Adjacent commands: `inbox ack`, `inbox get`, `inbox list`
- Examples:
  - Stream inbox updates: `oar inbox tail --json`
  - Resume inbox stream: `oar inbox tail --last-event-id <id> --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json inbox tail ... ; oar inbox tail ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `derived rebuild`

Rebuild derived views

```text
Generated Help: derived rebuild

- Command ID: `derived.rebuild`
- CLI path: `derived rebuild`
- HTTP: `POST /derived/rebuild`
- Stability: `beta`
- Input mode: `json-body`
- Why: Force deterministic recomputation of derived views after maintenance or migration.
- Output: Returns `{ ok: true }`.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `derived-views`, `maintenance`
- Agent notes: Mutating admin command; serialize with other writes.
- Examples:
  - Rebuild derived: `oar derived rebuild --actor-id system --json`

Body schema:
  Required: none
  Optional: actor_id (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json derived rebuild ... ; oar derived rebuild ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta commands`

List generated command metadata

```text
Generated Help: meta commands

- Command ID: `meta.commands.list`
- CLI path: `meta commands`
- HTTP: `GET /meta/commands`
- Stability: `beta`
- Input mode: `none`
- Why: Load generated command metadata used for help, docs, and agent introspection.
- Output: Returns generated command registry metadata from the canonical contract.
- Error codes: `meta_unavailable`, `cli_outdated`
- Concepts: `meta`, `introspection`
- Agent notes: Safe and idempotent. Response shape matches committed generated artifacts.
- Adjacent commands: `meta command`, `meta concept`, `meta concepts`, `meta handshake`, `meta health`, `meta version`
- Examples:
  - List command metadata: `oar meta commands --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta commands ... ; oar meta commands ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta command`

Get generated metadata for a command id

```text
Generated Help: meta command

- Command ID: `meta.commands.get`
- CLI path: `meta command`
- HTTP: `GET /meta/commands/{command_id}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve a stable command id to full generated metadata and guidance.
- Output: Returns `{ command }` metadata for the requested command id.
- Error codes: `not_found`, `meta_unavailable`, `cli_outdated`
- Concepts: `meta`, `introspection`
- Agent notes: Safe and idempotent.
- Adjacent commands: `meta commands`, `meta concept`, `meta concepts`, `meta handshake`, `meta health`, `meta version`
- Examples:
  - Read command metadata: `oar meta command --command-id threads.list --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta command ... ; oar meta command ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta concepts`

List generated concept metadata

```text
Generated Help: meta concepts

- Command ID: `meta.concepts.list`
- CLI path: `meta concepts`
- HTTP: `GET /meta/concepts`
- Stability: `beta`
- Input mode: `none`
- Why: Discover conceptual groupings of commands generated from contract metadata.
- Output: Returns `{ concepts }` summary metadata for all known concepts.
- Error codes: `meta_unavailable`, `cli_outdated`
- Concepts: `meta`, `concepts`
- Agent notes: Safe and idempotent.
- Adjacent commands: `meta command`, `meta commands`, `meta concept`, `meta handshake`, `meta health`, `meta version`
- Examples:
  - List concepts: `oar meta concepts --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta concepts ... ; oar meta concepts ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `meta concept`

Get generated metadata for one concept

```text
Generated Help: meta concept

- Command ID: `meta.concepts.get`
- CLI path: `meta concept`
- HTTP: `GET /meta/concepts/{concept_name}`
- Stability: `beta`
- Input mode: `none`
- Why: Resolve one concept tag to the commands that implement that concept.
- Output: Returns `{ concept }` including matched command ids and command metadata.
- Error codes: `not_found`, `meta_unavailable`, `cli_outdated`
- Concepts: `meta`, `concepts`
- Agent notes: Safe and idempotent.
- Adjacent commands: `meta command`, `meta commands`, `meta concepts`, `meta handshake`, `meta health`, `meta version`
- Examples:
  - Read one concept: `oar meta concept --concept-name compatibility --json`


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json meta concept ... ; oar meta concept ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `work-orders create`

Create work-order packet artifact

```text
Generated Help: work-orders create

- Command ID: `packets.work-orders.create`
- CLI path: `work-orders create`
- HTTP: `POST /work_orders`
- Stability: `stable`
- Input mode: `json-body`
- Why: Create structured action packets with deterministic schema enforcement.
- Output: Returns `{ artifact, event }`.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `packets`, `work-orders`
- Agent notes: Treat as non-idempotent unless artifact ids are controlled.
- Adjacent commands: `receipts create`, `reviews create`
- Examples:
  - Create work order: `oar work-orders create --from-file work-order.json --json`

Body schema:
  Required: artifact (object), packet.acceptance_criteria (list<string>), packet.constraints (list<string>), packet.context_refs (list<typed_ref>), packet.definition_of_done (list<string>), packet.objective (string), packet.thread_id (string), packet.work_order_id (string)
  Optional: actor_id (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json work-orders create ... ; oar work-orders create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `receipts create`

Create receipt packet artifact

```text
Generated Help: receipts create

- Command ID: `packets.receipts.create`
- CLI path: `receipts create`
- HTTP: `POST /receipts`
- Stability: `stable`
- Input mode: `json-body`
- Why: Record execution output and verification evidence for a work order.
- Output: Returns `{ artifact, event }`.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `packets`, `receipts`
- Agent notes: Include evidence refs that satisfy packet conventions.
- Adjacent commands: `reviews create`, `work-orders create`
- Examples:
  - Create receipt: `oar receipts create --from-file receipt.json --json`

Body schema:
  Required: artifact (object), packet.changes_summary (string), packet.known_gaps (list<string>), packet.outputs (list<typed_ref>), packet.receipt_id (string), packet.thread_id (string), packet.verification_evidence (list<typed_ref>), packet.work_order_id (string)
  Optional: actor_id (string)

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json receipts create ... ; oar receipts create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `reviews create`

Create review packet artifact

```text
Generated Help: reviews create

- Command ID: `packets.reviews.create`
- CLI path: `reviews create`
- HTTP: `POST /reviews`
- Stability: `stable`
- Input mode: `json-body`
- Why: Record acceptance/revision decisions over a receipt.
- Output: Returns `{ artifact, event }`.
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Concepts: `packets`, `reviews`
- Agent notes: Include refs to both receipt and work order artifacts.
- Adjacent commands: `receipts create`, `work-orders create`
- Examples:
  - Create review: `oar reviews create --from-file review.json --json`

Body schema:
  Required: artifact (object), packet.evidence_refs (list<typed_ref>), packet.notes (string), packet.outcome (string), packet.receipt_id (string), packet.review_id (string), packet.work_order_id (string)
  Optional: actor_id (string)
  Enum values: packet.outcome (strict): accept, escalate, revise

Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json reviews create ... ; oar reviews create ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events list`

Compose `threads timeline` responses with client-side thread/type/actor filters and preview summaries.

```text
Local Help: events list

- Kind: `local helper`
- Summary: Compose `threads timeline` responses with client-side thread/type/actor filters and preview summaries.
- Composition: Fetches one or more thread timelines locally, then filters and summarizes the events without changing contracts or core behavior.
- JSON body: `thread_id`, `thread_ids`, `events`, `total_events`, `returned_events`
- Examples:
  - `oar events list --thread-id <thread-id> --type actor_statement --mine --full-id`
  - `oar events list --thread-id <thread-id> --max-events 10`

Flags:
  --thread-id <thread-id>      Thread id to inspect (repeatable).
  --type <event-type>          Repeatable event type filter.
  --types <csv>                Comma-separated event types.
  --actor-id <actor-id>        Filter to one actor id.
  --mine                       Resolve to the active profile actor_id.
  --max-events <n>             Keep the most recent matching events.
  --full-id                    Render full event ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events list ... ; oar events list ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events validate`

Validate an `events create` payload locally from stdin or `--from-file` without sending it.

```text
Local Help: events validate

- Kind: `local helper`
- Summary: Validate an `events create` payload locally from stdin or `--from-file` without sending it.
- Composition: Parses the same JSON body accepted by `events create`, runs local validation rules, and returns a validation preview envelope without contacting core.
- JSON body: `command`, `command_id`, `path_params`, `query`, `body`, `valid`
- Examples:
  - `cat event.json | oar events validate`
  - `oar events validate --from-file event.json`

Flags:
  --from-file <path>           Load the request body from a JSON file instead of stdin.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events validate ... ; oar events validate ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `events explain`

Explain known event-type conventions, required refs, and validation hints for one type or the full catalog.

```text
Local Help: events explain

- Kind: `local helper`
- Summary: Explain known event-type conventions, required refs, and validation hints for one type or the full catalog.
- Composition: Formats the embedded event reference and validation guidance into a human-readable reference without sending a request.
- JSON body: `event_type`, `known`, `required_refs`, `payload_requirements`, `examples`, `hint`
- Examples:
  - `oar events explain`
  - `oar events explain review_completed`

Flags:
  <event-type>                 Optional event type to focus on; omit it to list known event types.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json events explain ... ; oar events explain ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `artifacts inspect`

Fetch artifact metadata and resolved content in one command for operator inspection.

```text
Local Help: artifacts inspect

- Kind: `local helper`
- Summary: Fetch artifact metadata and resolved content in one command for operator inspection.
- Composition: Loads artifact metadata with `artifacts get`, then fetches content with `artifacts content` using the resolved artifact id.
- JSON body: `artifact`, `content`, `content_headers`, `content_text`, `content_base64`
- Examples:
  - `oar artifacts inspect --artifact-id <artifact-id>`
  - `oar artifacts inspect <artifact-id-or-alias>`

Flags:
  --artifact-id <artifact-id>  Artifact id or unique alias to inspect.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json artifacts inspect ... ; oar artifacts inspect ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads inspect`

Canonical thread coordination read path: compose one view from `threads context` and related `inbox list` items.

```text
Local Help: threads inspect

- Kind: `local helper`
- Summary: Canonical thread coordination read path: compose one view from `threads context` and related `inbox list` items.
- Composition: Resolves one thread by id or discovery filters, loads `threads context`, then filters inbox items client-side by `thread_id` for one operator-focused coordination view.
- JSON body: `thread`, `context`, `collaboration`, `inbox`
- Examples:
  - `oar threads inspect --thread-id <thread-id>`
  - `oar threads inspect --status active --type initiative --full-id`

Flags:
  --thread-id <thread-id>      Thread id to inspect.
  --status <status>            Discover one thread by status.
  --priority <priority>        Discover one thread by priority.
  --stale <bool>               Discover one thread by stale state.
  --tag <tag>                  Repeatable discovery tag filter.
  --cadence <cadence>          Repeatable discovery cadence filter.
  --type <thread-type>         Local discovery filter after `threads list`.
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from `threads context`.
  --full-id                    Render full event and inbox ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads inspect ... ; oar threads inspect ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads workspace`

Single holistic thread coordination read: combine context, inbox, recommendation review, and related-thread signals in one command.

```text
Local Help: threads workspace

- Kind: `local helper`
- Summary: Single holistic thread coordination read: combine context, inbox, recommendation review, and related-thread signals in one command.
- Composition: Resolves one thread by id or discovery filters, loads `threads context`, adds thread-scoped inbox items from `inbox list`, and follows related thread refs for additional review context.
- JSON body: `thread`, `context`, `collaboration`, `inbox`, `pending_decisions`, `related_threads`, `related_recommendations`, `related_decisions`, `follow_up`
- Examples:
  - `oar threads workspace --thread-id <thread-id> --full-id`
  - `oar threads workspace --thread-id <thread-id> --include-related-event-content --verbose`
  - `oar threads workspace --status active --type initiative --full-summary`

Flags:
  --thread-id <thread-id>      Thread id to inspect.
  --status <status>            Discover one thread by status.
  --priority <priority>        Discover one thread by priority.
  --stale <bool>               Discover one thread by stale state.
  --tag <tag>                  Repeatable discovery tag filter.
  --cadence <cadence>          Repeatable discovery cadence filter.
  --type <thread-type>         Local discovery filter after `threads list`.
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from `threads context`.
  --include-related-event-content Hydrate related review items with full `events get` content in one command.
  --full-summary               Show full recommendation/decision summaries in human output.
  --full-id                    Render full event and inbox ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads workspace ... ; oar threads workspace ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads recommendations`

Review one thread's recommendation/decision inputs plus related-thread signals with provenance and follow-up hints.

```text
Local Help: threads recommendations

- Kind: `local helper`
- Summary: Review one thread's recommendation/decision inputs plus related-thread signals with provenance and follow-up hints.
- Composition: Resolves one thread by id or discovery filters, loads `threads context`, adds thread-scoped pending decision inbox items from `inbox list`, and follows related thread refs for additional review context.
- JSON body: `thread`, `recommendations`, `decision_requests`, `decisions`, `pending_decisions`, `related_threads`, `related_recommendations`, `follow_up`
- Examples:
  - `oar threads recommendations --thread-id <thread-id> --full-id`
  - `oar threads recommendations --thread-id <thread-id> --include-related-event-content --verbose`
  - `oar threads recommendations --status active --type initiative --full-summary`

Flags:
  --thread-id <thread-id>      Thread id to review.
  --status <status>            Discover one thread by status.
  --priority <priority>        Discover one thread by priority.
  --stale <bool>               Discover one thread by stale state.
  --tag <tag>                  Repeatable discovery tag filter.
  --cadence <cadence>          Repeatable discovery cadence filter.
  --type <thread-type>         Local discovery filter after `threads list`.
  --max-events <n>             Maximum recent context events to include.
  --include-artifact-content   Include artifact content previews from `threads context`.
  --include-related-event-content Hydrate related review items with full `events get` content in one command.
  --full-summary               Show full recommendation/decision summaries in human output.
  --full-id                    Render full event and inbox ids in human output.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads recommendations ... ; oar threads recommendations ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads patch`

Stage a thread patch proposal locally and show the diff before applying it.

```text
Local Help: threads patch

- Kind: `local helper`
- Summary: Stage a thread patch proposal locally and show the diff before applying it.
- Composition: Resolves the thread id, fetches current state with `threads get`, computes a local diff, and persists a proposal file instead of sending the patch immediately.
- JSON body: `proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`
- Examples:
  - `oar threads patch --thread-id <thread-id> --from-file patch.json`
  - `cat patch.json | oar threads patch --thread-id <thread-id>`

Flags:
  --thread-id <thread-id>      Thread id to patch.
  --from-file <path>           Load the patch body from a JSON file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads patch ... ; oar threads patch ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `threads apply`

Apply a previously staged thread patch proposal.

```text
Local Help: threads apply

- Kind: `local helper`
- Summary: Apply a previously staged thread patch proposal.
- Composition: Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `threads.patch` request.
- JSON body: `proposal_id`, `target_command_id`, `applied`, `kept`, `result`
- Examples:
  - `oar threads apply --proposal-id <proposal-id>`
  - `oar threads apply <proposal-id-prefix>`

Flags:
  --proposal-id <proposal-id>  Proposal id or unique prefix to apply.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json threads apply ... ; oar threads apply ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments update`

Stage a commitment update proposal locally and show the diff before applying it.

```text
Local Help: commitments update

- Kind: `local helper`
- Summary: Stage a commitment update proposal locally and show the diff before applying it.
- Composition: Resolves the commitment id, fetches current state with `commitments get`, computes a local diff, and persists a proposal file instead of sending the patch immediately.
- JSON body: `proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`
- Examples:
  - `oar commitments update --commitment-id <commitment-id> --from-file patch.json`

Flags:
  --commitment-id <commitment-id> Commitment id to update.
  --from-file <path>           Load the patch body from a JSON file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments update ... ; oar commitments update ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `commitments apply`

Apply a previously staged commitment update proposal.

```text
Local Help: commitments apply

- Kind: `local helper`
- Summary: Apply a previously staged commitment update proposal.
- Composition: Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `commitments.patch` request.
- JSON body: `proposal_id`, `target_command_id`, `applied`, `kept`, `result`
- Examples:
  - `oar commitments apply --proposal-id <proposal-id>`

Flags:
  --proposal-id <proposal-id>  Proposal id or unique prefix to apply.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json commitments apply ... ; oar commitments apply ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs update`

Stage a document update proposal locally and show the content diff before applying it.

```text
Local Help: docs update

- Kind: `local helper`
- Summary: Stage a document update proposal locally and show the content diff before applying it.
- Composition: Fetches the current document revision with `docs get`, computes a local diff against the proposed update, and persists a proposal file instead of sending the update immediately.
- JSON body: `proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`
- Examples:
  - `oar docs update --document-id <document-id> --content-file <path>`
  - `cat update.json | oar docs update --document-id <document-id>`

Flags:
  --document-id <document-id>  Document id to update.
  --content-file <path>        Load multiline content from a file into the JSON payload.
  --from-file <path>           Load the full JSON update body from a file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs update ... ; oar docs update ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs content`

Show the current document content together with authoritative head revision metadata.

```text
Local Help: docs content

- Kind: `local helper`
- Summary: Show the current document content together with authoritative head revision metadata.
- Composition: Loads `docs get`, then renders the current revision content and metadata in one operator-friendly response.
- JSON body: `document`, `revision`, `content`, `status_code`, `headers`
- Examples:
  - `oar docs content --document-id <document-id>`
  - `oar docs content <document-id-or-alias>`

Flags:
  --document-id <document-id>  Document id or unique alias to inspect.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs content ... ; oar docs content ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs validate-update`

Validate a `docs update` payload locally from stdin or file without sending the mutation.

```text
Local Help: docs validate-update

- Kind: `local helper`
- Summary: Validate a `docs update` payload locally from stdin or file without sending the mutation.
- Composition: Parses the same body accepted by `docs update`, expands `--content-file` when present, and returns a validation preview envelope without contacting core.
- JSON body: `command`, `command_id`, `path_params`, `query`, `body`, `valid`
- Examples:
  - `cat update.json | oar docs validate-update --document-id <document-id>`
  - `oar docs validate-update --document-id <document-id> --content-file body.md`

Flags:
  --document-id <document-id>  Document id to validate against.
  --content-file <path>        Load multiline content from a file into the JSON payload.
  --from-file <path>           Load the full JSON update body from a file.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs validate-update ... ; oar docs validate-update ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```

## `docs apply`

Apply a previously staged document update proposal.

```text
Local Help: docs apply

- Kind: `local helper`
- Summary: Apply a previously staged document update proposal.
- Composition: Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `docs.update` request.
- JSON body: `proposal_id`, `target_command_id`, `applied`, `kept`, `result`
- Examples:
  - `oar docs apply --proposal-id <proposal-id>`
  - `oar docs apply <proposal-id-prefix>`

Flags:
  --proposal-id <proposal-id>  Proposal id or unique prefix to apply.


Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json docs apply ... ; oar docs apply ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>
```
