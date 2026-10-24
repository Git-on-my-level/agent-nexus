# Agent Nexus CLI

## Quickstart

For a local development core, a human auth-admin bootstraps the workspace, then enrolls each machine once:

```bash
: "${ANX_BASE_URL:?Set to this workspace's reachable core API URL}"
anx host enroll --plan
anx host enroll --name "$(hostname -s | tr A-Z a-z)"
anx auth whoami
anx orient
```

Enrollment prints a user code and verification URL for human approval. For a shared deployment, set `ANX_BASE_URL` to its reachable core URL first. For CI, pipe a one-time headless token to `anx host enroll --token-stdin`. The first enrolled workspace becomes the default if none is set; later enrollments preserve that choice and print `anx config use <alias>` to select the new workspace. Run `anx config workspaces` when unsure, and use `anx config map "~/work/project/**" <alias>` for directory rules. Preferences are user-global in `~/.config/anx/workspaces.json`; never hardcode `--base-url` in agent prompts. Multiple enrolled workspaces without a selection fail with repair commands. The host key is stored owner-only under `~/.config/anx/hosts/<workspace-key>/`.

Inside `agentctl run`, or where anx sees an unambiguous active harness marker, the CLI may resolve identity automatically. Otherwise, pass `--as <agent-name>` or set `ANX_AS=<agent-name>` to the lowercase name of the agent tool you are running in. Its first authenticated call registers that name on this host if it is new. Doctor gives this repair when `identity_resolution` fails. Stop only if you cannot tell which agent tool you are running in; otherwise rerun doctor with that identity. See [host and runs runbook](docs/runbook.md) for token scripting and the agentctl subscription recipe.

## Workspace secrets (`anx secret`)

API shape and errors: `../contracts/anx-openapi.yaml` (`/secrets`). Core enforces **human-only** create/delete/update; agents may **list**, **reveal**, and use **`secret exec`** (each reveal is audited).

- **Flag order:** use `anx secret get --reveal NAME` (not `get NAME --reveal`; Go `flag` stops at the first non-flag).
- **Pipes:** use `--json=false` or `ANX_JSON=false` when you need plaintext secret-only stdout on `--reveal`. Prefer `secret exec --secret NAME -- cmd` for subprocess env injection.
- **`secret create` / `secret update`** require `--from-stdin` for secret values and never prompt implicitly. Create/delete/update remain human-only.

Generated command/concept docs are under `docs/generated/`.
The shipped runtime reference is available from the binary with `anx meta docs` / `anx meta doc <topic>`, including the bundled `agent-guide` topic. Install the opinionated ANX agent skill with `anx install skill --path <path>`; `anx meta skill anx` renders the same skill to stdout. The checked-in runtime-help artifact is regenerated with `go run ./cmd/anx-docs-gen`.

Default text output uses payload-first summaries and is the preferred mode for normal agent orientation. Resource output leads with public typed refs such as `card:<handle>` and JSON envelopes expose `ref` and `handle` as the primary identity fields. Commands pass typed refs and bare handles through to core for resolution. Use `--json` for code, scripts, CI, or `jq`, and use `--verbose` / `--headers` when debugging response framing.

See `docs/runbook.md` for command, integration-test, and Pi dogfood details.

## Network timeouts and retries

`--timeout` (or `ANX_TIMEOUT`) defaults to **60 seconds per exchange**, including
response-body reads and retry backoff. Host token exchange and the subsequent API
read have separate budgets. An earlier caller deadline always wins. TCP connect
and TLS handshake each have a **10 second** limit, further bounded by the attempt
and total budgets.

GET/HEAD reads, generated `refs resolve` reads, and host assertions get at most
three attempts. Each read attempt is capped at `min(total, max(20s, total/3))`
(20 seconds by default), with 200 ms then 400 ms backoff (scaled down for short
budgets). A short explicit timeout can be spent on one attempt; increasing the
total above 60 seconds also increases the attempt limit.
Retries cover timeouts, broken/refused connections, temporary DNS failures and
5xx responses. `Retry-After` is honored within the remaining total budget; if it
cannot fit, the last failure is returned. Permanent DNS failures, TLS verification
failures and 4xx/auth rejections stop immediately. Host assertions are signed
afresh for every attempt because core consumes each proof once.

Ordinary writes are not automatically retried, even with a `request_key` or an
`Idempotency-Key` header: a key alone does not establish that the server supports
atomic replay for that route. An uncertain write keeps its request key and
`outcome_unknown` recovery guidance. Streaming connections keep their existing
resume policy.

Error envelopes retain schema v2 and their existing fields/codes. Network errors
add `error.details.failure_class` (`timeout`, `connection_refused`,
`connection_reset`, `connection_closed`, `connection_error`, `dns`, `tls`,
`canceled` or `network_error`), `phase`, `attempts`, `timeout_ms`,
`attempt_timeout_ms` and `connect_timeout_ms`. HTTP errors add `failure_class`
(`http_status` or `auth_rejection`) alongside `status`. Timeout failures exit 8;
connection failures exit 6 and auth rejections exit 5. The text error names the
network failure without printing request URLs, credentials or signed proofs.

The manual agent-ergonomics dogfood lane lives under `dogfood/pi/`. It is an
intentional CLI-owned support package with its own docs, scenario seed data,
and runner tests; it is not part of the shipped `anx` runtime surface.

Read the same executive projection as the web UI with `anx overview --json`.
Pin a visual-report document with `anx workspace dashboard set document:<handle>`;
use `anx workspace dashboard set none` to return to the newest report. Workspace
summary counts exclude cards on archived boards. The web UI Archive view keeps
those cards accessible.

`anx workspace dashboard list` reads validated pin candidates on demand; Overview
returns only the selected dashboard. Cards attached to archived project topics
are excluded along with cards on archived boards.

## Release updates

`anx update status|now|policy auto|notify|off` manages installer-owned CLI releases.
The default `auto` starts a detached daily worker after the first successful read
or coordination write in an interactive terminal, or in a non-interactive agent
invocation identified by `--as`/`ANX_AS`, agentctl caller evidence, or a recognized
harness marker. Known CI provider markers always skip updates. Recognized harness
markers such as Cursor, Claude Code, Codex, Hermes, and agentctl can override
generic `CI=1`; `--as`/`ANX_AS` alone cannot. Ordinary non-interactive scripts are skipped. The worker
starts after command output and cannot change that command's output or exit code.
A separate worker checks `/meta/handshake` at most once per UTC day and runs the
release check when `core_version` is newer than the CLI. `anx update status`
reports the last automatic attempt and server-version trigger; when the handshake
does not expose `core_version`, status reports `skipped_no_server_version`. The
server check uses the workspace selected by the triggering command; with no
resolved workspace it records `skipped_no_workspace`. A failed handshake keeps a
short diagnostic and falls back to the daily release check.
Help, `update status`, dry runs, local maintenance, and `ANX_UPDATE_POLICY=off`
or `notify` keep automatic installation disabled for non-interactive agents.
After replacement, the next command emits a one-line `warning code=...` notice
that the `anx` skill may need reloading. A managed skill conflict is recorded and
surfaced once without counting as an update failure or retrying the update.
Existing installs need one rerun of `scripts/install-anx.sh` to obtain a
digest-bound ownership receipt. See [self-update policy](docs/self-update.md) for
rollback, skill sync, and compatibility details.

## Harness integrations

Harness hooks are **off by default**, separate from `anx install skill` and skill
sync. Opt in once to mirror Claude Code's suggested follow-up tasks as cards:

```bash
anx integrations list
anx integrations enable claude-code-suggested-tasks --board follow-ups --dry-run
anx integrations enable claude-code-suggested-tasks --board follow-ups
anx integrations status claude-code-suggested-tasks
anx integrations disable claude-code-suggested-tasks
```

The default scope edits `~/.claude/settings.json`. `--scope project` edits
`./.claude/settings.json`; `--scope local` edits `./.claude/settings.local.json`.
Run project/local setup from the project root. Settings must contain a JSON object
(an existing empty file is refused). ANX identifies ownership by the complete generated invocation namespace (quoted
absolute binary, exact config directory, recipe, and installation ID), so a
changed executable path can be repaired after an interrupted setup. It preserves unrelated keys and hooks,
keeps a one-time `.anx-backup` before changing an existing file, and uses locked,
atomic replacements. Before renaming, ANX checks file identity and contents,
re-merging up to three times if the harness wrote concurrently; continued edits
refuse setup. This check narrows the filesystem check/rename race; harness writers
do not share ANX's lock. New parent directories are explicitly chmodded to 0700;
existing directory modes remain unchanged. Object formatting/key order can
change; hook order is kept. Disable removes the ANX handler and drops its group,
including group metadata, when no handlers remain. Groups with other handlers
retain their metadata. Config is removed only after verifying the owned hook is
absent from settings.
Dry-run reports the exact prospective settings/config JSON without writing files.

Workspace selection uses the hook event's `cwd` and the normal `anx config map`
rules, even when the invoking shell exports `ANX_BASE_URL`. Ordinary commands
retain their usual environment precedence. `--workspace <alias>` pins a workspace instead. Board/column choices are
stored per resolved workspace; rerun enable from another mapped directory to
record its board. Missing boards stay queued as `no_board`; no board is created
implicitly. The derived host agent defaults to `claude-code`; `--as <name>`
overrides it. Enroll the host normally before delivery.

The hook uses the absolute running executable path from `os.Executable` without
resolving symlinks again. This retains a stable launcher path where the OS
provides one and avoids deliberately pinning Homebrew/mise symlink targets.
Some platforms return a versioned executable path; after moving/removing that
binary, status reports drift and rerunning enable repairs the recorded command.
The hook explicitly pins the ANX config directory and does not require PATH.

`integrations run` is a hook-only stdin entrypoint. It writes no stdout, exits 0,
and spends at most three seconds (the installed hook timeout is ten seconds).
Every accepted event is persisted before delivery. Missing or corrupt installation
config retains the event as `missing_config` or `config_invalid` and emits one
short diagnostic; status remains available. Failed requests keep their
original workspace, agent, request key, and complete card body for replay through
the existing actor-scoped card-create API. A task ID determines the request key;
when absent, session ID plus tool-use ID is required. Recent local receipts are
only an optimization; the server's atomic replay receipt prevents duplicates.

The spool retains at most 100 events total (pending plus dead letters) for 30
days, drains at most four pending events per invocation, rotates retryable
failures to avoid starvation, and keeps the last 1,000 local receipts. A definitive
card-create HTTP 400 or 422 becomes a dead letter, preserving its text without
retrying it. HTTP 401, 403 and 404 remain pending as `auth`, `forbidden` and
`board_not_found` so later hooks can replay after access or the board is fixed.
Other failures remain pending. Status reports `dead_letter_count`,
`dead_letter_reasons` (such as `terminal_http_400`), pending reasons, ledger size,
and retention/capacity drops. Input is limited to 64 KiB;
card titles/bodies are defensively truncated, and the retained prompt is quoted.
Malformed, oversized or unidentifiable input and storage failures produce one
short stderr diagnostic. A storage failure prevents sending unpersisted work.
The enrolled host and token principal are bound durably before the first card
request. If either changes, pending events stay queued as `identity_changed` rather than
replaying under a different actor; restore the original principal to retry.
The owner-only spool contains task text: keep the ANX config directory private.
Retention is enforced on hook invocations; status does not mutate the spool.
Disable removes the hook and integration config but preserves pending work and
receipts for inspection and later re-enablement. Disabling all installed scopes
stops automatic delivery; it does not cancel an already-running invocation.

The adapter schema follows the [Claude Code hooks reference](https://code.claude.com/docs/en/hooks).
`PostToolUse` matches `mcp__ccd_session__spawn_task` exactly. Availability of that
specific desktop MCP tool and activation of edited settings depend on the
installed Claude Code version; ANX status checks configuration, not activation.

Developers add recipes in `internal/integrations/registry.go` with an event parser,
default agent, existing ANX action command ID and body builder; the recipe supplies
the harness provenance stamp. Adapters own allowed/default scopes, project scope
semantics, settings location, merge and inspection. Neither requires a new CLI command. The first
adapter installs POSIX-shell commands; Windows hook-shell support is not yet
qualified.

### Workspace and resource ergonomics

`anx orient` reports the resolved workspace alias, base URL, selection source
(`--workspace`, `--base-url`, `ANX_BASE_URL`, directory rule, default or the sole
enrollment), and other configured aliases. JSON adds these in the top-level `workspace` field, separate from `result`, which retains the verbatim `brief.v1` object.

When a typed resource ref is missing, the CLI can suggest another configured,
already authenticated workspace. This is best effort: at most four distinct other
workspace URLs, a 350 ms timeout per lookup and a 1.5 s overall budget. Only that
workspace's unexpired cached agent token is used; redirects and token grants are
excluded. Expired or unavailable credentials and network errors leave the original
not-found error unchanged. Set `ANX_WORKSPACE_HINTS=0` to disable these reads.
JSON errors expose `error.details.workspace_hint` and an exact retry command in
`error.next_actions`. No command is automatically retried in another workspace.

`anx url card:ship-fix` prints the resource's API locator, derived from the API
base URL, suitable for shell substitution in scripts. Board, topic, document,
artifact and thread refs also work. The legacy `anx url card ship-fix` form
remains supported. The locator is not a link for humans: it answers with JSON or
requires API auth. Use `anx link` to share a web UI link with a human.

`anx link card:ship-fix document:runbook event:<ask-id>` prints one browser URL
per typed ref, one per line, to share with a human. Links come from the
workspace core: it reports its web UI root as `web_url` in `GET /meta/handshake`,
set from `ANX_WEB_BASE_URL` on the core (for example
`https://example.com/o/<org>/w/<workspace>`). Each link is
`<web_url>/r/<path-escaped ref>`; the web UI resolves handles and ids. Refs must
be typed (`<prefix>:<value>`). `--json` returns `result.links[]` with `ref` and
`web_url`. When the core reports no `web_url`, `anx link` fails with
`needs_context` (exit 10); the CLI never derives web links from the API URL.

Single-object reads and writes (`work get|create`, `context`, `cards get|create|patch|revise|move|assign|resolve|reopen|message|reply`,
`docs get|create|put|patch|revise|message|reply`, `topics get|create|message|reply`,
`boards get|create`, `artifacts create`, and `ask|review|escalate`) add `result.web_url` for their
primary ref, and default text ends the result with one `open url=<web_url>` line. This is
best effort: one handshake read per invocation, bounded to 2 seconds (or
`--timeout` when shorter). A failed handshake or a workspace without `web_url`
omits the field and the line; the command still succeeds. Other commands never
read the handshake for this.

Create commands (`cards|work|docs|topics|boards|artifacts create`, `docs put`)
also return `result.url`, which the CLI derives from the API base URL. It is an
API-side locator kept in JSON for existing scripts, not a link for humans: it may
answer with JSON or require API auth. When `web_url` is available, default text
omits that `url` fact (and the `url=` field on `created` lines) and shows only the
`open url=` line. Share `web_url` or `anx link` output with humans.

Choose a stable card name with
`anx cards create --board board:tasks --handle ship-fix --title "Ship fix" --body "Acceptance criteria"`. For JSON-backed work
creation, use `anx work create --from-file work.json --handle ship-fix`; the flag
overrides the JSON handle. Slugs use lowercase ASCII letters and digits separated
by single hyphens, up to 64 characters; reserved names and UUIDs are rejected.
A reserved handle fails with conflict instead of being silently renamed. Omitted
handles derive from the title with a 40-character cap; existing handles stay intact.
Create text confirms the ref, handle and, when the workspace reports `web_url`, the web link. `--verbose` retains full details.

`anx note card:ship-fix "Progress" --next 1d "Acceptance checks"` confirms the saved
note ref and resolved promise date/text. Suggestions render as one shell command;
JSON preserves their argument arrays.

Optional context hooks and Git notes/promises are off by default. See [agent hooks](docs/agent-hooks.md) for harness installation, explicit capture-writer binding, atomic `Anx-Next` promises and offline recovery.

### Optional sandbox file transport

`anx sync enable <directory> --card-id card:<handle> --dry-run` previews an explicit
outbox/mirror binding. Enabling registers a default-off files job in the one host
bridge; it never installs a service. A sandbox needs only the writable outbox and
no credentials or network. Notes and optional next promises only; mirror timestamps
must be checked by readers. See [File transport](docs/file-transport.md).

Attention placement and feedback: `anx help attention` documents `anx attention
place`, `anx attention agent get|set`, and `anx attention feedback list`. See
[attention records](../docs/attention.md) for principal refs, pagination, replay
keys, and the guarantee that an open human ask remains in Decide.
