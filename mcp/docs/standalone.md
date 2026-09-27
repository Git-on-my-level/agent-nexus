# Standalone anx-mcp

`anx-mcp` is the local/self-hosted MCP server for Agent Nexus. It runs over
stdio, obtains derived-agent tokens through the enrolled host identity owned by
the `anx` CLI, and calls the workspace HTTP API through the shared MCP catalog/executor. It does not contain
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

## Select a Derived Agent

Enroll the machine once, then select the derived agent name for this MCP
process:

```bash
anx host enroll
./anx-mcp --as reviewer
```

The MCP process asks the CLI for a short-lived token with
`anx --json --config-dir <dir> --base-url <url> host token --as <name>`. The
CLI owns host key handling. Use `--config-dir` or `ANX_CONFIG_DIR` when `HOME`
is unavailable; the directory must be absolute.

```bash
ANX_CONFIG_DIR=/workspace/.config/anx ANX_AS=reviewer ./anx-mcp \
  --base-url http://127.0.0.1:8000
```

Supported overrides:

- `--as <name>` / `ANX_AS`
- `--base-url <url>`
- `--config-dir <absolute-path>` / `ANX_CONFIG_DIR`
- `--anx <path>`: CLI executable; defaults to `anx` on `PATH`
- `--timeout <duration>`

## Docs knowledge tools

With a derived agent, `anx-mcp` exposes the same docs search/get/put/comment
commands as the CLI (`docs.search`, `docs.get`, `docs.put`, `docs.comments.*`).
Authorization uses the short-lived token returned by the host token command. Tag agent-facing docs `knowledge`.
Git-repo ingest is a CLI composition over `docs.put`:

```bash
anx docs ingest /path/to/knowledge-base \
  --source https://github.com/example/knowledge-base/blob/main
```

Diagnostics go to stderr. stdout is reserved for newline-delimited JSON-RPC MCP
messages.

## Run Over stdio

`anx-mcp` expects one JSON-RPC request per line on stdin:

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"limit":5}}' |
  ./anx-mcp --as leo
```

For MCP Inspector:

```bash
npx @modelcontextprotocol/inspector \
  --command "$(pwd)/anx-mcp" \
  --args "--as leo --log-level info"
```

With an explicit host config directory:

```bash
ANX_CONFIG_DIR="$HOME/.config/anx" \
  npx @modelcontextprotocol/inspector \
  --command "$(pwd)/anx-mcp" \
  --args "--base-url http://127.0.0.1:8000 --as leo"
```

## Automated Local Smoke

The repo includes a standalone smoke that builds `anx-mcp`, starts it over
stdio, and verifies:

- `initialize`
- `tools/list`
- one read call: `anx_docs_list`
- one safe write call: `anx_docs_create`

Run it against an enrolled host and selected agent:

```bash
cd agent-nexus/mcp
ANX_MCP_SMOKE_AS=leo ANX_CONFIG_DIR="$HOME/.config/anx" ./scripts/standalone-smoke.mjs
```

Or run it with explicit workspace auth:

```bash
cd agent-nexus/mcp
ANX_BASE_URL=http://127.0.0.1:8000 \
ANX_CONFIG_DIR="$HOME/.config/anx" \
ANX_AS=leo \
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

## Unified work and PM receipts

The generated catalog includes workspace-scoped `work.*` reads and observation
submission, plus `pm.*` context, conversations, decisions and action receipt reads.
Hosted defaults include only reads. Standalone defaults additionally expose durable
requests such as observation submission and receipt reconciliation; core still
validates workspace identity, replay keys and selected-PM-agent permissions.

`pm.decisions.answer` and `pm.decisions.dispatch` are gated sensitive tools, while
`pm.bindings.create` is gated administration. Making a tool visible never grants
human approval, source-write permission, or a new channel identity. Receipt
reconciliation reads back authoritative outcomes; it must not resend actions.
Generated readers and remote observations cannot self-certify accepted completion.

Request bodies use nested canonical JSON objects (for example
`body.observation.idempotency_key` denotes an `observation` object, not a literal
key containing dots). The optional MCP `idempotency_key` is mapped to the
observation's nested key or the PM creation request's `request_key`; conflicting
keys fail locally. Versioned annotations, refresh, approval, dispatch and receipt
reconciliation use their documented version/action identity and do not advertise
a generic replay-key option.
