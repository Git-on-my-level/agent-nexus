# MCP Tool Coverage

Generated from `contracts/gen/meta/commands.json` and `mcp/policy/default_tool_policy.yaml`.

- Command count: 280
- Contract version: 0.6.0
- OpenAPI version: 3.1.0

## Counts by Group

| Group | Commands |
| --- | --- |
| actors | 2 |
| adapters | 5 |
| agent | 8 |
| agents | 4 |
| artifacts | 10 |
| ask | 3 |
| asks | 4 |
| attention | 8 |
| auth | 22 |
| boards | 13 |
| bridge | 8 |
| cards | 13 |
| curation | 5 |
| deliveries | 3 |
| derived | 1 |
| docs | 19 |
| events | 8 |
| git | 5 |
| home | 2 |
| hook | 1 |
| host | 16 |
| inbox | 5 |
| integrations | 7 |
| lease | 4 |
| meta | 9 |
| notifications | 2 |
| ops | 3 |
| orient | 1 |
| overview | 2 |
| plan | 2 |
| pm | 27 |
| ref-edges | 1 |
| refs | 1 |
| report | 2 |
| runs | 3 |
| secret | 6 |
| series | 4 |
| sessions | 2 |
| sync | 3 |
| threads | 5 |
| topics | 10 |
| usage | 1 |
| work | 16 |
| workspace | 4 |

## Counts by Classification

| Classification | Commands |
| --- | --- |
| exposed_read | 94 |
| exposed_write | 79 |
| gated_admin | 29 |
| gated_sensitive | 13 |
| unsupported_bootstrap_auth | 11 |
| unsupported_interactive | 13 |
| unsupported_other | 9 |
| unsupported_shell_shaped | 26 |
| unsupported_streaming | 6 |

## Counts by Surface

| Surface | Commands | Rule |
| --- | --- | --- |
| standalone default | 173 | exposed_read + exposed_write + adapted |
| hosted default | 68 | explicit read-only private-app allowlist |
| gated | 42 | requires explicit admin/sensitive policy scope |
| adapted | 0 | provider compatibility adapters |
| unsupported | 65 | not represented as direct MCP tools in v1 |

## Command Inventory

| Command | Group | Method | Path | Classification | Reason |
| --- | --- | --- | --- | --- | --- |
| actors.create | actors | POST | /actors | unsupported_bootstrap_auth | dev-only actor registration is not an MCP auth path |
| actors.list | actors | GET | /actors | gated_admin | actor inventory is auth-administrative |
| adapters.declare | adapters | POST | /adapters | gated_admin | Explicitly grant a host agent permission to write named series; core rechecks administration transactionally. |
| adapters.delete | adapters | DELETE | /adapters/{name} | gated_admin | Delete declared series history; requires current workspace administration. |
| adapters.list | adapters | GET | /adapters | gated_admin | Inspect explicitly declared series grants; administration is required by core. |
| adapters.revoke | adapters | POST | /adapters/{name}/revoke | gated_admin | Withdraw a series grant; requires current workspace administration. |
| adapters.token | adapters | POST | /adapters/{name}/token | gated_sensitive | Return a short-lived credential for the owning enrolled agent; never expose by default. |
| agent.inbox.answers.read | agent | POST | /agent-inbox/answers/read | exposed_write | ordinary authenticated agent per-answer read state write |
| agent.inbox.asks.list | agent | GET | /agent-inbox/asks | exposed_read | requester-scoped, keyset-paginated human attention inbox projection |
| agent.inbox.subscribe | agent | POST | /agent-inbox/subscriptions | unsupported_other | Ask delivery is available through the CLI and HTTP API; MCP exposure requires separate qualification |
| agent.notification-receipts.stream | agent | GET | /stream/agent-notification-receipts | unsupported_streaming | SSE stream needs a bounded read adapter before MCP exposure |
| agent.notifications.dismiss | agent | POST | /agent-notifications/dismiss | exposed_write | ordinary authenticated agent notification state write |
| agent.notifications.list | agent | GET | /agent-notifications | exposed_read | bounded authenticated agent notification projection |
| agent.notifications.read | agent | POST | /agent-notifications/read | exposed_write | ordinary authenticated agent notification state write |
| agent.wakeups.stream | agent | GET | /stream/agent-wakeups | unsupported_streaming | SSE stream needs a bounded read adapter before MCP exposure |
| agents.get | agents | GET | /agents/{agent_id} | exposed_read | agent roster detail is workspace presence data |
| agents.list | agents | GET | /agents | exposed_read | agent roster is workspace presence data |
| agents.me.get | agents | GET | /agents/me | exposed_read | authenticated caller self-inspection |
| agents.me.presence | work | PATCH | /agents/me/presence | exposed_write | an agent may report its current task and progress note |
| agents.stream | agents | GET | /stream/agents | unsupported_streaming | ephemeral roster SSE needs a bounded read adapter before MCP exposure |
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
| asks.batch.create | ask | POST | /asks/batch | unsupported_other | Ask batch filing is available through the CLI and HTTP API; MCP exposure requires separate qualification |
| asks.delivery | asks | POST | /asks/{ask_id}/delivery | unsupported_other | Ask delivery is available through the CLI and HTTP API; MCP exposure requires separate qualification |
| asks.get | asks | GET | /asks/{ask_id} | unsupported_other | Ask delivery is available through the CLI and HTTP API; MCP exposure requires separate qualification |
| asks.presentation.get | ask | GET | /asks/{ask_id}/presentation | exposed_read | Scoped point read of ask presentation and concurrency token. |
| asks.revise | ask | POST | /asks/{ask_id}/revise | exposed_write | Strict presentation allowlist, current curator authorization, concurrency and atomic replay. |
| asks.stream | asks | GET | /stream/asks/{ask_id} | unsupported_streaming | SSE stream needs a bounded read adapter before MCP exposure |
| asks.subscribe | asks | POST | /asks/{ask_id}/subscriptions | unsupported_other | Ask delivery is available through the CLI and HTTP API; MCP exposure requires separate qualification |
| attention.agent.get | attention | GET | /attention/agent | exposed_read | Read the workspace's human-designated attention agent. |
| attention.agent.set | attention | PUT | /attention/agent | unsupported_interactive | Only a human may designate the workspace attention agent. |
| attention.changes | attention | GET | /attention/changes | exposed_read | Bounded principal-scoped dashboard changes without advancing visit markers. |
| attention.feedback.create | attention | POST | /attention/feedback | unsupported_interactive | Attention feedback records human judgment and requires a human principal. |
| attention.feedback.list | attention | GET | /attention/feedback | exposed_read | Workspace feedback available to agents under canonical resource visibility. |
| attention.items | attention | GET | /attention/items | exposed_read | Bounded recipient-scoped attention placements and mandatory human asks. |
| attention.place | attention | POST | /attention/placements | exposed_write | Reversible attention placement with a reason and optional replay key. |
| attention.visits | attention | POST | /attention/visits | exposed_write | Advance only the authenticated agent's own dashboard visit marker. |
| auth.access-requests.approve | auth | POST | /auth/access-requests/{request_id}/approve | unsupported_interactive | human-only privileged grant approval requires human judgment |
| auth.access-requests.deny | auth | POST | /auth/access-requests/{request_id}/deny | unsupported_interactive | human-only privileged grant denial requires human judgment |
| auth.access-requests.list | auth | GET | /auth/access-requests | unsupported_interactive | human-only pending access inventory |
| auth.access-requests.request | auth | POST | /auth/access-requests | exposed_write | self-scoped privileged grant request; approval remains human-only |
| auth.access-requests.summary | auth | GET | /auth/access/summary | unsupported_interactive | human-only pending access inventory count |
| auth.admins.grant | auth | POST | /auth/admins/{principal_id}/grant | gated_admin | delegating auth-admin requires a human principal |
| auth.admins.list | auth | GET | /auth/admins | gated_admin | explicit agent grant inventory is administrative |
| auth.admins.revoke | auth | POST | /auth/admins/{principal_id}/revoke | gated_admin | withdrawing auth-admin requires a human principal |
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
| bridge.add | bridge | LOCAL |  | unsupported_shell_shaped | host service and execution registration stay on the local CLI |
| bridge.doctor | bridge | LOCAL |  | unsupported_shell_shaped | host service and execution registration stay on the local CLI |
| bridge.install | bridge | LOCAL |  | unsupported_shell_shaped | host service and execution registration stay on the local CLI |
| bridge.remove | bridge | LOCAL |  | unsupported_shell_shaped | host service and execution registration stay on the local CLI |
| bridge.run | bridge | LOCAL |  | unsupported_shell_shaped | host service and execution registration stay on the local CLI |
| bridge.start | bridge | LOCAL |  | unsupported_shell_shaped | host service and execution registration stay on the local CLI |
| bridge.status | bridge | LOCAL |  | unsupported_shell_shaped | host service and execution registration stay on the local CLI |
| bridge.stop | bridge | LOCAL |  | unsupported_shell_shaped | host service and execution registration stay on the local CLI |
| brief.get | orient | GET | /brief | exposed_read | brief.v1: the caller's decisions, initiatives against promises, own asks and answers. |
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
| curation.candidates | curation | GET | /curation/candidates | exposed_read | Bounded curation candidates a PM reviews before proposing; read only. |
| curation.metrics | curation | GET | /curation/metrics | exposed_read | Counts only; no titles or refs. |
| curation.propose | curation | POST | /curation/proposals | exposed_write | Agent-only curation resolution applies or dismisses quiet suggestions and records a receipt; never creates human attention. |
| curation.run | curation | POST | /curation/run | gated_admin | Workspace-wide formula pass that core also runs on its own schedule. |
| curation.ungroup | curation | POST | /curation/ungroup | exposed_write | Authenticated agent or human reverses delivery grouping without answering or withdrawing asks. |
| deliveries.claim | deliveries | POST | /deliveries/claim | unsupported_other | Delivery claims belong to the owning adapter's runtime, not an MCP tool call |
| deliveries.list | deliveries | GET | /deliveries | exposed_read | The caller's own queued notifications and wake deliveries, bounded by limit. |
| deliveries.receipt | deliveries | POST | /deliveries/{delivery_id}/receipt | unsupported_other | Delivery receipts belong to the owning adapter's runtime, not an MCP tool call |
| derived.rebuild | derived | POST | /derived/rebuild | gated_admin | projection rebuild is maintenance/ops |
| docs.archive | docs | POST | /docs/{document_id}/archive | exposed_write | ordinary reversible document lifecycle write |
| docs.comments.create | docs | POST | /docs/{document_id}/comments | exposed_write | ordinary document comment write for cross-host knowledge |
| docs.comments.delete | docs | DELETE | /docs/{document_id}/comments/{comment_id} | exposed_write | author delete of a document comment |
| docs.comments.list | docs | GET | /docs/{document_id}/comments | exposed_read | document comment thread read |
| docs.comments.reply | docs | POST | /docs/{document_id}/comments/{comment_id}/replies | exposed_write | ordinary document comment reply write |
| docs.comments.update | docs | PATCH | /docs/{document_id}/comments/{comment_id} | exposed_write | author edit of a document comment |
| docs.create | docs | POST | /docs | exposed_write | ordinary document creation |
| docs.get | docs | GET | /docs/{document_id} | exposed_read | document read |
| docs.list | docs | GET | /docs | exposed_read | document inventory read |
| docs.patch | docs | PATCH | /docs/{document_id} | exposed_write | ordinary document update with concurrency controls |
| docs.purge | docs | POST | /docs/{document_id}/purge | gated_sensitive | permanent deletion is destructive |
| docs.put | docs | PUT | /docs/{document_id} | exposed_write | idempotent document create-or-replace by handle |
| docs.restore | docs | POST | /docs/{document_id}/restore | exposed_write | ordinary reversible document lifecycle write |
| docs.revisions.create | docs | POST | /docs/{document_id}/revisions | exposed_write | ordinary document revision creation |
| docs.revisions.get | docs | GET | /docs/{document_id}/revisions/{revision_id} | exposed_read | document revision read |
| docs.revisions.list | docs | GET | /docs/{document_id}/revisions | exposed_read | document revision inventory read |
| docs.search | docs | GET | /docs/search | exposed_read | document full-text search over title, body, and comments |
| docs.trash | docs | POST | /docs/{document_id}/trash | exposed_write | ordinary reversible document lifecycle write |
| docs.unarchive | docs | POST | /docs/{document_id}/unarchive | exposed_write | ordinary reversible document lifecycle write |
| events.archive | events | POST | /events/{event_id}/archive | exposed_write | ordinary reversible event lifecycle write |
| events.create | events | POST | /events | exposed_write | ask withdrawal is an agent write restricted by the server to the requester's own open asks |
| events.get | events | GET | /events/{event_id} | exposed_read | event read |
| events.list | events | GET | /events | exposed_read | bounded event inventory read |
| events.restore | events | POST | /events/{event_id}/restore | exposed_write | ordinary reversible event lifecycle write |
| events.stream | events | GET | /stream/events | unsupported_streaming | SSE stream needs a bounded read adapter before MCP exposure |
| events.trash | events | POST | /events/{event_id}/trash | exposed_write | ordinary reversible event lifecycle write |
| events.unarchive | events | POST | /events/{event_id}/unarchive | exposed_write | ordinary reversible event lifecycle write |
| git.binding.export | git | LOCAL |  | unsupported_shell_shaped | Local opt-in harness/Git configuration or callback; never a remote MCP action. |
| git.disable | git | LOCAL |  | unsupported_shell_shaped | Local opt-in harness/Git configuration or callback; never a remote MCP action. |
| git.enable | git | LOCAL |  | unsupported_shell_shaped | Local opt-in harness/Git configuration or callback; never a remote MCP action. |
| git.status | git | LOCAL |  | unsupported_shell_shaped | Local opt-in harness/Git configuration or callback; never a remote MCP action. |
| git.sync | git | LOCAL |  | unsupported_shell_shaped | Local opt-in harness/Git configuration or callback; never a remote MCP action. |
| home.read | home | POST | /home/read | exposed_write | ordinary home read-marker write |
| home.unread | home | GET | /home/unread | exposed_read | home unread projection |
| hook | hook | LOCAL |  | unsupported_shell_shaped | Local opt-in harness/Git configuration or callback; never a remote MCP action. |
| hosts.bridge_status | host | POST | /hosts/bridge-status | exposed_read | bounded workspace-local host liveness and job coverage read |
| hosts.bridge.check_in | host | POST | /hosts/{host_id}/bridge/check-in | unsupported_other | bridge check-in is host-signed infrastructure traffic |
| hosts.enroll.approve | host | POST | /auth/hosts/enrollments/{enrollment_id}/approve | gated_admin | approving a host grants machine-level credentials |
| hosts.enroll.complete | host | POST | /auth/hosts/enrollments/{enrollment_id}/complete | unsupported_bootstrap_auth | host enrollment is a local machine ceremony |
| hosts.enroll.deny | host | POST | /auth/hosts/enrollments/{enrollment_id}/deny | gated_admin | enrollment decisions are auth administration |
| hosts.enroll.headless | host | POST | /auth/hosts/enrollments/headless | unsupported_bootstrap_auth | host enrollment is a local machine ceremony |
| hosts.enroll.pending | host | GET | /auth/hosts/enrollments/pending | gated_admin | pending enrollments are auth administration |
| hosts.enroll.poll | host | GET | /auth/hosts/enrollments/{enrollment_id} | unsupported_bootstrap_auth | host enrollment is a local machine ceremony |
| hosts.enroll.start | host | POST | /auth/hosts/enrollments | unsupported_bootstrap_auth | host enrollment is a local machine ceremony |
| hosts.get | host | GET | /hosts/{host_id} | exposed_read | host detail without secrets is workspace presence data |
| hosts.list | host | GET | /hosts | exposed_read | host inventory without secrets is workspace presence data |
| hosts.patch | host | PATCH | /hosts/{host_id} | gated_admin | host exclusions and names are auth administration |
| hosts.revoke | host | DELETE | /hosts/{host_id} | gated_admin | host revocation cascades to derived agent credentials |
| hosts.tokens.create | host | POST | /auth/hosts/enrollment-tokens | gated_sensitive | headless enrollment tokens are one-time secrets |
| hosts.tokens.list | host | GET | /auth/hosts/enrollment-tokens | gated_admin | enrollment token inventory is auth administration |
| hosts.tokens.revoke | host | POST | /auth/hosts/enrollment-tokens/{token_id}/revoke | gated_admin | enrollment token revocation is auth administration |
| inbox.get | inbox | GET | /inbox/{inbox_id} | exposed_read | inbox item read |
| inbox.list | inbox | GET | /inbox | exposed_read | bounded inbox inventory read |
| inbox.respond | inbox | POST | /inbox/{inbox_id}/respond | unsupported_interactive | human-only response submission requires human judgment |
| inbox.stream | inbox | GET | /stream/inbox | unsupported_streaming | SSE stream needs a bounded read adapter before MCP exposure |
| inbox.summary | inbox | GET | /inbox/summary | exposed_read | workspace-local count and bounded human asks visible to the caller |
| integrations.bind | integrations | LOCAL |  | unsupported_shell_shaped | Local opt-in harness/Git configuration or callback; never a remote MCP action. |
| integrations.disable | integrations | LOCAL |  | unsupported_shell_shaped | Local CLI harness configuration or stdin hook; never a remote MCP action. |
| integrations.enable | integrations | LOCAL |  | unsupported_shell_shaped | Local CLI harness configuration or stdin hook; never a remote MCP action. |
| integrations.list | integrations | LOCAL |  | unsupported_shell_shaped | Local CLI harness configuration or stdin hook; never a remote MCP action. |
| integrations.map | integrations | LOCAL |  | unsupported_shell_shaped | Local opt-in harness/Git configuration or callback; never a remote MCP action. |
| integrations.run | integrations | LOCAL |  | unsupported_shell_shaped | Local CLI harness configuration or stdin hook; never a remote MCP action. |
| integrations.status | integrations | LOCAL |  | unsupported_shell_shaped | Local CLI harness configuration or stdin hook; never a remote MCP action. |
| lease.acquire | lease | POST | /cards/{card_id}/lease/acquire | exposed_write | Ordinary work claim on a card the caller can write; refuses with lease_held when another owner holds it. |
| lease.release | lease | POST | /cards/{card_id}/lease/release | exposed_write | Gives up a work lease the caller's owner id holds. |
| lease.renew | lease | POST | /cards/{card_id}/lease/renew | exposed_write | Extends a work lease the caller's owner id still holds. |
| lease.show | lease | GET | /cards/{card_id}/lease | exposed_read | Point read of one card's work lease state, owner, epoch and expiry. |
| meta.commands.get | meta | GET | /meta/commands/{command_id} | exposed_read | command metadata read |
| meta.commands.list | meta | GET | /meta/commands | exposed_read | command metadata inventory read |
| meta.concepts.get | meta | GET | /meta/concepts/{concept_name} | exposed_read | concept metadata read |
| meta.concepts.list | meta | GET | /meta/concepts | exposed_read | concept metadata inventory read |
| meta.handshake | meta | GET | /meta/handshake | exposed_read | workspace capability metadata read |
| meta.health | meta | GET | /health | exposed_read | health diagnostic read |
| meta.livez | meta | GET | /livez | exposed_read | liveness diagnostic read |
| meta.readyz | meta | GET | /readyz | exposed_read | readiness diagnostic read |
| meta.version | meta | GET | /version | exposed_read | version metadata read |
| notifications.settings.get | notifications | GET | /notification-settings | unsupported_interactive | human-only notification settings |
| notifications.settings.set | notifications | PUT | /notification-settings | unsupported_interactive | human-only notification settings require human judgment |
| ops.blob.usage.rebuild | ops | POST | /ops/blob-usage/rebuild | gated_admin | blob usage rebuild is maintenance/ops |
| ops.health | ops | GET | /ops/health | gated_admin | ops health can expose operational diagnostics |
| ops.usage.summary | ops | GET | /ops/usage-summary | gated_admin | unversioned usage summary is ops/quota telemetry |
| overview.changes | overview | GET | /overview/changes | exposed_read | Principal-scoped bounded visit digest; reads without advancing the baseline. |
| overview.get | overview | GET | /overview | exposed_read | Executive Overview projection shared with the web UI and CLI. |
| plan.set | plan | PUT | /cards/{card_id}/plan | exposed_write | Replace a local initiative plan with a card concurrency token; no upstream source writes. |
| plan.show | plan | GET | /cards/{card_id}/plan | exposed_read | Read a principal-scoped initiative plan and computed state. |
| pm.actions.acknowledge | pm | POST | /pm/actions/{action_id}/acknowledge | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.actions.get | pm | GET | /pm/actions/{action_id} | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| pm.actions.list | pm | GET | /pm/actions | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| pm.actions.reconcile | pm | POST | /pm/actions/{action_id}/reconcile | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.bindings.create | pm | POST | /pm/bindings | gated_admin | Binds an external channel user to a workspace principal; requires explicit human administration. |
| pm.bindings.list | pm | GET | /pm/bindings | exposed_read | Read which exact channel identities are bound to workspace principals; an operator check that sends nothing. |
| pm.connect | pm | POST | /pm/connect | unsupported_bootstrap_auth | Local runner bootstrap is owned by anx pm serve; model tools must not claim workspace PM ownership. |
| pm.context | pm | GET | /pm/context | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| pm.conversations.create | pm | POST | /pm/conversations | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.conversations.get | pm | GET | /pm/conversations/{conversation_id} | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| pm.conversations.list | pm | GET | /pm/conversations | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| pm.conversations.messages.create | pm | POST | /pm/conversations/{conversation_id}/messages | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.decisions.answer | pm | POST | /pm/decisions/{decision_id}/answer | gated_sensitive | Human approval or consequential source handoff; explicit exposure never bypasses core authorization. |
| pm.decisions.create | pm | POST | /pm/decisions | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.decisions.dispatch | pm | POST | /pm/decisions/{decision_id}/dispatch | gated_sensitive | Human approval or consequential source handoff; explicit exposure never bypasses core authorization. |
| pm.decisions.get | pm | GET | /pm/decisions/{decision_id} | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| pm.decisions.list | pm | GET | /pm/decisions | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| pm.disconnect | pm | POST | /pm/disconnect | unsupported_bootstrap_auth | Explicit local PM uninstall resets workspace onboarding; model tools must not remove PM registration. |
| pm.presence | pm | GET | /pm/presence | exposed_read | Read computed workspace PM connection state, bounded display labels and timestamps without turn data or commands. |
| pm.turns.claim | pm | POST | /pm/turns/claim | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.turns.complete | pm | POST | /pm/turns/{turn_id}/complete | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.turns.context | pm | POST | /pm/turns/{turn_id}/context | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| pm.turns.decisions.create | pm | POST | /pm/turns/{turn_id}/decisions | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.turns.fail | pm | POST | /pm/turns/{turn_id}/fail | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.turns.get | pm | GET | /pm/turns/{turn_id} | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| pm.turns.heartbeat | pm | POST | /pm/turns/{turn_id}/heartbeat | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| pm.turns.release | pm | POST | /pm/turns/{turn_id}/release | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| ref_edges.list | ref-edges | GET | /ref-edges | exposed_read | reference edge inventory read |
| refs.resolve | refs | POST | /refs/resolve | exposed_read | Read-only batch ref previews; core preserves unknown refs and enforces per-resource visibility. |
| report.preview | report | POST | /reports/preview | exposed_read | bounded live materialization of an unsaved report without persistence |
| report.render | report | GET | /docs/{document_id}/report | exposed_read | authorized live dashboard projection read |
| runs.get | runs | GET | /runs/{run_id} | exposed_read | run reports are workspace presence data |
| runs.list | runs | GET | /runs | exposed_read | run reports are workspace presence data |
| runs.upsert | runs | POST | /runs | unsupported_other | runs are reported by local launchers through anx runs ingest, not by MCP clients |
| secrets.create | secret | POST | /secrets | gated_sensitive | secret payload write is sensitive |
| secrets.delete | secret | DELETE | /secrets/{secret_id} | gated_sensitive | secret deletion is destructive |
| secrets.list | secret | GET | /secrets | gated_admin | secret inventory is administrative |
| secrets.reveal | secret | POST | /secrets/{secret_id}/reveal | gated_sensitive | secret value reveal is sensitive |
| secrets.reveal-batch | secret | POST | /secrets/reveal-batch | gated_sensitive | secret value reveal is sensitive |
| secrets.update | secret | PUT | /secrets/{secret_id} | gated_sensitive | secret payload write is sensitive |
| series.list | series | GET | /series | exposed_read | Read declared numeric/state series metadata and source provenance. |
| series.push | series | POST | /series/{name}/points | unsupported_shell_shaped | Use the enrolled host CLI for scoped token exchange and optional source command execution. |
| series.query | series | GET | /series/{name}/query | exposed_read | Read range/step-bounded series observations with explicit label filters and aggregation. |
| series.show | series | GET | /series/{name} | exposed_read | Read bounded series observations and freshness. |
| sessions.get | sessions | GET | /sessions/{session_id} | exposed_read | Read only the authenticated agent's private session metadata; no transcript access or other task links. |
| sessions.register | sessions | POST | /sessions | exposed_write | Register sequence-fenced metadata under an existing agent principal; never creates credentials or controls execution. |
| sync.disable | sync | LOCAL |  | unsupported_shell_shaped | Local directory and host-job configuration; never a remote MCP action. |
| sync.enable | sync | LOCAL |  | unsupported_shell_shaped | Local directory and host-job configuration; never a remote MCP action. |
| sync.status | sync | LOCAL |  | unsupported_shell_shaped | Local directory and host-job configuration; never a remote MCP action. |
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
| work.capabilities | work | GET | /work/capabilities | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| work.create | work | POST | /work | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| work.fold | work | POST | /work/{card_ref}/fold | exposed_write | Transfer references and archive with a reversible scoped snapshot. |
| work.get | work | GET | /work/{card_ref} | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| work.group | work | POST | /work/{card_ref}/group | exposed_write | Add an ordinary plan step atomically with replay safety. |
| work.list | work | GET | /work | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| work.observations.list | work | GET | /work/{card_ref}/observations | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| work.observations.submit | work | POST | /work/{card_ref}/observations | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| work.participants.list | work | GET | /work/{card_ref}/participants | exposed_read | Read task-scoped participation after core privacy checks; other agents' session identifiers remain private. |
| work.participants.register | work | POST | /work/{card_ref}/participants | exposed_write | Record sequence-fenced nonlocking participation for the caller's session; no task assignment, state change or completion. |
| work.patch | work | PATCH | /work/{card_ref} | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| work.refresh.get | work | GET | /work/{card_ref}/refresh | exposed_read | Read scoped central work or PM context and preserve evidence, freshness, pagination and receipt uncertainty. |
| work.refresh.request | work | POST | /work/{card_ref}/refresh | exposed_write | Authenticated durable request; core enforces scope, selected-agent identity, replay protection and evidence authority. |
| work.unfold | work | POST | /work/{card_ref}/unfold | exposed_write | Reverse a fold without overwriting conflicting user edits. |
| work.ungrouped | work | GET | /work/ungrouped | exposed_read | Bounded visible recent cards with deterministic likely homes. |
| workspace.dashboard.list | workspace | GET | /workspace/dashboard/reports | exposed_read | Validated dashboard report selector candidates. |
| workspace.dashboard.pins.list | workspace | GET | /workspace/dashboard/pins | exposed_read | Read only the requesting principal's ordered dashboard pins. |
| workspace.dashboard.pins.set | workspace | PUT | /workspace/dashboard/pins | exposed_write | Reversibly replace only the requesting principal's dashboard pins. |
| workspace.dashboard.set | workspace | PUT | /workspace/dashboard | exposed_write | Reversible workspace dashboard document pin. |
