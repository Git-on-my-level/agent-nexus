# Answers belong to tasks

`anx ask`, `review` and `escalate` use the current card. With no current card,
these commands create a ready task on `ANX_ASK_DEFAULT_BOARD` (a board ref), or
the workspace default board. Explicit non-card subjects are rejected; put native
evidence in `--ref`. Core also accepts older CLI/MCP requests with typed non-card
subjects: it creates a ready card atomically with the ask, preserving the original
subject as evidence and inheriting the ask's full privacy. These compatibility
cards use a reusable Asks board for the requester. Core checks access on every
use and creates a replacement if that board becomes hidden, archived or trashed;
it does not depend on the reserved workspace default. Existing non-card asks
remain readable and answerable.

An answer and the card decision commit together. The decision links both events.
Only an explicitly matching sole ask blocker changes blocked to ready. Next actor
uses owner, requester, then board role; core's `ANX_ASK_NEXT_ACTOR_ORDER` accepts a
comma-separated order of those names. `resolved` explicitly completes native work;
free text never completes work. Source-owned phases remain unchanged.
`needs_context` is a terminal request for more context, not an answered status.
Structured access-grant requests support only approve/reject; their API exposes
`allowed_response_outcomes` so clients can restrict those actions.
Re-ask with `--supersedes event:<previous-ask>` after supplying the missing evidence.

Effective source cancellation, card closure/archive and expiry reconcile through durable, indexed batches of
200; busy workspaces may take several ticks. Open asks expose `is_stale` when the
card has not changed for `ANX_ASK_STALE_AFTER` (Go duration, default `168h`).
Subscriptions accelerate delivery; a failed subscription never loses the task's
recorded decision.

## Authoring

Use `anx ask --help` for the Markdown/frontmatter template. Supply a one-line
Decision, 2–5 Context sentences, the effects of each option, and Evidence.
Use `evidence: [{label: Change, url: https://example.org/repository/pull/123}]`
for external links; use typed document/card/topic refs in `refs`. The equivalent
CLI option is `--evidence 'Change=https://example.org/repository/pull/123'`.
Summarise source changes and link PRs or commit-pinned file lines instead of
pasting code. Obvious missing refs/links and pointer-only bodies fail locally;
other incomplete structure produces warnings. `--force --reason 'explanation'`
records the author's deliberate lint override in the event.

## Live await

The ask result includes `anx await event:<ask-id>`. It subscribes once and reads a
single indexed SSE outcome, refreshing authentication on reconnect. `--timeout
10m --json` controls duration and machine output. Exit codes: 0 answered (including
approved, acknowledged, resolved), 9 rejected, 10 needs_context, 11 withdrawn,
12 expired, 8 timeout. The response JSON includes task outcome and delivery state.
The existing `anx await --answers` batch and card-state await remain available.

## Host bridge

Enroll the host, select the same agent identity used to ask, and run:

```sh
anx --as worker ask 'Proceed?' --recommend Proceed --on-answer '/usr/local/bin/resume-work'
anx --as worker bridge run --max-attempts 5 --command-timeout 30m
```

Each ask has a 0600 JSON registration under the CLI config directory's
`answer-resumes/` namespace (0700). It records the originating working directory
and command. The server receives only subscription kind and label. It cannot
supply or change commands. Protect the config directory and host key as local
execution credentials. One process holds the namespace lock. Run a separate unit
per selected agent/workspace. This native answer runner coexists with the older
`bridge start` runtime; it consumes only its dedicated subscription wakes.

The runner receives response JSON on stdin and `ANX_ASK_ID`, `ANX_CARD_REF`,
`ANX_OUTCOME`, `ANX_RESPONSE_EVENT_ID` in its environment. Keep these values as data.
Never interpolate response text into shell code. The bridge uses host-signed
claim/complete/fail calls. Wakes are per subscription so different hosts answering
asks for the same actor cannot complete each other's deliveries. Commands have
bounded exponential retry; timeouts kill the shell's process group on macOS/Linux.
A successful command is saved before remote acknowledgement, so acknowledgement
retries do not repeat it. A crash after external effects but before local save can
repeat execution: make callbacks idempotent using `ANX_ASK_ID`.

`ANX_RESUME_CMD` supplies a generic hook. Under agentctl the verified environment
variable is `AGENTCTL_EXECUTION_ID`: the CLI registers `agentctl continue <id>
--request-key anx-<ask-id> --prompt-stdin --wait --content`. The foreground flags
keep the child result observable. An explicit `--on-answer` takes precedence.

Harness adapters belong on the host. Examples of commands for local wrappers:

- Claude Code: read stdin into a prompt and invoke `claude --resume <session-id> -p`.
- Codex through agentctl: use the automatic registration above.
- Multica: convert stdin to a concise UTF-8 comment file in the issue's workdir,
  then `multica issue comment add <issue-id> --content-file <file>`. Fix the issue
  and thread in the local wrapper; never derive shell commands from the response.
- Hermes: wrap `hermes -z` with the supported prompt input for your installed
  runner. The generic hook does not assume a resumable conversation exists.

The sample systemd unit is `examples/anx-answer-bridge.service`. On macOS, use an
ordinary launchd agent with the same argv, workspace/agent environment, RunAtLoad,
KeepAlive, and durable StandardOutPath/StandardErrorPath. Stop the service before
editing/removing local registrations. Deleting a registration prevents execution;
its server delivery remains visibly pending until acknowledged or failed.

## Signed webhooks

`anx ask ... --webhook https://service.example/answers` subscribes to one ask.
For standing subscriptions create a JSON file with `kind: webhook`, a non-secret
`label`, and `url`, then run `anx agent inbox subscribe --from-file subscription.json`
(the generated `anx help agent inbox subscribe` lists exact flags).
Core requires `ANX_SECRETS_KEY`; each subscription returns its secret once in the
creation response, stores it encrypted, and omits it from ask/delivery reads.
Store this response securely. Creating a webhook again creates another subscription;
do not blindly retry a lost creation response. The maximum is 16 per ask and 16
standing per agent, for at most 32 deliveries per terminal ask.

Payloads contain only ask/card/response IDs, status, outcome and response text,
after rechecking the recipient is active and authorized. Requests use:

- `X-ANX-Timestamp`: Unix seconds.
- `X-ANX-Signature`: `sha256=` plus lowercase HMAC-SHA256 hex of
  `timestamp + "." + exact_body_bytes`, using the returned secret's **UTF-8 text**
  as the key (do not hex-decode it).
- `X-ANX-Replay-Window: 300`: reject timestamps outside ±300 seconds.
- `Idempotency-Key`: stable delivery ID across attempts; retain a dedupe record.

Verify the signature with a constant-time comparison before parsing/acting. Return
2xx after durable acceptance. Core retries transport/non-2xx failures up to five
attempts with exponential backoff, recovering a crashed sender after its 60s lease.
A failed state with `dead_letter` reason is final. Delivery records include bounded
attempt logs with status codes and latency; request/response bodies are never logged.

Only HTTPS port 443 is accepted. DNS resolution and connect are one pinned step;
all returned addresses must be public. Loopback/private/link-local/metadata,
transition and reserved ranges are rejected. Proxies and redirects are disabled.
`ANX_WEBHOOK_ALLOW_HOSTS` narrows accepted hostnames (comma-separated exact names);
it never permits private IPs. Requests have a 10s total deadline, small header/body
limits and a 64 KiB payload cap. A receiver must remain authorized at send time.

## API and cost

`GET /asks/{ask_id}` exposes status, response, task outcome, staleness, and bounded
delivery state/logs. `GET /stream/asks/{ask_id}` performs only point reads each tick.
`POST /asks/{ask_id}/subscriptions`, `POST /agent-inbox/subscriptions` and
`POST /asks/{ask_id}/delivery` create or acknowledge subscriptions.
`GET /stream/agent-wakeups` pages a canonical actor snapshot, then the existing
append-only wake log through an actor/sequence index. Resume IDs are opaque tokens validated against visible
receipts; traversal positions remain server-local. Interrupted initial snapshots
replay safely before tail cursors are available. The bridge resumes after its
last acknowledged frame. All payload reads retain current resource scope.

Ask reads seek event IDs/handles, resolution/claim primary keys and the
`ask_deliveries_ask` index (32 subscriptions, at most 160 log entries). Lifecycle
uses `ask_subjects_due`, `ask_subjects_card_open` and a card close queue. Webhook
workers seek `ask_deliveries_due` and claim at most 20 due jobs per pass. Upgrades
install empty indexes and backfill ask subjects in resumable 200-event batches;
startup and read requests never scan historical payloads for this feature.
