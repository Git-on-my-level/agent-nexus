# OAR Agent Bridge

Bridge adapters for Organization Autorunner (OAR).

This package is bridge-only. Workspace `@handle` routing is owned by the embedded `oar-router` sidecar inside `oar-core`. `oar-agent-bridge` assumes durable wake requests already exist in OAR and focuses on per-agent execution.

This package implements three things:

1. **Wake registration metadata** stored on the authenticated OAR principal
2. **Bridge readiness check-ins** stored in OAR events and reflected in that registration
3. **Local bridge adapters** that consume wake events and invoke concrete agents

Included adapters:

- `hermes_acp` - launches `hermes acp` and speaks ACP over stdio
- `zeroclaw_gateway` - POSTs wake prompts to a running ZeroClaw Gateway `/webhook`

## Why this shape

The bridge uses OAR's existing canonical primitives instead of inventing a parallel state system:

- registration = OAR auth principal metadata
- bridge check-in = OAR event
- wake request/claim/fail/complete = OAR events
- wake packet = OAR artifact

That means the agent-side runtime stays small and works against the current OAR API surface.

## Install

There are two supported install paths.

### 1. Fresh machine with only `oar` installed

This is the canonical bootstrap path for agent operators:

```bash
oar bridge install
oar-agent-bridge --version
```

That command:

- requires Python `3.11+`
- currently requires `git` on PATH
- creates a managed virtualenv for the bridge
- installs `oar-agent-bridge` from `main` unless you pin `--ref`
- writes an `oar-agent-bridge` launcher into `~/.local/bin` by default

If you need bridge test dependencies on the same machine:

```bash
oar bridge install --with-dev
```

### 2. Contributor workflow from this repo checkout

Use the adapter-local make targets:

```bash
make setup
make doctor
make test
```

The equivalent manual path is:

```bash
cd adapters/agent-bridge
python3.11 -m venv .venv
source .venv/bin/activate
python -m pip install --upgrade pip
python -m pip install -e .[dev]
```

## Commands

Register an OAR principal and save local key state:

```bash
oar-agent-bridge auth register --config examples/hermes.toml --invite-token <token> --apply-registration
```

Read the authenticated principal:

```bash
oar-agent-bridge auth whoami --config examples/hermes.toml
```

Apply or refresh wake registration after auth already exists:

```bash
oar-agent-bridge registration apply --config examples/hermes.toml
```

Inspect whether the agent is online for immediate delivery:

```bash
oar-agent-bridge registration status --config examples/hermes.toml
oar bridge status --config examples/hermes.toml
oar bridge doctor --config examples/hermes.toml
```

Pull or dismiss queued notifications directly:

```bash
oar notifications list --status unread
oar notifications dismiss --wakeup-id wake_123
oar-agent-bridge notifications list --config examples/hermes.toml --status unread
oar-agent-bridge notifications dismiss --config examples/hermes.toml --wakeup-id wake_123
```

Import existing `oar` auth into a bridge config instead of manually translating profile material:

```bash
oar bridge import-auth --config ./agent.toml --from-profile agent-a
```

Discover durable workspace ids from an existing registration:

```bash
oar bridge workspace-id --handle hermes
oar bridge workspace-id --document-id agentreg.hermes
```

Run a bridge for a concrete agent:

```bash
oar-agent-bridge bridge run --config examples/hermes.toml
oar-agent-bridge bridge run --config examples/zeroclaw.toml
```

Probe adapter readiness without starting the daemon loop:

```bash
oar-agent-bridge bridge doctor --config examples/hermes.toml
```

Preferred lifecycle management from the main CLI:

```bash
oar bridge start --config examples/hermes.toml
oar bridge status --config examples/hermes.toml
oar bridge logs --config examples/hermes.toml
oar bridge stop --config examples/hermes.toml
```

## Config files

See:

- `examples/hermes.toml`
- `examples/zeroclaw.toml`

Minimum config contract:

- Every config requires `[oar] base_url`, `[oar] workspace_id`, and `[oar] workspace_name`.
- Optional `[oar]` fields are `workspace_url` and `verify_ssl`.
- `[auth] state_path` is optional; if omitted it defaults under `.state/`.
- `bridge run` requires an `[agent]` section with at least `handle`, `state_dir`, and `workspace_bindings`.
- Bridge-managed agent configs also default to:
  - `status = "pending"`
  - `checkin_interval_seconds = 60`
  - `checkin_ttl_seconds = 300`
- Hermes ACP bridges also require `[adapter] kind = "hermes_acp"`, `command`, `cwd_default`, and `[adapter.workspace_map]`.
- ZeroClaw bridges also require `[adapter] kind = "zeroclaw_gateway"`, `base_url`, and `bearer_token`.

Presence lifecycle:

- Registrations start `pending`.
- The bridge runtime publishes the live readiness check-in event and flips the registration to `active`.
- The registration also records the bridge-generated public proof key and the latest check-in event id.
- The workspace router only treats the agent as online when that event carries a valid bridge proof signature for the registered key.
- Humans can tag a valid registered agent even while it is offline; the wake is stored as a durable notification until the bridge returns.
- If the bridge stops checking in, the registration becomes offline and routing queues notifications instead of immediate bridge delivery.

Workspace identity:

- `workspace_id` must be the durable workspace id, not a slug and not a UI path segment.
- If an existing registration is available, start with `oar bridge workspace-id --handle <handle>` to inspect its enabled workspace bindings.
- If the workspace deployment already documents its configured `workspace_id`, copy that exact value.
- If the deployment is driven by control-plane workspace records, copy the durable `workspace_id` from that workspace record, not the slug.
- The example value `ws_main` in this repo is only a sample.
- If you still do not know the real deployment value, stop and ask the operator. Do not guess.

Token choice:

- Use `--bootstrap-token` when bootstrapping the first principal in an environment.
- Use `--invite-token` for later principals after an invite has been created.

Minimal Hermes bridge config:

```toml
[oar]
base_url = "https://oar.example"
workspace_id = "<workspace-id>"
workspace_name = "Main"

[auth]
state_path = ".state/hermes-auth.json"

[agent]
handle = "<handle>"
driver_kind = "acp"
adapter_kind = "hermes_acp"
state_dir = ".state/hermes"
workspace_bindings = ["<workspace-id>"]
resume_policy = "resume_or_create"
status = "pending"
checkin_interval_seconds = 60
checkin_ttl_seconds = 300

[adapter]
kind = "hermes_acp"
command = ["hermes", "acp"]
cwd_default = "/absolute/path/to/your/hermes/workspace"

[adapter.workspace_map]
"<workspace-id>" = "/absolute/path/to/your/hermes/workspace"
```

## First-time operator path

1. Install the runtime and verify the wrapper exists:

```bash
oar bridge install
oar-agent-bridge --version
```

2. Confirm the workspace deployment's `oar-core` config and note the durable `workspace_id` it uses.

3. Generate or edit the agent config with your OAR base URL, durable workspace identity, and adapter-specific settings:

```bash
oar bridge workspace-id --handle <handle>
oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --workspace-path /absolute/path/to/hermes/workspace
oar bridge import-auth --config ./agent.toml --from-profile <agent>
```

If you omit `--workspace-path`, the Hermes template is written with placeholder paths and the CLI prints a warning so you can patch `[adapter].cwd_default` and `[adapter.workspace_map]` before starting the bridge. When `import-auth` reads a profile with a different OAR host, it also updates the default local `[oar].base_url` in the config so the two steps compose.

4. Register the agent and write its initial pending wake registration in one step:

```bash
oar-agent-bridge auth register --config ./agent.toml --invite-token <token> --apply-registration
```

5. Start the bridge through the main CLI process manager:

```bash
oar bridge start --config ./agent.toml
```

6. Confirm the bridge has checked in if you want immediate delivery:

```bash
oar bridge status --config ./agent.toml
oar bridge doctor --config ./agent.toml
oar-agent-bridge registration status --config ./agent.toml
```

The doctor path also probes the downstream adapter configuration before the bridge is treated as online for immediate delivery.

7. Post a thread message such as `@hermes summarize the latest onboarding blockers.` The expected durable trace is:

- existing `message_posted`
- new `agent_wakeup_requested`
- new `agent_wakeup_claimed`
- new `message_posted` from the bridge
- new `agent_wakeup_completed`

8. If the registration already exists and you want a bridge-managed refresh, run:

```bash
oar-agent-bridge registration apply --config examples/hermes.toml
```

If a registration apply returns a conflict or validation error, inspect the authenticated principal and update the bridge config instead of retrying blindly.

If a human tags the agent before step 6 succeeds, the notification should still be queued as long as the registration and workspace binding are valid. The bridge will consume it after it comes back online.

## File layout

- `oar_agent_bridge/registry.py` - registration apply/status and check-in publication
- `oar_agent_bridge/bridge.py` - wake claim, adapter dispatch, reply/failure writeback
- `oar_agent_bridge/adapters/hermes_acp.py` - Hermes ACP adapter
- `oar_agent_bridge/adapters/zeroclaw_gateway.py` - ZeroClaw Gateway adapter

## Event and artifact conventions

### Wake registration

Structured content version:

```text
agent-registration/v1
```

### Wake artifact

Artifact kind:

```text
agent_wake
```

Artifact ID is deterministic from:

```text
workspace_id + thread_id + trigger_event_id + target_actor_id
```

### Wake events

- `agent_wakeup_requested`
- `agent_wakeup_claimed`
- `agent_wakeup_completed`
- `agent_wakeup_failed`

### Reply event

Bridge writeback uses normal OAR `message_posted` with refs back to the thread, trigger event, and wake artifact.

## Session identity

The cross-agent session key is:

```text
oar:<workspace_id>:<thread_id>:<handle>
```

Adapters map that stable key into their native session model.

## Tests

```bash
make setup
make test
```
