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

Example using a public GitHub issue and a credential *handle*, not a secret:

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
`ANX_PM_AGENT_ACTOR_ID` (optional) restricts claim/complete/fail to that actor;
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
