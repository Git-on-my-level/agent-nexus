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
- Objects MUST preserve unknown fields (additive evolution).
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
- First-principal registration is bootstrap-token gated via `POST /auth/agents/register` or the passkey registration endpoints.
- Once the first principal exists, further registration requires a valid invite token.
- `GET /auth/bootstrap/status` exposes whether bootstrap registration is still available.
- Passkey auth is available via:
  - `POST /auth/passkey/register/options`
  - `POST /auth/passkey/register/verify`
  - `POST /auth/passkey/login/options`
  - `POST /auth/passkey/login/verify`
- `POST /auth/token` supports:
  - `grant_type=assertion` using an Ed25519 key assertion
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

- **`projection`**: Operator convenience surfaces that aggregate multiple canonical resources into workspace-friendly bundles. Examples: `topics.workspace` (primary operator coordination read), `threads.context`, `threads.workspace` (backing-thread diagnostic bundle), `boards.workspace`, `inbox.list/get/stream/ack`. **Do not build durable automation directly on projection payload shapes.** Use canonical APIs or CLI commands for durable substrate.

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
- **CLI version gate**: Clients may send `X-ANX-CLI-Version`. When below minimum compatibility, core responds with `426` and `cli_outdated` except on a small set of public/meta/auth bootstrap routes; see `x-anx-*` and handler logic — exact allowlist is in OpenAPI and code, not duplicated here.
- **Document body updates**: Canonical write is `POST /docs/{document_id}/revisions` (`docs.revisions.create`). There is no `PATCH /docs/{document_id}` on workspace core.
- **List-only canonical enrichments**: `GET /topics` may include `timeline_message_count`; `GET /documents` may include `revision_count`, `timeline_message_count`, and `head_revision_character_count`. These are derived read hints for list rows, not writable fields. Message counts include non-trashed `message_posted` events on the backing `thread_id`. Character counts are best-effort UTF-8 rune counts of the decoded head revision body and may be omitted for large or unavailable blobs.
- **Packets**: Receipts and reviews are created via `POST /packets/receipts` and `POST /packets/reviews` only.
- **Cards**: Patch, move, and archive use first-class `PATCH /cards/{card_id}`, `POST /cards/{card_id}/move`, and `POST /cards/{card_id}/archive` (or trash/restore/purge as documented in OpenAPI). Board-scoped duplicate paths have been removed. **Batch card create** is `POST /boards/{board_id}/cards/batch` (`boards.cards.batch_add`): one `if_board_updated_at`, many `items`, single transaction. Assigning a registered **agent** as the card assignee (via `assignee_refs`) enqueues an **agent wakeup**, visible to that agent as an **agent notification** (`GET /agent-notifications`).
- **Card timeline vs. Discussion (intentional split)**: `GET /cards/{card_id}/timeline` (`cards.timeline`) is the card's **lifecycle/audit log** — it returns only `card_*` events for the card and intentionally omits `message_posted`, even when a message carries a `card:<id>` ref. A card's **Discussion** (messages) lives on the card's backing thread and is served by `GET /threads/{thread_id}/timeline` using the card's `thread_id`. This mirrors the unified message-on-thread model used by boards, topics, and documents: every primitive's Discussion is `message_posted` on its backing thread; the per-primitive timeline endpoints expose lifecycle/audit, not the conversation.
- **SSE**: `GET /stream/events`, `GET /stream/inbox`, and `GET /stream/agent-notification-receipts` use `text/event-stream`; see OpenAPI `x-anx-input-mode` / streaming metadata.

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
