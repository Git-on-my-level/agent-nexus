# Resident memory probe

The opt-in `TestPerformanceResidentMemory` probe measures Go heap, stacks,
GC target, process RSS, SQLite native allocation, effective page-cache/mmap
settings, connection-pool statistics, and latency for Overview, card detail,
and inbox. Run it alone in its test process. The basic probe enables SQLite's
process-global allocation counters before SQLite initialization; hosted-mix mode
retains the serving driver's default counter setting instead.

Use only disposable **local copies** of authorized or synthetic workspaces.
The probe migrates its copy, and Overview visits write to it. Never point it at
a serving database, fetch production data as a fixture, or publish database
contents. SQLite's backup API includes committed WAL content; copying a live
database file alone does not.

## Reproduce

Copy the chosen local database with SQLite's backup API into a disposable
workspace root, along with any required local artifact content. On a synthetic
workspace without the performance corpus, run once with `ANX_MEMORY_SEED=1`:

```sh
cd core
ANX_PERFORMANCE_TEST=1 ANX_MEMORY_WORKSPACE="$PWD/memory-fixture" \
  ANX_MEMORY_SEED=1 go test ./internal/server \
  -run '^TestPerformanceResidentMemory$' -count=1 -v -timeout=15m
```

This installs the standard `perfguard` corpus and synthetic owner/stranger
credentials. Seeding is not idempotent; preserve the resulting fixture, then
make independent copies for before/after runs. An empty root can also be seeded
to obtain a smaller standalone synthetic corpus. The optional
`ANX_MEMORY_PRIVATE_HISTORY=1` preparation mode marks synthetic threads titled
`hidden subject` private to a third synthetic principal; it is intended for the
archived-history QA fixture described below, not arbitrary real workspaces.

Run both revisions with the same fixture, Go version, request count and
concurrency. Include the probe file in the baseline revision when comparing to
a revision that predates it. Do not run the two measurements concurrently.

```sh
ANX_PERFORMANCE_TEST=1 ANX_MEMORY_WORKSPACE="$PWD/memory-copy" \
  ANX_MEMORY_PROFILE_DIR="$PWD/memory-profile" \
  go test ./internal/server -run '^TestPerformanceResidentMemory$' \
  -count=1 -v -timeout=15m
go tool pprof -top memory-profile/heap.pprof
go tool pprof -top -alloc_space memory-profile/allocs.pprof
```

Default load is six serial warm-up requests, then three waves of four clients;
each client reads all three routes. Half the clients are the owner and half are
an unrelated agent. `ANX_MEMORY_WORKERS` can select 1–32 clients. Explicit
private-card positive/negative controls run outside the timed samples. Retain
untruncated stdout/stderr and the command exit code. RSS snapshots are samples
after warm-up/waves/GC, **not peak RSS**; SQLite also reports native high-water
allocation. Native allocation is outside Go's heap and memory-limit accounting.

## Local measurement, 2026-10-13

Baseline: `84e1a5cc` (v0.12.60), macOS arm64, Go 1.27.0. The local synthetic QA
database originated from a v0.12.20 fixture with 100,000 archived inbox rows.
After migration and adding `perfguard`'s 4,096-card/document/artifact corpus and
4,096 PM records, the database occupied approximately 640 MiB. The dense case
made the archived inbox ancestry private to a third synthetic principal before
taking identical before/after copies. No production workspace was accessed.

Four-client results:

| Metric                              |          Before |          After |
| ----------------------------------- | --------------: | -------------: |
| RSS after explicit GC               |       169.0 MiB |      111.8 MiB |
| Go live heap after GC               |        39.8 MiB |       24.8 MiB |
| Go allocation through the workload  |     1,308.7 MiB |      595.6 MiB |
| SQLite native high-water allocation |        97.6 MiB |       96.6 MiB |
| Overview median / p95               | 12.08 / 16.02 s | 9.10 / 11.83 s |
| Card detail median / p95            |   5.72 / 6.81 s |  3.59 / 5.71 s |
| Inbox median / p95                  |   2.01 / 3.33 s |  1.42 / 2.53 s |

These deliberately dense denial-closure timings are not ordinary workspace
latency targets. The existing scale gate separately checks indexed hot reads.
The same large database without private archived ancestry stayed near 70 MiB
RSS before the change: database bytes alone do not determine resident memory.

The baseline heap profile attributed about 27.5 MiB to the two decoded denial
indexes. Allocation profiling also identified repeated closure JSON copies
through SQLite and rebuilding fixed recursive SQL text. The change shares one
decoded immutable index between ordinary and point reads, drops obsolete cached
epochs, omits encoded closure round trips when SQL proves the cached epoch
current, and compiles four fixed SQL templates once. Principal roots, transaction
tokens, epoch validation and canonical fallback remain request/transaction local.

SQLite settings were already `cache_size=-2000` (about 2 MiB **per connection**),
`mmap_size=0`, `page_size=4096`, and `temp_store=0`. Its post-wave native allocation
was about 4.3 MiB. These settings were not the dominant retained cost, so this
change does not reduce their budgets or change hosted container limits.

A separate baseline-only 16-client overload experiment reached 392.8 MiB native
SQLite high-water allocation and 278.7 MiB post-GC RSS. Overview returned HTTP
500 under write-lock contention and the probe exited 1. It is a failed stress
experiment, not a passing latency baseline or evidence of 600 MiB steady RSS.

The reported hosted 594–657 MiB RSS has **not** been reproduced or attributed by
this macOS synthetic experiment. That initial experiment did not include hosted
background workers/streams.
The follow-up Linux experiment is described below. A Go heap limit alone
cannot cap native SQLite allocation. Keep the existing 1 GiB hosted container
limit until that measurement establishes a safe native-memory and OOM margin.

## Cgroup policy and isolated operator diagnostics

On Linux startup, core reads a finite cgroup v2 `memory.max` or v1
`memory.limit_in_bytes` and sets the Go soft memory limit to 70% of it. Standard
systemd subgroup/ancestor limits are considered too. An explicitly present
`GOMEMLIMIT` always wins, including `off`. Unlimited (`max` or the v1 sentinel),
missing and invalid limits leave Go's default unchanged. Non-Linux startup does
not set a limit. The chosen byte values are logged once. A 1 GiB ceiling produces
`Go_soft_bytes=751619276` (716.8 MiB). `GOGC` is unchanged. This is a Go soft limit,
not a process RSS cap: SQLite's native allocations are outside its accounting.

`ANX_MEMORY_DIAGNOSTICS=true` enables a **separate** IPv4 loopback listener,
default `127.0.0.1:6061`. `ANX_MEMORY_DIAGNOSTICS_PORT` selects its port (0 chooses
an ephemeral port logged at startup). Diagnostics are off by default, never
mounted on the workspace API or the shared default mux, and expose only:

- `GET /memory`: twelve numeric runtime counters, including heap in-use,
  released pages, live heap, GC target, soft limit and goroutine count.
- `GET /heap`: a binary pprof heap allocation profile, containing sizes and code
  stacks, not heap object bytes or workspace content.

The peer and literal Host must be loopback. Forwarding headers, browser origins,
cross-site navigation, workspace authorization/cookies, non-GET methods and
query options (including `debug` and `gc`) are rejected. No goroutine, CPU,
command-line, environment, arbitrary runtime profile, or text dump is served.
Profiling admits at most one simultaneous request. Responses are not cached.
Do not publish this port or add a public reverse-proxy route; use an operator's
local shell or authenticated SSH transport into the core's network namespace.
The operator can run `curl -fsS http://127.0.0.1:6061/memory` and save `/heap` as a
binary file for `go tool pprof -top`. No workspace credentials are needed or
accepted. Capturing a heap profile does not force GC.

## Hosted-mix probe

Set `ANX_MEMORY_HOSTED_MIX=1` to add five real HTTP SSE subscriptions (events,
inbox and notification receipts), signed heartbeat publishing to an in-process
collector, agent wake claim/completion cycles, PM message/lease/heartbeat/reply
cycles, projection maintenance, and the normal ask/inbox maintenance loops.
No model, external service or source credentials are used. All goroutines and
HTTP connections are canceled and joined before the probe returns.

Prepare a **fresh current-schema synthetic** fixture with `ANX_MEMORY_SEED=1`
and `ANX_MEMORY_HOSTED_SEED=1`. The latter removes the incomplete, expired PM turn
placeholders used by the read-plan fixture; live turns are created through the
PM service. Legacy upgrade queues and read-only fake turn rows must not be
confused with ordinary hosted activity. Failed legacy/overload experiments are
retained as separate evidence. Clone the prepared workspace for both revisions.
Fixture setup occurs before maintenance and is excluded from timed reads.

Run the same compiled probe under Go 1.23 in separate Linux containers with
`--memory=1g --memory-swap=1g`. For the candidate set
`ANX_MEMORY_APPLY_LIMIT=1`, which applies the same startup policy as `anx-core`.
For the baseline leave it unset and preserve the original core implementation;
only copy the diagnostic test helper into that revision. Keep concurrency and
CPU quota identical, and do not build or run other experiments alongside the
comparison. A 100 ms sampler records peak RSS with simultaneous Go heap in-use
and released counters, and captures a heap profile as observed heap peaks grow.
It also records retention after activity **before** explicit GC and again after
GC. The sampling interval means an instantaneous peak can be missed.

## Linux hosted-mix measurement, 2026-10-13

Both revisions ran sequentially on Linux arm64 with Go 1.23.12, one CPU quota,
a 1 GiB cgroup limit and no external network. Baseline serving code was
`84e1a5cc`; candidate includes the allocation fixes and the startup policy above.
The identical fresh synthetic database was 291 MiB with 4,096 corpus cards.
Each run made 42 timed reads and retained five SSE connections throughout,
with real maintenance, signed heartbeats, wake completions and PM lease turns.
These tests use the real stores and services, not model execution or hosted data.

| Metric (MiB)                   | Maintenance before | Maintenance after | Steady before | Steady after |
| ------------------------------ | -----------------: | ----------------: | ------------: | -----------: |
| Sampled peak RSS               |             146.84 |            138.04 |        138.06 |       138.27 |
| Heap in-use at peak RSS        |              14.20 |             13.35 |         11.92 |         9.88 |
| Heap released at peak RSS      |               8.73 |              9.80 |         13.62 |        14.13 |
| Sampled peak heap in-use       |              22.16 |             18.73 |         20.25 |        20.53 |
| Retained RSS, before forced GC |             100.04 |             91.21 |         94.94 |        92.04 |
| Retained heap in-use           |               8.52 |              6.68 |         11.06 |         9.54 |
| Retained heap released         |              13.64 |             17.77 |         11.91 |        16.05 |
| RSS after forced GC            |              96.60 |             90.39 |         93.38 |        91.92 |

The maintenance runs lasted 249.56 / 240.53 seconds, completed 49 / 47 wake
and PM cycles and 9 / 8 signed heartbeats. Maintenance deadline retries were
376 / 440, matching the serving retry behavior. For the separate steady pair,
prepare an identical copy with `ANX_MEMORY_WARM_MAINTENANCE=1` using the probe's
actual backfill APIs before cloning it. Those runs lasted 177.22 / 199.21 seconds,
completed 35 / 39 wake and PM cycles and 6 / 7 heartbeats, with zero maintenance
retries. All four probes exited 0, with `OOMKilled=false` and the original 1 GiB
container ceiling. Background operation counts differ because they run on timers.

The baseline's largest cumulative Go allocator was `ownershipClosure`:
496.89 MiB flat / 698.34 MiB cumulative in the maintenance run, versus
91.69 / 92.20 MiB afterward. `accessSnapshotCTEs` cumulative allocation fell
1,000.69 to 488.07 MiB. The sampled baseline peak heap profile attributed
58.67% to the Overview stack and included both duplicate denial indexes.
The steady profiles also show Overview/inbox response materialization; the
candidate no longer has both decoded indexes. Heap profiles sample allocations
and report GC-accounted objects; they are not exact instantaneous MemStats peaks.

Latency was **mixed** under the one-CPU write workload. Maintenance medians for
Overview/card/inbox improved from 9.49/4.78/9.50 to 7.66/4.09/7.99 seconds, but
Overview p95 worsened 43.18 to 52.65 seconds. In the steady pair, medians were
5.90/4.42/6.21 before and 7.55/3.60/8.03 after; p95 was 38.28/19.11/17.59 before
and 39.44/19.69/19.89 after. Fourteen samples per route and timer-driven writes
do not establish a consistent latency improvement. No assertion or budget was
relaxed. Failed legacy-fixture and aborted stress probes are separate evidence.

This Linux mix **did not reproduce the hosted ~600 MiB anonymous retention**.
The 716.8 MiB Go limit was installed and verified on the actual core executable,
but was far above this fixture's live heap; its effect at hosted pressure is not
measured here. RSS includes native SQLite allocation and other process costs;
kernel anonymous counters alone cannot distinguish those from Go. The isolated
diagnostic now permits an operator to collect numeric counters and allocation
stacks on the affected core without reading workspace content. The hosted
250 MB target remains unverified until that measurement is available.
