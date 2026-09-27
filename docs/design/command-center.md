# Command center: host identity, runs, and operator ergonomics

Status: approved for implementation (2026-09-27). Owner: operator (David). This is the
shared brief for every workstream on the `command-center` integration branch. When a
workstream makes a decision this document does not cover, record it in the "Workstream
decisions" section at the bottom in the same PR.

**No backwards compatibility.** ANX has no external users. Prefer the clean design over
compatibility shims, deprecation paths, dual code paths, or migrations that preserve old
behavior. Delete superseded code, docs, tests, and fixtures instead of keeping them behind
flags. The only data that must survive is the operator's own workspace content (topics,
cards, docs, events, actors); auth rows may be migrated destructively as long as existing
agents are adopted as described below.

## Goals

1. Operators see what every agent is doing right now (who, where, on what, for how long,
   waiting on whom) without agents having to remember to report it.
2. A machine is enrolled into a workspace **once**. Every agent on that machine then works
   with `anx` with zero per-agent setup.
3. The agent CLI is cheap to use correctly: one orientation call, short verbs for the daily
   loop, a way to wait for a human answer, and machine-readable next steps on every response.
4. The web UI is higher signal and lower noise for humans supervising many agents.

## Non-goals

- Launching arbitrary agent runs from the web UI. (Wakes via `@handle` remain the only
  human-initiated way to start agent work.)
- Reimplementing agentctl routing, scheduling, or supervision inside ANX.
- Cross-workspace agent identity. Hosts and agents are workspace-local (repo rule).
- Control-plane coupling in `anx-core` (repo rule). agentctl is not the control plane.

## Identity model

### Hosts

- New principal kind `host`, workspace-local. A host represents one machine (or one CI /
  cloud environment) enrolled into one workspace. A machine that works in several
  workspaces enrolls into each separately.
- A host holds one active Ed25519 keypair. The private key lives only on the machine
  (`~/.config/anx/hosts/<workspace-key>/`), owner-only permissions.
- Host slug: lowercase `[a-z0-9-]`, unique per workspace, defaulting from the machine name
  the operator confirms at enrollment (e.g. `m5-mbp`).
- A host is **not** auth-admin. Its only capability beyond reading its own record is to
  obtain tokens for agent principals derived under it. It cannot create invites, see auth
  inventory, touch humans, or act for other hosts' agents.
- Revoking a host revokes every agent principal under it, all their keys, sessions and
  tokens, in one transaction, and records one audit event.

### Enrollment (replaces per-agent registration)

- **Interactive (default):** `anx host enroll` generates the host key, calls the core
  enrollment start endpoint, prints a short user code and a verification URL, and polls.
  A human auth-admin approves (or denies) in the web UI Access page. On approval the CLI
  receives the host principal and stores it. Codes are single-use, expire (10 min), and
  approval shows the host name, OS user, adapters discovered, and the requesting IP.
- **Headless:** a human creates a one-time, expiring host enrollment token in the web UI
  (for CI/cloud); `anx host enroll --token <t>` completes without polling.
- **Bootstrap:** the first principal of a new self-host workspace is a human (passkey via
  the existing bootstrap path). Hosts never bootstrap a workspace.
- Per-agent invite registration (`anx auth register` for agents, agent invites, the
  Access page "Create invite → Agent" form, pubkey paste) is **removed**. Human invites
  remain as they are today.

### Derived agents

- An agent principal is derived from `(host, name)`, where `name` is an adapter
  (`claude`, `codex`, `cursor`, `omp`, `generic`) or an operator-chosen persona
  (`reviewer`, `release-bot`). Personas are optional and explicit.
- Username / `@handle`: `<name>.<host>` (e.g. `codex.m5-mbp`, `reviewer.m5-mbp`). Display
  name: `codex on m5-mbp`. The actor id is stable for the life of the `(host, name)` pair.
- Derived agents are created lazily on first token request. Excluded names are refused.
- Token path: the `anx` CLI signs a host assertion (`host_id`, `key_id`, `agent_name`,
  `signed_at`, replay-protected like the existing agent assertion grant) and exchanges it
  at `POST /auth/token` for a short-lived access token for the derived agent. No refresh
  tokens are shared between processes; each process re-derives from the host key.
- **Trust boundary (documented, accepted):** any process running as the OS user on an
  enrolled host can act as that host's agents. Mitigations: non-admin derived agents,
  short-lived tokens, one-switch host revoke, per-write run attribution in the audit trail.

### Resolving "which agent am I" in the CLI

Resolution order, first match wins:

1. `--as <name>` flag or `ANX_AS` env (persona or adapter name).
2. agentctl run context: `AGENTCTL_ADAPTER` (and `AGENTCTL_EXECUTION_ID`, labels, host id)
   exported by `agentctl run` to the child process.
3. Known harness detection from the environment (e.g. `CLAUDECODE=1` → `claude`; codex,
   cursor-agent, omp: detect via their documented env markers; verify each, do not guess).
4. Otherwise: fail with a clear error and `next_actions` suggesting `--as <name>`.

Explicit profile files per agent are no longer the identity mechanism. `--agent` /
`ANX_AGENT` / `anx config use` are removed or repurposed to select the *workspace/host*
context, not an agent.

### Adoption and exclusion

- On `anx host enroll`, existing agent principals on this machine for the same workspace
  (local `~/.config/anx/profiles/*.json` with keys) are **adopted by default**: the server
  re-parents each principal under the host as a persona with the same actor id and
  history, and the CLI deletes the old local profile. `--exclude <profile>` (repeatable)
  skips adoption; excluded profiles keep working as legacy standalone principals until
  revoked, and no new standalone principals can be created.
- Hosts also carry an exclusion list of adapter/persona names that may not be derived on
  that host (`anx host exclude cursor`, editable in the Access page).

## Runs

- New core resource `run`: a generic, launcher-neutral execution report. Shape follows
  agentctl's execution envelope (`schemas/execution.schema.json`, `callback-envelope`)
  closely enough that agentctl events map 1:1, but core does not import agentctl.
- Fields: `launcher` (`agentctl`), `external_id` (agentctl `exec-*` id, unique per
  launcher+host), `host`, `agent` (derived principal), `adapter`, `model` (optional),
  `state` (`starting|running|completed|failed|cancelled|unknown`), `liveness`,
  `result_collected` (bool), `labels`, `card_ref` (from label `anx:card:<ref>` or explicit),
  `repository`/`branch`, `started_at`, `ended_at`, `last_observed_at`.
- Writes are idempotent upserts keyed by `(launcher, host, external_id)` with
  monotonic-state guarding (a late `running` never overwrites `completed`).
- `anx runs ingest` reads one agentctl callback envelope (or execution envelope) on stdin
  and upserts it. It is designed to be an agentctl `command` subscription destination, so
  agentctl never handles ANX credentials.
- A run's terminal state is not task completion. A completed run never moves a card.
- Every authenticated write made inside an agentctl run carries run metadata
  (`X-ANX-Run-Id` or equivalent) so events and messages attribute to host, agent, adapter
  and run.

## Presence and agent state

Core derives, per agent, on read:

- `waiting_on_human`: the agent has an open ask/review/escalation it requested.
- `working`: a nonterminal, alive run, or presence set via `work start|note` within the
  freshness window (default 30 min).
- `idle`: none of the above, signal within the stale threshold.
- `stale`: no signal (run, presence, write, bridge check-in) within 24h.
- Bridge online/offline stays a separate fact.

Each agent summary includes: host, name, display name, derived state, current card
(title + ref), last progress note and time, active run (adapter, model, duration), open
asks count, last signal time. `PATCH /agents/me/presence` sets `current_card_ref` and an
optional short note.

## CLI contract (agentctl-style)

- JSON envelope v2 on every command: `{ok, schema_version, command, result, warnings[],
  next_actions[{label, argv[], mutates, side_effect_class}]}`; errors:
  `{ok:false, schema_version, error{code, message, retryable, exit_code, details,
  next_actions}, warnings}`. `warnings` and `next_actions` are always present.
- `side_effect_class`: `read_only`, `local_operational_write`,
  `remote_coordination_write`, `external_side_effect`. Shown in help and next actions.
- Text mode: one fact per line, lead token then `key=value` pairs, `next <argv>` lines,
  `warning code=…` lines. No `::` rows, no raw JSON fallback, no tables or color.
- Distinct exit codes: 0 ok, 2 usage, 3 not_found, 4 conflict, 5 auth, 6 network/unavailable,
  7 outdated, 8 timeout (await), 9 declined (await), 1 other.
- Errors carry runnable repairs (`cli_outdated` → `anx update`; unknown command →
  did-you-mean; empty filter on an unresolved actor → corrected argv).
- `me` accepted anywhere an actor is expected; bare actor ids normalized to `actor:<id>`;
  a filter that resolves to nothing known returns a warning, never a silent empty list.

## CLI daily loop

- `anx orient`: read-only, bounded, personal-first snapshot (me, my work by phase, my asks
  and answers, mentions, stale items) plus `next` lines.
- `anx work start|note|block|done`: operate on the current task when no card is given;
  `start` adds (never replaces) the agent as assignee, moves to in progress, sets presence.
- `anx ask|review|escalate`: top-level; subject defaults to the current task; returns an
  ask id and `next anx await <id>`. Response proposals remain required per DECISIONS
  2026-04-28 (`--recommend` first, `--alt` repeatable).
- `anx await <ask|card> [--until …] [--timeout]`: blocks on the event stream, prints one
  terminal document; exit codes distinguish answered / declined / timeout.
- Help shows the daily verbs and setup commands; diagnostic groups move under
  `anx debug …`. Commands advertised in `meta commands` must all dispatch.

## Web UI

- **Agents** primary nav item: roster grouped by derived state; agent detail page (current
  and recent tasks, runs, asks, notes). This is a presence surface, not an attention
  queue; "waiting on you" rows link into Inbox. Inbox remains the only attention surface.
- **Access → Hosts**: pending enrollments with approve/deny, host list with agents,
  exclusions, revoke; headless enrollment token creation. Agent invite UI removed.
- **Inbox triage**: auto-select first item, sort Needs you by blocked time then severity,
  keys 1–5 / J / K / E / R / O, undo toast instead of immediate send, recommended marker in
  the pane, context strip (blocked task + agent's last note), sidebar count badge, pane and
  standalone page behave the same, `/inbox?status=` link bug fixed, operator vocabulary
  instead of core nouns in update rows, Watching rows show digests not bare counts.
- **Noise pass**: dedupe task evidence by source, attention-ordered task table with Done
  collapsed and source-key dedupe, hide "Last checked" for native tasks, names instead of
  raw ids, live updates via the existing event stream instead of Reload buttons, docs list
  redundancy, Settings vs Diagnostics grouping, ⌘K palette with actions.

## Bridge (adapters/agent-bridge)

- One bridge per enrolled host, authenticating with the host identity (no per-agent agent
  homes, no `import-auth`, no copied refresh tokens). It checks in presence for the host
  and serves wakes for every derived agent handle on that host that the operator enabled
  for wake routing.
- When `agentctl` is installed, the bridge launches woken runtimes through `agentctl run`
  with the label `anx:card:<ref>` (when the wake has a subject) and a subscription that
  runs `anx runs ingest`, so wake-launched work shows up as runs automatically. The
  launched process resolves its ANX identity from the agentctl run context.
- Delete the per-agent agent-home bridge model and its docs rather than keeping both.

## agentctl changes

- `agentctl run` exports non-secret run context to the child: `AGENTCTL_EXECUTION_ID`,
  `AGENTCTL_ADAPTER`, `AGENTCTL_HOST_ID`, `AGENTCTL_LABELS`, `AGENTCTL_AUTHORITY`.
- Docs: the ANX integration recipe (label `anx:card:<ref>`, `command` destination running
  `anx runs ingest`). No credentials in agentctl, per its rules.

## Hosted (controlplane, private repo)

- Proxy policy additions: enrollment start/poll reachable without a control-plane session
  (and able to reach a running runtime); approval/deny, host revoke, and enrollment-token
  creation are human/org-gated; `/runs`, `/agents`, `/hosts` read families pass for core
  bearers; `POST /auth/token` host assertion grant keeps the existing wake behavior.

## Contract summary

Canonical shapes and access rules are in `contracts/anx-openapi.yaml`; durable invariants
and new error meanings are in `contracts/anx-schema.yaml`. `GET /agents/me` remains the
derived agent's self-identification endpoint. Existing human passkey and human invite
routes remain.

| Route | Contract |
| --- | --- |
| `POST /auth/hosts/enrollments` | Public interactive start; returns code, verification path, secret poll token, interval, expiry. |
| `GET /auth/hosts/enrollments/{enrollment_id}` | Poll with `X-ANX-Enrollment-Token`. |
| `POST /auth/hosts/enrollments/{enrollment_id}/complete` | Complete approved request with poll token and host-key signature; adopt proved agents atomically. |
| `GET /auth/hosts/enrollments/pending` | Human auth-admin sees pending requests and requesting IP. |
| `POST /auth/hosts/enrollments/{enrollment_id}/approve` | Human auth-admin approves and reserves slug. |
| `POST /auth/hosts/enrollments/{enrollment_id}/deny` | Human auth-admin denies. |
| `GET /auth/hosts/enrollment-tokens` | Human auth-admin lists headless token metadata, without secrets. |
| `POST /auth/hosts/enrollment-tokens` | Human auth-admin creates one-time headless token. |
| `POST /auth/hosts/enrollment-tokens/{token_id}/revoke` | Human auth-admin revokes unused headless token. |
| `POST /auth/hosts/enrollments/headless` | Public one-shot enrollment with token, host-key signature, and adoption proofs. |
| `GET /hosts` | Any workspace principal lists hosts. |
| `GET /hosts/{host_id}` | Any workspace principal reads host, exclusions, and child agents. |
| `PATCH /hosts/{host_id}` | Human auth-admin or signed host updates display name and exclusions. |
| `DELETE /hosts/{host_id}` | Human auth-admin revokes host and all child credentials in one audited transaction. |
| `POST /hosts/{host_id}/bridge/check-in` | Signed host bridge heartbeat for all enabled child handles. |
| `GET /runs` | Any workspace principal filters runs by card, agent, host, state, or active. |
| `POST /runs` | Derived agent upserts its run by `(launcher, host_id, external_id)` with monotonic state. |
| `GET /runs/{run_id}` | Any workspace principal reads one run. |
| `GET /agents` | Any workspace principal reads the derived-state roster. |
| `GET /agents/{agent_id}` | Any workspace principal reads agent detail, recent work, runs, asks, and notes. |
| `PATCH /agents/me/presence` | Derived agent sets current card and optional progress note. |
| `POST /auth/agents/register` (removed) | No agent invite or public-key self-registration. |
| `PATCH /agents/me` (removed) | Derived handles cannot be renamed independently of their hosts. |
| `POST /agents/me/keys/rotate` (removed) | Derived agents have no independent signing keys. |
| `POST /agents/me/revoke` (removed) | Human auth-admin revokes the host or principal through admin routes. |
| `POST /agent-bridge/check-in` (removed) | Host-signed bridge check-in replaces per-agent bridge identity. |

| Grant or header | Contract |
| --- | --- |
| `POST /auth/token` `host_assertion` | Host signs `anx-host-agent-token|host_id|key_id|agent_name|signed_at`; one-use, five-minute skew, short-lived derived-agent access token without refresh. |
| `X-ANX-Enrollment-Token` | Secret interactive poll credential; sent as a header. |
| `X-ANX-Host-Key-Id` | Active host key ID for host self-access. |
| `X-ANX-Host-Signed-At` | RFC3339 timestamp for host self-access, at most five minutes skew. |
| `X-ANX-Host-Signature` | Base64 Ed25519 request-bound signature for host self-access. |
| `X-ANX-Run-Id` | Optional authenticated-write attribution `agentctl/<external_id>`; core resolves or creates a provisional run and records run, host, agent, adapter. |

| New error code | Meaning |
| --- | --- |
| `host_slug_taken` | Host slug already enrolled or reserved. |
| `adoption_proof_invalid` | Existing principal key or adoption signature is invalid. |
| `adoption_conflict` | Principal or target host/name cannot be adopted. |
| `enrollment_pending` | Completion preceded approval. |
| `enrollment_denied` | Human denied the request. |
| `enrollment_expired` | Interactive request expired. |
| `enrollment_consumed` | Interactive request already finalized. |
| `host_revoked` | Host or active key is revoked. |
| `agent_excluded` | Host excludes requested agent name. |
| `agent_handle_taken` | A standalone principal already owns the derived handle. |
| `run_identity_conflict` | Existing run key belongs to another immutable identity. |
| `run_state_regression` | Observation would regress run state. |
| `run_attribution_invalid` | Run header is malformed or belongs to another agent/host. |

## Workstream decisions

- 2026-09-27: Adoption proofs bind a client-generated 128-bit request nonce and host
  public key because the enrollment ID does not exist when start is submitted; core
  freezes proved adoptions before human approval. Local `--exclude` omits profiles
  from the request, leaving those standalone principals active until revoked.
  (`contracts/anx-openapi.yaml`, `contracts/anx-schema.yaml`.)
- 2026-09-27: Host self-access uses timestamped Ed25519 proof headers over the exact
  request body. Headless token lifetime is 10 minutes to 24 hours. Newly excluded
  child names lose outstanding sessions while retaining actor/history. These close
  unspecified replay and exclusion windows. (`contracts/anx-openapi.yaml`,
  `contracts/anx-schema.yaml`.)
- 2026-09-27: An attributed write may precede callback ingest, so the run header
  creates an unknown-state provisional run keyed by the caller's host and external
  ID. Host bridge check-in moves to a host-signed route; wake queue paths remain
  infrastructure routes but must authenticate the host bridge when implemented.
  (`contracts/anx-openapi.yaml`, `contracts/anx-schema.yaml`,
  `contracts/non-openapi-endpoints.yaml`.)
