# Unified work and Decide

Work is an existing card, projects use topics, and source observations extend the
same workspace SQLite. `/work` serves board/table/Decide/CLI projections. External
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
  completion evidence gate. Operator revisions, moves, and content updates of
  external cards are rejected.
- `POST /work/{card_ref}/observations`: attributed, replay-safe observation.
  Exact replay returns the original; conflicting reuse returns 409. Client
  `status=verified` remains `verification=reported`, never independent proof.
  When that observation becomes the latest good one (status other than `error`,
  and newer by the existing source-sequence / observed-at order) for external
  work, and `facts.phase` maps to a different board column, the card moves in
  the same transaction. `cancelled` and `unknown` map to `backlog` unless the
  board has that column. The move uses the board's rank allocator and emits
  `card_moved`. `done` carries the observation's referenced evidence through
  the completion gate (a live `artifact:` or `event:` ref, or a workspace event
  recorded from URL evidence). Error observations, older observations, and
  duplicate replays do not move the card. Native cards are unchanged. The move
  does not bump `work_metadata.version`; a nonempty external `source.revision`
  remains `decision_revision`.
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

Example using a public GitHub issue and a credential _handle_, not a secret.
`make serve` sets `ANX_OBSERVATION_CONFIG` to `core/dev/observation.serve.json`
when that file exists: a builtin GitHub reader plus a JIT C transform against
`Git-on-my-level/agent-nexus#208`. `core/scripts/dogfood-jit.sh` compiles
`core/dev/github-transform.c`, then stages, validates, canaries and activates
the artifact. A Linux host is not available in this worktree; `make -C core check`
cross-compiles `./internal/observation` with `GOOS=linux`.

```json
{
  "targets": [
    {
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
    }
  ]
}
```

Work must first be registered with matching source authority, connection ID and
source-native ID. Workspace ID is injected from the running deployment and
cannot cross workspace boundaries. The optional GitHub and Multica connectors use bounded HTTPS readers
with redirects disabled. Multica also requires `source_workspace_id` and its
actual approved HTTPS `base_url`. Alternatively, set `transport=multica_cli`,
`cli_binary` to the approved installed absolute executable, and `cli_profile` to
an existing authorized profile. That trusted CLI owns its source authentication
and network policy; no token extraction or credential copying occurs. Explicit
`allowed_networks` CIDRs for the HTTP transport can approve
private destinations. The environment handle resolves only the named variable
in memory; choose source-side read-only credentials.

Self-hosted Forgejo and Gitea forges use `source: "forgejo"` (or `"gitea"`) with
the same HTTP transport: `base_url` is the forge HTTPS origin (the reader mounts
the Gitea-compatible API under `/api/v1`), `kind` is `issue` or `pull_request`,
`native_id` is the numeric index and `repository` is `owner/repo`. There is no
default origin; the credential env handle authenticates as a read-only
`Authorization: token` personal access token. The reader reports PR state,
reviews, commit statuses and comments; it is read-only. GitLab and Bitbucket
have no observation reader yet.

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

Any process holding secrets in its environment must run under
a different uid (or on a different host) from anx-core's JIT runner. Seatbelt
cannot block numeric `KERN_PROCARGS2` reads of same-uid process environments.
The availability probe checks only that the runner must not run as root; it does
not verify this deployment separation.

## Decide runtime

The Decide service uses the same SQLite and the current workspace principal.
Decision authorization rechecks current principals; a request cannot claim to be
human in a body/header.

Decide decision targets must be live: trashed, archived and purged work all project
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

## Checks and migration

Migration 26 adds card-owned metadata and append-only observations, preserving
existing cards. Existing cards project as native work at metadata version zero
until their first metadata mutation. No parallel tracker database is created.

Focused tests:

```sh
cd core
go test ./internal/primitives -run TestWork
go test ./internal/server -run 'TestWork|TestObservationRuntime|TestDecide'
```

Run `make -C core check` and `make contract-check` for component/contract gates.
Peer `internal/decide` and `internal/observation` packages must be integrated before
building the runtime-wiring commit.

### Decide phase actions and identity boundaries

A `work.phase` decision carries `payload: {"phase": "ready"}` (or backlog,
in_progress, blocked, review, done). The instruction is explanatory prose and
does not supply mutation parameters. Approvals without a valid structured target
are rejected. For done, `payload.resolution_refs` must contain evidence references;
execution still uses the canonical card completion gate.

Nexus-owned phase actions use the board move transaction with a work revision
precondition. All board moves advance the work revision; board and work reads
therefore share the same phase. Execution reads back the canonical phase and
reconciliation records verification. Source-owned phase requests never mutate
the projection. Accepted external observations are separate: they move the
board card to the observed phase without changing the source-revision fence.
Without a source executor, dispatch returns 503 `unavailable`
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

Direct proposals require a human principal; non-human principals are rejected
with `forbidden`. Agents support PM work by orienting, triaging, filing `anx ask`
recommendations, and updating cards, but cannot create or approve PM decisions.

New proposals reuse an awaiting decision for the same workspace, human, work_ref
and scope only when instruction, payload, target revision and origin match.
Changed intent atomically supersedes the earlier awaiting decision, records
`superseded_by` and `superseded_reason`, and creates the replacement.

Request keys remain immutable. Reusing one with changed content or target revision
returns 409 with `error.details.existing_decision_id`; revisions are opaque source
identifiers and cannot be ordered. Clients can link the existing decision or use
a new key to propose replacement intent.

Unpaginated service reads walk all batches internally. Decision and action pages
return newest `created_at` first, with descending internal rowid as the tiebreaker.
New actions record creation time at approval; legacy actions use their decision
creation time. Work lists return newest `updated_at` first, then descending card
id. Opaque cursors retain both ordering values, so newer inserts and changes ahead
of the cursor appear on a fresh first page without shifting the older continuation.
Work rows also project read-only `rank` and `column_key` from board membership.
Board-like clients sort within a phase by `board_ref` then `rank`; list and table
consumers keep this recency order.
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
and CLI proposals must send the structured payload; legacy prose-only pending
decisions must be rejected/superseded and proposed again with that payload.

### Proposal attribution, revision fences, and context pagination

New decisions persist `proposed_by` (the proposing principal actor ID) separately
from `actor_id` (the human who can answer and dispatch). `origin_kind` is
`human` for direct proposals and `channel` for direct proposals with a validated
channel origin. Legacy `pm_turn` records remain readable but no new turn
proposals are created. Superseded records retain their own attribution; reusing
identical intent retains the original proposal's attribution. A different
proposer or origin kind is different intent. Legacy decisions without recorded
provenance omit these fields rather than inventing attribution.

When a proposal replaces an awaiting decision, the create/propose response
includes `supersedes` (the replaced decision ID), `supersedes_proposed_by`, and
`supersedes_origin_kind`. The old decision records `superseded_by_proposed_by`
and `superseded_by_origin_kind` from its replacement; unknown legacy values are
omitted. These fields persist on the replacement for retries and
later reads; unknown legacy attribution is omitted. Clients should tell the
reader whose earlier proposal was replaced, including when a human drag replaces
an earlier proposal.

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

Decide context uses canonical work cursors: send the returned `next_cursor` as
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

Decide authorization caches only actor-to-principal IDs for at most 30 seconds,
bounded to 256 entries. Every cache hit reloads current principal authority and
wake routing from auth storage. A revoked/mismatched principal invalidates the
cached identity immediately on the next check, including external database
revocations. No authorization result is cached.

Answering or dispatching a superseded decision returns 409 `conflict` with
`error.details.status: superseded` and `error.details.superseded_by` (the
replacement ID). This also covers a
supersession racing the answer CAS. An identical replay of a rejection remains
idempotent. Clients can link the replacement directly from the conflict response.

Decide annotate proposals validate `instruction` as a nonempty JSON object using the
canonical primitives annotation validator before insertion. The allowed keys are
`project_ref`, `priority`, `next_actor`, `next_action`, `blockers`, `wake_condition`,
`start_at`, `due_at`, `relations`, and `executions`. Invalid keys and value shapes
return `400 invalid_request`, naming the offending keys and allowed set.

The `human_proposal_pending` code is retained for compatibility with the removed
agent-propose path; current servers reject non-human principals with `forbidden`
before that check. Human proposals still use the same actor boundary for
deduplication and supersession.

### Optional source connector boundary

Source readers run only for targets in an operator-installed observation config.
Leaving `ANX_OBSERVATION_CONFIG` unset disables observation collection. Installing
an upstream CLI alone does not enable a connector. Each configured connection
must supply its source origin, workspace binding, read-only credential reference
or approved CLI profile, and resource bounds; none has a personal service default.

Multica is an optional third-party issue-source connector, with the same evidence
and source-authority rules as GitHub. Its CLI transport uses an explicitly approved
binary and profile and inherits that CLI's network policy; use the HTTPS transport
when the observer must enforce DNS pinning. Neither transport dispatches work or
writes source issues. See `adapters/observation/README.md` for configuration and
transport limits.
