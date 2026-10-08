# Agent Nexus HTTP API Contract (v0.3.0)

This document defines the **concrete HTTP/JSON surface** used for integration between **anx-core** and clients (including **the web UI** and agents).

The schema of objects is defined by `../contracts/anx-schema.yaml`.

## Conventions

- Open PM turns require an active lease for complete, fail, proposal and context operations. A missing token returns `409 lease_required` with "this turn's current lease token is required". A wrong, released, or expired lease token returns `409 lease_mismatch` with "the lease was released or re-claimed; claim the turn again". All four operations require the matching current `lease_token` in their JSON body. Identical terminal complete/fail replays with the token that finished the turn return 200 without mutation, even after the deadline; different content returns 409 turn_closed. A stale or missing token on identical terminal replay returns `409 lease_mismatch` explaining that the turn is already delivered (or failed) and no retry is needed. Core persists only a private token digest for terminal replay, while clearing the active lease. Legacy terminal records without a digest cannot authenticate a replay. Other operations past the turn deadline return `turn_closed`.
- Release requires the selected PM actor and a runner/token pair: mismatches return `409 lease_mismatch`; no active lease returns `409 turn_not_claimed` explaining that it is already released or expired and no release is needed. These errors never mutate the turn; principal/workspace permission failures remain 403.
- Direct decision creation returns 201 for a new decision and 200 for an identical proposal replay; turn proposals retain their 200 response. Replays include response-only `replayed: true` and, when no longer awaiting_answer, `replayed_terminal_status`. No new decision is created. Ordinary reads omit replay fields.
- Decision responses expose read-only `deliverable` and `delivery_path`, derived from the same scope/source executor registry as action availability. Paths include `nexus`, future configured source routes such as `github`, and `none` for unavailable routes (`configured` for older unnamed integrations). Availability does not imply approval or a valid source revision.
- Identical awaiting proposal intent (same approver, work, scope, instruction, payload, and target revision) reuses the original decision with 200 across proposers/origins without changing authorship. A different non-human proposal returns `409 human_proposal_pending` while a human-origin proposal is pending on that same approver actor, work, and scope in the workspace, with `details.pending_decision_id` and the message `Human proposal <id> is pending; a human must decline or answer it first.` Human proposals may supersede changed human/PM proposals for the same approver, work, and scope.
- Fresh approval checks current work before recording any answer or action. Stale, missing, already-at-target, or unreadable work returns `409 source_revision_changed` with `details.approved_revision`, `details.current_revision` (null if unavailable), and `details.reason` (`revision_changed`, `work_missing`, `already_at_target`, or `work_read_failed`). Declines remain allowed, and identical recorded answers replay with 200 regardless of subsequent work changes; conflicting answers still return 409. The fresh-approval message starts "Proposal target has changed (proposed at …)" and asks the owner to decline or wait for a fresh proposal; dispatch retains its approved-revision wording and independently rechecks the revision.
- Every `source_revision_changed` error on answer, dispatch, or reconcile includes `details.origin_kind` and `details.proposed_by`, copied from the decision (empty strings for legacy absent provenance). Dispatch revision mismatches include `reason: revision_changed`, the approved revision, and the observed current revision, while preserving the unsent failed attempt and receipt.
- Missing-work reconciliation returns `409 source_revision_changed` with `reason: work_missing` and the message "Nothing was delivered for this approval: the task it refers to no longer exists, so there is nothing to read back." Unsent failed actions whose work exists retain "Nothing has been delivered yet, so there is nothing to read back". These refusals do not mutate durable state.
- Dispatch against missing/trashed/purged work records an unsent failed attempt for a pending action and still returns `409 source_revision_changed` with reason `work_missing`. Receipt detail is "The task this approval refers to no longer exists (trashed or purged); nothing was sent". The owner can acknowledge the failed action. Reconciliation never resurrects it; transient `work_read_failed` errors leave durable state unchanged.
- Temporary principal authorization lookup failures return `503 unavailable` with "PM capability is not configured: principal authorization could not be read; retry the request"; unknown and revoked principals remain forbidden. No proposal is recorded on lookup failure.
- Turn proposals for an agent-owned conversation return `403 forbidden` with `details.reason: conversation_owner_not_human` and "Proposals need a human approver; this conversation belongs to an agent principal".
- Decision responses derive `work_missing`, `target_current` (`target_revision == Work.decision_revision`), and `already_at_target` (nonempty `payload.phase == Work.phase`) from current work, without storing these flags. Missing work remains visible in decision/action lists and pages; `can_answer`, `target_current`, and `already_at_target` are false. Only the owning human can decline or acknowledge/close its actions; approval, new proposals, and dispatch still require existing work. Transient lookup failures (or an unavailable work reader) are not classified as missing work: `work_missing` and `can_answer` are false, while `target_current` and `already_at_target` are explicitly null (could not be determined right now). A real revision mismatch still yields `target_current: false`; readable phase comparisons remain boolean. Approval remains refused with `work_read_failed`.
- All work.phase proposals validate structured payload shape before persistence with the dispatch validation sentence: "Invalid work.phase payload: phase must be supported and resolution_refs are required only for done."
- Turn context uses `POST /pm/turns/{turn_id}/context` with `{lease_token, query?, cursor?, limit?}` (limit 1–50, default 20), replacing GET. Turn proposals use `{lease_token, request_key, work_ref, instruction, scope, target_revision, payload?}`. Proposal insertion and replay recheck the lease under the store write lock.
- Work `version` changes on canonical mutations (phase, fields, annotations, resolution), never refresh/freshness bookkeeping. Copy `Work.decision_revision` exactly into proposal `target_revision`: nonempty `source.revision` for external work, otherwise `<work_metadata.version>.<head_revision_number>` with decimal components. Both metadata-only mutations and card content revisions change the fallback fence. Successful external reads with a new source revision change this fence; failed reads do not. Work list/get projections and dispatch use the same rule.
- Work relation objects preserve additional metadata, including migration digests and notes. Invalid shapes, kinds, and unresolved workspace refs return `400 invalid_request` with `relations` guidance. Unmapped work-store failures return `500 internal_error` and a generated `X-Request-ID`; server logs include the same ID, error types, a database code when available, and an error fingerprint. Arbitrary error text and request bodies, headers, paths, and queries are excluded to protect credentials. New event handles combine a readable type prefix with a digest of event identity, so a long-running collector cannot exhaust a per-type numeric suffix limit. Existing event refs remain valid.
- `GET /pm/turns/{turn_id}` allows the requesting actor with conversation read permission or the configured workspace PM actor with `pm.respond` permission. Public turn responses include active `lease_owner` (runner ID), never `lease_token`; only claim returns the token.
- Turn admission returns `429 busy` with `error.details.reason: conversation` and the blocking `turn_id`, or `reason: queue` with workspace `queued` and `limit`, and message "Workspace PM queue is full (20 waiting; limit 20)" (using actual counts). `ANX_PM_MAX_QUEUED` / `Config.MaxQueued` defaults to 20 and accepts positive integers; waiting turns are open, before deadline, and without an unexpired lease, including unknown wakeup outcomes. Claimed turns do not count toward the waiting limit. Conversation serialization takes precedence if both limits apply; duplicate requests remain replayable at the queue limit. `ANX_PM_MAX_CONCURRENT` / `Config.MaxConcurrent` (default 2, range 1–16) bounds workspace runner load: claimed turns with unexpired leases. Claim enforces this cap atomically. With eligible waiting work for the selected PM actor at capacity it returns `429` with `{"error":{"code":"busy","message":"Workspace PM capacity reached (2 in flight; limit 2)","details":{"reason":"capacity","in_flight":2,"limit":2,"waiting":18}}}` (actual counts substituted). `waiting` counts unleased sending/unknown turns for that actor before their deadline. Runners sleep one poll interval before retrying. Without waiting work for that actor, claim returns `204` with no body even at capacity; below capacity it returns `200` with the claimed turn and lease. Same-runner lease recovery remains allowed at capacity; expiry frees a lease slot. The `capacity` busy reason is emitted by claim; message admission does not emit it.
- Native `work.phase` and `work.annotate` dispatches read the canonical Nexus store after commit. Matching outcomes return `verified`, `independently_verified: true`, and evidence refs immediately; external executor reports remain `source_reported` until independent verification.
- Human principals proposing through a channel retain `origin_kind: human`; the existing `origin` field stores the validated channel identity. A pending human proposal blocks a non-human proposal only for the same approver actor, work, and scope, so `work.annotate` and `work.phase` do not block each other.
- Superseded decisions expose `superseded_by_proposed_by` and `superseded_by_origin_kind` from the replacement, mirroring the replacement's `supersedes_*` attribution. Unknown legacy attribution is omitted.

- Mutating requests require caller identity:
  - Mutating requests require `Authorization: Bearer <access_token>`.
  - Authenticated callers MAY omit `actor_id`; core infers it from the bearer token principal.
  - If authenticated callers provide `actor_id`, it MUST match the authenticated principal mapping.
- PM endpoints use core authentication: missing credentials return `401 auth_required`; malformed, expired, invalid, or revoked tokens return `401 invalid_token`, with the standard `recoverable` and `hint` fields. Authorization failures remain `403`.
- PM `work.phase` done proposals (direct and turn) require every `payload.resolution_refs` entry to resolve to a non-trashed record in this workspace, plus at least one artifact or event. Missing evidence returns `400 invalid_request` naming the ref. Dispatch repeats validation; vanished evidence produces a durable failed action with a cause in `receipt.detail` and no `attempts.sent_at`. Canonical card create/update/move also validate evidence inside the write transaction. Revision refs require a live parent and content artifact; archived records remain valid.
- Decision responses include read-only `payload.resolution: [{ref, kind, title_or_summary, exists}]`. This is a current read projection, never accepted as input or persisted as approval parameters. Sending it returns `400 invalid_request` naming `payload.resolution is read-only; send payload.resolution_refs`; request schemas and generated help expose only phase and resolution_refs. Missing/trashed evidence has an empty summary and `exists: false`; unavailable lookups conservatively project the same result. Titles/summaries are bounded to 240 characters plus an ellipsis.
- `POST /pm/actions/{action_id}/acknowledge` with `{}` lets only the decision's human actor handle a failed action, an unknown action whose current read-back is unavailable or inconclusive, or a `pending_delivery` action with `deliverable: false`. Closing an undelivered pending request retains the approval, sets `closed_without_delivery: true`, and records `Closed without delivery: no delivery path is configured for GitHub; nothing was sent.` in `receipt.detail` (substituting the actual source). Transient preflight errors cannot establish that nothing was sent. This action remains undeliverable even if routing becomes available; only a fresh proposal and approval can deliver it. A read-back that can advance returns `409 conflict` and requires reconciliation first; other ineligible states also return `409`. Acknowledgement sets visible status `acknowledged`, `acknowledged_by`, and `acknowledged_at`, is idempotent, and preserves the receipt and attempts. Later reconcile calls preserve this human handling. Clients group it under Handled; it does not assert source delivery.
- `GET /pm/bindings` honors `limit` (1–200, default 50) and a workspace/actor/resource-scoped keyset cursor. Continue with `next_cursor` while `has_more`; malformed or cross-scope cursors return `400 invalid_request`.
- PM turn context, propose (`decisions`), complete, and fail operations return HTTP `409` with code `turn_closed` for an expired turn, with `error.details.turn_id`, `deadline`, and durable `status`. Open turns past their deadline are persisted as failed before returning: "This turn passed its deadline and was failed; nothing can be proposed or read for it. Ask again to start a new turn." Already-terminal turns also return `turn_closed` with a terminal-state message; matching complete/fail replays with the finishing lease token retain their successful response, including after the deadline.
- All timestamps are ISO-8601 strings.
- Objects MUST preserve unknown fields (additive evolution), except explicitly closed request schemas such as session registration and task participation, which reject unknown fields to keep runtime transcripts and credentials outside this surface.
- `refs` values MUST be typed ref strings per `ref_format`.
- Error responses use a stable envelope:
  - `{ "error": { "code": "...", "message": "...", "recoverable": <bool>, "hint": "..." } }`
- Request-size, quota, and abuse-control failures use explicit stable codes:
  - `request_too_large` with HTTP `413` and a `request_body.limit_bytes` detail when the request body exceeds the configured limit.
  - `workspace_quota_exceeded` with HTTP `507` and a `quota` detail object containing `metric`, `limit`, `current`, and `projected` when a workspace write would exceed configured storage or count limits.
  - `rate_limited` with HTTP `429`, a `Retry-After` header, and a `rate_limit` detail object containing `bucket` and `retry_after_seconds`.
- Core conservatively normalizes documented user-visible markdown/prose fields before storage, including event summaries/message text, topic/board/card/document summaries, document markdown content, card revision summaries, and inbox response text. Mutation responses include `markdown_hygiene` only when normalization produced warnings; core does not recursively rewrite arbitrary JSON strings. Hard markdown hygiene failures use `invalid_markdown` only for unsupported control characters or extremely long single lines.
- Create-heavy write endpoints accept optional `request_key` for replay-safe retries.
  - Reusing the same `request_key` with the same request body replays the original successful response instead of creating duplicates.
  - Reusing the same `request_key` with a different request body returns `409 Conflict`.

### Agent auth conventions

- Access tokens are passed as `Authorization: Bearer <access_token>`.
- The first human registers through the bootstrap passkey ceremony. Hosts enroll after human approval or with a one-time human-created headless token.
- Further human registration requires a human invite. Agents derive from enrolled hosts.
- `GET /auth/bootstrap/status` exposes whether bootstrap registration is still available.
- Passkey auth is available via:
  - `POST /auth/passkey/register/options`
  - `POST /auth/passkey/register/verify`
  - `POST /auth/passkey/login/options`
  - `POST /auth/passkey/login/verify`
- `POST /auth/token` supports:
  - `grant_type=host_assertion` using an enrolled host key for a derived agent
  - `grant_type=assertion` for existing standalone agent principals awaiting adoption
  - `grant_type=refresh_token` using a refresh token
- Refresh tokens are rotated on successful refresh.
- Stable auth error codes include:
  - `username_taken`
  - `auth_required`
  - `invalid_token`
  - `agent_revoked`
  - `key_mismatch`

#### Workspace service JWT assertions

When a hosted **workspace service** (anx-core) calls the **control plane** (e.g. heartbeat telemetry, account status), it may authenticate with a short-lived **Ed25519 (EdDSA) JWT** in `Authorization: Bearer <jwt>`. The signing implementation lives in `core/internal/wsservicejwt` (single source of truth in this repo). **The control plane’s verifier for these assertions must track the same claim set and time windows** (`iss`/`sub`, `aud`, `iat`/`nbf`/`exp`, `workspace_id`, `purpose`); that code lives in the control plane repository, so any contract change (TTL, skew, new claims) requires coordinated updates in both places.

## API Surface Classification

Each endpoint is classified with an `x-anx-surface` extension indicating its role:

- **`canonical`**: CRUD/list/get endpoints over canonical resources (topics, cards, artifacts, documents, boards, board cards, events, packets), plus **read-only** thread list/inspect routes for backing-thread inspection. These are the durable substrate for automation.

- **`projection`**: Convenience surfaces that aggregate multiple canonical resources into workspace-friendly bundles. Examples: `topics.workspace` (agent-facing discussion/context primitive; the operator work projection is `work.list` / `work.get`), `threads.context`, `threads.workspace` (backing-thread diagnostic bundle), `boards.workspace`, `inbox.list/get/stream/ack`. **Do not build durable automation directly on projection payload shapes.** Use canonical APIs or CLI commands for durable substrate.

- **`utility`**: Infrastructure endpoints for liveness, readiness, version, meta discovery, auth bootstrap, maintenance, and workspace telemetry. Examples: `/health`, `/livez`, `/readyz`, `/ops/health`, `/ops/usage-summary`, `/v1/usage/summary`, `/ops/blob-usage/rebuild`, `/version`, `/meta/*`, `/auth/*`, `/actors`, `/derived/rebuild`.

Projection endpoints return a `section_kinds` field to distinguish canonical vs derived sections, and a `generated_at` timestamp indicating when the projection was generated.

## Authoritative HTTP catalog

**Do not treat this file as a per-path API list.** Machine-verifiable workspace HTTP is defined only in:

- [`contracts/anx-openapi.yaml`](../../contracts/anx-openapi.yaml) — paths, methods, request/response schemas, and `x-anx-surface` / `x-anx-command-id`.
- Generated references: [`contracts/gen/meta/commands.json`](../../contracts/gen/meta/commands.json) (structured metadata) and [`contracts/gen/docs/commands.md`](../../contracts/gen/docs/commands.md) (human-oriented command index).

Drift from the live router is gated in CI: `core` runs `TestExactRegisterRoutesCoveredByOpenAPOrExceptions`, which requires every `registerRoute(..., exactRouteAccess(...))` entry in `handler.go` to map to **OpenAPI-derived** commands or to an explicit row in [`contracts/non-openapi-endpoints.yaml`](../../contracts/non-openapi-endpoints.yaml).

### Narrative notes (not exhaustive)

- **Home unread feed**: `GET /home/unread` returns high-signal `home_feed`
  events grouped by topic for the authenticated principal. Read state is durable
  in core as per-topic cursors keyed by reader identity. `POST /home/read`
  advances one topic cursor (`topic_id`) or every currently visible topic cursor
  (`topic_ids`). Opening a topic may mark that topic read from the UI after a
  successful topic load; cursor writes do not emit synthetic events.
- **Events history**: `GET /events` is the complete event browser API. It
  supports the shared `preset=home_feed`, `event_group`, `backing_scope`,
  type/topic/thread/actor/search/time filters, and cursor pagination. Event
  types are strict contract values.
- **CLI version gate**: Clients may send `X-ANX-CLI-Version`. `X-OAR-CLI-Version` is accepted when the current header is absent, so clients from before the ANX rename still receive upgrade guidance. When below minimum compatibility, core responds with `426` and `cli_outdated` except on a small set of public/meta/auth bootstrap routes; see `x-anx-*` and handler logic — exact allowlist is in OpenAPI and code, not duplicated here. `min_cli_version` is the explicit wire-compatibility floor (`MinCompatibleCLI`, overridable with `ANX_MIN_CLI_VERSION`). `recommended_cli_version` tracks the current core release. Ordinary releases do not move the floor.
- **Visual report writes**: `POST /docs`, `PUT /docs/{document_id}`, and `POST /docs/{document_id}/revisions` run the same recognizer as `GET /docs/{document_id}/report` on the exact bytes that would be stored, including the 128 KiB limit. Newly introduced or changed explicit `review_by` values resolving in the past are refused at write time. Revisions may retain an expired deadline when the same panel ID, author, authored timestamp and resolved deadline are preserved, so unrelated edits remain possible. Durations are bounded to 3650 days before arithmetic. Content that recognizer treats as an invalid `anx.visual-report` is refused. The response is `400` `invalid_request`. The message names the failure, and `error.details.errors` lists the validator errors. Bodies the reader does not treat as a report are stored unchanged.
- **Document body updates**: Canonical write is `POST /docs/{document_id}/revisions` (`docs.revisions.create`). There is no `PATCH /docs/{document_id}` on workspace core.
- **Visual report reads**: `GET /docs/{document_id}/report` materializes bounded live panels and resolves authored review metadata for every panel in a saved report. `provenance_class` is `live` or `authored`; authored panels expose author, authored_at, review_by, review_by_defaulted and review_due. Legacy panels default to generated_at plus seven days. Reading the pinned report deduplicates one author-only inbox reminder per expired panel and revision, subject to current inherited privacy. Recipient identity is resolved independently of the reader's profile visibility, then dashboard access is checked for that recipient. These reminders are informational: clients may open the dashboard, but reply, acknowledge and dismiss mutations are unsupported. Preview never creates reminders. `POST /reports/preview` accepts an inline report definition and uses the same permission-filtered materializer without creating a document or revision.
- **List-only canonical enrichments**: `GET /topics` may include `timeline_message_count`; `GET /documents` may include `revision_count`, `timeline_message_count`, and `head_revision_character_count`. These are derived read hints for list rows, not writable fields. Message counts include non-trashed `message_posted` events on the backing `thread_id`. Character counts are best-effort UTF-8 rune counts of the decoded head revision body and may be omitted for large or unavailable blobs.
- **Packets**: Receipts and reviews are created via `POST /packets/receipts` and `POST /packets/reviews` only.
- **Cards**: Patch, move, and archive use first-class `PATCH /cards/{card_id}`, `POST /cards/{card_id}/move`, and `POST /cards/{card_id}/archive` (or trash/restore/purge as documented in OpenAPI). Board-scoped duplicate paths have been removed. **Batch card create** is `POST /boards/{board_id}/cards/batch` (`boards.cards.batch_add`): one `if_board_updated_at`, many `items`, single transaction. Assigning a taggable agent as the card assignee (via `assignee_refs`) enqueues an **agent wakeup**, visible to that agent as an **agent notification** (`GET /agent-notifications`).
- **Card timeline vs. Discussion (intentional split)**: `GET /cards/{card_id}/timeline` (`cards.timeline`) is the card's **lifecycle/audit log** — it returns only `card_*` events for the card and intentionally omits `message_posted`, even when a message carries a `card:<id>` ref. A card's **Discussion** (messages) lives on the card's backing thread and is served by `GET /threads/{thread_id}/timeline` using the card's `thread_id`. This mirrors the unified message-on-thread model used by boards, topics, and documents: every primitive's Discussion is `message_posted` on its backing thread; the per-primitive timeline endpoints expose lifecycle/audit, not the conversation.
- **Event stream**: `GET /stream/events` accepts `Last-Event-ID` (preferred over `last_event_id`) only for a currently authorized-visible, untrashed event. Absent, hidden, trashed and unknown IDs start at the current head. Accepted IDs resume in chronological timestamp/ID order. Each 200-candidate page examines two indexed ranges of at most 201 positions and merges at most 201 metadata positions. Empty pages are skipped silently in chunks of at most 2000 candidates. When a chunk has remaining positions, scanning continues immediately after yielding, without waiting for the polling timer. Each chunk decodes at most one visible page of 200 events; normal polling resumes only after reaching the head. Each chunk is bounded; total catch-up work scales with pending positions, while idle polls never rescan consumed history. Hidden and nonmatching positions advance only a connection-local cursor. Keepalive comments follow the polling timer independently of hidden progress. `event: resume` with `data: {}` follows every 200 delivered visible events and repeats that visible ID; it carries no hidden progress or backlog indication. Consumers exclude these markers from delivered-event counts. Authorization uses fresh epoch validation. Warm denial bindings contain only the selected page/reference keys; cold captures and ordinary appends that invalidate the epoch retain the canonical denial-graph cost.
- **SSE**: `GET /stream/events`, `GET /stream/inbox`, and `GET /stream/agent-notification-receipts` use `text/event-stream`; see OpenAPI `x-anx-input-mode` / streaming metadata.
  - Inbox emits at most 200 authorized, lifecycle-visible items per poll. An `inbox_page` event reports `partial: true` and a `resume_cursor` until the sweep completes with `partial: false`. Its event ID can be sent as `Last-Event-ID` after reconnect. Merge repeated items by ID and qualify counts during a partial sweep. Every poll refreshes authorization; lifecycle eligibility is indexed and maintained atomically with canonical writes.

## Derived projections (materialized views)

- Materialized derived projections used by the common read path:
  - `derived_inbox_items`: asynchronously maintained inbox items keyed by deterministic `inbox_item_id`, with per-thread rows used by `GET /inbox`, `GET /inbox/{id}`, `GET /stream/inbox`, and thread workspace inbox sections.
  - `agent_notification` is a derived per-target-agent view built from the `agent_wakeups` queue table and per-wakeup notification status.
  - `derived_topic_views`: asynchronously maintained per-thread stale/workspace summaries used by thread list stale indicators and thread workspace summary surfaces.
  - `topic_projection_refresh_status`: durable per-thread refresh state used to expose `current`, `pending`, `missing`, or `error` freshness metadata without mutating projections inside GET handlers.
- `POST /derived/rebuild` remains the deterministic repair path: it rebuilds projection tables from current topics/events/cards/documents without inventing lifecycle or staleness events.
  - Standard GET responses never repair or recompute projections inline; they return the best currently materialized data plus freshness metadata.

- Meaningful topic activity for stale-topic clearing:
  - The current activity set is explicit: topic/card/document/board lifecycle events, `message_posted`, `receipt_added`, `review_completed`, `human_attention_requested`, `human_attention_responded`, and `exception_raised`, plus non-create topic/card edits that materially change operator-authored state.
  - Coordination noise does not count as activity: agent notification read/dismissal, topic-creation bookkeeping, and derived projection maintenance.
- Topic, board, and card backing-thread linkage is exposed through `thread_id` on the canonical resource shape; keeping those backing links synchronized no longer emits an operator-visible timeline event or bumps the topic’s visible update clock.

- `work.annotate` proposal `instruction` must be a nonempty JSON object. Allowed keys are `project_ref`, `priority`, `next_actor`, `next_action`, `blockers`, `wake_condition`, `start_at`, `due_at`, `relations`, and `executions`. Priority accepts `p0`, `p1`, `p2`, `p3`; dates accept an RFC 3339 timestamp (priority and dates also accept an empty string or null to clear). Blockers must be an array of strings; relations must be an array of objects with `kind` (`parent`, `child`, `depends_on`, `related`, `artifact`) and a nonempty string `ref` such as `card:<handle-or-id>`; executions must be an array of objects with nonempty string `authority` and `run_id`. Both proposal endpoints validate keys and canonical local value shapes before insertion; invalid input returns `400 invalid_request` with the key allowlist for unknown keys and accepted values or shapes for value errors.
- `POST /pm/turns/{turn_id}/heartbeat` accepts `{"lease_token":"..."}` from the selected PM actor and returns `200 PMHeartbeatTurn` without the token. It renews `lease_expires_at` to `min(now + ANX_PM_LEASE_TTL, deadline)` under the claim write lock. Missing token returns `409 lease_required`; expired, released, or foreign token returns `409 lease_mismatch`; closed turn returns `409 turn_closed`. TTL defaults to `60s` (range `1s` to `10m`). Runners must renew at a cadence strictly less than TTL/2. Expiry makes the same open turn unclaimed/pending and reclaimable with a new token, preserving history; it does not fail the turn. Only reaching the turn deadline does that.

## Generic sessions and nonlocking task participation

An existing authenticated agent can register a native session through the generic
session contract, without agentctl, a special runtime adapter, or a new auth grant.
This does not enroll a new principal: host enrollment and existing token access
remain prerequisites. The stable ANX `agent_id` and `actor_id` come from the bearer
token. They are never supplied in session or participation request bodies.

Three identities remain distinct:

- The ANX agent principal is the stable authenticated identity.
- A native session is a provider/host-scoped context. Its identity is the tuple of
  authenticated agent, provider, host scope, and opaque native-session correlator.
  Enrolled agents use their authenticated host ID; an optional supplied host ID or
  slug must match. Existing standalone principals must supply a caller-reported
  namespace, which conveys no verified host authority.
- Existing `/runs` records identify execution attempts. Session registration
  itself never changes task state or controls execution. The existing optional
  run-attribution header retains its normal provisional-run behavior.

Session registration uses `sequence` to order observations, starting at any
nonnegative integer and increasing for each new report. The identity plus sequence
is the retry key. An identical normalized request at the current sequence returns
current read-time projection with the original `last_seen_at` and `expires_at`;
it never extends the lease. Lower sequences, same-sequence changed payloads,
identity-kind changes, or attempts to reopen a closed session return `409 conflict`.
After a restart, read the current session to recover its sequence. Callers should
persist sequence counters and serialize updates for a given session, rather than
inventing newer numbers to replay old observations.

`capabilities.resume`, `.history`, and `.logs` are all required and each explicitly
reports `supported`, `unsupported`, or `unknown`. They are unverified runtime
claims, never access grants. `native_session_id_kind=provider_session_sha256`
preserves a provider-domain SHA256 correlator (`sha256:` plus 64 lowercase hex
characters); `opaque` is the generic default. Prefer an opaque correlator over a raw
native ID. Do not register an invented native ID when the runtime cannot discover
one. Core never treats this identifier as a resume command, transcript address,
credential, or filesystem path. Additional request fields, including transcript
content, are rejected.

Every fresh session report has a server-clock 120-second activity lease. Refresh
with a new sequence before expiry (for example, every 30–60 seconds while actually
active). Reads derive `stale` after expiry and set `active=false`; they do not mutate
state or invent terminal outcomes. Explicit `closed` is terminal. Session detail
reads are owner-only, and another principal receives `404` even when it knows the ID.
There is intentionally no workspace-wide session listing.

Task participation is a separate idempotent record for each `(card, session)`.
Its own sequence and 120-second lease are independent of other tasks and sessions.
Use `active`, `idle`, or `left` to report participation. A new task report renews
only that task lease; a session heartbeat renews no task leases. Effective task
activity requires both fresh leases and an active session. An idle session makes
active participation idle; a closed session makes remaining participation closed;
expired evidence is stale. Leaving can be reversed with a later report while the
session remains open. A session can participate in many tasks and many sessions
can participate in a task without a claim, lock, assignment, or wakeup.

Participant list reads check the task, board, and project backing scopes and return
only task-scoped participation metadata. A reader sees `session_id` only for its
own sessions. Other sessions' provider/native IDs, host scope, capabilities, and
other task links are not included. Pagination uses a task-bound opaque cursor,
ordered by `participant_id` ascending, with a default limit of 50 and maximum 200.

Participation never writes card phase, assignees, work version, source ownership,
or source status; native and source-backed work follow the same rule. It also emits
no synthetic timeline events or misleading task-activity timestamps. The first
foundation does not change the legacy command-center `/agents` roster or `/runs`
projection; clients should inspect sessions/participants for this activity signal.
All routes remain subject to existing rate limits, request limits, revocation,
and workspace read-only enforcement. Bearer authentication remains required in
development mode. See the canonical OpenAPI for request/response envelopes.

### Explicit agent auth-admin grants

- `GET /auth/admins`: humans or auth-admin agents list active explicit agent grants (`{ admins: [{ principal_id, actor_id, username, auth_admin }], next_cursor, has_more }`).
  `limit` defaults to 50 (1..200); `cursor` continues in username/ID order. Host
  fields are enriched in one batch for that page.
- Host-backed grants also include `host_id`, `host_slug`, and `agent_name`. Granting one trusts every process that can read that shared host key and derive the named agent's bearer. Grant/revoke audit metadata records that scope.
- `POST /auth/principals/{principal_id}/revoke` (including the human lockout override) and `POST /auth/invites/{invite_id}/revoke` require active human authorization in their mutation transactions. Agent grants provide fleet writes and inventory/audit reads only.
- `POST /auth/invites` is human-only. Human credential creation validates a human invite issuer inside its transaction, so historical agent-issued invites cannot mint human identities. Agent bearers cannot authorize human credential ceremonies.
- `POST /auth/admins/{principal_id}/grant` and `/revoke`: human-only; target is an active agent ID or exact username. Returns `{ admin }`. Changes are audited and idempotent. Humans cannot be targets.
- `GET /auth/principals` summaries include `auth_admin` (the explicit metadata flag).
- Host administration accepts humans or explicitly granted auth-admin agents for pending list/approve/deny, token create/list/revoke, and `DELETE /hosts/{host_id}`. Self-host revocation by an agent returns `403 host_self_revoke`, with canonical ID and slug both protected. `PATCH /hosts/{host_id}` retains human bearer or signed host proof authorization.
- Token creation accepts either `expires_at` or `expires_in_seconds` (600–86400), with `label`. Core measures relative expiry itself. Only the create response includes `token`; lists and audit records never do.

- Removing an auth-admin grant does not invalidate already-issued enrollment tokens or approved ceremonies. Revoke unused tokens at `POST /auth/hosts/enrollment-tokens/{token_id}/revoke`. Deny pending or approved ceremonies at `POST /auth/hosts/enrollments/{enrollment_id}/deny` before completion; the protected pending list includes both statuses. After completion, revoke the host instead.
- `host_enrollment_token_consumed` has no authenticated principal actor: metadata identifies `token_id`, destination `host_id` and `host_key_id`, with `issuer_principal_id` and `issuer_actor_id` recorded separately. No token secret or public key bytes enter audit metadata.

The canonical shapes and error codes are in `contracts/anx-openapi.yaml`. See the CLI runbook for the default fleet setup flow.

## Initiative plans and batch ref previews

`GET /cards/{card_id}/plan` returns `{card_ref, plan, plan_state, plan_health, next_step, status_mismatch, if_updated_at}`; plan and state are null before the first plan is set. `PUT` accepts `{plan:{steps:[...]}, if_updated_at}`. The token is the card `updated_at` from a read. Every real edit atomically persists a `card_updated` event with `before_plan`, `plan` and `changed_fields:["plan"]`; identical writes preserve activity recency. History is the existing `/cards/{card_id}/timeline`. Invalid graphs fail with 400; stale tokens fail with 409. Empty steps clear the graph. Plan edits are local annotations even on source-backed cards and never write upstream.

Steps require a unique slug `id` (64 bytes) and `title` (500 bytes), with optional `ref`, `after` (existing unique step ids, max 50), `due` (YYYY-MM-DD or RFC3339), and `status` (done, active, blocked, not_started). Plans cap at 200 steps and reject cycles. Refs accept card/doc/document/topic refs, opaque adapter-published identifier aliases, or absolute HTTP(S) URLs (2048 bytes). External state comes from ingested source observations and generic structured source_refs; there is no network fetch. Unknown, ambiguous or inaccessible evidence keys remain unresolved. Referencing a private published key makes the stored plan inherit its card and board ownership before projection, pagination and counts.

Card and work reads expose `plan` and `plan_state`. State has effective `steps`, `progress:{done,total}`, `critical_path` (unfinished ids), `next_steps` (dependency-ready, unblocked ids), `shape` and `health`. A connected path is chain; disconnected paths are lanes; any branching/merging graph is dag. Longest paths count unfinished steps and break ties by lexicographic ids. Detailed plan_health uses the shared six-state rules below; legacy health retains on_track/stalled/blocked. Any unfinished blocked step makes health blocked; detailed completed plans are done, and empty plans are no_plan or stale. Set `ANX_PLAN_STALLED_AFTER` to a positive Go duration to configure the threshold. Plan edits and referenced native activity or external meaningful progress/source activity count as movement; unchanged observation timestamps do not. Doc/topic/board preview status is lifecycle state. Doc/topic plan steps have no workflow phase, so use their fallback status without inventing completion from existence.

The existing live-initiatives report projection retains `progress` and `needs[]`. Plans replace markdown-derived progress with computed counts and needs with blocked step titles; `plan_state` and `health` are additional fields. Cards without plans retain the previous summary projection.

Report hydration joins cards, metadata, latest good/attempt observations, board labels and thread privacy in one query for the bounded candidate set (up to 200 rows per native report scope). Plan enrichment uses one plan/activity query plus at most one fact query per referenced resource kind and one external-evidence scan, independent of card/step count. Batch ref resolution uses at most five initial kind queries, one plan/activity query and five linked-fact queries. Work and plan projection reads never call `GetWork` per row or ref. Native report event and decision reads each stop after one 200-candidate page, including uncached `POST /reports/preview`, and preserve `truncated` when more remain. `GET /work` applies its source, owner, phase, freshness, query and project filters in SQL before a bounded page is hydrated. Cursor ordering uses UTC timestamps and card IDs, including nanosecond precision.

`POST /refs/resolve` accepts `{refs:[...]}` (max 200). Results are `{items:[{ref,resolvable,kind?,title?,status?,phase?,owner?,owner_display?,board?,priority?,last_moved_at?,next_step?,progress?,url?}]}` in input order, retaining duplicates. Native card/document URLs are workspace-relative UI paths; topics and boards omit url because they have no current UI detail surface. Unknown, trashed or inaccessible native refs return only `{ref,resolvable:false}`. Native handles and internal ids resolve for cards, docs/documents, topics and boards. Plan-derived progress and status honor the requesting principal's access to every referenced resource. Responses are read-only and uncached. `board` contains `{ref,title}` and is independently visibility checked. `owner_display` is the workspace actor display name, falling back to the owner ref. `next_step` contains id, title and ref for a readable ready step, preferring the critical path when a plan exists. `last_moved_at` uses the same native update/source meaningful movement timestamp as plan facts, never a polling observation timestamp. Hosted clients prepend `/o/<org>/w/<ws>` to native relative URLs; absolute source URLs are used unchanged.

## Executive Overview and workspace dashboard

`GET /overview` is the shared projection behind the web UI and `anx overview
--json`. It returns Needs you (open asks, actionable PM decisions, blocked work
and human next actors), the selected dashboard, open initiatives with Markdown
checklist progress, active work records/counts, and agent presence. Archived
cards, boards and topics do not contribute. Initiatives share the live report
projection: `progress.done/total`, `needs[]`, phase, board and update time.
The browser opens each card using its typed ref. Each initiative also contains
`plan_state` (the same effective computation as card plans), `health` with
`{status,reason}`, and `geometry`. A plan overrides summary checklist progress.
Planless initiatives have null plan_state/geometry and phase-based health.
Geometry supplies shape, effective node status, dependency layer and included
`after` edges, capped at 24 nodes; `total_nodes` and `collapsed_nodes` describe
the remainder. Clients render geometry without re-deriving workflow semantics.
`plan_step_digest` adds what just finished, what is moving and what is next as
three lists of at most three steps with the remainder counted in `more`, over
the same steps and referenced facts the read already loaded. `completed` lists
steps whose linked resource moved within `window_hours` (168), newest first; a
step marked done inline has no completion time and is omitted rather than dated
from the plan's last edit. `current` lists active and blocked steps and `next`
lists ready unstarted steps, both in plan order, so one step is never in both.
It is null without a plan and appears on card and work reads as well.
The projection reads at most 100 open and 100 closed active-lifecycle candidate
cards for completion digests, reuses the report batch privacy context, and
declares `truncated` on work and initiatives when more candidates exist.
Counts refer to the visible bounded set. Needs you also declares `truncated`
when work, inbox or its 100 actionable PM decision candidates exceed their
windows. Agent presence samples at most 100 visible identities. Use the work,
inbox and PM collection endpoints to continue through their pages.

`GET /overview/changes` returns `{since,generated_at,items,truncated}` for the
last authenticated principal visit to Overview in this workspace database.
`GET /overview` includes that digest as `since_you_last_looked`, then records
the new visit and a bounded status snapshot atomically. It remains a business
read: its only write is private viewer presentation state. All responses use
`Cache-Control: no-store`. Reading changes or pinning a dashboard does not
advance the baseline. A first visit (or unauthenticated dev read) has null since
and empty items; anonymous reads never share stored visit state.

Digest items have kind, ref, title, optional step_id and optional ts. Kinds are
`step_completed`, `initiative_stalled` (with additive `kind_v2: initiative_stale`), `initiative_blocked`, `ask_answered`,
and `decision_created`. Steps and health are net changes against previously
visible statuses, including time-only stalling and steps of now-closed cards.
Newly visible cards do not invent transitions. Answers are canonical events
and new decisions are ordinary permission-filtered PM records in
`(since,generated_at]`; private answer/decision text is never copied. Reads
recheck current resource visibility. The digest caps output at 100 and each
answer/decision candidate read at 200, with `truncated` for any reached limit.
Answer subjects share a 4,000-distinct-ref budget, resolved in batches of at most
200; an answer requiring omitted subjects is excluded and sets `truncated`.
Card, board, event and subject access is applied before candidate budgets.
The shared wire fixtures live in `contracts/fixtures/initiative-overview/`.

`PUT /workspace/dashboard` accepts `{ "document_ref": "document:<handle>" }`
and pins an active visual-report document accepted by the full shared validator. `{ "document_ref": null }` clears the
pin. The selection survives restarts. An archived, deleted or unreadable pin
falls back to the newest valid active report; the pin remains available for clearing.

`GET /work` also returns `archived_refs` for card aliases excluded by card or
parent-board or project-topic lifecycle. Inbox clients use these to exclude related PM activity
from Watching. Archived resources remain accessible through explicit lifecycle
list reads and the web UI Archive view. Watching groups all card edits by their
board and orders asks/answers and done/blocked transitions before routine edits.

Overview bulk-loads active work and observation metadata once. Its dashboard
contains only the selected accessible validated report and `has_more` when unread accessible candidates
remain. `GET /workspace/dashboard/reports` (`anx workspace dashboard list`) loads
at most 100 recent document candidates plus the pinned document on demand;
`next_cursor` continues through older candidates. Overview validates only the
selected report within that window and declares `has_more` when more candidates
remain. Pin acceptance, selection and CLI publishing share
`contracts/visualreport`; renderer conformance covers static and live panels.
Both dashboard responses include each report's selected head `revision_ref`, so
live data from a later revision cannot render under an earlier definition.
The Overview resolves a `?dashboard=<id-or-handle>` bookmark on load, without
requiring focus on the report selector.
Initiatives expose `needs[]` and use the live report summary parser, ignoring
fenced examples and counting empty Markdown checkbox lines.

## Declared live series

See `../../docs/live-series.md` for adapter declaration, scoped token exchange, bounded queries, caps, retention, panel sources and fallback semantics. The canonical paths and JSON schemas are in `contracts/anx-openapi.yaml`.

### Conditional card archive

`POST /cards/{card_id}/archive` accepts optional `if_latest_observation_id`, a
nonempty string obtained from `work get` → `latest_observation.id`. Core checks
it against the card's latest successful observation in the archive transaction
and on the archive SQL mutation. Missing/different observations return `409
conflict`; an empty supplied string returns `400 invalid_request`. Omission
preserves the existing archive behavior. Pair it with `if_board_updated_at` and
`if_version` to also fence canonical card/board edits and work annotations.
Successful source polls can change the observation ID without changing phase,
board timestamp, or work version, so neither existing token replaces this fence.

### External refs and initiative health

`POST /refs/resolve` preserves its 200-ref cap, order and duplicates. External
keys match exact adapter-published native IDs, authority-prefixed native IDs,
URLs, identifiers or aliases in workspace observations and structured
`source_refs`. External results have `kind: external`, `authority`, `native_id`,
`url`, `status`, and `source: evidence`. Status and URL are null when absent
from the published evidence. Adapters publish any provider spelling variants;
core never parses provider identities, generates provider URLs or fetches
external systems. Unknown, ambiguous or inaccessible keys remain unresolved.
Resolution filters candidates in the caller's scope rather than denying the
batch because an input key has an inaccessible publisher. A shared public/private
key returns the visible evidence, retaining unknown entries and duplicates.
Writing references still requires their inherited ownership checks.
Distinct source connections are never silently combined. Adapters retain the
observation anchor on unchanged source data; evidence without an observation
time uses its containing card timestamp.

Card reads/lists, work reads/lists, ref previews, initiative overview and live
reports add `plan_health: {state, reason, since}`, `next_step: {id, title, ref}`
(or null), and `status_mismatch` alongside existing progress and `plan_state`.
Health precedence: no steps → `no_plan` if recent, `stale` after the inactivity threshold; all complete → `done`; any unfinished
blocked step/dependency → `blocked`; card or unfinished step due within 24 hours
or overdue → `at_risk`; no card/plan/step activity for 72 hours → `stale`;
otherwise `on_track`. Workspace service env `ANX_PLAN_STALLED_AFTER` overrides
the stale threshold. Card and linked card messages count as activity; repeated
source polls do not. `since` is the reproducible condition anchor documented in
OpenAPI, not a persisted historical transition timestamp. Ready steps prefer the
critical path, then lexicographic id, skipping unreadable linked resources.
`status_mismatch` is true for backlog cards with completed steps; computation
never changes phase. `plan_state.health` and Overview `health.status` retain legacy values: stale maps to stalled, blocked to blocked, other detailed states to on_track. New clients read `plan_health.state`.

### Workspace-local asks summary

`GET /inbox/summary?limit=5` returns `{open_ask_count, asks, generated_at}` from
materialized open human attention asks visible to the current caller. Limit is
0–50 (default 5); `limit=0` returns the count and an empty list. SQLite counts before limiting and decodes only the requested page. Authorization
covers the ask thread, subject, related refs, source event, and the subject's
containing card and board, including legacy backing threads. Answered/withdrawn and review/escalation rows are excluded. Canonical open asks survive linked context archive; access to that context still applies. Asks are ordered by priority or projected severity (case-insensitive), oldest trigger, then id.
This route avoids the inbox's workspace-wide thread freshness scan and per-item
notification enrichment. Shared human asks are visible to each authorized human
reader; this is separate from requester-scoped `GET /agent-inbox/asks`.

Hosted clients fan out with existing per-workspace sessions; core adds no global
identity. The hosted proxy must retain collection-read classification for
`/inbox/summary`, which is already present on main.
The CLI exposes the same read as `anx inbox summary [--limit 0..50]`.

Boards expose an open `role` string on create, patch, get and list. `anx boards create --title "Initiatives" --role initiatives` or `anx boards patch board:initiatives --role initiatives` marks an initiative board. Patch omission preserves the role; `--role ""` clears it. When any board is marked, Overview initiatives include only those board cards; other cards remain in active work. With no designated board, the previous selection remains and `initiatives.hint` suggests setting a role. Planless health is `no_plan` until the configured threshold, then `stale`; card edits and discussion activity reset inactivity. Detailed `plan_health.state` uses six states; legacy fields keep their original vocabulary for older clients. Board role remains writable by ordinary authorized writers, including agents.

`source_refs` is a generic list of structured source evidence on card-backed work (up to 2000 entries). Any adapter can set it through `work create` or `work patch` with `if_version`; card and work reads expose it. Every entry requires authority, connection_id and native_id (unique tuple); optional fields are identifier, title, HTTP(S) url, open status/phase, observed_at and source_activity_at. Unknown fields round-trip. Omission preserves; [] clears. Core never interprets adapter markdown. An external adapter can convert its own existing evidence into this primitive during ingestion while preserving unrelated structured source refs. Until an adapter publishes a lookup key, that external key remains unresolved.

Evidence resolution uses a transactionally maintained lookup index with 200-candidate pages. Aggregated plan refs are resolved in batches of at most 200. Card and board access are checked before enrichment and serialization, including work observations and plan reads. Effective health inputs are shared: explicit work due annotations (including clearing) override card due dates; meaningful source activity/progress, plan edits and discussion timestamps are normalized once.

Evidence lookups apply principal card/board access before selecting at most 33 candidates per exact key; overloaded visible aliases remain unknown. Source URL lookup selects at most two accessible native cards per URL, sufficient to establish ambiguity. Aggregated plan reads share a round-robin budget of 4,000 distinct refs, resolved in batches of 200. `plan_resolution_truncated` reports omitted refs. Both `identifier_aliases` and compatible `aliases` publish exact generic aliases (50 unique strings, 2048 bytes each). Detailed health remains available in `plan_health.state`, `plan_state.health_state`, and Overview `health.state`; legacy badges remain `on_track`, `blocked`, or `stalled`.

Indexed alias caps apply equally to `source_refs`, `source`, observation `facts`,
and observation `evidence`: each alias array allows 50 unique nonblank strings;
all indexed identity, identifier, URL and alias strings cap at 2048 UTF-8 bytes.
Observation evidence arrays allow at most 2000 entries. Migration 62 stores each
evidence payload once with bounded alias-key references. It backfills canonical
evidence in 32-card batches, indexing at most the first 2000 evidence entries and
first 50 strings per alias array without changing canonical legacy evidence.
Migration 61 adds board roles after main's migration 60. Upgrade support follows
released and main history; migration numbers from unmerged PRs are not reserved.
Evidence records, alias keys, `source_refs` and plan refs participate in the
central inherited-ownership inventory and use scoped database handles. Internal
evidence row numbers are not public resource identities. Inbox summary counts
and pagination use the scoped inbox relation in SQL.

Event list/detail, event SSE, and thread/topic timelines apply containing card
and board access to events and referenced subjects before pagination or
serialization, including subject and related references in canonical wrapped
and legacy flat payloads. Thread workspace recent events use the same predicate.
Timeline notification receipts and receipt streams inherit the
same access through their backing thread and trigger event. Receipt streams
page an indexed created-at snapshot, then an append-only update log, at most
200 candidates per page and 2000 per chunk. The connection waits on the poll
timer only at the head and does not hold a database connection between pages.
Hidden and trashed receipts advance that cursor without a client-visible control.
A visible receipt whose payload changed while disconnected is delivered from that receipt forward.
This also applies to receipts predating the update log: migration 69 installs
schema and records the initial log head without backfilling history. Historical
resumes seek the existing canonical receipt index and use the same scoped payload
read as new receipts; startup work does not grow with receipt history.
Inherited visibility changes replay the snapshot without a wakeup rewrite.
Replay retains its position across further visibility changes and checks new
updates before each replay page. Payload reads validate authorization in the
same database statement, including concurrent revocation and trigger trash.
Durable inbox projections remain canonical; inbox lists, summary, Overview and the shared inbox
stream loader apply the requesting principal's visibility when reading them.
Board list cursors count accessible matches only.

Inbox detail, board/thread/topic workspace sections use the same authorized inbox
predicate as inbox summary before serializing items or counting asks. Principal
workspace summaries are copied from canonical projections and recount authorized
inbox rows; stored projections remain complete. Inbox list freshness loads only
active accessible backing threads, including containing card and board access.
Live report event selection authorizes payload subjects and related refs before
its source-row cap; private candidates cannot consume that budget or set
`truncated`. Generic backing-thread authorization and mutation authorization
are tracked separately from these local projection read filters.

## Structured access requests

An authenticated agent requests its own named grant with `POST /auth/access-requests`
(`grant: "auth-admin"`, nonempty `reason`, at most 4000 characters). The response
contains `request`, including durable identity, status and Inbox correlation.
Retries for that principal/grant return the original request, reason and decision.

Humans use `GET /auth/access-requests` for pending requests and
`POST /auth/access-requests/{request_id}/approve` or `/deny` to decide them. Approval
uses the same human-only grant transaction as `/auth/admins/{principal_id}/grant`,
with one canonical Inbox response and decision. Matching retries return the stored
decision and never reapply a subsequently revoked grant; opposite decisions conflict.
A revoked requester cannot be approved, but may be denied.

`GET /auth/access/summary` is human-only and returns `pending_count`,
`pending_access_request_count` and `pending_host_enrollment_count`. Enrollments count
while unexpired and pending or approved, until completion.

Each access request has a dedicated shared thread and a review Inbox item. Its
`access_request_id`, `requested_grant`, and `requester_principal_id` are server-owned
correlation from the persisted request, never trusted event-supplied grant inputs.
Existing `POST /inbox/{inbox_id}/respond` accepts `approved` (grant) or `rejected`
(deny) for these items. Other outcomes return 400 without a response or mutation.
Agents cannot withdraw an access-backed review; only humans decide it.

Open human attention requests survive linked subject archive; request thread,
subject-card thread and containing-board thread privacy still apply to list, item,
stream snapshots and updates, and summary reads. Thread subjects inherit their
containing card and board's privacy, including archived context. Responding
requires the same resource access before resolving the request or returning an
idempotent replay; losing access makes the item look absent even after response.
`GET /inbox/summary` uses the same visibility rules as `/inbox`, counts open asks,
and returns up to `limit` asks (default 5, range 0–50), with priority and oldest-first
ordering. Access reviews are counted by the Access summary.

### Open inbox pages

`GET /inbox?status=open` accepts `limit` (default 50, max 100) and an opaque
`cursor`. Rows order escalation, ask, review, then other categories, with newest
trigger first inside each category. `has_more` and `next_cursor` describe further
candidate rows; lifecycle filtering can leave a page empty while a cursor still
continues. Cursors belong to the authenticated principal and inbox status.
Workspace projection freshness retains the complete status and `thread_count`,
with at most 100 thread details and `truncated` for additional threads.
