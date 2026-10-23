# Brief refresh and allocation probe

`core/scripts/brief-load.py` creates a disposable local synthetic workspace: 4,096
cards, 14,296 events, 48 cadence/goal loops with 9,600 receipts, and 600 asks.
It retains the ordinary performance corpus's private-board ownership controls.
No production workspace, credentials, external service, or model is involved.

```sh
: "${BRIEF_SCRATCH:?Set BRIEF_SCRATCH to an empty disposable local scratch directory}"
python3 core/scripts/brief-load.py seed "$BRIEF_SCRATCH"
python3 core/scripts/brief-load.py measure "$BRIEF_SCRATCH" --label before
# Run the same harness on the candidate, reusing the unchanged seed.
python3 core/scripts/brief-load.py measure "$BRIEF_SCRATCH" --label after
cmp "$BRIEF_SCRATCH/before-profiles/brief.golden.json" "$BRIEF_SCRATCH/after-profiles/brief.golden.json"
go tool pprof -top -alloc_space "$BRIEF_SCRATCH/after-profiles/allocs.pprof"
```

The script refuses to replace existing output. Keep the seed offline while
copying it. Seeding, migrations and initial projection maintenance are excluded
from measurements. Each run warms the authenticated HTTP handler once, then
measures 40 `GET /brief` requests, including auth and response encoding, with
the same SQL instrumentation. It reports Go allocation deltas and SQL calls.
It then performs 60 receipt writes, projection maintenance and brief reads,
with one second between iterations. This is one reader plus one writer acting
in sequence, not a concurrency or container-pressure acceptance test.

RSS and Go heap are sampled after warm-up, every ten writes, and after explicit
GC. Those samples are not instantaneous peaks. Profiles include initialization
and the whole measured workload. A fixed-time complete brief snapshot from the
unchanged seed is also saved for byte-for-byte cross-revision comparison.

## Reproduced costs and fixes

Every workspace event previously invalidated the brief; the two independent
event/inbox subscriptions could invalidate the same read. In the deterministic
mounted-store regression, 60 notification batches over 60 seconds triggered
60 reads. A shared five-second live-refresh interval with a trailing read reduces
that to 12. Events arriving during slow reads remain dirty until another read.
Pending invalidations survive hidden-tab disconnects; teardown cancels timers.
Initial reads, returning-reader checks and confirmed local mutations retain
their direct refresh paths. Other users' changes may wait up to five seconds
after stream delivery while the brief is mounted, plus request time.

The allocation profile attributed 40.6% of cumulative allocation to cached loop
fact loading, including receipt decoding. The brief only renders twelve recent
runs, but decoded all fifty full receipt maps per loop. Its decoder now leaves
unused run detail encoded; counts, sixty goal points, incumbents and expiry
facts remain complete. The report loader still decodes all receipt detail.
The representative decoder test falls from 1,853 allocations to 523, with an
unchanged checked-in loop golden and complete-brief cross-revision golden.

Pinned reads also constructed a second complete authorization SQL fragment just
to test whether the first rewrite needed a snapshot parameter. Return that bit
with the first rewrite instead. The representative statement falls from 727
allocations to 426. This caches no SQL or reader data and changes neither SQL
authorization predicates nor snapshot/epoch validation.

## Local evidence, 2026-10-23

Baseline serving source: `75089722` (v0.13.24). macOS arm64, Go 1.27.0, identical
offline fixture copies and request counts. These are local synthetic results.

Initial sequential pair, host-default CPU setting:

| Metric                |     Before |      After |
| --------------------- | ---------: | ---------: |
| GET /brief p50        |   79.53 ms |   93.94 ms |
| GET /brief p95        |  103.47 ms |  168.04 ms |
| Allocated bytes/read  | 14,583,374 | 11,731,729 |
| Allocations/read      |    190,663 |    140,972 |
| SQL calls/read        |         20 |         20 |
| RSS after GC          |  67.81 MiB |  68.20 MiB |
| Live Go heap after GC |   3.87 MiB |   4.27 MiB |

Allocation bytes fell 19.6%, allocation objects 26.1%, and live client reads 80%.
The initial pair does **not** demonstrate a latency or retained-memory win.

A second pair used separately precompiled baseline/candidate binaries, run
sequentially with `GOMAXPROCS=2` so our builds could not compete with sampling:

| Metric                |     Before |      After |
| --------------------- | ---------: | ---------: |
| GET /brief p50        |  296.89 ms |  282.90 ms |
| GET /brief p95        |  569.10 ms |  440.19 ms |
| Allocated bytes/read  | 14,542,043 | 11,649,827 |
| Allocations/read      |    190,621 |    140,925 |
| SQL calls/read        |         20 |         20 |
| RSS after GC          |  67.61 MiB |  64.17 MiB |
| Live Go heap after GC |   3.41 MiB |   3.76 MiB |

Both pairs consistently reduce transient allocation. Latency is sensitive to
Go concurrency and other host activity; these samples do not establish a
production latency improvement. Retained heap did not improve. The complete
fixed-time output is byte-identical across both pairs (SHA-256
`9956ab3dd89be5f777b8608d36bcea5c038ed89b554e364c1f76adc04bbb3407`).

SQLite remained `cache_size=-2000` (about 2 MiB per connection) and `mmap_size=0`.
The existing denial cache bounds entries to 32 and encoded payloads to 8 MiB;
decoded structures and active requests take additional memory. This change adds
no cache and changes no SQLite or connection-pool settings. The reported
production container's 75% memory usage was not reproduced or attributed here.
Background workers, SSE fanout, many readers and container limits are outside
this single-reader experiment; its RSS cannot establish a production bound.

## Read complexity and acceptance checks

No HTTP routes or response schemas change. `GET /brief` retains its existing
section/candidate bounds and indexes in `performance.md` and
`internal/server/testdata/performance_routes.json`. SQL text, statement counts, authorization,
indexed admission and pagination are unchanged. Preparation grows with bounded
statement size. Decoding grows with each admitted loop's bounded stored facts,
but constructs at most twelve full run maps instead of fifty. It still parses
the encoded fifty-run array. Dirty/expired/reader-filtered facts retain the
existing scoped fallback, whose receipt-density cost is unchanged.

The ordinary brief/series scale gate and the 10,000-receipt complete-brief gate
retain their existing budgets and query-plan expectations. No exception or
budget is widened. Focused regressions cover allocation limits, full output,
private receipt provenance, dirty/expiry handling, slow reads, reader changes,
hidden tabs, local actions and timer cleanup. UI brief goldens cover Decide and
Watch rendering.
