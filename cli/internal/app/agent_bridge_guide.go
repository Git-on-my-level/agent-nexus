package app

func agentBridgeGuideText() string {
	return `Agent bridge

Enroll this machine once with anx host enroll. Run one bridge per enrolled host.
The bridge obtains short-lived tokens through anx host token --as <name> and
calls host-signed CLI helpers for check-in and wake mutations. It stores no
agent keys, copied refresh tokens, or per-agent homes.

Create one bridge.toml with [host] base_url, id, slug, config_dir and one [agents.<name>]
command array for each active, non-excluded derived agent. Then run:

  anx bridge install
  anx bridge start --config ./bridge.toml
  anx bridge status --config ./bridge.toml
  anx bridge doctor --config ./bridge.toml
  anx bridge stop --config ./bridge.toml

When agentctl is available, each wake uses agentctl run and a command
subscription to anx runs ingest. The subscription passes --config-dir and
--base-url because agentctl's command environment has no HOME or ANX variables.
A card subject adds anx.card.<slug>.
Without agentctl, the runtime receives ANX_AS=<name>. The runtime should
post its own response with anx; a run's end does not finish a card.
`
}
