# Pi Dogfood

Manual OAR CLI dogfood runs using a real Pi agent with bash and filesystem tools.

Goals:
- exercise the real `oar` binary against a managed seeded `oar-core`
- keep deterministic regression coverage separate in Go integration tests
- capture Pi JSON event logs and a final written findings artifact

This package is the only supported dogfood lane for CLI agent ergonomics.

## Prerequisites

Install the Pi dogfood package:

```bash
pnpm install --filter @organization-autorunner/pi-dogfood...
```

## Run

From the repo root:

```bash
pnpm --dir cli/dogfood/pi run pilot-rescue -- \
  --api-key-file ../../.secrets/zai_api_key \
  --provider zai \
  --model glm-5
```

Concurrent team run:

```bash
pnpm --dir cli/dogfood/pi run pilot-rescue -- \
  --api-key-file ../../.secrets/zai_api_key \
  --provider zai \
  --model glm-5 \
  --agent-count 4
```

Alternate playful three-agent run:

```bash
pnpm --dir cli/dogfood/pi run kids-lemonade-stand -- \
  --api-key-file ../../.secrets/zai_api_key \
  --provider zai \
  --model glm-5 \
  --agent-count 3
```

Timeout guidance:
- The runner defaults to `--max-seconds 900`.
- For multi-agent scenario validation, do not lower `--max-seconds` below `600` unless you are intentionally stress-testing timeout behavior.
- A lower override can terminate agents after they have already done most of the workflow, which makes the run look worse than the actual CLI ergonomics.

Artifacts are written under `cli/.tmp/pi-dogfood/<run-id>/`:

- `events.jsonl` or `events-agent-*.jsonl`: Pi JSON event stream
- `result.md` or `workspace/agent-*/result.md`: agent-written findings summary
- `run-metadata.json`: runner metadata
- `core.log`: managed core stdout/stderr
- `AGENTS.md`: local run instructions injected into the Pi workspace
- `SCENARIO.md`: scenario brief copied into the run workspace
- `TARGETS.md`: resolved thread/artifact/card ids for the scenario

The runner exits non-zero if any agent process fails, if Pi reports a runtime/provider error in the JSON event stream, or if a required `result.md` artifact is missing.

These run directories are disposable. Delete old `cli/.tmp/pi-dogfood/<run-id>/` folders manually when you no longer need the logs or agent artifacts.

The runner also:
- builds temporary `oar` and `oar-core` binaries
- starts a managed `oar-core` on a random local port
- starts that managed core with `OAR_ENABLE_DEV_ACTOR_MODE=1` and `OAR_ALLOW_UNAUTHENTICATED_WRITES=1` so the seed phase can bootstrap actors, reads, and threads before agents authenticate
- seeds the core from CLI-owned scenario data under `cli/dogfood/pi/seed/`
- points Pi at that isolated core via `OAR_BASE_URL`

Constraints enforced by the run workspace:
- use `oar` on `PATH` for OAR interactions
- do not edit repo source files
- work inside the temporary run directory
- in team mode, each agent gets its own profile/home/workspace but shares the same managed core

Scenario command-shape guidance:
- default to `oar topics workspace --topic-id <topic-id>` for the main operator coordination read
- use `oar threads workspace --thread-id <thread-id>` for backing-thread diagnostic review when you do not have a topic id or need the thread-scoped projection
- use `oar threads workspace --thread-id <thread-id> --include-related-event-content --include-artifact-content --verbose` when you want the richest one-command backing-thread diagnostic bundle
- use `oar threads recommendations --thread-id <thread-id>` for recommendation/decision review
- add `--include-related-event-content --verbose` when you need full related-thread recommendation content in one command
- use `oar cards get --card-id <card-id>` when a card listed in workspace needs full detail
- document proposals are a two-step flow: `oar docs propose-update ...` then `oar docs apply --proposal-id <proposal-id>`
- use `oar docs update ...` only when you want to write the new revision immediately without staging a proposal
- use `oar events validate --from-file <path>` when you want a local payload check before `oar events create`
- use `oar events create --from-file <path> --dry-run` when you want the exact create request preview without sending it
