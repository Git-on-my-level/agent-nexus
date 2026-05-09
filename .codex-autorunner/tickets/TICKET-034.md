---
title: "Treat OpenClaw runtime failures as wake failures"
agent: codex
done: true
ticket_id: "tkt_bridge_openclaw_failures_are_wake_failures_034"
---

## Triage

Priority: P1. Process launch, timeout, and nonzero exit are operational failures. Posting them as normal agent replies hides failure from core and operators.

## Problem

Bundled OpenClaw launch, timeout, or nonzero exit can become a normal `response_text`, causing the bridge to post an error message as an agent reply and complete the wake.

## Evidence

- `adapters/agent-bridge/anx_agent_bridge/adapters/openclaw.py:111`-`:130` routes launch, timeout, and nonzero process outcomes through `_dispatch_error`.
- `adapters/agent-bridge/anx_agent_bridge/adapters/openclaw.py:149` returns those failures as successful adapter responses.
- `adapters/agent-bridge/anx_agent_bridge/bridge.py:191` marks wake failure only when adapter dispatch raises.
- Command used by scout: `nl -ba adapters/agent-bridge/anx_agent_bridge/adapters/openclaw.py adapters/agent-bridge/anx_agent_bridge/bridge.py`.

## Proposed Fix

Make fatal OpenClaw process failures raise or emit a contract-level adapter error that `SubprocessAdapter` maps to an exception. Reserve `response_text` for successful final assistant text.

## Validation

- OpenClaw tests for launch failure, timeout, and nonzero exit.
- Bridge test that those failures mark the wake failed rather than completing it with a reply.
- `make -C adapters/agent-bridge test`

## Progress

- Changed bundled OpenClaw dispatch so launch failures, timeouts, and nonzero OpenClaw exits raise runtime errors instead of returning them as `response_text`.
- Added OpenClaw regression tests for launch failure, timeout, and nonzero exit.
- Added a bridge regression test that runs the bundled OpenClaw adapter through `SubprocessAdapter` and verifies a failing OpenClaw process records a wake failure without posting a reply or completing the wake.

## Validation Run

- `adapters/agent-bridge/.venv/bin/python -m pytest tests/test_openclaw.py tests/test_bridge.py -q` (29 passed)
- `make -C adapters/agent-bridge test` (102 passed)
