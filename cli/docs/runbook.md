# oar-cli Runbook (bootstrap)

## Build

```bash
cd cli
go build ./cmd/oar
```

## Test

```bash
cd cli
go test ./...
```

## Global flag precedence

Resolution order is:

1. command-line flags
2. environment variables
3. profile file (`~/.config/oar/profiles/<agent>.json` by default)
4. built-in defaults

Supported env vars:

- `OAR_BASE_URL`
- `OAR_AGENT`
- `OAR_JSON`
- `OAR_NO_COLOR`
- `OAR_TIMEOUT`
- `OAR_PROFILE_PATH`
- `OAR_ACCESS_TOKEN`

## Baseline commands

- `oar version`
- `oar doctor`
- `oar api call`

### `oar api call` examples

```bash
oar api call --path /version
printf '{"thread":{"title":"t"}}' | oar api call --method POST --path /threads --json
oar api call --raw --path /health
```
