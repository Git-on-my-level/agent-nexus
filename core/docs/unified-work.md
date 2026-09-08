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

PM conversation creation and context inspection work without a model. Messages
require a configured existing PM bridge. `ANX_PM_BRIDGE_ENABLED` defaults false.
Enabling it requires `ANX_PM_AGENT_ACTOR_ID`, `ANX_PM_AGENT_HANDLE`, and optionally
`ANX_PM_BASE_URL`. Core checks the selected principal's current wake registration
and online state. The deployment must separately provide an approved enforced
read-only runtime envelope. A config flag, prompt, or bounded-process wrapper
is not an isolation mechanism. Do not enable a general privileged coding actor
as the PM runtime. Missing/unready providers return unavailable, not canned text.

The PM peer package owns channel authentication, exact identity mappings,
durable outboxes, and source-action state machines. This core wiring does not
configure Telegram/Discord webhooks, consume a gateway, or grant external
credentials. Real channel delivery, a configured read-only PM actor, and
integration-specific external source writes/read-back remain deployment and
qualification requirements; fixture tests are not evidence of those capabilities.

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
