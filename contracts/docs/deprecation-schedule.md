# Deprecation and removal schedule

This document tracks compatibility shims and legacy surfaces called out in the consolidation plan. **No fixed dates** until a release policy assigns them; items are ordered by typical dependency (CLI scripts and env first, schema/data last).

Each row lists **owner** (code area), **reason** (why the shim exists), **removal gate** (what must be true before deletion), and **references** (implementation entry points).

## CLI

| Surface | Owner | Reason | Removal gate | References |
|--------|-------|--------|--------------|------------|
| ~~`compat_aliases.go` old command shapes~~ | CLI | Transitional command mapping | **Removed** (pre-user batch) | `cli/internal/app/compat_aliases.go` |
| ~~`--reconnect` on stream commands~~ | CLI | Older follow semantics | **Removed** (pre-user batch) | stream command handlers in `cli/internal/app/` |
| ~~`anx bridge workspace-id --document-id agentreg.<handle>`~~ | CLI | Superseded handle flag | **Removed** (pre-user batch) | `cli/internal/app/bridge_commands.go` |
| Nested `{"move":{...}}` body on board card move HTTP API | Core HTTP + contract | Clients sent nested envelope before flat body was canonical | Log clients still using nested envelope; remove `flattenLegacyMoveCardEnvelope` after confirmed zero use | `core/internal/server/board_card_request_parse.go` (`flattenLegacyMoveCardEnvelope`, `decodeMoveCardHTTPPayload`) |

## Web UI

| Surface | Owner | Reason | Removal gate | References |
|--------|-------|--------|--------------|------------|
| ~~`ANX_PROJECTS` / `ANX_DEFAULT_PROJECT` env fallbacks~~ | Web UI config | Project-era env names for workspace catalog | **Removed** (pre-user) — use only `ANX_WORKSPACES` / `ANX_DEFAULT_WORKSPACE` | `web-ui/src/lib/compat/workspaceCompat.js` |
| Legacy project-slug request header (superseded) | Web UI + core proxy | Older clients sent alternate workspace slug header | **Removed** — set `x-anx-workspace-slug` | `web-ui` hooks / proxy, workspace compat layer |
| Thread URL → topic route canonicalization | Web UI routes | Backing threads may still be bookmarked at `/threads/...` while product is topic-first | Remove when telemetry shows negligible traffic to legacy thread entry paths for topic-backed threads | `web-ui/src/lib/server/threadTopicRouteRedirect.js` (`resolveLegacyThreadCanonicalAppPath`), `web-ui/src/routes/o/[organization]/w/[workspace]/threads/+page.server.js`, `web-ui/src/lib/topicRouteUtils.js` |
| Legacy `topic_type` enum values in UI (glyph / selects) | Web UI | Contract `enums.topic_type` still allows `case`, `process`, `relationship` for forward compatibility | Remove after stored topics migrated / contract enum tightened and UI can drop "(legacy)" option | `web-ui/src/lib/topicTypeGlyph.js`, `web-ui/src/lib/components/topic-detail/TopicDetailHeader.svelte`, `TopicTypeGlyph.svelte` |
| `auth_method` dual-accept `control_plane` + `external_grant` | Web UI session | Migration window for hosted human grants (see `DECISIONS.md`) | Remove `control_plane` accept path once older sessions and docs fully converge on `external_grant` | `web-ui/src/lib/authSession.js` |

## Inbox and typed refs (Web UI + core)

| Surface | Owner | Reason | Removal gate | References |
|--------|-------|--------|--------------|------------|
| Legacy inbox `refs` / subject inference fallbacks | Web UI + API consumers | Older inbox rows lacked `subject_ref`; UI infers from `topic_id`, `card_id`, etc. | No remaining items missing `subject_ref` (or server backfills); drop fallbacks in `getInboxSubjectRef` and related | `web-ui/src/lib/inboxUtils.js` (`getInboxSubjectRef`, `normalizeInboxCategory`), `web-ui/tests/unit/inboxRelatedRefsContract.test.js` |
| Inbox category string handling | Web UI | Categories are free-form strings normalized for display | Tighten when contract + server enforce closed set end-to-end | `web-ui/src/lib/inboxUtils.js` (`normalizeInboxCategory`, `INBOX_CATEGORY_*`) |

## Core: topics and persisted JSON

| Surface | Owner | Reason | Removal gate | References |
|--------|-------|--------|--------------|------------|
| `coerceLegacyTopicPersistedBody` and legacy topic body keys | Core primitives | Historical topic JSON used `thread_ref` / split primary thread fields instead of `thread_id` | All persisted topic bodies use canonical `thread_id` (or migration job completes) | `core/internal/primitives/topics_store.go` (`coerceLegacyTopicPersistedBody`, strip helpers) |
| Legacy cadence preset strings (`daily`, `weekly`, …) on threads | Core + contract | Pre-cron schedule vocabulary | Stored snapshots migrated; contract may tighten | Same as **Schema and data** (cadence row); `core/internal/server/threads_handlers.go` (`threadMatchesTagsAndCadence`, cadence query filters) |

## Core: board HTTP (deprecated request fields)

| Surface | Owner | Reason | Removal gate | References |
|--------|-------|--------|--------------|------------|
| Deprecated HTTP-only board/card fields (document/thread id spellings) | Core HTTP | Older clients sent non-typed id fields; folded into typed refs for create/update | Reject or remove aliases after client audit shows zero use | `core/internal/server/boards_handlers.go` (`mergeBoardHTTPConvenienceFields`), `core/internal/server/boards_logic_test.go` (`TestValidateBoardCardCreateRejectsLegacyThreadFields`); related `board_card_request_parse.go` |

## Control plane: migrations and tests

| Surface | Owner | Reason | Removal gate | References |
|--------|-------|--------|--------------|------------|
| Migration test seed id `launch_legacy` | Control plane storage tests | Durable id for assertive migration test cases | May be renamed in a test refactor; not a product API — keep or replace when migration test layout changes | `controlplane/internal/controlplane/storage/migrations_test.go` |

## Schema and data

| Surface | Owner | Reason | Removal gate | References |
|--------|-------|--------|--------------|------------|
| Legacy cadence preset strings (`daily`, `weekly`, …) | Contract + core | Interim bridge before cron-only cadence | Stored snapshots migrated; contract may tighten | `anx-schema.yaml` / thread cadence; integration tests under `core/internal/server/*_integration_test.go` |
| Bridge TOML `[router]` section | Bridge / ops | Replaced by core-embedded router | Already ignored; remove docs references when safe | Bridge adapter docs |

## Process

1. Announce in release notes with **deprecated** and **removes in** when dates exist.
2. Prefer one breaking batch per major version for CLI flags and env names.
3. Run `make check` and targeted `cli` / `web-ui` / `core` checks before deleting shims.
