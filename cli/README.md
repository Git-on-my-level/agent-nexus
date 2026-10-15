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
The default `auto` starts a short-lived daily worker after the first successful
read or coordination write; `anx update status`, help, local maintenance, and dry
runs remain offline. `ANX_UPDATE_POLICY=off` disables automatic checks in CI.
Existing installs need one rerun of `scripts/install-anx.sh` to obtain
a digest-bound ownership receipt. See [self-update policy](docs/self-update.md) for
rollback, skill sync, compatibility, and the agentctl ergonomics audit.

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
atomic replacements. Object formatting/key order can change; hook order is kept.
Dry-run reports the exact prospective settings/config JSON without writing files.

Workspace selection uses the hook event's `cwd` and the normal `anx config map`
rules. `--workspace <alias>` pins a workspace instead. Board/column choices are
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
Every accepted event is persisted before delivery. Failed requests keep their
original workspace, agent, request key, and complete card body for replay through
the existing actor-scoped card-create API. A task ID determines the request key;
when absent, session ID plus tool-use ID is required. Recent local receipts are
only an optimization; the server's atomic replay receipt prevents duplicates.

The spool retains at most 100 pending events for 30 days, drains at most four
per invocation, rotating failed entries to avoid starvation, and keeps the last 1,000 local receipts. Status reports spool
reasons, ledger size and retention/capacity drops. Input is limited to 64 KiB;
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
existing ANX action command ID and body builder. Adapters implement settings
location, merge and inspection. Neither requires a new CLI command. The first
adapter installs POSIX-shell commands; Windows hook-shell support is not yet
qualified.
