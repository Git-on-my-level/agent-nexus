package app

import "strings"

func agentBridgeGuideText() string {
	const tickToken = "<<tick>>"
	guide := strings.TrimSpace(`Agent bridge

Use this when you want the preferred per-agent bridge path for wake registration and live <<tick>>@handle<<tick>> delivery.

What changed

- The main CLI now owns the per-agent bootstrap path for fresh machines:
  - <<tick>>oar bridge install<<tick>>
  - <<tick>>oar bridge import-auth<<tick>>
  - <<tick>>oar bridge init-config<<tick>>
  - <<tick>>oar bridge start|stop|restart|status|logs<<tick>>
  - <<tick>>oar bridge workspace-id<<tick>>
  - <<tick>>oar bridge doctor<<tick>>
- The Python package still owns runtime behavior:
  - <<tick>>oar-agent-bridge auth register<<tick>>
  - <<tick>>oar-agent-bridge bridge run<<tick>> under the hood
  - <<tick>>oar-agent-bridge notifications list|read|dismiss<<tick>> for bridge-local pull flows
- The workspace wake-routing service is deployment-owned and runs inside <<tick>>oar-core<<tick>>, not through <<tick>>oar bridge<<tick>>.
- Registrations become taggable once the registration and workspace binding are valid. Fresh bridge check-in only controls whether delivery is immediate.

Install on a fresh machine with only <<tick>>oar<<tick>>

1. Install the bridge runtime into a managed Python <<tick>>3.11+<<tick>> virtualenv:

  oar bridge install

  By default, this installs from <<tick>>main<<tick>> and writes the launcher into <<tick>>~/.local/bin<<tick>>. Override with <<tick>>--ref<<tick>> or <<tick>>--bin-dir<<tick>> if needed. The current bootstrap path also requires <<tick>>git<<tick>> on PATH.

2. If you need bridge test dependencies on the same machine:

  oar bridge install --with-dev

3. Verify the wrapper works:

  oar-agent-bridge --version

Contributor path from a repo checkout

- For local development inside this repo, prefer:
  - <<tick>>make setup<<tick>>
  - <<tick>>make doctor<<tick>>
  - <<tick>>make test<<tick>>
- Local contributor rules for the adapter live in <<tick>>adapters/agent-bridge/AGENTS.md<<tick>>.

Config generation

Generate minimal configs from the CLI:

  oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>
  oar bridge init-config --kind zeroclaw --output ./zeroclaw.toml --workspace-id <workspace-id> --handle <handle>

These templates intentionally default the agent lifecycle to:

- <<tick>>status = "pending"<<tick>>
- <<tick>>checkin_interval_seconds = 60<<tick>>
- <<tick>>checkin_ttl_seconds = 300<<tick>>

That is the guardrail for live delivery: the bridge still needs to check in before the agent shows online, but humans can tag a valid offline registration and let notifications queue.

Workspace id source of truth

- <<tick>><workspace-id><<tick>> must be the durable workspace id for the deployment, not a slug and not a UI path segment.
- If an <<tick>>agentreg.<handle><<tick>> document already exists, use <<tick>>oar bridge workspace-id --handle <handle><<tick>> to read its enabled workspace bindings first.
- If the workspace deployment already documents the configured <<tick>>workspace_id<<tick>>, copy that exact value.
- If the deployment is driven by control-plane workspace records, copy the durable <<tick>>workspace_id<<tick>> from that workspace record, not the slug.
- The bundled example value <<tick>>ws_main<<tick>> is only a sample.
- If you still do not know the real workspace id for your deployment, stop and ask the operator. Do not guess.

First-time agent-host path

1. Install the runtime:

  oar bridge install

2. Render the agent config:

  oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>

3. If a matching <<tick>>oar<<tick>> profile already exists for the target principal, import it into the bridge config:

  oar bridge import-auth --config ./agent.toml --from-profile <agent>

4. Register the target bridge principal and write the initial pending registration when auth does not already exist:

  oar-agent-bridge auth register --config ./agent.toml --invite-token <token> --apply-registration

5. Start the managed bridge daemon from the main CLI:

  oar bridge start --config ./agent.toml

6. Confirm the process and readiness state before expecting immediate delivery:

  oar bridge status --config ./agent.toml
  oar bridge doctor --config ./agent.toml

  Use <<tick>>oar bridge logs --config ./agent.toml<<tick>> when you need the recent daemon output, and <<tick>>oar bridge restart --config ./agent.toml<<tick>> if you change config or recover from a stale process.

  The doctor should report both adapter readiness and the bridge as online for immediate delivery. If it still says offline, stale, or adapter probe failed, tags will queue notifications until you fix that.

7. Post a test wake message containing <<tick>>@<handle><<tick>>.

8. Confirm the durable trace:
  - <<tick>>message_posted<<tick>>
  - <<tick>>agent_wakeup_requested<<tick>>
  - if online, <<tick>>agent_wakeup_claimed<<tick>>
  - if online, bridge reply <<tick>>message_posted<<tick>>
  - if online, <<tick>>agent_wakeup_completed<<tick>>
  - if offline, the notification remains queued until the bridge reconnects

9. Pull or dismiss queued notifications directly when needed:

  oar notifications list --status unread
  oar notifications dismiss --wakeup-id <wakeup-id>
  oar-agent-bridge notifications list --config ./agent.toml --status unread

10. If the bridge is online but tagged delivery still fails, hand off to the workspace operator to inspect the embedded wake-routing sidecar in <<tick>>oar-core<<tick>>.

Lifecycle note

- <<tick>>oar-agent-bridge registration apply<<tick>> writes the registration document, but the bridge runtime still owns live presence updates.
- The bridge runtime refreshes registration readiness on check-in.
- If the bridge stops checking in, the registration stays taggable but delivery falls back to queued notifications until the bridge returns.
- The preferred operational path is to manage the bridge daemon with <<tick>>oar bridge start|stop|restart|status|logs<<tick>>, not ad hoc shell backgrounding.

Troubleshooting

- <<tick>>oar-agent-bridge: command not found<<tick>>:
  - run <<tick>>oar bridge install<<tick>> or add the managed wrapper directory to PATH
- bridge doctor says the bridge is offline:
  - the bridge has not checked in yet or is no longer refreshing; start or restart <<tick>>oar bridge start --config ./agent.toml<<tick>> and verify the config points at the right workspace
- wake request is durable but never claimed:
  - the bridge is offline, the embedded wake-routing sidecar in <<tick>>oar-core<<tick>> is unhealthy, or <<tick>>workspace_id<<tick>> is wrong
- principal exists but wake still fails:
  - inspect <<tick>>agentreg.<handle><<tick>> for actor mismatch, disabled status, stale check-in, or missing workspace binding

Related docs

  oar help bridge
  oar meta doc wake-routing
  oar bridge doctor --config ./agent.toml`)
	return strings.ReplaceAll(guide, tickToken, "`")
}
