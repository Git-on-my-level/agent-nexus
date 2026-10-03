# fleet-sync

Read-only projector that mirrors fleet work into an Agent Nexus workspace and publishes one visual-report dashboard.

Sources are only listed or viewed. The adapter writes to Agent Nexus (`work create`, `work observations submit`, `docs create` / `docs revise`) and to its own state cache. It does not mutate Multica, GitHub, Hermes, agentctl, fleetctl, Prometheus, remote hosts, inbox items, or native cards. Inbox and loose-end cards are read with `debug inbox list` and `work list --source nexus`.

## Run

```sh
python3 adapters/fleet-sync/fleet_sync.py --dry-run
python3 adapters/fleet-sync/fleet_sync.py
python3 adapters/fleet-sync/fleet_sync.py --only multica,github
python3 adapters/fleet-sync/fleet_sync.py --quiet
```

`--config` defaults to `~/.config/anx-fleet-sync/config.json`. `--state` defaults to `~/.local/state/anx-fleet-sync/state.json`. Copy `config.example.json` and fill in the operator's own workspace, boards, hosts, and paths. Do not commit that file.

`--dry-run` reads the selected sources, prints the planned ANX writes and the report JSON, and writes nothing.

`--quiet` prints nothing when the run succeeds. It prints one line per reader that failed, and exits nonzero if any reader failed or the report failed validation. That is the mode an unattended job should use.

`fleetctl.ssh` runs `fleetctl status` on that host instead of the machine where the adapter is installed. The hub's contract and reports directories are the authority; a laptop copy of `~/.fleetctl/reports` is a replica and goes stale. `prometheus.ssh` is the same host. The alerts query is `curl -s -m 10` of the configured localhost URL, executed over ssh. Both ssh commands have a process timeout, and the whole run stops within five minutes.

`~/` in a remote config value is not expanded on the remote host. A path is passed through as a single quoted argument unless it is exactly `$HOME` followed by slash-separated path segments, which the remote shell expands. Write `$HOME/...` when the path should live in the remote home directory.

`node_bin` is the Node binary used to validate the visual report. Set it to a stable interpreter. An fnm shim on `PATH` is not stable in a non-interactive job. A copy installed outside the git checkout needs `repo_root` pointing at a checkout that contains `web-ui/scripts/validate-visual-report.mjs`. Running from the repository itself finds that script next to the adapter.

## Identity and observations

Cards are registered with `source.authority`, `source.connection_id`, and `source.native_id`. Agent Nexus dedupes that triple. The adapter never matches by title and never creates a card whose phase is already `done` or `cancelled`.

Each observation uses `idempotency_key = sha256(native key + canonical facts JSON)` and the same value as `source_revision`. A submit is skipped when that digest is already in the state file. `facts.phase = done` always includes evidence with a `url` or `ref`. A failed read, or a truncated open list, does not close cards for that source. A paged done/cancelled archive does not by itself block closes of items that left a complete open list.

The state file is a cache. Losing it is safe for card identity: `work create` returns the existing card. While merging that list, a missing cache digest is filled from the card's latest observation idempotency key, which is the facts digest. Unchanged facts are then skipped and no new `observed_at` is sent.

A 409 still happens when that key is already stored but is not the latest observation, or when the latest observation was not on the card payload. The same idempotency key means the facts digest is ours and only the body hash differs, usually because `observed_at` moved. That conflict is treated as already recorded: one log line, the state cache is updated, and the run continues. Replaying a new timestamp on purpose would require a preflight read of every card, so the adapter does not do that. The list merge covers the common lost-cache case; the 409 path covers the rest.

The state file also keeps the last 50 headline metric samples, each stamped with the run's real time. The dashboard draws a sparkline only after two samples exist.

## Report

The dashboard is one `anx.visual-report` schema version 1 document titled "Fleet Dashboard" under the configured topic. It is validated with `node web-ui/scripts/validate-visual-report.mjs` before publish and again after readback. An invalid report is not published.

Host-local sources do not get a `sources` entry. The visual-report schema requires every source `url` to be an absolute HTTP(S) URL, so a missing URL cannot be omitted or set to null. Pull request URLs and Multica issue URLs are cited where they exist. A Multica issue URL is `{app_url}/{workspace_slug}/issues/{id}`, the same path `multica issue url` prints and the web app builds as `issueDetail`. The adapter derives it from config because the Multica CLI's `app_url` setting is optional and may be unset. Host panels name the host in the table instead.

Prometheus firing alerts are cards on the ops board (`authority` `prometheus`). Pending alerts are counted on the dashboard and do not become cards. A failed Prometheus or fleetctl read is an unavailable panel. It is not reported as zero alerts or a healthy fleet.

The "Needs <operator> now" panel is a short ranked brief: counts, and at most three examples per line. Examples are identifiers (an issue key or `repo#n`) or a full title. A title is never cut mid-word. Loose ends are counts only: how many cards are on the decisions board, and how many are waiting on the operator, with a pointer to the Loose ends table. The name comes from `operator_name` in config and defaults to Operator. The per-item tables stay on the other tabs. "Decisions waiting" is the open inbox count. "Decisions for <operator>" adds red CI, requested changes, and paused watchdogs. The red-CI line names how many recently updated pull requests were checked, because CI is not fetched for every open pull request. Multica reviews older than 72 hours are "Aging reviews" and are not added into that decision count. Loose-end cards are listed and not registered again.

A headline count taken from an incomplete read is shown with a trailing `+` and the detail says the count is a lower bound. `unknown` still means the source was not read, which is not zero.

## Unattended run

Install a stable copy, not a git checkout the job can lose:

```sh
mkdir -p ~/.local/share/anx-fleet-sync
cp adapters/fleet-sync/*.py ~/.local/share/anx-fleet-sync/
cp -R adapters/fleet-sync/readers ~/.local/share/anx-fleet-sync/readers
```

Wrapper `~/.local/share/anx-fleet-sync/run.sh`:

```sh
#!/bin/sh
exec /usr/bin/python3 "$HOME/.local/share/anx-fleet-sync/fleet_sync.py" --quiet
```

Register it with Hermes as a no-agent cron. This repository does not install the job:

```sh
hermes cron create --no-agent --script "$HOME/.local/share/anx-fleet-sync/run.sh" --deliver local --name anx-fleet-sync "every 15m"
```

## Tests

```sh
python3 -m unittest discover -s adapters/fleet-sync/tests -v
```

Requires Python 3.11+. The report test shells `node` and skips when `node` is not installed.
