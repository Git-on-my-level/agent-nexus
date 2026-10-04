# fleet-sync

Read-only source ingestion into existing Agent Nexus initiatives and one visual-report dashboard.

Sources are only listed or viewed. The adapter writes to Agent Nexus (`cards revise`, `docs create` / `docs revise`) and to its own state cache. It does not mutate Multica, GitHub, Hermes, agentctl, fleetctl, Prometheus, remote hosts, inbox items, or initiative workflow state. Inbox and loose-end cards are read with `debug inbox list` and `work list --source nexus`.

## Run

```sh
python3 adapters/fleet-sync/fleet_sync.py --plan
python3 adapters/fleet-sync/fleet_sync.py
python3 adapters/fleet-sync/fleet_sync.py --only multica,github
python3 adapters/fleet-sync/fleet_sync.py --quiet
```

`--config` defaults to `~/.config/anx-fleet-sync/config.json`. `--state` defaults to `~/.local/state/anx-fleet-sync/state.json`. Copy `config.example.json` and fill in the operator's own workspace, mapping document, hosts, and paths. Do not commit that file.

`--dry-run` reads the selected sources, prints the planned ANX writes and the report JSON, and writes nothing.

`--quiet` prints nothing when the run succeeds. It prints one line per reader that failed, and exits nonzero if any reader failed or the report failed validation. That is the mode an unattended job should use.

`fleetctl.ssh` runs `fleetctl status` on that host instead of the machine where the adapter is installed. The hub's contract and reports directories are the authority; a laptop copy of `~/.fleetctl/reports` is a replica and goes stale. `prometheus.ssh` is the same host. The alerts query is `curl -s -m 10` of the configured localhost URL, executed over ssh. Both ssh commands have a process timeout, and the whole run stops within five minutes.

`~/` in a remote config value is not expanded on the remote host. A path is passed through as a single quoted argument unless it is exactly `$HOME` followed by slash-separated path segments, which the remote shell expands. Write `$HOME/...` when the path should live in the remote home directory.

`node_bin` is the Node binary used to validate the visual report. Set it to a stable interpreter. An fnm shim on `PATH` is not stable in a non-interactive job. A copy installed outside the git checkout needs `repo_root` pointing at a checkout that contains `web-ui/scripts/validate-visual-report.mjs`. Running from the repository itself finds that script next to the adapter.

## Initiative mapping (required)

Recurring runs create **zero cards**. Each item becomes linked evidence on an
existing Nexus-owned initiative, or a row in the single Unsorted dashboard panel.
Operational readers also feed aggregate dashboard metrics and history. The old
per-item registration/observation writer has been removed; an old config without
`mapping_doc` fails closed before reading sources or writing anything.

Rules live in a versioned ANX document, **not** a hidden host-only routing file.
The host config supplies `mapping_doc: "doc:fleet-sync-mapping"`, credentials stay
with the workspace-local host, and the document contains plain JSON matching
`mapping.example.json`. Its `workspace` must exactly match the config's `base_url`
(without a trailing slash). Targets are local `card:<handle>` refs; the adapter
verifies they exist, are active, and are Nexus-owned. It never creates targets.

Pins use the full `(authority, connection_id, native_id)` identity and win over
rules. Rules run top to bottom, first match wins. Fields within a rule are ANDed:
`authority`, `connection_id`, `project` (Multica project ID), `labels` (all exact
names), `repo` (exact owner/name), and `title_pattern` (Python regex). Unknown fields,
duplicate rule IDs/pins, invalid regexes, and a different workspace are errors.

To add a rule:

1. Find the existing initiative with `anx --workspace <alias> work list --source nexus`.
2. Read `anx --workspace <alias> docs get doc:fleet-sync-mapping --json` and save the
   revision content as JSON. Add the narrowest matching rule or explicit item pin.
3. Preview with `python3 adapters/fleet-sync/fleet_sync.py --config config.json
   --plan --mapping-file mapping.json`. This reads destination cards and prints
   each initiative's exact revision body, item membership, matching rule, Unsorted
   items, proposals, and the complete dashboard. It writes neither ANX nor cache.
4. Publish the reviewed rules with `anx --workspace <alias> docs revise
   doc:fleet-sync-mapping --apply --body-file mapping.json`. For initial setup use
   `docs create --topic <topic> --title "Fleet sync mapping" --body-file mapping.json`
   and put the returned ref in host config.
5. Run normally. `--mapping-file` is preview-only; writes always read the ANX doc.

Evidence is a collapsed, adapter-marked block on each initiative. The adapter
merges by source identity, preserves human text, acceptance criteria, refs and
provenance, and supplies `if_base_revision` to reject concurrent edits. Unchanged
facts produce no revision even after local cache loss. Partial reads and items
missing from a later source window never delete historical evidence or mark an
initiative done. Migration links survive future updates. Rerouting adds evidence
to the new initiative; the former initiative retains its historical evidence.
The existing initiative plan and core-derived state are preserved. An exact
source URL/step-ref match already links the source; ingestion adds no separate
step association and never edits steps or their statuses. Do not hand-edit the marked block. A malformed block stops the run.

Unmatched items stay in **one** Unsorted panel, with a full count, at most 199
listed rows plus an explicit overflow row (the report contract caps tables at
200). `--plan` always includes the complete list. Projects, repositories and every label are evaluated independently. Clusters
with at least five members qualify (configurable); overlapping qualifying
clusters, including transitive overlaps, share one suggestion with deduplicated
items and all dimensions recorded in the preview. Disjoint groups and source
connections remain separate. Sub-threshold clusters never combine to qualify. No card or inbox ask is created.
Humans/agents deliberately choose an existing initiative or create a justified
new one, then add a rule. Source/host alone is not enough to propose a cluster.

## Legacy migration (separate, dry-run first)

```sh
python3 adapters/fleet-sync/migrate.py --config config.json \
  --mapping-file proposed-mapping.json --output migration.json
# Coordinator only, after reviewing and publishing the exact proposed rules:
python3 adapters/fleet-sync/migrate.py --config config.json \
  --apply migration.json --approve-digest <digest-from-reviewed-manifest>
```

Only cards whose latest observation proves `fleet-sync/<authority>` ownership
are candidates. The manifest contains full source identities, card refs, proposed
destinations, revision fences, Unsorted/proposal details and a digest. Unmatched
cards are deferred, never archived speculatively. Legacy observations did not
retain Multica project/label metadata; use explicit pins/title rules for those
cards, or enrich from a separately reviewed source export. Do not infer a project
from an assignee. Re-run the preview if source observations changed since review.

Stop/upgrade every old fleet-sync writer before migration. Run one migrator per
workspace. Apply requires the exact reviewed digest, the same workspace, and the
same mapping published in ANX. It folds and reads back evidence first, preserves
the original detail-card body and adds a version-fenced destination/digest relation tombstone, then archives
with the board concurrency fence. No purge, trash, source mutation, or agent
identity transfer occurs. A failed fold archives nothing; a later failure may
leave a safe partial batch. Re-running the same manifest resumes exact tombstones
and skips already archived cards. Changed cards fail closed. Archive retains
history, conversations and source identity; `anx cards restore <ref>` restores
visibility. Keep the manifest for audit and recovery.

The coordinator reviews and executes live hosted migrations. Producing the
manifest does not authorize its execution. See the design rationale in
`docs/architecture/fleet-sync-initiatives.md`.

## Report

The dashboard is one `anx.visual-report` schema version 1 document titled "Fleet Dashboard" under the configured topic. It is validated with `node web-ui/scripts/validate-visual-report.mjs` before publish and again after readback. An invalid report is not published.

Host-local sources do not get a `sources` entry. The visual-report schema requires every source `url` to be an absolute HTTP(S) URL, so a missing URL cannot be omitted or set to null. Pull request URLs and Multica issue URLs are cited where they exist. A Multica issue URL is `{app_url}/{workspace_slug}/issues/{id}`, the same path `multica issue url` prints and the web app builds as `issueDetail`. The adapter derives it from config because the Multica CLI's `app_url` setting is optional and may be unset. Host panels name the host in the table instead.

Prometheus firing alerts contribute initiative evidence or Unsorted rows. Pending alerts are counted on the dashboard. A failed Prometheus or fleetctl read is an unavailable panel. It is not reported as zero alerts or a healthy fleet.

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

`make fleet-sync-check` runs the same suite and is included in `make check`.
For the isolated real-core/CLI migration smoke, build both binaries and run
`python3 adapters/fleet-sync/tests/smoke_local.py --core <anx-core> --cli <anx>`.
It uses a temporary workspace and cleans up its server.

Requires Python 3.11+. The report test shells `node` and skips when `node` is not installed.
