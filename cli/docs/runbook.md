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
- `oar auth register`
- `oar auth whoami`
- `oar auth update-username`
- `oar auth rotate`
- `oar auth revoke`
- `oar auth token-status`
- `oar meta commands`
- `oar meta command <id>`
- `oar meta concepts`
- `oar meta concept <name>`

## Typed resource commands (v0)

- `oar threads list|get|create|update`
- `oar commitments list|get|create|update`
- `oar artifacts list|get|create|content`
- `oar events get|create|tail`
- `oar inbox list|ack|tail`
- `oar work-orders create`
- `oar receipts create`
- `oar reviews create`
- `oar derived rebuild`

### `oar api call` examples

```bash
oar api call --path /version
printf '{"thread":{"title":"t"}}' | oar api call --method POST --path /threads --json
oar api call --raw --path /health
```

## Auth lifecycle examples

```bash
oar --json --base-url http://127.0.0.1:8000 --agent agent-a auth register --username agent.a
oar --json --base-url http://127.0.0.1:8000 --agent agent-a auth whoami
oar --json --base-url http://127.0.0.1:8000 --agent agent-a auth update-username --username agent.a.renamed
oar --json --base-url http://127.0.0.1:8000 --agent agent-a auth rotate
oar --json --base-url http://127.0.0.1:8000 --agent agent-a auth token-status
oar --json --base-url http://127.0.0.1:8000 --agent agent-a auth revoke
```

Notes:
- `auth register` and `auth update-username` also accept `OAR_USERNAME`.
- Profile and key material are written with strict local permissions (`0700` directories, `0600` files).
- Access tokens refresh automatically for authenticated commands when near expiry.

## Typed command examples

```bash
oar --json threads list --status active
printf '{"thread":{"title":"Incident #42"}}' | oar --json threads create
printf '{"thread":{"status":"resolved"}}' | oar --json threads update --thread-id thread_123

printf '{"commitment":{"thread_id":"thread_123","title":"Ship fix"}}' | oar --json commitments create

oar --json events tail --thread-id thread_123 --last-event-id event_100
oar --json inbox tail --cursor inbox:item-1@abcd

oar artifacts content --artifact-id artifact_123 > artifact.bin
oar --json meta commands
oar --json meta command threads.list
oar help threads
```
