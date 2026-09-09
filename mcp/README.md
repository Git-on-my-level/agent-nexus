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

## Docs as a cross-host knowledge base

Agents on different machines share documents through the same workspace core.
Configure `anx-mcp` with the same profile (or `ANX_BASE_URL` + `ANX_ACCESS_TOKEN`)
the `anx` CLI uses; MCP authorization is the workspace bearer token, not a
separate MCP credential.

Hosted default tools for this slice (read):

- `docs.search` — `GET /docs/search?q=` over title, body, source, tags, and comments
- `docs.get` — read one document and its head revision
- `docs.comments.list` — read the document comment thread

Write tools with the same workspace token (not in the hosted-default read set):

- `docs.put` — idempotent create-or-replace by handle, with `source` and `tags`
- `docs.comments.create` / `docs.comments.reply`

Tag agent-facing docs `knowledge`. `source` is a canonical URL or ref when the
document aggregates material that lives elsewhere. Git-repo ingest is not in
this slice.

Example: two profiles on one core (stand-in for two hosts):

```bash
# host A
ANX_ACCESS_TOKEN="$TOKEN_A" anx --json docs put notes.md \
  --title "Runbook" --source https://example.invalid/runbook.md --tags knowledge

# host B
ANX_ACCESS_TOKEN="$TOKEN_B" anx --json docs search "runbook" --knowledge
ANX_ACCESS_TOKEN="$TOKEN_B" anx --json docs comment doc:notes "Found this on host B"

# host A
ANX_ACCESS_TOKEN="$TOKEN_A" anx --json docs comments kb-shared
```

The MCP server exposes the same commands. After `initialize` / `tools/list`,
call `docs.search`, `docs.get`, `docs.put`, and `docs.comments.create` with the
catalog argument names.

Run the automated local smoke against an active workspace profile:

```bash
ANX_MCP_SMOKE_PROFILE=leo ./scripts/standalone-smoke.mjs
```
