---
title: "Require x-anx streaming metadata to be an object with a valid mode"
agent: codex
done: true
ticket_id: "tkt_x_anx_streaming_shape_validation_025"
---

## Triage

Priority: P2. This is generator-side contract hygiene: malformed metadata should fail before it can leak into CLI/help artifacts.

## Problem

The x-anx authoring rules describe `x-anx-streaming` as required streaming metadata, and current operations use objects such as `{ mode: none }` or `{ mode: sse }`. Generator validation only checks that the field is non-nil, then validates `mode` only when the value already decodes as a map. A malformed value like `x-anx-streaming: sse` or `{}` can pass required-field validation and produce ambiguous generated command metadata.

## Evidence

- `core/cmd/contract-gen/main.go:542` treats `x-anx-streaming` as present whenever `op.Streaming != nil`.
- `core/cmd/contract-gen/main.go:574` validates `x-anx-streaming.mode` only when `xAnxStreamingMode(op.Streaming)` returns `ok`.
- `core/cmd/contract-gen/main.go:596`-`:607` returns `ok=false` for non-map streaming values and returns an empty mode for maps without a string `mode`, so both shapes avoid the allowed-value failure path.
- `core/cmd/contract-gen/main.go:1559` documents `x-anx-streaming` as a streaming metadata object, and the current OpenAPI file uses object values for the 124 command operations.

## Proposed Fix

Tighten `validateXAnxAllowedValues` so every command operation with `x-anx-streaming` must decode as an object containing a non-empty string `mode`, and that mode must be one of the allowed values. Report failures with the same method/path/command/field detail used for other x-anx validation issues.

Add focused generator tests covering a scalar `x-anx-streaming` value and an object missing `mode`.

## Validation

- `go test ./cmd/contract-gen` from `core/`.
- `./scripts/contract-check`.

## Progress

- Tightened `validateXAnxAllowedValues` so present `x-anx-streaming` metadata must be an object with a non-empty string `mode`, and the mode must be allowed.
- Added focused generator tests for scalar streaming metadata and object metadata missing `mode`.
- Validation passed:
  - `go test ./cmd/contract-gen` from `core/`
  - `./scripts/contract-check`
