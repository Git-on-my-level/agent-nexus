---
title: "Table-drive the web UI core client adapter"
agent: codex
done: true
ticket_id: "tkt_webui_table_drive_core_client_adapter_040"
---

## Triage

Priority: P2. The UI should remain contract-aligned without maintaining a second hand-written API surface for every command.

## Problem

`createAnxCoreClient` wraps most generated client methods manually and also keeps direct raw paths. This creates drift risk between the generated contract, actor injection rules, proxy behavior, and route code.

## Evidence

- `web-ui/src/lib/anxCoreClient.js:1` imports the generated client and command registry.
- `web-ui/src/lib/anxCoreClient.js:475`-`:1114` contains a long manual wrapper block.
- `web-ui/src/lib/anxCoreClient.js:607`, `:758`, and `:1009` retain direct raw calls for streams, attachments/artifact content, and inbox respond.
- Command used by scout: `nl -ba web-ui/src/lib/anxCoreClient.js`.

## Proposed Fix

Generate or table-drive the UI adapter surface from `commandRegistry`, with explicit overrides only for raw streaming, blob, and multipart cases. Use one actor-body injection path for all write commands.

## Validation

- `tests/unit/anxCoreClient.test.js`
- `tests/unit/anxCoreClientAuth.test.js`
- `tests/unit/proxyContractParity.test.js`
- `tests/unit/directCoreProxyParity.test.js`
- `make contract-check`
- `make -C web-ui check`

## Progress

Completed:

- Replaced the hand-written generated-method wrapper surface in `web-ui/src/lib/anxCoreClient.js` with a table-driven adapter keyed by contract command IDs.
- Centralized generated command dispatch, generated method lookup, path parameter handling, JSON parsing, request error normalization, and actor-body injection for actor-aware writes.
- Kept explicit raw overrides only for SSE streams, multipart artifact attachment upload, and artifact content/blob reads.
- Moved `respondInboxItem` back through the generated `inbox.respond` command while preserving validation and actor injection.

Validation run:

- `pnpm exec vitest run tests/unit/anxCoreClient.test.js tests/unit/anxCoreClientAuth.test.js tests/unit/coreClientWorkspaceWrites.integration.test.js tests/unit/proxyContractParity.test.js tests/unit/directCoreProxyParity.test.js`
- `make contract-check`
- `make -C web-ui check`
