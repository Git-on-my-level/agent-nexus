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
# Default stdout is text and should be the first choice for agent readbacks.
# Add --json (or ANX_JSON=true) only when code/scripts parse the CLI envelope.
go run ./cmd/anx --agent agent-example auth whoami
go run ./cmd/anx --agent agent-example topics create --title "Incident #42" --summary "Investigate #42"
go run ./cmd/anx --agent agent-example boards create --topic <topic-ref-or-handle> --title "Incident #42 work"
printf 'Current discussion note.\n' > message.md
go run ./cmd/anx --agent agent-example topics message <topic-ref-or-handle> --body-file message.md
printf 'Investigate checkout failures and keep evidence links current.\n' > card.md
go run ./cmd/anx --agent agent-example cards create --board <board-ref-or-handle> --topic <topic-ref-or-handle> --title "Investigate checkout failures" --body-file card.md
go run ./cmd/anx --agent agent-example cards revise <card-ref-or-handle> --body-file card.md
go run ./cmd/anx --agent agent-example events stream --last-event-id event_123
go run ./cmd/anx --agent agent-example provenance walk --from event:event_123 --depth 2
printf '{"topic":{"title":"Incident #43","summary":"Triage #43","owner_refs":[],"board_refs":[],"document_refs":[],"related_refs":[],"provenance":{"sources":["event:example"]}}}' | go run ./cmd/anx --agent agent-example draft create --command topics.create
go run ./cmd/anx meta commands
go run ./cmd/anx help topics
```

The CLI is agent-first and follows the same domain model as the web UI: Topics
for topic-centered discussion, Boards for active work, Cards for work items, and
Docs for durable knowledge. Use `create` for new resources, `revise` for
text-heavy Card/Doc bodies drafted in local files, `patch` for metadata,
`move` for Card workflow position, and `workspace` for composed context reads.

## Workspace secrets (`anx secret`)

API shape and errors: `../contracts/anx-openapi.yaml` (`/secrets`). Core enforces **human-only** create/delete/update; agents may **list**, **reveal**, and use **`secret exec`** (each reveal is audited).

- **Flag order:** use `anx secret get --reveal NAME` (not `get NAME --reveal`; Go `flag` stops at the first non-flag).
- **Pipes:** if the active profile sets `"json": true` (legacy or manual), use `--json=false` or `ANX_JSON=false` when you need plaintext secret-only stdout on `--reveal`. Prefer `secret exec --secret NAME -- cmd` for subprocess env injection.
- **`secret create` / `secret update`** require `--from-stdin` for secret values and never prompt implicitly. Create/delete/update remain human-only.

Generated command/concept docs are under `docs/generated/`.
The shipped runtime reference is available from the binary with `anx meta docs` / `anx meta doc <topic>`, including the bundled `agent-guide` topic. Install the opinionated ANX agent skill with `anx install skill --path <path>`; `anx meta skill anx` renders the same skill to stdout. The checked-in runtime-help artifact is regenerated with `go run ./cmd/anx-docs-gen`.

Default text output uses payload-first summaries and is the preferred mode for normal agent orientation. Resource output leads with public typed refs such as `card:<handle>` and JSON envelopes expose `ref` and `handle` as the primary identity fields. Commands pass typed refs and bare handles through to core for resolution. Use `--json` for code, scripts, CI, or `jq`, and use `--verbose` / `--headers` when debugging response framing.

See `docs/runbook.md` for command, integration-test, and Pi dogfood details.

The manual agent-ergonomics dogfood lane lives under `dogfood/pi/`. It is an
intentional CLI-owned support package with its own docs, scenario seed data,
and runner tests; it is not part of the shipped `anx` runtime surface.
