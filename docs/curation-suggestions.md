# Quiet curation suggestions

Curation is agent work. Formula candidates are a bounded fallback; ANX core runs
no models. Human judgment is not needed to group ask delivery. Suggestions never
create a human ask, inbox item, Decide entry, or notification.

## The former interruption path

`core/internal/curation/candidates.go` generates withdraw-as-moot, duplicate-card
merge and related-ask batch candidates. The two writers were:

- `core/internal/server/curation_handlers.go`: `RunCurationFormulaLoop`, started
  by `core/cmd/anx-core/main.go`, runs hourly as the system principal. The manual
  `POST /curation/run` uses the same pass.
- Agents call `POST /curation/proposals` (`anx curation propose`, exposed to
  callers as `anx_curation_propose`). `mcp/catalog/catalog.go` derives that tool
  name from command ID `curation.propose`; `mcp/executor/executor.go` executes
  its registered method/path, so it is the same POST handler. The concrete
  agent caller in this repo is
  `cli/internal/app/pm_curate.go`: `anx pm curate` reads candidates, invokes an
  explicitly configured local PM harness, and posts its selected candidates.
  `--formula` calls the deterministic run endpoint without a model. The OSS
  repo does not define a schedule for this local command or the hosted tool
  adapter; this investigation did not read hosted schedules or production data.

Both called `fileCurationProposal`, which built a `human_attention_requested`
event with `kind: ask`, `response_proposals` and `payload.curation`.
`primitives/curation_store.go:FileCurationProposal` sent it through
`AppendTaskAttentionEvent`, creating ordinary task-addressed human attention.
The normal `human_attention*` resolution/inbox machinery then treated it as an
ask. `primitives/brief.go` exposed it as a `pm_proposal` decision, alongside the
underlying asks. Answering in `server/inbox_ask_handlers.go` called
`applyCurationAnswer`: the human's first option performed the action. That was
the source of “Batch 2 related asks?” / Batch / Keep separate prompts. A hosted
notifier forwarding new asks therefore forwarded these as well; Telegram
transport is outside this OSS repo and was not inspected here.

## Agent workflow and passive human access

`POST /curation/run` and the hourly fallback now record `curation_suggested`
events, atomically with their dedupe keys. `POST /curation/proposals` defaults to
this same quiet filing. Generic ask creation rejects `payload.curation`, so an
older caller cannot restore the human-prompt path.

`GET /brief` includes `curation_suggestions` for agents. `anx orient` renders
the suggestion plus runnable `apply_argv` and `dismiss_argv`. These use
`anx api createCurationProposal --json-body ...`, supplying the action, targets,
agent rationale and `resolution: apply|dismiss`. Only authenticated agents may
resolve suggestions. The action, `curation_receipt` and terminal dedupe state
commit together; a failed receipt rolls the action back and retries reuse the
same key. Successful receipts record who acted, why, targets, result and undo.
Dismissal also records a receipt. The local `pm curate` caller applies its
chosen cleanups through this protocol; formula passes only suggest.

- Merge uses `FoldWork`, preserving the normal transferred refs, plan context
  and reachability of original asks. Undo is `anx work unfold`.
- Batch records delivery grouping without asking a human. Existing grouped-ask
  answering remains available, and humans or agents may undo grouping with
  `anx curation ungroup`. Notification batching remains a notifier concern.
- Withdraw uses the normal atomic ask resolution claim with a moot reason.
  Access requests cannot be withdrawn by this path.

Humans read quietly through `GET /curation/candidates`: `suggestions` contains
open suggestions, separate from formula `candidates`. This is the minimum API
for a passive Suggestions affordance on Watch or the board. The companion UI
lane can render this list and group related asks in the inbox view. This lane
does not change Watch, collection or attention placement code. Human briefs
contain an empty suggestion section; Decide and inbox never receive suggestions.

## Upgrade and compatibility

Migration 119 withdraws every unresolved ask carrying object-valued
`payload.curation`, including orphaned filings missing their dedupe row. Each
gets a canonical `human_attention_withdrawn` event with an explicit reason and
an atomic resolution claim. Existing resolution triggers cancel pending push
notifications. Ordinary asks, access-request asks and already answered records
remain intact. Existing curation dedupe rows are dismissed to prevent re-filing
those legacy prompts. No historical event payload is edited. Installation runs
the migration; this lane did not install it or change production.

The existing proposal endpoint remains compatible for callers that only file
suggestions, but its returned `event_id` now names a quiet event. Historical
human answers remain readable; replaying one no longer performs curation.
No HTTP route is added or removed. Changed route behavior: `GET /brief`,
`GET /curation/candidates`, `POST /curation/proposals`, `POST /curation/run`,
`POST /curation/ungroup`, and ask creation through `POST /events` and
`POST /asks/batch` (curation metadata is rejected). Human answer routes no
longer apply or replay a curation action.

## Evidence and request cost

`TestCurationNeverFilesHumanAttention` failed on the original path with **2 new
human attention requests** for one duplicate/batch pass, and passes with **0**.
`TestCurationUpgradeWithdrawsLegacyAsks` failed without migration 119 (no
withdrawal resolution) and passes with it, including retained reasons, pending
push cancellation, ordinary/answered preservation and idempotence. Integration
tests cover quiet agent brief/API discovery, no inbox/delivery rows, human
resolution denial, generic-ask rejection, apply/dismiss receipts, replay,
privacy, fold, grouping and atomic rollback after an injected receipt failure.

The new read admits at most 51 ledger rows through
`idx_curation_proposals_open_page(status,created_at,key)`, then hydrates at most
50 event/thread pairs through scoped primary-key lookups. There is no refill
after visibility filtering, per-row query, or workspace-sized serving scan.
Scoped truncation is always marked potentially partial so hidden ledger
occupancy is not disclosed. Agent briefs and the candidates API each add these
two queries. Existing candidate caps remain 200 asks/cards/refs. The one-time
upgrade scans historical ask/withdrawal type ranges; it adds no recurring scan.

The native SQLite scale test measured **2 queries / 101 rows / 2,785 VM steps /
0 full-scan steps**, both with zero and 4,096 unrelated dismissed suggestions.
A new checked-in budget enforces 5,000 VM steps and zero scans. Existing route
budgets and plan exceptions are unchanged.

The expanded fixture includes 4,096 real old suggestion events as well as their
ledger rows. It caught an event-type-index walk at 47,853 VM steps; driving the
hydration query from the admitted ID set fixes that growth (2,785 VM steps).
The broader `TestPerformanceStartupAndMigrations` gate was also run. Its existing
90-second upgrade budget expired in unchanged migration 116, before migration
119 ran (`admission spellings: context deadline exceeded`). That failure is
reported separately; no timeout or budget was changed.
