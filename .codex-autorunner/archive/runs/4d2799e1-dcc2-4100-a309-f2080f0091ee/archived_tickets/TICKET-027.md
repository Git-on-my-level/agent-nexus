---
title: "Clean generated contract output directories before regeneration"
agent: codex
done: true
ticket_id: "tkt_contract_gen_stale_artifacts_027"
---

## Triage

Priority: P3. This is useful generated-artifact hygiene, but it is lower urgency than runtime correctness and stale operator-state issues.

## Problem

`scripts/contract-gen` rewrites current generated files in place, but it does not remove obsolete files from generated output directories before running the generator and mirror copies. If a generated artifact is renamed or removed later, the stale committed file can remain in `contracts/gen/`, `cli/docs/generated/`, `cli/internal/registry/`, or `web-ui/src/lib/generated/` without being produced by the generator anymore.

## Evidence

- `scripts/contract-gen:7` runs `go run ./cmd/contract-gen` directly against `../contracts/gen`.
- `scripts/contract-gen:21`-`:37` copies generated mirrors into CLI and web-ui paths with `cp`, but never removes stale generated files in those destination directories.
- `scripts/contract-check:38` calls `./scripts/contract-gen`; `--committed` then runs `git diff` against generated paths, which detects changed tracked files but not obsolete tracked generated files that were never deleted.
- Commands used while scouting: `sed -n '1,80p' scripts/contract-gen` and `sed -n '1,80p' scripts/contract-check`.

## Proposed Fix

Make regeneration authoritative by cleaning only known generated-output directories/files before writing them. Keep the cleanup narrowly scoped to derived outputs, for example `contracts/gen/{go,ts,meta,docs}`, `cli/docs/generated`, the specific generated registry JSON files under `cli/internal/registry`, and the specific generated JSON mirrors under `web-ui/src/lib/generated`.

Avoid deleting hand-authored parent directories. Consider generating into a temporary directory and syncing with delete semantics if that is less error-prone.

## Validation

- Add a focused regression check or script test that creates a fake stale file under a generated directory, runs `./scripts/contract-gen`, and asserts the stale file is removed.
- Run `./scripts/contract-check --committed`.

## Progress

- Updated `scripts/contract-gen` to clean authoritative generated output before regeneration:
  - removes and recreates `contracts/gen/{go,ts,meta,docs}`;
  - deletes generated JSON mirrors in `cli/internal/registry` and `web-ui/src/lib/generated`;
  - deletes contract-generated Markdown mirrors in `cli/docs/generated` while preserving `runtime-help.md`.
- Updated `scripts/contract-check --committed` to diff generated mirror file classes instead of only today's filenames, so deleted stale mirrors are reported.
- Added `scripts/test-contract-gen-cleanup`, which seeds stale files into each cleaned output area, runs `./scripts/contract-gen`, and fails if any stale artifact remains.
- Validated with `./scripts/test-contract-gen-cleanup`.
- Validated with `./scripts/contract-check --committed`.
