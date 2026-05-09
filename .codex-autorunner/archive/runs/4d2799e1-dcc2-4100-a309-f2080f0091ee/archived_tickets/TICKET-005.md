---
title: "Include taxonomy mirrors in committed contract drift checks"
agent: codex
done: true
ticket_id: "tkt_contract_taxonomy_drift_001"
---

## Triage

Priority: P1. This is a small generated-artifact drift guard that protects CLI and UI consumers from stale contract mirrors.

## Problem

`scripts/contract-gen` mirrors generated taxonomy metadata into CLI and web UI generated directories, but `scripts/contract-check --committed` does not verify those mirrored taxonomy files. A stale `cli/internal/registry/taxonomy.json` or `web-ui/src/lib/generated/taxonomy.json` can slip through the committed drift check even though they are derived contract outputs.

## Evidence

- `scripts/contract-gen:21-30` copies:
  - `contracts/gen/meta/taxonomy.json` to `cli/internal/registry/taxonomy.json`
  - `contracts/gen/meta/taxonomy.json` to `web-ui/src/lib/generated/taxonomy.json`
- `scripts/contract-check:40-44` checks drift for `contracts/gen`, selected CLI registry files, selected CLI docs, and `web-ui/src/lib/generated/event_ref_rules.json`, but omits both taxonomy mirror files.
- Command used: `nl -ba scripts/contract-gen | sed -n '15,36p'` and `nl -ba scripts/contract-check | sed -n '36,48p'`.

## Proposed Fix

Add both generated taxonomy mirror paths to the `--committed` drift check:

- `cli/internal/registry/taxonomy.json`
- `web-ui/src/lib/generated/taxonomy.json`

Consider centralizing the generated mirror path list in `scripts/contract-check` to reduce future omissions.

## Validation

- Run `./scripts/contract-check --committed`.
- As a focused regression check, temporarily edit one taxonomy mirror file and confirm `./scripts/contract-check --committed` fails after regeneration if the committed file remains stale.

## Progress

- Updated `scripts/contract-check` so committed drift checks use one `GENERATED_MIRROR_PATHS` list.
- Added both taxonomy mirror files to the committed drift check:
  - `cli/internal/registry/taxonomy.json`
  - `web-ui/src/lib/generated/taxonomy.json`
- Validation passed: `./scripts/contract-check --committed`.
- Focused regression passed: staged a deliberately stale `cli/internal/registry/taxonomy.json`, reran `./scripts/contract-check --committed`, and confirmed it failed on the taxonomy mirror diff. Cleaned up the temporary staged change and reran the committed check successfully.
