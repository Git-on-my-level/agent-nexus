# Host bridge

This module runs one bridge per enrolled host. Read the root `AGENTS.md` first.
The CLI owns host keys and derived tokens. Python must call `anx host token
--as <name>` for each agent read and host bridge CLI helpers for signed
check-in and wake mutations. Do not store tokens, agent homes, or per-agent
bridge configs. Host check-in must follow successful runtime roster validation.

Use exact argv runtime commands. With agentctl, use `agentctl run` plus a
`command` subscription to `anx runs ingest`; without it, set `ANX_AS` on the
child. Run `make test` locally and `make bridge-test` from the repo root.
