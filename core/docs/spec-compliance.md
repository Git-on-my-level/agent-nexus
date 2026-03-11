# oar-core Spec Compliance (v0.2.2)

Last updated: 2026-03-05

This checklist maps key requirements from:
- `docs/oar-core-spec.md`
- `../contracts/oar-schema.yaml`

For each item, it points to implementation code, validating tests, and any known gap.

## Legend

- `Implemented`: requirement is enforced in code and covered by tests.
- `Partial`: partially implemented; see noted gap.

## Compliance Matrix

| Requirement | Source | Implementation | Tests | Status / Gap |
|---|---|---|---|---|
| Workspace init creates SQLite + filesystem layout and is idempotent | Spec §2.1 | `internal/storage/workspace.go`, `internal/storage/migrations.go` | `internal/storage/workspace_test.go` | Implemented |
| Health/version endpoints expose local readiness + schema version | Spec §7, §11 | `internal/server/handler.go`, `cmd/oar-core/main.go` | `internal/server/handler_test.go`, `internal/storage/workspace_test.go` | Implemented |
| Compatibility handshake + version headers + generated metadata discovery endpoints (`/meta/handshake`, `/meta/commands*`, `/meta/concepts*`) | CLI spec draft §2.1–§2.4 | `internal/server/handler.go`, `internal/server/meta_handlers.go`, `cmd/oar-core/main.go` | `internal/server/meta_stream_integration_test.go` | Implemented |
| SSE streaming endpoints (`/events/stream`, `/inbox/stream`) with `Last-Event-ID` resume | CLI spec draft §1.1, §2.4 | `internal/server/handler.go`, `internal/server/stream_handlers.go` | `internal/server/meta_stream_integration_test.go` | Implemented |
| CLI compatibility rejection path returns stable `cli_outdated` payload with upgrade hints | CLI spec draft §0, §1.1 | `internal/server/handler.go` | `internal/server/meta_stream_integration_test.go` | Implemented |
| Schema contract loader exposes version, enums, typed-ref prefixes, provenance, packet + reference conventions | Spec §2.2, schema root | `internal/schema/schema.go`, `internal/schema/version.go` | `internal/schema/contract_test.go`, `internal/schema/version_test.go` | Implemented |
| Strict enums reject unknown values; open enums accept unknown values | Spec §2.2, schema `enums.*.enum_policy` | `internal/schema/validator.go`, write handlers in `internal/server/*.go` | `internal/schema/validator_test.go`, `internal/server/primitives_integration_test.go` | Implemented |
| Typed refs must be `<prefix>:<value>`; unknown prefixes preserved | Spec §3.1, §10; schema `ref_format.rules` | `internal/schema/validator.go`, write handlers in `internal/server/*.go` | `internal/schema/validator_test.go`, `internal/server/primitives_integration_test.go` | Implemented |
| Provenance shape enforced (`sources`, optional `notes`, optional `by_field`) | Spec §8.1; schema `provenance.fields` | `internal/schema/validator.go`, thread/commitment/event handlers | `internal/schema/validator_test.go` | Implemented |
| Actor registry exists; mutating endpoints reject unknown `actor_id` for unauthenticated callers | Spec §6 | `internal/actors/store.go`, `internal/server/primitives_handlers.go` (`requireRegisteredActorID`) | `internal/server/actor_integration_test.go`, broader integration tests in `internal/server/*_integration_test.go` | Implemented |
| Agent auth lifecycle (register, assertion login, refresh rotation, key rotation, self-revoke) | Spec draft auth ticket scope | `internal/auth/store.go`, `internal/server/auth_handlers.go`, `internal/storage/migrations.go` | `internal/server/auth_integration_test.go` | Implemented |
| Authenticated principal maps to actor identity and can omit `actor_id` on writes; mismatches are rejected | Spec draft auth ticket scope | `internal/server/auth_handlers.go` (`resolveWriteActorID`), write handlers in `internal/server/*.go` | `internal/server/auth_integration_test.go` | Implemented |
| Events are append-only; unknown event types accepted and stored | Spec §3.1, §11 | `internal/primitives/store.go` (`AppendEvent`), `internal/server/primitives_handlers.go` | `internal/server/primitives_integration_test.go`, `internal/server/event_reference_conventions_integration_test.go` | Implemented |
| Snapshot patch/merge preserves unknown fields; list fields replace wholesale when present | Spec §2.3 | `internal/primitives/store.go` (`PatchSnapshot`, `PatchThread`, `PatchCommitment`) | `internal/server/threads_integration_test.go`, `internal/server/api_comprehensive_integration_test.go` | Implemented |
| Snapshot mutation emits `snapshot_updated` with `snapshot:<id>` and `changed_fields` | Spec §3.2, §10 | `internal/primitives/store.go` (`PatchSnapshot`, thread/commitment flows) | `internal/server/threads_integration_test.go`, `internal/server/api_comprehensive_integration_test.go` | Implemented |
| Create-heavy writes support replay-safe `request_key` retries without duplicate thread/commitment/doc/packet/event writes | Spec §3.1, §7, retry-safety ticket scope | `internal/server/idempotency.go`, create handlers in `internal/server/*.go`, `internal/primitives/idempotency_store.go` | `internal/server/primitives_integration_test.go`, `internal/server/commitments_integration_test.go`, `internal/server/packets_integration_test.go` | Implemented |
| Thread/commitment snapshot mutations and their emitted collaboration events commit together or fail together | Spec §3.2, §4.1, §4.2 | `internal/primitives/store.go` (`PatchSnapshot`, `CreateThread`, `CreateCommitment`, `PatchCommitment`, `recomputeThreadOpenCommitmentsTx`) | `internal/server/threads_integration_test.go`, `internal/server/commitments_integration_test.go` | Implemented |
| Thread snapshot rules + API (`POST/GET/PATCH/list/timeline`) | Spec §4.1, §7.1, §7.2 | `internal/server/threads_handlers.go`, `internal/primitives/store.go` | `internal/server/threads_integration_test.go` | Implemented |
| Document lifecycle operations append durable collaboration events and thread timelines expand document/document_revision refs | Spec §3.1, §7.1, §7.2 | `internal/primitives/docs_store.go`, `internal/server/docs_handlers.go`, `internal/server/threads_handlers.go`, `internal/server/stream_handlers.go` | `internal/server/threads_integration_test.go`, `internal/server/meta_stream_integration_test.go` | Implemented |
| Thread-scoped document listing and `threads.context` document summaries expose linked docs with head revision metadata | Spec §4.1, §7.1, §7.2 | `internal/primitives/docs_store.go`, `internal/server/docs_handlers.go`, `internal/server/threads_handlers.go` | `internal/server/primitives_integration_test.go`, `internal/server/threads_integration_test.go` | Implemented |
| `open_commitments` is core-maintained and client-writes are rejected | Spec §4.1 | `internal/server/threads_handlers.go`, `internal/primitives/store.go` (`recomputeThreadOpenCommitments`) | `internal/server/threads_integration_test.go`, `internal/server/commitments_integration_test.go` | Implemented |
| Commitment create/patch + restricted transitions (`done`/`canceled`) + evidence refs | Spec §4.2, §8.2 | `internal/server/commitments_handlers.go`, `internal/primitives/store.go` (`enforceRestrictedCommitmentTransition`) | `internal/server/commitments_integration_test.go`, `internal/server/api_comprehensive_integration_test.go` | Implemented |
| Restricted status updates annotate `provenance.by_field.status` | Spec §8.1, §8.2 | `internal/primitives/store.go` (`statusEvidenceLabels`, commitment patch path) | `internal/server/commitments_integration_test.go`, `internal/server/api_comprehensive_integration_test.go` | Implemented |
| Core-emitted actor-caused events use `actor_statement:<event_id>`; only system-derived stale exceptions remain `inferred` | Spec §8.1, §9 | `internal/primitives/store.go`, `internal/server/packet_convenience_handlers.go`, `internal/server/inbox_handlers.go`, `internal/server/staleness.go` | `internal/primitives/store_test.go`, `internal/server/threads_integration_test.go`, `internal/server/commitments_integration_test.go`, `internal/server/packets_integration_test.go`, `internal/server/inbox_integration_test.go`, `internal/server/staleness_integration_test.go` | Implemented |
| Artifact metadata+content CRUD; content immutable by create-only path | Spec §3.3, §7.1, §7.2 | `internal/primitives/store.go` (`CreateArtifact`, `GetArtifact`, `GetArtifactContent`, `ListArtifacts`), `internal/server/primitives_handlers.go` | `internal/server/primitives_integration_test.go` | Implemented |
| Packet validation: required fields, typed-ref fields, packet ID = artifact ID, receipt min-items | Spec §5.1–§5.4; schema `packets.*` | `internal/server/packet_validation.go`, `internal/server/primitives_handlers.go` | `internal/server/packets_integration_test.go` | Implemented |
| Convenience endpoints create packet artifacts and corresponding events with required refs | Spec §5.2–§5.4, §7.3; schema `reference_conventions.event_refs` | `internal/server/packet_convenience_handlers.go` | `internal/server/packets_integration_test.go`, `internal/server/api_comprehensive_integration_test.go` | Implemented |
| Direct `POST /events` enforces event reference conventions for known event types | Spec §10 | `internal/server/event_reference_validation.go`, `internal/server/primitives_handlers.go` | `internal/server/event_reference_conventions_integration_test.go` | Implemented |
| Inbox derived view with deterministic IDs + ack suppression/retrigger | Spec §7.4; schema `derived.inbox_item`, `derived.inbox_derivation_rules` | `internal/server/inbox_handlers.go` | `internal/server/inbox_logic_test.go`, `internal/server/inbox_integration_test.go`, `internal/server/api_comprehensive_integration_test.go` | Implemented |
| Staleness detection + `exception_raised` (`stale_thread`) + idempotent emission | Spec §9 | `internal/server/staleness.go`, `internal/server/inbox_handlers.go` | `internal/server/staleness_test.go`, `internal/server/staleness_integration_test.go`, `internal/server/derived_rebuild_integration_test.go` | Implemented |
| Derived rebuild endpoint is idempotent and does not duplicate stale exceptions | Spec §7.4 | `internal/server/inbox_handlers.go` (`handleRebuildDerived`) | `internal/server/derived_rebuild_integration_test.go` | Implemented |
| Full end-to-end API workflow remains green in one integration path | Spec §7, §8, §10 | Multiple server/store modules | `internal/server/api_comprehensive_integration_test.go` | Implemented |

## Known Gaps / Follow-up Candidates

1. The spec text mentions a "record decision convenience operation" in §7.3.  
   - Current implementation supports equivalent behavior through `POST /events` with `decision_needed` / `decision_made`.
   - No dedicated convenience endpoint exists today.
