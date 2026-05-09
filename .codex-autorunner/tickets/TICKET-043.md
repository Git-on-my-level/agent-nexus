---
title: "Deduplicate adapter construction in bridge CLI"
agent: opencode
done: true
ticket_id: "tkt_bridge_deduplicate_adapter_construction_043"
---

## Triage

Priority: P2. This is mechanical simplification with low architectural risk. It reduces future drift as adapter kinds grow.

## Problem

`build_adapter` repeats cwd resolution, env table conversion, `SubprocessAdapter` construction, and timeout handling across `subprocess`, `hermes`, and `openclaw` branches.

## Evidence

- `adapters/agent-bridge/anx_agent_bridge/cli.py:106` starts the subprocess branch.
- `adapters/agent-bridge/anx_agent_bridge/cli.py:143`-`:169` repeats the shape for Hermes.
- `adapters/agent-bridge/anx_agent_bridge/cli.py:170`-`:196` repeats the shape for OpenClaw.
- Command used by scout: `nl -ba adapters/agent-bridge/anx_agent_bridge/cli.py | sed -n '1,420p'`.

## Proposed Fix

Extract helpers for `resolve_adapter_cwd`, `adapter_env`, and `build_subprocess_adapter(...)` with per-kind defaults. Preserve existing config fields and adapter behavior.

## Validation

- Existing adapter CLI tests.
- Focused test that cwd/env defaults remain identical for subprocess, Hermes, and OpenClaw adapters.
- `make -C adapters/agent-bridge test`

## Done

Extracted three private helpers in `cli.py`:

- `_resolve_adapter_cwd(config)` — cwd resolution with relative-to-config_dir expansion
- `_adapter_env(config)` — env table dict conversion
- `_build_subprocess_adapter(config, *, command, default_timeout, doctor_command)` — SubprocessAdapter construction

`build_adapter` now delegates to `_build_subprocess_adapter` for all three subprocess-based kinds (`subprocess`, `hermes`, `openclaw`), differing only in command resolution and default timeout. Added parametrized test confirming cwd, env, and timeout defaults are identical across kinds, plus a test confirming relative cwd resolution matches. All 106 tests pass.

