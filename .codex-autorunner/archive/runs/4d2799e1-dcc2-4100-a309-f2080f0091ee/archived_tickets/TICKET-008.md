---
title: "Narrow bridge auth refresh fallback errors"
agent: codex
done: true
ticket_id: "tkt_bridge_auth_refresh_044"
---

## Triage

Priority: P1. Auth fallback behavior affects unattended bridge recovery and should distinguish real credential failures from transport or server problems.

## Problem

Bridge auth refresh falls back from refresh-token grant to assertion grant on any exception. This masks transport errors, server errors, JSON shape errors, and other non-auth failures as if the refresh token were invalid. In unattended bridge runtimes, that can produce misleading recovery behavior and make auth/profile problems harder to diagnose.

## Evidence

- `adapters/agent-bridge/anx_agent_bridge/auth.py` catches broad `Exception` around `client.raw_request("POST", "/auth/token", refresh_token...)` and immediately retries with `assertion_payload()`.
- `adapters/agent-bridge/anx_agent_bridge/anx_client.py` exposes `ANXClientError` with `status_code`, `code`, and payload fields that could distinguish invalid refresh credentials from network or server failures.
- `rg -n "refresh\\(|refresh_token|assertion_payload|AuthManager" adapters/agent-bridge/tests` shows no focused tests for refresh-token fallback behavior.

## Proposed Fix

Catch only expected auth failures for refresh-token fallback, such as `ANXClientError` with a 400/401 auth-specific code (`invalid_token`, `invalid_grant`, or the core equivalent). Preserve transport, 5xx, malformed response, and unexpected exceptions. Add tests for both paths: invalid refresh token falls back to assertion; connect/server/unexpected errors do not.

## Validation

- Passed: `cd adapters/agent-bridge && .venv/bin/python -m pytest tests/test_anx_client.py tests/test_cli.py tests/test_auth.py`
- Passed: `make bridge-test`

## Progress

- Replaced broad refresh-token fallback handling with `ANXClientError`-only fallback for 400/401 `invalid_token` and `invalid_grant`.
- Updated bridge client error decoding to read core-style nested `error.code` / `error.message` payloads.
- Added focused auth refresh tests covering invalid-refresh fallback and preservation of server, session, and unexpected errors.
