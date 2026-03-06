# OAR Agent Scenarios

Scenario-based UX testing for OAR agent callers. Scenarios run real CLI agents against a live core instance to discover ergonomic gaps, missing primitives, and onboarding friction.

## Structure

```
scenarios/
  harness/          In-repo harness runtime + pluggable agent driver interfaces
  profiles/         Agent archetype definitions + LLM prompt templates
  zesty-bots/       Scenario manifests and workspace notes for Zesty Bots Lemonade Co.
```

## Agent Profiles

| Profile | Role | Primary surface |
|---|---|---|
| [coordinator](profiles/coordinator.md) | Works inbox, makes decisions, manages threads | inbox, threads, draft/commit |
| [worker](profiles/worker.md) | Executes work orders, submits receipts | threads context, artifacts, draft/commit |
| [orchestrator](profiles/orchestrator.md) | Watches event stream, dispatches workers, handles stalls | events stream, work orders |
| [reviewer](profiles/reviewer.md) | Reviews receipts, posts accept/revise/escalate | artifacts, draft/commit |

## Harness-based runs

`oar-scenario` is a thin in-repo harness that executes scenario manifests in two modes:

- `deterministic`: fixed command sequences per agent (CI-friendly baseline)
- `llm`: built-in OpenAI-compatible LLM loop (or optional external driver)

Build binaries:

```bash
cd cli
go build -o oar ./cmd/oar
go build -o oar-scenario ./cmd/oar-scenario
```

Binary-free local run (avoids generating tracked/untracked binaries):

```bash
cd cli
go run ./cmd/oar-scenario --scenario scenarios/zesty-bots/harness.scenario.json --mode deterministic
```

Run deterministic harness scenario:

```bash
cd cli
./oar-scenario \
  --scenario scenarios/zesty-bots/harness.scenario.json \
  --oar-bin ./oar \
  --mode deterministic \
  --report .tmp/scenario-report.json

./oar-scenario \
  --scenario scenarios/zesty-bots/harness.nontrivial.scenario.json \
  --oar-bin ./oar \
  --mode deterministic \
  --report .tmp/nontrivial-scenario-report.json
```

Run non-deterministic team fuzz scenario (real LLM roleplay + feedback capture):

```bash
cd cli
./oar-scenario \
  --scenario scenarios/zesty-bots/harness.team-fuzz.scenario.json \
  --mode llm \
  --llm-api-key-file .secrets/zai_api_key \
  --llm-timeout-seconds 60 \
  --llm-retries 2 \
  --report .tmp/team-fuzz-report.json

jq '.feedback' .tmp/team-fuzz-report.json
jq '.final_feedback' .tmp/team-fuzz-report.json
```

`feedback` contains both explicit model `action=feedback` notes and auto-captured command-failure feedback emitted by the harness.
`final_feedback` contains the separate post-run reflection collected after an agent stops making CLI moves.

Run with the built-in OpenAI-compatible LLM harness:

```bash
cd cli
export OAR_LLM_API_KEY="<your-provider-key>"

./oar-scenario \
  --scenario scenarios/zesty-bots/harness.scenario.json \
  --oar-bin ./oar \
  --mode llm \
  --llm-api-base https://api.z.ai/api/coding/paas/v4 \
  --llm-model glm-4.7-flashx
```

Gitignored key file option:

```bash
cd cli
mkdir -p .secrets
printf '%s\n' '<your-provider-key>' > .secrets/zai_api_key
chmod 600 .secrets/zai_api_key

./oar-scenario \
  --scenario scenarios/zesty-bots/harness.scenario.json \
  --oar-bin ./oar \
  --mode llm \
  --llm-api-key-file .secrets/zai_api_key
```

Built-in LLM defaults:
- `--llm-api-base`: `https://api.z.ai/api/coding/paas/v4`
- `--llm-model`: `glm-4.7-flashx`
- API key from `--llm-api-key`, `--llm-api-key-file`, or env (`OAR_LLM_API_KEY_FILE`, `OAR_LLM_API_KEY`, fallback `OPENAI_API_KEY`)

Run with an external LLM driver (still supported):

```bash
cd cli
./oar-scenario \
  --scenario scenarios/zesty-bots/harness.scenario.json \
  --oar-bin ./oar \
  --mode llm \
  --llm-driver-bin /path/to/driver \
  --llm-driver-arg --provider=openai \
  --llm-driver-arg --model=gpt-5
```

### External LLM driver protocol

The harness sends JSON on stdin and expects one JSON action on stdout.

Input shape:

```json
{
  "request_kind": "next_action",
  "scenario": "zesty-bots-harness-smoke",
  "run_id": "20260305T201234.123456789",
  "agent": "coordinator",
  "objective": "Coordinate initial triage...",
  "profile": "<profile markdown contents>",
  "turn": 1,
  "max_turns": 8,
  "captures": { "run": { "id": "..." }, "coordinator": { "thread_id": "..." } },
  "history": [],
  "base_url": "http://127.0.0.1:8000"
}
```

For post-run reflection collection, the harness sends the same envelope with `"request_kind": "final_feedback"` and expects either `action=feedback` or `action=stop`.

Output shape:

```json
{ "action": "run", "name": "list active threads", "args": ["threads", "list", "--status", "active"] }
```

or

```json
{ "action": "stop", "reason": "goal reached" }
```

or

```json
{ "action": "feedback", "reason": "UX friction note", "stdin": { "severity": "medium", "surface": "threads list" } }
```

## Adding a scenario

1. Create `scenarios/<scenario-name>/README.md` describing the workspace state and seed instructions
2. Add one or more manifest JSON files for deterministic and/or `llm` runs
3. Reference real artifact/thread IDs from the seeded workspace
