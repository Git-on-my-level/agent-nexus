---
title: "Quote hosted env files without sourcing generated data"
agent: codex
done: true
ticket_id: "tkt_hosted_safe_env_file_parser_035"
---

## Triage

Priority: P1. Hosted scripts touch secrets and operator-provided values. Shell-sourcing generated data is fragile and can become unsafe when values contain shell syntax.

## Problem

Hosted scripts write values directly into shell-style env and manifest files, then `source` env files. Values containing spaces, `#`, `$()`, quotes, or newlines can break provision/restore behavior or execute when sourced.

## Evidence

- `scripts/hosted/provision-workspace.sh:100` accepts S3 credential arguments from operators.
- `scripts/hosted/common.sh:367` emits env lines without robust shell quoting.
- `scripts/hosted/provision-workspace.sh:241` embeds those values into generated env files.
- `scripts/hosted/common.sh:596` loads dotenv files with `source`.
- Command used by scout: `nl -ba scripts/hosted/common.sh scripts/hosted/provision-workspace.sh`.

## Proposed Fix

Add one env-file writer/parser that either shell-quotes values safely or parses dotenv files without executing them. Keep manifests data-only. Update provisioning and restore paths to use the shared helper.

## Validation

- Hosted ops tests with S3 secrets containing spaces, `#`, quotes, and `$()`.
- `scripts/hosted/test-hosted-ops.sh`

## Progress

- Added shared hosted env/manifest writers that single-quote values before writing data files.
- Replaced hosted dotenv loading with a parser/export path that does not `source` generated files.
- Updated provision, backup, restore manifests, restore receipts, quota upserts, and blob env/metadata emitters to use the shared writer.
- Extended hosted ops coverage for S3 credential values containing spaces, `#`, single/double quotes, literal `$()`, and an embedded newline, including backup-with-secrets and restore propagation.
- Validation passed: `scripts/hosted/test-hosted-ops.sh`.
