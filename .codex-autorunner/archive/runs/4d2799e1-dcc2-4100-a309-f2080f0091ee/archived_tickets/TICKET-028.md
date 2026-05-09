---
title: "Remove no-op stale scan maintenance state"
agent: codex
done: true
ticket_id: "tkt_core_noop_stale_scan_028"
---

## Triage

Priority: P3. This is cleanup of misleading maintenance state after staleness inference was disabled; it should follow narrower correctness fixes.

## Problem
Staleness inference was intentionally disabled, but the projection maintainer still schedules and reports a periodic `stale_scan`. The scan calls a function that always returns no emitted threads, then records successful stale scan timestamps in ops health. This is dead background work and can mislead operators into thinking core is actively evaluating staleness.

## Evidence
- `core/docs/anx-core-spec.md:309`-`:315` says core does not auto-emit `exception_raised` / `stale_topic` from rebuild or projection maintenance.
- `core/internal/server/staleness.go:9`-`:19` implements `emitStaleThreadExceptions` as a no-op under the dumb-thread model.
- `core/internal/server/projection_maintenance.go:161`-`:164` still runs `m.runStaleScan(ctx, now)` on an interval.
- `core/internal/server/projection_maintenance.go:264`-`:283` records `lastSuccessfulStaleScanAt` after the no-op scan.
- `core/cmd/anx-core/main.go:89` and `:152` still expose `ANX_PROJECTION_STALE_SCAN_INTERVAL` / `--projection-stale-scan-interval` for that no-op path.

## Proposed Fix
Remove the scheduled stale-scan branch, `StaleScanInterval` configuration, stale-scan state fields, and stale-scan ops-health timestamps while keeping explicit operator-authored `stale_topic` events supported in derived inbox projections. Keep rebuild focused on marking current canonical threads dirty rather than calling the no-op emitter.

## Validation
- `go test ./internal/server -run 'ProjectionMaintainer|Stale|Rebuild'`
- `go test ./cmd/anx-core/...`
- `make -C core check`

## Progress
- Removed the projection maintainer stale-scan interval, scheduled scan branch, scan health timestamp, and related CLI/env configuration.
- Removed rebuild calls to the no-op stale exception emitter while keeping derived projections based on explicit `stale_topic` events.
- Updated projection maintenance tests and operator/spec docs so `/ops/health` reports queue lag/errors without stale-scan state.
- Validation passed:
  - `go test ./internal/server -run 'ProjectionMaintainer|Stale|Rebuild'`
  - `go test ./cmd/anx-core/...`
  - `make -C core check`
