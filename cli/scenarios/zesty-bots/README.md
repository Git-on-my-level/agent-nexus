# Scenario: Zesty Bots Lemonade Co.

A fictional automated lemonade stand run by bots. The workspace is pre-seeded with a realistic operational state spanning incidents, initiatives, and in-progress work orders.

## Workspace State

Seeded data lives in `core/.oar-workspace/`. Run core against this workspace to get the scenario live.

### Active Threads

| ID | Title | Priority | Type |
|---|---|---|---|
| `a582c6a3-7b67-40cd-8521-d1500082f8b3` | Emergency: Lemon Supply Disruption | P0 | incident |
| `a3746992-5f23-4315-8fde-2075009da066` | Summer Flavor Expansion: Lavender & Mango Chili | P1 | process |
| `4d22b650-d34e-41cb-bf12-9a5b0f79122c` | Q2 Initiative: Open Stand #2 at Riverside Park | P2 | initiative |
| `89d40582-7f6c-40da-acd0-6fd5317da767` | Daily Ops — Stand #1 (Corner of Maple & 5th) | P2 | process |
| `d6753093-d763-4164-aa72-a331cc61c1d6` | Pricing overcharge incident (Till-E POS) | — | incident |

### Key Artifacts

| ID | Kind | Description |
|---|---|---|
| `artifact-wo-lavender-sourcing` | work_order | Source food-grade lavender syrup supplier |
| `artifact-receipt-lavender-sourcing` | receipt | BotBotanicals selected, 2L order placed |
| `artifact-review-lavender-sourcing` | review | Accepted — margin target preserved |
| `artifact-wo-pricing-fix` | work_order | Fix Till-E POS stale cache overcharge |
| `artifact-summer-menu-draft` | doc | Lavender Lemonade + Mango Chili recipes |
| `artifact-supplier-sla` | doc | CitrusBot Farm SLA terms |
| `artifact-pricing-evidence` | evidence | POS transaction log showing overcharge |

### Inbox State (on fresh workspace)

| Category | Thread | Item |
|---|---|---|
| `decision_needed` | pricing overcharge | Approve customer refunds |
| `exception` | lemon shortage | Lemon inventory below safety threshold |
| `commitment_risk` | lemon shortage | SLA breach report due 2026-03-10 |

## Prerequisites

```bash
# Terminal 1: start core against the seeded workspace
cd core
go run ./cmd/oar-core \
  --host 127.0.0.1 --port 8000 \
  --schema-path ../contracts/oar-schema.yaml \
  --workspace-root .oar-workspace

# Terminal 2: run scenarios from the CLI module
cd cli
```

## Harness Manifest

This scenario also has a harness manifest for multi-agent integration runs:

- `harness.scenario.json`
- `harness.nontrivial.scenario.json` (docs lifecycle + optimistic concurrency conflict)
- `harness.team-fuzz.scenario.json` (non-deterministic multi-role LLM roleplay + feedback capture)

From `cli/`:

```bash
go build -o oar ./cmd/oar
go build -o oar-scenario ./cmd/oar-scenario

# Or run without creating binaries:
go run ./cmd/oar-scenario --scenario scenarios/zesty-bots/harness.scenario.json --mode deterministic

./oar-scenario \
  --scenario scenarios/zesty-bots/harness.scenario.json \
  --oar-bin ./oar \
  --mode deterministic

./oar-scenario \
  --scenario scenarios/zesty-bots/harness.nontrivial.scenario.json \
  --oar-bin ./oar \
  --mode deterministic

./oar-scenario \
  --scenario scenarios/zesty-bots/harness.team-fuzz.scenario.json \
  --mode llm \
  --llm-api-key-file .secrets/zai_api_key \
  --llm-timeout-seconds 60 \
  --llm-retries 2 \
  --report .tmp/team-fuzz-report.json
jq '.feedback' .tmp/team-fuzz-report.json
jq '.final_feedback' .tmp/team-fuzz-report.json

export OAR_LLM_API_KEY="<your-provider-key>"
./oar-scenario \
  --scenario scenarios/zesty-bots/harness.scenario.json \
  --oar-bin ./oar \
  --mode llm \
  --llm-api-base https://api.z.ai/api/coding/paas/v4 \
  --llm-model glm-4.7-flashx

mkdir -p .secrets
printf '%s\n' '<your-provider-key>' > .secrets/zai_api_key
chmod 600 .secrets/zai_api_key
./oar-scenario \
  --scenario scenarios/zesty-bots/harness.scenario.json \
  --oar-bin ./oar \
  --mode llm \
  --llm-api-key-file .secrets/zai_api_key
```

`feedback` captures explicit `action=feedback` notes and automatic command-failure capture.
`final_feedback` captures one end-of-run reflection per participating agent when `collect_final_feedback` is enabled.

## Resetting Workspace State

The seeded workspace state is read-only artifacts and snapshots. Events written by harness runs accumulate but do not break the seeded threads. To fully reset, stop core and restore `core/.oar-workspace/state.sqlite` from git.
