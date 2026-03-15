# AGENTS

## Scope
Local guide for `cli/dogfood/pi/`.

Read [../../AGENTS.md](../../AGENTS.md) first for the CLI module purpose. This file only covers the Pi dogfood lane.

## Local Purpose
This package is the manual scenario-testing lane for CLI agent ergonomics.

Use it to exercise the real `oar` experience with a real agent runner against an isolated seeded `oar-core`, while keeping results comparable across runs.

## Local Invariants
- Keep scenario validation on the default lane: `--provider zai --model glm-5`, unless the user explicitly asks to test a different lane.
- Treat provider or model debugging as separate from scenario validation.
- Respect runner defaults from [README.md](/Users/dazheng/workspace/organization-autorunner/cli/dogfood/pi/README.md) and `run.mjs`.
- The runner default is `--max-seconds 900`; do not lower below `600` for multi-agent validation unless intentionally testing timeout behavior.

## Why This Matters
- Comparable runs need a stable provider/model lane.
- Lower timeouts can make scenario health look worse than actual CLI ergonomics.
- This lane is for validating end-to-end agent experience, not for mixing in unrelated provider experiments.
