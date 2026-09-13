# Unified work and conversational PM

Work is an existing card, projects use topics, and source observations extend the
same workspace SQLite. `/work` serves board/table/PM/CLI projections. External
source identity `(authority, connection_id, native_id)` deduplicates registration.
Local annotations and source workflow fields have separate mutation paths.

## Work API

- `GET /work`: filters `project_ref`, `source`, `owner`, `phase`, `freshness`, `q`;
  bounded `limit` (1..200, default 50) and opaque `cursor`.
- `POST /work`: existing `board_ref`, `title`, optional source, objective details,
  owner, project and annotations. External sources require connection/native IDs.
- `GET /work/{card_ref}`: canonical card fields, source, normalized phase,
  next actor/action, evidence, freshness and refresh lifecycle.
- `PATCH /work/{card_ref}`: `{if_version,patch}` changes local annotations only.
  Native content/status changes use existing card revision/move APIs and their
  completion evidence gate. External card revisions/moves/updates are rejected.
- `POST /work/{card_ref}/observations`: attributed, replay-safe observation.
  Exact replay returns the original; conflicting reuse returns 409. Client
  `status=verified` remains `verification=reported`, never independent proof.
- `GET /work/{card_ref}/observations`: append-only evidence history.
- `POST /work/{card_ref}/refresh`: returns 202 queued, coalescing active work.
  Queue acceptance does not mean that a source read succeeded.
- `GET /work/capabilities`: includes whether an executor is configured.

All routes use existing workspace authentication and server-resolved actor
provenance. Public responses exclude private refresh lease tokens. An empty
`next_cursor` ends pagination. Source-native status is preserved, including
unknown values. Last observed, source activity, and meaningful progress are
separate clocks. Source sequence orders facts; observation time orders attempt
health, so unsequenced failures can still surface after sequenced successes.

## Read-only refresh runtime

`ANX_OBSERVATION_CONFIG` optionally names an operator-owned JSON file (regular,
no group/world write permission, maximum 64 KiB). No API request can configure
reader code, credentials, host paths, or destinations. The runtime validates
exact work/source binding before a read. At most 200 configured targets run
serially with per-reader deadlines. Persisted leases fence stale workers; retry
backoff and rate-limit hints survive service recreation. Failure retains the
last good evidence. Unconfigured targets remain queued and visibly unexecuted.

Example using a public GitHub issue and a credential *handle*, not a secret.
`make serve` sets `ANX_OBSERVATION_CONFIG` to `core/dev/observation.serve.json`
when that file exists: a builtin GitHub reader plus a JIT C transform against
`Git-on-my-level/agent-nexus#208`. `core/scripts/dogfood-jit.sh` compiles
`core/dev/github-transform.c`, then stages, validates, canaries and activates
the artifact. A Linux host is not available in this worktree; `make -C core check`
cross-compiles `./internal/observation` with `GOOS=linux`.

```json
{
  "targets": [{
    "work_ref": "card:release-review",
    "source_native_id": "example/project#42",
    "target": {
      "connection_id": "github-main",
      "source": "github",
      "kind": "issue",
      "native_id": "42",
      "repository": "example/project"
    },
    "base_url": "https://api.github.com",
    "credential_env": "ANX_GITHUB_READ_TOKEN",
    "interval_seconds": 300,
    "stale_after_seconds": 900,
    "timeout_seconds": 30,
    "max_backoff_seconds": 3600
  }]
}
```

Work must first be registered with matching source authority, connection ID and
source-native ID. Workspace ID is injected from the running deployment and
cannot cross workspace boundaries. GitHub and Multica use bounded HTTPS readers
with redirects disabled. Multica also requires `source_workspace_id` and its
actual approved HTTPS `base_url`. Alternatively, set `transport=multica_cli`,
`cli_binary` to the approved installed absolute executable, and `cli_profile` to
an existing authorized profile. That trusted CLI owns its source authentication
and network policy; no token extraction or credential copying occurs. Explicit
`allowed_networks` CIDRs for the HTTP transport can approve
private destinations. The environment handle resolves only the named variable
in memory; choose source-side read-only credentials.

For `ssh_git`, configure the exact target `host`, absolute repository `path`,
`known_hosts_file`, optional `identity_file` and `ssh_port`. Existing key and host
files are read in place. No forwarding, interactive prompts, caller-controlled
commands, or source writes are introduced. SSH Git evidence establishes code
state, not deployment success.

## Generated readers and isolation

`transport: "jit"` wraps a trusted built-in source reader. Generated code is a
stdin/stdout transform over that snapshot: no network, no credentials, no host
path grants. Linux executes the artifact under bubblewrap (`bwrap --unshare-all`,
prlimit). macOS executes it under `sandbox-exec` with a deny-default Seatbelt
profile: process-exec of the artifact, scratch writes, `(deny network*)`,
`(deny process-fork)`, and content-read denials for `/Users`, `/Volumes` (except
the artifact and scratch), keychains and `/private/etc`. Hosts without an
enforced runner fail closed. A managed directory is not a sandbox.
JIT refresh checks runner availability before requiring an active revision, so
an activation blocked by a failed probe reports `isolation_unavailable` with the
probe reason in both `refresh.last_error` and `freshness.last_error`. The last
good observation remains intact. `policy_denied` / "Generated reader has no
active version" applies only when the runner is available and no revision is active.

macOS 26 dyld requires a literal `/` read grant; other ancestors retain only
metadata access. Artifact, scratch, and dyld subtree grants remain bounded.
Signals are limited to `(target self)`. Sysctl reads require a broad allow:
Go reads `CTL_HW/HW_PAGESIZE` by numeric MIB, which an exact `sysctl-name`
allowlist does not satisfy on this OS. Explicit denies for `kern.procargs2`
and the `kern.proc` name prefix block the by-name reads. Known limit,
verified on macOS 26.6.2: Seatbelt's sysctl filters do not see numeric MIBs,
so a same-uid numeric `KERN_PROCARGS2` read still succeeds under every
variant of this rule. The profile therefore does not protect the argv or
environment of other same-uid processes; do not keep secrets in the
environment of long-lived same-uid processes on a host that runs generated
readers.
The profile and controlled process-argument denial/Go runtime probes live in
`internal/observation/isolation_integration_test.go`. Require
`ANX_OBSERVATION_ISOLATION_TEST=1` for host qualification; unavailable enforcement
must fail that gate rather than count as successful runtime proof.

Seatbelt cannot set Darwin `RLIMIT_AS`/`DATA`/`RSS` (the kernel returns
Invalid argument) and must not set `RLIMIT_NPROC` (it is user-global). CPU,
file size, open files and core dumps are applied with `ulimit` in a trusted
`/bin/sh` wrapper around `sandbox-exec`, analogous to Linux `prlimit`. Wall
timeout and output bytes are enforced in Go. `memory_bytes` is validated
(16 MiB through 1 GiB) but not enforced on Darwin; Linux enforces per-process
virtual address space with `prlimit --as=memory_bytes`, not aggregate RSS.
Darwin scratch is writable within the private runner directory; the file-size
limit is per file, with no aggregate byte/inode quota. Linux `/tmp` is read-only.
Never carry secrets in anx-core's environment on a shared-uid reader host.

Example JIT target fields: `jit_state_root` (absolute 0700 directory),
`jit_adapter_id`, and `jit_policy` with bounded isolation limits.

### Dogfood placement

`anx pm serve` and any process holding secrets in its environment must run under
a different uid (or on a different host) from anx-core's JIT runner. Seatbelt
cannot block numeric `KERN_PROCARGS2` reads of same-uid process environments.
The availability probe checks only that the runner must not run as root; it does
not verify this deployment separation.

## Conversational PM runtime

The PM service uses the same SQLite and the current workspace principal.
Conversation histories remain actor scoped. Decision authorization rechecks
current principals; a request cannot claim to be human in a body/header.

PM decision targets must be live: trashed, archived and purged work all project
`work_missing: true`, `target_current: false`, `already_at_target: false`, and
`can_answer: false`. The owner may still decline. Proposing against non-live work
returns `404 not_found`; fresh approval, dispatch and reconcile return
`409 source_revision_changed`, reason `work_missing`, with the approved revision
and `current_revision: null`, before any source call. Recorded answer replays
remain idempotent. Dispatch durably fails an unsent pending action with a failed
attempt and the receipt "The task this approval refers to no longer exists
(trashed or purged); nothing was sent", allowing human acknowledgement.
Reconcile cannot resurrect it, and transient reader errors do not change durable
state. Principal and decision ownership checks precede these diagnoses.

Executor routing is independent of work liveness. Decisions capture trusted
`source_authority` and copy it to their actions; reads derive `deliverable` and
`delivery_path` from the configured executor registry. Legacy records use work
as a fallback. If that work was already purged and no source was captured,
`deliverable: null` and `delivery_path: unknown` explicitly mean unknown routing,
not a missing executor. Dispatch and reconcile still fail with `work_missing`.
Empty decision `payload.resolution` summaries are omitted.

The built-in `work.annotate` scope supports Nexus-native local annotations only.
Its exact instruction is a JSON patch object, for example
`{"next_action":"Review acceptance evidence"}`. Propose a decision with the work
metadata version in `target_revision`, answer it as a human, then dispatch. Both
`work.annotate` and `work.phase` perform post-commit canonical read-back in that
dispatch and return `verified` with `independently_verified: true` when the
requested outcome matches. External executor reports remain `source_reported`
until independently verified. A changed version rejects stale approval. No external source
write executor, deployment tool, or shell action is enabled by default.

PM conversation creation, context inspection, and queued turns work without a
model and without the wake-routing bridge. `POST /pm/conversations/{id}/messages`
queues status `sending`. `POST /pm/turns/claim` hands the next queued turn to one
runner with an exclusive lease; `POST /pm/turns/{id}/complete` and
`POST /pm/turns/{id}/fail` require an active lease and its matching token.
Turn context and proposal operations also require an active lease. An open turn
without one returns `409 lease_required` for a missing token and
`409 lease_mismatch` for an expired, released, or stale token. Claim again after
release or lease expiry. Identical terminal completion/failure replays accept
the terminal owner token without mutation; other tokens return `409 lease_mismatch`.
Legacy tokenless bridge reply events cannot complete a turn: bridge runners must
claim and call the authenticated completion endpoint with that lease token.
Do not place lease tokens in durable events or public context.
`POST /pm/turns/{id}/release` requires the selected PM actor plus the current
`runner_id` and `lease_token`. It clears an unexpired lease and returns the turn
to the queue as `sending`, `claimed: false`, retaining `claimed_at`. A different
runner or wrong token returns `409 lease_mismatch`; no active lease returns
`409 turn_not_claimed`. Runners should
stop local execution, then release on SIGINT/SIGTERM with a bounded shutdown
request independent of the canceled run context. An abrupt kill cannot release;
a restart with the same runner ID recovers its existing lease and token.
Every HTTP turn representation includes read-only `claimed`, derived from the
current unexpired lease, plus `claimed_at` when the latest claim time is known.
That timestamp survives completion and expiry; older records omit it. Public
turns (including history, single-turn reads, and message replays) omit lease
credentials. Active `lease_owner` (runner ID) is visible to the requesting actor
and configured PM actor; only claim returns the token. Claim and heartbeat
return the lease expiry. The configured
PM actor may read any turn in its workspace using `pm.respond` authorization;
the requesting actor retains conversation read authorization.
Turn admission returns `429 busy` with `error.details.reason: conversation` and
`turn_id` for the blocking conversation turn, or `reason: capacity` plus workspace
`in_flight` and `limit`. Conversation serialization wins if both constraints apply.
The reason and counts are observed inside the admission transaction.
`ANX_PM_AGENT_ACTOR_ID` is required for turn creation and restricts claim/heartbeat/release/complete/fail to that actor;
`make serve` sets it to the seeded Studio PM (`actor-gds-pm` / `dev.pm`).

The optional existing-bridge path is separate. `ANX_PM_BRIDGE_ENABLED` defaults
false. Enabling it requires `ANX_PM_AGENT_ACTOR_ID`, `ANX_PM_AGENT_HANDLE`, and
`ANX_PM_RUNTIME_ENVELOPE_ENFORCED=true` only when an independently enforced
read-only capability envelope already exists for that actor. The env flag is
an operator attestation, not the envelope. Optionally `ANX_PM_BASE_URL`.
Core then checks the selected principal's current wake registration and online
state. A prompt, directory, or bounded-process wrapper is not isolation.
Missing or unready providers return unavailable, not canned text.
GitHub/Multica/SSH source writes stay unavailable until a dedicated authorized
executor is supplied.

Native `/threads`, `/events`, `/artifacts`, and inbox routes hide PM conversation
records from anyone who is not the conversation owner or the selected PM agent.
Missing identity fails closed (the resource looks absent).

Channel ingress is webhook/interaction only: `POST /pm/ingress/telegram` and
`POST /pm/ingress/discord`. Configure `ANX_PM_TELEGRAM_WEBHOOK_SECRET` (at least
32 characters), `ANX_PM_TELEGRAM_BOT_ID`, `ANX_PM_DISCORD_PUBLIC_KEY`, and
`ANX_PM_DISCORD_APPLICATION_ID` from deployment secrets. Bot tokens
(`ANX_PM_TELEGRAM_BOT_TOKEN`, `ANX_PM_DISCORD_BOT_TOKEN`) are env-only and never
written to the tree. Ingress does not call `setWebhook`, poll `getUpdates`, or
open a Discord gateway. Production bot streams must not be redirected here.

Test-only API bases `ANX_PM_TELEGRAM_API_BASE` and `ANX_PM_DISCORD_API_BASE`
point outbound delivery at local fakes under `tests/channels/`. Those fakes
speak the real Bot API / Discord REST and interaction shapes, including secret
and Ed25519 checks, and can inject 409/429. They are never production config.
`anx pm channels doctor` checks secret presence/shape, GET webhook
reachability, and binding state without sending a message.

A channel identity must be bound (`POST /pm/bindings` / `anx pm bindings create`)
before it can queue a turn. Unbound identities get a bounded bind-first reply.
Decisions awaiting the requester are offered with Telegram inline keyboards or
Discord components; `/pm-approve` and `/pm-reject` (and Discord slash
equivalents) record an answer against the displayed revision. A stale
revision is rejected and a fresh card is queued. Transport receipt (message
id) is separate from human acknowledgement. 429/409 retry with capped
exponential backoff; unknown send outcomes stay visible and are not
auto-retried.

The PM peer package owns channel authentication, exact identity mappings,
durable outboxes, and source-action state machines. Real channel delivery, a
configured read-only PM actor, and integration-specific external source
writes/read-back remain deployment and qualification requirements; fixture tests
are not evidence of those capabilities.

## Checks and migration

Migration 26 adds card-owned metadata and append-only observations, preserving
existing cards. Existing cards project as native work at metadata version zero
until their first metadata mutation. No parallel tracker database is created.

Focused tests:

```sh
cd core
go test ./internal/primitives -run TestWork
go test ./internal/server -run 'TestWork|TestObservationRuntime|TestPMRuntime'
```

Run `make -C core check` and `make contract-check` for component/contract gates.
Peer `internal/pm` and `internal/observation` packages must be integrated before
building the runtime-wiring commit.

### PM phase actions and identity boundaries

A `work.phase` decision carries `payload: {"phase": "ready"}` (or backlog,
in_progress, blocked, review, done). The instruction is explanatory prose and
does not supply mutation parameters. Approvals without a valid structured target
are rejected. For done, `payload.resolution_refs` must contain evidence references;
execution still uses the canonical card completion gate.

Nexus-owned phase actions use the board move transaction with a work revision
precondition. All board moves advance the work revision; board and work reads
therefore share the same phase. Execution reads back the canonical phase and
reconciliation records verification. Source-owned phase requests never mutate
the projection. Without a source executor, dispatch returns 503 `unavailable`
with an explanation and leaves the approved pending action unchanged, including
its revision and attempts. Action get/list/page responses derive `deliverable`
from configured scope/source routing; clients should hide Deliver when false.
Pending reconciliation returns 400 `invalid_request`: nothing has been delivered
yet, so there is nothing to read back.

Decision and action reads stay workspace-visible, subject to existing read
authorization. Only the decision's human actor may answer; clients must use the
derived `can_answer` field for answer controls. Dispatch revalidates that the
decision owner, answering actor and action approver agree, as well as current
approval permission, scope, structured payload and target revision.

Set `ANX_PM_AGENT_ACTOR_ID` to the selected PM identity. With an empty value, core
warns at startup and turn creation/response operations fail closed. Claim,
context, proposal, completion and failure operations require that configured,
currently authorized actor. Direct proposals require a human principal; agents
must propose through a turn addressed to a currently authorized human. An agent's
own conversation cannot create an unanswerable self-addressed decision.

New proposals reuse an awaiting decision for the same workspace, human, work_ref
and scope only when instruction, payload, target revision and origin match.
Changed intent atomically supersedes the earlier awaiting decision, records
`superseded_by` and `superseded_reason`, creates the replacement, and links it
in `turn.decision_ids`. Old turn links remain as audit history.

Request keys remain immutable. Reusing one with changed content or target revision
returns 409 with `error.details.existing_decision_id`; revisions are opaque source
identifiers and cannot be ordered. Clients can link the existing decision or use
a new key to propose replacement intent.

Conversation history and `GET /pm/turns/{turn_id}` expire open turns on read.
The existing five-second PM maintenance tick also expires them without any runner,
channel sender, or read. The required `deadline` is an RFC3339 timestamp. Expired
turns become failed, lose their lease, and report: "The PM did not answer before
the deadline. Retry, or check that a runner is attached."
They also expose `failure_kind: "expired"`, including legacy deadline failures.
A 409 `turn_closed` error includes `error.details.failure_kind: "expired"` and
says "expired at <deadline>" with the RFC3339 deadline. Ordinary terminal failures
remain distinct and do not acquire an expiry reason just because time has passed.

Claim first recovers an unexpired lease for the same PM actor and `runner_id`,
even at capacity. Recovery preserves the token, lease expiry, `claimed_at`, and
revision. Each runner ID must identify one serial worker; concurrent workers
must use different IDs. Other runners cannot steal an unexpired lease. After
lease expiry, a fresh claim rotates the token until the turn deadline. No owned
lease and no capacity or free work returns 204. Recovery, capacity counting, and
allocation share one SQLite transaction. Unpaginated service reads
walk all batches internally. PM decision/action/conversation pages return newest
`created_at` first, with descending internal rowid as the tiebreaker. New actions
record creation time at approval; legacy actions use their decision creation time.
Work lists return newest `updated_at` first, then descending card id. Opaque
cursors retain both ordering values, so newer inserts and changes ahead of the
cursor appear on a fresh first page without shifting the older continuation.
Cursors from the previous ordering must be discarded; response shapes are unchanged.

A stale source revision at dispatch persists a finished failed attempt and marks
the action failed, while returning the existing 409 `source_revision_changed`.
The receipt includes both approved and current revisions and explains that a
fresh proposal and approval are needed.
The decision remains answered; repeat dispatch never revives the failed action,
even if the source revision changes back. Propose with a fresh request key and
approve the current revision to deliver again.

Reconciliation preserves read-back receipts even when they cannot advance the
action's monotonic status. `reconciliation_conflict: true` marks that mismatch;
clients should display the receipt detail rather than infer success from status
alone. This does not authorize a blind retry.

Human acknowledgement records `acknowledged_by` and `acknowledged_at` and sets
visible status `acknowledged` while preserving existing receipts and attempts.
The decision actor may also acknowledge an undeliverable `pending_delivery`
action. Core retains its approval and the no-delivery-path reason in
`receipt.detail` (if empty). It cannot be delivered or reconciled after
acknowledgement, even if routing becomes available; a fresh proposal and approval
are required. Clients fold it under Handled. Acknowledgement moves
the row out of Needs you but does not freeze source reconciliation. Later
read-back can advance an unknown receipt to `verified` or `failed`, updating the
action normally while retaining both acknowledgement fields and attempt history.
An inconclusive read can refresh receipt detail while keeping the row acknowledged.
Failed actions with no sent attempt still return `invalid_request` on reconcile,
including after acknowledgement. Clients must use acknowledgement metadata to
keep handled rows out of Needs you even if subsequent source status is `failed`.

`make contract-gen` and `make contract-check` regenerate all consumer mirrors. UI phase drags
and CLI PM proposals must send the structured payload; legacy prose-only pending
decisions must be rejected/superseded and proposed again with that payload.

### Proposal attribution, revision fences, and context pagination

New decisions persist `proposed_by` (the proposing principal actor ID) separately
from `actor_id` (the human who can answer and dispatch). `origin_kind` is
`human` for direct proposals, `channel` for direct proposals with a validated
channel origin, and `pm_turn` for the selected PM principal's turn proposals.
Turn proposals record their originating `turn_id`. Superseded records retain
their own attribution; reusing identical intent retains the original proposal's
turn. A different proposer or origin kind is different intent. Legacy decisions
without recorded provenance omit these fields rather than inventing attribution.

When a proposal replaces an awaiting decision, the create/propose response
includes `supersedes` (the replaced decision ID), `supersedes_proposed_by`, and
`supersedes_origin_kind`. The old decision records `superseded_by_proposed_by`
and `superseded_by_origin_kind` from its replacement; unknown legacy values are
omitted. These fields persist on the replacement for retries and
later reads; unknown legacy attribution is omitted. Clients should tell the
reader whose earlier proposal was replaced, including when a human drag replaces
a PM turn proposal.

A human rejection records `declined`, without creating an action. `superseded`
is reserved for proposals replaced by another decision. Reads project legacy
`superseded` rows without `superseded_by` as `declined` without rewriting them.
Identical rejection retries remain idempotent for both formats; a declined
decision cannot be approved or dispatched.

Every work response exposes read-only `decision_revision`. Clients must copy it
verbatim into proposal `target_revision`. It is the nonempty `source.revision`
when authority is external, otherwise `<work_metadata.version>.<head_revision_number>`
with decimal components (for example `0.1` becomes `0.2` after a card content revision
and `1.1` after a metadata-only edit).
Dispatch uses the same helper. `freshness.source_revision` is not a fallback.
A canonical metadata mutation or card content revision invalidates a fallback fence;
refresh attempts, failures, and freshness bookkeeping do not change it;
a known external revision remains the fence independently of local version changes.

PM context uses canonical work cursors: send the returned `next_cursor` as
`cursor` with the same query, and keep `limit` within 1..50. A work-specific
context is one item and rejects cursors with 400 and "cursor is not valid for a
work-scoped context". Malformed cursors (including oversized cursors) return 400
and "cursor is malformed or from another scope".

Pending actions and failed actions with no sent attempt return 400
`invalid_request`: "Nothing has been delivered yet, so there is nothing to read back."
New action attempts record `sent_at` at entry to the executor handoff boundary,
before invoking it; crash uncertainty is conservatively treated as a possible
send. Preflight failures omit `sent_at`. Native validation and store failures
before commit are terminal `failed` receipts with the original cause, and core
clears the provisional `sent_at`. Native stores explicitly mark commit errors
and post-commit errors as uncertain; dispatch reads canonical state with a fresh,
bounded context before selecting `verified`, `failed` (fields do not match), or
`unknown` (read-back unavailable). Explicit reconciliation also returns `failed`
when canonical native fields do not match, so previously unknown native attempts
can leave Watching on read-back. Historical attempt markers are retained.
Only external executor errors use "Source
handoff outcome is unknown". Legacy failed attempts without a handoff
marker cannot establish delivery; other legacy delivery states still permit
read-back. A sent failure can be reconciled but never changes from `failed` to
`unknown`; inconclusive receipts remain visible with `reconciliation_conflict`.

PM authorization caches only actor-to-principal IDs for at most 30 seconds,
bounded to 256 entries. Every cache hit reloads current principal authority and
wake routing from auth storage. A revoked/mismatched principal invalidates the
cached identity immediately on the next check, including external database
revocations. No authorization result is cached.

Answering or dispatching a superseded decision returns 409 `conflict` with
`error.details.status: superseded` and `error.details.superseded_by` (the
replacement ID). This also covers a
supersession racing the answer CAS. An identical replay of a rejection remains
idempotent. Clients can link the replacement directly from the conflict response.

PM annotate proposals validate `instruction` as a nonempty JSON object using the
canonical primitives annotation validator before insertion. The allowed keys are
`project_ref`, `priority`, `next_actor`, `next_action`, `blockers`, `wake_condition`,
`start_at`, `due_at`, `relations`, and `executions`. Invalid keys and value shapes
return `400 invalid_request`, naming the offending keys and allowed set.

Runner leases use `ANX_PM_LEASE_TTL` (default `60s`, range `1s` to `10m`). Runners
must call `POST /pm/turns/{turn_id}/heartbeat` with `{"lease_token":"..."}` at a
cadence strictly less than TTL/2. Heartbeat returns `200 PMHeartbeatTurn` with renewed
`lease_expires_at`, capped by the turn deadline, without returning the token.
Missing tokens return `409 lease_required`; expired, released, and foreign tokens
return `409 lease_mismatch`. Closed turns return `409 turn_closed`. Lease expiry
makes the same open turn claimable with a fresh token; it does not fail the turn
or erase its history. The wire status stays `sending` (or `unknown` for an uncertain
wake); `claimed` becomes false and the turn is eligible for the next claim.
Only the turn deadline fails an unanswered turn. Complete/fail/release retain
their existing live-lease semantics.

A pending human proposal blocks changed PM proposals only for the same approver
actor, work, and scope. Another actor's human proposal does not block the current
actor's PM. This matches the actor boundary used for deduplication and supersession.
