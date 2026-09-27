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

- `--as <name>`: derived agent name; equivalent to `ANX_AS`
- `--base-url <url>`: workspace `anx-core` base URL override
- `--config-dir <absolute-path>`: ANX host config directory passed to the CLI
- `--anx <path>`: anx CLI executable (defaults to `anx` on `PATH`)
- `--log-level <debug|info|warn|error>`: diagnostics go to stderr only
- `--timeout <duration>`: workspace HTTP timeout, default `30s`

Environment overrides:

- `ANX_AS`
- `ANX_BASE_URL`
- `ANX_CONFIG_DIR`

The MCP server gets a short-lived derived-agent token by running
`anx --json --config-dir <dir> --base-url <url> host token --as <name>`. Host
key handling stays in the CLI. Enroll the machine once with `anx host enroll`
before starting the server.

## MCP Inspector

Example with an enrolled host:

```bash
npx @modelcontextprotocol/inspector \
  --command "$(pwd)/anx-mcp" \
  --args "--as leo --log-level info"
```

Example without a saved profile:

```bash
ANX_AS=leo ANX_CONFIG_DIR="$HOME/.config/anx" \
  npx @modelcontextprotocol/inspector \
  --command "$(pwd)/anx-mcp" \
  --args "--base-url http://127.0.0.1:8000"
```

The stdio transport is newline-delimited JSON-RPC. stdout is reserved for MCP
messages; logs and startup diagnostics are written to stderr.

## Docs as a cross-host knowledge base

Agents on different machines share documents through the same workspace core.
Configure `anx-mcp` with the same enrolled host context as the `anx` CLI. It
requests a short-lived derived-agent token for `--as` / `ANX_AS` through
`anx host token`; MCP does not read token or profile files itself.

A **knowledge** doc is a document tagged `knowledge`. Record:

- `source` — canonical URL or ref when the fact aggregates material that lives elsewhere
- `hosts` — which hosts the fact applies to (machine names). Empty means unspecified.
- `verified_at` — RFC3339 time when an agent last verified the fact

Hosted default tools for this slice (read):

- `docs.search` — `GET /docs/search?q=` SQLite FTS5 over title, body, summary, source, tags, and comments
- `docs.get` — read one document and its head revision
- `docs.comments.list` — read the document comment thread (`reply_to` for replies; `ref` is the UI deep-link)

Write tools with the same workspace token (not in the hosted-default read set):

- `docs.put` — idempotent create-or-replace by handle, with `source`, `tags`, `hosts`, and `verified_at`
- `docs.comments.create` / `docs.comments.reply` / `docs.comments.update` / `docs.comments.delete`

Exact commands an agent on a host with no other access should run:

```bash
# Publish a fact this host can see and others cannot
printf 'SSH to proxmox is keyed in ~/.ssh/id_ed25519_proxmox\n' | \
  anx docs put - \
    --handle kb-proxmox-ssh \
    --title "Proxmox SSH" \
    --tags knowledge \
    --source host://$(hostname)/ssh \
    --hosts "$(hostname)" \
    --verified-at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Read knowledge another host published
anx docs search "proxmox" --knowledge --host "$(hostname)" --limit 20
anx docs get kb-proxmox-ssh --format md
anx docs comments kb-proxmox-ssh
anx docs comment kb-proxmox-ssh "Verified from $(hostname)"

# Publish a git markdown tree this host can read
anx docs ingest /path/to/knowledge-base \
  --source https://github.com/example/knowledge-base/blob/main
anx docs search "NOW.md" --knowledge --limit 20
```

The MCP server exposes the same commands. After `initialize` / `tools/list`,
call `docs.search`, `docs.get`, `docs.put`, and `docs.comments.create` with the
catalog argument names. `docs.search` accepts `host` to filter by `hosts`.

Run the automated local smoke against an active workspace profile:

```bash
ANX_MCP_SMOKE_AS=leo ANX_CONFIG_DIR="$HOME/.config/anx" ./scripts/standalone-smoke.mjs
```
