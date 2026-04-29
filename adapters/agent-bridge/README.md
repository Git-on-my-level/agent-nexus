# ANX Agent Bridge

Bridge adapters for Agent Nexus (ANX).

This package is bridge-only. Workspace `@handle` routing is owned by the embedded `anx-router` sidecar inside `anx-core`. `anx-agent-bridge` assumes durable wake requests already exist in ANX and focuses on per-agent execution.

This package implements three things:

1. **Wake registration metadata** stored on the authenticated ANX principal
2. **Bridge readiness check-ins** stored in ANX events and reflected in that registration
3. **Pluggable local adapters** that consume wake events and invoke code you write (subprocess JSON, the bundled Hermes ACP subprocess adapter, or optional in-process Python plugin)

## Adapter kinds

### `hermes` (recommended for Hermes ACP operators)

`anx bridge init-config --kind hermes` writes a config that uses the bundled adapter:

```toml
[bridge]
driver_kind = "hermes"
adapter_kind = "hermes"

[adapter]
kind = "hermes"
```

The bundled adapter starts Hermes over ACP stdio through the same subprocess isolation used by custom adapters, creates or resumes an ACP session, collects streamed text from `session/update` notifications, and returns the response through the stable bridge JSON contract. It discovers the Hermes binary from `HERMES_BIN`, `[adapter].hermes_bin`, or `hermes` on `PATH`. Optional controls: `HERMES_ARGS` / `[adapter].hermes_args`, `HERMES_CWD` / `[adapter].hermes_cwd`, `HERMES_INTERACTIVE` / `[adapter].interactive`, and `[adapter].command` when you need to override the Python module entrypoint.

### `subprocess` (recommended)

The bridge runs `[adapter].command` with:

- **Environment:** `ANX_BRIDGE_MODE` is `dispatch` or `doctor`
- **Stdin:** one JSON object (see `anx-agent-bridge adapter contract --config <file>`)
- **Stdout:** one JSON object (response schema below)

### `python_plugin` (advanced)

Load an explicit module and factory from config:

```toml
[adapter]
kind = "python_plugin"
plugin_module = "my_package.bridge_adapter"
plugin_factory = "build_adapter"
```

The factory must accept the `[adapter]` table via exactly one of: a single positional parameter, a `*args` bucket (receives the dict as the only vararg), keyword parameters `config` / `adapter_config` / `cfg`, or `**kwargs`. It must return an object with `doctor()` and `dispatch(...)`.

### `deterministic_ack` (tests / local QA only)

Returns canned replies; do not use as a production integration.

## Why this shape

The bridge uses ANX's existing canonical primitives instead of inventing a parallel state system:

- registration = ANX auth principal metadata
- bridge check-in = ANX event
- wake request/claim/fail/complete = ANX events
- wake packet = ANX artifact

## Install

### 1. Fresh machine with only `anx` installed

```bash
anx bridge install
anx-agent-bridge --version
```

That command:

- requires Python `3.11+`
- currently requires `git` on PATH
- creates a managed virtualenv for the bridge
- installs `anx-agent-bridge` from `main` unless you pin `--ref`
- writes an `anx-agent-bridge` launcher into `~/.local/bin` by default

```bash
anx bridge install --with-dev
```

### 2. Contributor workflow from this repo checkout

```bash
make setup
make doctor
make test
```

## Commands

Paths below assume your current working directory is `adapters/agent-bridge` inside an `agent-nexus` checkout (adjust `--config` if you run from elsewhere).

Register an ANX principal and save local key state:

```bash
anx-agent-bridge auth register --config examples/subprocess.toml --invite-token <token> --apply-registration
```

Apply or refresh wake registration after auth already exists:

```bash
anx-agent-bridge registration apply --config examples/subprocess.toml
```

Inspect whether the agent is online for immediate delivery:

```bash
anx-agent-bridge registration status --config examples/subprocess.toml
anx bridge doctor --config examples/subprocess.toml
```

Print the subprocess JSON contract your adapter must implement:

```bash
anx-agent-bridge adapter contract --config examples/subprocess.toml
```

Run a bridge:

```bash
anx-agent-bridge bridge run --config examples/subprocess.toml
anx bridge start --config examples/subprocess.toml
```

Import existing `anx` auth into a bridge config:

```bash
anx bridge import-auth --config ./bridge.toml --from-profile agent-a
```

Discover durable workspace ids from an existing registration:

```bash
anx bridge workspace-id --handle myagent
```

## Config files

See `examples/subprocess.toml`.

Minimum config contract:

- top-level `agent_home`, pointing at a directory with `agent.toml`
- `agent.toml` `[identity]` with `base_url`, `handle`, and imported identity pins (`agent_id`, `actor_id`, `key_id`, `public_key_fingerprint`)
- `agent.toml` `[auth] state_path` optional; defaults to `profiles/default.json`
- `wake.toml` with one or more `[[workspaces]]` entries; this is the local wake subscription source
- bridge `[runtime] state_dir` optional; defaults under the agent home
- **Hermes ACP:** generated as `[adapter] kind = "hermes"`, optional `command`, `hermes_bin`, `hermes_args`, `hermes_cwd`, `interactive`
- **Subprocess:** `[adapter] kind = "subprocess"`, `command` (argv array), optional `cwd`, `env`, `timeout_seconds`, `doctor_timeout_seconds`, `doctor_command`

### JSON contract (subprocess)

**Request stdin** (`schema_version` `anx-bridge-adapter-request/v1`):

- `mode`: `dispatch` | `doctor`
- `dispatch`: includes `wake_packet`, `prompt_text`, `session_key`, `existing_native_session_id`, `adapter` (opaque copy of `[adapter]` table)
- `doctor`: includes `handle`, the current `workspace_id`, and `adapter`

**Response stdout** (`schema_version` `anx-bridge-adapter-response/v1`):

- **Dispatch:** `response_text` (required), optional `native_session_id`, optional `metadata` object
- **Doctor:** `ok` (boolean), optional `message`, optional `details` object

## First-time operator path

1. `anx bridge install` and `anx-agent-bridge --version`
2. Note the deployment `workspace_id`
3. For Hermes, `anx bridge init-config --kind hermes --output ./bridge.toml --agent-home ./.anx --workspace-id <id> --handle <handle>`
4. For custom adapters, use `--kind subprocess` or `--kind python-plugin`; validate subprocess adapters with `anx-agent-bridge adapter contract --config ./bridge.toml`
5. `anx bridge import-auth --config ./bridge.toml --from-profile <agent>` when auth exists
6. `anx-agent-bridge auth register ... --apply-registration` when auth does not exist
7. `anx bridge start --config ./bridge.toml`
8. `anx bridge doctor --config ./bridge.toml`

## File layout

- `anx_agent_bridge/registry.py` - registration apply/status and check-in publication
- `anx_agent_bridge/bridge.py` - wake claim, adapter dispatch, reply/failure writeback
- `anx_agent_bridge/adapters/adapter_contract.py` - JSON schemas and sample payloads
- `anx_agent_bridge/adapters/hermes_acp.py` - bundled Hermes ACP subprocess adapter
- `anx_agent_bridge/adapters/subprocess_adapter.py` - generic subprocess runner
- `anx_agent_bridge/adapters/python_plugin.py` - explicit module loader
- `anx_agent_bridge/adapters/deterministic_ack.py` - test-only canned replies

## Tests

```bash
make setup
make test
```
