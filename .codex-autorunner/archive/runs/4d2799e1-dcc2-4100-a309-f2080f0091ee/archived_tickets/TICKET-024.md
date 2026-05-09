---
title: "Retire legacy non-/stream SSE route registrations"
agent: codex
done: true
ticket_id: "tkt_core_stream_route_mount_drift_024"
---

## Triage

Priority: P2. Streaming route registration is now an explicit core invariant; the remaining legacy aliases should be either retired or clearly fenced as compatibility.

## Problem
Core now has the `internal/server/stream.Mount` helper and `/stream/` prefix invariant, but three legacy SSE routes are still registered through the normal route wrapper. That leaves streaming endpoints split across two routing models and keeps contract/docs/catalog metadata pointed at the older non-`/stream` paths.

## Evidence
- `core/AGENTS.md:62` says long-lived streaming endpoints must be registered via `internal/server/stream.Mount`.
- `core/internal/server/stream/stream.go:9`-`:14` defines `/stream/` as the single prefix for long-lived endpoints.
- `core/internal/server/handler.go:2052` registers `/stream/events` via `registerStreamRoute`, but `core/internal/server/handler.go:2059` also registers `/events/stream` through `registerRoute`.
- `core/internal/server/handler.go:2364` and `:2381` register `/stream/inbox` and `/stream/agent-notification-receipts`, while `:2371` and `:2388` keep `/inbox/stream` and `/agent-notification-receipts/stream` as normal routes.
- `core/docs/http-api.md:91` documents `/stream/events` and `/stream/inbox`, while generated command metadata still lists the legacy paths.

## Proposed Fix
Choose one compatibility policy and encode it explicitly. Prefer making `/stream/*` the canonical streaming surface, moving OpenAPI/generated command metadata and core docs to those paths, and either removing legacy non-`/stream` registrations or marking them as deprecated compatibility aliases with tests that keep them out of new generated command metadata.

## Validation
- `go test ./internal/server -run 'Stream|Route|OpenAPI|Parity'`
- `make contract-gen`
- `make contract-check`
- `make -C core check`

## Completion Notes
- Chose the clean canonical policy: `/stream/*` is the only mounted SSE surface for events, inbox, and agent notification receipt streams.
- Removed the legacy `/events/stream`, `/inbox/stream`, and `/agent-notification-receipts/stream` normal route registrations from core.
- Moved OpenAPI and generated command/client metadata to `/stream/events`, `/stream/inbox`, and `/stream/agent-notification-receipts`.
- Updated CLI and web UI stream call sites, tests, docs, and generated runtime help to use the canonical paths.
- Extended core route/OpenAPI parity coverage so `registerStreamRoute(...)` entries must be present in generated command metadata.

Validated:
- `go test ./internal/server -run 'Stream|Route|OpenAPI|Parity'`
- `make contract-gen`
- `make contract-check`
- `make -C core check`
- `make cli-check`
- `make -C web-ui check`
