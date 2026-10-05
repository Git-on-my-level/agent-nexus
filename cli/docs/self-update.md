# CLI release updates

`anx update status` is offline. It shows the effective policy and its source,
installer ownership, the running CLI's version/digest versus the installer
receipt, the last release observation, failure stage/code, and rollback outcome.
`anx update now` checks and installs the latest release synchronously. The existing
`anx update --check` (a pure release read) and `--version vX.Y.Z` remain available.
An explicit update works even when automatic policy is off.

```sh
anx update policy auto
anx update policy notify
anx update policy off
ANX_UPDATE_POLICY=off anx work start card:example
```

The default is `auto`. The first successful coordination write per UTC day starts
a detached worker with a two-minute deadline. The foreground command never waits
for release networking, verification, replacement, or skill synchronization. A
per-install atomic daily claim prevents simultaneous commands and separate
workspace config directories from launching duplicate checks. Reads (`orient`,
help, inbox list, await, doctor, status), local maintenance, and dry runs are exempt.

`notify` runs the same release discovery without installing. A known newer release
produces one warning per UTC day on eligible writes. An initial check can finish
after the triggering command; the warning then appears on the next eligible write.
`off` launches no worker and makes no automatic release request. Policy preferences
and observations live below the selected ANX config directory, independently of
workspace credentials. `ANX_UPDATE_POLICY` takes precedence over the saved policy.

Only an installation with a matching `anx.anx-install.json` receipt from
`scripts/install-anx.sh` is managed. The receipt binds ownership to the binary's
SHA-256. Source/dev builds, binaries changed outside the installer, and known
package manager paths are skipped with an explicit status reason. Older releases
have no receipt: rerun the release installer once to enroll them. Do not manufacture
receipts or automatically adopt a path merely because its name is `anx`.
The release installer supports macOS and Linux; Windows self-replacement remains
unsupported and is reported in status.

The worker uses the GitHub release API, falling back on 403/429 to the public
same-origin latest-release redirect. It downloads the archive for this OS/arch,
checks its checksum, verifies the candidate executable reports the release version,
and atomically renames it over the original. A backup remains until post-replace
verification and receipt commit succeed. On failure it restores the original; if
restoration fails, status records the retained backup path. The new executable
runs `skills sync`, preserving edits, unmanaged copies, and agentctl-owned skills.
A skills failure leaves the verified new CLI installed and reports `skills_sync`
with runnable repairs; it does not downgrade a healthy binary.

CLI/core compatibility remains the existing handshake-based doctor check (minimum
and recommended CLI versions). `orient` does not currently learn a core version;
this change adds no handshake/network request to orientation or update gating.
The updater never refuses an update because of a server version.

## Ergonomics audit against agentctl v0.14.0

Compared the installed `agentctl help update`, `bootstrap`, `doctor`, `recent`,
`schema`, `run`, and `update status` contracts with ANX's runtime help catalog,
output contract, registry, mutation parsers, doctor, and managed skill code.

| Surface | agentctl contract | ANX finding | Change / disposition |
| --- | --- | --- | --- |
| Release policy | auto/notify/off, daily invocation worker, managed installs, observed vs receipt, rollback | Only manual checksum updater | Implemented in this PR; old installs need one installer rerun |
| Help catalog / side effects | Machine-readable topics and `side_effect_class` | Generated API and local helper help already classify effects; JSON root help contained only prose | Add offline topic catalog and topic side-effect class to JSON help; retain text projection |
| JSON next actions | Runnable argv, mutation class, preconditions | Envelope v2 already has typed argv/mutates/class and bounded result-state actions | Add concrete update/status/skills actions; generic action preconditions need a shared output-contract decision |
| Structured error repairs | Stable code, retryability, exit code, typed repair actions | `deriveErrorActions` and `anx_cli_recovery` already cover auth, usage, concurrency, timeout, outdated clients | Add update and skills-failure repairs; broad repair completeness is a separate audit |
| Idempotency / request keys | Caller-preallocated execution identity; retry the same identity | Server-backed request keys exist on batch/card/domain writes, sessions use sequence; not every write has a replay key | Do not invent CLI-only deduplication; universal write idempotency requires core/contracts design |
| Recent / history discovery | Host-local paginated journal, state/liveness filters, no prompt/result reads | ANX has `runs list`, session/work queries, document/card history, domain messages, event timeline; no local invocation journal | Different authority/scope; discuss a composed recent-work read before adding local command history |
| Doctor depth | Bootstrap/config/journal/supervisor and live adapter capability probes | ANX already checks workspace, enrollment/key permissions, identity, core readiness, CLI/core handshake and skill state | Add offline updater health; adapter execution probes belong to agentctl/bridge rather than core CLI |
| Schema discovery | Normative schema files via `schema list` | Embedded generated metadata, body schemas in command help, `debug meta commands`, report schema | Existing API-input discovery; generic envelope-schema export/versioning needs agreement |
| Skills / bootstrap | Detect harnesses, ownership-aware refresh/adopt, instruction pointers | ANX already has harness detection, digest ownership, sync/status/adopt, PM preferences and daily skill refresh | Run new binary's sync after update; global instruction-pointer management is a separate policy decision |

## Decisions to record for the release

- Default automatic release updates apply only to installer-owned binaries; an
  old/manual installation needs explicit installer enrollment once.
- Successful coordination writes trigger daily maintenance; reads and local
  maintenance remain exempt. Explicit updates ignore the automatic off policy.
- Skill conflicts are visible and preserved; a skill sync failure does not roll
  back an independently verified binary update.
- Keep universal idempotency, a composed recent-work surface, action preconditions,
  envelope schema export, and global instruction pointers out of this release PR
  until their authority and compatibility contracts are agreed.
