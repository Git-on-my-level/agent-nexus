---
title: "Make work item stale counts meaningful or remove them"
agent: codex
done: true
ticket_id: "tkt_core_work_item_stale_count_023"
---

## Triage

Priority: P3. This appears to be dead or misleading derived summary data; useful cleanup, but lower urgency than integrity and operator-safety issues.

## Problem
Derived topic projection work-item summaries include a `StaleCount`, but the freshness window helper always returns no window. As a result, `StaleCount` is effectively dead data and can never reflect stale cards, even though the surrounding summary computes open, due-soon, overdue, and blocked counts.

## Evidence
- `core/internal/server/derived_projections.go:262` calls `threadFreshnessWindowStart` before deciding whether to increment `summary.StaleCount`.
- `core/internal/server/derived_projections.go:305` ignores all inputs and returns `time.Time{}, false`.
- Command used: `nl -ba core/internal/server/derived_projections.go | sed -n '249,313p'`.

## Proposed Fix
Either implement a real freshness source for work-item staleness or remove `StaleCount` from the derived summary payload if card stale counts are no longer a supported concept. If keeping it, add tests that create an old open card and verify the projection reports a nonzero stale count under the configured rule.

## Validation
- `go test ./internal/server -run 'Derived|Projection|Stale|WorkItem'`
- `make -C core check`

## Resolution
- Removed unsupported `stale_work_item_count` from derived topic projection summary data.
- Removed the dead `StaleCount` summary field and `threadFreshnessWindowStart` helper.
- Added regression coverage asserting derived topic projection data omits `stale_work_item_count`.

## Validation Results
- `go test ./internal/server -run 'Derived|Projection|Stale|WorkItem'` passed.
- `make -C core check` passed.
