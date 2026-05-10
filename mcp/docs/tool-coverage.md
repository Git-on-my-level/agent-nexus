# MCP Tool Coverage

Generated from `contracts/gen/meta/commands.json` and `mcp/policy/default_tool_policy.yaml`.

- Command count: 124
- Contract version: 0.6.0
- OpenAPI version: 3.1.0

## Counts by Group

| Group | Commands |
| --- | --- |
| actors | 2 |
| agent | 4 |
| agents | 4 |
| artifacts | 10 |
| auth | 15 |
| boards | 14 |
| cards | 13 |
| derived | 1 |
| docs | 12 |
| events | 8 |
| home | 2 |
| inbox | 4 |
| meta | 9 |
| ops | 3 |
| ref-edges | 1 |
| secret | 6 |
| threads | 5 |
| topics | 10 |
| usage | 1 |

## Counts by Classification

| Classification | Commands |
| --- | --- |
| exposed_read | 43 |
| exposed_write | 41 |
| gated_admin | 14 |
| gated_sensitive | 11 |
| unsupported_bootstrap_auth | 6 |
| unsupported_interactive | 5 |
| unsupported_shell_shaped | 1 |
| unsupported_streaming | 3 |

## Counts by Surface

| Surface | Commands | Rule |
| --- | --- | --- |
| standalone default | 84 | exposed_read + exposed_write + adapted |
| hosted default | 42 | explicit read-only private-app allowlist |
| gated | 25 | requires explicit admin/sensitive policy scope |
| adapted | 0 | provider compatibility adapters |
| unsupported | 15 | not represented as direct MCP tools in v1 |

## Command Inventory

| Command | Group | Method | Path | Classification | Reason |
| --- | --- | --- | --- | --- | --- |
| actors.create | actors | POST | /actors | unsupported_bootstrap_auth | dev-only actor registration is not an MCP auth path |
| actors.list | actors | GET | /actors | gated_admin | actor inventory is auth-administrative |
| agent.notification-receipts.stream | agent | GET | /stream/agent-notification-receipts | unsupported_streaming | SSE stream needs a bounded read adapter before MCP exposure |
| agent.notifications.dismiss | agent | POST | /agent-notifications/dismiss | exposed_write | ordinary authenticated agent notification state write |
| agent.notifications.list | agent | GET | /agent-notifications | exposed_read | bounded authenticated agent notification projection |
| agent.notifications.read | agent | POST | /agent-notifications/read | exposed_write | ordinary authenticated agent notification state write |
| agents.me.get | agents | GET | /agents/me | exposed_read | authenticated caller self-inspection |
| agents.me.keys.rotate | agents | POST | /agents/me/keys/rotate | gated_sensitive | agent key rotation changes credential material |
| agents.me.patch | agents | PATCH | /agents/me | gated_admin | agent profile mutation changes principal metadata |
| agents.me.revoke | agents | POST | /agents/me/revoke | gated_sensitive | revocation disables credentials |
| artifacts.archive | artifacts | POST | /artifacts/{artifact_id}/archive | exposed_write | ordinary reversible artifact lifecycle write |
| artifacts.attachments.create | artifacts | POST | /artifacts/attachments | unsupported_shell_shaped | multipart binary upload needs a dedicated MCP content adapter |
| artifacts.content | artifacts | GET | /artifacts/{artifact_id}/content | exposed_read | artifact content read; executor must bound and redact output |
| artifacts.create | artifacts | POST | /artifacts | exposed_write | ordinary artifact creation through workspace API |
| artifacts.get | artifacts | GET | /artifacts/{artifact_id} | exposed_read | artifact metadata read |
| artifacts.list | artifacts | GET | /artifacts | exposed_read | artifact inventory read |
| artifacts.purge | artifacts | POST | /artifacts/{artifact_id}/purge | gated_sensitive | permanent deletion is destructive |
| artifacts.restore | artifacts | POST | /artifacts/{artifact_id}/restore | exposed_write | ordinary reversible artifact lifecycle write |
| artifacts.trash | artifacts | POST | /artifacts/{artifact_id}/trash | exposed_write | ordinary reversible artifact lifecycle write |
| artifacts.unarchive | artifacts | POST | /artifacts/{artifact_id}/unarchive | exposed_write | ordinary reversible artifact lifecycle write |
| auth.agents.register | auth | POST | /auth/agents/register | unsupported_bootstrap_auth | agent registration is an auth bootstrap flow |
| auth.audit.list | auth | GET | /auth/audit | gated_admin | auth audit inventory is administrative |
| auth.bootstrap.status | auth | GET | /auth/bootstrap/status | unsupported_bootstrap_auth | bootstrap status is part of registration ceremony |
| auth.invites.create | auth | POST | /auth/invites | gated_admin | invite issuance is administrative |
| auth.invites.list | auth | GET | /auth/invites | gated_admin | invite inventory is administrative |
| auth.invites.revoke | auth | POST | /auth/invites/{invite_id}/revoke | gated_admin | invite revocation is administrative |
| auth.passkey.dev.login | auth | POST | /auth/passkey/dev/login | unsupported_bootstrap_auth | dev-only passkey bypass is not an MCP auth path |
| auth.passkey.dev.register | auth | POST | /auth/passkey/dev/register | unsupported_bootstrap_auth | dev-only passkey bypass is not an MCP auth path |
| auth.passkey.login.options | auth | POST | /auth/passkey/login/options | unsupported_interactive | WebAuthn ceremony requires interactive browser mediation |
| auth.passkey.login.verify | auth | POST | /auth/passkey/login/verify | unsupported_interactive | WebAuthn ceremony requires interactive browser mediation |
| auth.passkey.register.options | auth | POST | /auth/passkey/register/options | unsupported_interactive | WebAuthn ceremony requires interactive browser mediation |
| auth.passkey.register.verify | auth | POST | /auth/passkey/register/verify | unsupported_interactive | WebAuthn ceremony requires interactive browser mediation |
| auth.principals.list | auth | GET | /auth/principals | gated_admin | principal inventory is administrative |
| auth.principals.revoke | auth | POST | /auth/principals/{principal_id}/revoke | gated_admin | principal revocation is administrative and disabling |
| auth.token | auth | POST | /auth/token | unsupported_bootstrap_auth | raw token exchange is not exposed as an MCP tool |
| boards.archive | boards | POST | /boards/{board_id}/archive | exposed_write | ordinary reversible board lifecycle write |
| boards.cards.batch_add | boards | POST | /boards/{board_id}/cards/batch | exposed_write | ordinary board card creation |
| boards.cards.create | boards | POST | /boards/{board_id}/cards | exposed_write | ordinary board card creation |
| boards.cards.get | boards | GET | /boards/{board_id}/cards/{card_id} | exposed_read | board-scoped card read |
| boards.cards.list | boards | GET | /boards/{board_id}/cards | exposed_read | board card inventory read |
| boards.create | boards | POST | /boards | exposed_write | ordinary board creation |
| boards.get | boards | GET | /boards/{board_id} | exposed_read | board read |
| boards.list | boards | GET | /boards | exposed_read | board inventory read |
| boards.patch | boards | PATCH | /boards/{board_id} | exposed_write | ordinary board update with concurrency controls |
| boards.purge | boards | POST | /boards/{board_id}/purge | gated_sensitive | permanent deletion is destructive |
| boards.restore | boards | POST | /boards/{board_id}/restore | exposed_write | ordinary reversible board lifecycle write |
| boards.trash | boards | POST | /boards/{board_id}/trash | exposed_write | ordinary reversible board lifecycle write |
| boards.unarchive | boards | POST | /boards/{board_id}/unarchive | exposed_write | ordinary reversible board lifecycle write |
| boards.workspace | boards | GET | /boards/{board_id}/workspace | exposed_read | bounded board workspace projection |
| cards.archive | cards | POST | /cards/{card_id}/archive | exposed_write | ordinary reversible card lifecycle write |
| cards.create | cards | POST | /cards | exposed_write | ordinary card creation |
| cards.get | cards | GET | /cards/{card_id} | exposed_read | card read |
| cards.list | cards | GET | /cards | exposed_read | card inventory read |
| cards.move | cards | POST | /cards/{card_id}/move | exposed_write | ordinary card board-position write |
| cards.patch | cards | PATCH | /cards/{card_id} | exposed_write | ordinary card update with concurrency controls |
| cards.purge | cards | POST | /cards/{card_id}/purge | gated_sensitive | permanent deletion is destructive |
| cards.restore | cards | POST | /cards/{card_id}/restore | exposed_write | ordinary reversible card lifecycle write |
| cards.revisions.create | cards | POST | /cards/{card_id}/revisions | exposed_write | ordinary card revision creation |
| cards.revisions.get | cards | GET | /cards/{card_id}/revisions/{revision_id} | exposed_read | card revision read |
| cards.revisions.list | cards | GET | /cards/{card_id}/revisions | exposed_read | card revision inventory read |
| cards.timeline | cards | GET | /cards/{card_id}/timeline | exposed_read | bounded card timeline projection |
| cards.trash | cards | POST | /cards/{card_id}/trash | exposed_write | ordinary reversible card lifecycle write |
| derived.rebuild | derived | POST | /derived/rebuild | gated_admin | projection rebuild is maintenance/ops |
| docs.archive | docs | POST | /docs/{document_id}/archive | exposed_write | ordinary reversible document lifecycle write |
| docs.create | docs | POST | /docs | exposed_write | ordinary document creation |
| docs.get | docs | GET | /docs/{document_id} | exposed_read | document read |
| docs.list | docs | GET | /docs | exposed_read | document inventory read |
| docs.patch | docs | PATCH | /docs/{document_id} | exposed_write | ordinary document update with concurrency controls |
| docs.purge | docs | POST | /docs/{document_id}/purge | gated_sensitive | permanent deletion is destructive |
| docs.restore | docs | POST | /docs/{document_id}/restore | exposed_write | ordinary reversible document lifecycle write |
| docs.revisions.create | docs | POST | /docs/{document_id}/revisions | exposed_write | ordinary document revision creation |
| docs.revisions.get | docs | GET | /docs/{document_id}/revisions/{revision_id} | exposed_read | document revision read |
| docs.revisions.list | docs | GET | /docs/{document_id}/revisions | exposed_read | document revision inventory read |
| docs.trash | docs | POST | /docs/{document_id}/trash | exposed_write | ordinary reversible document lifecycle write |
| docs.unarchive | docs | POST | /docs/{document_id}/unarchive | exposed_write | ordinary reversible document lifecycle write |
| events.archive | events | POST | /events/{event_id}/archive | exposed_write | ordinary reversible event lifecycle write |
| events.create | events | POST | /events | exposed_write | ordinary event creation |
| events.get | events | GET | /events/{event_id} | exposed_read | event read |
| events.list | events | GET | /events | exposed_read | bounded event inventory read |
| events.restore | events | POST | /events/{event_id}/restore | exposed_write | ordinary reversible event lifecycle write |
| events.stream | events | GET | /stream/events | unsupported_streaming | SSE stream needs a bounded read adapter before MCP exposure |
| events.trash | events | POST | /events/{event_id}/trash | exposed_write | ordinary reversible event lifecycle write |
| events.unarchive | events | POST | /events/{event_id}/unarchive | exposed_write | ordinary reversible event lifecycle write |
| home.read | home | POST | /home/read | exposed_write | ordinary home read-marker write |
| home.unread | home | GET | /home/unread | exposed_read | home unread projection |
| inbox.get | inbox | GET | /inbox/{inbox_id} | exposed_read | inbox item read |
| inbox.list | inbox | GET | /inbox | exposed_read | bounded inbox inventory read |
| inbox.respond | inbox | POST | /inbox/{inbox_id}/respond | unsupported_interactive | human response submission requires human judgment |
| inbox.stream | inbox | GET | /stream/inbox | unsupported_streaming | SSE stream needs a bounded read adapter before MCP exposure |
| meta.commands.get | meta | GET | /meta/commands/{command_id} | exposed_read | command metadata read |
| meta.commands.list | meta | GET | /meta/commands | exposed_read | command metadata inventory read |
| meta.concepts.get | meta | GET | /meta/concepts/{concept_name} | exposed_read | concept metadata read |
| meta.concepts.list | meta | GET | /meta/concepts | exposed_read | concept metadata inventory read |
| meta.handshake | meta | GET | /meta/handshake | exposed_read | workspace capability metadata read |
| meta.health | meta | GET | /health | exposed_read | health diagnostic read |
| meta.livez | meta | GET | /livez | exposed_read | liveness diagnostic read |
| meta.readyz | meta | GET | /readyz | exposed_read | readiness diagnostic read |
| meta.version | meta | GET | /version | exposed_read | version metadata read |
| ops.blob.usage.rebuild | ops | POST | /ops/blob-usage/rebuild | gated_admin | blob usage rebuild is maintenance/ops |
| ops.health | ops | GET | /ops/health | gated_admin | ops health can expose operational diagnostics |
| ops.usage.summary | ops | GET | /ops/usage-summary | gated_admin | unversioned usage summary is ops/quota telemetry |
| ref_edges.list | ref-edges | GET | /ref-edges | exposed_read | reference edge inventory read |
| secrets.create | secret | POST | /secrets | gated_sensitive | secret payload write is sensitive |
| secrets.delete | secret | DELETE | /secrets/{secret_id} | gated_sensitive | secret deletion is destructive |
| secrets.list | secret | GET | /secrets | gated_admin | secret inventory is administrative |
| secrets.reveal | secret | POST | /secrets/{secret_id}/reveal | gated_sensitive | secret value reveal is sensitive |
| secrets.reveal-batch | secret | POST | /secrets/reveal-batch | gated_sensitive | secret value reveal is sensitive |
| secrets.update | secret | PUT | /secrets/{secret_id} | gated_sensitive | secret payload write is sensitive |
| threads.context | threads | GET | /threads/{thread_id}/context | exposed_read | bounded thread context projection |
| threads.inspect | threads | GET | /threads/{thread_id} | exposed_read | thread diagnostic read |
| threads.list | threads | GET | /threads | exposed_read | thread inventory read |
| threads.timeline | threads | GET | /threads/{thread_id}/timeline | exposed_read | bounded thread timeline projection |
| threads.workspace | threads | GET | /threads/{thread_id}/workspace | exposed_read | bounded thread workspace projection |
| topics.archive | topics | POST | /topics/{topic_id}/archive | exposed_write | ordinary reversible topic lifecycle write |
| topics.create | topics | POST | /topics | exposed_write | ordinary topic creation |
| topics.get | topics | GET | /topics/{topic_id} | exposed_read | topic read |
| topics.list | topics | GET | /topics | exposed_read | topic inventory read |
| topics.patch | topics | PATCH | /topics/{topic_id} | exposed_write | ordinary topic update with concurrency controls |
| topics.restore | topics | POST | /topics/{topic_id}/restore | exposed_write | ordinary reversible topic lifecycle write |
| topics.timeline | topics | GET | /topics/{topic_id}/timeline | exposed_read | bounded topic timeline projection |
| topics.trash | topics | POST | /topics/{topic_id}/trash | exposed_write | ordinary reversible topic lifecycle write |
| topics.unarchive | topics | POST | /topics/{topic_id}/unarchive | exposed_write | ordinary reversible topic lifecycle write |
| topics.workspace | topics | GET | /topics/{topic_id}/workspace | exposed_read | bounded topic workspace projection |
| usage.summary.v1 | usage | GET | /v1/usage/summary | gated_admin | versioned usage summary is quota/billing telemetry |
