# Polling and answer performance

The answer request commits its complete replay receipt, resolutions, task outcome,
and durable wake work together. It removes the resolved inbox row in that same
transaction. Whole-thread projection refresh and wake dispatch run through the
existing maintainers after the request; a process restart cannot lose that work.
This applies to individual asks and filed ask batches.

Finite hot reads share a small CPU admission lane. A decoded answer prevents new
reads from entering until its handler finishes, while existing WAL readers finish
normally. Uploads reserve no lane before body decoding; cancelled read waiters
leave promptly. Streams retain their separate bounded tick lifecycle.

## Synthetic single-CPU measurements

Measured on 2026-10-14 with `GOMAXPROCS=1`, using the existing route corpus plus
4096 large synthetic events on one answer backing thread: about 812 MB, 4096
cards, more than 8192 events and 4096 PM records. The corpus includes unrelated
private controls and a topic linked to a dense board. No production data was used.
Fixture construction is excluded from profiles and request measurements.

Before is release v0.12.71 (`2579b19d`), with the same new regression test copied
into a detached checkout. After includes the v0.12.72 ask-batch changes. Each run
answered 20 asks and replayed every request byte-for-byte while three workers
continuously polled inbox, unread and events. These are local synthetic timings,
not production latency promises.

| Operation                     |            Before |        After |        Queries before → after |
| ----------------------------- | ----------------: | -----------: | ----------------------------: |
| Answer p95 / max, 20 samples  |    4500 / 4673 ms | 311 / 318 ms |                             — |
| Replay p95 / max, 20 samples  |      438 / 450 ms | 180 / 183 ms |                             — |
| Inbox, limit 50               |             68 ms |        30 ms |                       17 → 17 |
| Home unread                   |           1847 ms |        17 ms |                      178 → 18 |
| Events, limit 50              |            257 ms |         6 ms |                       73 → 11 |
| Work, limit 50                |            110 ms |        64 ms |                       15 → 15 |
| PM actions, limit 50          |           1859 ms |        13 ms |                      159 → 12 |
| PM decisions, limit 50        |           1868 ms |        13 ms |                      164 → 17 |
| Dense topic workspace         | timed out at 60 s |        27 ms | 3628 before cancellation → 28 |
| Completed inbox, limit 50     |            102 ms |         4 ms |                         5 → 5 |
| Report preview, live fleet    |            179 ms |        20 ms |                       12 → 14 |
| Report preview, live activity |            896 ms |        68 ms |                       55 → 50 |

CPU profiles identify repeated SQLite preparation, per-row PM work hydration,
whole-workspace unread grouping, and whole-topic bundle hydration as the main
costs. The measured profile's SQLite preparation falls from 39.21 to 3.03 CPU
seconds; topic bundle construction accounts for 36.73 CPU seconds before the
change. Cumulative samples include concurrent poll traffic and the cancelled
baseline topic request, so they are not per-answer CPU measurements.

Answer transactions build the canonical denial closure in their own transaction,
keyed by actual transaction, principal and current authority epoch. Each consuming
read guards that epoch; mismatch returns no rows or denies submitted values.
Epoch changes rebuild the proof. Ordinary mutations retain the original policy;
admission never escapes the serial append or survives rollback-and-continue.
Tests cover exact identities, historical aliases, document synonyms, revision
handles, external keys, prose guards, ownership changes, equal-epoch transactions,
missing epochs, scope rebinding, explicit SQL parameters and closed admission.

## Bounds and indexes

Canonical ownership filtering remains before pagination and hydration. Existing
bounded denial-cache admission is reused for each finite read snapshot, avoiding
per-statement graph preparation; answer mutations use current transaction evidence.

| Read                   | Materialization bound and indexed selectors                                                                                                                                                                                                                                                                                                                                                        |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Inbox                  | Existing limit/cursor page, `idx_derived_inbox_items_*`; response deletion and claims use primary/unique keys.                                                                                                                                                                                                                                                                                     |
| Unread / events        | At most 200 eligible events before grouping, existing event order/type/thread indexes; group metadata uses ID/handle or `idx_topics_thread_id` / `idx_boards_thread_id`; cursors use `idx_home_read_cursors_reader`. No group-directory or read-state-history scan.                                                                                                                                |
| Work                   | Existing limit/cursor page and indexed board placements; bounded context hydration remains unchanged.                                                                                                                                                                                                                                                                                              |
| PM actions / decisions | Existing `pm_records_page` / `pm_records_owner_page` pages; at most 200 work selectors hydrate phase/authority in one batch through card ID/handle/alias keys and indexed board placements.                                                                                                                                                                                                        |
| Topic workspace        | At most 101 keys per forward/reverse topic edge arm, 10 boards, 50 cards, 50 document metadata rows, 100 threads and 50 inbox items. Each board/thread arm admits at most 51 candidate keys before merging. Migration 88 adds ordered source/target ref-edge, parent-thread card and backing-thread document indexes; query-plan tests reject scans and temporary sorts for these candidate seeks. |
| Report preview         | Body is parsed first, then one scoped finite read snapshot feeds existing bounded panel/work/event projections; document revision history is not loaded by topic previews.                                                                                                                                                                                                                         |

`GET /home/unread` now accepts `limit` and `cursor`, and returns `next_cursor` and
`has_more`. Counts describe the current event page. A group can recur across pages,
and an empty page can still advance. The inbox follows up to eight pages, merges
event identities and preserves partial-history status. Topic workspace previews
expose `cards_truncated` / `resources_truncated`; full resources remain available
through collection/detail APIs. These are intentional beta API behavior changes.

Browser clients share only simultaneous finite polled GETs within their session,
actor, workspace URL, headers and transport options. Each subscriber gets its own
body and cancellation; the transport aborts after the last reader leaves. Entries
expire on settlement. Pollers preserve a dirty follow-up and pause while hidden.
Mutations retain their durable senders and the executable mutation AST inventory.

## Reproduction

```sh
ANX_PERFORMANCE_TEST=1 GOMAXPROCS=1 \
  ANX_POLL_CPU_PROFILE=polling.cpu \
  go test -C core ./internal/server \
  -run '^TestPerformancePollingAnswersSingleCPU$' -count=1 -timeout=10m -v
go tool pprof -top core/internal/server/polling.cpu
```

The test is also included in `make -C core test-perf`. It asserts answer/replay
p95 below one second, hot-read latency below 500 ms and at most 40 queries, and
successful live report panels below one second and 100 queries. It checks private
controls, exact replays and committed claim counts, and joins every polling worker.
