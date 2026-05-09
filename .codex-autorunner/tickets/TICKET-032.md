---
title: "Do not let bridge local state preempt core writeback"
agent: codex
done: true
ticket_id: "tkt_bridge_writeback_before_local_handled_032"
---

## Triage

Priority: P1. The bridge must not let adapter-local state become more authoritative than core wake status. This can drop replies after partial failures.

## Problem

A successful adapter dispatch is marked handled locally before reply posting and wake completion succeed. If reply posting or `complete_agent_wakeup` fails, the bridge records failure but the local handled state can suppress future retries.

## Evidence

- `adapters/agent-bridge/anx_agent_bridge/bridge.py:152`-`:155` short-circuits already handled wakeups.
- `adapters/agent-bridge/anx_agent_bridge/bridge.py:215` marks dispatch consumed before core writeback.
- `adapters/agent-bridge/anx_agent_bridge/bridge.py:218`-`:222` posts the reply and completes the wake after the local marker.
- Command used by scout: `nl -ba adapters/agent-bridge/anx_agent_bridge/bridge.py | sed -n '1,460p'`.

## Proposed Fix

Move local handled-state persistence after core reply posting and `complete_agent_wakeup` both succeed. For partial writeback, rely on idempotent `message_request_key` and core wake status rather than local suppression.

## Validation

- Add a bridge unit test where adapter dispatch succeeds but `complete_agent_wakeup` fails; the next drain should retry writeback rather than skip as handled.
- `make -C adapters/agent-bridge test`

## Progress

- Moved bridge local handled-state persistence after reply posting and `complete_agent_wakeup` succeed.
- Updated bridge tests so completion/writeback failures do not locally suppress later drains.
- Validation passed: `make -C adapters/agent-bridge test` (93 passed).
