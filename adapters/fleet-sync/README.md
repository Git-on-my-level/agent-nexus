# fleet-sync

Read-only projector that mirrors fleet work into an Agent Nexus workspace and publishes one visual-report dashboard.

Sources are only listed or viewed. The adapter writes to Agent Nexus (`work create`, `work observations submit`, `docs create` / `docs revise`) and to its own state cache. It does not mutate Multica, GitHub, Hermes, agentctl, fleetctl, or remote hosts.

## Run

```sh
python3 adapters/fleet-sync/fleet_sync.py --dry-run
python3 adapters/fleet-sync/fleet_sync.py
python3 adapters/fleet-sync/fleet_sync.py --only multica,github
```

`--config` defaults to `~/.config/anx-fleet-sync/config.json`. `--state` defaults to `~/.local/state/anx-fleet-sync/state.json`. Copy `config.example.json` and fill in the operator's own workspace, boards, hosts, and paths. Do not commit that file.

`--dry-run` reads the selected sources, prints the planned ANX writes and the report JSON, and writes nothing.

## Identity and observations

Cards are registered with `source.authority`, `source.connection_id`, and `source.native_id`. Agent Nexus dedupes that triple. The adapter never matches by title and never creates a card whose phase is already `done` or `cancelled`.

Each observation uses `idempotency_key = sha256(native key + canonical facts JSON)` and the same value as `source_revision`. A submit is skipped when that digest is already in the state file. `facts.phase = done` always includes evidence with a `url` or `ref`. A failed read, or a truncated open list, does not close cards for that source. A paged done/cancelled archive does not by itself block closes of items that left a complete open list.

The state file is a cache. Losing it is safe for card identity: `work create` returns the existing card. Resubmitting the same observation body is an exact replay. Agent Nexus hashes the whole observation, including `observed_at`, so a fresh timestamp with the same key conflicts instead of replaying.

## Report

The dashboard is one `anx.visual-report` schema version 1 document titled "Fleet Dashboard" under the configured topic. It is validated with `node web-ui/scripts/validate-visual-report.mjs` before publish and again after readback. An invalid report is not published.

Host-local sources do not get a `sources` entry. The visual-report schema requires every source `url` to be an absolute HTTP(S) URL, so a missing URL cannot be omitted or set to null. Pull request and Multica issue URLs are cited where they exist. Host panels name the host in the table instead.

## Tests

```sh
python3 -m unittest discover -s adapters/fleet-sync/tests -v
```

Requires Python 3.11+. The report test shells `node` and skips when `node` is not installed.
