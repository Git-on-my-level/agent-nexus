# Agent Nexus MCP

`anx-mcp` is the standalone local/self-hosted MCP server for Agent Nexus. It
runs over stdio by default and calls the workspace HTTP API through the shared
MCP catalog and executor.

## Build

```bash
cd mcp
go build -o anx-mcp ./cmd/anx-mcp
```

Full standalone setup, local-client smoke, and unsupported/gated tool notes live
in [`docs/standalone.md`](docs/standalone.md).

## Configuration

`anx-mcp` intentionally uses only workspace-local configuration. It contains no
hosted OAuth, org, billing, provider-connection, or managed-slot logic.

Flags:

- `--profile <name-or-path>`: ANX profile name, or a path to a profile JSON file
- `--agent <name>`: explicit ANX profile/agent selector
- `--base-url <url>`: workspace `anx-core` base URL override
- `--log-level <debug|info|warn|error>`: diagnostics go to stderr only
- `--timeout <duration>`: workspace HTTP timeout, default `30s`

Environment overrides:

- `ANX_AGENT`
- `ANX_PROFILE_PATH`
- `ANX_BASE_URL`
- `ANX_ACCESS_TOKEN`

The profile reader is a minimal duplicate of the CLI profile resolution rules
because the CLI packages are under Go `internal/` boundaries. It reads
`~/.config/anx/default-profile` and `~/.config/anx/profiles/*.json`, auto-selects
a single local profile, and errors if multiple profiles exist without an
explicit selector.

## MCP Inspector

Example with a named local profile:

```bash
npx @modelcontextprotocol/inspector \
  --command "$(pwd)/anx-mcp" \
  --args "--profile leo --log-level info"
```

Example without a saved profile:

```bash
ANX_ACCESS_TOKEN="$TOKEN" \
  npx @modelcontextprotocol/inspector \
  --command "$(pwd)/anx-mcp" \
  --args "--base-url http://127.0.0.1:8000 --agent leo"
```

The stdio transport is newline-delimited JSON-RPC. stdout is reserved for MCP
messages; logs and startup diagnostics are written to stderr.

Run the automated local smoke against an active workspace profile:

```bash
ANX_MCP_SMOKE_PROFILE=leo ./scripts/standalone-smoke.mjs
```
