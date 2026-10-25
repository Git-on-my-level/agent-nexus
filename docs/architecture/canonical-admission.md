# Canonical authorization admission

Status: proposed in PR #469. Budgets and allowances are unchanged.

## Problem and choice

Main captures every denied root and reachable identity on a request epoch miss,
before a page admits candidates. On the shared 4,096-row corpus this adds about
1.24M SQLite VM steps to a denied reader's cold or invalidated request. It
predates grouping/rollup #463. Pure reverse ancestry checks remove that rebuild,
but the grouping cohort still reaches thousands of shared ancestors and exceeds
50,000 VM. Batching those ancestors also failed the ceiling.

Migration 116 therefore persists principal-independent root reachability and
reachable-owner counts, maintained in the canonical writer's transaction.
Candidate reads use at most three indexed owner-range existence seeks. They do
not walk ancestry or materialize the workspace denial closure. This trades
storage and access-write maintenance for bounded request admission. A root owner
transfer changes owner counts without rebuilding its reachability graph.

Broad epoch invalidation remains conservative. A local v0.13.28 reproduction
interleaves attribution-only edits, harmless public title edits and public event
appends with an unrelated partial-reader request: every intervening write
invalidates the old closure cache. With continuous writers, cold admission can
therefore become the usual request cost rather than an occasional startup cost.
This design still sees those epoch changes but never rebuilds a bounded read's
workspace closure. It also removes the expensive warm canonical ancestry path.
See the interleaved-write measurements in the performance guide.

Narrowing epoch triggers is a possible separate optimization, but is not the
chosen first shipment. Attribution-only updates can be excluded; a new event or
changed text may introduce inherited private references and still needs fresh
authority. Proving that arbitrary content writes leave denial unchanged requires
the dependency maintenance this change already supplies. An attribution-only
patch would leave content-driven invalidation, the first cold read and the warm
ancestry cost. Retaining broad epochs avoids adding a second revocation-sensitive
change to this repair. Epoch narrowing can be evaluated independently afterwards.

Direct dependencies retain origin contributions, so deleting one alias,
revision or mention cannot remove another source's dependency. Structural
inheritance shares the forward policy's executable inventory. Reachability is
separate for legacy, compact and common-only (unknown selection) ledgers.
Insertion visits only newly reached root/vertex pairs. Edge deletion recomputes
only roots that reached that edge, then deletes only memberships that changed;
recursive UNION handles cycles. These writes can read the affected root's
reachable component. They do not rebuild unrelated components. Initial migration
backfills these derivable indexes before readiness. Schema/direct-index setup
is an atomic linear phase; transitive root reachability then commits one distinct
root and its durable primary-key cursor per transaction. Restart resumes without
replaying completed roots; initialization exposes no partially built workspace.
One chunk can still traverse one root's entire component, and total upgrade work
can still grow as roots times vertices. Storage grows with
reachable root/vertex pairs in three ledgers (the worst case is the product
of roots and vertices), plus direct contributions and spelling prefixes.

Untouched historical wakeups retain their existing indexed thread/event parents
instead of being copied at startup. The reachability writer follows those
canonical indexes directly. Reached wakeups acquire resolution keys and projection
spellings; a structural edit materializes its old contribution before deleting
obsolete ancestry. New rows use normal incremental admission. This preserves the
released receipt-history startup bound without changing visibility semantics.

Lazy historical key resolution must finish before propagation in each ledger.
Resolving a wakeup while the common-only membership is inserted can create a
legacy/compact dependency before that ledger's parent membership exists. A later
conflicting key insert does not repeat its insert trigger. Each new wakeup
membership therefore resolves its keys, then walks dependencies from its own
ledger/root. This does not rely on recursive INSERT evaluation or trigger order.
Retained wakeup aliases and tombstones are backfilled through explicitly pinned
origin-leading uniqueness-index seeks;
only untouched canonical wakeup history is omitted.

Canonical ID mappings also come from every canonical structural declaration,
indexed source vertex and root, even when the resource row is absent. The input
inventory shares the forward policy's structural definitions. Typed IDs support
navigation; bare IDs keep the original exclusions. Resolve from canonical inputs,
not from key-generated graph rows, so key creation cannot recursively suppress
its own resolver trigger. One ID mapping is shared across declarations and kept
after deletion: it carries no authority, and current root/reachability counts
alone decide denial. This adds storage proportional to distinct observed IDs;
ID-unchanged writes do not change the dictionary. Existing alias/publication
contributions retain their separate lifetime semantics.

Both migration loaders and the history fixture use one runner. Chunk callbacks
execute inside its transaction and cannot record versions. Completion verifies
the required admission schema before recording 116; a fenced ledger and its new
digest commit together. Empty or mixed execution plans fail. Reopens verify an
already-recorded 116 and refuse incomplete schema before serving any request.
Both fenced and ordinary upgrades support one-root checkpoints and resumption;
the downgrade fence remains intact.

Retained aliases and tombstones contribute canonical typed/bare ID keys directly
from their resource tuple, independently of the retained spelling. This includes
empty spellings, virtual document-history identities and external-key IDs, with
the same exclusions as the canonical policy. Removing one retained contribution
does not remove keys contributed by another. Migration and runtime use the same
insertion SQL.

## Exact authority and projection matching

Root owners retain their SQLite value types. Owner indexes preserve the original
comparison semantics, including numeric legacy JSON owners, anonymous readers,
designated PM reads and unconditional unknown-artifact denials. Read-only page
transactions see one consistent authority snapshot; the next request sees a
committed revocation. Borrowed writers also check their connection's
`total_changes()`. Business mutations retain canonical transactional validation.

Projection spellings retain binary endpoint comparisons, NOCASE JSON atoms,
document aliases, external keys and retained purge provenance. Prose uses
Unicode-folded digest seeks and a counted prefix index in 16-rune chunks. A read
continues only through prefixes that exist, then applies the complete legacy
byte-length and boundary matcher. Its work grows with admitted reference starts
and matching text prefixes, not with unrelated identities sharing a bucket.
No prefix or digest alone decides visibility.

Optional compact tables may be torn down independently. Canonical triggers name
a required admission-owned compact atom mirror; optional-table triggers maintain
it while that capability exists and are reinstalled on capability recreation.
A selected but missing compact capability fails closed. Legacy admission does not
prepare SQL against the removed optional table.

SQL-proven resource/text decisions share a request-local 8,192-entry/1-MiB limit.
Every reuse checks epoch and connection changes; expired tokens use indexed
canonical checks. Closing a pinned read removes its token. The former unused ancestor-JSON
argument was removed; only the proven candidate result is remembered. Full inventories take
one indexed denial batch for the entire immutable read and use its membership
map with the candidate projection policy. Mutable borrowed transactions retain
fresh owner checks. This does not add an inventory batch to bounded routes.

## Lifecycle pages and write cost

`/work` no longer inventories every card, document, topic and thread for each
page. Lifecycle refs follow active work through the same opaque continuation.
The inactive-card routing index has public, required-owner and designated-PM
ranges, maintained from reachable owners. It skips private prefixes before
hydration; canonical authorization is still enforced before counts, lookahead
and output. Following the continuation collects the complete visible lifecycle
inventory. A private-only inventory returns no hidden count or continuation.
The lifecycle helper rejects a missing access scope before querying; explicit
anonymous scopes retain ordinary policy. Empty lifecycle pages may still carry a
visible-resource continuation, which callers must follow within their existing
traversal caps. SQL trigger construction retains its reviewed string-shape and
explicit history-refresh constraints; this change does not replace that machinery.

Canonical source updates retain unchanged atoms and their rowids. Identity and
admission triggers skip unchanged inputs. Evidence descriptions retain stable
publication keys. Explicit history refresh signals remain active when artifact
revision membership changes. Write-audit tests reject touches to unchanged
indexes and unrelated components. A grant/revoke may update all descendants
whose ownership actually changes; that is the deliberate write-side cost.

No model, external authority, provider-specific adapter, deployment rule or flag
participates in admission. See [measurements and remaining inventory gaps](../../core/docs/performance.md#canonical-admission-main-reproduction-and-candidate-index).

## Trigger preparation

Migration 117 narrows guarded generic update triggers with explicit `UPDATE OF`
inputs. An unchanged-input `WHEN` saves execution but still makes SQLite prepare
the complete nested ownership program. This mattered for production attribution
writes as well as large test fixtures: fewer executed VM steps did not imply a
cheap write. Retain both kinds of guard and inspect compiled programs alongside
executed VM in write-cost regressions. Artifact and revision membership refresh
signals retain their existing exceptions.

This is a schema-only repair over the fixed trigger inventory. It preserves
every trigger body and canonical input, performs no data backfill, and verifies
the installed programs on both migration ledgers. Required correctness tests
use bounded unrelated corpora; the existing advisory performance lane runs the
same assertions at their original full scale. P0 and randomized parity tests
retain their complete assertions and fixed seeds. See the
[primitives timing and write measurements](../../core/docs/performance.md#primitives-test-time-after-canonical-admission).
