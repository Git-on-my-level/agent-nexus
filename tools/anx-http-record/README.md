# anx-http-record

Local development reverse proxy that records CLI-to-`anx-core` HTTP exchanges in a
single interleaved JSONL log.

## Scope

- local dev only
- CLI-to-core recording
- completion-ordered `seq` values for replay-oriented causality
- bounded request/response body capture
- default probe filtering for `/readyz` and `/version`
- default exclusion of SSE endpoints `/events/stream` and `/inbox/stream`
- companion compiler and replay commands for seed experimentation

SSE is intentionally excluded in v1 because the recorder defines global order at
response completion, and long-lived streams do not have a useful completion
point for seed generation.

## Run From Repo Root

```bash
./scripts/anx-http-record \
  --listen 127.0.0.1:8010 \
  --upstream http://127.0.0.1:8000 \
  --output /tmp/anx-record.jsonl
```

Then point the CLI at the proxy instead of core directly:

```bash
ANX_BASE_URL=http://127.0.0.1:8010 anx --agent support-lead topics list
```

If you want an explicit human-analysis label separate from the normal
`X-ANX-Agent` header, send `X-ANX-Record-Agent`. The proxy strips that header
before forwarding upstream.

## Compile And Replay

Compile a successful mutation recording into a replay artifact:

```bash
./scripts/anx-http-compile \
  --input /tmp/anx-record.jsonl \
  --output /tmp/anx-seed.json
```

Replay that artifact against a fresh core:

```bash
./scripts/anx-http-replay \
  --input /tmp/anx-seed.json \
  --base-url http://127.0.0.1:8000 \
  --bindings-output /tmp/anx-seed-bindings.json
```

The compiler is intentionally scoped to the current seed-style mutation flows:

- successful `POST`/`PUT`/`PATCH`/`DELETE` exchanges only
- JSON request bodies required for placeholder-aware compilation
- `/auth/*` exchanges skipped
- response-derived placeholders inferred for topic/thread, document/revision,
  board, and board-card IDs
- replay retries transient `5xx` responses with short backoff to mirror the
  current seed scripts' startup-contention handling

## Flags

- `--listen`: listen address for the proxy. Default `127.0.0.1:8010`.
- `--upstream`: required upstream base URL, usually local `anx-core`.
- `--output`: required JSONL output path.
- `--max-body-bytes`: max bytes retained per request or response body. Default `1048576`.
- `--mutations-only`: record only `POST`, `PUT`, `PATCH`, and `DELETE`.

## Recording Behavior

- `seq` is assigned when the proxied exchange completes and the recorder writes
  the JSONL line.
- `ts` is completion time in UTC RFC3339Nano.
- Sensitive headers are redacted: `Authorization`, `Cookie`, `Set-Cookie`,
  `Proxy-Authorization`, `X-Api-Key`, and `X-ANX-Bootstrap-Token`.
- JSON body keys are redacted when present: `token`, `access_token`,
  `refresh_token`, `bootstrap_token`, `invite_token`, and `password`.
- If JSON body redaction cannot be applied safely, the recorder omits that body
  and marks it as redacted.
- Truncated JSON bodies are kept as best-effort redacted `json_fragment`
  snippets instead of being dropped entirely.
- Large bodies are truncated to the configured byte cap and marked with
  truncation booleans.

## Test

```bash
make http-record-test
```
