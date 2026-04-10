# PR #170 clean-slate review — execution notes

Standalone summary of review execution (April 2026). The detailed checklist lives in the review plan; this file records outcomes and fixes applied in-repo.

## Current alignment (clean-slate model)

The repo documentation and operator surfaces now treat **topics**, **cards**, **boards**, and **documents** as the primary resources. **Threads** remain backing infrastructure (timelines, packet subject resolution, read-only inspection in the workspace contract). Older alternate resource names have been dropped from module specs and architecture docs in favor of topic/card vocabulary.

## Fixes applied during review (historical)

1. **`web-ui/src/lib/config.js`** — `EXPECTED_SCHEMA_VERSION` updated to match `contracts/oar-schema.yaml` and `contracts/oar-openapi.yaml`.

2. **`web-ui/src/lib/oarCoreClient.js`** — Client methods that called non-existent generated helpers were switched to direct JSON/raw invocation where needed. Thread reads use **`threads.inspect`**; document history/revision paths use generated docs revision commands where registered. **`ackInboxItem`** posts to **`POST /inbox/ack`** with the core-shaped body.

3. **`web-ui/tests/unit/oarCoreClient.test.js`** — Mock handshake/version payloads updated to match the contract schema version.

4. **`web-ui/AGENTS.md`** — Terminology aligned with the topic/card model.

5. **`POST /docs/{document_id}/revisions`** — Implemented in core so POST matches OpenAPI and CLI envelopes.

6. **`web-ui/src/lib/oarCoreClient.js` — `purgeCard`** — Aligned with `withActorId` like other card lifecycle calls.

## Verification run

- `make contract-check`
- `make -C core check`
- `make cli-check`
- `make -C web-ui check`
- `make check`
- `./scripts/e2e-smoke`

## Contract / OpenAPI spot-check

- `oar-openapi.yaml`: **`/topics`**, **`/cards`**, boards, documents; threads exposed as **read-only** list/inspect/timeline/context/workspace (no thread create/patch in the workspace contract).
- `oar-schema.yaml`: topic/card/board/document resources and typed ref prefixes; contract prose matches the current resource set.

## Previously documented gaps — now closed or superseded

| Former gap | Resolution |
|------------|------------|
| Web UI legacy panels tied to removed resource types | Removed; UI and tests use topic/card flows. |
| Docs mixing old mutable-thread semantics with the operator model | Architecture, core, and web-ui specs updated to topic/card/board/document language; threads described as backing only. |
| Seed script bridging removed ref/event types | Removed from `web-ui/scripts/seed-core-from-mock.mjs`; mock data is expected to use current ref types and event types. |

## Remaining follow-ups (if any)

- **CLI discoverability:** `threads` subcommand help/registry may still under-list commands that exist in code — reconcile help metadata vs `runThreadsCommand` when touching CLI docs.
- **Schema/doc version strings:** Example `schema_version` lines in samples may drift from runtime; prefer `/version` and handshake for truth.

### How to repeat this review style

1. Spawn scoped agents (contracts-only, core-only, web-ui-only, CLI).
2. Require verbatim evidence (paths, `rg` patterns, or test names) for each finding.
3. Treat “all todos green” as insufficient — cross-check OpenAPI path × handler and CLI body × schema for write routes.
