# PM turn activity

Conversations pin up to eight typed `context_refs`; `work_ref` remains the
primary task for older clients. Ask PM resolves context and inline answer refs
through batches of at most 200 refs for the loaded conversation pages. Context persists in History. Until the
shared WorkSummary component lands (SCA-694), the header uses the shared ref chip,
computed phase and last-moved age.

The selected PM actor can send activity through the existing
`POST /pm/turns/{turn_id}/heartbeat` route while holding its current live lease.
No new route or SSE connection is introduced. The existing conversation poll
returns the retained log, latest draft and final answer under the same reader
permissions. Core retains at most 50 events with a short label (120 bytes) and
optional target (160 bytes). Only `status` and `tool` kinds are accepted. Increasing
sequence numbers make identical retries safe; conflicting or evicted retries
fail with 409. Core stamps `recorded_at`. Send `partial_response` with increasing
`partial_sequence` for a full draft snapshot, bounded by `max_output_bytes`.
An omitted draft preserves it; an empty snapshot clears it. Drafts are not final
answers and do not complete turns.

## Runner protocol

A core claim advertises `activity_supported`; older cores omit it, so the CLI
keeps lease-only heartbeats. A direct `anx pm serve --runner '... {prompt}'` runner may emit newline-delimited
JSON on stdout while it executes:

```json
{"type":"pm_activity","kind":"status","label":"Reading the task"}
{"type":"pm_activity","kind":"tool","label":"anx work get","target":"card:release"}
{"type":"pm_partial","text":"The task is awaiting review."}
{"role":"assistant","content":"The task is awaiting review."}
```

The CLI recognizes only the two explicit progress types, assigns sequence
numbers, retains a bounded snapshot and forwards it with lease heartbeats at
most five seconds apart. It removes progress lines before extracting the final
answer. Invalid or oversized progress is ignored. Other native stdout does not
become activity. The initial status says “Preparing an answer”; the adapter
must report actual tools and model steps. On an older core that rejects the new
heartbeat fields, the CLI falls back to lease-only renewal. Agentctl result
collection does not expose live native events through this protocol.

Adapters must map native events to allowlisted names and short public targets.
Never emit tool arguments, full tool results, credentials, environment contents,
prompts or raw native event payloads. Emit partial text only when the native
runner supports an actual draft stream. Keep provider-specific translation in
the deployment adapter, outside OSS. A runner may also call the heartbeat route
directly using the same contract.

## Latency evidence and rollout

`created_at`, `claimed_at`, activity `recorded_at` and the runner's existing
“completed in Ns” log separate queue wait from execution. Measure a real simple
question before recommending any model or concurrency change. The prompt now
uses requesting-reader `pm turns context --limit 10 --context-ref <pinned-ref>`, names pinned refs, avoids
workspace lists and repeated tool loops, and forbids explaining internal turn
mechanics. These changes alone do not establish a sub-60-second serving result.

A deployment adapter must inventory its native runner's event capabilities,
map available events to this protocol and validate a simple-question latency
trace before claiming the target. No route classification changes are needed:
existing heartbeat/conversation/ref routes retain their methods, transport and
authority boundaries. The turn-context request adds the optional `context_ref`
selector, restricted to persisted pins and evaluated under the requesting reader.

## Request cost

Heartbeat reads and writes one PM record via its `(kind,id)` primary key; log
processing is O(50), draft processing O(max_output_bytes), independent of
workspace size. Conversation history uses the existing scoped pagination and
PM parent/scope indexes, with at most 50 events and one bounded draft per turn.
The UI resolves loaded page refs in batches of at most 200 requested refs using existing indexed
reference resolution, replacing the previous four task-list page scans.
Each poll remains bounded by the requested conversation page. Native progress
parsing retains at most 50 events and one line/draft bounded by the output limit.
