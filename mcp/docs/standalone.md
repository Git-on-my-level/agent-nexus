# Standalone anx-mcp

`anx-mcp` is the local/self-hosted MCP server for Agent Nexus. It runs over
stdio, uses normal workspace-local ANX auth/profile configuration, and calls the
workspace HTTP API through the shared MCP catalog/executor. It does not contain
hosted OAuth, billing, org, provider-connection, or managed-agent slot logic.

## Build

From the OSS repo:

```bash
cd agent-nexus/mcp
go build -o ./anx-mcp ./cmd/anx-mcp
```

For a one-shot install into a user bin directory:

```bash
cd agent-nexus/mcp
go install ./cmd/anx-mcp
```

## Select a Workspace Profile

By default `anx-mcp` follows the same local profile shape as the `anx` CLI:

- `~/.config/anx/default-profile`
- `~/.config/anx/profiles/*.json`

Use an explicit selector when the machine has more than one profile:

```bash
./anx-mcp --profile leo
./anx-mcp --agent reviewer
./anx-mcp --profile /path/to/profile.json
```

For ephemeral CI or local debugging without a saved profile, pass the workspace
base URL and token directly:

```bash
ANX_ACCESS_TOKEN="$TOKEN" ./anx-mcp \
  --base-url http://127.0.0.1:8000 \
  --agent leo
```

Supported overrides:

- `--profile <name-or-path>`
- `--agent <name>`
- `--base-url <url>`
- `--timeout <duration>`
- `ANX_AGENT`
- `ANX_PROFILE_PATH`
- `ANX_BASE_URL`
- `ANX_ACCESS_TOKEN`

Diagnostics go to stderr. stdout is reserved for newline-delimited JSON-RPC MCP
messages.

## Run Over stdio

`anx-mcp` expects one JSON-RPC request per line on stdin:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"limit":5}}' |
  ./anx-mcp --profile leo
```

For MCP Inspector:

```bash
npx @modelcontextprotocol/inspector \
  --command "$(pwd)/anx-mcp" \
  --args "--profile leo --log-level info"
```

Without a saved profile:

```bash
ANX_ACCESS_TOKEN="$TOKEN" \
  npx @modelcontextprotocol/inspector \
  --command "$(pwd)/anx-mcp" \
  --args "--base-url http://127.0.0.1:8000 --agent leo"
```

## Automated Local Smoke

The repo includes a standalone smoke that builds `anx-mcp`, starts it over
stdio, and verifies:

- `initialize`
- `tools/list`
- one read call: `anx_docs_list`
- one safe write call: `anx_docs_create`

Run it against an active local profile:

```bash
cd agent-nexus/mcp
ANX_MCP_SMOKE_PROFILE=leo ./scripts/standalone-smoke.mjs
```

Or run it with explicit workspace auth:

```bash
cd agent-nexus/mcp
ANX_BASE_URL=http://127.0.0.1:8000 \
ANX_ACCESS_TOKEN="$TOKEN" \
ANX_AGENT=leo \
./scripts/standalone-smoke.mjs
```

The smoke creates a text document titled `MCP standalone smoke <timestamp>`.
Use a disposable local workspace or a test profile when running it repeatedly.

For CI-style verification of the MCP stdio protocol and workspace executor
without a live `anx-core`, run the script against its in-process mock workspace:

```bash
cd agent-nexus/mcp
ANX_MCP_SMOKE_MOCK=1 ./scripts/standalone-smoke.mjs
```

## Unsupported and Gated Tools

The complete generated inventory lives in
[`tool-coverage.md`](tool-coverage.md). V1 exposes ordinary read and
non-destructive write tools by default for standalone use. The following classes
are deliberately not exposed or are gated:

- Bootstrap, passkey, invite-token acquisition, and raw token exchange.
- WebAuthn ceremonies and human response submission.
- SSE/streaming routes until adapted to bounded reads.
- Multipart/binary upload until a content adapter exists.
- Auth inventory, principal revocation, invite management, ops/quota telemetry,
  and projection rebuilds unless an explicit admin policy permits them.
- Secret create/reveal/update/delete, credential rotation, purge, and other
  destructive or sensitive operations unless an explicit sensitive policy
  permits them.

Tool results are redacted before returning through MCP. Raw access tokens,
refresh tokens, invite tokens, private keys, secret values, authorization
headers, and environment payloads should not appear in normal responses.
