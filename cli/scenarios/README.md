# OAR Agent Scenarios

Scenario-based UX testing for OAR agent callers. Each scenario runs real agents against a live core instance to discover ergonomic gaps, missing primitives, and onboarding friction.

## Structure

```
scenarios/
  harness/          In-repo harness runtime + pluggable agent driver interfaces
  profiles/         Agent archetype definitions + LLM prompt templates
  zesty-bots/       Scenario: Zesty Bots Lemonade Co.
```

## Agent Profiles

| Profile | Role | Primary surface |
|---|---|---|
| [coordinator](profiles/coordinator.md) | Works inbox, makes decisions, manages threads | inbox, threads, draft/commit |
| [worker](profiles/worker.md) | Executes work orders, submits receipts | threads context, artifacts, draft/commit |
| [orchestrator](profiles/orchestrator.md) | Watches event stream, dispatches workers, handles stalls | events stream, work orders |
| [reviewer](profiles/reviewer.md) | Reviews receipts, posts accept/revise/escalate | artifacts, draft/commit |

## Running a scenario

```bash
# Prerequisites: core running at http://127.0.0.1:8000
# From repo root: cd core && go run ./cmd/oar-core ...

cd cli
go build ./cmd/oar

cd scenarios/zesty-bots
bash coordinator.sh
bash worker.sh
bash orchestrator.sh
bash reviewer.sh
```

Each script self-registers a fresh agent, walks its profile's happy path against the seeded workspace, and prints annotated output. UX gaps surface as friction in the script — note them and file issues.

## Harness-based runs

`oar-scenario` is a thin in-repo harness that executes scenario manifests in two modes:

- `deterministic`: fixed command sequences per agent (CI-friendly baseline)
- `llm`: external driver decides actions turn-by-turn

Build binaries:

```bash
cd cli
go build -o oar ./cmd/oar
go build -o oar-scenario ./cmd/oar-scenario
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

Run with an external LLM driver:

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

Output shape:

```json
{ "action": "run", "name": "list active threads", "args": ["threads", "list", "--status", "active"] }
```

or

```json
{ "action": "stop", "reason": "goal reached" }
```

## Adding a scenario

1. Create `scenarios/<scenario-name>/README.md` describing the workspace state and seed instructions
2. Add a walkthrough script per relevant profile
3. Reference real artifact/thread IDs from the seeded workspace
