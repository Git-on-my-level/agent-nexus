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
backfills these derivable indexes once before readiness. Storage grows with
reachable root/vertex pairs in three ledgers (the worst case is the product
of roots and vertices), plus direct contributions and spelling prefixes.

Untouched historical wakeups retain their existing indexed thread/event parents
instead of being copied at startup. The reachability writer follows those
canonical indexes directly. Reached wakeups acquire resolution keys and projection
spellings; a structural edit materializes its old contribution before deleting
obsolete ancestry. New rows use normal incremental admission. This preserves the
released receipt-history startup bound without changing visibility semantics.

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
canonical checks. Closing a pinned read removes its token. Full inventories take
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

Canonical source updates retain unchanged atoms and their rowids. Identity and
admission triggers skip unchanged inputs. Evidence descriptions retain stable
publication keys. Explicit history refresh signals remain active when artifact
revision membership changes. Write-audit tests reject touches to unchanged
indexes and unrelated components. A grant/revoke may update all descendants
whose ownership actually changes; that is the deliberate write-side cost.

No model, external authority, provider-specific adapter, deployment rule or flag
participates in admission. See [measurements and remaining inventory gaps](../../core/docs/performance.md#canonical-admission-main-reproduction-and-candidate-index).
