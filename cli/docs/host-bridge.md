# One bridge per host

The bridge is the recommended next step after enrollment. It remains optional:
await/batch and direct CLI commands work without it. Without its answer job,
unattended resumes wait; Git capture and spool drain are manual without their
jobs. File transport is deferred to PR 3. Background spool drain keeps foreign
entries untouched and defers retained work after 25 days or a full receipt ledger;
inspect it locally instead of silently pruning or replaying expired requests.

```sh
anx bridge install --dry-run
anx bridge install
anx bridge add --as codex
anx bridge status
anx bridge doctor
```

Register each intended agent/workspace with `bridge add --workspace`. Registration
enables answers; `--answers=false`, `--git`, `--spool`, and `--runtime` select jobs
independently. Git still requires its separate explicit repo writer/binding.
Adding agents creates no units. Repeating add preserves unmentioned jobs, runtime
wiring, policy and existing resume roots. `--wake=false` retains runtime wiring
while disabling wake execution. `anx bridge remove --as <name> --workspace <alias>`
removes a registration even after revocation; it keeps commands, cursors and
receipts for reconciliation. Invalid/revoked registrations reject the entire
signed snapshot until repaired or removed; doctor identifies the local binding. `--max-attempts` (1–20, default 5) and
`--command-timeout` (positive duration, default 30m) set that registration's policy.

| Local resource | Contract                                                                                                                                                                                                                  |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Registry       | Owner-only `~/.config/anx/bridge/registry.json`; resolved workspace ID/base URL, enrollment config root, host/actor IDs, jobs, resume roots and execution policy. References credentials in place; no copied tokens/keys. |
| Unit           | One `io.agent-nexus.bridge` launchd agent or `anx-bridge.service` systemd user unit. No workspace/agent identity on its argv.                                                                                             |
| Foreground     | `anx bridge run` uses the same host singleton as the unit. `start`, `stop`, `status`, `doctor` address that service. No Python mode or per-agent units.                                                                   |
| Work           | One dispatcher per registered agent partitions answer wakes from ordinary runtime wakes. Separate short-work and child-execution budgets (four each); independent workspace heartbeat requests cannot wait on callbacks.  |
| Reload         | Registry changes are admitted every three seconds. A changed job stops before its replacement starts; uncertain interrupted callbacks require reconciliation.                                                             |

## Exact runtime commands

`bridge add --runtime` accepts an owner-only JSON file with `argv`, absolute `cwd`,
optional string `env`, `adapter`, and optional `agentctl`. For example, a minimal
stdin consumer for isolated verification is:

```json
{ "argv": ["/bin/cat"], "cwd": "/tmp", "env": {} }
```

Choose your existing agent's approved argv for real work. Runtime env cannot
override ANX/agentctl identity. When `agentctl` is explicitly configured, the job
preallocates an execution ID, installs a `runs ingest` command subscription with
explicit config/base URL/agent, and launches exact argv with prompt stdin.
Direct runtimes receive the registered workspace and `ANX_AS`. Callback stdout
and errors stay local; no prompt, path, command or error enters the heartbeat.

## Presence and wake truth

Install/run explicitly opts into one host presence lifecycle. Every 60 seconds,
one timer sends a separately signed workspace-only snapshot per enrolled
workspace, under independent two-second deadlines. Fresh proofs cover the exact
request bytes. Instance IDs are workspace-scoped; monotonic sequences order one
instance, even through a backward clock step. Preserve the maximum accepted
signed timestamp: a different non-retired instance must prove a strictly newer
time before replacing it. Delayed old packets cannot retire a live successor.
A restart during a clock correction waits until signed time exceeds that mark.
Sequences advance on every send attempt so an unobserved response cannot wedge
reporting. Core retains retired instance IDs and rejects late updates from them.
Core sets last-seen
and a 300-second expiry, replacing bounded normalized job rows atomically.

Only active, nonexcluded agents with an enabled, ready **wake** job under fresh
parent presence are wake-online. Answers alone never imply wake capability for
that agent or its neighbors. Agents and Access show Live / Stale / No bridge per
machine, including zero-agent machines. Badges age locally without a reload.
Status reads batch at most 100 visible host IDs, with at most 32 admitted agent
groups per host; a truncation flag qualifies partial detail. Neither presence nor
self-reported readiness grants authorization or proves a completed callback.

## Legacy install migration

`install --dry-run` previews labels/PIDs and registration counts without changing
services, units or the bridge registry. Identity preflight can refresh existing
auth caches. Real install follows the same preflight before stopping anything:

1. Inspect owner-controlled unit files, loaded main PIDs, kernel argv, executable,
   UID, parent ancestry and process start times. Custom wrapper labels are valid;
   unowned consumers, environment-file dependencies and unsupported flags block
   cutover. It does not execute wrappers or read environment files.
2. Require `host_bridge_v1` from each configured workspace. Resolve every old
   agent/workspace and policy; include enrolled zero-agent workspaces. Preserve
   all resume directories and cursors **in place**, deduplicating identical roots.
   Conflicting duplicate asks pause for reconciliation.
3. Fsync a private `migration.json` journal with registry and unit snapshots.
   Disable restart sources, stop proven old launchers, and recheck process identity
   before retiring survivors. Re-read final resume state after quiescence.
4. Delivered callbacks retry acknowledgement only. Interrupted attempts with
   unknown effects become retained `uncertain` records and block activation;
   reconcile their real outcome before retrying. Never reset attempts or blindly
   repeat a command. Callbacks should still use `ANX_ASK_ID` idempotently.
5. Publish the merged registry and sole new unit, start it, verify every imported
   job's readiness, and require exactly one supervisor with no legacy consumers.
   Print replaced labels/PIDs and imported counts. Reinstall is idempotent.

Legacy unit files stay in place until the new service is verified healthy.
Any failed quiesce, bootstrap, start or readiness check stops/disables the new
service and restores **all** legacy files and their prior loaded/enabled states.
The journal becomes `rolled_back`; another install retries normally. An interrupted
journal is recovered automatically before a new attempt, including `activating`.

An already executing/uncertain resume blocks cutover before any service stops.
If execution races quiescence or new-service startup, its retained record becomes
`<original>.json.paused` in the same namespace, listed in `migration.json`. Older
consumers only read `*.json`, so restoring them cannot replay that unknown command.
Other namespaces resume normally. `bridge doctor` reports the paused namespace;
new registration cannot overwrite that record. Preserve it until an owner verifies
the external outcome, sets the retained state to `delivered` or `failed`, and
renames it back to its original `.json` path. Reset that namespace's cursor for
acknowledgement replay before retrying install; never reset attempts or command
identity to force another execution.

The current CLI still accepts restored legacy argv, for example:

```sh
anx --as worker --workspace <alias> bridge run --command-timeout 30m --max-attempts 5
```

An explicit `--as` or `ANX_AS` selects this **rollback-only answer consumer**.
It resolves exactly that workspace, preserves the timeout/attempt policy and
resume files/cursor, reconnects on transient errors, and uses the new answer
executor with a per-namespace lock shared by the host service. Unknown outcomes
still pause that namespace. It does not install units, alter the host registry,
publish heartbeats, or enable wake/Git/spool jobs. Retry `anx bridge install` to
consolidate again. New host units clear `ANX_AS`; for a foreground host supervisor
with an inherited agent identity use `ANX_AS= anx bridge run`.

Runtime receipts are private under each retained namespace's `runtime-receipts/`.
An earlier runtime claim without a durable local outcome becomes an inspectable
`uncertain` receipt and pauses that lane; it is never silently adopted or rerun.
Doctor also inventories extra/unowned consumers, singleton ownership, migration
phase, enrollment, namespace safety, runtime uncertainty, and Git writer bindings.

The fixture suite covers three independent workspace units and a custom wrapper,
including bootstrap/readiness/quiesce failures, interrupted outcomes, saved
loaded/enabled states and journal retries on both OS
managers using temporary homes and fake commands. The real CLI/core test runs
one foreground service against three disposable workspaces without installing
an OS unit. Tests never operate an existing service or initialize the task repo.

## Scoped file transport

Off by default. `anx sync enable <directory> --card-id card:<handle> --dry-run`
previews an explicit directory allowlist and writer binding; enable registers the
independent `files` job without installing a unit or enabling answers/wake.
`sync disable` clears generated caches and stops that directory while retaining
all original/frozen work. The host supervisor rotates up to 16 directories per
agent, with a five-second short-job budget and ten card refreshes per directory
tick. Heartbeats contain only files readiness, never paths, queue counts or notes.
`bridge doctor` checks retained claims, binding safety, capacity, FIFO head,
cutoff/reconciliation and mirror freshness. See [File transport](file-transport.md)
for the producer/mount boundary and recovery rules.

Every resume root traversed by the host consumer has its own exclusive consumer
lock. A rollback executor on any preserved root fences the host dispatcher; it
cannot overlap a consumer merely because that root is second in the registry.
