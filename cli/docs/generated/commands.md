# OAR Command Registry

Generated from `contracts/oar-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.3.0`
- Commands: `117`

## `actors.create`

- CLI path: `actors create`
- HTTP: `POST /actors`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Legacy dev actor registration when dev_actor_mode is enabled.
- Concepts: `actors`, `auth`
- Error codes: `auth_required`, `invalid_request`, `dev_actor_mode_disabled`
- Output: Returns `{ actor }`.

## `actors.list`

- CLI path: `actors list`
- HTTP: `GET /actors`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Enumerate durable actor records for operator UI and dev fixtures.
- Concepts: `actors`, `auth`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ actors, next_cursor? }`.

## `agent.notifications.dismiss`

- CLI path: `agent notifications dismiss`
- HTTP: `POST /agent-notifications/dismiss`
- Stability: `beta`
- Surface: `projection`
- Input mode: `json-body`
- Why: Dismiss a wake notification for the authenticated agent.
- Concepts: `agents`, `notifications`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Output: Returns `{ event, notification }`.

## `agent.notifications.list`

- CLI path: `agent notifications list`
- HTTP: `GET /agent-notifications`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Derived read of notifications for the authenticated agent.
- Concepts: `agents`, `notifications`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ items, generated_at }`.

## `agent.notifications.read`

- CLI path: `agent notifications read`
- HTTP: `POST /agent-notifications/read`
- Stability: `beta`
- Surface: `projection`
- Input mode: `json-body`
- Why: Record read state for a wake notification.
- Concepts: `agents`, `notifications`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Output: Returns `{ event, notification }`.

## `agents.me.get`

- CLI path: `agents me`
- HTTP: `GET /agents/me`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Resolve bearer principal to agent profile and keys.
- Concepts: `auth`, `agents`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ agent, keys }`.

## `agents.me.keys.rotate`

- CLI path: `agents me keys rotate`
- HTTP: `POST /agents/me/keys/rotate`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Add a new Ed25519 key for assertions.
- Concepts: `auth`, `agents`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ key }`.

## `agents.me.patch`

- CLI path: `agents me patch`
- HTTP: `PATCH /agents/me`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Rename or adjust profile fields for the authenticated agent.
- Concepts: `auth`, `agents`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ agent }`.

## `agents.me.revoke`

- CLI path: `agents me revoke`
- HTTP: `POST /agents/me/revoke`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Self-revocation or admin-style revocation flow for the active principal.
- Concepts: `auth`, `agents`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `conflict`
- Output: Returns `{ ok: true }`.

## `artifacts.archive`

- CLI path: `artifacts archive`
- HTTP: `POST /artifacts/{artifact_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Set archived_at on artifact metadata (orthogonal to trash lifecycle).
- Concepts: `artifacts`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ artifact }`.

## `artifacts.content`

- CLI path: `artifacts content`
- HTTP: `GET /artifacts/{artifact_id}/content`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Return raw artifact payload with varying content types.
- Concepts: `artifacts`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Raw bytes or JSON/text depending on artifact.

## `artifacts.create`

- CLI path: `artifacts create`
- HTTP: `POST /artifacts`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Store content-addressed artifact metadata and payload (bytes, text, or structured packet JSON).
- Concepts: `artifacts`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `conflict`
- Output: Returns `{ artifact }`.

## `artifacts.get`

- CLI path: `artifacts get`
- HTTP: `GET /artifacts/{artifact_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve immutable artifact metadata referenced from timelines and packets.
- Concepts: `artifacts`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ artifact }`.

## `artifacts.list`

- CLI path: `artifacts list`
- HTTP: `GET /artifacts`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Search and filter immutable artifacts across the workspace.
- Concepts: `artifacts`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ artifacts }`.

## `artifacts.purge`

- CLI path: `artifacts purge`
- HTTP: `POST /artifacts/{artifact_id}/purge`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Permanently delete a trashed artifact (human-gated).
- Concepts: `artifacts`, `write`
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ purged, artifact_id }`.

## `artifacts.restore`

- CLI path: `artifacts restore`
- HTTP: `POST /artifacts/{artifact_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear trash lifecycle fields on an artifact after an explicit restore action.
- Concepts: `artifacts`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ artifact }`.

## `artifacts.trash`

- CLI path: `artifacts trash`
- HTTP: `POST /artifacts/{artifact_id}/trash`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Move artifact metadata to trash with an explicit operator reason.
- Concepts: `artifacts`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Output: Returns `{ artifact }`.

## `artifacts.unarchive`

- CLI path: `artifacts unarchive`
- HTTP: `POST /artifacts/{artifact_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear archived_at on artifact metadata.
- Concepts: `artifacts`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ artifact }`.

## `auth.agents.register`

- CLI path: `auth agents register`
- HTTP: `POST /auth/agents/register`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Bootstrap or invite-gated agent registration with key material.
- Concepts: `auth`, `agents`
- Error codes: `invalid_request`, `invalid_token`, `auth_required`
- Output: Returns `{ agent, tokens, ... }` per core auth handlers.

## `auth.audit.list`

- CLI path: `auth audit list`
- HTTP: `GET /auth/audit`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Operator audit trail for auth-sensitive actions.
- Concepts: `auth`, `audit`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns audit list JSON.

## `auth.bootstrap.status`

- CLI path: `auth bootstrap status`
- HTTP: `GET /auth/bootstrap/status`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Report whether first-principal bootstrap registration is still available.
- Concepts: `auth`
- Output: Returns `{ bootstrap_registration_available, dev_passkey_bypass_available? }`, where the dev bypass field reflects the effective local-only passkey bypass capability.

## `auth.invites.create`

- CLI path: `auth invites create`
- HTTP: `POST /auth/invites`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Issue a one-time invite for human or agent principals.
- Concepts: `auth`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ invite, token }`.

## `auth.invites.list`

- CLI path: `auth invites list`
- HTTP: `GET /auth/invites`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Operator listing of outstanding invites.
- Concepts: `auth`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ invites }`.

## `auth.invites.revoke`

- CLI path: `auth invites revoke`
- HTTP: `POST /auth/invites/{invite_id}/revoke`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Invalidate an outstanding invite by id.
- Concepts: `auth`
- Error codes: `auth_required`, `invalid_request`, `not_found`, `invalid_token`
- Output: Returns `{ invite }`.

## `auth.passkey.dev.login`

- CLI path: `auth passkey dev login`
- HTTP: `POST /auth/passkey/dev/login`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Local development bypass when OAR_ALLOW_PASSKEY_DEV_BYPASS=1 and the workspace carries the local-only .oar-dev-insecure-auth marker; optional username or display_name, or sole passkey principal.
- Concepts: `auth`, `passkeys`
- Error codes: `invalid_request`, `not_found`, `dev_passkey_bypass_disabled`
- Output: Returns `{ agent, tokens }`.

## `auth.passkey.dev.register`

- CLI path: `auth passkey dev register`
- HTTP: `POST /auth/passkey/dev/register`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Local development bypass for human onboarding when OAR_ALLOW_PASSKEY_DEV_BYPASS=1 and the workspace carries the local-only .oar-dev-insecure-auth marker; stores a synthetic credential. Optional existing_actor_id links a pre-seeded actor when OAR_DEV_REGISTER_LINKED_ACTORS=1.
- Concepts: `auth`, `passkeys`
- Error codes: `invalid_request`, `invalid_token`, `dev_passkey_bypass_disabled`
- Output: Returns `{ agent, tokens }`.

## `auth.passkey.login.options`

- CLI path: `auth passkey login options`
- HTTP: `POST /auth/passkey/login/options`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: WebAuthn assertion challenge for returning principals.
- Concepts: `auth`, `passkeys`
- Error codes: `invalid_request`
- Output: Returns `{ session_id, options }`.

## `auth.passkey.login.verify`

- CLI path: `auth passkey login verify`
- HTTP: `POST /auth/passkey/login/verify`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Verify WebAuthn assertion and issue tokens.
- Concepts: `auth`, `passkeys`
- Error codes: `invalid_request`, `invalid_token`
- Output: Returns `{ agent, tokens }`.

## `auth.passkey.register.options`

- CLI path: `auth passkey register options`
- HTTP: `POST /auth/passkey/register/options`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: WebAuthn registration challenge for workspace agents.
- Concepts: `auth`, `passkeys`
- Error codes: `invalid_request`
- Output: Returns `{ session_id, options }`.

## `auth.passkey.register.verify`

- CLI path: `auth passkey register verify`
- HTTP: `POST /auth/passkey/register/verify`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Verify WebAuthn attestation and issue tokens.
- Concepts: `auth`, `passkeys`
- Error codes: `invalid_request`, `invalid_token`
- Output: Returns `{ agent, tokens }`.

## `auth.principals.list`

- CLI path: `auth principals list`
- HTTP: `GET /auth/principals`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Operator visibility into registered principals for the workspace.
- Concepts: `auth`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns principal list JSON.

## `auth.principals.revoke`

- CLI path: `auth principals revoke`
- HTTP: `POST /auth/principals/{principal_id}/revoke`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Administrative revocation of a principal linkage.
- Concepts: `auth`
- Error codes: `auth_required`, `invalid_request`, `not_found`, `invalid_token`, `conflict`
- Output: Returns result JSON.

## `auth.token`

- CLI path: `auth token`
- HTTP: `POST /auth/token`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Assertion or refresh-token exchange for bearer access.
- Concepts: `auth`
- Error codes: `invalid_request`, `invalid_token`
- Output: Returns `{ tokens }` envelope.

## `boards.archive`

- CLI path: `boards archive`
- HTTP: `POST /boards/{board_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Soft-archive a board (orthogonal to business status; clears default list visibility).
- Concepts: `boards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board }`.

## `boards.cards.batch_add`

- CLI path: `boards cards create-batch`
- HTTP: `POST /boards/{board_id}/cards/batch`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create multiple cards in one transaction using a single board concurrency token.
- Concepts: `boards`, `cards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board, cards }`.

## `boards.cards.create`

- CLI path: `boards cards create`
- HTTP: `POST /boards/{board_id}/cards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a first-class card and attach it to a board.
- Concepts: `boards`, `cards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ card }`.

## `boards.cards.get`

- CLI path: `boards cards get`
- HTTP: `GET /boards/{board_id}/cards/{card_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve a card through its board membership context.
- Concepts: `boards`, `cards`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ card }`.

## `boards.cards.list`

- CLI path: `boards cards list`
- HTTP: `GET /boards/{board_id}/cards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: List cards on one board in canonical order.
- Concepts: `boards`, `cards`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ board_id, cards }`.

## `boards.create`

- CLI path: `boards create`
- HTTP: `POST /boards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a durable board over topics and cards.
- Concepts: `boards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ board }`.

## `boards.get`

- CLI path: `boards get`
- HTTP: `GET /boards/{board_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve canonical board state and summary.
- Concepts: `boards`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ board, summary }`.

## `boards.list`

- CLI path: `boards list`
- HTTP: `GET /boards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Scan durable coordination boards and lightweight summaries.
- Concepts: `boards`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ boards, summaries }`.

## `boards.patch`

- CLI path: `boards patch`
- HTTP: `PATCH /boards/{board_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Update board metadata with optimistic concurrency.
- Concepts: `boards`, `write`, `concurrency`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board }`.

## `boards.purge`

- CLI path: `boards purge`
- HTTP: `POST /boards/{board_id}/purge`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Permanently delete a trashed board (human-gated).
- Concepts: `boards`, `write`
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ purged, board_id }`.

## `boards.restore`

- CLI path: `boards restore`
- HTTP: `POST /boards/{board_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear trash lifecycle fields on a board after an explicit restore action.
- Concepts: `boards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board }`.

## `boards.trash`

- CLI path: `boards trash`
- HTTP: `POST /boards/{board_id}/trash`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Move board to trash with an explicit operator reason.
- Concepts: `boards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board }`.

## `boards.unarchive`

- CLI path: `boards unarchive`
- HTTP: `POST /boards/{board_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear archived_at on a board (restore default list visibility).
- Concepts: `boards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board }`.

## `boards.workspace`

- CLI path: `boards workspace`
- HTTP: `GET /boards/{board_id}/workspace`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load the operator-facing board workspace with cards, docs, and inbox sections.
- Concepts: `boards`, `workspace`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ board_id, board, primary_topic, cards, documents, inbox, board_summary, projection_freshness, board_summary_freshness, warnings, section_kinds, generated_at }`.

## `cards.archive`

- CLI path: `cards archive`
- HTTP: `POST /cards/{card_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Soft-delete a first-class card by setting archived_at (board concurrency via if_board_updated_at).
- Concepts: `cards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`, `already_trashed`
- Output: Returns `{ board, card }`.

## `cards.create`

- CLI path: `cards create`
- HTTP: `POST /cards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a card with the same body as POST /boards/{board_id}/cards, but supply board_id or board_ref here instead of a path segment. Interoperable with board-scoped create.
- Concepts: `cards`, `boards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board, card }` (same as board-scoped create).

## `cards.get`

- CLI path: `cards get`
- HTTP: `GET /cards/{card_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve one first-class card by id.
- Concepts: `cards`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ card }`.

## `cards.list`

- CLI path: `cards list`
- HTTP: `GET /cards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Scan first-class card resources across boards.
- Concepts: `cards`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ cards }`.

## `cards.move`

- CLI path: `cards move`
- HTTP: `POST /cards/{card_id}/move`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Reposition a card within a board column using the card's first-class identity.
- Concepts: `cards`, `boards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ card }`.

## `cards.patch`

- CLI path: `cards patch`
- HTTP: `PATCH /cards/{card_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Update card fields, including resolution and resolution refs.
- Concepts: `cards`, `write`, `concurrency`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ card }`.

## `cards.purge`

- CLI path: `cards purge`
- HTTP: `POST /cards/{card_id}/purge`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Permanently delete an archived or trashed card (human-gated).
- Concepts: `cards`, `write`
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ purged, card_id }`.

## `cards.restore`

- CLI path: `cards restore`
- HTTP: `POST /cards/{card_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear archive or trash lifecycle fields on a card so it reappears on boards.
- Concepts: `cards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board, card }`.

## `cards.timeline`

- CLI path: `cards timeline`
- HTTP: `GET /cards/{card_id}/timeline`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load chronological evidence and related resources for one card.
- Concepts: `cards`, `timeline`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ card, events, artifacts, cards, documents, threads }`.

## `cards.trash`

- CLI path: `cards trash`
- HTTP: `POST /cards/{card_id}/trash`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Move a card to trash with an explicit operator reason while keeping archive lifecycle distinct.
- Concepts: `cards`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ board, card }`.

## `derived.rebuild`

- CLI path: `derived rebuild`
- HTTP: `POST /derived/rebuild`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Deterministic operator repair for inbox/thread projections.
- Concepts: `projections`, `maintenance`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ ok: true }`.

## `docs.archive`

- CLI path: `docs archive`
- HTTP: `POST /docs/{document_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Soft-archive a document lineage (orthogonal to head revision content).
- Concepts: `docs`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ document, revision }`.

## `docs.create`

- CLI path: `docs create`
- HTTP: `POST /docs`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a canonical document lineage anchored to a typed subject ref.
- Concepts: `docs`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ document, revision }`.

## `docs.get`

- CLI path: `docs get`
- HTTP: `GET /docs/{document_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve a document lineage and its current head revision.
- Concepts: `docs`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ document, revision }`.

## `docs.list`

- CLI path: `docs list`
- HTTP: `GET /docs`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Scan canonical document lineages.
- Concepts: `docs`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ documents }`.

## `docs.purge`

- CLI path: `docs purge`
- HTTP: `POST /docs/{document_id}/purge`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Permanently delete a trashed document (human-gated).
- Concepts: `docs`, `write`
- Error codes: `auth_required`, `human_only`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ purged, document_id }`.

## `docs.restore`

- CLI path: `docs restore`
- HTTP: `POST /docs/{document_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear trash state on a document after an explicit restore action.
- Concepts: `docs`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ document, revision }`.

## `docs.revisions.create`

- CLI path: `docs revisions create`
- HTTP: `POST /docs/{document_id}/revisions`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Append a new immutable revision and advance the document head.
- Concepts: `docs`, `revisions`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ document, revision }`.

## `docs.revisions.get`

- CLI path: `docs revisions get`
- HTTP: `GET /docs/{document_id}/revisions/{revision_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve one immutable document revision.
- Concepts: `docs`, `revisions`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ document_id, revision }`.

## `docs.revisions.list`

- CLI path: `docs revisions list`
- HTTP: `GET /docs/{document_id}/revisions`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Enumerate immutable revisions for one document lineage.
- Concepts: `docs`, `revisions`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ document_id, revisions }`.

## `docs.trash`

- CLI path: `docs trash`
- HTTP: `POST /docs/{document_id}/trash`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Move a document lineage to trash with an explicit operator reason.
- Concepts: `docs`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ document, revision }`.

## `docs.unarchive`

- CLI path: `docs unarchive`
- HTTP: `POST /docs/{document_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear archived_at on a document so it returns to default visibility.
- Concepts: `docs`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ document, revision }`.

## `events.archive`

- CLI path: `events archive`
- HTTP: `POST /events/{event_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Set archived_at on an append-only event record for filtered views.
- Concepts: `events`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ event }`.

## `events.create`

- CLI path: `events create`
- HTTP: `POST /events`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Append an event that links first-class resources and evidence through typed refs.
- Concepts: `events`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ event }`.

## `events.get`

- CLI path: `events get`
- HTTP: `GET /events/{event_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Fetch one append-only event record by stable id.
- Concepts: `events`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ event }`.

## `events.list`

- CLI path: `events list`
- HTTP: `GET /events`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Inspect append-only event history across the workspace.
- Concepts: `events`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ events }`.

## `events.restore`

- CLI path: `events restore`
- HTTP: `POST /events/{event_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear trash state on an event after an explicit restore action.
- Concepts: `events`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ event }`.

## `events.stream`

- CLI path: `events stream`
- HTTP: `GET /events/stream`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Long-lived SSE feed of workspace events with optional thread/type filters and Last-Event-ID resume.
- Concepts: `events`
- Error codes: `auth_required`, `invalid_token`
- Output: Each SSE message is `event: …` with JSON data `{ "event": <event> }` (see core/docs/http-api.md).

## `events.trash`

- CLI path: `events trash`
- HTTP: `POST /events/{event_id}/trash`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Move event to trash with an explicit operator reason.
- Concepts: `events`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Output: Returns `{ event }`.

## `events.unarchive`

- CLI path: `events unarchive`
- HTTP: `POST /events/{event_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear archived_at on an event.
- Concepts: `events`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ event }`.

## `inbox.acknowledge`

- CLI path: `inbox acknowledge`
- HTTP: `POST /inbox/{inbox_id}/acknowledge`
- Stability: `beta`
- Surface: `projection`
- Input mode: `json-body`
- Why: Suppress or clear a derived inbox item via a durable acknowledgment event.
- Concepts: `inbox`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Output: Returns `{ event }`.

## `inbox.get`

- CLI path: `inbox get`
- HTTP: `GET /inbox/{inbox_id}`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Side-effect free read of one materialized inbox row.
- Concepts: `inbox`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ item, generated_at, projection_freshness }`.

## `inbox.list`

- CLI path: `inbox list`
- HTTP: `GET /inbox`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load the derived operator inbox generated from refs and canonical events.
- Concepts: `inbox`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ items }`.

## `inbox.stream`

- CLI path: `inbox stream`
- HTTP: `GET /inbox/stream`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Server-sent events feed of inbox projection updates.
- Concepts: `inbox`
- Error codes: `auth_required`, `invalid_token`
- Output: SSE `inbox_item` events with JSON payloads.

## `meta.commands.get`

- CLI path: `meta commands get`
- HTTP: `GET /meta/commands/{command_id}`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Resolve command metadata by stable command id.
- Concepts: `compatibility`
- Error codes: `meta_unavailable`, `not_found`
- Output: Returns `{ command }`.

## `meta.commands.list`

- CLI path: `meta commands list`
- HTTP: `GET /meta/commands`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Expose embedded OAR command metadata for discovery and codegen parity.
- Concepts: `compatibility`
- Error codes: `meta_unavailable`
- Output: Returns generated command registry JSON.

## `meta.concepts.get`

- CLI path: `meta concepts get`
- HTTP: `GET /meta/concepts/{concept_name}`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Expand one concept into related commands.
- Concepts: `compatibility`
- Error codes: `meta_unavailable`, `not_found`
- Output: Returns `{ concept: {...} }`.

## `meta.concepts.list`

- CLI path: `meta concepts list`
- HTTP: `GET /meta/concepts`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Group command metadata by concept tags.
- Concepts: `compatibility`
- Error codes: `meta_unavailable`
- Output: Returns `{ concepts: [...] }`.

## `meta.handshake`

- CLI path: `meta handshake`
- HTTP: `GET /meta/handshake`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Surface schema version, command registry digest, CLI gates, and instance metadata.
- Concepts: `compatibility`
- Error codes: `meta_unavailable`
- Output: Returns `{ core_version, api_version, schema_version, command_registry_digest, ... }`.

## `meta.health`

- CLI path: `meta health`
- HTTP: `GET /health`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Probe whether the core process is alive.
- Concepts: `health`
- Output: Returns `{ ok: true }`.

## `meta.livez`

- CLI path: `meta livez`
- HTTP: `GET /livez`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Lightweight liveness probe independent of storage readiness.
- Concepts: `health`
- Output: Returns `{ ok: true }`.

## `meta.readyz`

- CLI path: `meta readyz`
- HTTP: `GET /readyz`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Verify storage and projection subsystems are ready for traffic.
- Concepts: `health`, `readiness`
- Error codes: `storage_unavailable`
- Output: Returns `{ ok: true }` when the workspace is ready.

## `meta.version`

- CLI path: `meta version`
- HTTP: `GET /version`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Check compatibility between clients and core before writes.
- Concepts: `compatibility`
- Output: Returns `{ schema_version, command_registry_digest }`.

## `ops.blob.usage.rebuild`

- CLI path: `ops blob usage rebuild`
- HTTP: `POST /ops/blob-usage/rebuild`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Repair path for derived blob accounting after storage events.
- Concepts: `ops`, `maintenance`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ ok: true }` or error.

## `ops.health`

- CLI path: `ops health`
- HTTP: `GET /ops/health`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Operational readiness for projections, jobs, and operators.
- Concepts: `health`, `ops`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns structured health JSON.

## `ops.usage.summary`

- CLI path: `ops usage summary`
- HTTP: `GET /ops/usage-summary`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Operator-facing storage and count telemetry for the workspace.
- Concepts: `ops`, `quotas`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns usage envelope JSON.

## `packets.receipts.create`

- CLI path: `packets receipts create`
- HTTP: `POST /packets/receipts`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Record structured delivery evidence anchored by `subject_ref`.
- Concepts: `packets`, `evidence`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ artifact, packet_kind, packet }`.

## `packets.reviews.create`

- CLI path: `packets reviews create`
- HTTP: `POST /packets/reviews`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Record a structured review over a receipt anchored to the same card as subject_ref.
- Concepts: `packets`, `evidence`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ artifact, packet_kind, packet }`.

## `ref_edges.list`

- CLI path: `ref-edges list`
- HTTP: `GET /ref-edges`
- Stability: `beta`
- Surface: `diagnostic`
- Input mode: `query`
- Why: Query the write-through ref index by source or target typed ref (mutually exclusive); reverse lookup uses target_ref.
- Concepts: `refs`, `inspection`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ ref_edges }`.

## `secrets.create`

- CLI path: `secret create`
- HTTP: `POST /secrets`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Store an encrypted workspace credential with metadata.
- Concepts: `secrets`, `write`
- Error codes: `auth_required`, `invalid_token`, `human_only`, `invalid_request`, `resource_exists`, `secrets_not_configured`
- Output: Returns `{ secret }` (metadata only, value is not echoed).
- Agent notes: Only human principals may create secrets.

## `secrets.delete`

- CLI path: `secret delete`
- HTTP: `DELETE /secrets/{secret_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Permanently remove a secret and its encrypted value.
- Concepts: `secrets`, `write`
- Error codes: `auth_required`, `invalid_token`, `human_only`, `not_found`, `secrets_not_configured`
- Output: Returns `{ deleted: true, secret_id }`.
- Agent notes: Only human principals may delete secrets.

## `secrets.list`

- CLI path: `secret list`
- HTTP: `GET /secrets`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: List workspace secret metadata without exposing values.
- Concepts: `secrets`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ secrets }`.

## `secrets.reveal`

- CLI path: `secret get --reveal`
- HTTP: `POST /secrets/{secret_id}/reveal`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Decrypt and return a secret value. Logged in audit.
- Concepts: `secrets`
- Error codes: `auth_required`, `invalid_token`, `not_found`, `secrets_not_configured`
- Output: Returns `{ name, value }`.
- Agent notes: Every reveal is logged in auth audit. POST (not GET) to prevent caching.

## `secrets.reveal-batch`

- CLI path: `secret exec`
- HTTP: `POST /secrets/reveal-batch`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Batch-fetch secrets for env injection. Each reveal is audited.
- Concepts: `secrets`
- Error codes: `auth_required`, `invalid_token`, `not_found`, `invalid_request`, `secrets_not_configured`
- Output: Returns `{ secrets: [{ name, value }] }`.
- Agent notes: Each resolved secret generates an audit event. Missing names return not_found.

## `secrets.update`

- CLI path: `secret update`
- HTTP: `PUT /secrets/{secret_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Replace an encrypted secret value.
- Concepts: `secrets`, `write`
- Error codes: `auth_required`, `invalid_token`, `human_only`, `not_found`, `invalid_request`, `secrets_not_configured`
- Output: Returns `{ secret }` (metadata only).
- Agent notes: Only human principals may update secrets.

## `threads.context`

- CLI path: `threads context`
- HTTP: `GET /threads/{thread_id}/context`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load a compact coordination bundle (thread, recent events, key artifacts, cards, documents) for inspection and triage.
- Concepts: `threads`, `inspection`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`
- Output: Returns `{ thread, recent_events, key_artifacts, open_cards, documents }` plus forward-compatible fields.

## `threads.inspect`

- CLI path: `threads inspect`
- HTTP: `GET /threads/{thread_id}`
- Stability: `beta`
- Surface: `diagnostic`
- Input mode: `none`
- Why: Resolve one backing thread for low-level inspection and diagnostics.
- Concepts: `threads`, `inspection`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ thread }`.

## `threads.list`

- CLI path: `threads list`
- HTTP: `GET /threads`
- Stability: `beta`
- Surface: `diagnostic`
- Input mode: `none`
- Why: Inspect backing infrastructure threads without making them the primary planning noun.
- Concepts: `threads`, `inspection`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ threads }`.

## `threads.timeline`

- CLI path: `threads timeline`
- HTTP: `GET /threads/{thread_id}/timeline`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Retrieve event history plus typed-ref expansions for one backing thread.
- Concepts: `threads`, `timeline`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ thread, events, artifacts, topics, cards, documents }`.

## `threads.workspace`

- CLI path: `threads workspace`
- HTTP: `GET /threads/{thread_id}/workspace`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Read-only diagnostic projection that bundles context, inbox, and related-thread signals for one backing thread. Prefer topics.workspace for normal operator coordination when a topic exists.
- Concepts: `threads`, `workspace`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ thread, related_topics, cards, documents, board_memberships, inbox, projection_freshness }`.

## `topics.archive`

- CLI path: `topics archive`
- HTTP: `POST /topics/{topic_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Soft-archive a topic (orthogonal to business status; clears default list visibility).
- Concepts: `topics`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ topic }`.

## `topics.create`

- CLI path: `topics create`
- HTTP: `POST /topics`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a first-class durable topic before attaching cards, docs, or packets.
- Concepts: `topics`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`
- Output: Returns `{ topic }`.
- Agent notes: Replay-safe when the same request key and body are reused.

## `topics.get`

- CLI path: `topics get`
- HTTP: `GET /topics/{topic_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve one topic and its canonical durable fields.
- Concepts: `topics`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ topic }`.

## `topics.list`

- CLI path: `topics list`
- HTTP: `GET /topics`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Scan the durable topic inventory.
- Concepts: `topics`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ topics }`.

## `topics.patch`

- CLI path: `topics patch`
- HTTP: `PATCH /topics/{topic_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Update topic state with provenance and optimistic concurrency.
- Concepts: `topics`, `write`, `concurrency`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ topic }`.

## `topics.restore`

- CLI path: `topics restore`
- HTTP: `POST /topics/{topic_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear trash lifecycle fields on a topic after an explicit restore action.
- Concepts: `topics`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ topic }`.

## `topics.timeline`

- CLI path: `topics timeline`
- HTTP: `GET /topics/{topic_id}/timeline`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load chronological evidence and related resources for one topic.
- Concepts: `topics`, `timeline`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ topic, events, artifacts, cards, documents, threads }`.

## `topics.trash`

- CLI path: `topics trash`
- HTTP: `POST /topics/{topic_id}/trash`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Move topic to trash with an explicit operator reason.
- Concepts: `topics`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ topic }`.

## `topics.unarchive`

- CLI path: `topics unarchive`
- HTTP: `POST /topics/{topic_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Clear archived_at on a topic (restore default list visibility).
- Concepts: `topics`, `write`
- Error codes: `auth_required`, `invalid_request`, `invalid_token`, `not_found`, `conflict`
- Output: Returns `{ topic }`.

## `topics.workspace`

- CLI path: `topics workspace`
- HTTP: `GET /topics/{topic_id}/workspace`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Primary operator coordination read — load the topic workspace composed from linked cards, docs, backing threads, and inbox items. Prefer this over thread workspace for triage and planning.
- Concepts: `topics`, `workspace`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ topic, cards, boards, documents, threads, inbox, projection_freshness, generated_at }`.

