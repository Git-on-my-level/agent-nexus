# Agent Nexus CLI

## Quickstart

A human auth-admin bootstraps the workspace, then enrolls each machine once:

```bash
export ANX_BASE_URL=http://127.0.0.1:8091
anx host enroll --plan
anx host enroll --name my-mac
anx --as codex auth whoami
anx --as codex orient
```

Enrollment prints a user code and verification URL for human approval. For CI, use a one-time headless token: `anx host enroll --token <token>`. Enrollment prints `anx config use <alias>` to select that workspace without changing your default. Run `anx config workspaces` when unsure, and use `anx config map "~/work/project/**" <alias>` for directory rules. Preferences are user-global in `~/.config/anx/workspaces.json`; never hardcode `--base-url` in agent prompts. Multiple enrolled workspaces without a selection fail with repair commands. The host key is stored owner-only under `~/.config/anx/hosts/<workspace-key>/`.

Inside `agentctl run`, the CLI resolves the adapter and run attribution automatically. Use `--as` or `ANX_AS` for a persona or when no harness context is available. See [host and runs runbook](docs/runbook.md) for token scripting and the agentctl subscription recipe.

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
The default `auto` starts a short-lived daily worker on successful coordination
writes; reads and dry runs remain exempt. `ANX_UPDATE_POLICY=off` disables automatic
checks in CI. Existing installs need one rerun of `scripts/install-anx.sh` to obtain
a digest-bound ownership receipt. See [self-update policy](docs/self-update.md) for
rollback, skill sync, compatibility, and the agentctl ergonomics audit.
