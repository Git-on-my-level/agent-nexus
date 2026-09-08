# Read-only observation readers

The implementation lives in `core/internal/observation`, with an operator runner
at `core/cmd/anx-observe`. It collects evidence into the existing workspace work
model. Source reads never repair issues, post comments, dispatch implementation
work, or mark an outcome verified.

## Source coverage

| Reader | Implemented reads | Explicit limits |
| --- | --- | --- |
| GitHub HTTPS | Selected issue/PR; bounded comments, reviews and head check runs | No discovery, artifact downloads, deployment checks or acceptance verification |
| Multica HTTPS | Selected issue, task runs, explicit linked PRs | Requires source workspace binding; task-runs endpoint is unpaginated; at most 100 run/link entries |
| Multica installed CLI | Same selected issue/runs/PR reads via an approved existing profile | CLI/profile owns auth and network behavior; does not inherit HTTPS reader DNS pinning; no token extraction |
| SSH Git | Exact approved host/repository HEAD, commit time, tracked-file dirty state | No arbitrary command, untracked-file contents, deployment inference or source writes |
| Remote reports | Workspace/target/reader binding, claim normalization, replay identity | Canonical store owns authorization, durable ordering and deduplication |

HTTPS readers use HTTPS-only registered origins, no redirects or ambient proxies,
DNS address validation and pinned dial addresses, connection/workspace binding,
request deadlines, cumulative byte budgets and bounded pagination. Private
networks require explicit approved prefixes; link-local/metadata destinations are
always denied. Pagination links only indicate whether another page exists: the
reader constructs the next fixed source endpoint and never follows a supplied
URL. Errors omit response bodies and credentials. Read-only source credentials
are recommended; this code does not establish an existing token's provider scopes.

The SSH transport ignores ambient SSH configuration and agents, verifies an
explicit known_hosts file, disables forwarding/password prompts, uses a configured
identity file or no identity, and emits only fixed quoted Git commands. The remote
account and known_hosts must already be provisioned. It never installs software
or modifies host services. Git commit time is provenance, not observed liveness or
deployment time.

## Run a selected target

Build from `core/`:

```sh
go build -o ./anx-observe ./cmd/anx-observe
./anx-observe --config ../adapters/observation/github.example.json read
./anx-observe --config ../adapters/observation/github.example.json --core-envelope read
```

Flags precede the action. The example uses a public fixture target; choose an
approved real target. HTTPS auth can resolve the dedicated
`ANX_OBSERVATION_SOURCE_TOKEN` environment slot via operator configuration. Never
place token values in source records or files committed to Git. Multica CLI
transport uses the installed binary and existing named profile without retrieving
its token. `--core-envelope` emits data for the existing authenticated observation
submission API; this command itself performs no Nexus or upstream writes.

## Core runtime integration

`LoadRegistrations(io.Reader, CredentialResolver)` consumes strict version-1
operator JSON, such as `registry.example.json`. Load it from an operator-installed
file, not a public HTTP request. Registrations bind a card to one exact target,
transport, credential handle and resource policy. Unsupported fields/transports,
duplicate cards and overlarge registries fail closed.

`NewRuntime(callbacks, registrations, actorID, workerID, concurrency)` accepts
method callbacks to the canonical workspace store:

- `GetWork(ctx, cardRef)`
- `RequestWorkRefresh(ctx, actorID, cardRef)`
- `ClaimWorkRefresh(ctx, cardRef, workerID, ttl)`
- `SubmitWorkObservation(ctx, actorID, cardRef, observation)`
- `FinishWorkRefresh(ctx, cardRef, leaseToken, result)`

All return `(map[string]any, error)`. Supply them as `RuntimeCallbacks.GetWork`,
`Request`, `Claim`, `Submit`, `Finish`. The caller must authorize the service actor
and workspace; these callbacks are trusted in-process interfaces. Call `Run` with
a shutdown context, a 1–60 second polling interval and an optional result callback.
The caller owns that goroutine and shutdown. `Tick` is available for an existing
scheduler. Canonical durable leases fence competing workers and recover abandoned
refreshes; a bounded pool limits concurrent reads. Persisted backoff survives
restart and cannot be bypassed by a manual refresh. A failed read appends failure
evidence and retains the last good observation. Source fields are checked against
the registered work identity before any source access.

GitHub native work identity is `owner/repository#number`, with the reader's
`Target.Repository` and numeric `Target.NativeID` separate. `ssh_git` maps to core
source authority `git`. IDs never derive from titles.

The standalone `Scheduler` offers in-process coalescing, bounded concurrency,
backoff/jitter, retry hints, last-good retention and snapshot/restore. It is useful
for embedding, but its snapshots are **not** a substitute for canonical durable
queue claims. The core `Runtime` uses those claims directly.

`Report.IdempotencyKey` identifies one originating observation (including its
observed timestamp), so exact replay deduplicates and a new unchanged poll refreshes
liveness. `Report.SemanticKey` excludes observed/received clocks and identifies
unchanged source content. Neither clock becomes source activity or meaningful
progress. Remote report normalization always downgrades verification claims to
reported. The core still authenticates and orders every submission.

## Generated executable readers

See [JIT isolation and lifecycle](jit.md). Built-in readers execute reviewed
fixed source operations. Arbitrary generated code has a separate, much narrower
runtime and is never treated as a trusted built-in reader.

`NewInvestigationStore` stores bounded specifications in the **existing** workspace
SQL database: objective, target, approved sources/paths, evidence requirements,
acceptance interpretation, existing preset reference and budgets. Save uses
workspace scope and optimistic versioning. The preset resolver must resolve the
existing agent/provider configuration; there is no new model registry or fallback.
Saving a spec does not launch a model. Enforced model investigation execution and
coding-generation dispatch need the deployment's approved bridge and remain
unconfigured in this package.

## Verification

```sh
cd core
go test -race ./internal/observation
go test ./...
go vet ./...
ANX_OBSERVATION_ISOLATION_TEST=1 go test ./internal/observation -run TestLinuxIsolationEnforcement -v
```

The last command requires a dedicated unprivileged Linux account, usable user
namespaces, Bubblewrap, `prlimit` and a static C fixture compiler. It exercises
harmless local fixtures for denied host reads, environment credentials, source
writes, host-loopback networking and nested user namespaces. A skipped test is
**not** runtime isolation qualification. See [qualification evidence](qualification.md)
for what was actually run and remaining integration/platform gaps.
