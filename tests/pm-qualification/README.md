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

## Executable scenarios

| Scenario | Boundary checked |
| --- | --- |
| baseline | Authenticated CLI read, missing/foreign workspace token rejection, durable identity after SIGKILL |
| authority | Canonical source tuple deduplication, equal-title independence, local external-status write rejection, API/CLI readback |
| replay | Duplicate report durable ID, source-sequence ordering, crash/replay persistence, changed-content replay conflict |
| outage | Expired evidence is stale, failed read retains last good observation and visible failure, restart retention |
| completion | Completed run with no acceptance evidence does not complete work; client cannot self-verify |
| views | Work appears in card board and work list, inbox read does not mutate it, dates/relations survive |

An assertion failure is a finding to investigate, not permission to weaken the
assertion. First distinguish a contract-shape mismatch from a product invariant
failure. Preserve baseline failing evidence before applying a fix. For example,
`/work` returning 404 on the pre-feature baseline is an expected missing-feature
failure, not a successful qualification.

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
