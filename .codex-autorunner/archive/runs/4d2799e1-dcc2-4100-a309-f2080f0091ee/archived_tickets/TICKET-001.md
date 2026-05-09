---
title: "Resolve event lifecycle mutation versus append-only invariant"
agent: codex
done: true
ticket_id: "tkt_core_event_append_only_024"
---

## Triage

Priority: P0. This is first because it challenges a core audit invariant; the team should decide and encode the event lifecycle model before building more behavior on top of it.

## Problem
Core guidance says events are append-only and corrections are new records, but event archive/trash/restore operations mutate rows in `events` by setting or clearing lifecycle columns. That makes event history partly mutable and creates ambiguity for audit consumers about whether event lifecycle changes are themselves durable facts.

## Evidence
- `core/AGENTS.md:36` states: "Events are append-only. Corrections are new records, not edits in place."
- `core/internal/primitives/store.go:1247` updates `events.archived_at` / `archived_by` in place.
- `core/internal/primitives/store.go:1403` updates `events.trashed_at`, `trashed_by`, `trash_reason`, and clears archive fields in place.
- Command used: `nl -ba core/internal/primitives/store.go | sed -n '1191,1250p'; nl -ba core/internal/primitives/store.go | sed -n '1348,1406p'; nl -ba core/AGENTS.md | sed -n '34,42p'`.

## Proposed Fix
Decide and encode the invariant explicitly. If events must be immutable, move event lifecycle changes into appended lifecycle/correction events or a separate event-lifecycle table with its own audit trail, then adjust readers to project lifecycle state. If in-place lifecycle columns are intended, update the core invariant/spec and add tests that define which event fields may mutate.

## Validation
- Add regression tests around event archive/trash/restore that assert the chosen invariant.
- `go test ./internal/primitives -run 'Event.*Archive|Event.*Trash|AppendOnly'`
- `go test ./internal/server -run 'Event.*Archive|Event.*Trash'`
- `make -C core check`

Completed:
- `go test ./internal/primitives -run 'Event.*Archive|Event.*Trash|AppendOnly|EventLifecycle'`
- `go test ./internal/server -run 'Event.*Archive|Event.*Trash|EventLifecycle'`
- `make -C core check`

## Resolution
Chose the bounded mutable lifecycle-column model and encoded it explicitly:
- Event identity, ordering, refs, and payload content remain append-only.
- Event lifecycle visibility columns (`archived_at`, `archived_by`, `trashed_at`, `trashed_by`, `trash_reason`) are the documented mutable exception used for filtered views.
- Added store and HTTP regression tests that archive/trash/restore events while asserting append-only event content is preserved and lifecycle operations do not append replacement event rows.
