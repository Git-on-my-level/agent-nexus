# fleet-sync

Read-only source ingestion into existing Agent Nexus initiatives and, by default, one visual-report dashboard.

Sources are only listed or viewed. The adapter writes to Agent Nexus (`cards revise`, `docs create` / `docs revise`) and to its own state cache. It does not mutate Multica, GitHub, Hermes, agentctl, fleetctl, Prometheus, remote hosts, inbox items, or initiative workflow state. Inbox and loose-end cards are read with `debug inbox list` and `work list --source nexus`.

## Run

```sh
python3 adapters/fleet-sync/fleet_sync.py --plan
python3 adapters/fleet-sync/fleet_sync.py
python3 adapters/fleet-sync/fleet_sync.py --only multica,github
python3 adapters/fleet-sync/fleet_sync.py --quiet
```

`--config` defaults to `~/.config/anx-fleet-sync/config.json`. `--state` defaults to `~/.local/state/anx-fleet-sync/state.json`. Copy `config.example.json` and fill in the operator's own workspace, mapping document, hosts, and paths. Do not commit that file.

The optional `report.publish` config key defaults to `true`, preserving the personal/fleet workspace dashboard. Set it to `false` in a product-workspace config to keep source ingestion and initiative updates while skipping report generation, validation, and dashboard document creation or revision. For example, add this to each workspace config such as `~/.config/anx-fleet-sync/omi/config.json` and `~/.config/anx-fleet-sync/anx/config.json`:

```json
"report": { "publish": false }
```

Do not add a workspace dashboard pin or default-dashboard setting; fleet-sync only manages its own report document when report publishing is enabled.

`--dry-run` reads the selected sources, prints the planned ANX writes and (when enabled) the report JSON, and writes nothing. With report publication disabled, its `report` and `report_valid` fields are `null`.

`--quiet` prints nothing when the run succeeds. It prints one line per reader that failed, and exits nonzero if any reader failed or the report failed validation. That is the mode an unattended job should use.

`fleetctl.ssh` runs `fleetctl status` on that host instead of the machine where the adapter is installed. The hub's contract and reports directories are the authority; a laptop copy of `~/.fleetctl/reports` is a replica and goes stale. `prometheus.ssh` is the same host. The alerts query is `curl -s -m 10` of the configured localhost URL, executed over ssh. Both ssh commands have a process timeout, and the whole run stops within five minutes.

`~/` in a remote config value is not expanded on the remote host. A path is passed through as a single quoted argument unless it is exactly `$HOME` followed by slash-separated path segments, which the remote shell expands. Write `$HOME/...` when the path should live in the remote home directory.

`node_bin` is the Node binary used to validate the visual report. Set it to a stable interpreter. An fnm shim on `PATH` is not stable in a non-interactive job. A copy installed outside the git checkout needs `repo_root` pointing at a checkout that contains `web-ui/scripts/validate-visual-report.mjs`. Running from the repository itself finds that script next to the adapter.

## Initiative mapping (required)

Recurring runs create **zero cards**. Each item becomes linked evidence on an
existing Nexus-owned initiative, an Unsorted sample, or a count for another workspace.
Operational readers also feed aggregate dashboard metrics and history. The old
per-item registration/observation writer has been removed; an old config without
`mapping_doc` fails closed before reading sources or writing anything.

Rules live in a versioned ANX document, **not** a hidden host-only routing file.
The host config supplies `mapping_doc: "doc:fleet-sync-mapping"`, credentials stay
with the workspace-local host, and the document contains plain JSON matching
`mapping.example.json`. Its `workspace` must exactly match the config's `base_url`
(without a trailing slash). Initiative targets are local `card:<handle>` refs; the adapter
verifies they exist, are active, and are Nexus-owned. It never creates targets.

Pins use the full `(authority, connection_id, native_id)` identity and win over
rules. Rules run top to bottom, first match wins. Fields within a rule are ANDed:
`authority`, `connection_id`, `project` (Multica project ID), `labels` (all exact
names), `repo` (exact owner/name), and `title_pattern` (Python regex). Unknown fields,
duplicate rule IDs/pins, invalid regexes, and a different workspace are errors.

Each rule or pin requires exactly one destination: `initiative` or `elsewhere`.
For example, `{"id":"omi-prs","elsewhere":"omi","match":{"authority":"github","repo":"Example/omi"}}`
declares ownership elsewhere. `elsewhere` is a plain workspace label: 1–63 lowercase
ASCII letters, digits, hyphens or underscores, starting with a letter or digit.
URLs, card refs, empty labels and mixed destinations are rejected. Pins still win
over rules, and rules still use first match. Place specific ownership rules before
broad local initiative rules.

Elsewhere-owned items contribute neither Unsorted items nor proposals. The
dashboard shows one count per destination (for example, “156 items belong to omi”);
`--plan` includes full membership in `planned_writes.elsewhere` and its total in
`counts.elsewhere`. Labels are never resolved to endpoints: the adapter performs
no reads or writes in those workspaces. Fleet-wide source metrics still describe
all sources read. Previously linked local evidence stays historical, and legacy
migration defers elsewhere-owned cards without archiving them.

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

Unmatched items stay in **one** Unsorted panel, with full item/suggestion counts,
the five largest suggestions (stable ties), and ten sample items. Sample text is
capped at 160 characters; full titles, URLs, identities and all suggestions are
available through `--plan` (also spelled `--dry-run`). Incomplete reads retain a
`+` count marker. Projects, repositories and every label are evaluated independently. Clusters
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
with the board, work-version and latest-observation fences checked atomically
by core. A new source observation, even with unchanged phase, rejects archive
with 409 and requires a fresh preview. Deploy a core supporting
`if_latest_observation_id` before applying either migrator. No purge, trash, source mutation, or agent
identity transfer occurs. A failed fold archives nothing; a later failure may
leave a safe partial batch. Re-running the same manifest resumes exact tombstones
and skips already archived cards. Changed cards fail closed. Archive retains
history, conversations and source identity; `anx cards restore <ref>` restores
visibility. Keep the manifest for audit and recovery.

The coordinator reviews and executes live hosted migrations. Producing the
manifest does not authorize its execution. See the design rationale in
`docs/architecture/fleet-sync-initiatives.md`.

## Retiring copies owned by another workspace

`retire_elsewhere.py` is a separate, dry-run-first command. Recurring fleet-sync
still never accesses another workspace. First ingest the source evidence into
existing initiatives in each owning workspace using that workspace's normal
writer. This command only **reads** destinations; it never folds or writes there.
Each destination uses its own configured `base_url` and locally enrolled `agent`.
No token is copied from the source workspace.

Use a source config with a `destinations` object, whose keys are reviewed workspace
labels and whose values contain `base_url`, `agent`, `mapping_doc` and optionally
`anx_binary`. The source config also needs those fields. Supply a reviewed JSON
array of candidates, each with `ref`, `destination` (a configured label), and
`source: {authority, connection_id, native_id}`. No implicit discovery or archive
of other cards occurs. Explicit candidates may supply ownership absent from old
observations; a conflicting published local/elsewhere route still defers them.

```sh
python3 adapters/fleet-sync/retire_elsewhere.py --config retirement-config.json \
  --candidates retirement-candidates.json --output retirement.json
# Coordinator only, after reviewing the manifest and stopping legacy writers:
python3 adapters/fleet-sync/retire_elsewhere.py --config retirement-config.json \
  --apply retirement.json --approve-digest <reviewed-digest>
```

The manifest records source fences, published mappings, destination endpoints,
initiative URLs, observed evidence revision/digest, and each deferred reason.
Unavailable destination credentials, missing evidence, unproven source ownership,
non-Nexus/inactive initiatives and unmatched destination routes remain deferred.
Apply never promotes a deferred row, even if evidence has since appeared: generate
and review a fresh manifest. Historic items outside the recurring reader window
must be deliberately ingested locally in their owning workspace before retirement.

Apply checks the full eligible batch before its first mutation and rechecks the
full source identity in destination evidence immediately before each source
archive. It preserves source bodies and annotations, adds a version-fenced
tombstone anchored to the source mapping document (with `destination_url` and a
Markdown link in its note), reads it back, and supplies the source board,
work-version and latest-observation archive fences. Core checks the observation
ID atomically; another poll after readback returns 409 without archiving. Both
published mappings are reread after tombstone/evidence readback immediately
before each archive; either differing from the manifest stops the batch.
Exact tombstones make retries resumable. Changed mappings, endpoints or source fences fail closed.
Destination reads and source writes cannot form an atomic transaction: concurrent
removal of destination evidence or a routing edit after the last read is still
possible. Quiesce
initiative edits during coordinator apply. New unrelated destination evidence
need not invalidate a reviewed manifest. No archive is authorized by generating
one; review the digest separately. Restore with `anx cards restore <ref>` in the
source workspace if needed.

## Report

The dashboard is one `anx.visual-report` schema version 1 document titled "Fleet Dashboard" under the configured topic. It is validated with `node web-ui/scripts/validate-visual-report.mjs` before publish and again after readback. An invalid report is not published.

The complete report, including Unsorted and destination counts, has a 96 KiB
UTF-8 budget, leaving headroom below the 128 KiB contract limit. Sizing, validation
and publishing use the same JSON encoding. Detail tables shrink with explicit
shown/total row counts when necessary; oversized charts can become an omission
notice. Overview counts and routing summaries are preserved. If those summaries
alone cannot fit, the run fails before any writes. The preview membership is
never shortened by dashboard sizing.

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
`python3 adapters/fleet-sync/tests/smoke_retire.py --core <anx-core> --cli <anx>`
checks retirement against two isolated cores with separate credentials, including
the source mapping relation and destination URL roundtrip, then removes both.

Requires Python 3.11+. The report test shells `node` and skips when `node` is not installed.
