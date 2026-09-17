# Independent qualification report — cycle 2 (2026-09-09)

# Qual acceptance — cycle 2, slice 3

- Lane: qualification (read-mostly). This lane never reports DONE.
- Branch: `feat/unified-work-pm-qual`
- Worktree: `/Volumes/scratch/worktrees/agent-nexus-lane-qual`
- Integration base / HEAD: `941f3ba5` (already matched `origin/feat/unified-work-pm`; UI slice 2 is in this SHA)
- UTC: 2026-09-09

David can stop after the table + defects. Commands and exit codes follow.

## Slice-2 contradictions vs this base

| Slice-2 finding | Now | Proof |
|---|---|---|
| PM in primary navigation | **fixed** | Live sidebar `aria-label="Primary"` = Inbox, Tasks, Docs. Ask PM is a header action. `tests/pm-qualification/.cache/primary-nav.local.json` |
| Inbox receipt badges do not fold | **fixed** | `inbox/+page.svelte` uses `ReceiptSignal` (quiet). Live Needs you: `foldedInList=0`, `hedgingInList=0` (all rows were primary `Needs you` / Blocked). Watching mailbox had no non-primary rows this run |
| PM evidence links `/work/...` | **fixed** | Live evidence hrefs are `/tasks/...` and `/inbox?...`. Zero `/work/` |
| `TopicBoardsPanel` `/boards/` leftovers | **fixed** | File gone. No `TopicBoardsPanel` / `/boards/` in `web-ui/src` |
| DecisionPanel hedging prose | **open** | `DecisionPanel.svelte` still renders `Outcome not independently verified` when an action exists and `independently_verified` is not true. Live awaiting_answer path did not display it (`panelHedging=0`) |
| Eleven machine receipt states, not fourteen | **open** | `core/internal/pm/types.go` + `contracts/anx-schema.yaml`: 3 decision + 8 action = **11** |

## Rulings (one row per bullet)

Verdicts: `verified live` / `verified synthetic` / `not implemented` / `contradicted`.

| Ruling | Verdict | Proof |
|---|---|---|
| Inbox is the only attention surface; root routes there | `verified live` | `shell.spec.js` "workspace root routes to Inbox" exit 0. Live `/inbox?mailbox=needs-you` after PM turn |
| Inbox mailboxes Needs you / Watching / Handled | `verified live` | Live Needs you had 4 `decision:` rows + blocked seeded task. `inboxMailbox.js` |
| Home unread feed and Decisions page removed | `verified synthetic` | `/decisions` 307. No Home feed |
| Tasks = former Work; table + board; `/work*` redirects | `verified live` | Live `/tasks?view=board` drag. `/work/+page.server.js` 307 |
| Nexus-owned drag mutates; source-owned drag proposes, never silent source mutation | `verified live` | `live_source_drag.spec.js` github card `phase=done` before and after; decision `pm_95febd46…` `awaiting_answer` |
| Docs as global KB; comments first-class; source pointer when aggregating | `verified live` (ingest) / `verified synthetic` (two-profile) | `anx docs ingest` on :8340; CLI two-profile test exit 0 |
| Topics, Boards, Events, Artifacts, Trash leave primary/secondary nav | `verified live` | Sidebar: Inbox, Tasks, Docs. Settings: Access, Secrets, Integrations, Audit |
| Nothing else in primary navigation | `verified live` | Ask PM is not a nav item. Bottom bar is labeled "Primary navigation" but hidden at 1280px and is chrome (Ask PM / Search / More), not a fourth primitive |
| Boards surface deleted | `verified synthetic` | No `routes/**/boards`. `TopicBoardsPanel` deleted |
| Events as audit log under settings | `verified live` (nav) | Settings "Audit" → `/events` |
| PM is not in-process; `anx pm serve` → agentctl → harness | `verified live` | `pm serve` claimed `pm_925b32500ba7a7206123f726966f6a31` then Maya CLI turn `pm_ff1a8ff2…`. omp `zai/glm-5.3` (`omp.2026-09-09.19349.log`, `30266.log`) |
| No wake-routing / online-handle prerequisite | `verified live` | `make serve` printed runner without `ANX_PM_BRIDGE_ENABLED` |
| Authorization per requesting principal; bearer required | `verified live` | Unauth `GET`/`POST /pm/conversations` → **403** `PM permission denied` |
| Dogfood omp/glm-5.3 first | `verified live` | omp logs above |
| Seatbelt deny-default, scratch write, no network, fail closed | `verified live` | Isolation + handwritten + generated GitHub canaries exit 0. `TestIsolationNeverFallsBackToHostExecution` PASS |
| Generated code never holds credentials; transform over trusted snapshot | `verified live` | `core/dev/github-transform.c` via `make serve` + `observation.serve.json`. Live card `card:card-anx-github-208-jit` `reader=generated-c-transform`, title="Ship first-class OpenClaw bridge integration", native=closed, freshness=fresh |
| Linux bubblewrap | `verified synthetic` / **no Linux host** | `make -C core check` prints GOOS=linux arm64+amd64 build. No bwrap run |
| Telegram and Discord to test-readiness with fakes | `verified synthetic` | `tests/channels` + `TestChannelE2E*` exit 0. Live bots **not implemented** |
| E2E on web + CLI until David supplies bots | `verified live` | Playwright `/pm` + `anx --agent maya pm ask --wait` |
| Record keeps many machine states; UI shows four; rest behind disclosure | **partial** | Four primary badges exist (`receiptSignal`). Record has **11** states, not 14. Hedging sentence still in DecisionPanel (not shown on awaiting_answer) |
| Order 1: web+CLI e2e omp answer naming a seeded task | `verified live` | Web 3.1m + CLI 222s. Named `card:prepare-vertical-slice-capture-candidate-build` |
| Order 2: JIT macOS canary + negatives | `verified live` | Handwritten rev `7ee6386c…`; generated C rev `5aa741aa…`; serve dogfood rev `1754e047…` |
| Order 3: Inbox/Tasks/Docs; legacy out of nav | `verified live` | Remaining product nits: hedging sentence, 11≠14 |
| Telegram/Discord test-readiness | `verified synthetic`; live bots `not implemented` | Needs David credentials |
| Independent qualification | **in progress** (this lane) | |

## Defects (severity)

1. **Open — fourteen receipt states vs eleven in the record.** Schema `decision_status` (3) + `action_status` (8) = 11. UI `RECEIPT_FOLDED` also lists `pending` / `queued` / `applied` that core does not define. Either the ruling number is stale, or states are missing.
2. **Open — hedging sentence in DecisionPanel.** `"Outcome not independently verified"` plus folded label `"Source reported; not independently verified"`. Live PM path (awaiting_answer) does not show it.
3. **Low — `anx docs ingest --json` first pass failed 33 files, then 1**, with the JSON error envelope omitting per-file rows. Under concurrent `make -C core check` on this host. Subsequent text runs: leftover 1 create, then **489 unchanged / 0 created / 0 updated / 0 failed**. Second consecutive successful run is 0 changes as required. Not a source mutation (tree was read-only).
4. **Low — live `make serve` still advertises unauthenticated writes.** PM layer still 403s without a principal.
5. **Flake — first `live_source_drag` `page.goto` `ERR_ABORTED`** (decision already created). Retry with `waitUntil: "domcontentloaded"` passed in 2.3s. First `live_web_pm` failed because the spec still asserted a `PM` nav link after UI slice 2; harness updated.
6. **Nuance — dogfood JIT stage JSON still carries `last_error.rate_limited` 403 from an earlier GitHub hit.** Live refresh of the JIT card this slice succeeded (`freshness.status=fresh`, `generated-c-transform`). Builtin github-main later logged `observation refresh: rate_limited`.

No silent source mutation. No unsandboxed generated-code fallback. No successful `/pm` call without a principal.

## What still needs David

1. **Dedicated Telegram + Discord bot credentials** — transports are test-ready on fakes; live bots are not implemented.
2. **A Linux host** — bubblewrap is compile-checked (`GOOS=linux` arm64+amd64) but has not been executed.
3. **A ruling on "fourteen" vs the 11 states in schema/core** — do not invent three more states in qualification.
4. Optional: delete or replace the DecisionPanel hedging sentence (UI lane).

## Gates

| Check | Exit | Notes |
|---|---|---|
| `make -C core check` | **0** | 100s. Isolation flake from slice 1 not reproduced. GOOS=linux observation build printed |
| `make cli-check` | **0** | 53s |
| `make contract-check-committed` | **0** | 43s |
| `make cli-build` | **0** on retry | First attempt raced contract gen (`go.mod` missing mid-regen), exit 2 |
| web-ui lint + vitest | **0** | 860 tests / 134 files |
| PM Playwright (synthetic) | **0** | 8 passed, 27.8s. First attempt used wrong cwd (exit 127); rerun clean, no 390px flake |
| shell spec `CORE=8320 PORT=8330 BASE=8331` + `env -u GOROOT` | **0** | 4 passed, 15.2s |
| `qa:diff` `QA_VISUAL_CORE_PORT=8310` + `PLAYWRIGHT_BROWSERS_PATH=$HOME/Library/Caches/ms-playwright` | **0** | 28/28 |
| `tests/channels` `go test ./...` | **0** | 0.422s |
| `go test ./internal/pm -run TestChannelE2E` | **0** | Telegram + Discord bound turn, dedup, approve/stale, 429/409 |
| JIT canaries `ANX_OBSERVATION_GITHUB_CANARY=1` | **0** | handwritten + generated C + negatives + never-fallback |
| Docs two-profile | **0** on retry | First attempt raced contract gen. Retry 3.25s PASS. Synthetic two CLI profiles |
| live Playwright (`live_source_drag` + `live_web_pm`) | **0** | 3.2m after harness fix. Drag first-run flake then pass |
| `anx --agent maya pm ask --wait` | **0** | 222s. `pm_497fdcde…` / `pm_ff1a8ff2…` status **delivered**. Named the seeded blocked card + GitHub drag decision |
| `anx docs ingest` omi-kb on :8340 | **0** on the 0-change rerun | See defects. 489 markdown files. Source prefix `https://git-01.tail76ea03.ts.net/hermes/omi-knowledge/blob/main`. Tree not written |

Playwright on this host must use `PLAYWRIGHT_BROWSERS_PATH=$HOME/Library/Caches/ms-playwright`. `GOCACHE=/Volumes/scratch/tmp/qual-go-cache` and `env -u GOROOT`.

## Live stack (this slice)

```
ANX_DEV_BLOB_BACKEND=filesystem CORE_PORT=8300 WEB_UI_PORT=8301 ANX_CORE_WAIT_TIMEOUT_MS=120000 \
  env -u GOROOT GOCACHE=/Volumes/scratch/tmp/qual-go-cache make serve
# dogfood JIT revision 1754e0479e0289d9b751a0ac7110c356b65fa2dcbbaac568d03f491fa1c035cd already active
# reader=generated-c-transform on card:card-anx-github-208-jit (live GET)
# core 127.0.0.1:8300, Vite 8301, seed 156 events

HOME=.tmp/anx-dev-profile-homes/pm ./cli/anx --agent pm pm serve \
  --work-dir .tmp/pm-runner \
  --runner 'omp -p --mode json --model zai/glm-5.3 --auto-approve'
```

Maya via `POST /auth/dev/session`. Reply named `card:prepare-vertical-slice-capture-candidate-build`. Inbox Needs you: 4 decision rows.

Spare ingest core: `PORT=8340 WORKSPACE_ROOT=/Volumes/scratch/tmp/qual-docs-ingest-ws make -C core serve` with `ANX_BOOTSTRAP_TOKEN=qual-docs-ingest-bs`.

## Stubbed / not verified

- Fresh `TestLiveGenerateHarnessGitHub` (agentctl/omp generate a new transform this slice). Lifecycle of the checked-in C artifact was.
- Linux bubblewrap execution.
- Live Telegram/Discord bots.
- Watching-mailbox fold of a non-primary receipt (no such live rows this run).
- `qualify.py --suite all`.

## Next concrete step

Re-run against the next integration merge. Treat hedging prose and 11≠14 as the remaining product contradictions. Do not implement them in this lane unless they stay open after a UI/core change that claims they are gone.
