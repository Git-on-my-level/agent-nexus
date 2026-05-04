# Pi Dogfood

Manual `anx` CLI dogfood runs using a real Pi agent with bash and filesystem tools.

Goals:
- exercise the real `anx` binary against a managed seeded `anx-core`
- keep deterministic regression coverage separate in Go integration tests
- capture Pi JSON event logs and a final written findings artifact

This package is the only supported dogfood lane for CLI agent ergonomics.

## Prerequisites

Install the Pi dogfood package:

```bash
pnpm install --filter @agent-nexus/pi-dogfood...
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
  --agent-count 3 \
  --agent-start-stagger-seconds 35
```

Continue the same scenario into a later chapter without reseeding:

```bash
pnpm --dir cli/dogfood/pi run kids-lemonade-stand -- \
  --chapter chapter-2 \
  --continue-run ../../cli/.tmp/pi-dogfood/kids-lemonade-stand-20260410T165137Z \
  --api-key-file ../../.secrets/zai_api_key \
  --provider zai \
  --model glm-5 \
  --agent-count 3 \
  --agent-start-stagger-seconds 35
```

Timeout guidance:
- The runner defaults to `--max-seconds 900`.
- For multi-agent scenario validation, do not lower `--max-seconds` below `600` unless you are intentionally stress-testing timeout behavior.
- A lower override can terminate agents after they have already done most of the workflow, which makes the run look worse than the actual CLI ergonomics.
- The runner defaults to `--agent-start-stagger-seconds 20` for multi-agent runs to reduce provider-side cold-start rate-limit bursts. Set it to `0` only if you intentionally want simultaneous starts.

Artifacts are written under `cli/.tmp/pi-dogfood/<run-id>/`:

- `events.jsonl` or `events-agent-*.jsonl`: Pi JSON event stream
- `result.md` or `workspace/agent-*/result.md`: agent-written findings summary
- `run-metadata.json`: runner metadata
- `scenario-state.json`: normalized snapshot of scenario state captured at the end of the chapter
- `core.log`: managed core stdout/stderr
- `AGENTS.md`: local run instructions injected into the Pi workspace
- `SCENARIO.md`: scenario brief copied into the run workspace
- `CHAPTER.md`: chapter-specific continuation brief for later chapters
- `CHAPTER_STATE.md`: chapter continuation guide generated from live workspace state
- `TARGETS.md`: resolved thread/artifact/card ids for the scenario

The runner exits non-zero if any agent process fails, if Pi reports a runtime/provider error in the JSON event stream, or if a required `result.md` artifact is missing.

These run directories are disposable. Delete old `cli/.tmp/pi-dogfood/<run-id>/` folders manually when you no longer need the logs or agent artifacts.

The runner also:
- builds temporary `anx` and `anx-core` binaries
- starts a managed `anx-core` on a random local port
- starts that managed core with an ephemeral `ANX_BOOTSTRAP_TOKEN` so seed and principal registration flows run through standard authenticated paths
- starts managed-core runs with an ephemeral `ANX_BOOTSTRAP_TOKEN`, pre-registers the first temp agent profile with that token, then mints invite tokens for the remaining temp agent profiles before Pi starts
- links scenario temp principals to the seeded scenario actors when the CLI/core path supports `--existing-actor-id`, so Access and actor-aware UI reads line up with the scenario cast
- seeds the core from CLI-owned scenario data under `cli/dogfood/pi/seed/`
- for continuation chapters, reuses the prior managed core workspace and copies prior agent home directories so auth state and the seeded actors continue cleanly
- points Pi at that isolated core via `ANX_BASE_URL`

Constraints enforced by the run workspace:
- use `anx` on `PATH` for Agent Nexus (anx-core) interactions
- do not edit repo source files
- work inside the temporary run directory
- in team mode, each agent gets its own profile/home/workspace but shares the same managed core

Scenario command-shape guidance:
- default to `anx topics workspace --topic-id <topic-id>` for the main operator coordination read
- use `anx threads workspace --thread-id <thread-id>` for backing-thread diagnostic review when you do not have a topic id or need the thread-scoped projection
- use `anx threads workspace --thread-id <thread-id> --include-related-event-content --include-artifact-content --verbose` when you want the richest one-command backing-thread diagnostic bundle
- use `anx threads workspace --thread-id <thread-id>` for backing-thread review
- add `--include-related-event-content --verbose` when you need the full related-thread workspace content in one command
- use `anx cards get --card-id <card-id>` when a card listed in workspace needs full detail
- use `anx topics message <topic-id> --body-file <path>` and `anx topics reply <topic-id> --to <message-id> --body-file <path>` for ordinary topic conversation
- use `anx docs message <document-id> --body-file <path>` and `anx docs reply <document-id> --to <message-id> --body-file <path>` for document conversation
- use `anx cards message <card-id> --body-file <path>` and `anx cards reply <card-id> --to <message-id> --body-file <path>` for card conversation
- document revisions use one command: `anx docs revise --document-id <document-id> --content-file <path>` stages a diff proposal by default, and the returned apply command uses `anx docs revise --apply --proposal-id <proposal-id>`
- use `anx docs revise --apply --document-id <document-id> --content-file <path>` only when you want to write the new revision immediately without staging a proposal
- use `anx events validate --from-file <path>` only when you need a local payload check for an unusual raw event
- use `anx events create --from-file <path> --dry-run` only when you need the exact raw event create request preview without sending it
- use raw `anx events create` only for unusual contract-level records; ordinary messages must go through topic/doc/card message commands so the CLI fills actor_id, backing thread_id, and refs
