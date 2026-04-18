# Deprecation and removal schedule

This document tracks compatibility shims and legacy surfaces called out in the consolidation plan. **No fixed dates** until a release policy assigns them; items are ordered by typical dependency (CLI scripts and env first, schema/data last).

## CLI

| Surface | Replacement | Removal gate |
|--------|-------------|--------------|
| `compat_aliases.go` old command shapes | Current `anx` subcommands per generated registry | Major version or documented breaking window + changelog |
| `--reconnect` on stream commands | `--follow` | Telemetry or deprecation notice in help for one release cycle |
| `anx bridge workspace-id --document-id agentreg.<handle>` | `--handle <handle>` | Same as above |

## Web UI

| Surface | Replacement | Removal gate |
|--------|-------------|--------------|
| `ANX_PROJECTS` / `ANX_DEFAULT_PROJECT` | `ANX_WORKSPACES` / `ANX_DEFAULT_WORKSPACE` | Operator comms; no removal until hosted/docs stop referencing aliases |
| Header `x-oar-project-slug` | Workspace slug header used by current auth | Same |

## Schema and data

| Surface | Replacement | Removal gate |
|--------|-------------|--------------|
| Legacy cadence preset strings (`daily`, `weekly`, …) | Cron expressions per contract | Stored snapshots migrated; contract may tighten |
| Bridge TOML `[router]` section | Core-embedded router; `[agent]` bridge config | Already ignored; remove docs references when safe |

## Process

1. Announce in release notes with **deprecated** and **removes in** when dates exist.
2. Prefer one breaking batch per major version for CLI flags and env names.
3. Run `make check` and targeted `cli` / `web-ui` / `core` checks before deleting shims.
