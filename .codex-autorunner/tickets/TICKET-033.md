---
title: "Reject misleading multi-core bridge workspace configs"
agent: codex
done: true
ticket_id: "tkt_bridge_multicore_workspace_config_033"
---

## Triage

Priority: P1. Bridge config should describe what runtime can actually poll. Multi-workspace config that silently collapses to one base URL is architecture drift.

## Problem

`wake.toml` supports multiple workspace entries with separate `base_url`, but runtime builds one `ANXClient` from the primary workspace only. Registration/check-in signs all workspace ids, so cross-core configs can advertise support the bridge cannot honor.

## Evidence

- `adapters/agent-bridge/anx_agent_bridge/config.py:35` and `:148` model workspace-level `base_url`.
- `adapters/agent-bridge/anx_agent_bridge/config.py:195`-`:198` selects only the primary workspace base URL into `config.anx.base_url`.
- `adapters/agent-bridge/anx_agent_bridge/cli.py:30` constructs the runtime client with that single URL.
- Command used by scout: `nl -ba adapters/agent-bridge/anx_agent_bridge/config.py adapters/agent-bridge/anx_agent_bridge/cli.py`.

## Proposed Fix

Either reject enabled workspaces with different `base_url` for now, or implement per-workspace clients and drain loops. Keep durable binding by `workspace_id`, not slug. Prefer the rejection path unless there is an immediate product need for true multi-core polling.

## Validation

- Config tests for mixed `base_url` behavior.
- Runtime test that advertised workspace ids match pollable clients.
- `make -C adapters/agent-bridge test`

## Progress

- Added config validation that rejects enabled `wake.toml` workspaces spanning multiple Agent Nexus `base_url` values.
- Runtime config now uses the enabled primary workspace `base_url`, preserving the single-client polling model.
- Added config coverage for explicit primary `base_url`, same-core multi-workspace configs, mixed-core rejection, and disabled workspace exclusion.
- Added bridge check-in coverage showing registration/check-in advertise the workspace ids pollable through the single configured core.
- Validation passed: `make -C adapters/agent-bridge test` (98 tests).
