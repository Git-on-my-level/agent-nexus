# Scoped inbox rollout

Enable the reader for one workspace's core process with
`ANX_SCOPED_INBOX_READER=true` (or `--scoped-inbox-reader`). It is off by default.
For a deployment running one core per workspace, set the environment variable
only on that workspace's service and restart it. No user credentials or API
setting are needed. Roll back by unsetting it and restarting; the additive schema
and canonical data remain compatible with the legacy reader.

This is the fast-mode serving bridge. The proof-gated dispatcher remains staged.
The live model has one ordering row per canonical inbox item, partitioned by its
backing thread (`scope_id`), without copying titles, JSON or authority. The index
selects candidates; every returned payload still loads from the existing
principal-scoped `derived_inbox_items` relation, using the current ownership
policy. This retains transitive mention privacy, recipient and lifecycle filters,
report-review checks and the handlers' live access-request/profile enrichment.
It does not yet replace the legacy authorization graph with scope membership.

Migration 71 installs empty tables, indexes and three row triggers. It performs
no historical backfill. With the flag enabled, the server's maintenance lifecycle
builds 64 rows per transaction, at most once every 100ms, with a 100ms deadline.
The committed primary-key cursor survives interruption and restart. Readiness is
published on an empty final seek. Inserts, key changes, edits and deletes maintain
the ordering index atomically, including writes behind the build cursor.

Open inbox pages, inbox SSE pages, Overview's inbox window, and ask summaries use
the index when eligible. Item detail and completed inbox retain their existing
readers. Every new-selector error, incomplete build, unsupported/unbounded list,
or exhausted candidate budget retries the entire legacy selection for that read.
No partial new page or approximate total is published. Handler enrichment and the
existing cursor format are unchanged; no HTTP route or contract shape changes.

An ordinary page admits at most `min(4*(page_size+1),512)` raw candidates and
hydrates at most `page_size+1` authorized items. Cursor spans seek
`scope_inbox_live_order` or `scope_inbox_live_scope_order` before LIMIT; their
bounded union is at most three candidate windows. Hydration uses canonical inbox
primary-key probes and sorts only the admitted IDs. If hidden/inactive candidates
prevent filling the page, it falls back. Summaries enumerate at most 513 positions:
more than 512 uses the legacy aggregate. Canonical authorization preparation and
legacy fallback retain their existing costs; this rollout makes no claim of
removing all workspace-sized legacy work. Each inbox mutation adds one index-row
upsert/delete and two index updates; storage is O(inbox items), with no edge or
payload duplication.

One in 100 flagged selector calls queues a fresh off-request ID/order/count
comparison against legacy. The queue holds eight selectors, retains identity only,
and processes at most one job per second with a two-second deadline. Both readers
share one new read snapshot and fresh principal policy, so concurrent changes
cannot create false parity mismatches. Shutdown cancels and joins both new workers.
Logs contain reason codes and counts only, without private IDs/titles/principals.

Every minute the core logs `scoped inbox diagnostics` with Requests, Served,
Fallbacks, NotBuilt, CandidateBudget, ReadError, Unsupported, ShadowCompared,
ShadowMismatch, ShadowError and ShadowDropped. Fallback rate is
`Fallbacks / Requests`; counters reset on process restart. A shadow mismatch logs
`count_mismatch` or `id_order_mismatch`. Missing build, candidate budget and deadline
refusals are counted separately as skipped comparisons. These are operator logs,
not an unauthenticated HTTP diagnostics endpoint.
