---
title: "Remove inbox read-time projection recomputation"
agent: codex
done: true
ticket_id: "tkt_core_inbox_read_projection_022"
---

## Triage

Priority: P2. This is architectural cleanup around projection semantics; it should follow narrower correctness fixes unless inbox projection work is already in flight.

## Problem
Most inbox GET reads use materialized derived inbox items plus projection freshness metadata, but `risk_horizon_days` switches `/inbox` and `/inbox/{id}` back to deriving inbox items during the GET request. The inbox SSE path also derives every poll. This creates two read semantics for the same projection surface and bypasses the durable projection freshness model that core documents as the common read path.

## Evidence
- `core/internal/server/inbox_handlers.go:290` calls `deriveInboxItems` directly when `risk_horizon_days` is present, then returns without `projection_freshness`.
- `core/internal/server/stream_handlers.go:234` calls `deriveInboxItemsNoStaleEmission` on every SSE poll.
- `core/AGENTS.md:40` says derived views must stay rebuildable from canonical state, and `core/docs/http-api.md` says standard GET responses consume materialized derived projections with freshness metadata.
- Commands used:
  - `nl -ba core/internal/server/inbox_handlers.go | sed -n '266,318p'`
  - `nl -ba core/internal/server/stream_handlers.go | sed -n '228,246p'`

## Proposed Fix
Define one materialized read path for inbox list/get/stream. Either persist risk-horizon-specific projection state explicitly or remove the custom horizon read-time recomputation and document the supported horizon semantics. Keep freshness metadata on list/get responses and make SSE emit changes from the same materialized source.

## Validation
- Add integration coverage showing `/inbox`, `/inbox?risk_horizon_days=...`, `/inbox/{id}`, and inbox stream agree on item shape/source and do not recompute projections inline.
- `go test ./internal/server -run 'Inbox|Projection|Stream'`
- `make -C core check`

## Progress
- Removed the `risk_horizon_days` read-time derivation branches from `/inbox` and `/inbox/{id}`. The parameter is now validated for compatibility, while both endpoints read `derived_inbox_items` and include projection freshness for open inbox reads.
- Changed `/stream/inbox` to poll `ListDerivedInboxItems` and emit stored projection rows shaped through the same payload path as list/get responses.
- Updated OpenAPI and HTTP docs to document the materialized horizon semantics, then regenerated contract artifacts.
- Added integration coverage for materialized risk-horizon list/get reads, pending-projection non-recomputation, and stream emission from stored derived rows.
- Validation passed:
  - `go test ./internal/server -run 'Inbox|Projection|Stream'`
  - `make contract-check`
  - `make -C core check`
