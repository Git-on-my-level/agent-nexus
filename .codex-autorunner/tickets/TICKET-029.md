---
title: "Enforce known typed-ref prefixes in core writes"
agent: codex
done: true
ticket_id: "tkt_core_enforce_typed_ref_prefixes_029"
---

## Triage

Priority: P1. Typed refs are the cross-module identity language. Unknown prefixes passing validation weakens the contract boundary and lets clients persist references that no consumer can interpret reliably.

## Problem

Core loads the schema contract's typed-ref prefix vocabulary, but write validation only checks that refs look like `<prefix>:<value>`. Unknown prefixes can pass request validation even though `contracts/anx-schema.yaml` defines the durable prefix set.

## Evidence

- `contracts/anx-schema.yaml:106`-`:120` defines the typed-ref prefix contract.
- `core/internal/schema/schema.go:214`-`:223` loads those prefixes into `Contract.TypedRefPrefixes`.
- `core/internal/schema/validator.go:47`-`:50` accepts a contract argument but currently ignores it in `ValidateTypedRef`.
- `core/internal/schema/validator.go:52`-`:58` validates ref lists without checking prefix membership.
- Command used by scout: `nl -ba contracts/anx-schema.yaml core/internal/schema/schema.go core/internal/schema/validator.go`.

## Proposed Fix

Make `ValidateTypedRef` reject unknown prefixes when a contract is provided. Keep pure shape validation only for explicit nil-contract callers, or remove nil-contract use from write paths. Add tests for accepted schema prefixes and rejected unknown prefixes on topic, document, board, and card writes.

## Validation

- Focused schema validator tests for known and unknown prefixes.
- Focused HTTP/store write tests for invalid refs on major resource writes.
- `make -C core check`

## Progress

- Updated core typed-ref validation to reject prefixes not listed by the loaded schema contract while preserving shape-only validation for nil-contract callers.
- Added `actor:<id>` to the canonical typed-ref prefix vocabulary because existing core write paths persist `actor:` owner/assignee refs.
- Extended card create, batch create, and patch handlers to validate related/resolution refs against the contract.
- Added validator coverage for all schema prefixes and unknown prefixes, plus HTTP coverage rejecting unknown prefixes on topic, document, board, and card writes.

## Completed Validation

- `make contract-gen`
- `make contract-check`
- `go test ./internal/schema`
- `go test ./internal/server -run 'Test(NamedResourceAPIsUsePublicRefsAndHandles|InvalidTypedRefsRejectedForEventsAndArtifacts|UnknownTypedRefPrefixesRejectedForResourceWrites|EventReferenceConventionsRejectUnknownEventType)$'`
- `make -C core check`
