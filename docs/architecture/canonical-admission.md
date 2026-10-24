# Candidate authorization admission

Status: proposed; full ordinary-route performance acceptance is incomplete.

The request denial cache traverses every private root's reachable descendants on
an epoch miss, before a page knows which records it will read. Main's shared
4,096-row corpus pays roughly 1.24M extra SQLite instructions for this admission.
An endpoint candidate index cannot eliminate a closure captured before it runs.

Persist the canonical graph's direct edges, maintained atomically by the same
canonical writes, and admit a requested resource by walking its indexed parents.
Keep origin identifiers for independent contributions: removing an alias, a
revision, or one mention must preserve a dependency still supplied elsewhere.
The exact and compact ledgers remain independently selectable. Structural
inheritance shares one executable inventory with the existing forward policy.
Prose references continue to resolve through the existing write-time mention
index, including identities created after the prose.

Maintain a small owner-count index with canonical root writes. Three indexed
existence seeks determine whether the reader has any denied root, including
unknown artifact provenance. Preserve the original SQLite value type for legacy
JSON owners. A reader with no denied roots needs no ancestry walk.

This chooses read-time candidate ancestry over a persisted transitive closure.
It avoids per-principal state, a workspace rebuild after revocation, and closure
deletion/reachability maintenance across cycles. It costs storage proportional
to direct canonical dependency contributions. Writes pay for changed origins
and their indexed matching reference fanout; the initial migration backfills
those direct contributions once. Dense candidate ancestry can still be expensive.

Admission remains part of the consuming SQLite statement. Read-only page
transactions see one consistent graph; the next request sees a committed
revocation. Business write checks retain the canonical transactional policy.
No model, provider adapter, external authority, deployment-specific rule, quota,
or feature flag participates in authorization.

Exact record shadows and projection spellings use candidate admission. A
separate spelling index preserves binary endpoint comparisons, NOCASE JSON
atoms, document aliases, external URLs and legacy prose matching. Prose seeks
mention buckets before invoking the complete boundary matcher. Dense buckets
and dense ancestry still require adversarial cost coverage.

Pinned pages memoize SQL-proven resource and text decisions in separate
namespaces with a shared 8,192-entry/1-MiB bound. Ancestor expansion stops at a
known proof. Every use checks the epoch and SQLite connection total_changes(),
including borrowed mutable transactions; missing proofs evaluate the canonical
indexed policy. The token expires when the read closes. Counts, filters and
lookahead operate on scoped relations before limits.

Before shipping, the complete ordinary-route matrix must meet the unchanged
50,000 VM ceiling. Unpaged inventory hydration, lifecycle inventory scans,
optional compact-ledger rollback, historical-schema replay and large-history
write amplification remain blockers/validation concerns. The protected visit
test also still assumes eager complete request snapshots. No protected visit
implementation or test was changed. This remains a draft until these are
resolved; selected passing pages do not constitute full acceptance.

Measurements, limitations and reproduction commands are in
[performance guardrails](../../core/docs/performance.md#canonical-admission-main-reproduction-and-candidate-index).
