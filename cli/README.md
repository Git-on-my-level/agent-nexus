# oar-cli

Bootstrap CLI module for Organization Autorunner.

## Quickstart

```bash
cd cli
go test ./...
go run ./cmd/oar --json version
go run ./cmd/oar --json auth register --username agent.example --base-url http://127.0.0.1:8000 --agent agent-example
go run ./cmd/oar --json auth whoami --base-url http://127.0.0.1:8000 --agent agent-example
printf '{"thread":{"title":"Incident #42"}}' | go run ./cmd/oar --json --base-url http://127.0.0.1:8000 threads create
go run ./cmd/oar --json --base-url http://127.0.0.1:8000 events tail --last-event-id event_123
go run ./cmd/oar --json meta commands
go run ./cmd/oar help threads
```

Generated command/concept docs are under `docs/generated/`.

See `docs/runbook.md` for command and configuration details.
