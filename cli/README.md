# oar-cli

Bootstrap CLI module for Organization Autorunner.

## Quickstart

```bash
cd cli
go test ./...
go test -tags=integration ./integration/...
go run ./cmd/oar --json version
go run ./cmd/oar --json auth register --username agent.example --bootstrap-token <token> --base-url http://127.0.0.1:8000 --agent agent-example
go run ./cmd/oar --agent agent-example auth whoami
printf '{"thread":{"title":"Incident #42"}}' | go run ./cmd/oar --agent agent-example threads create
go run ./cmd/oar --agent agent-example events stream --last-event-id event_123
go run ./cmd/oar --json --agent agent-example provenance walk --from event:event_123 --depth 2
printf '{"thread":{"title":"Incident #43"}}' | go run ./cmd/oar --agent agent-example draft create --command threads.create
go run ./cmd/oar --json meta commands
go run ./cmd/oar help threads
```

Generated command/concept docs are under `docs/generated/`.
The shipped runtime reference is available from the binary with `oar meta docs` / `oar meta doc <topic>`, including the bundled `agent-guide` topic. Editor-specific agent skill exports are available with `oar meta skill <target>`, for example `oar meta skill cursor --write-dir ~/.cursor/skills/oar-cli-onboard`. The checked-in runtime-help artifact is regenerated with `go run ./cmd/oar-docs-gen`.

Human-readable inspection commands now default to payload-first summaries. Use `--verbose` to print the full response body and `--headers` to opt into response status/header framing when debugging.

## Command-shape compatibility aliases

The CLI supports a small exact-token compatibility layer for high-value command-shape drift:

- `oar packets receipts create ...` -> `oar receipts create ...`
- `oar packets reviews create ...` -> `oar reviews create ...`
- `oar packets work-orders create ...` -> `oar work-orders create ...`
- `oar artifacts content get ...` -> `oar artifacts content ...`
- `oar threads update ...` -> `oar threads patch ...`

These aliases are explicit and exact only; unknown command paths still fail when no compatibility alias matches.

See `docs/runbook.md` for command, integration-test, and Pi dogfood details.
