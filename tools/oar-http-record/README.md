# oar-http-record

Local development reverse proxy that records CLI-to-`oar-core` HTTP exchanges in a
single interleaved JSONL log.

## Scope

- local dev only
- CLI-to-core recording
- completion-ordered `seq` values for replay-oriented causality
- bounded request/response body capture
- default probe filtering for `/readyz` and `/version`
- default exclusion of SSE endpoints `/events/stream` and `/inbox/stream`

SSE is intentionally excluded in v1 because the recorder defines global order at
response completion, and long-lived streams do not have a useful completion
point for seed generation.

## Run From Repo Root

```bash
./scripts/oar-http-record \
  --listen 127.0.0.1:8010 \
  --upstream http://127.0.0.1:8000 \
  --output /tmp/oar-record.jsonl
```

Then point the CLI at the proxy instead of core directly:

```bash
OAR_BASE_URL=http://127.0.0.1:8010 oar --agent support-lead topics list
```

If you want an explicit human-analysis label separate from the normal
`X-OAR-Agent` header, send `X-OAR-Record-Agent`. The proxy strips that header
before forwarding upstream.

## Flags

- `--listen`: listen address for the proxy. Default `127.0.0.1:8010`.
- `--upstream`: required upstream base URL, usually local `oar-core`.
- `--output`: required JSONL output path.
- `--max-body-bytes`: max bytes retained per request or response body. Default `1048576`.
- `--mutations-only`: record only `POST`, `PUT`, `PATCH`, and `DELETE`.

## Recording Behavior

- `seq` is assigned when the proxied exchange completes and the recorder writes
  the JSONL line.
- `ts` is completion time in UTC RFC3339Nano.
- Sensitive headers are redacted: `Authorization`, `Cookie`, `Set-Cookie`,
  `Proxy-Authorization`, `X-Api-Key`, and `X-OAR-Bootstrap-Token`.
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
