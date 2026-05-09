---
title: "Reject trailing JSON in core request bodies"
agent: codex
done: true
ticket_id: "tkt_core_strict_json_body_020"
---

## Triage

Priority: P0. This is a narrow system-of-record validation fix with high leverage and low implementation ambiguity.

## Problem
Core's shared JSON body helpers accept the first JSON value and do not check for trailing non-whitespace data. A request body such as `{"actor_id":"a"}{"ignored":true}` can be accepted by any write handler using `decodeJSONBody` or `decodeJSONBodyAllowEmpty`, making API behavior surprising and weakening validation at the system-of-record boundary.

## Evidence
- `core/internal/server/request_controls.go:403` decodes once with `json.NewDecoder(r.Body).Decode(dst)` and returns success immediately.
- `core/internal/server/request_controls.go:419` has the same single-decode pattern for optional bodies.
- Command used: `nl -ba core/internal/server/request_controls.go | sed -n '400,430p'`.

## Proposed Fix
Update both helpers to reject trailing data after the first decoded JSON value, allowing only whitespace/EOF. Add focused handler tests that post concatenated JSON to at least one representative create/patch endpoint and assert `400 invalid_json`.

## Validation
- `go test ./internal/server -run 'JSON|Request|invalid_json'`
- `make -C core check`

## Progress
- Updated `decodeJSONBody` and `decodeJSONBodyAllowEmpty` to perform a second decode after the first JSON value and reject any trailing JSON value or non-whitespace data as `400 invalid_json`.
- Added `TestWriteHandlersRejectTrailingJSONBody` covering concatenated JSON against representative topic create and patch handlers.
- Validation passed:
  - `go test ./internal/server -run 'JSON|Request|invalid_json'`
  - `make -C core check`
