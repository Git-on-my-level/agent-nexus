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
