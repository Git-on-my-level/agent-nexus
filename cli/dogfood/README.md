# CLI dogfood (local)

## `.anx-import` inputs

Import fixtures under `cli/internal/app/.anx-import/` are **local-only** and gitignored. They are not checked in.

To exercise import flows, generate or copy inputs on your machine (for example a stable `inventory.jsonl` and `scan-summary.json` with placeholder workspace paths and fixed timestamps for deterministic tests). See `agent-nexus/cli` runbook or import-related tests for the expected shape.
