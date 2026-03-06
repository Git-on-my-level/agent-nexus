# oar-cli

Bootstrap CLI module for Organization Autorunner.

## Quickstart

```bash
cd cli
go test ./...
go test -tags=integration ./integration/...
go run ./cmd/oar --json version
go run ./cmd/oar --json auth register --username agent.example --base-url http://127.0.0.1:8000 --agent agent-example
go run ./cmd/oar --agent agent-example auth whoami
printf '{"thread":{"title":"Incident #42"}}' | go run ./cmd/oar --agent agent-example threads create
go run ./cmd/oar --agent agent-example events stream --last-event-id event_123
printf '{"thread":{"title":"Incident #43"}}' | go run ./cmd/oar --agent agent-example draft create --command threads.create
go run ./cmd/oar --json meta commands
go run ./cmd/oar help threads
```

Generated command/concept docs are under `docs/generated/`.

See `docs/runbook.md` for command, integration-test, and Pi dogfood details.
