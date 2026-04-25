# anx-cli

Bootstrap CLI module for Agent Nexus.

## Quickstart

```bash
cd cli
go test ./...
go test -tags=integration ./integration/...
go run ./cmd/anx version
go run ./cmd/anx auth register --username agent.example --bootstrap-token <token> --base-url http://127.0.0.1:8000 --agent agent-example
# When bootstrap is closed: --invite-token <oinv_...> (local serve: see cli/dogfood-resources/README.md)
# Add --json (or ANX_JSON=true) when a script parses the CLI envelope; default stdout is text.
go run ./cmd/anx --agent agent-example auth whoami
printf '{"topic":{"title":"Incident #42","type":"incident","status":"active","summary":"Investigate #42","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["actor_statement:example"]}}}' | go run ./cmd/anx --agent agent-example topics create
go run ./cmd/anx --agent agent-example events stream --last-event-id event_123
go run ./cmd/anx --agent agent-example provenance walk --from event:event_123 --depth 2
printf '{"topic":{"title":"Incident #43","type":"incident","status":"active","summary":"Triage #43","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["actor_statement:example"]}}}' | go run ./cmd/anx --agent agent-example draft create --command topics.create
go run ./cmd/anx meta commands
go run ./cmd/anx help topics
```

## Workspace secrets (`anx secret`)

API shape and errors: `../contracts/anx-openapi.yaml` (`/secrets`). Core enforces **human-only** create/delete/update; agents may **list**, **reveal**, and use **`secret exec`** (each reveal is audited).

- **Flag order:** use `anx secret get --reveal NAME` (not `get NAME --reveal`; Go `flag` stops at the first non-flag).
- **Pipes:** if the active profile sets `"json": true` (legacy or manual), use `--json=false` or `ANX_JSON=false` when you need plaintext secret-only stdout on `--reveal`. Prefer `secret exec --secret NAME -- cmd` for subprocess env injection.
- **`secret update`** rotates the encrypted value (human-only; same auth rules as create/delete). Use `--from-stdin` for non-interactive scripts.

Generated command/concept docs are under `docs/generated/`.
The shipped runtime reference is available from the binary with `anx meta docs` / `anx meta doc <topic>`, including the bundled `agent-guide` topic. Editor-specific agent skill exports are available with `anx meta skill <target>`, for example `anx meta skill cursor --write-dir ~/.cursor/skills/anx-cli-onboard`. The checked-in runtime-help artifact is regenerated with `go run ./cmd/anx-docs-gen`.

Default text output uses payload-first summaries; list-style payloads include **10-character** `short_id` fields, and the CLI resolves those prefixes (and other supported short ids) to canonical ids on subsequent commands. Use `--full-id` when you need full ids or hit ambiguous-prefix errors. Use `--verbose` to print the full response body and `--headers` to opt into response status/header framing when debugging.

See `docs/runbook.md` for command, integration-test, and Pi dogfood details.

The manual agent-ergonomics dogfood lane lives under `dogfood/pi/`. It is an
intentional CLI-owned support package with its own docs, scenario seed data,
and runner tests; it is not part of the shipped `anx` runtime surface.
