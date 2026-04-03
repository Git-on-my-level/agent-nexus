# OAR Command Registry

Generated from `contracts/oar-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.2.3`
- Commands: `100`

## `actors.list`

- CLI path: `actors list`
- HTTP: `GET /actors`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Resolve available actor identities for routing writes.
- Concepts: `identity`
- Error codes: `actor_registry_unavailable`
- Output: Returns `{ actors, next_cursor? }` ordered by created time ascending. Pagination is optional and backward-compatible.
- Agent notes: Safe and idempotent. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.
- Examples:
  - List actors: `oar actors list --json`
  - Search actors by name: `oar actors list --q "bot" --json`
  - Paginated actor list: `oar actors list --limit 50 --json`

## `actors.register`

- CLI path: `actors register`
- HTTP: `POST /actors`
- Stability: `stable`
- Surface: `utility`
- Input mode: `json-body`
- Why: Bootstrap an authenticated caller identity before mutating thread state.
- Concepts: `identity`
- Error codes: `invalid_json`, `invalid_request`, `actor_exists`
- Output: Returns `{ actor }` with canonicalized stored values.
- Agent notes: Not idempotent by default; repeated creates with same id return conflict.
- Examples:
  - Register actor: `oar actors register --id bot-1 --display-name "Bot 1" --created-at 2026-03-04T10:00:00Z --json`

## `agents.me.get`

- CLI path: `agents me get`
- HTTP: `GET /agents/me`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Inspect current principal metadata and active/revoked keys.
- Concepts: `auth`, `identity`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`
- Output: Returns `{ agent, keys }`.
- Agent notes: Requires Bearer access token.
- Examples:
  - Get current profile: `oar agents me get --json`

## `agents.me.keys.rotate`

- CLI path: `agents me keys rotate`
- HTTP: `POST /agents/me/keys/rotate`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Replace the assertion key and invalidate the old key path.
- Concepts: `auth`, `key-management`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`, `invalid_request`
- Output: Returns `{ key }` for the new active key.
- Agent notes: Old keys are marked revoked and cannot mint assertion tokens.
- Examples:
  - Rotate key: `oar agents me keys rotate --public-key <base64-ed25519-pubkey> --json`

## `agents.me.patch`

- CLI path: `agents me patch`
- HTTP: `PATCH /agents/me`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Rename the authenticated agent or update its wake registration without re-registration.
- Concepts: `auth`, `identity`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`, `invalid_request`, `username_taken`
- Output: Returns `{ agent }`.
- Agent notes: Requires Bearer access token.
- Examples:
  - Rename current agent: `oar agents me patch --username renamed_agent --json`
  - Update wake registration: `oar agents me patch --from-file wake-registration.json --json`

## `agents.me.revoke`

- CLI path: `agents me revoke`
- HTTP: `POST /agents/me/revoke`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Permanently revoke the authenticated agent so future mint/refresh calls fail.
- Concepts: `auth`, `revocation`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`, `last_active_principal`
- Output: Returns `{ ok, principal, revocation }`; repeated calls after successful self-revoke require a fresh principal because the revoked caller can no longer authenticate.
- Agent notes: Requires Bearer access token. `allow_human_lockout=true` is an explicit break-glass path that can leave the workspace without an active human principal; include a non-empty `human_lockout_reason`.
- Examples:
  - Revoke self: `oar agents me revoke --json`

## `artifacts.archive`

- CLI path: `artifacts archive`
- HTTP: `POST /artifacts/{artifact_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Hide an artifact from default list views while preserving it for search and direct access.
- Concepts: `artifacts`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ artifact }` with archive metadata set.
- Agent notes: Idempotent; repeated archive calls on the same artifact are safe. Returns 409 if artifact is tombstoned.
- Examples:
  - Archive artifact: `oar artifacts archive --artifact-id artifact_123 --json`

## `artifacts.content.get`

- CLI path: `artifacts content get`
- HTTP: `GET /artifacts/{artifact_id}/content`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Fetch opaque artifact bytes for downstream processors.
- Concepts: `artifacts`, `content`
- Error codes: `not_found`
- Output: Raw bytes; content type mirrors stored artifact media.
- Agent notes: Stream to file for large payloads.
- Examples:
  - Download content: `oar artifacts content get --artifact-id artifact_123 > artifact.bin`

## `artifacts.create`

- CLI path: `artifacts create`
- HTTP: `POST /artifacts`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `file-and-body`
- Why: Persist immutable evidence blobs and metadata for references and review.
- Concepts: `artifacts`, `evidence`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Output: Returns `{ artifact }` metadata after content write.
- Agent notes: Treat as non-idempotent unless caller controls artifact id collisions.
- Examples:
  - Create structured artifact: `oar artifacts create --from-file artifact-create.json --json`

## `artifacts.get`

- CLI path: `artifacts get`
- HTTP: `GET /artifacts/{artifact_id}`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve artifact refs before downloading or rendering content.
- Concepts: `artifacts`
- Error codes: `not_found`
- Output: Returns `{ artifact }` metadata.
- Agent notes: Safe and idempotent.
- Examples:
  - Get artifact: `oar artifacts get --artifact-id artifact_123 --json`

## `artifacts.list`

- CLI path: `artifacts list`
- HTTP: `GET /artifacts`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Discover evidence and packets attached to threads.
- Concepts: `artifacts`, `filtering`
- Error codes: `invalid_request`
- Output: Returns `{ artifacts }` metadata only.
- Agent notes: Safe and idempotent.
- Examples:
  - List work orders for a thread: `oar artifacts list --kind work_order --thread-id thread_123 --json`

## `artifacts.purge`

- CLI path: `artifacts purge`
- HTTP: `POST /artifacts/{artifact_id}/purge`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Permanently remove a tombstoned artifact and reclaim storage. Human-only to prevent accidental data loss by automated agents.
- Concepts: `artifacts`, `lifecycle`
- Error codes: `invalid_json`, `not_found`, `not_tombstoned`, `artifact_in_use`, `human_only`
- Output: Returns `{ purged: true, artifact_id }` on success.
- Agent notes: 403 if the caller is not a human principal. 409 if the artifact is not tombstoned or is still referenced by document revisions.
- Examples:
  - Purge artifact: `oar artifacts purge --artifact-id artifact_123 --json`

## `artifacts.restore`

- CLI path: `artifacts restore`
- HTTP: `POST /artifacts/{artifact_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Reverse a tombstone on an artifact, making it active and visible in default list queries again.
- Concepts: `artifacts`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `not_tombstoned`
- Output: Returns `{ artifact }` with tombstone metadata cleared.
- Agent notes: Returns 409 if the artifact is not currently tombstoned.
- Examples:
  - Restore artifact: `oar artifacts restore --artifact-id artifact_123 --json`

## `artifacts.tombstone`

- CLI path: `artifacts tombstone`
- HTTP: `POST /artifacts/{artifact_id}/tombstone`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Mark an artifact as inactive while preserving provenance; tombstoned artifacts are excluded from list by default.
- Concepts: `artifacts`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ artifact }` with updated tombstone metadata.
- Agent notes: Idempotent; repeated tombstone calls on the same artifact are safe.
- Examples:
  - Tombstone artifact: `oar artifacts tombstone --artifact-id artifact_123 --reason "superseded by newer version" --json`

## `artifacts.unarchive`

- CLI path: `artifacts unarchive`
- HTTP: `POST /artifacts/{artifact_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Return an archived artifact to the default list views.
- Concepts: `artifacts`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `not_archived`
- Output: Returns `{ artifact }` with archive metadata cleared.
- Agent notes: Returns 409 if the artifact is not currently archived.
- Examples:
  - Unarchive artifact: `oar artifacts unarchive --artifact-id artifact_123 --json`

## `auth.agents.register`

- CLI path: `auth register`
- HTTP: `POST /auth/agents/register`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Register an agent principal with a bootstrap token for the first principal or an invite token for later principals.
- Concepts: `auth`, `identity`
- Error codes: `invalid_json`, `invalid_request`, `invalid_token`, `username_taken`
- Output: Returns `{ agent, key, tokens }`.
- Agent notes: Bootstrap is accepted only for the first successful principal registration. Later registrations require an invite token.
- Examples:
  - Bootstrap first agent: `oar auth register --username agent.one --bootstrap-token <token> --json`
  - Register invited agent: `oar auth register --username agent.two --invite-token <token> --json`

## `auth.audit.list`

- CLI path: `auth audit list`
- HTTP: `GET /auth/audit`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect durable auth and onboarding audit facts for principal registration, invite lifecycle, and revocation activity.
- Concepts: `auth`, `audit`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`, `invalid_request`
- Output: Returns `{ events, next_cursor? }` ordered newest first.
- Agent notes: Requires Bearer access token. Pagination is bounded from the start with `limit` and `cursor`.
- Examples:
  - List auth audit events: `oar auth audit list --json`

## `auth.bootstrap.status`

- CLI path: `auth bootstrap status`
- HTTP: `GET /auth/bootstrap/status`
- Stability: `beta`
- Input mode: `none`
- Why: Check whether first-principal bootstrap registration is still available for this workspace.
- Concepts: `auth`, `onboarding`
- Output: Returns `{ bootstrap_registration_available }` without exposing token material.
- Agent notes: This endpoint is intentionally non-enumerating beyond the single bootstrap availability boolean.
- Examples:
  - Read bootstrap status: `oar auth bootstrap status --json`

## `auth.invites.create`

- CLI path: `auth invites create`
- HTTP: `POST /auth/invites`
- Stability: `beta`
- Input mode: `json-body`
- Why: Mint a single-use invite token for a future human or agent registration.
- Concepts: `auth`, `onboarding`
- Error codes: `auth_required`, `invalid_json`, `invalid_request`, `invalid_token`, `agent_revoked`
- Output: Returns `{ invite, token }`. The raw token is returned only once at creation time.
- Agent notes: Requires Bearer access token. `kind` may be `human`, `agent`, or `any`.
- Examples:
  - Create agent invite: `oar auth invites create --kind agent --json`

## `auth.invites.list`

- CLI path: `auth invites list`
- HTTP: `GET /auth/invites`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect current invite state without exposing token secrets.
- Concepts: `auth`, `onboarding`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`
- Output: Returns `{ invites }` ordered by create time descending.
- Agent notes: Requires Bearer access token. Returned invites contain metadata only, never raw tokens.
- Examples:
  - List invites: `oar auth invites list --json`

## `auth.invites.revoke`

- CLI path: `auth invites revoke`
- HTTP: `POST /auth/invites/{invite_id}/revoke`
- Stability: `beta`
- Input mode: `none`
- Why: Invalidate an invite token before it is consumed.
- Concepts: `auth`, `onboarding`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`, `not_found`
- Output: Returns `{ invite }` with updated revoke metadata.
- Agent notes: Requires Bearer access token.
- Examples:
  - Revoke invite: `oar auth invites revoke --invite-id invite_123 --json`

## `auth.passkey.login.options`

- CLI path: `auth passkey login options`
- HTTP: `POST /auth/passkey/login/options`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Create a WebAuthn assertion challenge for passkey authentication.
- Concepts: `auth`, `passkey`
- Error codes: `invalid_json`, `not_found`
- Output: Returns `{ session_id, options }` where `options` is a WebAuthn assertion payload.
- Agent notes: Provide `username` to scope login to one principal, or omit it for discoverable login.

## `auth.passkey.login.verify`

- CLI path: `auth passkey login verify`
- HTTP: `POST /auth/passkey/login/verify`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Verify a WebAuthn assertion and issue a fresh token bundle.
- Concepts: `auth`, `passkey`
- Error codes: `invalid_json`, `invalid_request`, `invalid_token`, `agent_revoked`
- Output: Returns `{ agent, tokens }` when passkey verification succeeds.
- Agent notes: Session ids are one-time use and expire quickly.

## `auth.passkey.register.options`

- CLI path: `auth passkey register options`
- HTTP: `POST /auth/passkey/register/options`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Create a WebAuthn registration challenge for a human principal during managed bootstrap or invite acceptance.
- Concepts: `auth`, `passkey`
- Error codes: `invalid_json`, `invalid_request`, `invalid_token`
- Output: Returns `{ session_id, options }` where `options` is a WebAuthn registration payload.
- Agent notes: Requires a bootstrap token for the first successful human registration or an invite token for later registrations.

## `auth.passkey.register.verify`

- CLI path: `auth passkey register verify`
- HTTP: `POST /auth/passkey/register/verify`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Verify a WebAuthn attestation for managed bootstrap or invite acceptance, create the principal, and issue the initial token bundle.
- Concepts: `auth`, `passkey`
- Error codes: `invalid_json`, `invalid_request`, `invalid_token`
- Output: Returns `{ agent, tokens }` for the newly registered passkey principal.
- Agent notes: Session ids are one-time use and expire quickly. The same bootstrap or invite token used to open the registration flow must be presented again here.

## `auth.principals.list`

- CLI path: `auth principals list`
- HTTP: `GET /auth/principals`
- Stability: `beta`
- Input mode: `none`
- Why: Inspect the current workspace principal inventory, including revoked principals, without direct database access.
- Concepts: `auth`, `identity`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`, `invalid_request`
- Output: Returns `{ principals, active_human_principal_count, next_cursor? }` ordered by create time descending.
- Agent notes: Requires Bearer access token. Pagination is bounded from the start with `limit` and `cursor`.
- Examples:
  - List principals: `oar auth principals list --json`

## `auth.principals.revoke`

- CLI path: `auth principals revoke`
- HTTP: `POST /auth/principals/{agent_id}/revoke`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Let a hosted operator revoke another principal through a first-class, audit-safe path.
- Concepts: `auth`, `identity`, `revocation`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`, `not_found`, `last_active_principal`
- Output: Returns `{ ok, principal, revocation }` and is idempotent when the target principal is already revoked.
- Agent notes: Requires Bearer access token. Set `allow_human_lockout=true` only for explicit break-glass recovery work and include a non-empty `human_lockout_reason`.
- Examples:
  - Revoke a principal: `oar auth principals revoke --agent-id agent_123 --json`
  - Break glass to revoke the last active human principal: `oar auth principals revoke --agent-id agent_123 --allow-human-lockout --human-lockout-reason "incident recovery" --json`

## `auth.token`

- CLI path: `auth token`
- HTTP: `POST /auth/token`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Exchange a refresh token or key assertion for a fresh token bundle.
- Concepts: `auth`, `token-lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `invalid_token`, `key_mismatch`, `agent_revoked`
- Output: Returns `{ tokens }`.
- Agent notes: Refresh tokens are one-time use and rotated on successful exchange.
- Examples:
  - Refresh token grant: `oar auth token --grant-type refresh_token --refresh-token <token> --json`
  - Assertion grant: `oar auth token --grant-type assertion --agent-id <id> --key-id <id> --signed-at <rfc3339> --signature <base64> --json`

## `boards.archive`

- CLI path: `boards archive`
- HTTP: `POST /boards/{board_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Hide a board from default list views while preserving it for search and direct access.
- Concepts: `boards`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ board }` with archive metadata set.
- Agent notes: Idempotent; repeated archive calls on the same board are safe. Returns 409 if board is tombstoned.
- Examples:
  - Archive board: `oar boards archive --board-id board_product_launch --json`

## `boards.cards.archive`

- CLI path: `boards cards archive`
- HTTP: `POST /cards/{card_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Archive a board card artifact while preserving its version history and board provenance.
- Concepts: `boards`, `planning`, `history`, `concurrency`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Output: Returns `{ board, card }` after the card is archived.
- Agent notes: Archive is the v2 replacement for legacy remove semantics. Historical thread-backed cards remain resolvable through old events.
- Examples:
  - Archive card: `oar boards cards archive --card-id card_123 --json`

## `boards.cards.create`

- CLI path: `boards cards create`
- HTTP: `POST /boards/{board_id}/cards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a first-class board card artifact, optionally linked to a parent thread, with canonical placement and server-owned rank.
- Concepts: `boards`, `planning`, `ordering`, `concurrency`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Output: Returns `{ board, card }` after card creation and board concurrency-token advancement.
- Agent notes: Replay-safe when `request_key` is reused with the same body. Cards may be standalone tasks or wrap an existing thread via `parent_thread`.
- Examples:
  - Create standalone board card: `oar boards cards create --board-id board_product_launch --title "Buy groceries" --column backlog --json`

## `boards.cards.get`

- CLI path: `boards cards get`
- HTTP: `GET /boards/{board_id}/cards/{id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Read the current board card artifact plus version history.
- Concepts: `boards`, `planning`, `history`
- Error codes: `invalid_request`, `not_found`
- Output: Returns `{ card }` with embedded version history.
- Agent notes: The identifier accepts `card_id` and legacy thread-backed cards can still be resolved through their parent thread id.
- Examples:
  - Get board card: `oar boards cards get --board-id board_product_launch --card-id card_123 --json`

## `boards.cards.list`

- CLI path: `boards cards list`
- HTTP: `GET /boards/{board_id}/cards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Read canonical board membership, column placement, and rank ordering without hydrating the full board workspace.
- Concepts: `boards`, `planning`, `ordering`
- Error codes: `invalid_request`, `not_found`
- Output: Returns `{ board_id, cards }` ordered by canonical column sequence and per-column rank.
- Agent notes: Safe and idempotent. Use `boards.workspace` when you also need hydrated thread, document, and summary sections.
- Examples:
  - List board cards: `oar boards cards list --board-id board_product_launch --json`

## `boards.cards.move`

- CLI path: `boards cards move`
- HTTP: `POST /boards/{board_id}/cards/{id}/move`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Request relative placement for a card while keeping rank tokens opaque and server-owned.
- Concepts: `boards`, `planning`, `ordering`, `concurrency`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Output: Returns `{ board, card }` after the move is applied.
- Agent notes: Provide at most one of `before_thread_id` or `after_thread_id`. If neither is set, the card moves to the end of the target column.
- Examples:
  - Move card into review: `oar boards cards move --board-id board_product_launch --card-id card_123 --column review --json`

## `boards.cards.update`

- CLI path: `boards cards update`
- HTTP: `PATCH /cards/{card_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Patch mutable board-card fields and record a new card version automatically.
- Concepts: `boards`, `planning`, `history`, `concurrency`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Output: Returns `{ board, card }` after the card update and version increment are persisted.
- Agent notes: Set `if_board_updated_at` from the current board read before patching card metadata.
- Examples:
  - Mark card done: `oar boards cards update --card-id card_123 --status done --if-board-updated-at 2026-03-08T00:00:00Z --json`

## `boards.create`

- CLI path: `boards create`
- HTTP: `POST /boards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create a first-class coordination board with a canonical primary thread and optional primary document.
- Concepts: `boards`, `planning`, `concurrency`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`
- Output: Returns `{ board }` with server-owned identity and concurrency metadata.
- Agent notes: Replay-safe when `request_key` is reused with the same body. The primary thread is required and is never created as a card implicitly.
- Examples:
  - Create board: `oar boards create --from-file board-create.json --json`

## `boards.get`

- CLI path: `boards get`
- HTTP: `GET /boards/{board_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve one board's canonical metadata and concurrency token without hydrating the full workspace projection.
- Concepts: `boards`, `planning`
- Error codes: `invalid_request`, `not_found`
- Output: Returns `{ board }`.
- Agent notes: Safe and idempotent.
- Examples:
  - Get board: `oar boards get --board-id board_product_launch --json`

## `boards.list`

- CLI path: `boards list`
- HTTP: `GET /boards`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Discover durable coordination boards with enough summary data for list pages and CLI triage without per-board fan-out.
- Concepts: `boards`, `planning`, `summaries`
- Error codes: `invalid_request`
- Output: Returns `{ boards, next_cursor? }`, where each item includes canonical board metadata plus a derived summary. Pagination is optional and backward-compatible.
- Agent notes: Safe and idempotent. Use repeatable `label` and `owner` filters to narrow the list server-side. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.
- Examples:
  - List boards: `oar boards list --json`
  - List active boards for an owner: `oar boards list --status active --owner actor_ceo --json`
  - Search boards by label: `oar boards list --q "launch" --json`
  - Paginated board list: `oar boards list --limit 30 --json`

## `boards.purge`

- CLI path: `boards purge`
- HTTP: `POST /boards/{board_id}/purge`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Permanently remove a tombstoned board and reclaim storage. Human-only to prevent accidental data loss by automated agents.
- Concepts: `boards`, `lifecycle`
- Error codes: `invalid_json`, `not_found`, `not_tombstoned`, `human_only`
- Output: Returns `{ purged: true, board_id }` on success.
- Agent notes: 403 if the caller is not a human principal. 409 if the board is not tombstoned.
- Examples:
  - Purge board: `oar boards purge --board-id board_product_launch --json`

## `boards.restore`

- CLI path: `boards restore`
- HTTP: `POST /boards/{board_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Reverse a tombstone on a board, making it active and visible in default list queries again.
- Concepts: `boards`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `not_tombstoned`
- Output: Returns `{ board }` with tombstone metadata cleared.
- Agent notes: Returns 409 if the board is not currently tombstoned.
- Examples:
  - Restore board: `oar boards restore --board-id board_product_launch --json`

## `boards.tombstone`

- CLI path: `boards tombstone`
- HTTP: `POST /boards/{board_id}/tombstone`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Mark a board as inactive while preserving provenance; tombstoned boards are excluded from list by default.
- Concepts: `boards`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ board }` with updated tombstone metadata.
- Agent notes: Idempotent; repeated tombstone calls are safe.
- Examples:
  - Tombstone board: `oar boards tombstone --board-id board_product_launch --reason "initiative closed" --json`

## `boards.unarchive`

- CLI path: `boards unarchive`
- HTTP: `POST /boards/{board_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Return an archived board to the default list views.
- Concepts: `boards`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `not_archived`
- Output: Returns `{ board }` with archive metadata cleared.
- Agent notes: Returns 409 if the board is not currently archived.
- Examples:
  - Unarchive board: `oar boards unarchive --board-id board_product_launch --json`

## `boards.update`

- CLI path: `boards update`
- HTTP: `PATCH /boards/{board_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Patch mutable board metadata with optimistic concurrency while preserving server-owned identity and timestamps.
- Concepts: `boards`, `planning`, `concurrency`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Output: Returns `{ board }` after the metadata patch is applied.
- Agent notes: Set `if_updated_at` from `boards.get` or `boards.workspace` to avoid lost updates.
- Examples:
  - Update board metadata: `oar boards update --board-id board_product_launch --from-file board-update.json --json`

## `boards.workspace`

- CLI path: `boards workspace`
- HTTP: `GET /boards/{board_id}/workspace`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load one board's canonical organizing map plus hydrated backing resources and derived scan sections in a single round-trip.
- Concepts: `boards`, `planning`, `threads`, `docs`, `commitments`, `inbox`
- Error codes: `invalid_request`, `not_found`
- Output: Returns `{ board_id, board, primary_thread, primary_document, cards, documents, commitments, inbox, board_summary, projection_freshness, board_summary_freshness, section_kinds, generated_at }`, where each card keeps canonical membership/backing data separate from derived summary/freshness.
- Agent notes: Derived board workspace projection; do not build durable automation directly on projection payload shapes. Prefer canonical boards, board-card membership, and threads for durable substrate. Prefer this as the board workspace read path for CLI and web. Card envelopes keep canonical membership/backing refs separate from derived summary/freshness.
- Examples:
  - Board workspace: `oar boards workspace --board-id board_product_launch --json`

## `commitments.create`

- CLI path: `commitments create`
- HTTP: `POST /commitments`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Track accountable work items tied to a thread.
- Concepts: `commitments`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`
- Output: Returns `{ commitment }` with generated id.
- Agent notes: Replay-safe when `request_key` is reused with the same body; otherwise each create issues a new commitment id.
- Examples:
  - Create commitment: `oar commitments create --from-file commitment.json --json`

## `commitments.get`

- CLI path: `commitments get`
- HTTP: `GET /commitments/{commitment_id}`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Read commitment status/details before status transitions.
- Concepts: `commitments`
- Error codes: `not_found`
- Output: Returns `{ commitment }`.
- Agent notes: Safe and idempotent.
- Examples:
  - Get commitment: `oar commitments get --commitment-id commitment_123 --json`

## `commitments.list`

- CLI path: `commitments list`
- HTTP: `GET /commitments`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Monitor open/blocked work and due windows.
- Concepts: `commitments`, `filtering`
- Error codes: `invalid_request`
- Output: Returns `{ commitments }`.
- Agent notes: Safe and idempotent.
- Examples:
  - List open commitments for a thread: `oar commitments list --thread-id thread_123 --status open --json`

## `commitments.patch`

- CLI path: `commitments patch`
- HTTP: `PATCH /commitments/{commitment_id}`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Update ownership, due date, or status with evidence-aware transition rules.
- Concepts: `commitments`, `patch`, `provenance`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Output: Returns `{ commitment }` and emits a status-change event when applicable.
- Agent notes: Provide `refs` for restricted transitions and use `if_updated_at` to avoid lost updates.
- Examples:
  - Mark commitment done: `oar commitments patch --commitment-id commitment_123 --from-file commitment-patch.json --json`

## `derived.rebuild`

- CLI path: `derived rebuild`
- HTTP: `POST /derived/rebuild`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Force deterministic recomputation of derived views after maintenance or migration.
- Concepts: `derived-views`, `maintenance`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Output: Returns `{ ok: true }`.
- Agent notes: Mutating admin command; serialize with other writes.
- Examples:
  - Rebuild derived: `oar derived rebuild --actor-id system --json`

## `docs.archive`

- CLI path: `docs archive`
- HTTP: `POST /docs/{document_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Hide a document from default list views while preserving it for search and direct access.
- Concepts: `docs`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ document, revision }` with archive metadata set.
- Agent notes: Idempotent; repeated archive calls on the same document are safe. Returns 409 if document is tombstoned.
- Examples:
  - Archive document: `oar docs archive --document-id product-constitution --json`

## `docs.create`

- CLI path: `docs create`
- HTTP: `POST /docs`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Bootstrap a first-class document identity and initial revision without manual head-pointer management.
- Concepts: `docs`, `revisions`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`
- Output: Returns `{ document, revision }` where `revision` is the new head.
- Agent notes: Replay-safe when `request_key` is reused with the same body; core can issue the canonical document id when one is omitted.
- Examples:
  - Create document: `oar docs create --from-file doc-create.json --json`

## `docs.get`

- CLI path: `docs get`
- HTTP: `GET /docs/{document_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve the current authoritative document head without client-side lineage traversal.
- Concepts: `docs`, `revisions`
- Error codes: `invalid_request`, `not_found`
- Output: Returns `{ document, revision }` where `revision` is the current head.
- Agent notes: Safe and idempotent.
- Examples:
  - Get document head: `oar docs get --document-id product-constitution --json`

## `docs.history`

- CLI path: `docs history`
- HTTP: `GET /docs/{document_id}/history`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Traverse full document lineage in canonical revision-number order.
- Concepts: `docs`, `revisions`, `lineage`
- Error codes: `invalid_request`, `not_found`
- Output: Returns `{ document_id, revisions }` ordered by ascending `revision_number`.
- Agent notes: Safe and idempotent.
- Examples:
  - List document history: `oar docs history --document-id product-constitution --json`

## `docs.list`

- CLI path: `docs list`
- HTTP: `GET /docs`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Discover available documents without resolving each head individually, optionally scoped to a single thread.
- Concepts: `docs`, `revisions`
- Error codes: `invalid_request`
- Output: Returns `{ documents, next_cursor? }` ordered by `updated_at` descending. Pagination is optional and backward-compatible.
- Agent notes: Safe and idempotent. Use `thread_id` to focus on one thread's docs and `include_tombstoned=true` when auditing superseded documents. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.
- Examples:
  - List documents: `oar docs list --json`
  - Search documents by title: `oar docs list --q "constitution" --json`
  - Paginated document list: `oar docs list --limit 50 --json`

## `docs.purge`

- CLI path: `docs purge`
- HTTP: `POST /docs/{document_id}/purge`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Permanently remove a tombstoned document and its revisions. Human-only.
- Concepts: `docs`, `lifecycle`
- Error codes: `not_found`, `not_tombstoned`, `human_only`
- Output: Returns `{ purged: true, document_id }` on success.
- Agent notes: 403 if the caller is not a human principal. 409 if the document is not tombstoned.
- Examples:
  - Purge document: `oar docs purge --document-id product-constitution --json`

## `docs.restore`

- CLI path: `docs restore`
- HTTP: `POST /docs/{document_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Reverse a tombstone on a document, making it active and visible in default list queries again.
- Concepts: `docs`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `not_tombstoned`
- Output: Returns `{ document, revision }` with tombstone metadata cleared.
- Agent notes: Returns 409 if the document is not currently tombstoned.
- Examples:
  - Restore document: `oar docs restore --document-id product-constitution --json`

## `docs.revision.get`

- CLI path: `docs revision get`
- HTTP: `GET /docs/{document_id}/revisions/{revision_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Read a specific historical revision payload without mutating document head.
- Concepts: `docs`, `revisions`
- Error codes: `invalid_request`, `not_found`
- Output: Returns `{ revision }` including metadata and revision content.
- Agent notes: Safe and idempotent.
- Examples:
  - Get revision: `oar docs revision get --document-id product-constitution --revision-id 019f... --json`

## `docs.tombstone`

- CLI path: `docs tombstone`
- HTTP: `POST /docs/{document_id}/tombstone`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Mark a document as inactive while preserving revision history and provenance.
- Concepts: `docs`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ document, revision }` with updated tombstone metadata.
- Agent notes: Idempotent; repeated tombstone calls on the same document are safe.
- Examples:
  - Tombstone document: `oar docs tombstone --document-id product-constitution --reason "replaced by v2" --json`

## `docs.unarchive`

- CLI path: `docs unarchive`
- HTTP: `POST /docs/{document_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Return an archived document to the default list views.
- Concepts: `docs`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `not_archived`
- Output: Returns `{ document, revision }` with archive metadata cleared.
- Agent notes: Returns 409 if the document is not currently archived.
- Examples:
  - Unarchive document: `oar docs unarchive --document-id product-constitution --json`

## `docs.update`

- CLI path: `docs update`
- HTTP: `PATCH /docs/{document_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Append a revision and atomically advance document head with optimistic concurrency.
- Concepts: `docs`, `revisions`, `concurrency`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Output: Returns `{ document, revision }` for the newly-created head revision.
- Agent notes: Set `if_base_revision` from `docs.get` to prevent lost updates.
- Examples:
  - Update document: `oar docs update --document-id product-constitution --from-file doc-update.json --json`

## `events.archive`

- CLI path: `events archive`
- HTTP: `POST /events/{event_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Hide an event from default timeline and message views while preserving it.
- Concepts: `events`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ event }` with archive metadata set.
- Agent notes: Idempotent. For message_posted events, cascades to all reply descendants.
- Examples:
  - Archive event: `oar events archive --event-id evt_123 --json`

## `events.create`

- CLI path: `events create`
- HTTP: `POST /events`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Record append-only narrative or protocol state changes that complement snapshots.
- Concepts: `events`, `append-only`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Output: Returns `{ event }` with generated id and timestamp.
- Agent notes: Replay-safe when `request_key` is reused with the same body.
- Examples:
  - Append event: `oar events create --from-file event.json --json`

## `events.get`

- CLI path: `events get`
- HTTP: `GET /events/{event_id}`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve event references and evidence links.
- Concepts: `events`
- Error codes: `not_found`
- Output: Returns `{ event }`.
- Agent notes: Safe and idempotent.
- Examples:
  - Get event: `oar events get --event-id event_123 --json`

## `events.restore`

- CLI path: `events restore`
- HTTP: `POST /events/{event_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Undo a tombstone and return the event to active state.
- Concepts: `events`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ event }` with tombstone metadata cleared.
- Agent notes: Idempotent. For message_posted events, cascades to all reply descendants.
- Examples:
  - Restore event: `oar events restore --event-id evt_123 --json`

## `events.stream`

- CLI path: `events stream`
- HTTP: `GET /events/stream`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Follow live event updates with resumable SSE semantics.
- Concepts: `events`, `streaming`
- Error codes: `internal_error`, `cli_outdated`
- Output: SSE stream where each event carries `{ event }` and uses event id for resume.
- Agent notes: Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Examples:
  - Stream all events: `oar events stream --json`
  - Resume by id: `oar events stream --last-event-id <event_id> --json`

## `events.tombstone`

- CLI path: `events tombstone`
- HTTP: `POST /events/{event_id}/tombstone`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Mark an event as deleted while preserving audit trail; tombstoned events are excluded from timeline and messages by default.
- Concepts: `events`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ event }` with tombstone metadata set.
- Agent notes: Idempotent. For message_posted events, cascades to all reply descendants.
- Examples:
  - Tombstone event: `oar events tombstone --event-id evt_123 --reason "spam" --json`

## `events.unarchive`

- CLI path: `events unarchive`
- HTTP: `POST /events/{event_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Restore an archived event back to default timeline and message views.
- Concepts: `events`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ event }` with archive metadata cleared.
- Agent notes: Idempotent. For message_posted events, cascades to all reply descendants.
- Examples:
  - Unarchive event: `oar events unarchive --event-id evt_123 --json`

## `inbox.ack`

- CLI path: `inbox ack`
- HTTP: `POST /inbox/ack`
- Stability: `stable`
- Surface: `projection`
- Input mode: `json-body`
- Why: Suppress already-acted-on derived inbox signals.
- Concepts: `inbox`, `events`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Output: Returns `{ event }` representing acknowledgment.
- Agent notes: Derived inbox view; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Idempotent at semantic level; repeated acks should not duplicate active inbox items.
- Examples:
  - Ack inbox item: `oar inbox ack --thread-id thread_123 --inbox-item-id inbox:item-1 --json`
  - Ack inbox item by id: `oar inbox ack inbox:decision_needed:thread_123:none:event_1 --json`

## `inbox.get`

- CLI path: `inbox get`
- HTTP: `GET /inbox/{inbox_item_id}`
- Stability: `stable`
- Surface: `projection`
- Input mode: `none`
- Why: Inspect one inbox item in detail before acting on it.
- Concepts: `inbox`, `derived-views`
- Error codes: `not_found`
- Output: Returns `{ item, generated_at }` for the requested inbox item.
- Agent notes: Derived inbox view; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. CLI supports canonical ids, aliases, and unique prefixes.
- Examples:
  - Get inbox item by canonical id: `oar inbox get --id inbox:decision_needed:thread_123:none:event_123 --json`
  - Get inbox item by alias: `oar inbox get --id ibx_abcd1234ef56 --json`

## `inbox.list`

- CLI path: `inbox list`
- HTTP: `GET /inbox`
- Stability: `stable`
- Surface: `projection`
- Input mode: `none`
- Why: Surface derived actionable risk and decision signals.
- Concepts: `inbox`, `derived-views`
- Output: Returns `{ items, generated_at }`.
- Agent notes: Derived inbox view; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Safe and idempotent.
- Examples:
  - List inbox: `oar inbox list --json`

## `inbox.stream`

- CLI path: `inbox stream`
- HTTP: `GET /inbox/stream`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Follow live derived inbox updates without repeated polling.
- Concepts: `inbox`, `derived-views`, `streaming`
- Error codes: `internal_error`, `cli_outdated`
- Output: SSE stream where each event carries `{ item }` derived inbox metadata.
- Agent notes: Derived inbox view; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.
- Examples:
  - Stream inbox updates: `oar inbox stream --json`
  - Resume inbox stream: `oar inbox stream --last-event-id <id> --json`

## `meta.commands.get`

- CLI path: `meta commands get`
- HTTP: `GET /meta/commands/{command_id}`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Resolve a stable command id to full generated metadata and guidance.
- Concepts: `meta`, `introspection`
- Error codes: `not_found`, `meta_unavailable`, `cli_outdated`
- Output: Returns `{ command }` metadata for the requested command id.
- Agent notes: Safe and idempotent.
- Examples:
  - Read command metadata: `oar meta commands get --command-id threads.list --json`

## `meta.commands.list`

- CLI path: `meta commands list`
- HTTP: `GET /meta/commands`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Load generated command metadata used for help, docs, and agent introspection.
- Concepts: `meta`, `introspection`
- Error codes: `meta_unavailable`, `cli_outdated`
- Output: Returns generated command registry metadata from the canonical contract.
- Agent notes: Safe and idempotent. Response shape matches committed generated artifacts.
- Examples:
  - List command metadata: `oar meta commands list --json`

## `meta.concepts.get`

- CLI path: `meta concepts get`
- HTTP: `GET /meta/concepts/{concept_name}`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Resolve one concept tag to the commands that implement that concept.
- Concepts: `meta`, `concepts`
- Error codes: `not_found`, `meta_unavailable`, `cli_outdated`
- Output: Returns `{ concept }` including matched command ids and command metadata.
- Agent notes: Safe and idempotent.
- Examples:
  - Read one concept: `oar meta concepts get --concept-name compatibility --json`

## `meta.concepts.list`

- CLI path: `meta concepts list`
- HTTP: `GET /meta/concepts`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Discover conceptual groupings of commands generated from contract metadata.
- Concepts: `meta`, `concepts`
- Error codes: `meta_unavailable`, `cli_outdated`
- Output: Returns `{ concepts }` summary metadata for all known concepts.
- Agent notes: Safe and idempotent.
- Examples:
  - List concepts: `oar meta concepts list --json`

## `meta.handshake`

- CLI path: `meta handshake`
- HTTP: `GET /meta/handshake`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Discover compatibility, upgrade, and instance identity metadata before command execution.
- Concepts: `compatibility`, `handshake`
- Output: Returns compatibility fields including minimum supported CLI version.
- Agent notes: Safe and idempotent. Use this endpoint to proactively gate incompatible CLI versions.
- Examples:
  - Read handshake metadata: `oar meta handshake --json`

## `meta.health`

- CLI path: `meta health`
- HTTP: `GET /health`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Probe whether the core process is alive with a minimal public liveness payload.
- Concepts: `health`, `liveness`
- Output: Returns `{ ok: true }` when the service process is alive.
- Agent notes: Safe and idempotent; retry with backoff on transport failures.
- Examples:
  - Liveness check: `oar meta health --json`

## `meta.livez`

- CLI path: `meta livez`
- HTTP: `GET /livez`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Provide an explicit Kubernetes-style liveness alias for the minimal public probe.
- Concepts: `health`, `liveness`
- Output: Returns `{ ok: true }` when the service process is alive.
- Agent notes: Safe and idempotent.
- Examples:
  - Liveness alias: `oar api call --method GET --path /livez`

## `meta.ops.health`

- CLI path: `meta ops health`
- HTTP: `GET /ops/health`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Inspect detailed operator diagnostics such as projection-maintenance lag without exposing them on the public liveness/readiness probes.
- Concepts: `health`, `readiness`, `operations`
- Error codes: `auth_required`, `invalid_token`, `agent_revoked`, `storage_unavailable`
- Output: Returns `{ ok, projection_maintenance? }` after the readiness check passes.
- Agent notes: Requires an authenticated principal outside development-mode and loopback verification exceptions. Safe and idempotent.
- Examples:
  - Authenticated operator diagnostics: `oar api call --method GET --path /ops/health --header 'Authorization: Bearer <access-token>'`

## `meta.readyz`

- CLI path: `meta readyz`
- HTTP: `GET /readyz`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Probe whether core storage is ready before issuing stateful commands or marking the instance ready.
- Concepts: `health`, `readiness`
- Error codes: `storage_unavailable`
- Output: Returns `{ ok: true }` when the service and storage are ready.
- Agent notes: Safe and idempotent; retry with backoff on transport failures.
- Examples:
  - Readiness check: `oar api call --method GET --path /readyz`

## `meta.version`

- CLI path: `meta version`
- HTTP: `GET /version`
- Stability: `stable`
- Surface: `utility`
- Input mode: `none`
- Why: Verify compatibility between core and generated clients before performing writes.
- Concepts: `compatibility`, `schema`
- Output: Returns `{ schema_version, command_registry_digest }` for frontend/core compatibility checks.
- Agent notes: Safe and idempotent.
- Examples:
  - Read version: `oar meta version --json`

## `notifications.dismiss`

- CLI path: `notifications dismiss`
- HTTP: `POST /agent-notifications/dismiss`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Suppress future push wake delivery for one wake notification.
- Concepts: `events`
- Error codes: `auth_required`, `invalid_json`, `invalid_request`, `agent_revoked`, `not_found`, `conflict`
- Output: Returns `{ event, notification }` after the dismiss transition.
- Agent notes: Only the authenticated target agent can dismiss a notification.
- Examples:
  - Dismiss one notification: `oar notifications dismiss --wakeup-id wake_123 --json`

## `notifications.list`

- CLI path: `notifications list`
- HTTP: `GET /agent-notifications`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Read unread, read, or dismissed wake notifications for the current authenticated agent.
- Concepts: `events`, `derived-views`
- Error codes: `auth_required`, `invalid_request`, `agent_revoked`
- Output: Returns `{ items, generated_at }` for the current authenticated agent.
- Agent notes: Notification state is derived from canonical wake and notification events. Only the authenticated target agent can read its notifications.
- Examples:
  - List unread notifications: `oar notifications list --status unread --json`
  - List oldest unread first: `oar notifications list --status unread --order asc --json`

## `notifications.read`

- CLI path: `notifications read`
- HTTP: `POST /agent-notifications/read`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Mark one wake notification as consumed by the authenticated target agent.
- Concepts: `events`
- Error codes: `auth_required`, `invalid_json`, `invalid_request`, `agent_revoked`, `not_found`, `conflict`
- Output: Returns `{ event, notification }` after the read transition.
- Agent notes: Only the authenticated target agent can mark a notification read.
- Examples:
  - Mark one notification read: `oar notifications read --wakeup-id wake_123 --json`

## `packets.receipts.create`

- CLI path: `packets receipts create`
- HTTP: `POST /receipts`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Record execution output and verification evidence for a work order.
- Concepts: `packets`, `receipts`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Output: Returns `{ artifact, event }`.
- Agent notes: Replay-safe when `request_key` is reused with the same body. Include evidence refs that satisfy packet conventions.
- Examples:
  - Create receipt: `oar packets receipts create --from-file receipt.json --json`

## `packets.reviews.create`

- CLI path: `packets reviews create`
- HTTP: `POST /reviews`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Record acceptance/revision decisions over a receipt.
- Concepts: `packets`, `reviews`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Output: Returns `{ artifact, event }`.
- Agent notes: Include refs to both receipt and work order artifacts.
- Examples:
  - Create review: `oar packets reviews create --from-file review.json --json`

## `packets.work-orders.create`

- CLI path: `packets work-orders create`
- HTTP: `POST /work_orders`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create structured action packets with deterministic schema enforcement.
- Concepts: `packets`, `work-orders`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`
- Output: Returns `{ artifact, event }`.
- Agent notes: Replay-safe when `request_key` is reused with the same body; packet id fields may be omitted and core will issue the canonical artifact id.
- Examples:
  - Create work order: `oar packets work-orders create --from-file work-order.json --json`

## `snapshots.get`

- CLI path: `snapshots get`
- HTTP: `GET /snapshots/{snapshot_id}`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve arbitrary snapshot references encountered in event refs.
- Concepts: `snapshots`
- Error codes: `not_found`
- Output: Returns `{ snapshot }`.
- Agent notes: Safe and idempotent.
- Examples:
  - Get snapshot: `oar snapshots get --snapshot-id snapshot_123 --json`

## `threads.archive`

- CLI path: `threads archive`
- HTTP: `POST /threads/{thread_id}/archive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Hide a thread from default list views while preserving it for search and direct access.
- Concepts: `threads`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ thread }` with archive metadata set.
- Agent notes: Idempotent; repeated archive calls on the same thread are safe. Returns 409 if thread is tombstoned.
- Examples:
  - Archive thread: `oar threads archive --thread-id thread_123 --json`

## `threads.context`

- CLI path: `threads context`
- HTTP: `GET /threads/{thread_id}/context`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load one thread's state, recent events, key artifacts, open commitments, and linked documents in a single round-trip; CLI `oar threads context` can aggregate across threads by composing multiple calls.
- Concepts: `threads`, `events`, `artifacts`, `commitments`, `docs`
- Error codes: `invalid_request`, `not_found`
- Output: Returns `{ thread, recent_events, key_artifacts, open_commitments, documents }`.
- Agent notes: Derived thread context projection; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Use include_artifact_content for prompt-ready previews; default mode keeps payloads lighter. Prefer `oar threads inspect` as the first single-thread coordination read.
- Examples:
  - Context with defaults: `oar threads context --thread-id thread_123 --json`
  - Context with artifact previews: `oar threads context --thread-id thread_123 --include-artifact-content --max-events 50 --json`

## `threads.create`

- CLI path: `threads create`
- HTTP: `POST /threads`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Open a new thread for tracking ongoing organizational work.
- Concepts: `threads`, `snapshots`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`
- Output: Returns `{ thread }` including generated id and audit fields.
- Agent notes: Replay-safe when `request_key` is reused with the same body; otherwise core issues a new canonical thread id.
- Examples:
  - Create thread: `oar threads create --from-file thread.json --json`

## `threads.get`

- CLI path: `threads get`
- HTTP: `GET /threads/{thread_id}`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Resolve a raw authoritative thread snapshot for low-level reads before patching or composing packets.
- Concepts: `threads`
- Error codes: `not_found`
- Output: Returns `{ thread }`.
- Agent notes: Safe and idempotent. Prefer `oar threads inspect` for operator coordination reads.
- Examples:
  - Read thread: `oar threads get --thread-id thread_123 --json`

## `threads.list`

- CLI path: `threads list`
- HTTP: `GET /threads`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Retrieve current thread state for triage and scheduling decisions.
- Concepts: `threads`, `filtering`
- Error codes: `invalid_request`
- Output: Returns `{ threads, next_cursor? }`; query filters are additive. Pagination is optional and backward-compatible.
- Agent notes: Safe and idempotent. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.
- Examples:
  - List active p1 threads: `oar threads list --status active --priority p1 --json`
  - Search threads by title: `oar threads list --q "launch" --json`
  - Paginated thread list: `oar threads list --limit 20 --json`

## `threads.patch`

- CLI path: `threads patch`
- HTTP: `PATCH /threads/{thread_id}`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Update mutable thread fields while preserving unknown data and auditability.
- Concepts: `threads`, `patch`
- Error codes: `invalid_json`, `invalid_request`, `unknown_actor_id`, `conflict`, `not_found`
- Output: Returns `{ thread }` after patch merge and emitted event side effect.
- Agent notes: Use `if_updated_at` for optimistic concurrency.
- Examples:
  - Patch thread: `oar threads patch --thread-id thread_123 --from-file patch.json --json`

## `threads.purge`

- CLI path: `threads purge`
- HTTP: `POST /threads/{thread_id}/purge`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Permanently remove a tombstoned thread and reclaim storage. Human-only to prevent accidental data loss by automated agents.
- Concepts: `threads`, `lifecycle`
- Error codes: `invalid_json`, `not_found`, `not_tombstoned`, `human_only`
- Output: Returns `{ purged: true, thread_id }` on success.
- Agent notes: 403 if the caller is not a human principal. 409 if the thread is not tombstoned.
- Examples:
  - Purge thread: `oar threads purge --thread-id thread_123 --json`

## `threads.restore`

- CLI path: `threads restore`
- HTTP: `POST /threads/{thread_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Reverse a tombstone on a thread, making it active and visible in default list queries again.
- Concepts: `threads`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `not_tombstoned`
- Output: Returns `{ thread }` with tombstone metadata cleared.
- Agent notes: Returns 409 if the thread is not currently tombstoned.
- Examples:
  - Restore thread: `oar threads restore --thread-id thread_123 --json`

## `threads.timeline`

- CLI path: `threads timeline`
- HTTP: `GET /threads/{thread_id}/timeline`
- Stability: `stable`
- Surface: `canonical`
- Input mode: `none`
- Why: Retrieve narrative event history plus referenced snapshots/artifacts in one call.
- Concepts: `threads`, `events`, `provenance`
- Error codes: `not_found`
- Output: Returns `{ events, snapshots, artifacts }` where snapshot/artifact maps are sparse.
- Agent notes: Events stay time ordered; missing refs are omitted from expansion maps.
- Examples:
  - Timeline: `oar threads timeline --thread-id thread_123 --json`

## `threads.tombstone`

- CLI path: `threads tombstone`
- HTTP: `POST /threads/{thread_id}/tombstone`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Mark a thread as inactive while preserving provenance; tombstoned threads are excluded from list by default.
- Concepts: `threads`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ thread }` with updated tombstone metadata.
- Agent notes: Idempotent; repeated tombstone calls are safe.
- Examples:
  - Tombstone thread: `oar threads tombstone --thread-id thread_123 --reason "merged into parent" --json`

## `threads.unarchive`

- CLI path: `threads unarchive`
- HTTP: `POST /threads/{thread_id}/unarchive`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Return an archived thread to the default list views.
- Concepts: `threads`, `lifecycle`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `not_archived`
- Output: Returns `{ thread }` with archive metadata cleared.
- Agent notes: Returns 409 if the thread is not currently archived.
- Examples:
  - Unarchive thread: `oar threads unarchive --thread-id thread_123 --json`

## `threads.workspace`

- CLI path: `threads workspace`
- HTTP: `GET /threads/{thread_id}/workspace`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load one thread workspace projection from the server, including canonical thread context plus derived collaboration and inbox summaries, so CLI and web do not need client-side joins.
- Concepts: `threads`, `events`, `artifacts`, `commitments`, `docs`, `boards`, `inbox`
- Error codes: `invalid_request`, `not_found`
- Output: Returns `{ thread_id, thread, context, collaboration, board_memberships, inbox, pending_decisions, related_threads, follow_up, section_kinds }`, with explicit section classifications.
- Agent notes: Derived workspace projection; do not build durable automation directly on projection payload shapes. Prefer canonical events and threads for durable substrate. Prefer this as the single-thread coordination read path. `section_kinds` distinguishes canonical versus derived sections, including additive board membership joins.
- Examples:
  - Workspace with defaults: `oar threads workspace --thread-id thread_123 --json`
  - Workspace with hydrated related review events: `oar threads workspace --thread-id thread_123 --include-related-event-content --include-artifact-content --json`

