---
title: "Make bridge config loading side-effect free"
agent: codex
done: true
ticket_id: "tkt_bridge_config_no_mkdir_043"
---

## Triage

Priority: P1. Read-only bridge commands should not mutate local state; fixing this reduces automation surprise and setup drift.

## Problem

`anx_agent_bridge.config.load_config` creates directories while resolving paths. Read-only commands such as `anx-agent-bridge registration status`, `bridge doctor`, and config validation can mutate the agent home by creating auth/state directories even when they only need to inspect configuration. That makes preflight behavior less safe for automation and can hide setup drift by materializing missing directories too early.

## Evidence

- `adapters/agent-bridge/anx_agent_bridge/config.py` calls `_expand_path` during config load.
- `_expand_path` calls `ensure_dir(path.parent if path.suffix else path)` unconditionally.
- `load_config` uses `_expand_path` for `agent_home`, `wake_config_path`, `auth_state_path`, and `state_dir`.
- `adapters/agent-bridge/anx_agent_bridge/cli.py` calls `load_config` for read-style commands including `registration status`, `bridge doctor`, and notification listing.
- Read-only command used while scouting: `rg -n "load_config\\(|ensure_dir|_expand_path" adapters/agent-bridge/anx_agent_bridge adapters/agent-bridge/tests`.

## Proposed Fix

Split path expansion from directory creation. Make `load_config` only resolve paths and validate required existing files. Move directory creation to write/runtime paths such as auth registration/import, state-store initialization, and bridge run startup. Add a regression test that calls `load_config` with a missing auth/state directory and asserts no new directories are created by config loading alone.

## Validation

- `cd adapters/agent-bridge && pytest tests/test_config.py`
- `make bridge-test`

## Progress

- Removed directory creation from bridge config path expansion; `load_config` now resolves paths without materializing auth/state directories.
- Kept write/runtime directory creation in existing write paths (`atomic_write_json`) and runtime state initialization (`JSONStateStore`).
- Added regression coverage asserting missing auth and runtime state directories are not created by config loading alone.
- Validation passed:
  - `cd adapters/agent-bridge && .venv/bin/python -m pytest tests/test_config.py`
  - `make bridge-test`
