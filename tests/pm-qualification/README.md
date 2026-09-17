# Unified work / PM qualification

Run from the repository root with Python 3.10+ and Go installed:

```sh
python3 tests/pm-qualification/qualify.py --suite baseline
python3 tests/pm-qualification/qualify.py --suite all --report /tmp/pm-qualification.local.json
```

`baseline` exercises real core and CLI authentication, rejects a token from a
second independently bootstrapped workspace, and crash-restarts core to check
durable board identity. `all` adds the work scenarios below. Missing endpoints
fail; they are never silently skipped. During parallel implementation, run
against a coordinator's integrated checkout using `--build-root <checkout>`.
The harness builds that checkout and records its Git head, dirtiness and binary
hashes. It does not modify its sources. A dirty checkout's HEAD alone does not
identify the tested source content.

Both workspaces bind only loopback, use temporary SQLite state and synthetic
principals, disable the execution sidecar, and are removed at exit. Tokens stay
in memory and child environment. The harness neither registers a profile in the
operator's home nor alters `HOME`. Core receives an allowlisted environment;
host credentials, proxy settings and auth bypass flags are not inherited.
Compilation uses a temporary Go cache and the Go installation selected by PATH,
without inheriting a potentially mismatched GOROOT. All child operations have
deadlines. Logs and raw responses are not part of the report.

This is **synthetic local evidence against real built binaries**. It does not
prove source collection, channel delivery, deployment, browser rendering or
generated-code isolation. The report retains an explicit `not_verified` list
even when all executable scenarios pass. See [acceptance.md](acceptance.md) for
the full product gates and [CAR reference cases](car-reference.md) for relevant
prior failure scenarios.

## Live web PM proof (real `make serve` + `anx pm serve`)

Against an already running stack (`CORE_PORT=8300`, `WEB_UI_PORT=8301`, and
`anx pm serve` with omp/glm-5.3 as printed by `make serve`):

```sh
PLAYWRIGHT_BROWSERS_PATH="$HOME/Library/Caches/ms-playwright" \
NODE_PATH=web-ui/node_modules \
ANX_LIVE_UI_URL=http://127.0.0.1:8301 \
ANX_LIVE_CORE_URL=http://127.0.0.1:8300 \
./web-ui/node_modules/.bin/playwright test --config tests/pm-qualification/playwright.live.config.js
```

The suite signs in as Maya through `POST /auth/dev/session`. `live_web_pm.spec.js`
asks "What needs my decision?" on `/pm` and checks that the delivered turn names a
seeded task and that Inbox Needs you shows the proposed decisions.
`live_source_drag.spec.js` drags a source-owned card on `/tasks?view=board` and
checks that a PM decision is proposed without mutating the task phase. Neither
spec starts servers.

## Executable scenarios

| Scenario | Boundary checked |
| --- | --- |
| baseline | Authenticated CLI read, missing/foreign workspace token rejection, durable identity after SIGKILL |
| authority | Canonical source tuple deduplication, equal-title independence, local external-status write rejection, API/CLI readback |
| replay | Duplicate report durable ID, source-sequence ordering, crash/replay persistence, changed-content replay conflict |
| outage | Expired evidence is stale, failed read retains last good observation and visible failure, restart retention |
| attempt_ordering | An unreachable-source error with no source sequence remains visible after sequenced evidence |
| completion | Completed run with no acceptance evidence does not complete work; client cannot self-verify |
| views | Work appears in card board and work list, inbox read does not mutate it, dates/relations survive |
| pm_boundaries | Real core/CLI durable PM proposal replay, agent approval denial, pending restart, explicit missing-provider error |

An assertion failure is a finding to investigate, not permission to weaken the
assertion. First distinguish a contract-shape mismatch from a product invariant
failure. Preserve baseline failing evidence before applying a fix. For example,
`/work` returning 404 on the pre-feature baseline is an expected missing-feature
failure, not a successful qualification.

## Independent public-service seam tests

```sh
python3 tests/pm-qualification/check_pm.py --build-root <integrated-checkout> --report /tmp/pm-service.local.json
```

This runner creates an isolated temporary Go module and reuses the integrated
core's actual PM, observation, primitive and SQLite packages. It checks unchanged
source rereads versus delivery replay, canonical event emission, human discovery
of agent proposals, originating-agent receipt discovery, unknown action recovery
without resend, stale approval denial, and exact channel identity/deduplication
after reopening SQLite. No peer-owned tests or module files are modified.

Identity policy, source action, channel and bridge callbacks are synthetic.
These service tests complement the real-binary suite; they do not prove real
transport authentication or channel delivery. Known regressions remain failures
until the owning lane fixes the behavior; there is no expected-failure bypass.

## Real-source reads

`probe_sources.py` performs opt-in, bounded, read-only probes through the already
configured `gh`, `multica` and `ssh` executables. It never logs in, configures a
bot, consumes Telegram updates, posts a message, writes an upstream issue,
copies a credential, installs a dependency or changes a remote checkout.

```sh
python3 tests/pm-qualification/probe_sources.py --github-repo OWNER/REPO --github-issue NUMBER
python3 tests/pm-qualification/probe_sources.py --multica-profile PROFILE --multica-workspace WORKSPACE_ID --multica-issue ISSUE_ID
python3 tests/pm-qualification/probe_sources.py --ssh-host APPROVED_ALIAS --ssh-repo /approved/absolute/repository
```

Select targets from the current authorized inventory. SSH uses an existing
known-host binding, disables connection sharing and forwarding, and fails on
unknown keys. A successful SSH Git revision read is only code-state evidence.
An unreachable path is recorded as a failure without suggesting the task is
healthy. No connection implies permission to test external writes.

Probe output includes selected source IDs/URLs or remote paths so it is
reviewable, but excludes titles, descriptions, people, messages and tokens.
Keep private source metadata in private evidence storage; review/redact before
placing any report in a public PR. API connectivity probes do not prove the new
Nexus collector imported the source or preserved its authority.

For an actual built-reader -> disposable core -> built CLI import qualification:

```sh
python3 tests/pm-qualification/qualify_source.py --build-root <integrated-checkout> --config <approved-anx-observe-config.json> --report /tmp/real-source-import.local.json
```

This uses the observation lane's existing operator configuration format and
credential handles. It performs one real read, registers the corresponding
source-owned commitment only in temporary local core, checks API/CLI parity,
duplicate replay, reported verification and crash persistence, then deletes the
temporary tracker. Source titles/bodies and credentials are omitted from the
report. The target is fingerprinted; retain the private operator configuration
separately for exact provenance. This is real-source import into a synthetic
local workspace, not a production deployment or remote-CLI/browser proof.

For an SSH source, add `--controlled-ssh-outage` to repeat the read using a
temporary empty known-hosts file. The host/path and strict verification remain
unchanged, and operator files are untouched. The runner requires that read to
fail, maps a labeled failure observation into temporary core, and verifies
last-good evidence plus visible failure survive restart. Failure mapping is
performed by the harness; this does not certify the production scheduler.
