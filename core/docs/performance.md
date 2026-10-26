# Performance guardrails

## Explicit initiative visibility

Brief's explicit-board selection uses one board primary-key existence probe
inside each existing initiative-selection statement. It does not add a query,
refill candidates, or change the 200-position admission and 50-result bounds.
Work grows with the admitted candidates, independent of unrelated workspace
size. Promise/evidence ordering indexes and existing card, tracking and lease
primary-key lookups remain unchanged. The grouping, denied-prefix and tracking
scale gates below cover this read; no budget or plan allowance is increased.

## Default tracking and daily digest reads

Delivery claim/list serialization admits at most 50 deliveries and at most 50
contributors per supported aggregate. Each admitted digest adds one recipient-scoped batch
lookup, with primary-key card/tracking seeks and the partial unique handle index
for older queued snapshots. The handle predicate includes the index's exact
nonempty condition. Claim hydration uses the same statement-validated recipient
policy within the claim transaction; it does not rebuild workspace ownership.
The credential-only active-human routing check retains its existing service
boundary, independently of private business profile metadata. List cursors close
before hydration; denied contributors never trigger a refill.
Tracking reminder/digest queues retain canonical refs only, and ask pushes/digests
omit titles; serialization resolves fresh titles. All delivery kinds then pass
source and serialized payload through the same recipient-bound value admission.
That additional check uses the existing exact/prose admission spelling indexes
and statement-local ancestor probes, on the claim transaction's connection or
the list read handle. Work grows with admitted payload atoms and their canonical
ancestry, rather than unrelated workspace rows. No denied-row refill occurs.
Opaque provider errors are suppressed for every kind. Receipt acknowledgements
strip all business content without an authorization read or another query.
`TestPerformanceDigestSerializationBudget` enforces the new checked-in
`delivery/testdata/digest_read_budget.json` (10 queries, 60 returned rows,
50,000 VM steps, zero full scans) with 50 contributors and 4,096 unrelated
private cards. List uses 5 queries / 53 rows / 34,027 VM steps; claim uses
8 queries / 55 rows / 34,127 VM steps. No existing budget is widened.
The production event-read callback also uses `WithRequestAccessScope`, retaining
current recipient authorization through indexed candidate admission. The added
`TestPerformancePushSerializationRecipientCallback` shares that same budget and
includes the actual callback with 4,096 unrelated private cards. Before repair,
list/claim executed 1,857,690 / 1,857,790 VM steps and 4,097 full scans. After
repair they use 7 queries / 6 rows / 1,333 VM and 10 queries / 8 rows / 1,433 VM,
respectively, with zero full scans. The extra indexed proof query replaces a
workspace ownership rebuild; no budget or plan exception changes.

Lease acquisition/renewal preserves a reminded default's owner and next digest
time with two scalar CASE expressions in the existing primary-key update. It
adds no reads or round trips. Overview's digest ordering reuses admitted rows;
CLI and Watch render the existing at-most-50-row digest preview in one pass,
without another request. Receipt lifecycle filtering keeps the migration's
existing 50-event sample bound and adds no request or database query.

The brief contract reconciliation preserves fractional promise deadlines in
materialization and wire serialization. It changes no SQL shape, index, batch
size or admission bound. The cadence/digest scale gates and pinned preparation
allocation gate pass without new allowances. The client digest normalization is
one pass over the existing at-most-50-row preview; it adds no API request.

Migration 121 adds card-inherited `work_tracking` bookkeeping and a tiny
maintenance cursor. It installs schema only, without a canonical-row backfill.
Maintenance admits 100 historical identities and at most 50 due defaults or
pending unassignments per tick. A reminder folds one admitted card's bounded
notes/plan facts in its transaction; it never scans a workspace history.
Release/expiry uses the existing indexed parent lookup and point principal/card
authorization checks. Its cost includes the selected child's incoming parent
links, as in grouping, not unrelated workspace cards.

Plan health adds primary-key joins to one tracking and lease row per admitted
card; no new SQL round trip is added to ordinary summary batches. Cadence
enrichment retains the existing 50-card batches and receipt windows. The
50-cadence-card fixture now measures 32,358 VM uncached and 22,318 cached,
within the unchanged 50,000 ceiling.

Brief's digest preview seeks
`idx_work_tracking_owner_digest(fallback_owner,next_digest_at,card_id)` through
an internal keys-only routing view. It admits 200 physical identities before
scoped canonical hydration, returns at most 50, and never refills through a
denied prefix. It exposes neither hidden occupancy nor a hidden-row count.
`TestPerformanceWorkTrackingDigestBudget` enforces the checked-in
`work_tracking_read_budget.json`: one query and 3,172 VM for 50 visible cards;
one query and 21,577 VM behind 4,096 denied cards, with zero full-scan steps.
No budget, baseline or plan exception is widened. The broader pre-existing
summary gate described below still blocks complete read-scale acceptance.
The populated startup/upgrade gate passed in 32.6s against 90s; warm open
passed in 22ms against 5s before main's idle-maintenance migration landed.
The tracking migration now follows that migration at version 121.

After that integration, the opt-in populated startup gate timed out in
historical migration 116 at the unchanged 90-second upgrade budget (91.26s
observed on the follow-up). A Go-source baseline restoring all 47 differences
to current main `746e093ca57b3185226dc8dc0e5cf19d73027c7e` also timed out in
migration 116 at 90.00s. Tracking migration 121 had not run in either sample.
This remains a startup acceptance limitation; no budget or allowlist is widened.

Reminder and digest delivery identities are stable per card/default or owner/UTC
day. Ownership and visibility rechecks share the scoped enqueue transaction.
Default/needs-owner/reminder/digest partial indexes bound clock admission;
terminal and assignment triggers remove stale routing positions. A daily
notification holds a 50-card preview and directs readers to paged work.

The offline cadence migrator pages work by 50 and reads at most 50 indexed,
thread/type-filtered events per card. Total operator work grows with visible
cards; each HTTP request keeps its existing bounds. Apply uses an exact card
read, receipt fingerprint and version-conditional patch. It introduces no HTTP
route, model call or unscoped database read.

```sh
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 ./scripts/slot run tests -- \
  go test -C core ./internal/primitives -run '^TestPerformance(WorkTrackingDigestBudget|CadenceTrackingBudget)$' -count=1 -v -timeout=5m
```

## Plain-language asks

The initial authored presentation fields add no SQL, query, poll tick or migration. Inbox list/get/SSE,
completed history and `brief.v1` copy seven optional fields from the canonical
ask payload already admitted by their existing scoped event/subject indexes.
Work is O(admitted asks + their text bytes), independent of unrelated workspace
size. There is no per-row lookup, ownership rebuild or read-time text scan.
New text bounds are 4,000 Unicode characters each for situation, impact,
if_no_answer and recommendation.why, 240 for an action, five alternatives
(consequence: 4,000 each), and 32,000 for details. Existing event/request-size
and page limits also apply; evidence uses the existing event payload budget.
No new read or plan allowance is introduced. Authorship references still inherit
privacy through the canonical event payload's existing ownership indexing.

The unchanged `TestPerformanceAskDeliveryReadBudgets`,
`TestPerformanceInboxItemResolutionRead` and
`TestPerformanceInboxStreamTickHiddenVolumeBudget` cover the point reads and
poll ticks at scale; no ceiling or exception is widened.

## Cadence tracking on card summaries

Cadence tracking reuses the scoped loop-facts reader for only the cadence cards
already admitted to a plan/summary read. Batches contain at most 50 cards; an
ordinary batch adds no SQL. Brief reuses the loop state it already reads. There
is no new route, workspace inventory, authorization bypass or plan allowance.
The existing subject/time receipt indexes and epoch-checked loop projection
bound history hydration to 12 receipts and 60 goal points per card; existing
14-day outcome aggregates and series-goal buckets retain their documented cost.
Legacy unpaged inventories keep their existing size and enrich in 50-card batches.

`TestPerformanceCadenceTrackingBudget` measures one and 50 cadence cards with
4,096 unrelated cards, for owner and stranger, before and after projection
maintenance. It enforces the existing summary ceilings (100 queries, 1,024
rows, 50,000 VM steps). Uncached 50-card reads use six queries, 150 returned rows
and 30,345 VM steps on the local fixture. No budget is widened.

The broader `TestPerformanceWorkSummaryBudgetAndPlans` remains red on this
checkout and on `origin/main` (`23bda1e1`) through a source overlay of the changed
read files. Its non-cadence 50-card stranger sample is identical: five queries,
200 rows and 58,310 VM steps against the unchanged 50,000 ceiling, with the same
unreviewed authorization/summary query plans. This is baseline evidence, not a
passing full summary gate. Cadence-specific acceptance above does not waive it.

```sh
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 ./scripts/slot run tests -- \
  go test -C core ./internal/primitives -run '^TestPerformanceCadenceTrackingBudget$' -count=1 -v -timeout=5m
```

## SQLite temp space and migration 116

Core uses `modernc.org/sqlite v1.38.2` (SQLite 3.50.3), with disk-backed temporary
storage enabled by its `TEMP_STORE=1` build. Its translated Unix VFS honors
`SQLITE_TMPDIR`, then `TMPDIR`, then `/var/tmp`, `/usr/tmp`, `/tmp`, and `.`.
See [SQLite's temp-file documentation](https://www.sqlite.org/tempfiles.html).
Modernc libc snapshots the process environment and the VFS caches its environment
choices during initialization; merely calling Go's `os.Setenv` later is unreliable.
The two production Go entrypoints therefore select the process-wide
[`sqlite3_temp_directory`](https://www.sqlite.org/c3ref/temp_directory.html) once,
before opening any database. The pointer uses SQLite's allocator and remains
valid until process exit. No connection changes that global during serving, and
no migration uses `temp_store=MEMORY`. Embedded/library callers must explicitly
prepare their process spill volume before opening connections; a process serving
several workspaces cannot assign a different global directory to each connection.

Migration 116's canonical vertex backfill constructs a deduplicating `UNION`
B-tree of `(kind,id)` across the schema-owned source inventory. Its generated
typed/raw/history key statements run sequentially, so their UNION trees do not
all coexist. Let `U` be the distinct source vertex count, `K` and `I` the average
kind/ID byte lengths, and `D` the pre-upgrade database file size. A useful vertex
spill estimate is `T_vertex ≈ 1.5–2 × U × (K + I + 16)` bytes, including record,
page and partially filled B-tree overhead. This grows with distinct vertices,
not blob bytes or duplicate source rows. If `U ≈ fD/B`, where `f` is the fraction
of the DB occupied by vertex-bearing rows and `B` is their average stored bytes,
then `T_vertex/D ≈ (1.5–2)f(K+I+16)/B`. For `K=16`, `I=36`, `f=1`, `B=128–256`,
the estimate is approximately `0.4–1.1 × D`.

A local modernc disk-backed `WITHOUT ROWID` primary-key B-tree proxy, with
`kind='document_history'` and 36-byte IDs, measured 671,744 / 6,619,136 /
66,166,784 bytes for 10k / 100k / 1m distinct vertices (about 66 bytes/vertex).
This estimates the vertex UNION's record shape; it is **not** a measurement of
the incident database or peak whole-migration usage. Reproduce the proxy by
creating `TEMP TABLE vertices(kind TEXT,id TEXT,PRIMARY KEY(kind,id)) WITHOUT ROWID`,
inserting those distinct pairs with `temp_store=FILE`, and multiplying
`PRAGMA temp.page_count` by `PRAGMA temp.page_size`.

For an initial deploy budget, reserve `max(256 MiB, 2D)` for temporary spills.
The reported 1.9 GB database therefore suggests about 3.8 GB of spill headroom,
on the state volume or a dedicated disk-backed override, not a 64 MiB tmpfs.
Add separate capacity for durable admission-index growth, WAL and any backup or
`VACUUM INTO` destination. There is no universal peak bound from `D` alone:
root-to-vertex reachability can grow with `R × V` in dense graphs, and its
recursive work tables and statement journals can exceed the vertex estimate.
Observe large/dense upgrade fixtures and provision more space if needed; this
change fixes placement, not graph amplification or a full state volume.

`TestSQLiteStartupTempDirectory` runs fresh subprocesses with a nonexistent
general `TMPDIR`. Before the fix the VFS selected `/var/tmp/etilqs_*`, failing
the database-local-path assertion. After configuration, a real 16 MiB payload
fixture and index use a 97,939,456-byte temp pager (over 64 MiB), verified through
the native VFS file-size method. The test checks the selected VFS filename,
explicit override and raw-driver environment behavior, cleanup, permissions,
invalid overrides, symlinks and an active directory lease. Docker was unavailable
on the local test host; this uses the invalid-default-path fallback rather than
claiming a capped-tmpfs `SQLITE_FULL` reproduction. The backup subprocess
environment regression likewise failed before its export was added.

All connections opened by core, including later pooled connections, compaction,
and the document-history worker, use the same immutable VFS setting. Offline
reclamation configures it before its separate pool opens. External backup uses
SQLite's page-copy API and writes its output database to the backup destination;
the wrapper also exports `SQLITE_TMPDIR` for temp-heavy manifest queries.
WAL and recovery journals remain beside their database and must never be swept.
See the [deployment and restart procedure](runbook.md#migrations--initialization).
No HTTP routes, read queries, budgets or query-plan exceptions change. Startup
adds one directory inventory proportional to its stale file count, once before
readiness; serving paths gain no filesystem scan or SQL work.

```sh
go test -C core ./internal/storage -run '^TestSQLiteStartupTempDirectory$' -count=1 -v
python3 -m unittest scripts/tests/test_backup_sqlite_tmpdir.py
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 go test -C core -p=1 ./internal/storage \
  -run '^TestPerformanceStartupAndMigrations$' -count=1 -v -timeout=10m
```

## Canonical admission: main reproduction and candidate index

**Bounded-route gates pass; legacy inventory gaps remain.** The 50,000 VM
ceiling, budgets, baselines and query-plan allowances are unchanged. This change
removes eager workspace denial capture from bounded reads. Local scale results
do not certify repository-wide CI or authorize merging.

Complete native HTTP requests were measured on main `b7c3a77f`, before #463
merged, using `perfguard.Seed`'s analyzed 4,096-row corpus and 256 private cards.
Each route/reader opens a fresh instrumented pool; the second request is warm;
the third follows an authorization epoch advance from a canonical thread write.
Authentication is included. The fixture omits unrelated PM/stream runtime setup.

| Main route / reader                              |  First VM |   Warm VM | Invalidated VM |
| ------------------------------------------------ | --------: | --------: | -------------: |
| `/work?limit=20` / owner                         |   401,329 |   401,281 |        401,405 |
| `/work?limit=20` / partial                       | 2,112,717 |   873,276 |      2,112,857 |
| `/brief` / owner                                 |       657 |       616 |            740 |
| `/brief` / partial                               | 1,240,051 |       610 |      1,240,191 |
| `/cards?limit=20` / owner                        | 3,339,879 | 3,339,838 |      3,339,962 |
| `/cards?limit=20` / partial                      | 7,162,750 | 5,923,309 |      7,162,890 |
| `/boards?limit=20` / owner                       | 2,301,658 | 2,301,617 |      2,301,741 |
| `/boards?limit=20` / partial                     | 9,520,232 | 8,280,791 |      9,520,372 |
| `/boards/scale-board-0/cards?limit=20` / owner   | 3,242,079 | 3,242,038 |      3,242,162 |
| `/boards/scale-board-0/cards?limit=20` / partial | 8,682,552 | 7,443,111 |      8,682,692 |
| `/cards/scale-card-1` / owner                    |     1,790 |     1,749 |          1,873 |
| `/cards/scale-card-1` / partial                  | 2,188,574 |   949,133 |      2,188,714 |
| `/boards/scale-board-1` / owner                  |    20,158 |    20,117 |         20,241 |
| `/boards/scale-board-1` / denied (404)           | 1,430,712 |   191,271 |      1,430,852 |

The partial reader's cold-minus-warm overhead is consistently 1,239,441 VM.
This is a complete request delta, not a separately measured capture statement.
The denial rebuild therefore predates grouping/rollup #463. Brief is **empty**
in this corpus; its numbers demonstrate admission, not populated brief acceptance.
`limit=20` does not page the legacy card inventories: they hydrate thousands of
rows. Board aggregation and work lifecycle inventories also exceed the ceiling
after the authorization cache is warm.

Migration 116 maintains principal-independent root reachability and reachable
owner counts in the canonical writer's transaction. Ordinary candidates use at
most three existence seeks in `(ledger,kind,id,unconditional,owner)`, independent
of ancestry size and unrelated denial reachability. Direct edges retain origin
contributions, and structural inheritance shares the old policy's executable
inventory. Legacy, compact and common-only ledgers remain separate. Owners keep
their SQLite value types, including numeric legacy JSON values.

Topology additions visit newly reached root/vertex pairs. Deletion recomputes
only roots that reached the removed edge and changes only memberships that
differ. Owner transfer updates owner counts without rebuilding reachability.
A write can read its affected root's reachable component; it never rebuilds an
unrelated component. Initial migration backfills the derived indexes once.
Storage is proportional to reachable root/vertex pairs across three ledgers,
plus direct contributions and spelling prefixes; dense connected graphs can
require the product of root and vertex counts.

Projection spellings preserve binary endpoints, NOCASE atoms, document aliases,
external publications and retained purge provenance. Prose now seeks full
Unicode-folded digests through a counted 16-rune prefix index. Only existing
prefixes continue, and the complete legacy byte-length/boundary matcher remains
authoritative. This costs O(admitted reference starts and matching text prefixes),
without scanning unrelated spellings sharing a token or punctuation bucket.
Resource/text SQL proofs retain their 8,192-entry/1-MiB bound, epoch and
`total_changes()` guards, and request-lifetime tokens.

`/work` pages lifecycle removals after active work through its opaque
continuation. Existing v1 cursors remain accepted; v2 also carries lifecycle
position. Public, required-owner and designated-PM routing indexes skip private
inactive prefixes before hydration. The consuming SQL still applies canonical
visibility before counts, lookahead and output. Following `next_cursor` to empty
collects the complete visible inventory; no global card/document/topic/thread
inventory runs on an ordinary page.

The optional compact table may be removed independently: required key triggers
refer to an admission-owned compact atom mirror. Capability recreation refreshes
and reinstalls its optional-table sync hooks. A selected but missing capability
fails closed, while legacy candidate reads remain usable after teardown.

### Epoch churn with continuous writers

`TestPerformanceCanonicalEpochChurn` interleaves committed writes with complete
partial-reader `GET /cards/scale-card-1` requests on the analyzed 4,096-row
corpus. Each mode uses a fresh pool: one cold request, then eight further reads.
The idle control performs no writes; the other modes write before every later
read. Writes target public card/thread 4,095, unrelated to the requested card.
The v0.13.28 runtime at `4abbb1e9fd017840362df41b255a87b7d21fcd75` was replayed
through a Go source overlay with this same test and a fresh native fixture.
Both versions were also run with `GOMAXPROCS=1`; this limits Go execution to one
P, not a container CPU quota. VM counts, not host timings, are the comparison.

| Interleaved write          | Main epoch advances per write | Main misses after writes | #469 epoch advances per write | #469 closure captures after writes |
| -------------------------- | ----------------------------: | -----------------------: | ----------------------------: | ---------------------------------: |
| Thread attribution only    |                             3 |                      8/8 |                             1 |                                0/8 |
| Harmless public title edit |                             5 |                      8/8 |                             3 |                                0/8 |
| Public event append        |                             4 |                      8/8 |                             4 |                                0/8 |

Main's idle control has one cold miss and eight warm hits. Each write mode has
nine misses including its cold request: **all 24 interleaved writes invalidate
the next read**. Each HTTP request may probe twice; only an absent or stale epoch
binding counts as a capture, since CASE skips rebuilding on a current binding.
Main's warm point read uses 949,304 VM; after a write it uses 2,188,885 VM, a
**1,239,581-VM invalidation penalty**. The initial cold sample uses 2,371,174 VM.
#469 uses 4,563 VM cold/idle and 4,572 after a write on the instrumented pool,
with no closure probe or capture. The separate native route matrix writes through
another pool and retains its 4,563-VM result in every phase.

| Canonical SQL write and triggers | Main VM | #469 VM |
| -------------------------------- | ------: | ------: |
| Attribution                      |     434 |     189 |
| Public title                     |     837 |   1,802 |
| Public event append              |   1,603 |   7,648 |

The write probe exposed an admission teardown defect: TEXT contribution IDs
compared directly with INTEGER edge IDs scanned the entire `exact` origin range.
A title edit initially cost 564,443 VM. Matching the stored TEXT affinity restores
the composite-key seek. The independent 10,000-unrelated-origin regression fails
at 51,551 VM before that fix and passes at 1,890, preserving all contributions.
The interleaved test also enforces 50,000 VM for these unrelated write controls.
Affected ownership/topology changes can still visit their actual descendants;
write-audit tests continue to reject unrelated index rewrites.
These write controls exclude HTTP mutation authorization; the measured reads
include authentication and the full HTTP handler.

This confirms the reported epoch-churn mechanism locally. For a workspace with
at least one registered canonical write between polls, a reader's old shared
closure cache can miss on almost every poll, regardless of whether the writer
changes that reader's target. Multiple principals pay their own rebuilds.
#469 retains conservative epochs but makes those misses irrelevant to bounded
admission: it reads transactionally maintained owner indexes and starts fresh
request proofs. It removes both the repeated workspace closure capture and the
expensive warm ancestry path. Write maintenance grows with changed contributions
and affected reachability, not with the number of reader principals.

An epoch-only first PR could suppress attribution-only invalidations, but would
leave cold admission, warm ancestry and content/event-driven invalidations. New
content can introduce inherited private references, so excluding it safely needs
dependency-aware maintenance. With the full repair implemented, retain broad
epochs rather than adding another revocation-sensitive optimization first.

This synthetic reproduction does not attribute every reported CPU spike or
three-second cancellation to denial capture. Native VM counts exclude Go work
and SQLite preparation time; no production deadline guarantee follows from them.
Authentication-token queries do not themselves capture business-resource denials,
but may wait behind shared CPU, pool or database contention. Other projection
work, full inventories, canonical mutation authorization and access-write
maintenance retain their own costs. Net CPU depends on the read/write mix.

### Required CI regressions repaired without new allowances

Two required checks failed on the previous draft head, independently of the six
advisory inventory overruns. Both original tests and limits remain unchanged:

- Scoped all-public hierarchy used 56,154 VM. It now avoids proof-cache work
  when there are no denied roots and uses **47,982 VM**. One statement proof
  retains epoch, connection-change and token validation for the shared public
  fact; expired or invalidated proofs use current indexed authority. Borrowed
  writer coverage includes revocation with an apparently unchanged epoch.
- The released-schema 100k-wakeup startup fixture performed 1,100,070 writes
  versus 11,070 at 1k. Migration 116 now leaves untouched historical wakeup
  parents in their existing thread/event indexes. Reachability follows those
  indexed relations; reached wakeups receive keys and spellings as needed, and
  structural edits materialize the old contribution before normal teardown.
  Both 1k and 100k now perform **68 writes and allocate 424 pages**, including
  the durable migration cursor introduced below. Historical
  parity covers private threads/events, explicit references and referring cards,
  a 128-wakeup reference cycle, empty legacy parents, moves, revocation/transfer,
  rename, deletion and compact cutover. New wakeups use the ordinary incremental
  triggers.

### Security review: late resolution and retained spellings

The Sol review reproduced two authorization leaks in the previous #469 head
(`987295b7`). Both now have regressions that failed against that head:

- After upgrading a public historical wakeup with no reference atoms, making
  its parent thread private failed to deny a referring card. The common-only
  reach insertion created the dependency before the legacy/compact parent
  memberships existed; later key conflicts skipped the insert-trigger side
  effect. Each newly reached wakeup now resolves keys and then propagates from
  that membership's own ledger/root. Recursive INSERT evaluation order cannot
  decide visibility. Coverage includes legacy, compact, forced legacy and a
  missing selection, navigational references and ownership transfer.
- An alias-only document tombstone failed to deny cards referencing its typed
  or bare canonical ID. Retained alias/tombstone tuples now contribute canonical
  ID keys independently of retained spelling, through identical migration and
  runtime SQL. The audit also covered empty tombstone spellings and external-key
  IDs. An all-resource-kind upgrade test found another omission: skipping all
  historical wakeup identities also skipped retained wakeup aliases. Backfill
  now includes those via origin-index seeks while leaving untouched canonical
  wakeup history uncopied.

`TestAdmissionUpgradeRandomizedScaleParity` seeds 4,096 cards into the released
schema, with public historical wakeups, alias-only tombstones and retained
aliases before migration 116. Two fixed seeds in each fence mode exercise 48 post-migration
operations: access revocation/transfer, wakeup parent/reference changes, alias
renames, tombstone spelling/owner changes and ledger cutover. After each operation,
the old canonical closure and maintained admission must agree for nine principal
scopes: owners, unrelated human/agent, designated PM, owner PM, auth administrator,
numeric legacy owner and anonymous. Comparison includes the complete denial and
projection-spelling sets, actual scoped rows, limited pages, counts and lookahead.
The second seed reverses admission-trigger installation order before exercising
the transitions, so independent-trigger order cannot conceal a missing effect.
The two exact reported reproductions also run separately in the fast tier.

The conflict audit covered admission keys/links, graph contributions, root and
owner counts, reach membership, spelling prefixes, compact synchronization and
the insert-before-delete source/evidence diff. Duplicate keys/links suppress only
identical contributions; graph/root/owner aggregates update counts explicitly;
removal keeps surviving contributors. Historical lazy key resolution was the
only path that introduced graph dependencies inside a recursive reach insertion;
its propagation now runs after resolution per membership. The spelling matrix
covers every registered source kind plus external keys, virtual document history
and series streams, with canonical typed/bare IDs, retained aliases, empty
spellings, duplicate contributors and deletion/owner transitions. Existing live
handle/revision/Unicode/binary policy tests remain active. No budget, allowance,
startup guard or test timeout changed.

Legacy schema-reconstruction fixtures remove post-116 admission triggers before
dropping older indexes. Their canonical legacy triggers and all retained-privacy
assertions remain active; migration 116 must rebuild the new derived state.

### Second security review: structural identity and fenced upgrades

The second Sol review reproduced two additional defects against published
`a82c1cea`: a private `work_metadata` row for an absent card did not propagate
canonical typed-ID or navigational inheritance to its dependents, and a fenced
schema-115 upgrade recorded migration 116 while ignoring its chunk callback.
Both reproductions became failing regressions before their fixes. The former
returned `typed-dependent` and `nav-dependent` to a pinned unrelated reader; the
latter recorded 116 without `resource_access_admission_root_owners`.

Canonical ID keys now derive from canonical declarations: the shared structural
ownership inventory's endpoints, raw/exact/external indexed source vertices,
navigational source vertices and roots. This includes absent resources represented
only by metadata, observations, participants or plan steps. Typed IDs are also
navigational keys; bare-ID exclusions, external publication policy and virtual
document history retain the forward oracle's rules. Deriving keys from the
key-generated graph itself is unsafe: recursive key creation can suppress the
resolver trigger and leave one spelling unresolved. Canonical input triggers and
migration backfill use the same declaration inventory instead.

One immutable canonical ID mapping is shared across declarations and retained
when rows disappear. These keys carry no authority: only current roots and
reachability can deny a resource. Retention preserves the forward policy's
existence-independent ID resolution, including deletion and later reuse, without
scanning all declarations on an access write. Storage adds O(distinct observed
canonical identities) to the existing graph/reachability terms. Alias and
publication contributions still follow their own live/retained source policy;
this dictionary stores only ID mappings. ID-unchanged updates do no dictionary
writes. Untouched historical wakeups keep their existing lazy path, so startup
does not copy or index that history.

The deterministic sweep covers every ownership/structural kind through typed,
bare and navigational references, including policy-excluded kinds. Concrete
missing-card metadata, empty/nested plan steps, observations, participants and
ref-edge fixtures supplement existing live/alias/tombstone tests. Creation order,
live arrival/rename/deletion, moved and deleted metadata, owner transfer,
source removal and retained-ID reuse all compare old and new authority. The
4,096-card randomized differential now runs two seeds through both fenced and
unfenced upgrades (four runs), with 48 post-migration operations each, including
structural-only identity creation, updates, deletion and live-row transitions.
All nine principal scopes compare complete denial/projection-spelling sets and
actual pinned candidate rows, pages, counts and lookahead. Historical structural
cycles and the earlier exact Sol regressions also pass.

Fenced and ordinary loaders, plus the history fixture, now share one transaction
runner. A chunk executes inside a runner-owned transaction; it cannot write its
own version. Empty plans and mixed chunk/transactional plans are rejected. On
completion the runner verifies every required admission table and absence of an
unfinished cursor before atomically recording 116; fenced completion also updates
the ledger digest in that same commit. Already-recorded 116 is verified on every
open, so a malformed preview fails during initialization. No legacy fence is
removed or its missing function registered. Tests cover both loaders, unchanged
reopens, missing recorded tables, no-op callbacks, skipped steps, digest/progress
behavior and cancellation after one durable root followed by resume without
replaying that root.

Fresh native verification reproduces **all 54 route VM measurements below**.
Partial grouping remains 48,169 VM in every phase; the combined native run took
209.84 s and retains only the six documented legacy inventory assertions. The
interleaved controls remain 4,563 VM idle / 4,572 after writes, with zero closure
captures across all 24 writes. Write controls now use **189 / 1,802 / 7,648 VM**
for attribution/title/event writes; the latter two include canonical-ID maintenance.
The unchanged-index/unrelated-component write audits pass. No net production CPU
reduction is asserted from VM counts alone; the read/write mix still matters.

The first full fast-suite run exposed a required write regression:
`TestRevokedSeriesCapacityScaleAndPlan` measured 5,074 VM over 100k retired
series definitions, above its unchanged 5,000 ceiling. Filter/evidence-only
vertices have no ID spellings under the forward policy; their canonical-key
trigger now exits before six no-op INSERTs. The same unmodified regression
passes at **2,434 VM / zero scan steps**. The all-kind pinned differential and
write-index audits pass with that guard; no excluded identity gained propagation.

The 512-root/4,096-card migration profile remains **514 transactions**, with
31,750 setup row changes, at most **1,546 per root**, a longest chunk of 67.72 ms
and 7.96 s backfill time on this run. The unchanged 1k/100k receipt guard passes
with **68 writes / 424 allocated pages** at both sizes; the 100k startup took
358 ms. Additional trigger/schema pages are constant, not history-sized. These
replace earlier implementation measurements; no budget, baseline, allowance or
timeout changed. The separate #472 inventory gap remains unchanged.

### GLM follow-up: resumable migration and explicit scope

Migration 116 separates schema/direct-index setup from transitive reachability.
Setup remains one atomic, linear phase; the potentially root-by-vertex backfill
then commits **one distinct root per transaction**, with a durable primary-key
cursor in the same commit. Multiple retained owners of a root do not replay it.
Cancellation or restart resumes after the last committed root; the migration is
marked complete only after its final chunk. Initialization returns no workspace
handle before completion, so partially built authority cannot serve requests.
Total work and storage can still grow as roots times reachable vertices; a root
chunk can traverse its entire affected component. This is chunking, not a claim
of constant total startup cost or a measurement of an unspecified production
workspace. Runtime grant/revoke component work remains the accepted write-side
tradeoff (GLM #2).

The released-schema synthetic profile has 4,096 cards, 512 private roots and a
512-dependent shared hub. It completed in **514 transactions**, with 31,750 row
changes in setup and at most 1,546 per root chunk. The longest root chunk was
62.93 ms; backfill elapsed time was 12.43 s on this local run. A separate fast
regression interrupts after one independently visible root commit, reopens the
workspace, rejects replay of that root with an abort trigger, and verifies full
old/new admission parity after completion. It fails against the former atomic
migration because no partial root commit is visible. No startup allowance changed.

The retained-wakeup backfill also now pins the existing origin-leading identity
uniqueness index. EXPLAIN previously selected the kind index and scanned every
historical wakeup for the retained-origin arm; the regression requires origin
seeks instead. The unchanged 1k/100k receipt startup guard passes with identical
68 writes and 424 allocated pages; the latest 100k startup took 358 ms. This does
not copy untouched wakeup history or add an index over that history.

`hiddenWorkPage` now rejects a missing request access scope before reading any
rows, and returns no refs or continuation. An explicit anonymous scope is still
a scope and follows normal policy. Its regression previously returned the
private archived card's ID and handle. Direct work test fixtures supply their
actual reader scope, preserving ordering and callback-pagination assertions.
The dead candidate-proof `ancestors` argument and its reuse comment were removed;
only SQL-proven candidate results are cached, with the existing entry/byte cap,
epoch, connection-change and token-expiry checks intact.

The topics-update lifecycle probe now uses `ReferenceSQL` and the canonical
`idx_work_access_project` expression index. A populated 10,000-card probe shows
**29 VM / zero scan steps**, and its complete topic update uses **1,950 VM** under
the unchanged 50,000 ceiling. The old raw-JSON probe already used the covering
`idx_work_metadata_project` index at the same 29 VM: the reported full-scan
hypothesis was refuted. This is consistent index-expression use, not a claimed
VM reduction.

The remaining GLM observations are documented rather than changed: grant/revoke
may traverse its affected component (#2); lifecycle continuations can yield an
empty work page, and clients must honor the cursor and their existing traversal
caps/honest partial result (#3); SQL trigger construction still depends on known
string shapes, with explicit artifact/revision refresh exceptions (#6); and the
six improved, pre-existing full-inventory overruns remain tracked in #472 (#8).
No budget, baseline, allowance, required-gate selection or timeout is widened.

### Final native bounded-route measurements

The checked-in native request matrix independently seeds the shared analyzed
4,096-row corpus, opens a fresh instrumented pool per route/reader, includes
authentication, and measures first, warm and canonical-epoch-invalidated requests.
The table below is from the final run, **without a test overlay**:

| Route / reader                                             | First VM | Warm VM | Invalidated VM |
| ---------------------------------------------------------- | -------: | ------: | -------------: |
| `/work?limit=20` / owner                                   |   21,285 |  21,285 |         21,285 |
| `/work?limit=20` / partial                                 |   35,714 |  35,714 |         35,714 |
| `/brief` / owner                                           |   27,913 |  27,913 |         27,913 |
| `/brief` / partial                                         |   46,085 |  46,085 |         46,085 |
| `/cards?summary=1&limit=20` / owner                        |   16,920 |  16,920 |         16,920 |
| `/cards?summary=1&limit=20` / partial                      |   29,960 |  29,960 |         29,960 |
| `/boards/scale-board-0/cards?summary=1&limit=20` / owner   |   19,240 |  19,240 |         19,240 |
| `/boards/scale-board-0/cards?summary=1&limit=20` / partial |   37,560 |  37,560 |         37,560 |
| `/cards/scale-card-1` / owner                              |    2,056 |   2,056 |          2,056 |
| `/cards/scale-card-1` / partial                            |    4,563 |   4,563 |          4,563 |
| `/boards/scale-board-1` / owner                            |   15,319 |  15,319 |         15,319 |
| `/boards/scale-board-1` / partial                          |      405 |     405 |            405 |

The separate complete grouping gate uses **30,479 owner / 48,169 partial VM**
in every phase, under the unchanged 50,000 ceiling. The populated brief is now
46,085 partial VM; main's original baseline brief was empty, so these are different
content controls. The fresh post-review combined run took 154.57 seconds including fixture seeds,
interleaved writes and dense ancestry/bucket probes. The dead `ancestors` JSON
argument was removed from candidate proofs; these measurements include that change.
The earlier rebase onto v0.13.29 (`74aba48f`) reproduced the prior measurements;
the table records the latest implementation rather than that earlier run.
Every bounded request, the grouping gate and the micro/adversarial gates passed.
The combined command remains red only for the six legacy inventory reader cases
below; their hard 50,000 assertions are deliberately retained.

### Separate known issue: legacy full inventory hydration

`limit=20` does not page the legacy card/board inventories. They still hydrate
workspace inventories and exceed 50,000 VM on main after warming. This remains
a separate pagination/hydration repair, tracked in
[issue #472](https://github.com/Git-on-my-level/agent-nexus/issues/472), with no
new budget, baseline or allowance.
Full inventories now take one complete indexed denial batch for their immutable
read transaction, then retain the candidate projection policy and constant-time
membership map. Empty-root inventories use the cheaper empty-root fast path.
Borrowed mutable transactions skip this batch and evaluate current indexed owners.
No ordinary bounded route takes this full inventory batch.

| Legacy inventory / reader                        | Main warm VM (`b7c3a77f`) | Final VM, each phase |
| ------------------------------------------------ | ------------------------: | -------------------: |
| `/cards?limit=20` / owner                        |                 3,339,838 |            3,275,798 |
| `/cards?limit=20` / partial                      |                 5,923,309 |            3,807,859 |
| `/boards?limit=20` / owner                       |                 2,301,617 |            1,815,211 |
| `/boards?limit=20` / partial                     |                 8,280,791 |            3,735,985 |
| `/boards/scale-board-0/cards?limit=20` / owner   |                 3,242,038 |            3,101,254 |
| `/boards/scale-board-0/cards?limit=20` / partial |                 7,443,111 |            4,653,975 |

All six warm costs improve on the original native main measurements, even though
current main also adds grouping hydration. A supplemental main-source replay on
the same saved synthetic corpus at `cb156382` measured 3,673,389 / 38,336,046 VM
for owner/partial cards, 2,301,617 / 8,280,791 for boards, and
3,554,786 / 7,755,859 for board cards. This supplemental code-overlay probe is
not substituted for the fresh native before/after tables.

All six failures above belong to `TestPerformanceCanonicalAdmission`, whose
`requirePerformanceTest` guard requires `ANX_PERFORMANCE_TEST=1` and a non-short
run. Required server CI discovers this test but leaves that variable unset, so
it skips at runtime; a local run with the same environment confirms the skip.
Main's required CI also leaves the variable unset, and main predates this new
diagnostic test. None of these six failures is a required gate on either tree.
The repository's main ruleset requires `ci-ok`; `.github/workflows/ci.yml` does
not include the separate `.github/workflows/performance.yml` jobs in `ci-ok`.
That advisory workflow runs on main pushes or manual dispatch and enables the
variable. Its discovered legacy shard and broad advisory partition both select
this test. Those runs retain all six hard failures until #472 fixes hydration;
no allowance, test selection change or gate weakening is introduced here.

### Regression and write-cost evidence

- The draft head's partial grouping / populated brief / work costs were
  179,091 / 177,735 / 3,404,754 VM. A controlled saved-corpus probe after compact
  teardown repair still used 179,116 / 177,735 for grouping/brief. Reverse ancestry
  batching also missed the ceiling; persisted reachability removed that read work.
- A 128-node DAG with 1,016 incoming dependencies, cycles and 128 reachable owned
  roots now uses **2,429 VM** per phase. The 512-spelling legacy bucket fanout
  case used 276,447 VM before prefix seeks and now uses **12,226 VM** per phase.
  Both include cold admission and retain a 50,000 ceiling.
- A complete `/work?limit=20` over 4,096 private archived cards used 230,045 VM
  before owner routing and now uses **640 VM** per phase, returning no work,
  archived refs or continuation. No hidden-prefix marker is exposed.
- The isolated point gate uses **269 / 479 / 479 cold VM** for 0 / 1,024 / 4,096
  unrelated denials. Warm membership is **55 / 47 / 47 VM** under the unchanged
  64-step cap.
- Write-audit triggers reproduce unnecessary graph/key/index churn before the
  fix. Unrelated card/metadata edits and identical-owner updates now touch no
  admission indexes. Owner transfer changes root declarations, root-owner counts
  and reachable-owner entries only. Abort guards protect an unrelated component.
  Source atom rowids and unchanged evidence publication keys survive updates.
  Deliberate artifact/revision self-updates still restore history edges.
- The 10,000-document handle test passed on main in 20.19s and initially on this
  branch in 99.24s. Timeout stacks showed repeated `prepareV2` calls: modernc
  prepares its native VM for each Go prepared-statement execution, recompiling
  the larger trigger program. One bulk fixture INSERT retains all 10,000 rows,
  every trigger and every assertion, and passed in **11.71s**. The two reservation
  queries retain their original 256-VM/no-scan guard. No test timeout was raised.
  Confirmed slow fixture loops for 10,000 tombstones, 2,001/4,001 cards and 201
  hidden events use the same bulk setup technique, preserving their exact rows,
  triggers and assertions. Their focused checks completed in 13–15 seconds.

Differential coverage compares the old full closure with the actual reachable
owner index on the scale corpus (3,344 denied identities for partial/anonymous,
zero for owner/designated-owner), plus 48 randomized mutation rounds and full
projection-spelling comparisons. Tests cover inherited references, case/raw-ID
collisions, virtual purged documents, exact/compact cutover and teardown,
missing epochs/selections, counts/lookahead, grant/revoke/transfer and snapshot
isolation. Full inventory batching is checked for one batch per immutable read
and for fresh authority inside borrowed writers after unversioned compact writes.
The protected visit test uses the approved legacy constructor and keeps all its
complete-denial enforcement assertions.

```sh
ANX_PERFORMANCE_TEST=1 ./scripts/slot run tests -- go test -C core ./internal/server \
  -run '^TestPerformance(CanonicalAdmission|GroupingFeedBudget|ResourceReadSnapshotBudget|WorkLifecyclePrivatePrefixBudget|CanonicalEpochChurn)$|^TestCandidateDenseAncestryAndBucketFanout$' \
  -count=1 -v -timeout=30m
go test -C core ./internal/primitives \
  -run '^Test(AdmissionGraph|AdmissionWrites|InventoryAdmission|Candidate|WorkLifecycle|OverviewReadSnapshot|ResourceReadSnapshotRandomizedDifferential)' \
  -count=1 -timeout=15m
```

See the [architecture decision](../../docs/architecture/canonical-admission.md).

## Grouping and promise preservation

The grouping API admits recent physical routing identities before visibility and metadata matching,
uses indexed reverse plan references for parentage, and batches rollups under
the 200-step plan limit. The [grouped-work guide](../../docs/grouping-work.md)
documents the candidate windows, indexes, complexity and lower-bound budget.
`internal/primitives/testdata/work_grouping_budget.json` enforces 50,000 VM steps
for tray/home and hierarchy reads against 10,000 unrelated cards. Existing brief
ceilings remain unchanged. Promise inputs seek relevant next/evidence facts;
ordinary chatter cannot displace a promise from the bounded window. A dense
10,000-card denied prefix used 110,457 VM steps before physical admission and
8,198 afterwards; the admitted brief uses 10,028. Scoped budget markers are
always conservative, independent of hidden window occupancy. The populated
50-card/100-series-ref brief remains below its unchanged 50,000 ceiling.

```sh
go test -C core ./internal/primitives -run '^TestGrouping' -count=1 -v
go test -C core ./internal/storage -run '^TestGroupingUpgrade' -count=1
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/series -run '^TestPerformanceBriefSeriesBudget$' -count=1 -v -timeout=10m
```

The #463 investigation originally blocked HTTP scale acceptance on cold
canonical admission. `/work/ungrouped` is an exact collection operation, with
no card selector named `ungrouped`; its selector regression prevents an
unnecessary point authorization read or dependence on a private card with that
handle.

Historical `TestPerformanceGroupingFeedBudget` measurements on the shared
4,096-row corpus were 31,323 / 31,275 / 31,399 VM for the owner and
1,269,342 / 29,901 / 1,269,482 for the denied reader (first / warm / invalidated).
They omit unrelated PM/stream setup and are not comparable to the earlier broad
matrix's 273,020 / 1,603,957 figures. Replaying denial capture used 1,239,696 VM,
leaving 29,786 elsewhere in the invalidated request.

That workspace closure preceded page admission, so changing the candidate
window could not remove it. The indexed authorization repair above brings the
complete grouping request to 30,479 owner / 48,169 partial VM in all phases.
These historical figures are evidence of the repaired cause, not a baseline or
allowance. The separate legacy inventory gap remains documented above. No
budget or query-plan requirement is weakened.

```sh
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/server -run '^TestPerformanceGroupingFeedBudget$' -count=1 -v -timeout=20m
```

## PM proposal admission

PM proposal admission uses partial expression indexes on workspace, actor, work
and scope for awaiting decisions, with a separate matching index for pending
human proposals. Equality fixes every prefix, so SQLite can take the first rowid
without scanning actor history or sorting. `TestProposalLookupIndexedPlan`
checks the actual preflight/transactional SQL for indexed SEARCH plans and no
temporary sort; `TestProposalSameActorHistoryScale` measures the full proposal
service with 100 and 100,000 same-actor/same-work decision rows, including a dense
awaiting prefix with unrelated scopes. Its fixed budget is 2,048
VM steps, 30 queries/returned rows, zero full-scan steps and zero sorts:

```sh
go test -C core ./internal/pm -run 'TestProposal(LookupIndexedPlan|SameActorHistoryScale)$' -count=1 -v
```

Receipt GC uses the existing creation-time index and selects at most
`gcBatchSize` rowids per transaction; its cost is O(batch), not O(receipt history).
It yields between batches, never runs inside an HTTP writer, and deletes no
canonical data. Retention/recovery windows are defined once in
`internal/replaypolicy/policy.json`; see
`web-ui/docs/durable-user-work.md` for key-reuse and client cutoff semantics.

A read or stream tick must be bounded by its selector or page, with indexed
filtering before materialization. Hot-path O(workspace) work is a merge-blocking
P1, including an authorization query that rebuilds a graph or scans stored text.
A LIMIT applied after loading, sorting, or enriching every record is not a bound.

For heap/native allocation, RSS and hot-route latency measurements on disposable
local workspace copies, see [the resident memory probe](resident-memory.md).

## Brief initiative series

`GET /brief` admits at most 50 initiatives before enriching their canonical
`cards.refs_json`. Each row admits its first two valid, distinct series refs
before visibility filtering (no refill past hidden candidates). Admission examines
at most 200 authored refs within the first 64 KiB of each card's refs JSON; SQL
caps returned bytes and a streaming decoder bounds Go allocation/work. One scoped card
primary-key batch and one `seriesref.LatestRefs` batch (100 refs)
load metadata and latest values; each ref uses the ordinary series visibility,
at most 100 registered label candidates and two matching streams. Explicit
unmatched/hidden selectors are omitted; a visible definition without registered
streams can still appear without values. Ambiguous selectors have null values.
Latest values are limited to seven days, independent of authored `_range` and
`_baseline` options. No related refs or plan refs are added to this list.

The first returned series is primary. One additional scoped batch reads at most
eight UTC daily buckets from each of `series_live_daily` and `series_daily` per
primary, using their `(series,labels,day)` primary keys. Raw density and older
history add no work; `LatestRefs` seeks raw endpoints through
`(series,labels,ts)`. The added work is O(admitted rows _ 2 _ 100) with fixed
bounds, at most three statements (zero with no initiatives), and no per-row
queries. Existing `/brief` budgets remain unchanged.

Flatness uses a seven-day lookback and a three-day threshold. Every daily low
and high in a consecutive suffix must be within `max(1e-9, abs(latest)*1e-6)`
of the latest numeric value. Whole edge buckets are included conservatively;
`for_seconds` spans the oldest qualifying bucket's last observation to the latest
observation, capped at seven days. Thus an intra-day excursion cannot disappear
when endpoints agree. Missing buckets break the span. Missing, ambiguous,
nonnumeric, future-contaminated or older-than-24h latest data yields false/zero,
while no returned series yields null. This is an observed stability signal;
it does not infer measurements between recorded observations.

Run the populated maximum-page scale gate (50 cards, 250 attached refs, 100 admitted refs and over
10,000 raw points) and functional privacy/normalization checks:

```sh
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/series \
  -run '^TestPerformanceBriefSeriesBudget$' -count=1 -v -timeout=10m
go test -C core ./internal/series -run '^TestBriefSeries' -count=1
pnpm -C web-ui exec vitest run tests/unit/briefSeries.test.js
```

## Brief resolved plan progress

Migration 108 indexes each plan's references in `brief_plan_refs`, with
`(ref,card_id)` reverse lookups and `(card_id,ref)` replacement/deletion. Plan
writes examine at most 200 steps. Referenced-card state, identity, lifecycle or
source-observation/evidence-key changes queue only indexed dependents, O(log references +
fanout); board lifecycle changes first seek its cards by board ID. No workspace
plan scan occurs on writes or reads. Metadata triggers compare only effective
phase, evidence identities, aliases and status, reading the old/new observations
by primary key. Timestamp-only polls, errors, stale reads and evidence-index
rebuilds do not queue referencers. Changed inputs enumerate old and new keys
under the existing 2,000-entry and 50-alias caps, seeking the reverse covering
index for each key, without workspace scans. Signature comparison sorts at most
4,001 bounded evidence entries, O(E log E); reverse probes cost O(K log R + affected fanout), where K
is the bounded old/new key set and R is the reverse-reference index. The
migration alone backfills all plans.

Maintenance folds at most 200 cards per transaction, resolving at most 200 steps
per plan through the same scoped ref reader and `plans.Compute` as `plan_state`.
Distinct references are resolved in batches of 200 using existing ID, handle,
source URL and evidence lookup indexes. It stores scalar progress counts and a
bounded provenance snapshot of the current plan and the plan that supplied the
retained step-change clock; indexed ownership edges keep a private fold
hidden until replacement, even when its plan is cleared between maintenance
runs or its refs change without a state change. The snapshot never accumulates
history: each component admits at most 200 steps. `/brief` reads these counts from its existing 50-row projection; progress
adds no query, JSON parsing or reference resolution on read. Existing budgets
remain unchanged. Run `TestBriefPlanRefsUpgradeAndInvalidation`,
`TestBriefResolvesReferencedStepProgressAndPromises`,
`TestBriefResolvedProgressRetainsPrivatePlanProvenance`, and the populated brief
series gate above.

## Idle streaming

Inbox, event and notification-receipt streams observe SQLite `data_version` on
one dedicated connection shared by the handler. The connection performs only
the version PRAGMA, holds no transaction and is released when the last stream
disconnects. An unchanged tick performs O(1) metadata work and a keepalive,
without authorization graph, projection or summary recomputation. Open asks also
track the earliest subject inactivity deadline while admitting each page; a clock
comparison on idle ticks starts a normal bounded sweep when that deadline passes.
This emits the stale transition once without SQL or a record scan on earlier idle
ticks. Poll intervals
have a 100 ms floor; the default remains one second.

Every committed database write invalidates this conservative cursor, including
external writers, non-event permission changes and derived projection updates.
On changes, streams retain the existing bounded page readers and authorization
checks. Inbox pages (200 candidates plus lookahead) share computation for matching
authorization scope, recipient and pagination position. The cache holds at most
128 pages and expires delivered pages after a poll interval so a late subscriber
gets current ages and health. An incomplete inbox sweep always continues; commits
during a sweep cause another sweep from the head. Event/receipt history chunks
continue without waiting for the poll timer. Global commit versions are never
exposed as client cursors.

Custom stores without the observer, and SQLite pools explicitly limited to one
connection, retain uncached polling rather than reserving their only connection.
The new cursor needs no table scans, indexes or migration. Existing changed-page
authorization and enrichment costs remain covered by their separate scale gates.

Reproduce the five-stream synthetic CPU/latency measurement (fixture setup and
initial multi-page delivery are outside the CPU profile):

```sh
cd core
ANX_STREAM_PROFILE="$PWD/stream-idle.pprof" go test ./internal/server \
  -run '^TestStreamIdleScaleProfile$' -count=1 -v -timeout=15m
go tool pprof -top -cum stream-idle.pprof
go test ./internal/server ./internal/primitives -run \
  'TestFiveIdleInbox|TestSharedInbox|TestInboxCommitDuringSweep|TestStreamRevision'
```

The profile includes ten bounded page latency probes; their recomputations are
reported explicitly. The deterministic regression requires zero page reads on
idle ticks, one shared page for five matching readers after a non-event commit,
and complete delivery after a mutation to an earlier page during a sweep.

## Running the gates

Agent detail's `summary=1` recent-card preview admits at most 51 current/recent-run
refs and two 51-position assignment windows (bare actor ID and typed ref).
Migration 73 installs `idx_cards_agent_summary(assignee,id)` for active cards and
the internal `agent_summary_card_positions` routing view. Handle/ID resolution
uses the existing unique handle and primary-key indexes. Only routing keys pass
through the view; canonical scoped card hydration receives at most 50 IDs in a
pinned read snapshot, then uses the existing bounded summary batches. Scoped
previews always mark `cards_truncated`, independent of hidden candidate counts.
A private prefix may leave fewer visible cards; the reader never scans beyond
the fixed candidate windows to fill the preview.

The added selection/enrichment cost grows with those fixed windows, not the
workspace. The preexisting command-center roster/open-ask projection and default
unpaged agent cards retain their existing main baselines; this preview does not
repair or expand those paths. Verify the new selector separately, including a
dense denied prefix, and the shared enrichment budget:

```sh
cd core
ANX_PERFORMANCE_TEST=1 go test -p=1 -parallel=1 ./internal/server \
  -run '^TestPerformance(AgentSummarySelection|WorkSummaryBudgetAndPlans)$' -count=1 -v
```

Run the cheap route-inventory, SQL-hook/classifier and migration-progress checks:

```sh
cd core
go test -short ./internal/server ./internal/storage ./internal/testutil/perfguard
```

The independent scale tier is advisory. `.github/workflows/performance.yml`
runs four `core-performance-routes` shards, four `core-performance-legacy`
shards, and their executed-artifact coverage check on pushes to main or manual
dispatch. Legacy shards discover their tests with `go test -list` and balance
whole tests by the measured seconds in `scripts/performance-legacy-weights.json`
(`scripts/performance-legacy-shard.py`), so a new test runs exactly once and no
shard approaches its ten-minute package timeout. It has
no pull-request trigger and no dependency from `ci-ok`, so it does not delay
ordinary PRs or releases. Failed deterministic checks remain visible in that
separate workflow. Run the same tier locally with:

```sh
cd core
ANX_PERFORMANCE_TEST=1 go test -p=1 -parallel=1 -timeout=120m -count=1 -v \
  ./internal/server ./internal/storage ./internal/testutil/perfguard -run 'TestPerformance|TestResourceAccessCommonReadPerformance|TestResourceAccessLargeDenialPrepareAndPMRoutes'
```

The fixture is synthetic; it never opens an existing workspace or reads real
credentials. `internal/testutil/perfguard.Seed(ctx, db, contentDir, owner,
seriesAnchor)` creates 4,096 cards, documents, artifacts, content-bearing events,
plans, inbox items, series points, principals and runs; nine boards include eight
private boards. Four PM record families contain 1,024 entries each (4,096 PM records total).
The public board contains 3,840 cards; 256 cards belong to the eight private
boards. Each artifact retains a 16 KiB logical content blob; summaries/FTS use
1 KiB prose to avoid redundant copies. Deterministic
IDs and content are shared, while the explicit series anchor keeps observations
inside the requested window. Canonical ownership/reference triggers remain on.
Content-addressed artifacts share one synthetic text blob to reduce disk work.

All routes reuse one migrated workspace, with a fresh instrumented SQL pool and
complete native handler/PM runtime for each case and principal. The first read
cannot inherit another route's authorization cache. Five warm reads reuse only
that pool. A real trigger-backed thread update then advances the authorization
epoch before a separately measured post-invalidation read. Cursor setup uses a
separate handler, preserving the measured handler's cache state. No global cache
reset or production cache API is introduced. `/overview` and the default changes
case explicitly start without a saved visit; a separate `returning-visit` changes
case persists a scoped visit through the setup handler, then requires a recent
private decision in the owner response and a non-null visit cursor for both
principals. Shard order cannot turn that workload into an empty first visit. The
unfiltered default stream retains 8,192+ events, 4,096 distinct baseline threads
and card references, the large-history thread and canonical private-board graph.
After #311 it starts at HEAD: its first completed chunk must skip old history,
and its second must deliver fresh public/private controls. The large-history
case explicitly resumes after event 0000 and checks two ordered bounded pages,
with a private-reference control inside the second page. Resumed and idle cases
check fresh delivery separately; timer keepalive flushes never count as reads.
Both principals exercise every case, and fixture policy is recorded in each
sample. The startup test uses a separate schema-58 corpus
to measure a real upgrade. Every fixture consumer validates table/PM
cardinalities, the private-card distribution, FTS and populated series. The two
authorization hotfix regressions remain separate: their established common-read
fixture and 1,000-root denial graph retain their 25-sample latency, privacy and
SQL prepare-size/time budgets. Bulk construction takes a few minutes on a laptop
with all canonical triggers enabled, rather than issuing thousands of HTTP writes; there is no persisted cross-run cache that can conceal
schema or trigger changes. New targeted tests should call the same generator,
then add valid point records through store APIs and measure only the request.

The schema-65 external-evidence projection is sparse in this generic corpus,
and its inbox ask page is not a dense ask-page fixture. These results do not
certify dense external-evidence alias fanout or every composed inbox selector.
Keep the focused source-reference and inbox privacy/bounds regressions; add
populated high-fanout fixtures when changing those reads.

## Heartbeat attention summary

The heartbeat's per-principal due/overdue counts never evaluate access or
materialize the ask ledger on the heartbeat path. `principal_ask_readability`
holds one row per notified principal and open ask, refreshed by the escalation
clock under fixed budgets (1000 access checks and about 2000 inserted rows per
step). Readability rows carry each ask's deadline and escalation state, so the
heartbeat computes each principal's exact due, overdue and earliest deadline
as uncapped index-only aggregates (two covering ranges and one seek), for at
most 200 principals. Run the gate with 200 humans ×
2000 open asks:

```sh
cd core
ANX_PERFORMANCE_TEST=1 go test ./internal/delivery -run '^TestPerformanceAttentionSummaryBudget$' -count=1 -v
```

It fails if the heartbeat evaluates any access check, exceeds 4 indexed queries
per principal plus 20, scans either table, reports any principal's due,
overdue or earliest deadline differently from the fixture (each principal can
read more than 1000 asks), or if one refresh step exceeds its access-check or
statement budget.

## Route coverage and SQL plans

`internal/server/testdata/resource_access_routes.json` is the authority for route
coverage. Every entry must have either a budget in `performance_routes.json` or
an explicit mutation reason in `performance_write_routes.json`. Read-only POSTs
(ref resolution, report previews, PM turn context and secret reveals) have budgets too. A new or
stale entry fails in the short tier; GETs cannot be classified as mutations.

Each route uses an owner (or the appropriate agent) and an unrelated principal.
Point fixtures require successful authorized responses; denial expectations
follow each route's public/private semantics. Enrollment polling separately
requires its poll credential. Series responses must contain populated data, and
selected collection responses must contain known records. Adapter inventory requires an explicit series grant (403 for the unrelated
principal); public series metadata/query and PM collections retain their
existing filtered-200 semantics. These are representative
selectors (including unfiltered, large-history and resumed event streams, a one-row
target-board page and a populated one-card ref source), not every possible route filter or a replacement for privacy
tests. The target-board page requires a positive owner result and an explicitly
empty unrelated-reader result. It does not certify default unpaginated board/ref
hydration, whose existing unrelated-reader overrun is tracked in SCA-665. The
one-card ref-source selector likewise does not certify a public source with
thousands of edges; that fanout overrun remains tracked in the same P1.
Secret listing/reveal operations use an encrypted positive fixture and audit
writes. The preview uses a populated fleet-health panel; saved reports cover live activity.
Secret cardinality and document/card revision histories are shallow;
those fanout dimensions need dedicated scale fixtures when changed.

Each case/principal executes one independently isolated first read, five warm
reads, and one read after invalidation of a previously warmed pool. The local
unpartitioned matrix uses the same pool for all seven. CI uses the separately
measured preparation described below and records eight requests per subject.
The warm median must meet the 500 ms ordinary latency reference; nearest-rank
p95 and the maximum are reported separately. A loose secondary maximum (four
times the reference, at least five seconds) bounds stalls and supplies deadlines.
The single first-read and invalidation samples primarily enforce deterministic
statement, row and VM ceilings, with that same loose secondary maximum rather
than a one-sample median. Their finite, source-pinned allowances have separate
cache-phase keys and never widen warm ceilings. All seven requests retain
status, privacy, positive-control and exact-plan assertions.

The four route shards keep both principals and all cache states of each case
together. `performance_shard_weights.json` records measured execution weights;
deterministic longest-processing-time balancing validates exact inventory
coverage before fixture setup. Select a local shard with
`ANX_PERFORMANCE_SHARD=1` (values 1–4); omitting it runs the full matrix. Every
shard uploads its actual executed report. `scripts/check-performance-shards.py`
requires four distinct, current-source reports, all 109+ cases, both principals,
exactly five warm and one first-read/invalidation samples, plus the measured
post-invalidation preparation in the correct order, valid metrics,
effective per-state limits, and successful case completions. Missing, duplicate,
stale, partial and zero-test reports fail. The separate legacy job retains both
25-sample authorization regressions, prepare-size/time checks, cache controls,
native-counter controls and startup measurements without repeating them in each
route shard. `TestResourceAccessCommonReadPerformance` logs its wall-clock
overruns as advisory measurements; SQL and row bounds still fail. The route
matrix retains fatal SQL, row, VM-work, response and privacy checks.

Each CI shard compiles once and runs three independent workers on
the standard four-CPU `ubuntu-24.04-arm` runner. Two workers retain the established `GOMAXPROCS=2`
setting and partition whole cases for the first read and five warm requests.
The third uses `GOMAXPROCS=1` and lower scheduling priority to reduce competition
with warm requests' garbage collection. It runs every case's
post-invalidation request on its own pool, first capturing and validating its
fresh cache-warming request against the first-read limits, then performing the
real epoch-changing write. That extra preparation is measured in the artifact;
no costly cold setup is hidden. Both principals remain in the same shard, and
workers never share an authorization cache. `ANX_PERFORMANCE_WORKER=1`, `2` or
`3` selects a selected shard's worker for local iteration.
CI waits for all processes, preserves their exit results and logs, and merges
their reports without manufacturing successful case completions. The coverage
job verifies each sample's worker assignment as well as the full shard union.
Reports record and validate the actual architecture, CPU count and Go CPU settings. Worker 1 completes its authorized stage before worker 2 constructs its
fixture and completes its authorized stage. Both finish before denied reads.
The post worker waits before constructing its fixture, preventing heavy denial
setup and GC from competing with owner timing checks. Atomic markers include
source, worker, completion time and stage outcome; failure releases peers with
its failed outcome intact. Waits are bounded by the test deadline, recorded
in artifacts and excluded from request timings. Coverage checks actual request
and fixture timestamps against every stage release. CI clears markers before launch.
Measured weights guide balancing; actual CI timings remain the runtime evidence,
since an indivisible slow case can dominate one worker.

Every read has an ordinary 50,000-VM-instruction ceiling alongside 100 statements
and 1,024 returned rows. `sqlite3_stmt_status` counters include indexed aggregate
work, prepared statements, transactions, early close and errors; fullscan, sort
and autoindex work are also recorded. A result with one row can execute thousands
of instructions. Native per-statement overflow, unsupported driver layouts and automatic
schema reprepare without a reliable trace lifetime fail closed. The route
fixture schema stays fixed during measurement. Cached FTS/internal statement
counters reset per execution; nested callbacks preserve parent guards and
capture epochs, so earlier executions cannot inflate a later sample. The test-only adapter validates the pinned modernc connection and
rows fields and uses its generated SQLite API; it does not alter production
connections. VM instructions are a work proxy, not bytes processed by Go UDFs,
so plan checks and the secondary timer remain necessary. See the
[SQLite counter definitions](https://www.sqlite.org/c3ref/c_stmtstatus_counter.html).

Stream cases have independent IDs, headers and allowances. Event coverage
includes the existing selected thread, the unfiltered workspace history, a
4,096-event single thread, resumed history and an idle Last-Event-ID poll.
Inbox snapshot/resumed/idle polls and agent-change invalidations are exercised.
The canonical inbox endpoint is `/stream/inbox`; there is no `/inbox/stream`
alias. Fresh public and inherited-private controls distinguish two actual polls
from header readiness or replay. Event controls are deleted after measurement
with canonical triggers active so sample count does not grow the denial graph.
Network transport latency is not measured.

A wrapper around the canonical SQLite driver captures raw and authorization-
rewritten statements, including prepared and transactional operations. Every
unique SQL shape plus typed argument set is explained outside timing/capture.
This SQLite build enables STAT4: equality/range distributions and LIKE/GLOB
patterns can change a plan. Repeated executions still count toward query/row
budgets; identical SQL text is interned to avoid retaining thousands of copies
of the authorization compiler output. Targeted filter-value tests remain necessary.
Tables with at least 1,024 rows are discovered from the fixture. Plans fail on
large-table SCANs (including full covering-index scans), automatic indexes, aggregate/window SEARCH inputs, or a
registered custom function in WHERE over a large relation. Aggregate/window names come from SQLite’s actual function registry, including MIN/MAX expressions and JSON aggregates. Referenced view definitions are expanded recursively for analysis, so a view cannot hide the aggregate or its input alias. Their SEARCH findings are conservative across nested SQL; bounded optimized cases need exact review rather than a wildcard. Alias handling includes
quoted and schema-qualified identifiers. Classification is conservative; SQLite
owns execution and its actual plan, and this lexer is not a general SQL parser.

`performance_plan_allowlist.json` identifies an exact SQL-shape SHA-256 (including
recursively referenced view definitions) and
specific EXPLAIN findings and the full ordered plan fingerprint (including
duplicate nodes), with a justification and link to the existing P1 issue.
The fingerprint preserves all SQL except the numeric snapshot epochs emitted
by the authorization compiler: the two gates of a fresh closure and the single
gate that opens a point-snapshot or pinned-denial statement. Those are data and
change with fixture writes; each route shard reaches different epochs, so an
exact epoch made the same shape unreviewable across shards.
Quoted business literals and comments are preserved. Changing a view's bounds
expires its fingerprint even when submitted SQL and EXPLAIN stay identical;
unrelated view definitions do not affect it. Actual SQL is always used
for EXPLAIN; normalization never changes execution. New SQL or findings require
reviewing a new entry. There are no table-wide or route-wide plan exemptions.

The two compiler gates must use the same internal epoch table and value.
Receipt snapshots use `receipt_access_epoch`; ordinary snapshots use
`resource_access_epoch`. The table name stays in the fingerprint. Mixed tables
or additional gates retain their exact literals.

The scoped Overview baseline SELECT is one such admitted point-snapshot read:
its single epoch gate is normalized by the same rule. Its business SQL and
scope literals, referenced views, findings and the full ordered plan remain
exact.

Receipt-stream cases retain the private-thread 200/404 case and add a public
thread for both principals. Initial and fresh payloads must have valid receipt
IDs and digests; comments and empty flushes cannot complete a receipt sample.
Two fixed control records change only delivery timestamps between polls, keeping
receipt ancestry and cardinality stable across five warm requests. The subject
cleanup runs after all seven requests. The independent cache control verifies
cold acquisition, warm reuse, stale-candidate validation after a trigger-backed
write, and acquisition through a second fresh pool. Both authorization epochs
must advance for post-invalidation receipt requests. Sparse receipt cardinality
does not certify the metadata query's full receipt-table scan or dense ancestry;
its exact plan exception states that limitation under SCA-665.

`performance_budget_allowlist.json` records finite baselines for existing hazards
on main, per method/path/case/principal/cache phase, with linked repair justification. Standard budgets
remain the default for every new route. Baselines retain query/row ceilings,
success/denial expectations, positive fixtures, private controls and two SSE data
flushes; exceeding a ceiling still fails. Their purpose is to allow the guardrail
harness to land while SCA-663/664/665/673 and the existing SCA-652 repairs proceed,
not to authorize new O(workspace) work. The merged #302 repairs retired the
previous broad PM/overview exceptions. Remaining entries are remeasured with
the expanded stream fixture and SQLite counters, and retain only dimensions
that exceed ordinary limits. The
ordinary limits stay at 500 ms, 100 SQL executions and 1,024 returned rows
(except the existing fixed series-observation cap).

Completed merged-code measurements replace the former threefold latency
allowances and identity-cache refresh extrapolation. Latency headroom is 20%,
rounded up to 50 ms; overrun counts get 2% headroom with a minimum of eight SQL
executions, 64 returned rows or 1,000 VM instructions. Dimensions below the
ordinary limit use that limit. First
and returning overview visits remain separately sampled. Legacy PM response
projection still issues 159 SQL for actions and up to 164 for decisions; identity
cache refresh is measured directly, rather than extrapolating a multiplier.
Completed denied PM/overview probes take seconds even after indexed principal
lookup. These are existing main hazards linked to SCA-663, not permission to
add repeated lookups. Diagnostic probes provide completed baseline evidence
but intentionally fail; normal mode and CI must independently pass status,
privacy, count, stream and exact-plan checks.

Round-two isolation preserves all previously reviewed warm ceilings numerically.
Cold and post-invalidation caps are measured separately and cannot widen warm
assertions. Shard weights combine prior CI measurements with the initial public
receipt probe; weights are scheduling estimates, never request allowances.

Every remaining route exception carries `core_source_sha256`: a fingerprint of core runtime
Go, module dependencies, local replacement modules, fixture/performance-harness
code, inventories and relevant text assets. Release-version metadata, unrelated
tests and the self-referential budget manifest are excluded. A changed or added
input expires the allowance in the advisory scale tier before corpus construction;
remeasure the linked P1 instead of copying the hash automatically. To remeasure,
dispatch the workflow at the current source, download every
`core-performance-routes-N` artifact, and run
`scripts/refresh-performance-allowances.py <dir> --write --summary <file>`. It
re-pins only entries whose measurement fits their reviewed ceiling, tightens each
to the measurement plus the headroom below (never above the old ceiling), retires
entries the ordinary budget covers, and leaves the rest expired for review. The
coverage checker recomputes the same pin in Python;
`TestPerformanceRuntimeSourcePinMatchesShardChecker` keeps the two walkers
identical. The cheap
inventory check always requires valid source hashes and finite exact entries,
but freshness does not gate ordinary CI. All other
routes retain their ordinary count ceilings. Remove entries as those repairs land.
A changed read must satisfy the ordinary budget; do not add a baseline for a new
regression. Populate new large record families and high-fanout selectors when
adding endpoints; empty tables and shallow histories are not scale evidence.

Set `ANX_PERFORMANCE_REPORT` to an absolute writable JSON path to collect
synthetic SQL shapes/findings and route samples for review. CI uploads each report as
`core-performance-routes-N`, so reviewers can inspect every shard's plans.
`ANX_PERFORMANCE_DIAGNOSTIC=1`
retains all seven state-specific samples and extends the request deadline to sixty minutes to
observe baseline counts; it always fails as a diagnostic run, and
latency/query/row/work and plan failures still fail. An exact
`ANX_PERFORMANCE_DIAGNOSTIC_ROUTE="METHOD /path"` selector can isolate one
diagnostic route; append ` [case-id]` to select one stream case. Setting it in acceptance mode fails before fixture setup. CI
never enables either diagnostic option.

## Startup and migration readiness

### First authenticated reads after restart

SCA-720 isolates `/inbox` and `/overview` with the analyzed 4,096-record
fixture, a fresh SQLite pool and a complete native handler for each route and
principal. The first request constructs the epoch-validated authorization
closure; the second reuses it. No other route warms that pool. Fixture creation
and migrations are outside the request CPU profile:

```sh
cd core
ANX_PERFORMANCE_TEST=1 ANX_COLD_START_PROFILE="$PWD/cold-start.pprof" \
  go test ./internal/server -run '^TestPerformanceColdStartReads$' -count=1 -v
go tool pprof -top -cum -focus=withRequestAccessEpoch cold-start.pprof
ANX_PERFORMANCE_TEST=1 ANX_PERFORMANCE_DIAGNOSTIC=1 \
  ANX_PERFORMANCE_DIAGNOSTIC_ROUTE='GET /inbox' \
  ANX_PERFORMANCE_REPORT="$PWD/inbox-diagnostic.json" \
  go test ./internal/server -run '^TestPerformanceRoutes$' -count=1 -v
```

Repeat the diagnostic with `GET /overview`. Diagnostic mode intentionally
fails and still reports existing budget and exact-plan findings; it does not
replace acceptance tests or refresh source-pinned exceptions.

On a local Apple Silicon host, before/after diagnostic samples were:

| Route / reader           | First before | Second before | First after | Second after |
| ------------------------ | -----------: | ------------: | ----------: | -----------: |
| Inbox / owner            |        68 ms |         49 ms |       56 ms |        50 ms |
| Inbox / denied reader    |       651 ms |         77 ms |      207 ms |        77 ms |
| Overview / owner         |       123 ms |        105 ms |      107 ms |       104 ms |
| Overview / denied reader |       843 ms |        270 ms |      399 ms |       271 ms |

The first denial closure dominated the additional cold work. SQLite's analyzed
statistics for mostly NULL `cards.parent_thread_id` and
`agent_wakeups.trigger_event_id` chose a table scan for each denied vertex.
Source-URL traversal also reordered the join to scan source-owned metadata
before the recursive vertex. Retaining the existing parent indexes and fencing
the metadata lookup behind that vertex removed those scans without changing
the closure or its invalidation policy. Inbox's cold-minus-second full-scan
steps fell from 1,189,826 to 3,192 for the denied reader; VM overhead fell from
5,982,614 to 1,243,478. The regression checks actual closure plans and cold work,
separately from existing collection and overview-visit baselines.

No new cache, migration, startup warm-up or health behavior is required for
this query-plan repair. Each changed recursive probe costs an index lookup
plus its matching descendants: `idx_cards_parent_thread_id`,
`idx_wakeups_access_trigger_event`, and the `work_metadata.card_id` primary key,
followed by the existing exact/external target-key indexes. It no longer scans
unrelated cards, wakeups or source-owned metadata per vertex. The existing
denied closure still scales with denied roots and reachable identities; this
does not retire all SCA-663 authorization or collection exceptions.

The compact-ledger cutover (#381) doubled every exact-edge recursive arm, one
per ledger, each gated by a control-row subquery that SQLite tests only after
the arm's probe. On this fixture's legacy ledger the cold denied closure grew
from about 1.24M to 1,785,428 VM instructions and failed the 1.5M overhead
bound. The closure now carries the root's ledger selection on each vertex
(1,517,582 with both arms), and request captures compile only the ledger their
pool (or, for a new pool, the process) last observed, verified inside the
capturing statement: 1,330,391, with the same denied set and no extra query.
The residual over the pre-cutover closure is fixture growth, not ledger work.

These fresh-pool samples keep the OS file cache warm and do not establish a
physical cold-disk or hosted-network bound. They do not reproduce the reported
hosted 6–8 seconds. A deployment warm-up should only be proposed after measuring
the remaining delay in that environment; synthetic timings cannot identify an
OS page-cache miss or a specific maintenance writer there.

The startup gate builds an actual schema-58 workspace, measures upgrading through
all current migrations with a 90-second budget, then measures a warm open with
a five-second budget. It includes production store construction and a database
ping. Main #305 removed historical blob backfill from startup; explicit blob
maintenance is outside readiness, and unindexed blobs remain fail-closed.
A failed upgrade does not proceed to the
warm-open measurement. This is a database/store readiness proxy; deployment and
network readiness checks require their own integration coverage.

Main #275's batched source-edge reconciliation removes the measured legacy
upgrade overrun. The former fifteen-minute
SCA-664 exception is retired: `performance_startup_allowlist.json` is `null`, so
CI enforces the ordinary 90-second upgrade and five-second warm-open budgets.
If a legacy exception is ever needed, it must be finite, linked to a reviewed P1
and pinned to the current migration version and exact startup/index/backfill
source hashes. Changes expire it in the short tier. Exceptions do not relax
deployment probes automatically; adapters own any bounded probe grace.

Migrations emit `migration_started`, elapsed `migration_progress` every five
seconds, and `migration_finished`. The final signal means work stopped; successful
commit and the absence of an initialization error establish success. No record
contents are logged. Migrations still run transactionally before the listener
opens; progress does not make an unmigrated database ready. Probe supervisors
should permit an upgrade-specific startup grace period while progress advances,
then require normal readiness. They must still bound a stalled upgrade, preserve
logs, and distinguish process exit or errors from continuing migration work.
The generic core supplies signals; deployment-specific probe policy belongs in
the deployment adapter, outside core.

## Sweep findings and current baseline

The authorization repair (#294/#298) is now the base of the harness. Existing
projection, query-plan and startup hazards are tracked separately; the checked-in
exceptions link their P1 issues and cap the current behavior. This is a baseline,
not proof that every reader is bounded. The following independently visible hazards require remedies that preserve
privacy, legacy data and response contracts:

| Severity | Source                                                                                              | Affected reads                          | Cost before a request/page bound                                                                                                                                                                                                                                       |
| -------- | --------------------------------------------------------------------------------------------------- | --------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1       | `internal/primitives/inbox_freshness.go:7`, `internal/primitives/inbox_reads.go:39`                 | Inbox freshness and summary             | Aggregate freshness still scans scoped thread candidates, with view/status joins. Summary counts and JSON-kind/lifecycle predicates inspect the corpus; output pagination does not bound candidates. #302 removes full page materialization.                           |
| Repaired | `internal/server/event_stream_scanner.go`                                                           | Default and large-history event streams | #311 bounds history traversal and starts an unfiltered new connection at HEAD. Canonical authorization and other stream work retain separate SCA-665 allowances where measured.                                                                                        |
| P1       | `internal/server/stream_handlers.go:261`, `:491`, `:349`                                            | Inbox, events and receipt streams       | Repeated freshness/history and subject work per tick remains. #302's internal 100-record inbox sampling also has a completeness regression, owned by the SCA-665 forward fix; representative two-tick tests do not certify complete feeds.                             |
| P1       | `internal/primitives/docs_store.go:120`, `:135`                                                     | Document point/history reads            | TRIM/COALESCE event joins inspect events rather than using direct indexed equality.                                                                                                                                                                                    |
| P1       | `internal/primitives/docs_store.go:1480`                                                            | Document revisions                      | Unpaginated revision history has queries per revision, O(R) round trips. The generic fixture has shallow histories.                                                                                                                                                    |
| P1       | `internal/primitives/docs_knowledge.go:327`, `:335`, `:372`                                         | Document search                         | Matching-corpus ranking and correlated event scans can grow with matches and event history.                                                                                                                                                                            |
| P1       | `internal/server/pm_runtime.go:310`, `internal/pm/resolution.go:48`, `internal/pm/decisions.go:622` | PM actions and decisions                | Legacy delivery-authority projection resolves work per returned record despite batched snapshots. Measured action/decision requests execute 159/164 SQL. #302's HTTP page selectors and indexed principal lookup retire the prior exhaustion/cache-refresh allowances. |
| P1       | `internal/pm/store.go:101`                                                                          | Internal PM list consumers              | Internal `listRecords` still accumulates 200-row windows to exhaustion; bounded HTTP pages bypass it. Do not apply this finding to the repaired HTTP page loaders.                                                                                                     |
| P1       | `internal/auth/hosts.go:738`, `internal/primitives/store.go:1905`                                   | Host enrichment, thread list            | Indexed host selection still hydrates an agent directory; thread lists retain per-thread subject/summary hydration. Selection indexes do not bound those downstream costs.                                                                                             |
| P1       | `internal/primitives/access_scope.go:56`, `internal/primitives/overview_changes.go:76`              | Fresh transaction/denied reads          | Fresh canonical denial closure and candidate filtering remain costly. Snapshot fallback executes on an epoch miss; cached denied/reference membership still executes on a hit.                                                                                         |
| P1       | `internal/storage/workspace.go:83`, `internal/storage/migrations.go:1018`                           | Upgrade and startup                     | Remaining reconciliation grows with corpus before readiness. #275 batches source edges and #305 removes historical blob reads; ordinary startup budgets now pass without an exception.                                                                                 |

#302 also repairs work selection before metadata/projection (`work_store.go:905`),
page-before-enrichment inbox loading (`derived_store.go:167`), bounded PM/overview
loaders, principal lookup and batched admin host lookup. Their former full-load,
N+1 and clock-refresh descriptions no longer apply to these paths.

CLI `internal/app/daily_loop.go:86` requests `/work?limit=200`: the client makes a
bounded request and now inherits #302's selection-before-projection bound. Work commands
carry limits/cursors through. No additional client-side unbounded pagination loop
was found in this sweep. Web UI overview server load returns an empty bootstrap;
business data loads use core. `src/lib/server/authSession.js:805` makes a fixed
`/agents/me` request, and layout/resolver loads do fixed configuration/auth work.
No additional workspace-corpus scan was found in those server loads. This is a
source sweep, not proof that every consumer or filter is bounded.

## Review prompt to adopt

For each changed read and stream tick, trace the complete request through
authorization, SQL, projection, enrichment and pagination. State its complexity,
maximum selector/page cardinality, and the indexes used before filtering or
LIMIT. Inspect the scale result and actual EXPLAIN plans, including denied reads.
Treat any hot-path O(workspace) scan, graph rebuild, read-time JSON/text extraction,
materialization-before-limit, or N+1 loop as a merge-blocking P1. Require a bounded
implementation and meaningful scale evidence; do not accept a future follow-up
as mitigation for new work. Existing main exceptions must remain exact, finite
and linked to the P1 repair; do not broaden them for a changed hot path. Review startup/backfill cost and advancing progress separately
from readiness. Check fixture coverage, successful point selectors, second SSE
ticks, and narrowly justified SQL exceptions before trusting a green result.

Inbox ask staleness enrichment uses only authorized response-page subject refs,
in batches of at most 200. Card IDs, `idx_cards_handle_unique` (with its partial
predicate), and the unique identity `(origin,origin_id,kind,resource_id,ref)` index resolve alias routing keys
before timestamp hydration. The new projection performs O(page) indexed probes
in one SQL batch per 200 distinct subjects, independently of workspace size.
`TestPerformanceInboxAskStalenessBudgetAndPlans` measures scoped projection cost
on the scale fixture; `TestInboxAskStalenessPageBoundAndQueryPlan` checks fixed
page queries as the corpus grows and certifies all three lookup indexes.
The exact scoped SQL/plan exception covers the conservative custom-function
heuristic: page-value normalization and O(1) pinned denial membership occur only
on indexed candidate lookups. It permits no scans or budget increases; the
200-subject owner/stranger probes measured one SQL execution and at most 8,685 VM
instructions.

## Claimed PM card and workspace reads (SCA-733)

`POST /pm/turns/{id}/context` validates the selected PM and lease, then binds
the conversation requester and pins one epoch-validated read snapshot. Pinned
reads admit at most eight refs; a full card read admits one requester-visible ref,
including an unpinned plan step. Batches cover aliases, card bodies, metadata,
plans, and six candidates each for asks, activity and awaiting decisions per card.
Hidden candidates do not refill.

Without pins, the same route returns a workspace overview: up to 15 open
initiative cards, ordered by blocked/review/active/ready/backlog attention then
recency, plus up to 15 requesting-reader asks, awaiting decisions and recent
events. Routing indexes admit 64 card IDs, two 16-ID recipient ask windows,
16 reader decision IDs and 32 event IDs before canonical scoped hydration.
All partial markers are unconditional; no hidden candidate counts are serialized.
Selection and projection cost depends on these fixed windows and bounded plan
fanout, not a workspace scan. The shared denial snapshot has the inherited
SCA-665 cold admission cost and retains epoch fallback.

`TestPerformancePMCardContext` certifies full HTTP reads on 4096 cards with
owner/stranger, one/eight pins and all fifteen overview slots filled, dense
private messages/asks and private decision evidence. The card and workspace
budgets are separate checked-in JSON files: pinned reads allow 30 SQL calls,
256 rows and 20,000 warm VM steps; the fifteen-card workspace overview allows
30 SQL calls, 512 rows and 40,000 warm VM steps. Both keep the inherited
2,000,000 cold VM admission ceiling. Exact indexed-window query plans
are recorded under SCA-733. Run:

```sh
GOMAXPROCS=2 GOFLAGS=-p=1 ANX_PERFORMANCE_TEST=1 go test ./internal/server \
  -run '^TestPerformancePMCardContext$' -count=1 -timeout=4m -v
```

## Handled inbox ref projection (SCA-780)

The handled inbox admits one immutable resource-read snapshot, selects at most
100 response events and resolves their presentation refs in batches of at most
200 distinct IDs per resource type. Original request events use one indexed ID
or handle batch, followed by the same scoped ref projection. Generic
`publicRefsIn*` and typed-list projections batch each response value; nested
projections reuse an immutable presentation map. Hidden or missing IDs retain
raw typed refs. Rebinding access scope discards presentation handles.

Ref hydration costs O(page refs), using canonical primary keys (and event handle
indexes for original requests), independently of unrelated event history. Each
200-ID batch compiles one scoped statement. Canonical inherited denial admission
still depends on denied roots and reachable identities, with the existing bounded
shared epoch cache. Admission validates the epoch and conditionally captures a
stale closure in one SQL statement, retaining the existing query budgets across
workspace writes. Inside the admitted transaction, point membership uses the
existing fail-closed token rather than recompiling the recursive graph for every
ref lookup. Epoch misses recompute the full canonical policy. Ordinary unpinned
and mutation reads retain canonical fallback. Report rendering retains its
ordinary scoped reads and response-level ref batches, without a new snapshot
admission round trip.

The modernc driver keeps SQL text in `sql.Stmt`, then calls native
`sqlite3_prepare_v2` on every execution. A Go prepared-statement cache therefore
cannot remove native parse cost. This repair reduces the number of statements
and the SQL compiled in the read snapshot instead.

Run the dedicated scale guards:

```sh
cd core
GOMAXPROCS=2 ANX_PERFORMANCE_TEST=1 go test ./internal/server \
  -run '^TestPerformanceHandledInboxRefsAndConcurrentAnswer$' -count=1 -timeout=5m -v
GOMAXPROCS=2 ANX_PERFORMANCE_TEST=1 go test ./internal/storage \
  -run '^TestPerformanceCompactAccess(OnlineMigration|Shadow)$' -count=1 -timeout=10m -v
```

The HTTP guard builds more than 4096 ref-bearing events, a large synthetic
workspace and 50 handled responses with 500 distinct resource refs. It allows
25 native read preparations and two seconds per measured GET, and requires an
answer POST plus idempotent replay to succeed during two finite polling workers.
All workers finish inside the test. The guard fails on the pre-repair handled
reader's repeated ref lookups. Timing is local CPU/storage evidence, not a hosted
network SLA. Budgets live in `internal/server/testdata/handled_inbox_budget.json`.
On one Apple Silicon run with `GOMAXPROCS=2`, the same roughly 363 MB fixture
measured 2,206 application read preparations and 4.43–4.83 seconds per GET
on the pre-repair main, versus 17 preparations and 6.2–18.4 ms after repair.
Authentication and actor reads use the same instrumented pool. Both baseline polling workers
hit their ten-second client timeout; the repaired workers completed. Answer POST
latency under polling fell from 949 ms to 210 ms. These samples include the
existing production immediate writer configuration in both revisions.

Compact shadow installation performs DDL only, but formerly received the same
100 ms deadline as copying one maintenance chunk. Repeated installation rollback
cannot benefit from the chunk-size adaptation. Installation now has a bounded
five-second deadline; backfills retain their adaptive 100 ms writer leases.
Worker errors report fixed stage/reason labels and numeric SQLite codes without
SQL or resource contents. Verification mismatch labels name the ledger direction;
operators should preserve the legacy reader and run offline verification before
repairing inconsistent ledgers. Deadline reports identify installation versus
backfill; write-contention reports identify retryable writer pressure. No
production workspace is required or inspected by these guards.

Answer persistence retains the production WAL, 20-second per-connection busy
handler and immediate write transactions. Immediate locking queues writers at
transaction admission instead of attempting an unsupported deferred read-to-write
upgrade. The HTTP scale guard covers the full response, persisted replay and
single committed claim under read load.

Compact certification gets a synchronous preparation phase before the HTTP
listener and other maintenance loops start. That phase has a ten-second budget:
it installs transactional mirrors, copies without inter-batch pauses, and
certifies if the full verification fits. Incomplete durable progress resumes
online within the remaining ten-minute overall startup budget. A complete
shadow is recognized from its job and every mirror/generation trigger, so the
online worker need not win the writer again just to reinstall it.

Compact maintenance uses a 20 ms busy wait to catch a writer handoff. Actual
SQLite BUSY/LOCKED failures use exponential equal-jitter backoff (initial
125–250 ms, capped at 2.5–5 seconds). Expired adaptive slices and changed proofs
retry in the next short slice. Successful copying batches yield 25 ms;
committed progress resets contention backoff. Transactions and pinned
connections are released before any wait or attempt observer runs. Serving
connections retain their normal busy handlers.

Install and cutover have one-second deadlines; copying retains adaptive 100 ms
leases. Cutover atomically verifies epoch/generation, renames the certified raw
ledger, recreates its triggers and publishes the reader. It does not drop the
populated retired table. Separate cleanup drains at most 200 retired rowids per
adaptive 100 ms lease, then drops the empty table. Cleanup resumes after restart,
never changes certification/authorization, and the retired ledger is excluded
from serving reads. Its three internal authorization columns are classified in
the storage inventory. Full byte-for-byte certification verification is unchanged.
One final safe outcome is logged for the combined preparation/online run.

Run the held-writer, exhaustion, adaptive-deadline, preparation and retired
cleanup regressions, plus the deterministic answer guard:

```sh
go test -C core ./internal/storage -run 'TestCompact|TestAdaptive' -count=1
go test -C core ./internal/server -run '^TestCompactMigrationConcurrentAnswers$' -count=1 -timeout=2m
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 go test -C core ./internal/storage \
  -run '^TestPerformanceCompactAccessPreServeStartup$' -count=1 -timeout=15m -v
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 go test -C core ./internal/server \
  -run '^TestPerformanceCompactMigrationConcurrentAnswers$' -count=1 -timeout=15m -v
```

The HTTP guard observes the first BUSY installation attempt before releasing a
held writer. SQLite gates then hold install and cutover until a fresh answer
reaches its serving BEGIN IMMEDIATE admission. It sends 25 fresh answer POSTs
and enforces a two-second **maximum** sample budget, so a single slow writer
lease cannot hide outside p95. Overlap uses those gates rather than fixed sleeps
or a random backoff window. The scale tests use the shared canonical synthetic
corpus with real ownership/reference indexes and report physical file bytes,
pre-serving duration, certification time, background writes and answer maximum.

## Card metric references and waiting ages

Series metric enrichment admits eight refs per card and 200 distinct refs per
page. One scoped SQL query uses `json_each` of the admitted refs, primary-key
lookups on `series_definitions(name)` and `series_adapters(name)`, at most 100
registered label sets per ref through `series_labels(series,labels)`, and two
endpoint seeks per matching stream through
`series_points(series,labels,ts)`. It admits at most two matching streams to
recognize ambiguity; no history aggregates or per-ref statements. Ranges cap
at raw retention (90 days) so first/latest points are exact. Plan fact batches
use definition primary-key lookups without observation hydration.

Ask links use `work_summary_asks(subject_ref,recipient_actor_id,trigger_at,inbox_id)`
with at most eleven candidates per card identity and audience, joined back to
the scoped `derived_inbox_items` primary key. Related refs are indexed at write
time; the read checks only each bounded candidate's refs. Migration 89 changes
triggers and resets the existing 64-row maintenance cursor, with no startup
history scan. Sorting needs-you rows costs O(page log page) over existing rows
and adds no query.

Verify the metric endpoint budget (100 streams, 10,000 observations, four refs;
one query, four result rows, at most 20,000 VM steps and indexed point seeks):

```sh
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/series \
  -run '^TestPerformanceSeriesRefEndpointBudget$' -count=1 -v
```

The canonical `series_points` privacy shadow is explicitly `NOT MATERIALIZED`
so oldest/latest endpoint probes retain their primary-key seeks rather than
copying the workspace's points. The scoped-reader regression uses the real
nonempty denial graph, captures the rewritten SQL, rejects a materialized
point shadow or point scan, and requires both endpoint index seeks. Against
100 streams and 10,000 observations, one scoped metric must stay within eight
queries, sixteen result rows, and 20,000 VM steps (authorization admission is
outside the measured read phase):

```sh
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/series \
  -run '^TestPerformanceScopedSeriesRefEndpointBudget$' -count=1 -v
```

## Report activity enrichment

`GET /docs/{document_id}/report` and `POST /reports/preview` share one
request-local reader and retain `maxReportCandidates = 200`. Activity admission
includes asks/responses, card creation/update/movement/resolution, board metadata,
document/topic changes and promise notes (`message_posted.next`). Plain messages
are dropped inside this fixed candidate window; filters never refill past it.
The `recent-changes` template admits 15 high-impact rows from that history rather
than increasing the candidate budget or issuing a workspace history scan.

Subject title hydration reuses the `refs/resolve` fact projection in a
presentation-only mode, omitting movement, due-date and message-history reads.
At most 200 distinct subjects are resolved per batch. Thread-only promise notes
find at most two owners with `idx_cards_thread_id` (or its equivalent `idx_cards_access_thread`); ambiguity stays unresolved.
Card IDs/handles use their primary key and `idx_cards_handle_unique`; current
initiative classification joins board and plan primary keys for admitted card
IDs. Document/topic/board selectors retain the resolver's ID/handle indexes.
Actor display names use one scoped primary-key batch per 200 admitted IDs.
There are no added per-item queries or shared reader caches. Inherited privacy
admission remains covered by the existing report HTTP scale gates. Enrichment
cost grows with the fixed candidate page, independently of unrelated history.
Coalescing/sorting runs over the bounded page; `min_impact` filters after
coalescing and before the response limit. Highest impact wins; equal-impact ties keep the newest verb, summary and
detail. Bursts remain anchored within 30 minutes of the newest event.

The maximum-page scale guard runs owner/stranger reads with card refs and
thread-only refs on the canonical 4,096-card fixture. Its checked-in incremental
budget is `internal/primitives/testdata/report_activity_budget.json`; it captures
SQL calls, returned rows and VM work after ordinary read admission and verifies
the thread-owner lookup index. No existing budget or plan exception is widened.

```sh
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 go test -C core ./internal/primitives \
  -run '^TestPerformanceReportActivitySubjectsBudgetAndPlans$' -count=1 -v -timeout=6m
```

## Initiative loop receipts

Loop enrichment admits the existing brief page (at most 50 cards) with one
primary-key card/work-metadata batch and indexed receipt-existence probes.
Migration 100 materializes per-card receipt facts beside `brief_initiatives`.
`brief_dirty` invalidates insert/update/delete receipt changes and work metadata;
maintenance folds at most 200 queued cards per transaction. Indexed expiry seeks
queue facts at the next 7/14/30-day window transition or future receipt activation.
The reader computes elapsed durations and health at request time.

The serving `brief_loop_facts` view uses primary-key card lookups. Its synthetic
ownership node inherits all contributing receipts; it does not restrict the
canonical card. A private receipt denies reuse of the shared aggregate for that
reader. The backing JSON and validity columns are also masked. Denial epochs
apply immediately, independent of maintenance. Dirty, expired or unauthorized
cache entries fall back to the same reader-scoped receipt query; filtered results
never overwrite the shared canonical projection.

Maintenance/fallback use migration 99's `idx_events_run_subject_ts` and
`idx_events_run_effect_ts`; replay uses `idx_events_run_identity`. Their work
grows with the admitted subjects' receipt density inside 14 days for exact
counts and 30 days for directional incumbent bests, not unrelated workspace
history. Incumbent seeks and aggregates exclude rejected/reverted decisions;
one additional subject/time-indexed seek obtains the newest incumbent even
when all 60 displayed candidate points were rejected. Migration 101 queues
existing loop caches once so earlier candidate bests cannot survive the change.
Packed result detail remains bounded to 50 runs (brief renders 12) and 60 goal
points per card. Valid cached reads do not scan those receipt ranges. Goal
series selectors share the existing bounded daily-rollup batches. The ordinary
50-card/100-ref scale ceiling remains 50,000 VM steps for both principals.
The dense 10,000-receipt complete-brief gate now also enforces 50,000 VM steps
(previously 750,000), 30 SQL calls, 100 returned rows and a one-second safety cap.

`live-loops` admission uses migration 102's two hints on the existing
`brief_initiatives` rows, updated atomically by metadata and receipt triggers.
`idx_brief_loop_annotated_recent` traverses annotation metadata update times
(newest first), materialized atomically by migration 103;
`idx_brief_loop_recent` traverses last-receipt timestamps newer than 30 days.
Each arm returns at most 51 cards after scoped card, board, metadata and receipt
checks. The reader deduplicates at most 102 IDs and retains the 50 most recent
by annotation/receipt activity, enriches those loops and
sorts this cohort by health before applying the display limit (at most 48).
Ordinary cards never consume slots or require JSON scans. Candidate index work
can grow with loops excluded by board/lifecycle/visibility; receipt admission is
an indexed subject/time existence seek, with no receipt-density aggregation.
The hints are internal selectors, never response facts or authorization: a
private receipt alone cannot admit a public card, and mixed receipt visibility
uses the ordinary scoped receipt query. Stats describe the admitted cohort;
per-loop and aggregate `effects_7d` both count acted-or-kept runs once.

`TestLoopReportAdmissionBudget` measures the complete report over 4,096 ordinary
cards plus annotation/receipt loops, using `loop_report_budget.json` and requiring
both admission indexes. Owner and non-owner reads each measured four SQL calls,
six rows, 674 VM steps and zero full-scan steps. The existing complete-brief and
series budgets remain unchanged. Migration 102 performs one indexed per-card
backfill on upgrade; subsequent startup does no admission backfill.

`TestLoopBriefComparison` is portable to main: copy that exact test file into an
isolated main checkout. Every case seeds 10,000 otherwise-identical events on one
cadence/goal card. The zero-receipt control changes only the type to
`card_updated`. Setup and projection maintenance are excluded. Each sample uses
a fresh actor access scope and pinned resource read; native SQL instrumentation
is enabled in all cases. First latency and the median of 20 subsequent warm
reads are reported. These local synthetic results are not deployed latency claims.

Measured sequentially on this host (milliseconds; main at
`42fd88cb22be86e2185d93269b8d7dbbc682c77d`):

| Revision / receipt count                           |  First | Warm median (20 reads) |
| -------------------------------------------------- | -----: | ---------------------: |
| origin/main, 10,000 events                         | 16.117 |                  7.769 |
| branch, zero run_recorded (10,000 ordinary events) | 16.026 |                  8.314 |
| branch, 10,000 run_recorded                        | 16.412 |                  8.800 |

The dense loop increment is 0.485 ms over the branch control, 1.031 ms over main.

```sh
# Run sequentially on an idle host; the same file/workload on main first.
ANX_BRIEF_COMPARE=1 go test -C core ./internal/server -run '^TestLoopBriefComparison$' -count=1 -v -timeout=5m
ANX_BRIEF_COMPARE=1 ANX_BRIEF_BENCH_MODE=ordinary go test -C core ./internal/server -run '^TestLoopBriefComparison$' -count=1 -v -timeout=5m
go test -C core ./internal/server -run '^TestLoopBriefCost10KEvents$' -count=1 -v -timeout=5m
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/series -run '^TestPerformanceBriefSeriesBudget$' -count=1 -v -timeout=10m
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/storage -run '^TestPerformanceStartupAndMigrations$' -count=1 -v -timeout=10m
```

## Scoped curation promise reads

`GET /curation/metrics` retains its existing bounded window and row limits.
Open promises seek `idx_brief_initiatives_page` by `(open, promise_at)` and
join the reader-scoped card relation by primary key before aggregation and
pagination. The relation compiler removes this physical `INDEXED BY` hint when
`brief_initiatives` is a scoped CTE; SQLite still pushes the range into the
canonical table and uses that index. It must not materialize the entire
initiative relation. No loop facts or receipt ranges are loaded by this query.
The `brief_initiatives` shadow filters every column by card visibility, including
promise text, evidence times and loop admission hints. Loop aggregates retain
their additional contributor gate. `brief_notes` requires both card and source
event visibility; ask/block projections require ask visibility. All brief shadows
are `NOT MATERIALIZED` so the canonical index seeks precede filtering and limits.
Physical `idx_brief_*` hints are removed under scope, including the cleared-note
`idx_brief_notes_next_at` path. Direct promise-fold/note reads are safe without a
caller-supplied cards join.

Ask-block counts deduplicate at most two refs per admitted card before SQL.
Their query starts from this bounded ref batch and seeks
`idx_brief_ask_blocks_ref`, then asks/events/threads by primary key. Batch-member
reads likewise start from the indexed batch key. Explicit `CROSS JOIN` order
prevents the added projection visibility predicates from choosing a scan of all
open asks instead. The existing primary-key/index-plan test consumes the
production block-count SQL and retains its index and no-scan assertions.
The populated brief gate measures 41,643 VM steps for the owner and 49,593 for
the scoped reader against the unchanged 50,000 limit.

`TestCurationMetricsScopedPromiseIndex` checks live and request-cached scopes,
private-card exclusion, and the actual indexed query plan. The shared 50-card
brief scale gate retains its 50,000-VM-step ceiling. Ask and cleared-note reads
keep their existing type/time and next-at indexes and admission limits.

```sh
go test -C core ./internal/primitives -run '^Test(CurationMetricsScopedPromiseIndex|BriefProjectionRelationsFilterPrivateRows|LoopReportAdmissionRetainsRecentLoopsBeyondFifty)$' -count=1
go test -C core ./internal/server -run '^Test(ResourceAccessRouteMatrix|CurationMetricsLogsSQLErrorCause|BriefProjectionsPrivateChangesDoNotAffectOtherReaders)$' -count=1
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/series -run '^TestPerformanceBriefSeriesBudget$' -count=1 -v -timeout=10m
```

The generic curation route gate currently fails its existing authorization
budget on main as well as this branch. At main
`42fd88cb22be86e2185d93269b8d7dbbc682c77d`, the denied-reader warm path uses
66,461 VM steps; this fix uses 66,586 (six queries, three returned rows in both).
Post-invalidation work is 1,761,675 versus 1,761,800 VM steps. The inherited
closure/query-plan failures are recorded separately from the successful
index-plan regression and brief scale checks. No budget or plan allowance was
relaxed for this fix.

## Agent brief ownership and hidden history

Agent brief ownership admits at most 50 distinct card IDs. Assignment reuses the
already admitted card rows; `participatingInitiativeCards` uses
`idx_work_participants_actor_card_active(actor_id,card_id,session_id,id)` with
`activity <> 'left'`, nonempty actor and `session_active=1` before scoped
participant/session checks. Migration 105 backfills the normalized session flag
once and maintains it on participant insert/rebind and session close/reopen or
actor change. Session-side updates seek `idx_work_participants_session(session_id,id)`
and touch only that session's participants. Closed history never enters the
serving participation index. Installation is idempotent and retains canonical
participant activity.

The brief no longer probes globally for visible assignments, participants,
plans, notes, asks, blocks or work metadata before reading its candidates.
Notes seek `idx_brief_notes_card_ts` for the admitted cards; ask subjects and
blocks start from at most two distinct refs per admitted card and seek their
subject/ref indexes. Metadata and plan hydration use card primary keys.
Unrelated hidden rows cannot make an empty candidate result scan those tables.

Pinned series reads reuse the admitted immutable point proof for stream
identity checks. Empty label objects contain no authored references and skip
atom/prose extraction; arbitrary label JSON retains the canonical reference
policy. Unknown legacy provenance uses `(series,labels)` primary-key seeks.
The unknown-ledger empty check is an unscoped physical first-row seek on internal
authorization metadata, so hidden business rows cannot turn it into a scan.
Missing or expired proof tokens fail closed. Ordinary unpinned reads retain the
canonical policy, and the series differential regression compares both paths
for private labels, private historical state, unknown provenance, grants,
revocations, and shared proof-cache reuse.

`TestPerformanceBriefSeriesBudget` retains its 50,000 VM-step, 100-query and
1,024-row ceilings. It covers 10,000 reader-owned open sessions, 10,000 closed
sessions with still-active participants, a full assigned page, and 1,000 notes
on an unrelated private card. All variants include 50 initiatives, 100 admitted
metric refs and over 10,000 metric observations. Read admission and fixture
construction are outside the measured serving phase.

## Workspace growth admission (migration 106)

Card writes do not fan out to referencing asks. `blocks_count` is an admission
hint; undated decisions seek `idx_brief_asks_open_page` for at most 240 candidates
(dated decisions retain 60), then resolve open blockers through scoped card
primary-key/handle batches and rank that fixed cohort. The existing 1,000-ref
hydration cap and 50-item display cap remain. Older candidates outside the
window can be omitted; the response reports truncation. No workspace-wide
blocker recount runs on a card insert, patch or delete.

Series declarations sum exact per-adapter definition counts, maintained by
insert/delete/move triggers. `idx_series_adapters_capacity` contains only active
adapters with definitions; declaration admits at most 1,001 entries, independent
of retired definitions and delivery-only adapters. Push checks both new and
expired retained label sets against capacity. Two index seeks on
`idx_series_labels_capacity(series,last_point_ts)` admit at most 100 recent and
100 reserved (no observation yet) labels. Point writes advance that timestamp
atomically; expiry is evaluated against the request clock without cleanup.
Historical labels and daily rollups remain discoverable. Counts alone use the
canonical accounting context; business reads and writes retain reader policy.
Migration 106 backfills these hints once and is idempotent.

`GET /work` retains 20 filter passes, each at most 200 candidates plus lookahead.
It reads one persistent cursor key by the `document_search_state` singleton
primary key, derives a separate work-token key, and emits authenticated encrypted
AES-GCM continuations. Version, principal scope, callback presence and all query
selectors bind each token; page size may change. Trusted callback callers must
supply the reader's access scope when representing a principal. Tokens expire
in 24 hours; malformed, old-format, foreign and expired tokens return the existing
`invalid_cursor` error. Restart resumes with the persisted workspace key; no
process-local cursor storage exists. Empty filtered pages can continue without
exposing hidden IDs or timestamps. Hydration and visibility remain scoped on
every page. The route's existing scale budget includes the added singleton read.

New native SQLite VM-step budgets live in
`internal/series/testdata/capacity_budget.json` and
`internal/storage/testdata/blocker_write_budget.json`; the added singleton key
lookup is covered by `internal/primitives/testdata/work_cursor_budget.json`.
The push gate permits two
constant full-scan steps for the existing two-scope request-budget cleanup;
capacity selection itself uses the indexes above. These are new focused budgets;
no existing allowance is widened.

```sh
go test -C core ./internal/storage -run '^Test(BlockerWriteTenThousandAskReferencesBudget|Upgrade105SeriesCapacityBackfill)$' -v
go test -C core ./internal/series -run '^Test(SeriesCapacityTenThousand.*Budget|ExpiredLabelReactivationHonorsCapacity|RevokedSeriesCapacityScaleAndPlan)$' -v
go test -C core ./internal/primitives -run '^Test(WorkCursorReaderFiltersRestartAndNoEviction|WorkListPaginationAdvancesPastAccessFilteredPages)$'
```

## Host bridge presence (migration 107)

`POST /hosts/bridge-status` is an authenticated read, bounded to 100 explicit
host IDs before scoped host filtering. Two batch queries seek the host/presence
primary keys and `(host_id,actor_id,kind)` job primary key. Each host admits 33
actor groups of at most five jobs; at most 32 groups are emitted, without refill
after denial, with `agents_truncated` for partial detail. Scoped host/agent
relations apply before returning data. Cost grows with admitted hosts/jobs,
never the workspace roster, history, or wake log. Expiry is evaluated by callers;
the UI batches visible hosts and ages badges locally.

`GET /hosts?summary=true&limit=100` admits up to 100 recent machine identities
through `idx_hosts_created_id(created_at DESC,id)` and scoped host visibility.
It does no roster, progress, or wake enrichment. `GET /hosts` without summary
keeps its existing inventory contract. No per-machine HTTP fanout is added.

Signed `POST /hosts/{host_id}/bridge/check-in` replaces a full operational
snapshot under one transaction. At most 256 actors/five jobs each enter a
batched indexed enrollment check and job insert; the presence and retired
instance lookups use their primary keys. No card/event writes or progress
projection work occur. Workspace-scoped daemon sequences advance even after
an unobserved response. Migration 107 preserves enrollments and key material,
drops obsolete shared wake-presence columns, and starts with no liveness data.

`TestHostBridgeStatusBudgetAndPlans` use native SQL
instrumentation with 10,000 unrelated hosts. The scoped nonempty-denial case
measured two queries, 166 admitted rows, 20,545 VM steps and no full scans;
the 100-row summary measured 808 VM steps and 99 bounded ordered index steps.
With foreign keys enabled, the full 256-actor/1,280-job write snapshot used six
statements, 60,270 VM steps initially and 84,609 on replacement, with no full scans.
`TestHostBridgeFullSnapshotWriteBudget` enforces 120,000 VM steps.
Budgets live in `auth/testdata/host_bridge_budget.json`; no existing budget or
exact query-plan exception was widened.

```sh
go test -C core ./internal/auth -run 'TestHostBridge.*Budget' -count=1 -v
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/storage -run '^TestPerformanceStartupAndMigrations$' -count=1 -v -timeout=10m
```

## Revision refs in batch ref resolve

`POST /refs/resolve` resolves `document_revision:` and `card_revision:` refs
through their parent. Per revision kind present in the batch (at most 200 refs)
it runs one statement by revision primary key and one by `<parent>-r<N>`
handle, which seeks the parent through its handle or primary key and the
revision through `UNIQUE(parent, revision_number)`, then one presentation-only
parent projection batch for visibility and title. A batch with no revision refs
runs nothing extra. `TestResolveRevisionQueriesAreIndexed` rejects a table scan
or temporary structure in either plan.

## Watch collection and per-panel rendering

Report materialization uses four workers, each with its own admitted resource
snapshot. Equal panel queries share one job. Native result-cache keys include
report revision, query, summary mode, actor/PM scope and the canonical data and
authorization epoch; a two-second TTL bounds clock-dependent promise changes.
A permission or native-record write invalidates reuse immediately. Source-bound
series, fleet and agent roster panels do not use this cache. Browser caching is
disabled. The cache holds at most 64 results of at most 128 KiB each.

Collection candidate admission precedes projection: initiatives seek designated
boards through `idx_boards_role` and cards through the board index; loops seek
annotated candidates through the partial `idx_brief_loop_annotated_recent` index;
project filters use `idx_work_metadata_project`. Admission caps are 200/50/100
for initiatives/loops/agents; displayed rows cap at 48. Plan and loop enrichment
reuse the existing bounded batches. Agent project filtering occurs inside the
100-agent cohort and preserves its truncation marker. Principal-kind admission
seeks `idx_agents_collection_roster` before username ordering and the cohort
limit; unrelated human principals do not add scan work. Agent stuck state uses
per-admitted-actor `idx_brief_asks_requester` existence probes, avoiding the
Overview roster's workspace-wide ask window. Actor labels are hydrated in batches.

Fourteen-day activity counts seek `(thread_id,ts)` or
`(actor_id,anx_timestamp_key(ts))` ranges for the admitted rows. Their work grows
with visible events in those fourteen-day subject ranges, not unrelated workspace
history. Their grouping has at most fourteen buckets per admitted row. Scoped
activity admits at most 201 events per explicit backing thread or subject key,
then sorts the bounded union before the existing 200-event budget. The subject
expression index `idx_events_report_subject_ts` covers asks posted outside the
subject's backing thread. Migration 113 installs those indexes and the pin table.

Pin reads seek `(principal_id,position)` for at most twelve IDs, then batch the
scoped document/revision/artifact heads by primary key. At most twelve report
blobs are checked; there are no per-document SQL hydration calls. Replacement
validates at most twelve targets in one transaction and reuses normal scoped
reference checks. Pins are principal-private preferences; their document refs
are additionally filtered through current document authority on read.

The 22-loop-panel comparator measured serial 1.35 s / 111 queries, bounded cold
306 ms / 122 queries and cached 128 ms / 12 queries (832 VM steps) on the task
host. Timings are indicative under shared host load; fixed query/VM assertions
are the acceptance gate. A separate collection fixture includes 4,096 unrelated
cards and tests both principals. No existing performance budget or plan exception
is widened.

Agent admission also tests 22 agents after 4,096 unrelated human principals.
The username scan used 62,535 VM steps; `idx_agents_collection_roster` reduces
the same collection read to 5,124 steps and an indexed principal-kind SEARCH.
That regression keeps the standard 50,000-step ceiling.

```sh
go test -C core ./internal/server -run '^TestWatch' -count=1 -v
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 go test -C core -p=1 -parallel=1 ./internal/server -run '^TestPerformanceWatch(CollectionBudget|AgentCollectionAdmission)$' -count=1 -v
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 go test -C core -p=1 ./internal/storage -run '^TestPerformanceStartupAndMigrations$' -count=1 -v -timeout=10m
```

## Attention records

Attention reads admit at most 101 candidates for 100-item pages. Recipient/subject
routing seeks the `attention_subjects` primary key, and winner selection uses
one preferred-placer primary-key seek and one newest-placer seek through
`idx_attention_placements_latest` per admitted subject. No all-placer window or
workspace-wide sort runs on reads. Asks seek
`idx_attention_asks_recipient(recipient,state,ask_id)` before scoped canonical
event joins. Feedback uses `idx_attention_feedback_page` or
`idx_attention_feedback_for_page`; admission precedes privacy filtering. Empty
pages retain encrypted, reader-bound continuations, so dense denied prefixes do
not require unbounded refill work. Titles and subject timestamps hydrate in
bounded batches. Default work uses the existing bounded work cursor.

Visit baselines seek `(principal_id,scope_ref)` on `overview_visits`. Builtin
snapshots admit at most 50 recent work subjects; document snapshots reuse bounded
report queries and retain at most 100 rendered subjects. Changes compare bounded
in-memory snapshots, never workspace history. Loop goal values use the existing
bounded loop facts/series readers. Visit writes compare timestamps atomically;
legacy markers are migrated by migration 114 to `builtin:overview`. New read budgets are in
`performance_routes.json`; no existing budget or plan allowance is widened.

## Inbox item resolution reads

`GET /inbox/{id}` checks the canonical ask resolution before returning an old
projection row. It seeks the inbox primary key and probes
`human_attention_request_resolutions(request_event_id)` through its unique
primary-key index in the same statement. The predicate adds no SQL round trip,
materialization, migration or workspace scan. It returns at most one inbox row;
existing bounded ref/summary enrichment and authorization remain unchanged.
Withdrawn and superseded asks return 404 immediately, including between lifecycle
maintenance and projection refresh. Their canonical `/asks/{id}` outcome remains
readable under the same ownership policy. Archiving alone retains readable asks
until canonical lifecycle maintenance withdraws them.

`TestPerformanceInboxItemResolutionRead` compares open and resolved point reads
with zero and 4,096 unrelated resolution rows. It enforces one query, one returned
row, 2,048 VM steps, zero full-scan steps and an indexed resolution plan, using
`primitives/testdata/inbox_item_read_budget.json`. The existing HTTP route budget
is unchanged. Run it with the established ask poll/read gate:

```sh
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/primitives \
  -run '^TestPerformance(InboxItemResolutionRead|AskDeliveryReadBudgets)$' -count=1 -v
```

## Primitives test time after canonical admission

The first main CI run after #469 hit the unchanged 25-minute primitives
package timeout: [run 40160966779, job 121072289854](https://github.com/Git-on-my-level/agent-nexus/actions/runs/40160966779/job/121072289854).
The preceding PR job passed but took
[18m36s](https://github.com/Git-on-my-level/agent-nexus/actions/runs/40159110376/job/121066742216).
This was a real regression, including a production write-preparation cost.
Rerunning the failed job is not the repair.

Uncached `go test -json` runs compared main `9d0fe66` with its parent `74aba48f`
using the exact correctness selection from `scripts/ci-core.py primitives`,
`-parallel=2`, `-count=1`, and the unchanged `-timeout=25m`. The local host was
an M5 Max, Go 1.27.0, `GOMAXPROCS=2`; CI uses its configured Linux Go toolchain.
These are package elapsed times, excluding compilation and slot wait:

| Test                             | Before #469 | Main after #469 |   Repair |
| -------------------------------- | ----------: | --------------: | -------: |
| complete primitives package      |    144.85 s |        549.27 s | 233.02 s |
| lifecycle cascade/fanout         |     36.37 s |        199.09 s |   9.55 s |
| frozen home membership           |      8.51 s |         45.79 s |   3.78 s |
| 10,000-document posting plan     |      5.71 s |         39.52 s |   4.32 s |
| snapshot randomized differential |      5.96 s |         11.97 s |  11.38 s |
| document handle history          |      8.32 s |          6.17 s |  10.04 s |

Main was 3.79 times its parent. The repair reduces it by 57.6%. The new small
admission randomized test itself took 1.14 seconds on main and 1.03 second after
repair; it was not the dominant cost. Its seeds, operations and assertions are
unchanged, as are the storage upgrade-shaped differential and all P0 regressions.
Fixtures already copied one immutable, fully migrated template, so migration
116 did not run for every test. The 20-second injected document-write failure
test also took 20 seconds on the parent and remains unchanged.

### Preparation is part of write cost

The admission update guards prevented unnecessary execution, but `WHEN` does
not prevent SQLite from preparing a trigger body and its nested trigger
programs. Modernc prepares a native VM for each execution, including Go
prepared-statement executions. A CPU sample of the synthetic write benchmark
placed 56.8% of samples under SQLite preparation. Executed-VM ceilings alone
missed this regression: an attribution update executed fewer VM steps while
compiling about 29 times as many instructions as its parent.

Migration 117 adds explicit `UPDATE OF` columns to guarded generic admission,
identity and source triggers. It uses the existing schema-owned OLD/NEW input
inventory, retains each existing `WHEN` and explicit column list, and preserves
the artifact/revision refresh exceptions. Integer primary-key inputs retain
`rowid`, `_rowid_`, `oid` and declared-key update spellings; a regression checks
real contribution moves as well as the reverse `NEW.rowid` spelling. A
contributions-only counter update
no longer compiles root ownership cascades. Trigger bodies, ownership policy,
read predicates and canonical epoch invalidation are unchanged. The migration
changes only a bounded schema inventory, with no canonical-row backfill. Both
ordinary and fenced upgrades verify completion before recording 117 and on
reopen. A no-op migration callback fails without advancing either ledger.

`BenchmarkCanonicalAdmissionWrite` makes the preparation cost reproducible.
The following synthetic public-write means use 50 executions in a transaction;
the separate driver capture includes executed trigger VM steps. Each sample
submits one SQL statement. These are local diagnostic means, not production
latency promises:

| Write              | VM: parent / main / repair | Compiled instructions: parent / main / repair | Local time: parent / main / repair |
| ------------------ | -------------------------- | --------------------------------------------- | ---------------------------------- |
| thread attribution | 433 / 189 / 107            | 688 / 20,246 / 108                            | 0.22 / 2.58 / 0.32 ms              |
| card title         | 531 / 1,715 / 1,664        | 1,131 / 16,001 / 8,516                        | 0.28 / 2.12 / 1.48 ms              |
| event append       | 1,171 / 4,145 / 4,145      | 4,472 / 17,469 / 14,249                       | 0.79 / 2.75 / 2.56 ms              |

Private event appends use 4,720 VM after repair versus 1,171 before #469;
attribution uses 107 and title uses 1,665. Admission still maintains the exact
and compact ownership indexes on relevant writes, so those writes retain a
measured cost above the old design. Unchanged-index and unrelated-component
write audits remain required. The new regression also inspects actual EXPLAIN
programs on fresh, released-116 and fenced released-116 databases, rejecting
unrelated ownership programs while checking a real ownership transfer across
all three ledgers.

```sh
go test -C core ./internal/primitives -run '^$' \
  -bench '^BenchmarkCanonicalAdmissionWrite$' -benchtime=50x -count=1
```

### Bounded correctness fixtures, retained full-scale assertions

The posting fixture now inserts all 10,000 unrelated documents and updates
their heads with two set statements. The lifecycle fixture similarly inserts
its imported events and edges with two statements per phase. Every canonical
trigger stays enabled. With the original full corpora and only these batching
and production changes, the complete package fell to 346.25 seconds; lifecycle
took 71.95 seconds and posting took 4.30 seconds.

Ordinary correctness CI then uses 256 canonical rows / 64 per PM kind for the
two repeated unrelated-workspace fixtures, 3,000 plus 6,000 fanout rows, and 512
unrelated project metadata rows. Both fanout phases exceed the unchanged
2,001-candidate cap. All assertions, VM ceilings and index-plan checks remain.
`perfguard.SeedRows` validates the exact requested cardinalities and preserves
the same ownership shapes. The default `Seed`, `Validate`, `Rows=4096` and
`PMRows=1024` used by route performance gates are unchanged.

`TestPerformancePrimitivesLifecycleMembershipAndWrites` calls these same tests
with their original 4,096-row / 1,024-PM corpus, 33,000 fanout edges, and 10,000
project metadata rows. Its name is discovered by the existing advisory workflow,
which sets `ANX_PERFORMANCE_TEST=1`; no workflow selection or timeout changed.
Run the complete preserved scale assertions locally with:

```sh
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/primitives -count=1 -v \
  -run '^TestPerformancePrimitivesLifecycleMembershipAndWrites$' -timeout=10m
```

No budget, baseline or allowance was widened. The six legacy inventory gaps
remain the separate pre-existing issue #472 described above.

## Quiet curation suggestions

Agent `GET /brief` and `GET /curation/candidates` add a bounded two-query
suggestion read: 51 open ledger positions via
`idx_curation_proposals_open_page(status,created_at,key)`, then at most 50
scoped event/thread primary-key lookups. Hidden rows do not refill the cohort.
There is no per-row hydration or scan through resolved proposal history.
`TestPerformanceCurationSuggestionsBudget` measured 2 queries, 101 returned
rows, 2,785 VM steps and zero scans with both zero and 4,096 unrelated dismissed
suggestions. Its new budget is 5,000 VM steps and zero scans; existing route
budgets and plan exceptions are unchanged. Migration 119 performs the one-time
legacy curation-ask withdrawal sweep, removes their materialized inbox rows
through the source-event index, and queues only affected threads by primary key.
Serving reads never perform that sweep. The proposal response capability adds
no read; re-suggestion after undo reuses the existing key lookup and upsert.

```sh
ANX_PERFORMANCE_TEST=1 ./scripts/slot run tests -- go test -C core ./internal/primitives \
  -run '^TestPerformanceCurationSuggestionsBudget$' -count=1 -v -timeout=5m
ANX_PERFORMANCE_TEST=1 ./scripts/slot run tests -- go test -C core ./internal/storage \
  -run '^TestPerformanceStartupAndMigrations$' -count=1 -v -timeout=10m
```

## Idle background maintenance

The [idle-maintenance reproduction and cost inventory](idle-maintenance.md)
records the synthetic 2.27 GB comparison, loop accounting, opt-in pprof listener,
indexed router/FTS positions, writer-free idle probes and dirty heartbeat usage
cache. New read budgets live in `primitives/testdata/idle_maintenance_budget.json`;
existing budgets and plan allowances are unchanged.

## Watch workstream rollups

Hierarchy enrichment retains its existing two batch statements and adds at most
two more for a single additional level, never a statement per initiative. Each
batch admits 200 cards; each plan admits 200 steps. At most 200 visible child
workstreams are expanded per root batch; deeper levels are not read. Thus fan-out
is bounded by 40,000 direct rows plus 40,000 second-level rows per 200-root batch,
independent of unrelated workspace size. Workstreams already admitted as input
rows reuse a detached direct-child rollup; only missing workstreams enter the
second SQL batch. Detaching child rollup pointers preserves the one-extra-level
bound and prevents cycles in legacy graphs. Smaller pages grow with their admitted
plans and referenced child cards. The existing primary-key/handle seeks,
`idx_brief_plan_refs_target`, and subject-ask indexes remain authoritative.
Visibility filters precede aggregation; no hidden contributor affects a count.

Migration 122 adds `idx_events_card_done`, a partial lifecycle index over
`(payload.card_id, anx_timestamp_key(ts) DESC, id DESC)` restricted to untrashed `card_moved` events entering
Done from a different column. The internal `work_done_event_positions` view
selects one latest candidate by this index using the shared nanosecond-precision
UTC key (mixed RFC3339 fractional precision and offsets sort chronologically); its ID never leaves SQL. Canonical
scoped event hydration then seeks that one ID, returning null if unreadable.
The comparison suppresses the outer card column's TEXT affinity with unary `+`
so SQLite can seek the JSON expression index rather than scanning its entries. `done_at` uses one indexed scalar seek per admitted
Done child within the batch SQL, with canonical event visibility and a one-row
limit. No event history is materialized or scanned as text at read time. The
migration builds one index over existing events once; there is no new canonical
state or authorization exception.

Brief outcomes add a primary-key metadata join to the already admitted cards;
SQL admits at most 281 code points and the projection emits 280. The tray reads
creator IDs/annotation versions with its existing card batch, then at most 50
actor profiles through primary keys in one scoped statement. Intentional-root
filtering occurs within the existing 200-card cohort without refilling hidden
rows. No workspace roster read is added.

`TestWatchWorkstreamRollupBudget` adds a populated ten-workstream/100-grandchild
fixture with 4,096 unrelated cards and lifecycle events, for both writer and
stranger. Its new `work_grouping_budget.json` workstreams entry preserves the existing hierarchy ceiling:
12 queries, 500 returned rows, 50,000 VM steps, and requires a SEARCH on the lifecycle index
in the native plan. Existing grouping and denied-prefix budgets are unchanged.

```sh
./scripts/slot run tests -- go test -C core ./internal/primitives -count=1 -v \
  -run '^Test(WatchWorkstreamRollupBudget|GroupingReadsBoundedWithTenThousandUnrelatedCards|GroupingUngroupedBoundedBehindTenThousandDeniedCards)$' -timeout=5m
ANX_PERFORMANCE_TEST=1 ./scripts/slot run tests -- go test -C core ./internal/server \
  -run '^TestPerformanceGroupingFeedBudget$' -count=1 -v -timeout=5m
```

Local acceptance on this branch, with an unrelated private board: nested
rollups use 6 queries / 123 returned rows / 27,096 owner or 48,050 partial-reader
VM steps / zero full scans. The existing
200-child hierarchy gate uses 40,403 VM steps; the denied-prefix brief uses
47,178. The complete ungrouped HTTP gate also passes (partial-reader samples
49,730 VM steps). No ceiling or query-plan allowance is increased.

The full startup scale gate remains blocked before migration 122: its fixed
90-second upgrade deadline expires in existing migration 116. A second run
with `migrations.go` overlaid verbatim from branch baseline `746e093c` reproduces
the same failure before the new index/view exists. This is a baseline gate
failure, not a passing startup certification. The focused released-120 upgrade
and reopen regression verifies migration 122 over existing lifecycle events.
`TestWatchRollupMigrationGap` verifies that both the legacy and fenced runners
record 122 without 121, then apply a subsequently introduced 121 on reopen,
without rerunning 122. Both runners test membership in their applied-version
ledger rather than a maximum version. The independent Watch index/view does
not depend on the work-tracking schema planned for 121.

The broader work-summary gate also reproduces against the same baseline source
(read implementations and migrations overlaid verbatim). Its authorized
50-card sample is identical before and after this change: 5 queries, 198 rows,
58,567 VM steps, above the existing 50,000 ceiling, with the same unreviewed
plans. The baseline partial-reader sample is 58,310 VM steps. The changed-branch
run reached the matching authorized samples before its five-minute process
timeout; the ten-minute baseline run completed and reported the gate failures.
Neither run is represented as passing.

The overlapping work-list-page regression (root plus its ten workstreams)
exposed duplicate hydration at 62,729 VM steps. Reusing the already admitted
rollups reduces it to 3 queries, 122 rows, and zero full scans. With an unrelated
private board, the final values are 27,825 owner / 48,611 partial-reader VM steps. The same unchanged workstreams budget gates this case.

Parent eligibility now admits the current scoped card/board once before its
steps and uses an indexed existence probe for an older visible alternative.
It retains the oldest-created/id-tie rule without rehydrating and sorting the
same current parent per child. Workstream admission uses the same rule. The
private-board fixture exercises the nonempty authorization path; no policy,
budget or plan allowance is relaxed.

## Ask presentation revisions

`GET /asks/{ask_id}/presentation` resolves one scoped ask by its existing
ID/handle indexes, loads that event by primary key, then seeks one
`ask_presentation_heads(ask_id)` and its audit event primary key. Brief and inbox
readers batch only their admitted asks in chunks of at most 200; each chunk
adds one query and at most one row per ask. Batch asks remain capped at ten.
There is no history scan, read-time rewrite, per-row query or new polling loop.
Original event history remains immutable; Decide/inbox overlay only the seven
allowed presentation fields and the audit metadata. Plain event GET/export
continues to expose canonical original events and separate revision events.

Cost grows with the admitted page and bounded presentation bytes, independent
of unrelated workspace size or revision history. The scoped head and event
relations apply current visibility before exposing copy or audit metadata.
A head the reader cannot see contributes no copy. Writes serialize one ask,
append one audit event and update one pointer plus one actor/key replay receipt;
current permission, open-state, subject visibility and the presentation token
are checked in that transaction. Migration 123 creates only an empty pointer
relation, with no historical rewrite or backfill. It follows Watch migration 122;
Work-tracking migration 121 is preserved from main. New route and point-read
budgets are checked in; no existing budget or plan allowance is widened.

```sh
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 ./scripts/slot run tests -- go test -C core ./internal/primitives \
  -run '^TestPerformance(AskPresentationReadBudget|AskDeliveryReadBudgets|InboxItemResolutionRead)$' -count=1 -v
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=2 ./scripts/slot run tests -- go test -C core ./internal/storage \
  -run '^TestPerformanceStartupAndMigrations$' -count=1 -v -timeout=10m
```

## Reversible stale ask retirement

`POST /asks/{ask_id}/retire` is a mutation, not a new read/poll surface. It
resolves one ask and subject by the existing ID/handle indexes, checks current
authority by principal primary key, and seeks the canonical resolution and
actor/key replay receipt by their unique indexes. Evidence admission performs
at most 32 indexed point checks (HTTP(S) evidence is validated without fetching).
Public-record preparation checks at most 32 evidence refs and the reason under
recipient scope using the existing exact/prose reference indexes. No evidence or
private audit ref is copied into the status record or notice. Both public events
strip run attribution and each performs one final persisted-event visibility
admission by the persisted event ref inside the write transaction, after
insertion-time decoration and ownership triggers. It follows the complete current
ancestry through indexed reference keys, avoiding a workspace ownership rebuild. Work is bounded by input bytes and their
ancestry. It appends a public lifecycle event, a curator audit and a notice, and
inserts/deletes one resolution. Undo identifies the original public event and
private audit by the public retirement key; the audit ID is derived as UUIDv5.
It requires no audit/history lookup and does not expose private audit content.
`TestPerformanceAskRetirementVisibilityBudget` enforces the new
`ask_retirement_visibility_budget.json`: one query, one row, 3,000 VM steps and
zero full scans per final check. The actual persisted notice ancestry uses
111 VM with no unrelated private roots and 359 VM with 4,096, with zero full scans.
The outcome and presentation budgets remain unchanged.
Undo updates only that ask's `ask_subjects`, `brief_asks` and `ask_deadlines`
primary-key rows. It never loads ask history or workspace rosters.

`GET /asks/{ask_id}` loads the ask-visible lifecycle record through its existing
primary-key projection, exposing the current retirement token with no additional
query. It never reads the curator audit, evidence or run attribution.
Inbox point/list/stream admission already excludes canonical resolutions, so
retirement adds no reads to those paths. Brief already selects only open
indexed `brief_asks` rows. Roster admission adds a primary-key resolution
exclusion per existing ask candidate, retaining its existing finite baseline;
it does not introduce a history scan or new per-ask round trip. Ordinary
background projection replay recognizes retire/reopen audit pairs during its
existing thread pass, using linear maps and exact event identities.

The existing ask outcome/delivery and presentation scale gates are unchanged;
no read budget or query-plan allowance is increased. Retirement needs no new schema or backfill; migration 124 below only replaces
an enqueue trigger.

## Original presentation and outcome delivery corrections

Presentation hydration adds the immutable original event to the existing head
query through its primary key. This is still one query per admitted batch (at
most 200), one row per ask, and bounded original/presentation text. It never
scans revision history. The existing presentation read budget is unchanged.

Migration 124 replaces only the subscription-enqueue trigger, excluding
retirement with one primary-key lookup of the resolution event. It adds no
column or index, scans no existing rows and backfills no content. Late
subscription creation checks the current resolution event through the same
primary key before enqueueing. Existing ask outcome/status, receipt and webhook
read paths and their bounds/indexes are unchanged. Retirement/undo add one
bounded notification message on the existing card thread in their mutation
transaction and add no read path or poll loop. No budget or plan allowance is
increased.

## Canonical single-ask answer fallback

`POST /inbox/{inbox_id}/respond` can recover a single ask before its asynchronous
inbox projection exists. Each lookup admits at most two prefix variants, seeks
`derived_inbox_items` by primary key, then loads at most one original event and
one backing thread by primary key per variant. The full reconstructed inbox ID
must match; revision events and filed batch members cannot impersonate a single
ask. Existing scoped event/thread hydration retains its indexed reference
lookups. The handler's batch detection, authorization and answer preparation
perform at most three such lookups, independent of workspace size and revision
history. There is no thread-history scan, projection rebuild or polling change.
Canonical resolution and idempotency admission remain in the existing write
transaction using their unique indexes.

`TestPerformanceCanonicalAskAnswerLookup` enforces
`server/testdata/ask_answer_lookup_budget.json` with zero and 4,096 unrelated
asks. It bounds each warm scoped fallback to 20 statements, 32 returned rows,
10,000 VM steps and zero full scan steps. No existing budget or query-plan
allowance is changed. Run alongside the existing presentation/delivery gates:

```sh
ANX_PERFORMANCE_TEST=1 ./scripts/slot run tests -- go test -C core ./internal/server \
  -run '^TestPerformanceCanonicalAskAnswerLookup$' -count=1 -v -timeout=5m
ANX_PERFORMANCE_TEST=1 ./scripts/slot run tests -- go test -C core ./internal/primitives \
  -run '^TestPerformance(AskPresentationReadBudget|AskDeliveryReadBudgets|InboxItemResolutionRead)$' -count=1 -v -timeout=5m
```

## Watch trends

`GET /watch/trends` is three requests, one per group, so no single read admits
every record family. Each family is one newest-first indexed seek with a fixed
cap; visibility filters run inside the scoped relations before counting, and
the cap bounds examined rows, not just returned rows:

- decisions: `idx_events_ask_order` (type, time key) over at most 111 asks with
  primary-key `brief_asks`/thread joins; the `brief_asks` state index over at
  most 91 open asks; one actor batch of at most five ids.
- initiatives: two `idx_brief_initiatives_page` seeks (56 open, 36 closed
  promised initiatives) with primary-key card joins and one lifecycle-index
  done seek per admitted Done card; two grouping-index seeks of 21 created
  cards; one `idx_cards_closed_work_page` seek of 41 done cards.
- loops: `idx_events_ask_order` over at most 171 run receipts;
  `idx_brief_loop_annotated_recent` over 25 loops; one actor batch.

Under a canonical access scope the PM-private rule is already a denial root,
so the read skips the brief's per-row owner lookups, except for the PM actor
reading as itself, whose scope is broader. Nothing grows with workspace size
except through the caps; a capped family marks its panel truncated and leaves
older buckets null. Each group is cached per caller, authorization epoch,
group and range for 60 seconds.

`TestPerformanceWatchTrendsBudget` fills every cap (240 asks, 240 runs, 4,096
unrelated cards, every initiative promised, a private board) and measures the
90-day read for the owner and an unrelated reader against the ordinary budget.
Local acceptance: owner 5-7 queries / 86-204 rows / 5,658-9,047 VM steps;
unrelated reader decisions 36,434, initiatives 41,016, loops 28,471 VM steps.
The per-row cost of scoped reads for a reader who is denied part of the graph
(about 140-380 VM steps) is why the caps are this size and why the groups are
separate requests.

```sh
ANX_PERFORMANCE_TEST=1 go test -C core ./internal/server -run '^TestPerformanceWatchTrendsBudget$' -count=1 -v
go test -C core ./internal/primitives -run '^TestWatchTrends' -count=1
```
