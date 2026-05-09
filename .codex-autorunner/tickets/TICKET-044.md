---
title: "Refresh stale web UI contract version docs"
agent: opencode
done: true
ticket_id: "tkt_webui_refresh_contract_version_docs_044"
---

## Triage

Priority: P3. This is documentation cleanup, but stale contract versions confuse onboarding and contract alignment.

## Problem

Web UI docs still reference schema/spec `0.2.3` while the active contract is `0.6.0`.

## Evidence

- `web-ui/README.md:6` references schema contract `0.2.3`.
- `web-ui/src/lib/config.js:1` expects `0.6.0`.
- `contracts/anx-schema.yaml:8` declares the canonical schema as `0.6.0`.
- Additional stale references were found in `web-ui/docs/spec-compliance.md`, `web-ui/docs/anx-ui-spec.md`, and `web-ui/docs/http-api.md`.
- Command used by scout: `rg -n "0\\.2\\.3|0\\.6\\.0" web-ui contracts/anx-schema.yaml`.

## Proposed Fix

Update stale web-ui docs to reference `0.6.0` or avoid hardcoding schema versions where generated contract docs are authoritative.

## Validation

- `rg -n "0\\.2\\.3" web-ui`
- `make -C web-ui check` if docs tooling is included in the check.

