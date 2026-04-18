# CLI Resource Command Decomposition Plan

## Scope
This plan defines an incremental, behavior-preserving split of `cli/internal/app/resource_commands.go` into smaller files inside the same `app` package.

The current ticket note saying the file is "roughly 4,100 lines" is stale in this checkout: the file is 891 lines, but it still mixes too many responsibilities:

- typed resource command routing,
- per-resource flag parsing and command ID mapping,
- shared request/body parsing,
- streaming reconnect logic,
- response/output shaping, and
- generated command path/header helpers.

Goal: make common CLI resource edits local to one file without changing command paths, command IDs, output envelopes, local validation, or streaming behavior.

Non-goals:

- no contract or API changes,
- no new package boundaries,
- no command/help surface changes,
- no output-schema changes.

## Audit Summary

### Current code seams

| Concern | Current functions | Notes |
|---|---|---|
| Typed-resource router | `runTypedResource` | Already a clean dispatch seam; keep as the stable entrypoint from `commands.go`. |
| Domain command handlers | `runThreadsCommand`, `runTopicsCommand` / `runCardsCommand`, `runArtifactsCommand`, `runEventsCommand`, `runInboxCommand`, `runPacketsCreateCommand`, `runDerivedCommand` | These are the lowest-risk first moves because they mostly depend on helpers by method call, not shared local state. |
| Stream-specific parsing/behavior | `runEventsStream`, `runInboxStream`, `runTailStream`, `writeStreamEvent`, `isStreamReadTimeout` | Events and inbox share one reconnect loop; this should stay centralized after the split. |
| Transport/output helpers | `invokeTypedJSON`, `invokeArtifactContent`, `cfgWithResolvedAuthToken`, `generatedHeaders`, `normalizedHeaders`, `resolveCommandMethod`, `resolveCommandPath`, `commandSpecByID` | `invokeTypedJSON` and `commandSpecByID` are also used by `draft_commands.go`; `invokeArtifactContent` is artifact-specific and should stay with artifact command ownership. |
| Input/query helpers | `parseJSONBodyInput`, `parseIDAndBodyInput`, `parseIDArg`, `validateID`, `parseAckBodyInput`, `readBodyInput`, `decodeJSONPayload`, `addSingleQuery`, `addMultiQuery`, `firstNonEmpty` | Shared validation behavior and usage error codes live here today. |

### Current test leverage

`cli/internal/app/resource_commands_test.go` already pins the highest-risk compatibility points:

- `TestTypedThreadCommandsGolden`: JSON envelope shape, command names, thread list/create/update request wiring.
- `TestTypedWorkflowCommands`: cross-resource smoke for threads, packet creation, inbox list/ack.
- `TestArtifactContentRaw`: raw artifact content behavior.
- `TestEventsTailReconnect`, `TestInboxTailReconnect`, `TestEventsStreamDefaultNoFollow`: stream reconnect and default follow semantics.
- `TestTypedCommandUsageFailures`: local validation failure returns exit code `2`.

This existing file should stay in place during the refactor. Do not split tests as part of the first implementation pass unless a new focused characterization test is needed for a gap.

## Architecture Decision
Keep everything in `cli/internal/app`.

Do not introduce a new subpackage for resource commands. The split is purely file-level:

- package-private helpers stay reachable without API churn,
- `commands.go` can keep calling `runTypedResource(...)`,
- review risk stays limited to file movement plus small cleanup,
- the refactor remains easy to land incrementally with `go test` after each step.

## Target File Map

| File | Responsibility | Functions owned after refactor |
|---|---|---|
| `cli/internal/app/resource_commands.go` | Typed-resource router only | `runTypedResource` |
| `cli/internal/app/resource_threads.go` | Thread CLI behavior | `runThreadsCommand` |
| `cli/internal/app/resource_topics_cards.go` | Topics/cards CLI behavior | `runTopicsCommand`, `runCardsCommand` |
| `cli/internal/app/resource_artifacts.go` | Artifact CLI behavior | `runArtifactsCommand`, `invokeArtifactContent` |
| `cli/internal/app/resource_events.go` | Event command entrypoints and event-specific flag parsing | `runEventsCommand`, `runEventsStream` |
| `cli/internal/app/resource_inbox.go` | Inbox command entrypoints and inbox-specific flag parsing | `runInboxCommand`, `runInboxStream` |
| `cli/internal/app/resource_packets.go` | Packet/derived write entrypoints | `runPacketsCreateCommand`, `runDerivedCommand` |
| `cli/internal/app/resource_input.go` | Shared body/id/query parsing and validation | `queryParam`, `idPattern`, `parseJSONBodyInput`, `parseIDAndBodyInput`, `parseIDArg`, `validateID`, `parseAckBodyInput`, `readBodyInput`, `decodeJSONPayload`, `addSingleQuery`, `addMultiQuery`, `firstNonEmpty` |
| `cli/internal/app/resource_transport.go` | Shared typed HTTP invocation, auth/header wiring, response shaping, registry path lookup | `invokeTypedJSON`, `cfgWithResolvedAuthToken`, `generatedHeaders`, `normalizedHeaders`, `resolveCommandMethod`, `resolveCommandPath`, `commandSpecByID` |
| `cli/internal/app/resource_streaming.go` | Shared stream loop and stream output | `runTailStream`, `writeStreamEvent`, `streamPathForCommand`, `isStreamReadTimeout` |

Why this split:

- domain files own only resource-specific command semantics,
- shared helper files group by one axis of behavior instead of by call site,
- events/inbox keep their resource-specific flags near their command handlers while sharing one stream loop,
- packet/derived commands stay together because they are simple wrappers around shared JSON invocation.

## Extraction Order
Use small compile-safe moves. Each step should be behavior-preserving and shippable on its own.

### 1. Add missing characterization coverage only where the audit shows a gap
Before moving helpers, add at most a few focused tests to `cli/internal/app/resource_commands_test.go` for branches not already pinned:

- `derived rebuild` request wiring,
- `artifacts content` in `--json` mode,
- one non-stream typed resource path that relies on positional ID parsing if coverage is still missing.

Do not rewrite or reorganize the test file yet.

### 2. Extract the leaf domain handlers first
Move these functions into new files without moving helpers yet:

- `runThreadsCommand` -> `resource_threads.go`
- `runTopicsCommand` / `runCardsCommand` -> `resource_topics_cards.go` (already split in this checkout)
- `runArtifactsCommand` and `invokeArtifactContent` -> `resource_artifacts.go`
- `runPacketsCreateCommand` and `runDerivedCommand` -> `resource_packets.go`

Rationale: these are the lowest-risk moves because they only depend on existing shared helpers and return stable command-name strings.

### 3. Extract event and inbox entrypoints next
Move:

- `runEventsCommand` and `runEventsStream` -> `resource_events.go`
- `runInboxCommand` and `runInboxStream` -> `resource_inbox.go`

Keep `runTailStream` and `writeStreamEvent` in the old file until this step is green.

### 4. Extract shared input/query helpers
Move the body/id/query helpers into `resource_input.go`.

This is the first helper move because:

- the domain files will already be separated,
- the helper cluster has clear ownership,
- it preserves all existing usage-error wording/codes in one place.

### 5. Extract shared transport/path helpers
Move invocation and command-registry/path helpers into `resource_transport.go`.

Keep these together so request wiring remains centralized:

- auth token attachment,
- generated headers,
- command ID -> method/path resolution,
- response formatting for default text and JSON output.

These helpers are shared with `cli/internal/app/draft_commands.go`, so the move must preserve their signatures and package-level visibility.

### 6. Extract the shared stream loop last
Move `runTailStream`, `writeStreamEvent`, `streamPathForCommand`, and `isStreamReadTimeout` into `resource_streaming.go`.

This is last because it is the most cross-cutting helper group and already shared by both event and inbox resources.

### 7. Reduce `resource_commands.go` to the stable router
At the end of the move series, `resource_commands.go` should contain only `runTypedResource` plus any minimal file-local comment explaining that resource implementations now live in adjacent files.

## Compatibility Constraints

### Command surface

- Preserve the resource list handled by `runTypedResource` exactly:
  - `threads`
  - `topics`
  - `cards`
  - `artifacts`
  - `events`
  - `inbox`
  - `receipts`
  - `reviews`
  - `derived`
- Preserve every returned command name string (`"threads list"`, `"events tail"`, etc.) because JSON envelopes and errors include it.
- Preserve every command ID passed to generated invocation:
  - examples: `topics.patch`, `cards.list`, `packets.receipts.create`, `derived.rebuild`.

### Input/local validation

- Preserve positional-ID fallback alongside `--thread-id` / `--topic-id` / `--card-id` / `--artifact-id` / `--event-id`.
- Preserve current local error codes/messages from `errnorm.Usage(...)`, especially:
  - `subcommand_required`
  - `unknown_subcommand`
  - `invalid_flags`
  - `invalid_args`
  - `invalid_request`
  - `invalid_json`
- Preserve `validateID(...)` rules and accepted character set.
- Preserve `inbox ack` semantics:
  - stdin/`--from-file` body wins over flag-derived body,
  - `actor_id` remains optional,
  - local validation still happens when body is synthesized from flags.

### Output and transport behavior

- Preserve `invokeTypedJSON(...)` default text formatting and JSON envelope data shape.
- Preserve `artifacts content` dual behavior:
  - raw bytes when not in `--json`,
  - `status_code` / `headers` / `body_base64` / optional `body_text` when in `--json`.
- Preserve auth resolution via `cfgWithResolvedAuthToken(...)`; missing profile remains non-fatal.
- Preserve generated headers including `X-ANX-CLI-Version`, optional `X-ANX-Agent`, and optional bearer token.

### Streaming behavior

- Preserve `stream` versus `tail` default follow semantics:
  - `stream` defaults to no reconnect,
  - `tail` defaults to reconnect.
- Preserve `--follow`, `--reconnect`, `--last-event-id`, `--cursor`, and `--max-events`.
- Preserve reconnect cursor propagation via both header and `last_event_id` query param logic.
- Preserve JSON stream envelopes and non-JSON line format from `writeStreamEvent(...)`.

### Cross-command shared-helper behavior

- Preserve `commandSpecByID(...)` and `invokeTypedJSON(...)` as stable package-private helpers because `draft_commands.go` depends on them for:
  - `draft commit` execution,
  - command-ID resolution from CLI paths,
  - draft body validation against command metadata.
- Treat any change that forces edits in `draft_commands.go` as suspicious unless it is a pure import/comment adjustment.

## Exact Files To Touch (Implementation Ticket)

Must touch:

- `cli/internal/app/resource_commands.go`
- `cli/internal/app/resource_threads.go` (new)
- `cli/internal/app/resource_topics_cards.go` (topics/cards; may already exist)
- `cli/internal/app/resource_artifacts.go` (new)
- `cli/internal/app/resource_events.go` (new)
- `cli/internal/app/resource_inbox.go` (new)
- `cli/internal/app/resource_packets.go` (new)
- `cli/internal/app/resource_input.go` (new)
- `cli/internal/app/resource_transport.go` (new)
- `cli/internal/app/resource_streaming.go` (new)
- `cli/internal/app/resource_commands_test.go`

Avoid touching unless the implementation unexpectedly requires it:

- `cli/internal/app/commands.go`
- `cli/internal/app/draft_commands.go`
- `cli/internal/app/help_generated.go`
- `cli/internal/app/flags.go`
- `cli/internal/registry/*`
- `contracts/*`
- `core/*`
- `web-ui/*`

## Tests To Keep Green During The Move

After each extraction step, run the smallest affected app tests first:

- domain smoke:
  - `cd cli && go test ./internal/app -run 'TestTypedThreadCommandsGolden|TestTypedWorkflowCommands'`
- artifact content:
  - `cd cli && go test ./internal/app -run 'TestArtifactContentRaw'`
- streaming:
  - `cd cli && go test ./internal/app -run 'TestEventsTailReconnect|TestInboxTailReconnect|TestEventsStreamDefaultNoFollow'`
- local validation:
  - `cd cli && go test ./internal/app -run 'TestTypedCommandUsageFailures'`
- draft compatibility after transport-helper moves:
  - `cd cli && go test ./internal/app -run 'TestDraft'`

Repo/module gate for the finished implementation ticket:

- `cd cli && go test ./...`
- `make cli-check`

## Implementation Notes

- Do not mix file moves with semantic cleanup. If a helper rename would improve readability but is not required for the split, defer it.
- Prefer moving complete functions unchanged first; clean up comments/import grouping only after tests pass.
- Preserve the existing package name `app` everywhere.
- Keep `resource_commands_test.go` as the compatibility anchor for the first pass; follow-up test-file decomposition can be a separate ticket once code ownership has settled.
