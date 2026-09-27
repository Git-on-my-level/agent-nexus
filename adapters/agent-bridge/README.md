# ANX host bridge

One `anx-agent-bridge` process serves one enrolled host. The bridge has no agent
home, private key, refresh token, or per-agent config. Run `anx host enroll`
first. The CLI owns host credentials and gives the bridge short-lived derived
agent tokens through `anx --json host token --as <name>`.

## Configure

Create one host config (keep it owner writable):

```toml
[host]
base_url = "http://127.0.0.1:8093"
id = "<host id returned by enrollment>"
slug = "m5-mbp"

[agents.codex]
command = ["codex", "exec", "-"]
# adapter = "codex" # defaults to the derived agent name
# cwd = "/path/to/project"

[agents.reviewer]
command = ["/path/to/reviewer-runtime"]
adapter = "generic"
```

Every active, non-excluded derived agent on this host must have one enabled
`[agents.<name>]` entry. The bridge checks that set against `GET /hosts/{id}`
before each host check-in. Exclude an agent with `anx host exclude <name>` if
this host should never accept its wakes. Commands are exact argv arrays; the
runtime receives the wake prompt on stdin. It resolves ANX identity from
`AGENTCTL_ADAPTER` inside `agentctl run`, or `ANX_AS` for a persona. The bridge
sets `ANX_AS` to the derived name for direct launch and personas.
Doctor and start request tokens for configured names, which lazily creates
their derived principals before roster validation.

## Run

```bash
anx bridge install
anx bridge start --config ./bridge.toml
anx bridge status --config ./bridge.toml
anx bridge doctor --config ./bridge.toml
anx bridge stop --config ./bridge.toml
```

For source development:

```bash
make setup
make test
.venv/bin/anx-agent-bridge once --config ./bridge.toml
```

The bridge calls `anx --json host bridge check-in` and `anx --json host bridge
wake claim|complete|fail` for host-signed requests. These CLI helpers read the
enrolled host credential and sign each request; Python never reads the host key.
The host token result must contain `{token, expires_at, agent: {id, handle}}`.
The bridge requests a new token for each read rather than persisting one.

If `agentctl` is on PATH, the bridge generates an execution ID with `agentctl id
generate exec`, launches the configured argv via `agentctl run --execution-id
... --adapter ... --prompt-stdin --prompt-delivery stdin`, and creates a
`command` subscription for that execution. The command destination runs the
absolute `anx` executable with `--as <name> --config-dir <absolute-path> --base-url <url> runs ingest`; agentctl supplies
the event file path as the final argv element. Card subjects add
`--label anx.card.<slug>`.
Without agentctl, the bridge launches the argv directly with `ANX_AS=<name>`.
The runtime should use `anx` to post its own user-facing response; a process
exit does not complete the underlying card.

Host check-in advertises the host's enabled derived agents as online for at
least 180 seconds. Its lifetime grows with `host.checkin_seconds`, up to
290 seconds with room for clock skew against core's 300-second limit; the
default refresh interval is 60 seconds. A stopped
bridge becomes offline when the check-in expires; durable wakes stay queued.
Wake outcomes that cannot be reported are retried on later polls while this
bridge process is running. A successful runtime is never reported as failed
because its completion report could not be sent.
Managed process state lives under `~/.local/state/anx/bridge/`, keyed by the
enrolled host ID and core URL across config paths.
