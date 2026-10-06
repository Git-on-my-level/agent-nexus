# Scope model experiment — do not merge

This branch is supporting evidence for design PR #300, not production code.
It starts at main `e637014e8c6eadc660c7effe608a7e3ff6af6814` (schema 63).
All data is deterministically synthesized; no hosted workspace is opened.

From the repository root:

```sh
GOMAXPROCS=1 go -C core test ./internal/server \
  -run '^TestScopeDesignExperiment$' -count=1 -timeout=25m -v
GOMAXPROCS=1 go -C core test ./internal/server \
  -run '^TestScopeDesignDowngradeFence$' -count=1 -v
```

The 10x seed is intentionally expensive: the real current ownership triggers
remain enabled. Budget several GB of temporary disk and substantial setup time.
`GOMAXPROCS=1` is not a container CPU quota. The recorded run used Go 1.27.0,
modernc SQLite 1.38.2, macOS ARM64 and a shared machine with concurrent tests.
WAL and synchronous=FULL were applied to both databases. Each timing has two
warmups and 25 samples, using nearest-rank p95.

The current-model measurements invoke authenticated HTTP inbox, Overview and
work routes. The proposal is a small SQL kernel with synthetic canonical payloads,
scope-prefixed feed keys and counters. It excludes complete repository/grant
authorization, reference hydration, actual health, search, SSE, HTTP and wire
equivalence. It does not establish an end-to-end speedup. Write timings are small
single-reference event appends, not worst-case writes or full HTTP mutations.

The 1x corpus has 26 documents, 433 cards, 2,759 events, 806 artifacts, 499 threads
and 12 inbox items, plus setup controls. 10x multiplies each family. Text has 60
reference occurrences. At 1x these repeat among 26 documents; at 10x up to 60 are
distinct, so unique edge count grows faster than 10x. This is not a fixed-fanout
asymptotic scaling test. Private controls also increase with the scale.

The databases have intentionally different schema coverage. `page_count*page_size`
is allocated SQLite size, not a complete disk/WAL peak. Authorization accounting
joins `dbstat` to `sqlite_master` by table ownership to include every associated
index; feed/counter/page-index bytes are reported separately for the prototype.
Neither storage figure predicts the completed proposed schema's overhead.

An optional storage-only repeat avoids collecting new route samples:

```sh
ANX_SCOPE_STORAGE_ONLY=1 GOMAXPROCS=1 go -C core test ./internal/server \
  -run '^TestScopeDesignExperiment$/^1$' -count=1 -timeout=5m -v
```

The fence test first proves an unknown migration number does not stop the
unchanged schema-63 initializer. It then atomically replaces its migration ledger
name with a view requiring an unavailable function and verifies that initializer
refuses two repeated opens while all canonical ledger rows remain in the renamed
table. This is a proof of that fence mechanism, not a full migration/crash test or
proof against every historical release binary.

Read the design PR's evidence note for measured results and remaining gates.
