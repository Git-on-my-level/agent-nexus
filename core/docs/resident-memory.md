# Resident memory probe

The opt-in `TestPerformanceResidentMemory` probe measures Go heap, stacks,
GC target, process RSS, SQLite native allocation, effective page-cache/mmap
settings, connection-pool statistics, and latency for Overview, card detail,
and inbox. It must run alone in its test process because it enables SQLite's
process-global allocation counters before SQLite initialization. Normal tests
and serving binaries retain the driver's default allocation-counter setting.

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
this macOS synthetic experiment. Validating a hosted 250 MiB target still needs
an authorized representative local fixture and a comparable isolated Linux
runtime with normal background workers/streams enabled. A Go heap limit alone
cannot cap native SQLite allocation. Keep the existing 1 GiB hosted container
limit until that measurement establishes a safe native-memory and OOM margin.
