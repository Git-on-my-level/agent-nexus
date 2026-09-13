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
timeout and output bytes are enforced in Go.

Example JIT target fields: `jit_state_root` (absolute 0700 directory),
`jit_adapter_id`, and `jit_policy` with bounded isolation limits.

## Conversational PM runtime

The PM service uses the same SQLite and the current workspace principal.
Conversation histories remain actor scoped. Decision authorization rechecks
current principals; a request cannot claim to be human in a body/header.

The built-in `work.annotate` scope supports Nexus-native local annotations only.
Its exact instruction is a JSON patch object, for example
`{"next_action":"Review acceptance evidence"}`. Propose a decision with the work
metadata version in `target_revision`, answer it as a human, dispatch, then
reconcile. Dispatch records `source_reported`; separate canonical read-back can
record `verified`. A changed version rejects stale approval. No external source
write executor, deployment tool, or shell action is enabled by default.

PM conversation creation, context inspection, and queued turns work without a
model and without the wake-routing bridge. `POST /pm/conversations/{id}/messages`
queues status `sending`. `POST /pm/turns/claim` hands the next queued turn to one
runner with an exclusive lease; `POST /pm/turns/{id}/complete` and
`POST /pm/turns/{id}/fail` require that lease token when a lease is held.
Every HTTP turn representation includes read-only `claimed`, derived from the
current unexpired lease, plus `claimed_at` when the latest claim time is known.
That timestamp survives completion and expiry; older records omit it. Public
turns (including history, single-turn reads, and message replays) omit lease
credentials. Only the authenticated claim response returns the lease token,
owner, and expiry needed by the runner protocol.
`ANX_PM_AGENT_ACTOR_ID` is required for turn creation and restricts claim/complete/fail to that actor;
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

Claim allocates new work; it never reoffers an active lease, even to the same
runner. No capacity or no free work returns the existing 204 response. Capacity
counting and allocation share one SQLite transaction. Unpaginated service reads
walk all batches internally. PM decision/action/conversation pages return newest
`created_at` first, with descending internal rowid as the tiebreaker. New actions
record creation time at approval; legacy actions use their decision creation time.
Work lists return newest `updated_at` first, then descending card id. Opaque
cursors retain both ordering values, so newer inserts and changes ahead of the
cursor appear on a fresh first page without shifting the older continuation.
Cursors from the previous ordering must be discarded; response shapes are unchanged.

A stale source revision at dispatch persists a finished failed attempt and marks
the action failed, while returning the existing 409 `source_revision_changed`.
The receipt includes both approved and current revisions and asks for re-approval.
The decision remains answered; repeat dispatch never revives the failed action,
even if the source revision changes back. Propose with a fresh request key and
approve the current revision to deliver again.

Reconciliation preserves read-back receipts even when they cannot advance the
action's monotonic status. `reconciliation_conflict: true` marks that mismatch;
clients should display the receipt detail rather than infer success from status
alone. This does not authorize a blind retry.

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

Every work response exposes read-only `decision_revision`. Clients must copy it
verbatim into proposal `target_revision`. It is the nonempty `source.revision`
when authority is external, otherwise the decimal Nexus work `version`.
Dispatch uses the same helper. `freshness.source_revision` is not a fallback.
A metadata or refresh version change therefore invalidates a fallback fence;
a known external revision remains the fence independently of local version changes.

PM context uses canonical work cursors: send the returned `next_cursor` as
`cursor` with the same query, and keep `limit` within 1..50. A work-specific
context is one item and rejects cursors. Invalid cursors return 400.

Pending actions and failed actions with no sent attempt return 400
`invalid_request`: "Nothing has been delivered yet, so there is nothing to read back."
New action attempts record `sent_at` at entry to the executor handoff boundary,
before invoking it; crash uncertainty is conservatively treated as a possible
send. Preflight failures omit `sent_at`. Legacy failed attempts without a handoff
marker cannot establish delivery; other legacy delivery states still permit
read-back. A sent failure can be reconciled but never changes from `failed` to
`unknown`; inconclusive receipts remain visible with `reconciliation_conflict`.

PM authorization caches only actor-to-principal IDs for at most 30 seconds,
bounded to 256 entries. Every cache hit reloads current principal authority and
wake routing from auth storage. A revoked/mismatched principal invalidates the
cached identity immediately on the next check, including external database
revocations. No authorization result is cached.
