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

The default is `auto`. A managed install that receives `cli_outdated` on HTTP 426
runs this same verified update immediately, then retries the original command
once. A command that already read stdin, or that read a non-regular input such as
`/dev/stdin` or a FIFO, or that already wrote output, prints
`anx update --version <recommended>` instead of retrying. A `cli_outdated` body
on any other HTTP status does not update or retry. Policy `notify` or `off`,
and unmanaged installs, print the same command instead of replacing the binary.
Separately,
the first successful read or coordination write per UTC day starts
a detached worker with a two-minute deadline. The foreground command never waits
for release networking, verification, replacement, or skill synchronization. A
per-install atomic daily claim prevents simultaneous commands and separate
workspace config directories from launching duplicate checks. Read-only commands
such as `orient`, `inbox list`, `doctor`, and `work list` now trigger the daily
check. `anx update status`, help, `await`, local maintenance, and dry runs stay
offline. The gate uses the parsed command result, including every accepted true
spelling of `--dry-run`.

`notify` runs the same release discovery without installing. A known newer release
produces one warning per UTC day on eligible reads and writes. An initial check
can finish after the triggering command; the warning then appears on the next
eligible invocation.
`off` launches no worker and makes no automatic release request. Policy preferences
and observations live below the selected ANX config directory, independently of
workspace credentials. `ANX_UPDATE_POLICY` takes precedence over the saved policy.

Only an installation with a matching `anx.anx-install.json` receipt from
`scripts/install-anx.sh` is managed. The receipt binds ownership to the binary's
SHA-256. Source/dev builds, binaries changed outside the installer, and known
package manager paths are skipped with an explicit status reason. Older releases
have no receipt: rerun the release installer once to enroll them. Do not manufacture
receipts or automatically adopt a path merely because its name is `anx`.
If a source build overwrites a managed binary, explicitly rerunning the verified
installer re-enrolls it when no unresolved transaction is pending. Before
replacement, it retains the changed binary (with its permissions) and the exact
stale receipt in a private `.anx-reenroll-*` directory beside the executable,
and prints that directory for manual inspection. The new transaction treats
the changed binary as unmanaged: rollback restores those bytes without an active
ownership receipt, while the saved evidence remains. Pending transaction recovery
still rejects mismatched receipts or foreign bytes before any re-enrollment.
The release installer requires Python 3.8 or newer and supports macOS and Linux;
Windows self-replacement remains
unsupported and is reported in status.

The worker uses the canonical repository's GitHub release API without redirects,
falling back on 403/429 to its public same-origin latest-release redirect.
Every download requires HTTPS. Asset redirects may retain the exact repository
asset path or reach `release-assets.githubusercontent.com`,
`objects.githubusercontent.com`, or `github-releases.githubusercontent.com` on
the HTTPS port. Other origins, repository paths, credentials, and HTTP downgrades
are rejected. The installer applies the same restrictions.

It downloads the archive for this OS/arch,
checks its checksum, verifies the candidate executable reports the release version,
and atomically renames it over the original. A backup remains until post-replace
verification and receipt commit succeed. On failure it restores the original; if
restoration fails, status records the retained backup path. The new executable
runs `skills sync`, preserving edits, unmanaged copies, and agentctl-owned skills.
A drifted (edited), conflicted, or outdated skill requires attention and is not
reported as synchronized. A skills failure leaves the verified new CLI installed
and reports `skills_sync`
with runnable repairs; it does not downgrade a healthy binary.

Both installer and updater acquire a kernel process lock on the same permanent
`anx.anx-update.lock` inode. They never reclaim by age or unlink that inode;
process death releases ownership. Installer skill synchronization runs after
releasing this lock and has a 60-second deadline. Candidates must execute and
report the expected version before replacement and again afterward. An execution
or verification failure preserves/restores the original binary and receipt and
returns failure.

A durable `anx.anx-transaction.json` journal records the old receipt, binary
digest, backup path, and new receipt before replacement. Candidate and backup
contents, the journal, receipts, and destination-directory renames are synchronized
to disk. Under the process lock, a later update or installer invocation recovers
an interrupted prepared transaction by restoring the old binary and receipt;
an interrupted committed transaction finishes receipt and backup cleanup.
Recovery is idempotent, including interruption during rollback. Offline status
shows a pending transaction and backup even before the normal update state is
written. Recovery validates every journal field and receipt (including digest,
version, timestamp, and original-receipt consistency) before changing files.
Existing target bytes must match the recorded original or candidate; a
first-install rollback may remove only its recorded candidate. Backup files must
also match their recorded digest, and symlink/nonregular targets or backups are
rejected. Missing backups are allowed only after an original was already restored
or a committed update reached cleanup. Malformed journals or foreign bytes stop
recovery and preserve the binary, receipt, journal, and backup for manual repair;
offline status retains the failed rollback outcome. Replacement rechecks target
identity after staging. Filesystem/hardware support for synchronization remains
the boundary of power-loss durability, and the process lock coordinates the
installer/updater rather than unrelated tools writing the same files concurrently.

Archive handling caps compressed downloads and total expanded data at 128 MiB,
including ignored entries, and permits at most 1,024 entries. Go extraction
checks cancellation throughout decompression. Neither implementation extracts
archive paths to disk: only the root binary is selected, and traversal, links,
special files, and duplicate binaries are rejected.

## Release provenance and proposed verification

The release workflow generates GitHub build-provenance attestations for every
tar/zip release archive and `checksums.txt` before publication. Its job is limited
to version tags in `Git-on-my-level/agent-nexus`, with OIDC and attestation write
permissions scoped to that job. The attestation action is pinned to a commit;
the signed provenance identifies this repository and
`.github/workflows/release-cli.yml`. A rerun refuses existing assets with different
bytes, so it cannot attest one build while leaving a different build published.
See [GitHub's attestation documentation](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations)
and the [attestation action](https://github.com/actions/attest).

Updater attestation verification is deliberately deferred for a separate decision.
The proposed gate verifies both downloaded archive and checksum-manifest digests,
requires GitHub build-provenance statements from the exact repository and signer
workflow, and binds their source tag/commit to the selected release. An initial
implementation could use `gh attestation verify <file> --repo
Git-on-my-level/agent-nexus --signer-workflow
Git-on-my-level/agent-nexus/.github/workflows/release-cli.yml`; an embedded Go
verifier would avoid making `gh` another updater dependency. Signature trust,
certificate workflow identity, and predicate source ref must all be checked,
not just the existence of an attestation. Missing, mismatched, or unverifiable
provenance should fail closed before candidate execution. We need to choose the
verifier and an explicit policy for older unattested releases before enforcing
this gate. Current updates verify checksums and executable behavior; they do not
yet enforce attestations.

CLI/core compatibility remains the existing handshake-based doctor check (minimum
and recommended CLI versions). `orient` does not currently learn a core version;
this change adds no handshake/network request to orientation or update gating.
The updater never refuses an update because of a server version.

## Ergonomics audit against agentctl v0.14.0

Compared the installed `agentctl help update`, `bootstrap`, `doctor`, `recent`,
`schema`, `run`, and `update status` contracts with ANX's runtime help catalog,
output contract, registry, mutation parsers, doctor, and managed skill code.

| Surface                     | agentctl contract                                                                         | ANX finding                                                                                                                    | Change / disposition                                                                                           |
| --------------------------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- |
| Release policy              | auto/notify/off, daily invocation worker, managed installs, observed vs receipt, rollback | Only manual checksum updater                                                                                                   | Implemented in this PR; old installs need one installer rerun                                                  |
| Help catalog / side effects | Machine-readable topics and `side_effect_class`                                           | Generated API and local helper help already classify effects; JSON root help contained only prose                              | Add offline topic catalog and topic side-effect class to JSON help; retain text projection                     |
| JSON next actions           | Runnable argv, mutation class, preconditions                                              | Envelope v2 already has typed argv/mutates/class and bounded result-state actions                                              | Add concrete update/status/skills actions; generic action preconditions need a shared output-contract decision |
| Structured error repairs    | Stable code, retryability, exit code, typed repair actions                                | `deriveErrorActions` and `anx_cli_recovery` already cover auth, usage, concurrency, timeout, outdated clients                  | Add update and skills-failure repairs; broad repair completeness is a separate audit                           |
| Idempotency / request keys  | Caller-preallocated execution identity; retry the same identity                           | Server-backed request keys exist on batch/card/domain writes, sessions use sequence; not every write has a replay key          | Do not invent CLI-only deduplication; universal write idempotency requires core/contracts design               |
| Recent / history discovery  | Host-local paginated journal, state/liveness filters, no prompt/result reads              | ANX has `runs list`, session/work queries, document/card history, domain messages, event timeline; no local invocation journal | Different authority/scope; discuss a composed recent-work read before adding local command history             |
| Doctor depth                | Bootstrap/config/journal/supervisor and live adapter capability probes                    | ANX already checks workspace, enrollment/key permissions, identity, core readiness, CLI/core handshake and skill state         | Add offline updater health; adapter execution probes belong to agentctl/bridge rather than core CLI            |
| Schema discovery            | Normative schema files via `schema list`                                                  | Embedded generated metadata, body schemas in command help, `debug meta commands`, report schema                                | Existing API-input discovery; generic envelope-schema export/versioning needs agreement                        |
| Skills / bootstrap          | Detect harnesses, ownership-aware refresh/adopt, instruction pointers                     | ANX already has harness detection, digest ownership, sync/status/adopt, PM preferences and daily skill refresh                 | Run new binary's sync after update; global instruction-pointer management is a separate policy decision        |

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
