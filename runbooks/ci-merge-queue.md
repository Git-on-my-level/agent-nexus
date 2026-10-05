# CI and merge queue setup

The coordinator applies these settings after the PR introducing `ci-ok` has
merged and a successful main run has advertised that check. This document does
not change repository settings. Auto-merge is already enabled on the repository;
do not use it until `ci-ok` exists and the main ruleset requires it.

## Queue eligibility prerequisite

[GitHub requires an organization-owned public repository for merge queues](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue).
The repository API reports `owner.type: User` as of 2026-10-05. Therefore the
coordinator can apply the required-check and squash settings now, but cannot
enable the queue on this owner type. An administrator must first decide whether
to move the repository to an eligible organization. No transfer or settings
change is part of this PR. The queue settings below apply after eligibility is
resolved; the workflow triggers are ready in advance.

Check the owner type before attempting queue setup:

```sh
gh api repos/OWNER/REPO --jq '.owner.type'
```

## Main ruleset

In **Settings → Rules → Rulesets**, create a branch ruleset named `main CI and
merge queue`:

- Enforcement: **Active**. Target: **Include default branch** (`main`).
- Bypass list: empty. Preserve any existing review requirements; do not weaken
  them when replacing per-job required checks.
- Enable **Restrict deletions**, **Block force pushes**, and **Require a pull
  request before merging**. Allowed PR merge method: **Squash** only.
- Enable **Require status checks to pass**. Add exactly `ci-ok`, with expected
  source **GitHub Actions**. Remove the individual path-filtered CI jobs from
  required checks. Enable **Require branches to be up to date before merging**
  while the queue is unavailable or inactive. Once the queue is enabled, turn
  this option off: the queue validates the combined commit against the latest base.
- Once eligible, enable **Require merge queue**. Merge method: **Squash**. Build concurrency:
  **2**. Minimum group size: **1**. Maximum group size: **1**. Minimum group wait:
  **0 minutes**. Status check timeout: **20 minutes**. Require every queued PR
  to pass required checks (do not allow a failing PR merely because the combined
  group is green).

In **Settings → General → Pull Requests**, enable squash merging and disable
merge commits and rebase merging. Keep auto-merge enabled. Once enabled, the rule
requires the queue; while unavailable, strict required checks protect main. The
coordinator controls when PRs are reviewed and queued.

Check for older overlapping branch protections/rulesets. Keep their review and
security requirements, but replace obsolete individual CI requirements with
`ci-ok` so path skips cannot leave the queue waiting forever.

GitHub documents the [merge queue settings](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue)
and [ruleset options](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets).

## What the gate covers

`ci-ok` runs with `always()` and depends on every job in `CI`, including every
browser and cross-platform matrix entry. It requires successful change detection;
all other jobs must be successful or intentionally skipped. Failed, cancelled,
or unrecognized results fail the gate. The automation test checks dependency
coverage so adding a job without updating the gate fails CI.

Both `CI` and `System Smokes` run on `merge_group: checks_requested`. CI retains
path filtering; dorny/paths-filter v4 defaults to the merge group's base/head
SHAs. System Smokes runs its baseline smokes and end-to-end smoke on queued
groups. Its checks are separate from `ci-ok` and are **not required** by this
ruleset. Scheduled and manual smoke runs cannot hold up PR CI. If system smokes
must later become a merge requirement, that needs a separate always-reporting
gate on both PR and merge-group events before changing the ruleset.

Concurrency is keyed by PR number or full ref, so different merge groups do not
cancel one another. A superseded run may be cancelled; its `ci-ok` cannot pass.

## Browser timing and coverage

`scripts/ci-e2e.mjs` discovers the full current Playwright suite, then uses
longest-first scheduling with `.github/e2e-timings.json` to balance five shards.
Files stay together, preserving serial suites and including the base-path
project. Timing data is a weight, never an allowlist. New files receive the
median measured time per test multiplied by their discovered test count.
New tests in existing files run automatically; refresh measurements when their
cost changes substantially.

```sh
node scripts/ci-e2e.mjs 1/5 --plan
node --test scripts/tests/ci-automation.test.mjs
CI=1 node scripts/ci-e2e.mjs 1/5
```

The automation test asks Playwright to list each shard and verifies that their
union equals the full suite exactly, with no duplicate tests or omitted project.
CI uploads a Playwright JSON timing report per shard, including on failure.
When refreshing weights, sum successful first-attempt `result.duration` values
by `spec.file` across all five reports; exclude retries and failed attempts.
Review the reports and update the source run URL/revision with the weights.

The pnpm composite already caches the pnpm store using the workspace lockfile
and installs with `--frozen-lockfile`; it does not cache `node_modules`.
The Playwright composite caches `~/.cache/ms-playwright` by OS, architecture,
exact Playwright version and Chromium. It installs browsers only on a cache
miss and installs Linux system libraries on every run. Browser installation
therefore stays correct on both warm and cold runners.

## Initial measurements

Baseline: [main run 37276158568](https://github.com/Git-on-my-level/agent-nexus/actions/runs/37276158568),
revision `5b6003b979dc75cda9beab3b781bd86f13224dde`, 2026-10-05.
All 440 tests passed. Browser job durations were **7m11s, 7m25s, 4m04s,
6m09s**; workflow elapsed time was **7m37s**.

The same successful test durations sum to 1,130.3 seconds. The five-file-shard
plan predicts **225.3, 226.3, 225.9, 226.2, 226.6 seconds** of test execution,
versus the original browser command's slowest **6.6 minutes** (including
startup). These are scheduling estimates, not measured new CI durations;
runner startup, route compilation and browser cache misses add overhead.
Six shards would leave the largest file at 220.1 seconds, saving only 6.5
seconds over five while starting another runner, so five is the initial choice.

The slowest files were `inbox-shell-states.spec.js` (220.1s) and
`docs-states.spec.js` (203.5s). Only four fixed sleeps exist (1,000ms, 250ms,
250ms, and an 80ms scroll settle used repeatedly). Their removal would have
little effect on the critical path and needs separate assertions for the
behavior they observe. No tests are removed or automatically retried.
The known mobile specs are only candidates for quarantine if they fail during
measurement; a passing measurement does not justify dropping them.
