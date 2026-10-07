export const commandRegistry = [
    {
        "command_id": "actors.create",
        "cli_path": "actors create",
        "group": "actors",
        "method": "POST",
        "path": "/actors",
        "operation_id": "createActor",
        "summary": "Create actor (dev fixture)",
        "why": "Dev-only actor registration when dev_actor_mode is enabled.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ actor }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "dev_actor_mode_disabled"
        ],
        "concepts": [
            "actors",
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "actors.list"
        ],
        "go_method": "ActorsCreate",
        "ts_method": "actorsCreate"
    },
    {
        "command_id": "actors.list",
        "cli_path": "actors list",
        "group": "actors",
        "method": "GET",
        "path": "/actors",
        "operation_id": "listActors",
        "summary": "List actors",
        "why": "Enumerate durable actor records for operator UI and dev fixtures.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ actors, next_cursor? }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "actors",
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "actors.create"
        ],
        "go_method": "ActorsList",
        "ts_method": "actorsList"
    },
    {
        "command_id": "adapters.declare",
        "cli_path": "adapters declare",
        "group": "adapters",
        "method": "POST",
        "path": "/adapters",
        "operation_id": "adapters_declare",
        "summary": "Declare an adapter and its series",
        "why": "Declare an adapter and its series.",
        "input_mode": "file-and-body",
        "http_input_mode": "json-body",
        "cli_input": {
            "mode": "file-and-body",
            "flags": [
                {
                    "name": "body-file",
                    "required": true,
                    "description": "UTF-8 adapter declaration JSON file or stdin (-)."
                }
            ]
        },
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns JSON with provenance and explicit freshness.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "series_capacity"
        ],
        "concepts": [
            "documents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Explicit declared push source; core never fetches external data.",
        "examples": [
            {
                "title": "Declare before pushing",
                "command": "anx adapters declare --body-file adapter.json"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "agent_id",
                    "type": "string"
                },
                {
                    "name": "description",
                    "type": "string"
                },
                {
                    "name": "expected_interval",
                    "type": "string"
                },
                {
                    "name": "name",
                    "type": "string"
                },
                {
                    "name": "series",
                    "type": "list\u003cany\u003e"
                }
            ]
        },
        "adjacent_commands": [
            "adapters.delete",
            "adapters.list",
            "adapters.revoke",
            "adapters.token"
        ],
        "go_method": "AdaptersDeclare",
        "ts_method": "adaptersDeclare"
    },
    {
        "command_id": "adapters.delete",
        "cli_path": "adapters delete",
        "group": "adapters",
        "method": "DELETE",
        "path": "/adapters/{name}",
        "operation_id": "adapters_delete",
        "summary": "Delete an adapter and its series",
        "why": "Delete an adapter and its series.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns JSON with provenance and explicit freshness.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "series_capacity"
        ],
        "concepts": [
            "documents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Explicit declared push source; core never fetches external data.",
        "path_params": [
            "name"
        ],
        "adjacent_commands": [
            "adapters.declare",
            "adapters.list",
            "adapters.revoke",
            "adapters.token"
        ],
        "go_method": "AdaptersDelete",
        "ts_method": "adaptersDelete"
    },
    {
        "command_id": "adapters.list",
        "cli_path": "adapters list",
        "group": "adapters",
        "method": "GET",
        "path": "/adapters",
        "operation_id": "adapters_list",
        "summary": "List declared adapters",
        "why": "List declared adapters.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns JSON with provenance and explicit freshness.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "series_capacity"
        ],
        "concepts": [
            "documents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Explicit declared push source; core never fetches external data.",
        "adjacent_commands": [
            "adapters.declare",
            "adapters.delete",
            "adapters.revoke",
            "adapters.token"
        ],
        "go_method": "AdaptersList",
        "ts_method": "adaptersList"
    },
    {
        "command_id": "adapters.revoke",
        "cli_path": "adapters revoke",
        "group": "adapters",
        "method": "POST",
        "path": "/adapters/{name}/revoke",
        "operation_id": "adapters_revoke",
        "summary": "Revoke an adapter grant",
        "why": "Revoke an adapter grant.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns JSON with provenance and explicit freshness.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "series_capacity"
        ],
        "concepts": [
            "documents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Explicit declared push source; core never fetches external data.",
        "path_params": [
            "name"
        ],
        "adjacent_commands": [
            "adapters.declare",
            "adapters.delete",
            "adapters.list",
            "adapters.token"
        ],
        "go_method": "AdaptersRevoke",
        "ts_method": "adaptersRevoke"
    },
    {
        "command_id": "adapters.token",
        "cli_path": "adapters token",
        "group": "adapters",
        "method": "POST",
        "path": "/adapters/{name}/token",
        "operation_id": "adapters_token",
        "summary": "Exchange owner identity for a scoped series-write token",
        "why": "Exchange owner identity for a scoped series-write token.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns JSON with provenance and explicit freshness.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "series_capacity"
        ],
        "concepts": [
            "documents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Explicit declared push source; core never fetches external data.",
        "path_params": [
            "name"
        ],
        "adjacent_commands": [
            "adapters.declare",
            "adapters.delete",
            "adapters.list",
            "adapters.revoke"
        ],
        "go_method": "AdaptersToken",
        "ts_method": "adaptersToken"
    },
    {
        "command_id": "agent.inbox.answers.read",
        "cli_path": "agent inbox answers read",
        "group": "agent",
        "method": "POST",
        "path": "/agent-inbox/answers/read",
        "operation_id": "markAgentInboxAnswerRead",
        "summary": "Mark one of the authenticated agent's answers read",
        "why": "Persist answer-level read state independently from a wake notification batch.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ answer }` with the per-answer read state.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "agents",
            "inbox",
            "write"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Only the requesting agent can mark one of its own response events read. This state is independent from wake notification read state.",
        "body_schema": {
            "required": [
                {
                    "name": "answer_event_id",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "agent.inbox.asks.list",
            "agent.notifications.dismiss",
            "agent.notifications.list",
            "agent.notifications.read"
        ],
        "go_method": "AgentInboxAnswersRead",
        "ts_method": "agentInboxAnswersRead"
    },
    {
        "command_id": "agent.inbox.asks.list",
        "cli_path": "agent inbox asks list",
        "group": "agent",
        "method": "GET",
        "path": "/agent-inbox/asks",
        "operation_id": "listAgentInboxAsks",
        "summary": "List authenticated agent's human attention asks",
        "why": "Requester-scoped projection of open and answered asks, including per-answer read state.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ items, page_info }` with keyset pagination.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "agents",
            "inbox"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Results are scoped to the authenticated requester and keyset-paginated; do not infer open state from a partial event timeline.",
        "adjacent_commands": [
            "agent.inbox.answers.read",
            "agent.notifications.dismiss",
            "agent.notifications.list",
            "agent.notifications.read"
        ],
        "go_method": "AgentInboxAsksList",
        "ts_method": "agentInboxAsksList"
    },
    {
        "command_id": "agent.notification-receipts.stream",
        "cli_path": "",
        "method": "GET",
        "path": "/stream/agent-notification-receipts",
        "operation_id": "streamAgentNotificationReceipts",
        "summary": "Stream agent notification receipts (SSE)",
        "why": "Server-sent events feed of sender/operator-visible agent wake receipt updates for a backing thread.",
        "input_mode": "query",
        "streaming": {
            "mode": "sse"
        },
        "output_envelope": "SSE `notification_receipt` events with JSON payloads `{ \"receipt\": \u003cAgentNotificationReceipt\u003e }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "agents",
            "notifications"
        ],
        "stability": "beta",
        "surface": "projection",
        "go_method": "AgentNotificationReceiptsStream",
        "ts_method": "agentNotificationReceiptsStream"
    },
    {
        "command_id": "agent.notifications.dismiss",
        "cli_path": "agent notifications dismiss",
        "group": "agent",
        "method": "POST",
        "path": "/agent-notifications/dismiss",
        "operation_id": "dismissAgentNotification",
        "summary": "Dismiss agent notification",
        "why": "Dismiss a wake notification for the authenticated agent.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ event, notification }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "agents",
            "notifications",
            "write"
        ],
        "stability": "beta",
        "surface": "projection",
        "adjacent_commands": [
            "agent.inbox.answers.read",
            "agent.inbox.asks.list",
            "agent.notifications.list",
            "agent.notifications.read"
        ],
        "go_method": "AgentNotificationsDismiss",
        "ts_method": "agentNotificationsDismiss"
    },
    {
        "command_id": "agent.notifications.list",
        "cli_path": "agent notifications list",
        "group": "agent",
        "method": "GET",
        "path": "/agent-notifications",
        "operation_id": "listAgentNotifications",
        "summary": "List agent wake notifications",
        "why": "Derived read of notifications for the authenticated agent.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ items, generated_at }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "agents",
            "notifications"
        ],
        "stability": "beta",
        "surface": "projection",
        "adjacent_commands": [
            "agent.inbox.answers.read",
            "agent.inbox.asks.list",
            "agent.notifications.dismiss",
            "agent.notifications.read"
        ],
        "go_method": "AgentNotificationsList",
        "ts_method": "agentNotificationsList"
    },
    {
        "command_id": "agent.notifications.read",
        "cli_path": "agent notifications read",
        "group": "agent",
        "method": "POST",
        "path": "/agent-notifications/read",
        "operation_id": "markAgentNotificationRead",
        "summary": "Mark agent notification read",
        "why": "Record read state for a wake notification.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ event, notification }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "agents",
            "notifications",
            "write"
        ],
        "stability": "beta",
        "surface": "projection",
        "adjacent_commands": [
            "agent.inbox.answers.read",
            "agent.inbox.asks.list",
            "agent.notifications.dismiss",
            "agent.notifications.list"
        ],
        "go_method": "AgentNotificationsRead",
        "ts_method": "agentNotificationsRead"
    },
    {
        "command_id": "agents.get",
        "cli_path": "agents get",
        "group": "agents",
        "method": "GET",
        "path": "/agents/{agent_id}",
        "operation_id": "getAgent",
        "summary": "Get agent state and recent work",
        "description": "Any authenticated workspace principal. Agent ID or handle; includes current/recent cards, recent runs, open asks and progress notes.",
        "why": "Inspect one agent's state and recent activity.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ agent, recent_cards, recent_runs, open_asks, recent_notes }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "agents",
            "runs",
            "cards",
            "inbox"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "path_params": [
            "agent_id"
        ],
        "adjacent_commands": [
            "agents.list",
            "agents.me.get",
            "agents.stream"
        ],
        "go_method": "AgentsGet",
        "ts_method": "agentsGet"
    },
    {
        "command_id": "agents.list",
        "cli_path": "agents list",
        "group": "agents",
        "method": "GET",
        "path": "/agents",
        "operation_id": "listAgents",
        "summary": "List agent roster",
        "description": "Any authenticated workspace principal. Includes derived and adopted agents; a legacy excluded standalone principal is marked standalone until revoked. State precedence is waiting_on_human, working, idle, stale. Waiting means an open ask/review/escalation requested by this agent. Working means an alive nonterminal run or presence refreshed within 30 minutes. Stale means no run, presence, write, or bridge signal within 24 hours. Bridge online is separate from state.",
        "why": "See who is working or waiting.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ agents }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "agents",
            "runs",
            "inbox"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "adjacent_commands": [
            "agents.get",
            "agents.me.get",
            "agents.stream"
        ],
        "go_method": "AgentsList",
        "ts_method": "agentsList"
    },
    {
        "command_id": "agents.me.get",
        "cli_path": "agents me",
        "group": "agents",
        "method": "GET",
        "path": "/agents/me",
        "operation_id": "getCurrentAgent",
        "summary": "Get current authenticated principal",
        "why": "Resolve any workspace bearer to its principal and durable actor identity, including host-derived agent details when present.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ agent: PrincipalSelf }` for a human, derived/adopted agent, or standalone agent. The agent key is retained for existing clients.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "auth",
            "agents"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "agents.get",
            "agents.list",
            "agents.stream"
        ],
        "go_method": "AgentsMeGet",
        "ts_method": "agentsMeGet"
    },
    {
        "command_id": "agents.me.presence",
        "cli_path": "work presence",
        "group": "work",
        "method": "PATCH",
        "path": "/agents/me/presence",
        "operation_id": "patchAgentPresence",
        "summary": "Set current agent presence",
        "description": "Derived-agent bearer only. Each write refreshes presence time. Explicit null current_card_ref clears the card; omitted field preserves it. Note is an optional short progress line; explicit null clears it. Presence expires from working after 30 minutes but remains last note for roster display.",
        "why": "Report current card and progress.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ presence }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "run_attribution_invalid"
        ],
        "concepts": [
            "agents",
            "cards",
            "runs"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "body_schema": {
            "optional": [
                {
                    "name": "current_card_ref",
                    "type": "string"
                },
                {
                    "name": "note",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.get",
            "work.list",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.list",
            "work.participants.register",
            "work.patch",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "AgentsMePresence",
        "ts_method": "agentsMePresence"
    },
    {
        "command_id": "agents.stream",
        "cli_path": "agents stream",
        "group": "agents",
        "method": "GET",
        "path": "/stream/agents",
        "operation_id": "streamAgentChanges",
        "summary": "Stream ephemeral agent roster changes (SSE)",
        "description": "Authenticated workspace principals receive an initial `agents_changed` notification and subsequent notifications after run upserts, presence writes, and host identity or bridge changes. The signal is process-local and not a canonical workspace event: it is not written to the event log, has no replay cursor, and may be coalesced. Reconnect and refetch `GET /agents` after every notification. Keep the existing visibility/focus refresh as a fallback across reconnects or server restarts.",
        "why": "Prompt clients to refresh the agent roster without writing presence telemetry to workspace events.",
        "input_mode": "none",
        "streaming": {
            "mode": "sse"
        },
        "output_envelope": "SSE `agents_changed` messages with JSON `{ \"revision\": \u003cinteger\u003e }`; no agent data or event-log cursor.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "agents"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "This is an ephemeral invalidation stream. Refetch the roster on connect, notification, and reconnect; do not treat revisions as durable event IDs.",
        "adjacent_commands": [
            "agents.get",
            "agents.list",
            "agents.me.get"
        ],
        "go_method": "AgentsStream",
        "ts_method": "agentsStream"
    },
    {
        "command_id": "artifacts.archive",
        "cli_path": "artifacts archive",
        "group": "artifacts",
        "method": "POST",
        "path": "/artifacts/{artifact_id}/archive",
        "operation_id": "archiveArtifact",
        "summary": "Archive artifact",
        "why": "Set archived_at on artifact metadata (orthogonal to trash lifecycle).",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ artifact }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "artifacts",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "artifact_id"
        ],
        "adjacent_commands": [
            "artifacts.attachments.create",
            "artifacts.content",
            "artifacts.create",
            "artifacts.get",
            "artifacts.list",
            "artifacts.purge",
            "artifacts.restore",
            "artifacts.trash",
            "artifacts.unarchive"
        ],
        "go_method": "ArtifactsArchive",
        "ts_method": "artifactsArchive"
    },
    {
        "command_id": "artifacts.attachments.create",
        "cli_path": "artifacts attachments create",
        "group": "artifacts",
        "method": "POST",
        "path": "/artifacts/attachments",
        "operation_id": "createArtifactAttachment",
        "summary": "Upload a file attachment",
        "why": "Create kind=attachment via multipart form (efficient binary upload; previews use GET /artifacts/{artifact_id}/content, where the path segment accepts artifact ref or handle).",
        "input_mode": "multipart-form",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ artifact }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "conflict",
            "unsupported_mime",
            "payload_too_large"
        ],
        "concepts": [
            "artifacts",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Multipart/form-data upload; generated HTTP Invoke helpers JSON-encode bodies and are not suitable—use UI, curl -F, or a multipart-aware client.",
        "adjacent_commands": [
            "artifacts.archive",
            "artifacts.content",
            "artifacts.create",
            "artifacts.get",
            "artifacts.list",
            "artifacts.purge",
            "artifacts.restore",
            "artifacts.trash",
            "artifacts.unarchive"
        ],
        "go_method": "ArtifactsAttachmentsCreate",
        "ts_method": "artifactsAttachmentsCreate"
    },
    {
        "command_id": "artifacts.content",
        "cli_path": "artifacts content",
        "group": "artifacts",
        "method": "GET",
        "path": "/artifacts/{artifact_id}/content",
        "operation_id": "getArtifactContent",
        "summary": "Download artifact bytes",
        "why": "Return raw artifact bytes with accurate Content-Type, Content-Disposition, ETag, and Last-Modified for attachments.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Raw bytes or JSON/text depending on artifact.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "artifacts"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "artifact_id"
        ],
        "adjacent_commands": [
            "artifacts.archive",
            "artifacts.attachments.create",
            "artifacts.create",
            "artifacts.get",
            "artifacts.list",
            "artifacts.purge",
            "artifacts.restore",
            "artifacts.trash",
            "artifacts.unarchive"
        ],
        "go_method": "ArtifactsContent",
        "ts_method": "artifactsContent"
    },
    {
        "command_id": "artifacts.create",
        "cli_path": "artifacts create",
        "group": "artifacts",
        "method": "POST",
        "path": "/artifacts",
        "operation_id": "createArtifact",
        "summary": "Create artifact",
        "why": "Store content-addressed artifact metadata and payload (bytes, text, or structured JSON).",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ artifact }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "conflict"
        ],
        "concepts": [
            "artifacts",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "artifact",
                    "type": "object"
                },
                {
                    "name": "content_type",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "content",
                    "type": "any"
                }
            ]
        },
        "adjacent_commands": [
            "artifacts.archive",
            "artifacts.attachments.create",
            "artifacts.content",
            "artifacts.get",
            "artifacts.list",
            "artifacts.purge",
            "artifacts.restore",
            "artifacts.trash",
            "artifacts.unarchive"
        ],
        "go_method": "ArtifactsCreate",
        "ts_method": "artifactsCreate"
    },
    {
        "command_id": "artifacts.get",
        "cli_path": "artifacts get",
        "group": "artifacts",
        "method": "GET",
        "path": "/artifacts/{artifact_id}",
        "operation_id": "getArtifact",
        "summary": "Get artifact metadata",
        "why": "Resolve immutable artifact metadata referenced from timelines and resources.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ artifact }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "artifacts"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "artifact_id"
        ],
        "adjacent_commands": [
            "artifacts.archive",
            "artifacts.attachments.create",
            "artifacts.content",
            "artifacts.create",
            "artifacts.list",
            "artifacts.purge",
            "artifacts.restore",
            "artifacts.trash",
            "artifacts.unarchive"
        ],
        "go_method": "ArtifactsGet",
        "ts_method": "artifactsGet"
    },
    {
        "command_id": "artifacts.list",
        "cli_path": "artifacts list",
        "group": "artifacts",
        "method": "GET",
        "path": "/artifacts",
        "operation_id": "listArtifacts",
        "summary": "List artifacts",
        "why": "Search and filter immutable artifacts across the workspace.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ artifacts }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "artifacts"
        ],
        "stability": "beta",
        "surface": "canonical",
        "adjacent_commands": [
            "artifacts.archive",
            "artifacts.attachments.create",
            "artifacts.content",
            "artifacts.create",
            "artifacts.get",
            "artifacts.purge",
            "artifacts.restore",
            "artifacts.trash",
            "artifacts.unarchive"
        ],
        "go_method": "ArtifactsList",
        "ts_method": "artifactsList"
    },
    {
        "command_id": "artifacts.purge",
        "cli_path": "artifacts purge",
        "group": "artifacts",
        "method": "POST",
        "path": "/artifacts/{artifact_id}/purge",
        "operation_id": "purgeArtifact",
        "summary": "Permanently delete trashed artifact",
        "why": "Permanently delete a trashed artifact (human-gated).",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ purged, artifact_ref, artifact_handle }`; internal artifact_id may appear for admin/debug compatibility.",
        "error_codes": [
            "auth_required",
            "human_only",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "artifacts",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "artifact_id"
        ],
        "adjacent_commands": [
            "artifacts.archive",
            "artifacts.attachments.create",
            "artifacts.content",
            "artifacts.create",
            "artifacts.get",
            "artifacts.list",
            "artifacts.restore",
            "artifacts.trash",
            "artifacts.unarchive"
        ],
        "go_method": "ArtifactsPurge",
        "ts_method": "artifactsPurge"
    },
    {
        "command_id": "artifacts.restore",
        "cli_path": "artifacts restore",
        "group": "artifacts",
        "method": "POST",
        "path": "/artifacts/{artifact_id}/restore",
        "operation_id": "restoreArtifact",
        "summary": "Restore artifact from trash",
        "why": "Clear trash lifecycle fields on an artifact after an explicit restore action.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ artifact }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "artifacts",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "artifact_id"
        ],
        "adjacent_commands": [
            "artifacts.archive",
            "artifacts.attachments.create",
            "artifacts.content",
            "artifacts.create",
            "artifacts.get",
            "artifacts.list",
            "artifacts.purge",
            "artifacts.trash",
            "artifacts.unarchive"
        ],
        "go_method": "ArtifactsRestore",
        "ts_method": "artifactsRestore"
    },
    {
        "command_id": "artifacts.trash",
        "cli_path": "artifacts trash",
        "group": "artifacts",
        "method": "POST",
        "path": "/artifacts/{artifact_id}/trash",
        "operation_id": "trashArtifact",
        "summary": "Move artifact to trash",
        "why": "Move artifact metadata to trash with an explicit operator reason.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ artifact }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "artifacts",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "reason",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "artifact_id"
        ],
        "adjacent_commands": [
            "artifacts.archive",
            "artifacts.attachments.create",
            "artifacts.content",
            "artifacts.create",
            "artifacts.get",
            "artifacts.list",
            "artifacts.purge",
            "artifacts.restore",
            "artifacts.unarchive"
        ],
        "go_method": "ArtifactsTrash",
        "ts_method": "artifactsTrash"
    },
    {
        "command_id": "artifacts.unarchive",
        "cli_path": "artifacts unarchive",
        "group": "artifacts",
        "method": "POST",
        "path": "/artifacts/{artifact_id}/unarchive",
        "operation_id": "unarchiveArtifact",
        "summary": "Unarchive artifact",
        "why": "Clear archived_at on artifact metadata.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ artifact }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "artifacts",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "artifact_id"
        ],
        "adjacent_commands": [
            "artifacts.archive",
            "artifacts.attachments.create",
            "artifacts.content",
            "artifacts.create",
            "artifacts.get",
            "artifacts.list",
            "artifacts.purge",
            "artifacts.restore",
            "artifacts.trash"
        ],
        "go_method": "ArtifactsUnarchive",
        "ts_method": "artifactsUnarchive"
    },
    {
        "command_id": "auth.access-requests.approve",
        "cli_path": "auth access-requests approve",
        "group": "auth",
        "method": "POST",
        "path": "/auth/access-requests/{request_id}/approve",
        "operation_id": "approveAccessRequest",
        "summary": "Approve an access request",
        "description": "Human only. Atomically grants auth-admin using the existing grant semantics and records an approved Inbox response. Same decision retries are idempotent; opposite decisions conflict. Revoked requesters cannot be approved.",
        "why": "Request and review explicit workspace authority.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `AccessRequestResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "agent_required",
            "human_required",
            "invalid_request",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Agents request their own grant. Humans alone decide requests and read the Access queue.",
        "path_params": [
            "request_id"
        ],
        "adjacent_commands": [
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthAccessRequestsApprove",
        "ts_method": "authAccessRequestsApprove"
    },
    {
        "command_id": "auth.access-requests.deny",
        "cli_path": "auth access-requests deny",
        "group": "auth",
        "method": "POST",
        "path": "/auth/access-requests/{request_id}/deny",
        "operation_id": "denyAccessRequest",
        "summary": "Deny an access request",
        "description": "Human only. Records a denied request and rejected Inbox response without granting authority. Same decision retries are idempotent; opposite decisions conflict.",
        "why": "Request and review explicit workspace authority.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `AccessRequestResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "agent_required",
            "human_required",
            "invalid_request",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Agents request their own grant. Humans alone decide requests and read the Access queue.",
        "path_params": [
            "request_id"
        ],
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthAccessRequestsDeny",
        "ts_method": "authAccessRequestsDeny"
    },
    {
        "command_id": "auth.access-requests.list",
        "cli_path": "auth access-requests list",
        "group": "auth",
        "method": "GET",
        "path": "/auth/access-requests",
        "operation_id": "listAccessRequests",
        "summary": "List pending access requests",
        "description": "Human only. Lists pending requests in creation order, including requester identity, grant, reason and Inbox correlation.",
        "why": "Request and review explicit workspace authority.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `AccessRequestsResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "agent_required",
            "human_required",
            "invalid_request",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Agents request their own grant. Humans alone decide requests and read the Access queue.",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthAccessRequestsList",
        "ts_method": "authAccessRequestsList"
    },
    {
        "command_id": "auth.access-requests.request",
        "cli_path": "auth access-requests request",
        "group": "auth",
        "method": "POST",
        "path": "/auth/access-requests",
        "operation_id": "createAccessRequest",
        "summary": "Request a named grant",
        "description": "Agent only. Requests auth-admin for the authenticated principal, with a nonempty reason. Idempotent per principal and grant for the lifetime of the request; retries return the original request without changing its reason or decision. Creates a review Inbox item.",
        "why": "Request and review explicit workspace authority.",
        "input_mode": "flags",
        "http_input_mode": "json-body",
        "cli_input": {
            "mode": "flags",
            "flags": [
                {
                    "name": "grant",
                    "body_path": "grant",
                    "required": true,
                    "description": "Named grant; currently auth-admin."
                },
                {
                    "name": "reason",
                    "body_path": "reason",
                    "required": true,
                    "description": "Why this agent needs the grant."
                }
            ]
        },
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `AccessRequestResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "agent_required",
            "human_required",
            "invalid_request",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Agents request their own grant. Humans alone decide requests and read the Access queue.",
        "body_schema": {
            "required": [
                {
                    "name": "grant",
                    "type": "string",
                    "enum_values": [
                        "auth-admin"
                    ]
                },
                {
                    "name": "reason",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthAccessRequestsRequest",
        "ts_method": "authAccessRequestsRequest"
    },
    {
        "command_id": "auth.access-requests.summary",
        "cli_path": "auth access-requests summary",
        "group": "auth",
        "method": "GET",
        "path": "/auth/access/summary",
        "operation_id": "getAccessSummary",
        "summary": "Count pending access actions",
        "description": "Human only. Cheap pending count covering pending access requests and unexpired pending or approved host enrollments awaiting completion.",
        "why": "Request and review explicit workspace authority.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `AccessSummary`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "agent_required",
            "human_required",
            "invalid_request",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Agents request their own grant. Humans alone decide requests and read the Access queue.",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthAccessRequestsSummary",
        "ts_method": "authAccessRequestsSummary"
    },
    {
        "command_id": "auth.admins.grant",
        "cli_path": "auth admins grant",
        "group": "auth",
        "method": "POST",
        "path": "/auth/admins/{principal_id}/grant",
        "operation_id": "grantAuthAdmin",
        "summary": "Grant agent auth-admin grants",
        "description": "Human only. Grant or revoke fleet administration on an active agent principal. Agents may decide enrollments, manage enrollment tokens, revoke other hosts, and read inventory/audit; principal and human invite revocation remain human-only. Idempotent, audited when changed, and effective on its next request even with an existing access token.",
        "why": "Manage explicit workspace administration authority for agents.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `AuthAdminResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "human_required",
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Only a human can change a grant. No default grant is assigned to agents or hosts.",
        "path_params": [
            "principal_id"
        ],
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthAdminsGrant",
        "ts_method": "authAdminsGrant"
    },
    {
        "command_id": "auth.admins.list",
        "cli_path": "auth admins list",
        "group": "auth",
        "method": "GET",
        "path": "/auth/admins",
        "operation_id": "listAuthAdmins",
        "summary": "List agent auth-admin grants",
        "description": "Human or auth-admin principal. Lists active explicitly granted agents only, ordered by username and id in pages of at most 200; humans retain their existing administration rights.",
        "why": "Manage explicit workspace administration authority for agents.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `AuthAdminsResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "auth_admin_required",
            "invalid_request"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Only a human can change a grant. No default grant is assigned to agents or hosts.",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthAdminsList",
        "ts_method": "authAdminsList"
    },
    {
        "command_id": "auth.admins.revoke",
        "cli_path": "auth admins revoke",
        "group": "auth",
        "method": "POST",
        "path": "/auth/admins/{principal_id}/revoke",
        "operation_id": "revokeAuthAdmin",
        "summary": "Revoke agent auth-admin grants",
        "description": "Human only. Grant or revoke fleet administration on an active agent principal. Agents may decide enrollments, manage enrollment tokens, revoke other hosts, and read inventory/audit; principal and human invite revocation remain human-only. Idempotent, audited when changed, and effective on its next request even with an existing access token.",
        "why": "Manage explicit workspace administration authority for agents.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `AuthAdminResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "human_required",
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Only a human can change a grant. No default grant is assigned to agents or hosts.",
        "path_params": [
            "principal_id"
        ],
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthAdminsRevoke",
        "ts_method": "authAdminsRevoke"
    },
    {
        "command_id": "auth.audit.list",
        "cli_path": "auth audit list",
        "group": "auth",
        "method": "GET",
        "path": "/auth/audit",
        "operation_id": "listAuthAudit",
        "summary": "List auth audit entries",
        "why": "Operator audit trail for auth-sensitive actions.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns audit list JSON.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "auth",
            "audit"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthAuditList",
        "ts_method": "authAuditList"
    },
    {
        "command_id": "auth.bootstrap.status",
        "cli_path": "auth bootstrap status",
        "group": "auth",
        "method": "GET",
        "path": "/auth/bootstrap/status",
        "operation_id": "getAuthBootstrapStatus",
        "summary": "Bootstrap registration availability",
        "why": "Report whether first-human passkey bootstrap registration is still available; hosts cannot bootstrap.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ bootstrap_registration_available, dev_passkey_bypass_available? }`, where the dev bypass field reflects the effective local-only passkey bypass capability.",
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthBootstrapStatus",
        "ts_method": "authBootstrapStatus"
    },
    {
        "command_id": "auth.invites.create",
        "cli_path": "auth invites create",
        "group": "auth",
        "method": "POST",
        "path": "/auth/invites",
        "operation_id": "createAuthInvite",
        "summary": "Create invite token",
        "description": "Human only. Auth-admin agents cannot issue human identity or credential invitations.",
        "why": "Issue a one-time invite for a human principal; kind must be human.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ invite, token }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "human_required"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "body_schema": {
            "required": [
                {
                    "name": "kind",
                    "type": "string",
                    "enum_values": [
                        "human"
                    ]
                }
            ],
            "optional": [
                {
                    "name": "expires_at",
                    "type": "datetime"
                }
            ]
        },
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthInvitesCreate",
        "ts_method": "authInvitesCreate"
    },
    {
        "command_id": "auth.invites.list",
        "cli_path": "auth invites list",
        "group": "auth",
        "method": "GET",
        "path": "/auth/invites",
        "operation_id": "listAuthInvites",
        "summary": "List invite tokens",
        "why": "Operator listing of outstanding invites.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ invites }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthInvitesList",
        "ts_method": "authInvitesList"
    },
    {
        "command_id": "auth.invites.revoke",
        "cli_path": "auth invites revoke",
        "group": "auth",
        "method": "POST",
        "path": "/auth/invites/{invite_id}/revoke",
        "operation_id": "revokeAuthInvite",
        "summary": "Revoke invite",
        "description": "Human only, revalidated in the mutation transaction. Agents cannot revoke human recovery invitations.",
        "why": "Invalidate an outstanding invite by id.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ invite }`.",
        "error_codes": [
            "human_required",
            "auth_required",
            "invalid_request",
            "not_found",
            "invalid_token"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "path_params": [
            "invite_id"
        ],
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthInvitesRevoke",
        "ts_method": "authInvitesRevoke"
    },
    {
        "command_id": "auth.passkey.dev.login",
        "cli_path": "auth passkey dev login",
        "group": "auth",
        "method": "POST",
        "path": "/auth/passkey/dev/login",
        "operation_id": "passkeyDevLogin",
        "summary": "Issue session for passkey principal without WebAuthn (dev only)",
        "why": "Local development bypass when ANX_ALLOW_PASSKEY_DEV_BYPASS=1 and the workspace carries the local-only .anx-dev-insecure-auth marker; optional username or display_name, or sole passkey principal.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ agent, tokens }`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "dev_passkey_bypass_disabled"
        ],
        "concepts": [
            "auth",
            "passkeys"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthPasskeyDevLogin",
        "ts_method": "authPasskeyDevLogin"
    },
    {
        "command_id": "auth.passkey.dev.register",
        "cli_path": "auth passkey dev register",
        "group": "auth",
        "method": "POST",
        "path": "/auth/passkey/dev/register",
        "operation_id": "passkeyDevRegister",
        "summary": "Complete passkey registration without WebAuthn (dev only)",
        "why": "Local development bypass for human onboarding when ANX_ALLOW_PASSKEY_DEV_BYPASS=1 and the workspace carries the local-only .anx-dev-insecure-auth marker; stores a synthetic credential. Optional existing_actor_id links a pre-seeded actor when ANX_DEV_REGISTER_LINKED_ACTORS=1.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ agent, tokens }`.",
        "error_codes": [
            "invalid_request",
            "invalid_token",
            "dev_passkey_bypass_disabled"
        ],
        "concepts": [
            "auth",
            "passkeys"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthPasskeyDevRegister",
        "ts_method": "authPasskeyDevRegister"
    },
    {
        "command_id": "auth.passkey.login.options",
        "cli_path": "auth passkey login options",
        "group": "auth",
        "method": "POST",
        "path": "/auth/passkey/login/options",
        "operation_id": "passkeyLoginOptions",
        "summary": "Begin passkey login",
        "why": "WebAuthn assertion challenge for returning principals.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ session_id, options }`.",
        "error_codes": [
            "invalid_request",
            "human_required"
        ],
        "concepts": [
            "auth",
            "passkeys"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthPasskeyLoginOptions",
        "ts_method": "authPasskeyLoginOptions"
    },
    {
        "command_id": "auth.passkey.login.verify",
        "cli_path": "auth passkey login verify",
        "group": "auth",
        "method": "POST",
        "path": "/auth/passkey/login/verify",
        "operation_id": "passkeyLoginVerify",
        "summary": "Complete passkey login",
        "why": "Verify WebAuthn assertion and issue tokens.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ agent, tokens }`.",
        "error_codes": [
            "invalid_request",
            "invalid_token",
            "human_required"
        ],
        "concepts": [
            "auth",
            "passkeys"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthPasskeyLoginVerify",
        "ts_method": "authPasskeyLoginVerify"
    },
    {
        "command_id": "auth.passkey.register.options",
        "cli_path": "auth passkey register options",
        "group": "auth",
        "method": "POST",
        "path": "/auth/passkey/register/options",
        "operation_id": "passkeyRegisterOptions",
        "summary": "Begin passkey registration",
        "why": "WebAuthn registration challenge for workspace humans.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ session_id, options }`.",
        "error_codes": [
            "invalid_request",
            "human_required"
        ],
        "concepts": [
            "auth",
            "passkeys"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthPasskeyRegisterOptions",
        "ts_method": "authPasskeyRegisterOptions"
    },
    {
        "command_id": "auth.passkey.register.verify",
        "cli_path": "auth passkey register verify",
        "group": "auth",
        "method": "POST",
        "path": "/auth/passkey/register/verify",
        "operation_id": "passkeyRegisterVerify",
        "summary": "Complete passkey registration",
        "why": "Verify WebAuthn attestation and issue tokens.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ agent, tokens }` for a human passkey principal.",
        "error_codes": [
            "invalid_request",
            "invalid_token",
            "human_required"
        ],
        "concepts": [
            "auth",
            "passkeys"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.principals.list",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthPasskeyRegisterVerify",
        "ts_method": "authPasskeyRegisterVerify"
    },
    {
        "command_id": "auth.principals.list",
        "cli_path": "auth principals list",
        "group": "auth",
        "method": "GET",
        "path": "/auth/principals",
        "operation_id": "listAuthPrincipals",
        "summary": "List principals",
        "description": "Auth-admin principal inventory. Each summary includes auth_admin, the explicit metadata grant; human administration remains implicit in the human principal kind.",
        "why": "Operator visibility into registered principals for the workspace.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns principal list JSON.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.revoke",
            "auth.token"
        ],
        "go_method": "AuthPrincipalsList",
        "ts_method": "authPrincipalsList"
    },
    {
        "command_id": "auth.principals.revoke",
        "cli_path": "auth principals revoke",
        "group": "auth",
        "method": "POST",
        "path": "/auth/principals/{principal_id}/revoke",
        "operation_id": "revokeAuthPrincipal",
        "summary": "Revoke principal by agent id",
        "description": "Human only, revalidated in the mutation transaction. Agents cannot administratively revoke any principal or use the human lockout override.",
        "why": "Administrative revocation of a principal linkage.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns result JSON.",
        "error_codes": [
            "human_required",
            "auth_required",
            "invalid_request",
            "not_found",
            "invalid_token",
            "conflict"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "path_params": [
            "principal_id"
        ],
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.token"
        ],
        "go_method": "AuthPrincipalsRevoke",
        "ts_method": "authPrincipalsRevoke"
    },
    {
        "command_id": "auth.token",
        "cli_path": "auth token",
        "group": "auth",
        "method": "POST",
        "path": "/auth/token",
        "operation_id": "issueAuthToken",
        "summary": "Exchange host assertion or refresh auth tokens",
        "description": "grant_type=host_assertion requires host_id, key_id, agent_name, signed_at, signature. Sign UTF-8 bytes of `anx-host-agent-token|\u003chost_id\u003e|\u003ckey_id\u003e|\u003cagent_name\u003e|\u003csigned_at\u003e` with the host Ed25519 key and send base64 signature. signed_at is RFC3339 UTC, within 5 minutes of server time. Core atomically records a hash of message and signature; replay fails. The name is lowercase `[a-z][a-z0-9-]*` (1–32 characters). Discovered adapters use claude, codex, cursor, omp, or generic; other names are explicit personas. The returned agent handle is `\u003cagent_name\u003e.\u003chost_slug\u003e`. Missing agents are created lazily with stable actor ID. Excluded names, revoked agents, and revoked hosts receive no token. Host grants return only a short-lived agent access token, no refresh token. Existing grants apply only to their existing eligible principals; agent-key assertion cannot mint derived agents.",
        "why": "Assertion or refresh-token exchange for bearer access.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Host assertion returns `{ agent, tokens: { access_token, token_type, expires_in } }`; other grants retain their existing `{ tokens }` envelope.",
        "error_codes": [
            "invalid_request",
            "invalid_token",
            "key_mismatch",
            "host_revoked",
            "agent_excluded",
            "agent_handle_taken",
            "agent_revoked"
        ],
        "concepts": [
            "auth"
        ],
        "stability": "beta",
        "surface": "utility",
        "body_schema": {
            "required": [
                {
                    "name": "grant_type",
                    "type": "string",
                    "enum_values": [
                        "assertion",
                        "host_assertion",
                        "refresh_token",
                        "workspace_human_grant",
                        "workspace_managed_agent_grant"
                    ]
                }
            ],
            "optional": [
                {
                    "name": "agent_id",
                    "type": "string"
                },
                {
                    "name": "agent_name",
                    "type": "string"
                },
                {
                    "name": "assertion",
                    "type": "string"
                },
                {
                    "name": "host_id",
                    "type": "string"
                },
                {
                    "name": "key_id",
                    "type": "string"
                },
                {
                    "name": "refresh_token",
                    "type": "string"
                },
                {
                    "name": "signature",
                    "type": "string"
                },
                {
                    "name": "signed_at",
                    "type": "datetime"
                }
            ]
        },
        "adjacent_commands": [
            "auth.access-requests.approve",
            "auth.access-requests.deny",
            "auth.access-requests.list",
            "auth.access-requests.request",
            "auth.access-requests.summary",
            "auth.admins.grant",
            "auth.admins.list",
            "auth.admins.revoke",
            "auth.audit.list",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.dev.login",
            "auth.passkey.dev.register",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.principals.list",
            "auth.principals.revoke"
        ],
        "go_method": "AuthToken",
        "ts_method": "authToken"
    },
    {
        "command_id": "boards.archive",
        "cli_path": "boards archive",
        "group": "boards",
        "method": "POST",
        "path": "/boards/{board_id}/archive",
        "operation_id": "archiveBoard",
        "summary": "Archive board",
        "why": "Soft-archive a board and derive its lifecycle state from archived_at.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "boards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsArchive",
        "ts_method": "boardsArchive"
    },
    {
        "command_id": "boards.cards.batch_add",
        "cli_path": "boards cards create-batch",
        "group": "boards",
        "method": "POST",
        "path": "/boards/{board_id}/cards/batch",
        "operation_id": "batchCreateBoardCards",
        "summary": "Batch create cards on board",
        "why": "Create multiple cards in one transaction using a single board concurrency token.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, cards }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "boards",
            "cards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "items",
                    "type": "list\u003cany\u003e"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_board_updated_at",
                    "type": "datetime"
                },
                {
                    "name": "request_key",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsCardsBatchAdd",
        "ts_method": "boardsCardsBatchAdd"
    },
    {
        "command_id": "boards.cards.get",
        "cli_path": "boards cards get",
        "group": "boards",
        "method": "GET",
        "path": "/boards/{board_id}/cards/{card_id}",
        "operation_id": "getBoardCard",
        "summary": "Get board-scoped card",
        "why": "Resolve a card through its board membership context.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ card }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "boards",
            "cards"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "board_id",
            "card_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsCardsGet",
        "ts_method": "boardsCardsGet"
    },
    {
        "command_id": "boards.cards.list",
        "cli_path": "boards cards list",
        "group": "boards",
        "method": "GET",
        "path": "/boards/{board_id}/cards",
        "operation_id": "listBoardCards",
        "summary": "List board cards",
        "why": "List cards on one board in canonical order.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board_ref, board_handle, cards }`; internal board_id may appear for admin/debug compatibility.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "boards",
            "cards"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsCardsList",
        "ts_method": "boardsCardsList"
    },
    {
        "command_id": "boards.create",
        "cli_path": "boards create",
        "group": "boards",
        "method": "POST",
        "path": "/boards",
        "operation_id": "createBoard",
        "summary": "Create board",
        "why": "Create a durable board over topics and cards.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "boards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "board.document_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "board.pinned_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "board.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "board.title",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "board.column_schema",
                    "type": "object"
                },
                {
                    "name": "board.id",
                    "type": "string"
                },
                {
                    "name": "board.primary_topic_ref",
                    "type": "string"
                },
                {
                    "name": "board.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "board.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "board.role",
                    "type": "string"
                },
                {
                    "name": "board.summary",
                    "type": "string"
                },
                {
                    "name": "board.thread_id",
                    "type": "string"
                },
                {
                    "name": "board.workspace_move",
                    "type": "object"
                }
            ]
        },
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsCreate",
        "ts_method": "boardsCreate"
    },
    {
        "command_id": "boards.get",
        "cli_path": "boards get",
        "group": "boards",
        "method": "GET",
        "path": "/boards/{board_id}",
        "operation_id": "getBoard",
        "summary": "Get board",
        "why": "Resolve canonical board state and summary.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, summary }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "boards"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsGet",
        "ts_method": "boardsGet"
    },
    {
        "command_id": "boards.list",
        "cli_path": "boards list",
        "group": "boards",
        "method": "GET",
        "path": "/boards",
        "operation_id": "listBoards",
        "summary": "List boards",
        "why": "Scan durable coordination boards and lightweight summaries.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ boards, next_cursor? }` (each `boards[]` item is `{ board, summary }` with `summary` a `BoardSummary` projection, not the board's text blurb).",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "boards"
        ],
        "stability": "beta",
        "surface": "canonical",
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsList",
        "ts_method": "boardsList"
    },
    {
        "command_id": "boards.patch",
        "cli_path": "boards patch",
        "group": "boards",
        "method": "PATCH",
        "path": "/boards/{board_id}",
        "operation_id": "patchBoard",
        "summary": "Patch board",
        "why": "Update board metadata with optimistic concurrency.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "boards",
            "write",
            "concurrency"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ],
            "optional": [
                {
                    "name": "patch.document_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.pinned_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.primary_topic_ref",
                    "type": "string"
                },
                {
                    "name": "patch.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "patch.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "patch.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.role",
                    "type": "string"
                },
                {
                    "name": "patch.summary",
                    "type": "string"
                },
                {
                    "name": "patch.title",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsPatch",
        "ts_method": "boardsPatch"
    },
    {
        "command_id": "boards.purge",
        "cli_path": "boards purge",
        "group": "boards",
        "method": "POST",
        "path": "/boards/{board_id}/purge",
        "operation_id": "purgeBoard",
        "summary": "Permanently delete trashed board",
        "why": "Permanently delete a trashed board (human-gated).",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ purged, board_ref, board_handle }`; internal board_id may appear for admin/debug compatibility.",
        "error_codes": [
            "auth_required",
            "human_only",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "boards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.restore",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsPurge",
        "ts_method": "boardsPurge"
    },
    {
        "command_id": "boards.restore",
        "cli_path": "boards restore",
        "group": "boards",
        "method": "POST",
        "path": "/boards/{board_id}/restore",
        "operation_id": "restoreBoard",
        "summary": "Restore board from trash",
        "why": "Clear trash lifecycle fields on a board after an explicit restore action.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "boards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.trash",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsRestore",
        "ts_method": "boardsRestore"
    },
    {
        "command_id": "boards.trash",
        "cli_path": "boards trash",
        "group": "boards",
        "method": "POST",
        "path": "/boards/{board_id}/trash",
        "operation_id": "trashBoard",
        "summary": "Move board to trash",
        "why": "Move board to trash with an explicit operator reason.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "boards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "reason",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.unarchive",
            "boards.workspace"
        ],
        "go_method": "BoardsTrash",
        "ts_method": "boardsTrash"
    },
    {
        "command_id": "boards.unarchive",
        "cli_path": "boards unarchive",
        "group": "boards",
        "method": "POST",
        "path": "/boards/{board_id}/unarchive",
        "operation_id": "unarchiveBoard",
        "summary": "Unarchive board",
        "why": "Clear archived_at on a board (restore default list visibility).",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "boards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.workspace"
        ],
        "go_method": "BoardsUnarchive",
        "ts_method": "boardsUnarchive"
    },
    {
        "command_id": "boards.workspace",
        "cli_path": "boards workspace",
        "group": "boards",
        "method": "GET",
        "path": "/boards/{board_id}/workspace",
        "operation_id": "getBoardWorkspace",
        "summary": "Get board workspace view",
        "why": "Load the operator-facing board workspace with cards, docs, and inbox sections.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, primary_topic, cards, documents, inbox, board_summary, projection_freshness, board_summary_freshness, warnings, section_kinds, generated_at }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "boards",
            "workspace"
        ],
        "stability": "beta",
        "surface": "projection",
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.archive",
            "boards.cards.batch_add",
            "boards.cards.get",
            "boards.cards.list",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.patch",
            "boards.purge",
            "boards.restore",
            "boards.trash",
            "boards.unarchive"
        ],
        "go_method": "BoardsWorkspace",
        "ts_method": "boardsWorkspace"
    },
    {
        "command_id": "cards.archive",
        "cli_path": "cards archive",
        "group": "cards",
        "method": "POST",
        "path": "/cards/{card_id}/archive",
        "operation_id": "archiveCard",
        "summary": "Archive card",
        "why": "Soft-delete a first-class card by setting archived_at (board concurrency via if_board_updated_at).",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, card }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict",
            "already_trashed"
        ],
        "concepts": [
            "cards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_board_updated_at",
                    "type": "datetime"
                },
                {
                    "name": "if_latest_observation_id",
                    "type": "string"
                },
                {
                    "name": "if_version",
                    "type": "integer"
                }
            ]
        },
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsArchive",
        "ts_method": "cardsArchive"
    },
    {
        "command_id": "cards.create",
        "cli_path": "cards create",
        "group": "cards",
        "method": "POST",
        "path": "/cards",
        "operation_id": "createCard",
        "summary": "Create card (global path)",
        "why": "Create a card by supplying board_ref or board_handle in the request body. Use this canonical card workflow path for single-card creation.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, card }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "cards",
            "boards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "card.title",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "board_handle",
                    "type": "string"
                },
                {
                    "name": "board_id",
                    "type": "string"
                },
                {
                    "name": "board_ref",
                    "type": "any"
                },
                {
                    "name": "card.after_card_id",
                    "type": "string"
                },
                {
                    "name": "card.assignee_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "card.before_card_id",
                    "type": "string"
                },
                {
                    "name": "card.column_key",
                    "type": "string",
                    "enum_values": [
                        "backlog",
                        "blocked",
                        "done",
                        "in_progress",
                        "ready",
                        "review"
                    ]
                },
                {
                    "name": "card.definition_of_done",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "card.document_ref",
                    "type": "string"
                },
                {
                    "name": "card.due_at",
                    "type": "datetime"
                },
                {
                    "name": "card.handle",
                    "type": "string"
                },
                {
                    "name": "card.id",
                    "type": "string"
                },
                {
                    "name": "card.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "card.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "card.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "card.related_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "card.resolution",
                    "type": "string",
                    "enum_values": [
                        "done"
                    ]
                },
                {
                    "name": "card.resolution_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "card.risk",
                    "type": "string",
                    "enum_values": [
                        "critical",
                        "high",
                        "low",
                        "medium"
                    ]
                },
                {
                    "name": "card.summary",
                    "type": "string"
                },
                {
                    "name": "card.topic_ref",
                    "type": "string"
                },
                {
                    "name": "if_board_updated_at",
                    "type": "datetime"
                },
                {
                    "name": "request_key",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "cards.archive",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsCreate",
        "ts_method": "cardsCreate"
    },
    {
        "command_id": "cards.get",
        "cli_path": "cards get",
        "group": "cards",
        "method": "GET",
        "path": "/cards/{card_id}",
        "operation_id": "getCard",
        "summary": "Get card",
        "why": "Resolve one first-class card by public ref or handle.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ card }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "cards"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsGet",
        "ts_method": "cardsGet"
    },
    {
        "command_id": "cards.list",
        "cli_path": "cards list",
        "group": "cards",
        "method": "GET",
        "path": "/cards",
        "operation_id": "listCards",
        "summary": "List cards",
        "why": "Scan the canonical card store. `cards.*` is the store API; `work.*` is the operator Tasks projection over the same rows. Use this family for card workflow writes.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ cards }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "cards"
        ],
        "stability": "beta",
        "surface": "canonical",
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsList",
        "ts_method": "cardsList"
    },
    {
        "command_id": "cards.move",
        "cli_path": "cards move",
        "group": "cards",
        "method": "POST",
        "path": "/cards/{card_id}/move",
        "operation_id": "moveCard",
        "summary": "Move card",
        "why": "Reposition a card within a board column using the card's first-class identity.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ card }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "cards",
            "boards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "column_key",
                    "type": "string",
                    "enum_values": [
                        "backlog",
                        "blocked",
                        "done",
                        "in_progress",
                        "ready",
                        "review"
                    ]
                },
                {
                    "name": "if_board_updated_at",
                    "type": "datetime"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "after_card_id",
                    "type": "string"
                },
                {
                    "name": "before_card_id",
                    "type": "string"
                },
                {
                    "name": "resolution",
                    "type": "string",
                    "enum_values": [
                        "done"
                    ]
                },
                {
                    "name": "resolution_refs",
                    "type": "list\u003cany\u003e"
                }
            ]
        },
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsMove",
        "ts_method": "cardsMove"
    },
    {
        "command_id": "cards.patch",
        "cli_path": "cards patch",
        "group": "cards",
        "method": "PATCH",
        "path": "/cards/{card_id}",
        "operation_id": "patchCard",
        "summary": "Patch card",
        "why": "Update card fields, including resolution and resolution refs.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ card }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "cards",
            "write",
            "concurrency"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "patch.assignee_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.document_ref",
                    "type": "string"
                },
                {
                    "name": "patch.due_at",
                    "type": "datetime"
                },
                {
                    "name": "patch.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "patch.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "patch.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.related_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.resolution",
                    "type": "string",
                    "enum_values": [
                        "done"
                    ]
                },
                {
                    "name": "patch.resolution_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.risk",
                    "type": "string",
                    "enum_values": [
                        "critical",
                        "high",
                        "low",
                        "medium"
                    ]
                },
                {
                    "name": "patch.topic_ref",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsPatch",
        "ts_method": "cardsPatch"
    },
    {
        "command_id": "cards.purge",
        "cli_path": "cards purge",
        "group": "cards",
        "method": "POST",
        "path": "/cards/{card_id}/purge",
        "operation_id": "purgeArchivedCard",
        "summary": "Permanently delete archived or trashed card",
        "why": "Permanently delete an archived or trashed card (human-gated).",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ purged, card_ref, card_handle }`; internal card_id may appear for admin/debug compatibility.",
        "error_codes": [
            "auth_required",
            "human_only",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "cards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsPurge",
        "ts_method": "cardsPurge"
    },
    {
        "command_id": "cards.restore",
        "cli_path": "cards restore",
        "group": "cards",
        "method": "POST",
        "path": "/cards/{card_id}/restore",
        "operation_id": "restoreArchivedCard",
        "summary": "Restore archived or trashed card",
        "why": "Clear archive or trash lifecycle fields on a card so it reappears on boards.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, card }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "cards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_board_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsRestore",
        "ts_method": "cardsRestore"
    },
    {
        "command_id": "cards.revisions.create",
        "cli_path": "cards revise",
        "group": "cards",
        "method": "POST",
        "path": "/cards/{card_id}/revisions",
        "operation_id": "createCardRevision",
        "summary": "Create card revision",
        "why": "Append a new immutable card content revision and advance the card head.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ card, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "cards",
            "revisions",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "if_base_revision",
                    "type": "string"
                },
                {
                    "name": "revision.summary",
                    "type": "string"
                },
                {
                    "name": "revision.title",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "revision.definition_of_done",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "revision.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "revision.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "revision.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "revision.refs",
                    "type": "list\u003cany\u003e"
                }
            ]
        },
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsRevisionsCreate",
        "ts_method": "cardsRevisionsCreate"
    },
    {
        "command_id": "cards.revisions.get",
        "cli_path": "cards revision get",
        "group": "cards",
        "method": "GET",
        "path": "/cards/{card_id}/revisions/{revision_id}",
        "operation_id": "getCardRevision",
        "summary": "Get card revision",
        "why": "Resolve one immutable card content revision.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ card_ref, card_handle, revision }`; internal card_id may appear for admin/debug compatibility.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "cards",
            "revisions"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "card_id",
            "revision_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsRevisionsGet",
        "ts_method": "cardsRevisionsGet"
    },
    {
        "command_id": "cards.revisions.list",
        "cli_path": "cards history",
        "group": "cards",
        "method": "GET",
        "path": "/cards/{card_id}/revisions",
        "operation_id": "listCardRevisions",
        "summary": "List card revisions",
        "why": "Enumerate immutable content revisions for one card lineage.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ card_ref, card_handle, revisions }`; internal card_id may appear for admin/debug compatibility.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "cards",
            "revisions"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline",
            "cards.trash"
        ],
        "go_method": "CardsRevisionsList",
        "ts_method": "cardsRevisionsList"
    },
    {
        "command_id": "cards.timeline",
        "cli_path": "cards timeline",
        "group": "cards",
        "method": "GET",
        "path": "/cards/{card_id}/timeline",
        "operation_id": "getCardTimeline",
        "summary": "Get card timeline",
        "why": "Load chronological evidence and related resources for one card.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ card, events, artifacts, cards, documents, threads }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "cards",
            "timeline"
        ],
        "stability": "beta",
        "surface": "projection",
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.trash"
        ],
        "go_method": "CardsTimeline",
        "ts_method": "cardsTimeline"
    },
    {
        "command_id": "cards.trash",
        "cli_path": "cards trash",
        "group": "cards",
        "method": "POST",
        "path": "/cards/{card_id}/trash",
        "operation_id": "trashCard",
        "summary": "Move card to trash",
        "why": "Move a card to trash with an explicit operator reason while keeping archive lifecycle distinct.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, card }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "cards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "reason",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_board_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "cards.archive",
            "cards.create",
            "cards.get",
            "cards.revisions.list",
            "cards.list",
            "cards.move",
            "cards.patch",
            "cards.purge",
            "cards.restore",
            "cards.revisions.create",
            "cards.revisions.get",
            "cards.timeline"
        ],
        "go_method": "CardsTrash",
        "ts_method": "cardsTrash"
    },
    {
        "command_id": "derived.rebuild",
        "cli_path": "derived rebuild",
        "group": "derived",
        "method": "POST",
        "path": "/derived/rebuild",
        "operation_id": "rebuildDerivedProjections",
        "summary": "Rebuild derived projections",
        "why": "Deterministic operator repair for inbox/thread projections.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ ok: true }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "projections",
            "maintenance"
        ],
        "stability": "beta",
        "surface": "utility",
        "go_method": "DerivedRebuild",
        "ts_method": "derivedRebuild"
    },
    {
        "command_id": "docs.archive",
        "cli_path": "docs archive",
        "group": "docs",
        "method": "POST",
        "path": "/docs/{document_id}/archive",
        "operation_id": "archiveDocument",
        "summary": "Archive document",
        "why": "Soft-archive a document lineage (orthogonal to head revision content).",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "Archive a document",
                "command": "anx docs archive doc:runbook --reason \"superseded\""
            }
        ],
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsArchive",
        "ts_method": "docsArchive"
    },
    {
        "command_id": "docs.comments.create",
        "cli_path": "docs comment",
        "group": "docs",
        "method": "POST",
        "path": "/docs/{document_id}/comments",
        "operation_id": "createDocumentComment",
        "summary": "Post a document comment",
        "why": "Post a comment on a document so another agent can read it later.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ comment }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Posts a `message_posted` event on the document backing thread. Optional `reply_to` or `parent_id` creates a reply. Comment refs (`event:\u003chandle\u003e`) are stable across document revisions and are the deep-link identity.",
        "examples": [
            {
                "title": "Post a comment",
                "command": "anx docs comment doc:runbook \"Host B found this\""
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "text",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "parent_id",
                    "type": "string"
                },
                {
                    "name": "reply_to",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsCommentsCreate",
        "ts_method": "docsCommentsCreate"
    },
    {
        "command_id": "docs.comments.delete",
        "cli_path": "docs comments delete",
        "group": "docs",
        "method": "DELETE",
        "path": "/docs/{document_id}/comments/{comment_id}",
        "operation_id": "deleteDocumentComment",
        "summary": "Delete one's own document comment",
        "why": "Remove a comment you authored from the document discussion.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ comment }` with the trashed comment.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "forbidden"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Trashes the backing `message_posted` event. Only the original author may delete. The comment ref stays stable; list omits trashed comments.",
        "examples": [
            {
                "title": "Delete own comment",
                "command": "anx docs comments delete doc:runbook event:note"
            }
        ],
        "path_params": [
            "document_id",
            "comment_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsCommentsDelete",
        "ts_method": "docsCommentsDelete"
    },
    {
        "command_id": "docs.comments.list",
        "cli_path": "docs comments",
        "group": "docs",
        "method": "GET",
        "path": "/docs/{document_id}/comments",
        "operation_id": "listDocumentComments",
        "summary": "List document comments",
        "why": "Read the document discussion thread with stable comment ids.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ comments, next_cursor? }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "docs"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Comments are the document backing-thread `message_posted` events, projected with stable `event:\u003chandle\u003e` refs that survive document revisions. `reply_to` is the parent comment ref for threaded replies; `parent_id` is the same parent as an internal id.",
        "examples": [
            {
                "title": "List comments",
                "command": "anx docs comments doc:runbook"
            }
        ],
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsCommentsList",
        "ts_method": "docsCommentsList"
    },
    {
        "command_id": "docs.comments.reply",
        "cli_path": "docs comments reply",
        "group": "docs",
        "method": "POST",
        "path": "/docs/{document_id}/comments/{comment_id}/replies",
        "operation_id": "replyDocumentComment",
        "summary": "Reply to a document comment",
        "why": "Reply in a document comment thread without leaving the docs surface.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ comment }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Posts a reply `message_posted` event with `reply_to` set to `{comment_id}`. Prefer `docs comment --reply-to` from the CLI.",
        "examples": [
            {
                "title": "Reply in thread",
                "command": "anx docs comments reply doc:runbook event:note --body \"Acknowledged\""
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "text",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "parent_id",
                    "type": "string"
                },
                {
                    "name": "reply_to",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "document_id",
            "comment_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsCommentsReply",
        "ts_method": "docsCommentsReply"
    },
    {
        "command_id": "docs.comments.update",
        "cli_path": "docs comments edit",
        "group": "docs",
        "method": "PATCH",
        "path": "/docs/{document_id}/comments/{comment_id}",
        "operation_id": "updateDocumentComment",
        "summary": "Edit one's own document comment",
        "why": "Edit a comment you authored without changing its stable ref.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ comment }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "forbidden"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Updates the comment body in place. Only the original author may edit. The comment `ref`/`handle` stay the same so UI deep-links remain valid across edits and document revisions.",
        "examples": [
            {
                "title": "Edit own comment",
                "command": "anx docs comments edit doc:runbook event:note --body \"Corrected\""
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "text",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "document_id",
            "comment_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsCommentsUpdate",
        "ts_method": "docsCommentsUpdate"
    },
    {
        "command_id": "docs.create",
        "cli_path": "docs create",
        "group": "docs",
        "method": "POST",
        "path": "/docs",
        "operation_id": "createDocument",
        "summary": "Create document",
        "why": "Create a canonical document lineage anchored to a typed subject ref.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "Create from a local file",
                "command": "anx docs create --topic topic:launch --title \"Runbook\" --body-file runbook.md"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "content_type",
                    "type": "string",
                    "enum_values": [
                        "binary",
                        "structured",
                        "text"
                    ]
                },
                {
                    "name": "document.title",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "content",
                    "type": "any"
                },
                {
                    "name": "content_base64",
                    "type": "string"
                },
                {
                    "name": "document.document_id",
                    "type": "string"
                },
                {
                    "name": "document.handle",
                    "type": "string"
                },
                {
                    "name": "document.hosts",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "document.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "document.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "document.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "document.refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "document.source",
                    "type": "string"
                },
                {
                    "name": "document.subject_ref",
                    "type": "string"
                },
                {
                    "name": "document.summary",
                    "type": "string"
                },
                {
                    "name": "document.tags",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "document.thread_id",
                    "type": "string"
                },
                {
                    "name": "document.verified_at",
                    "type": "datetime"
                },
                {
                    "name": "document.workspace_move",
                    "type": "object"
                },
                {
                    "name": "refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "request_key",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsCreate",
        "ts_method": "docsCreate"
    },
    {
        "command_id": "docs.get",
        "cli_path": "docs get",
        "group": "docs",
        "method": "GET",
        "path": "/docs/{document_id}",
        "operation_id": "getDocument",
        "summary": "Get document",
        "why": "Resolve a document lineage and its current head revision.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "docs"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Returns `{ document, revision }` including the head revision body. CLI `--format md` prints only the markdown body. Knowledge docs expose `source`, `hosts`, and `verified_at`.",
        "examples": [
            {
                "title": "Print markdown body",
                "command": "anx docs get kb-shared --format md"
            }
        ],
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsGet",
        "ts_method": "docsGet"
    },
    {
        "command_id": "docs.list",
        "cli_path": "docs list",
        "group": "docs",
        "method": "GET",
        "path": "/docs",
        "operation_id": "listDocuments",
        "summary": "List documents",
        "why": "Scan canonical document lineages.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ documents }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "docs"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "List knowledge docs",
                "command": "anx docs list --knowledge"
            }
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsList",
        "ts_method": "docsList"
    },
    {
        "command_id": "docs.patch",
        "cli_path": "docs patch",
        "group": "docs",
        "method": "PATCH",
        "path": "/docs/{document_id}",
        "operation_id": "patchDocument",
        "summary": "Patch document resource",
        "why": "Update document resource fields (e.g. summary) without creating a revision.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "docs",
            "write",
            "concurrency"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "Patch knowledge hosts",
                "command": "anx docs patch kb-shared --from-file patch.json"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ],
            "optional": [
                {
                    "name": "patch.hosts",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.source",
                    "type": "string"
                },
                {
                    "name": "patch.summary",
                    "type": "string"
                },
                {
                    "name": "patch.tags",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.title",
                    "type": "string"
                },
                {
                    "name": "patch.verified_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsPatch",
        "ts_method": "docsPatch"
    },
    {
        "command_id": "docs.purge",
        "cli_path": "docs purge",
        "group": "docs",
        "method": "POST",
        "path": "/docs/{document_id}/purge",
        "operation_id": "purgeDocument",
        "summary": "Permanently delete trashed document",
        "why": "Permanently delete a trashed document (human-gated).",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ purged, document_ref, document_handle }`; internal document_id may appear for admin/debug compatibility.",
        "error_codes": [
            "auth_required",
            "human_only",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "Purge a trashed document",
                "command": "anx docs purge doc:runbook"
            }
        ],
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsPurge",
        "ts_method": "docsPurge"
    },
    {
        "command_id": "docs.put",
        "cli_path": "docs put",
        "group": "docs",
        "method": "PUT",
        "path": "/docs/{document_id}",
        "operation_id": "putDocument",
        "summary": "Create or replace a document by handle",
        "why": "Idempotent write of document body and metadata keyed by handle, so agents can republish knowledge without duplicating lineages.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "conflict"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Path `{document_id}` is the public handle (or `document:\u003chandle\u003e`). If that handle exists, a new revision is appended and metadata (`title`, `source`, `tags`, `hosts`, `verified_at`) is updated. If it does not exist, the document is created with that handle. Visibility/lifecycle is unchanged. CLI `anx docs put -` reads the body from stdin.",
        "examples": [
            {
                "title": "Publish from stdin",
                "command": "anx docs put - --handle kb-shared --title \"Note\" --tags knowledge --source https://example.invalid/note.md --hosts laptop-a --verified-at 2026-09-08T12:00:00Z"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "content",
                    "type": "any"
                },
                {
                    "name": "content_type",
                    "type": "string",
                    "enum_values": [
                        "binary",
                        "structured",
                        "text"
                    ]
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "document.hosts",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "document.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "document.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "document.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "document.refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "document.source",
                    "type": "string"
                },
                {
                    "name": "document.subject_ref",
                    "type": "string"
                },
                {
                    "name": "document.summary",
                    "type": "string"
                },
                {
                    "name": "document.tags",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "document.title",
                    "type": "string"
                },
                {
                    "name": "document.verified_at",
                    "type": "datetime"
                },
                {
                    "name": "refs",
                    "type": "list\u003cany\u003e"
                }
            ]
        },
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsPut",
        "ts_method": "docsPut"
    },
    {
        "command_id": "docs.restore",
        "cli_path": "docs restore",
        "group": "docs",
        "method": "POST",
        "path": "/docs/{document_id}/restore",
        "operation_id": "restoreDocument",
        "summary": "Restore document from trash",
        "why": "Clear trash state on a document after an explicit restore action.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "Restore a trashed document",
                "command": "anx docs restore doc:runbook"
            }
        ],
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "reason",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsRestore",
        "ts_method": "docsRestore"
    },
    {
        "command_id": "docs.revisions.create",
        "cli_path": "docs revise",
        "group": "docs",
        "method": "POST",
        "path": "/docs/{document_id}/revisions",
        "operation_id": "createDocumentRevision",
        "summary": "Create document revision",
        "why": "Append a new immutable revision and advance the document head.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "docs",
            "revisions",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "Revise from a local file",
                "command": "anx docs revise doc:runbook --body-file runbook.md"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "content",
                    "type": "any"
                },
                {
                    "name": "content_type",
                    "type": "string",
                    "enum_values": [
                        "binary",
                        "structured",
                        "text"
                    ]
                },
                {
                    "name": "if_base_revision",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "document",
                    "type": "object"
                },
                {
                    "name": "provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "provenance.notes",
                    "type": "string"
                },
                {
                    "name": "provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "refs",
                    "type": "list\u003cany\u003e"
                }
            ]
        },
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsRevisionsCreate",
        "ts_method": "docsRevisionsCreate"
    },
    {
        "command_id": "docs.revisions.get",
        "cli_path": "docs revision get",
        "group": "docs",
        "method": "GET",
        "path": "/docs/{document_id}/revisions/{revision_id}",
        "operation_id": "getDocumentRevision",
        "summary": "Get document revision",
        "why": "Resolve one immutable document revision.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document_ref, document_handle, revision }`; internal document_id may appear for admin/debug compatibility.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "docs",
            "revisions"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "document_id",
            "revision_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsRevisionsGet",
        "ts_method": "docsRevisionsGet"
    },
    {
        "command_id": "docs.revisions.list",
        "cli_path": "docs history",
        "group": "docs",
        "method": "GET",
        "path": "/docs/{document_id}/revisions",
        "operation_id": "listDocumentRevisions",
        "summary": "List document revisions",
        "why": "Enumerate immutable revisions for one document lineage.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document_ref, document_handle, revisions }`; internal document_id may appear for admin/debug compatibility.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "docs",
            "revisions"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "List revision history",
                "command": "anx docs history doc:runbook"
            }
        ],
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsRevisionsList",
        "ts_method": "docsRevisionsList"
    },
    {
        "command_id": "docs.search",
        "cli_path": "docs search",
        "group": "docs",
        "method": "GET",
        "path": "/docs/search",
        "operation_id": "searchDocuments",
        "summary": "Search documents",
        "why": "Full-text search over document title, body, and comments so agents can find knowledge another host wrote.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ documents, next_cursor? }`. Each document may include `search_rank` (higher is better).",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "docs"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "SQLite FTS5 over title, body, summary, source, tags, and backing-thread comments. Query terms are AND-matched; punctuation is tokenized. Prefer this over `docs.list?q=` when matching body or comments. `search_rank` is higher for stronger matches (title weighted above body, then summary/source/tags, then comments).",
        "examples": [
            {
                "title": "Search knowledge",
                "command": "anx docs search \"runbook\" --knowledge --limit 20"
            }
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.trash",
            "docs.unarchive"
        ],
        "go_method": "DocsSearch",
        "ts_method": "docsSearch"
    },
    {
        "command_id": "docs.trash",
        "cli_path": "docs trash",
        "group": "docs",
        "method": "POST",
        "path": "/docs/{document_id}/trash",
        "operation_id": "trashDocument",
        "summary": "Move document to trash",
        "why": "Move a document lineage to trash with an explicit operator reason.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "Trash a document",
                "command": "anx docs trash doc:runbook --reason \"obsolete\""
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "reason",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.unarchive"
        ],
        "go_method": "DocsTrash",
        "ts_method": "docsTrash"
    },
    {
        "command_id": "docs.unarchive",
        "cli_path": "docs unarchive",
        "group": "docs",
        "method": "POST",
        "path": "/docs/{document_id}/unarchive",
        "operation_id": "unarchiveDocument",
        "summary": "Unarchive document",
        "why": "Clear archived_at on a document so it returns to default visibility.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "docs",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "examples": [
            {
                "title": "Unarchive a document",
                "command": "anx docs unarchive doc:runbook"
            }
        ],
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "docs.archive",
            "docs.comments.create",
            "docs.comments.list",
            "docs.comments.delete",
            "docs.comments.update",
            "docs.comments.reply",
            "docs.create",
            "docs.get",
            "docs.revisions.list",
            "docs.list",
            "docs.patch",
            "docs.purge",
            "docs.put",
            "docs.restore",
            "docs.revisions.create",
            "docs.revisions.get",
            "docs.search",
            "docs.trash"
        ],
        "go_method": "DocsUnarchive",
        "ts_method": "docsUnarchive"
    },
    {
        "command_id": "events.archive",
        "cli_path": "events archive",
        "group": "events",
        "method": "POST",
        "path": "/events/{event_id}/archive",
        "operation_id": "archiveEvent",
        "summary": "Archive event",
        "why": "Set archived_at on an append-only event record for filtered views.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ event }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "events",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "event_id"
        ],
        "adjacent_commands": [
            "events.create",
            "events.get",
            "events.list",
            "events.restore",
            "events.stream",
            "events.trash",
            "events.unarchive"
        ],
        "go_method": "EventsArchive",
        "ts_method": "eventsArchive"
    },
    {
        "command_id": "events.create",
        "cli_path": "events create",
        "group": "events",
        "method": "POST",
        "path": "/events",
        "operation_id": "createEvent",
        "summary": "Create event",
        "why": "Append an event that links first-class resources and evidence through typed refs.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ event }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "events",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "event.actor_id",
                    "type": "string"
                },
                {
                    "name": "event.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "event.refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "event.summary",
                    "type": "string"
                },
                {
                    "name": "event.type",
                    "type": "string",
                    "enum_values": [
                        "agent_notification_dismissed",
                        "agent_notification_read",
                        "board_created",
                        "board_updated",
                        "card_archived",
                        "card_created",
                        "card_moved",
                        "card_resolved",
                        "card_trashed",
                        "card_updated",
                        "document_created",
                        "document_restored",
                        "document_revised",
                        "document_trashed",
                        "exception_raised",
                        "human_attention_requested",
                        "human_attention_responded",
                        "human_attention_withdrawn",
                        "message_posted",
                        "receipt_added",
                        "review_completed",
                        "topic_archived",
                        "topic_created",
                        "topic_restored",
                        "topic_trashed",
                        "topic_updated"
                    ],
                    "enum_policy": "strict"
                }
            ],
            "optional": [
                {
                    "name": "event.handle",
                    "type": "string"
                },
                {
                    "name": "event.payload",
                    "type": "object"
                },
                {
                    "name": "event.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "event.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "event.ref",
                    "type": "typed_ref"
                },
                {
                    "name": "event.thread_ref",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "events.archive",
            "events.get",
            "events.list",
            "events.restore",
            "events.stream",
            "events.trash",
            "events.unarchive"
        ],
        "go_method": "EventsCreate",
        "ts_method": "eventsCreate"
    },
    {
        "command_id": "events.get",
        "cli_path": "events get",
        "group": "events",
        "method": "GET",
        "path": "/events/{event_id}",
        "operation_id": "getEventById",
        "summary": "Get event",
        "why": "Fetch one append-only event record by public ref or handle.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ event }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "events"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "event_id"
        ],
        "adjacent_commands": [
            "events.archive",
            "events.create",
            "events.list",
            "events.restore",
            "events.stream",
            "events.trash",
            "events.unarchive"
        ],
        "go_method": "EventsGet",
        "ts_method": "eventsGet"
    },
    {
        "command_id": "events.list",
        "cli_path": "events list",
        "group": "events",
        "method": "GET",
        "path": "/events",
        "operation_id": "listEvents",
        "summary": "List events",
        "why": "Inspect append-only event history across the workspace.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ events, page_info }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "events"
        ],
        "stability": "beta",
        "surface": "canonical",
        "adjacent_commands": [
            "events.archive",
            "events.create",
            "events.get",
            "events.restore",
            "events.stream",
            "events.trash",
            "events.unarchive"
        ],
        "go_method": "EventsList",
        "ts_method": "eventsList"
    },
    {
        "command_id": "events.restore",
        "cli_path": "events restore",
        "group": "events",
        "method": "POST",
        "path": "/events/{event_id}/restore",
        "operation_id": "restoreEvent",
        "summary": "Restore event from trash",
        "why": "Clear trash state on an event after an explicit restore action.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ event }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "events",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "event_id"
        ],
        "adjacent_commands": [
            "events.archive",
            "events.create",
            "events.get",
            "events.list",
            "events.stream",
            "events.trash",
            "events.unarchive"
        ],
        "go_method": "EventsRestore",
        "ts_method": "eventsRestore"
    },
    {
        "command_id": "events.stream",
        "cli_path": "events stream",
        "group": "events",
        "method": "GET",
        "path": "/stream/events",
        "operation_id": "streamEvents",
        "summary": "Stream events (SSE)",
        "description": "Starts at the current workspace head unless the supplied resume ID identifies a currently authorized-visible, untrashed event. Hidden, trashed and unknown IDs behave identically. Accepted IDs resume in chronological timestamp/ID order. Each tick reads pages of 200 candidates and silently skips empty pages up to a fixed 2000-candidate budget; at most one visible page is decoded. Hidden and nonmatching positions advance only a connection-local cursor. Keepalive comments follow the polling timer regardless of hidden activity. A `resume` SSE marker with empty JSON data follows every 200 delivered visible events and repeats that visible ID; it does not indicate hidden backlog or carry internal progress.",
        "why": "Long-lived SSE feed of workspace events with optional thread/type filters and Last-Event-ID resume.",
        "input_mode": "none",
        "streaming": {
            "mode": "sse"
        },
        "output_envelope": "Resource messages use `event: event` with JSON data `{ \"event\": \u003cevent\u003e }`. A `resume` control has empty JSON data and is excluded from delivered-event counts (see core/docs/http-api.md).",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "events"
        ],
        "stability": "beta",
        "surface": "canonical",
        "adjacent_commands": [
            "events.archive",
            "events.create",
            "events.get",
            "events.list",
            "events.restore",
            "events.trash",
            "events.unarchive"
        ],
        "go_method": "EventsStream",
        "ts_method": "eventsStream"
    },
    {
        "command_id": "events.trash",
        "cli_path": "events trash",
        "group": "events",
        "method": "POST",
        "path": "/events/{event_id}/trash",
        "operation_id": "trashEvent",
        "summary": "Move event to trash",
        "why": "Move event to trash with an explicit operator reason.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ event }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "events",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "reason",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "event_id"
        ],
        "adjacent_commands": [
            "events.archive",
            "events.create",
            "events.get",
            "events.list",
            "events.restore",
            "events.stream",
            "events.unarchive"
        ],
        "go_method": "EventsTrash",
        "ts_method": "eventsTrash"
    },
    {
        "command_id": "events.unarchive",
        "cli_path": "events unarchive",
        "group": "events",
        "method": "POST",
        "path": "/events/{event_id}/unarchive",
        "operation_id": "unarchiveEvent",
        "summary": "Unarchive event",
        "why": "Clear archived_at on an event.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ event }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "events",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "event_id"
        ],
        "adjacent_commands": [
            "events.archive",
            "events.create",
            "events.get",
            "events.list",
            "events.restore",
            "events.stream",
            "events.trash"
        ],
        "go_method": "EventsUnarchive",
        "ts_method": "eventsUnarchive"
    },
    {
        "command_id": "home.read",
        "cli_path": "",
        "method": "POST",
        "path": "/home/read",
        "operation_id": "markHomeRead",
        "summary": "Mark Home activity read",
        "why": "Advance durable per-group Home read cursors for the authenticated operator.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ ok, unread_count, group_count }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "home",
            "events",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "expected_newest_event_cursor.id",
                    "type": "string"
                },
                {
                    "name": "expected_newest_event_cursor.ts",
                    "type": "datetime"
                },
                {
                    "name": "group_cursors",
                    "type": "object"
                },
                {
                    "name": "group_ref",
                    "type": "string"
                },
                {
                    "name": "group_refs",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "reader_id",
                    "type": "string"
                }
            ]
        },
        "go_method": "HomeRead",
        "ts_method": "homeRead"
    },
    {
        "command_id": "home.unread",
        "cli_path": "",
        "method": "GET",
        "path": "/home/unread",
        "operation_id": "getHomeUnread",
        "summary": "Get Home unread activity",
        "why": "Load unread high-signal workspace activity grouped by typed group (topic, board, etc.) for the authenticated operator.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ groups, unread_count, group_count, generated_at }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "home",
            "events"
        ],
        "stability": "beta",
        "surface": "canonical",
        "go_method": "HomeUnread",
        "ts_method": "homeUnread"
    },
    {
        "command_id": "hosts.bridge.check_in",
        "cli_path": "host bridge check-in",
        "group": "host",
        "method": "POST",
        "path": "/hosts/{host_id}/bridge/check-in",
        "operation_id": "checkInHostBridge",
        "summary": "Check in host bridge",
        "description": "Host self-authentication through the three host proof headers. Sign UTF-8 `anx-host-bridge-check-in|\u003chost_id\u003e|\u003csigned_at\u003e|\u003cbase64url(SHA256(raw request body))\u003e` with the active host key. Timestamp skew is at most five minutes and message/signature replay is rejected. One bridge instance checks in for all enabled derived agents on the host; no per-agent bridge identity or key is accepted.",
        "why": "Record a host bridge heartbeat for wake routing.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ bridge }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "key_mismatch",
            "host_revoked",
            "not_found"
        ],
        "concepts": [
            "hosts",
            "agents"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Host proof is required; no agent bearer or per-agent bridge proof is accepted.",
        "body_schema": {
            "required": [
                {
                    "name": "bridge_instance_id",
                    "type": "string"
                },
                {
                    "name": "checked_in_at",
                    "type": "datetime"
                },
                {
                    "name": "expires_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "host_id"
        ],
        "adjacent_commands": [
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsBridgeCheckIn",
        "ts_method": "hostsBridgeCheckIn"
    },
    {
        "command_id": "hosts.enroll.approve",
        "cli_path": "host enrollments approve",
        "group": "host",
        "method": "POST",
        "path": "/auth/hosts/enrollments/{enrollment_id}/approve",
        "operation_id": "approveHostEnrollment",
        "summary": "Approve host enrollment",
        "description": "Human or explicitly granted auth-admin agent. Atomically reserves the host slug for this enrollment; approval does not deliver credentials or perform adoption.",
        "why": "Approve a verified machine.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `HostEnrollmentStatusResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "auth_admin_required",
            "not_found",
            "enrollment_expired",
            "enrollment_consumed",
            "host_slug_taken"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "path_params": [
            "enrollment_id"
        ],
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsEnrollApprove",
        "ts_method": "hostsEnrollApprove"
    },
    {
        "command_id": "hosts.enroll.complete",
        "cli_path": "host enroll complete",
        "group": "host",
        "method": "POST",
        "path": "/auth/hosts/enrollments/{enrollment_id}/complete",
        "operation_id": "completeHostEnrollment",
        "summary": "Complete approved host enrollment",
        "description": "Public ceremony. Requires the poll token and host-key proof; atomically creates the host and adopts the frozen, proved agent set. Single use; a denied or pending request cannot complete. No host bearer or refresh token is issued.",
        "why": "Finish an approved host enrollment.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ host }` with adopted agents.",
        "error_codes": [
            "invalid_request",
            "invalid_token",
            "enrollment_pending",
            "enrollment_denied",
            "enrollment_expired",
            "enrollment_consumed",
            "adoption_conflict",
            "host_slug_taken",
            "not_found"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "body_schema": {
            "required": [
                {
                    "name": "poll_token",
                    "type": "string"
                },
                {
                    "name": "signature",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "enrollment_id"
        ],
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsEnrollComplete",
        "ts_method": "hostsEnrollComplete"
    },
    {
        "command_id": "hosts.enroll.deny",
        "cli_path": "host enrollments deny",
        "group": "host",
        "method": "POST",
        "path": "/auth/hosts/enrollments/{enrollment_id}/deny",
        "operation_id": "denyHostEnrollment",
        "summary": "Deny host enrollment",
        "description": "Human or explicitly granted auth-admin agent. Denies a pending request or cancels an approved ceremony before completion; terminal and audited.",
        "why": "Reject an untrusted machine.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `HostEnrollmentStatusResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "auth_admin_required",
            "not_found",
            "enrollment_expired",
            "enrollment_consumed"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "path_params": [
            "enrollment_id"
        ],
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsEnrollDeny",
        "ts_method": "hostsEnrollDeny"
    },
    {
        "command_id": "hosts.enroll.headless",
        "cli_path": "host enroll headless",
        "group": "host",
        "method": "POST",
        "path": "/auth/hosts/enrollments/headless",
        "operation_id": "completeHeadlessHostEnrollment",
        "summary": "Enroll host using headless token",
        "description": "Public single-request ceremony. Atomically verifies and consumes the auth-admin-created token, verifies the host key and adoption proofs, and creates the host. A failed request does not consume the token.",
        "why": "Enroll a CI or cloud host without polling.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ host }`; never issues host bearer credentials.",
        "error_codes": [
            "invalid_request",
            "invalid_token",
            "host_slug_taken",
            "adoption_proof_invalid",
            "adoption_conflict"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "body_schema": {
            "required": [
                {
                    "name": "adoptions",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "discovered_adapters",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "enrollment_token",
                    "type": "string"
                },
                {
                    "name": "hostname",
                    "type": "string"
                },
                {
                    "name": "os_user",
                    "type": "string"
                },
                {
                    "name": "public_key",
                    "type": "string"
                },
                {
                    "name": "request_nonce",
                    "type": "string"
                },
                {
                    "name": "requested_slug",
                    "type": "string"
                },
                {
                    "name": "signature",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsEnrollHeadless",
        "ts_method": "hostsEnrollHeadless"
    },
    {
        "command_id": "hosts.enroll.pending",
        "cli_path": "host enrollments list",
        "group": "host",
        "method": "GET",
        "path": "/auth/hosts/enrollments/pending",
        "operation_id": "listPendingHostEnrollments",
        "summary": "List pending host approvals",
        "description": "Human or explicitly granted auth-admin agent. Lists pending and approved ceremonies awaiting completion, so an approval can be canceled. Includes the requested slug, OS user, hostname, discovered adapters, adopted agent names, requesting IP and expiry; never returns poll tokens or proofs.",
        "why": "Review host enrollment requests.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ enrollments }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "auth_admin_required"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsEnrollPending",
        "ts_method": "hostsEnrollPending"
    },
    {
        "command_id": "hosts.enroll.poll",
        "cli_path": "host enroll poll",
        "group": "host",
        "method": "GET",
        "path": "/auth/hosts/enrollments/{enrollment_id}",
        "operation_id": "pollHostEnrollment",
        "summary": "Poll enrollment status",
        "description": "Public ceremony authenticated by the high-entropy X-ANX-Enrollment-Token header. Returns pending, approved, denied, expired, or completed; never exposes the approval session or host credentials.",
        "why": "Wait for an auth-admin to decide the host request.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `HostEnrollmentStatusResponse`.",
        "error_codes": [
            "invalid_request",
            "invalid_token",
            "enrollment_expired",
            "not_found"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "path_params": [
            "enrollment_id"
        ],
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsEnrollPoll",
        "ts_method": "hostsEnrollPoll"
    },
    {
        "command_id": "hosts.enroll.start",
        "cli_path": "host enroll start",
        "group": "host",
        "method": "POST",
        "path": "/auth/hosts/enrollments",
        "operation_id": "startHostEnrollment",
        "summary": "Start interactive host enrollment",
        "description": "Public ceremony. Does not create a principal. The enrollment and user code expire after 10 minutes; core records requesting IP for the approval view. Adoption proofs are checked before approval and frozen with the request.",
        "why": "Obtain a user code for human host approval.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `HostEnrollmentStartResponse`; poll_token is secret and shown only once.",
        "error_codes": [
            "invalid_request",
            "host_slug_taken",
            "adoption_proof_invalid",
            "adoption_conflict",
            "enrollment_capacity",
            "rate_limited"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "body_schema": {
            "required": [
                {
                    "name": "adoptions",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "discovered_adapters",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "hostname",
                    "type": "string"
                },
                {
                    "name": "os_user",
                    "type": "string"
                },
                {
                    "name": "public_key",
                    "type": "string"
                },
                {
                    "name": "request_nonce",
                    "type": "string"
                },
                {
                    "name": "requested_slug",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsEnrollStart",
        "ts_method": "hostsEnrollStart"
    },
    {
        "command_id": "hosts.get",
        "cli_path": "host get",
        "group": "host",
        "method": "GET",
        "path": "/hosts/{host_id}",
        "operation_id": "getHost",
        "summary": "Get host and derived agents",
        "description": "Any authenticated workspace principal. Returns excluded names and derived agents, including adopted principals, even when revoked.",
        "why": "Inspect one machine and its agents.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ host }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "hosts",
            "agents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "path_params": [
            "host_id"
        ],
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsGet",
        "ts_method": "hostsGet"
    },
    {
        "command_id": "hosts.list",
        "cli_path": "host list",
        "group": "host",
        "method": "GET",
        "path": "/hosts",
        "operation_id": "listHosts",
        "summary": "List workspace hosts",
        "description": "Any authenticated workspace principal. Includes revoked hosts for audit, never public keys or secrets beyond the host key ID.",
        "why": "Inspect enrolled machines.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ hosts }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "hosts",
            "agents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsList",
        "ts_method": "hostsList"
    },
    {
        "command_id": "hosts.patch",
        "cli_path": "host patch",
        "group": "host",
        "method": "PATCH",
        "path": "/hosts/{host_id}",
        "operation_id": "patchHost",
        "summary": "Update host display name or exclusions",
        "description": "Human auth-admin bearer or this host via the three host proof headers; derived-agent bearer tokens cannot edit the host. Omitted fields remain unchanged. Replacing excluded_names invalidates outstanding sessions for newly excluded agents while preserving their actor/history.",
        "why": "Name a host or block agent names on it.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ host }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "key_mismatch",
            "host_revoked",
            "not_found"
        ],
        "concepts": [
            "hosts",
            "agents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "body_schema": {
            "optional": [
                {
                    "name": "display_name",
                    "type": "string"
                },
                {
                    "name": "excluded_names",
                    "type": "list\u003cany\u003e"
                }
            ]
        },
        "path_params": [
            "host_id"
        ],
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsPatch",
        "ts_method": "hostsPatch"
    },
    {
        "command_id": "hosts.revoke",
        "cli_path": "host revoke",
        "group": "host",
        "method": "DELETE",
        "path": "/hosts/{host_id}",
        "operation_id": "revokeHost",
        "summary": "Revoke host and all derived agents",
        "description": "Human or explicitly granted auth-admin agent. Agents cannot revoke their own host (host_self_revoke). In one transaction revoke host, every child principal/key/session/token, and record one audit event. Idempotent; retained records remain visible to reads.",
        "why": "Cut off a compromised machine.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ host }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "auth_admin_required",
            "host_self_revoke",
            "not_found"
        ],
        "concepts": [
            "hosts",
            "auth"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "path_params": [
            "host_id"
        ],
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.tokens.create",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsRevoke",
        "ts_method": "hostsRevoke"
    },
    {
        "command_id": "hosts.tokens.create",
        "cli_path": "host tokens create",
        "group": "host",
        "method": "POST",
        "path": "/auth/hosts/enrollment-tokens",
        "operation_id": "createHostEnrollmentToken",
        "summary": "Create headless enrollment token",
        "description": "Human or explicitly granted auth-admin agent. One-time secret shown only in this response; expires within 10 minutes to 24 hours.",
        "why": "Authorize one headless host enrollment.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ enrollment_token, token }` once.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "auth_admin_required",
            "invalid_request"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "body_schema": {
            "required": [
                {
                    "name": "label",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "expires_at",
                    "type": "datetime"
                },
                {
                    "name": "expires_in_seconds",
                    "type": "integer"
                }
            ]
        },
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.list",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsTokensCreate",
        "ts_method": "hostsTokensCreate"
    },
    {
        "command_id": "hosts.tokens.list",
        "cli_path": "host tokens list",
        "group": "host",
        "method": "GET",
        "path": "/auth/hosts/enrollment-tokens",
        "operation_id": "listHostEnrollmentTokens",
        "summary": "List headless enrollment tokens",
        "description": "Human or explicitly granted auth-admin agent. Token secrets are never returned by list.",
        "why": "Inspect headless host enrollment grants.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ enrollment_tokens }` without secrets.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "auth_admin_required"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.revoke"
        ],
        "go_method": "HostsTokensList",
        "ts_method": "hostsTokensList"
    },
    {
        "command_id": "hosts.tokens.revoke",
        "cli_path": "host tokens revoke",
        "group": "host",
        "method": "POST",
        "path": "/auth/hosts/enrollment-tokens/{token_id}/revoke",
        "operation_id": "revokeHostEnrollmentToken",
        "summary": "Revoke headless enrollment token",
        "description": "Human or explicitly granted auth-admin agent; consumed tokens remain consumed.",
        "why": "Invalidate an unused headless grant.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ enrollment_token }` without secret.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "auth_admin_required",
            "not_found"
        ],
        "concepts": [
            "auth",
            "hosts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "path_params": [
            "token_id"
        ],
        "adjacent_commands": [
            "hosts.bridge.check_in",
            "hosts.enroll.complete",
            "hosts.enroll.headless",
            "hosts.enroll.poll",
            "hosts.enroll.start",
            "hosts.enroll.approve",
            "hosts.enroll.deny",
            "hosts.enroll.pending",
            "hosts.get",
            "hosts.list",
            "hosts.patch",
            "hosts.revoke",
            "hosts.tokens.create",
            "hosts.tokens.list"
        ],
        "go_method": "HostsTokensRevoke",
        "ts_method": "hostsTokensRevoke"
    },
    {
        "command_id": "inbox.get",
        "cli_path": "inbox get",
        "group": "inbox",
        "method": "GET",
        "path": "/inbox/{inbox_id}",
        "operation_id": "getInboxItem",
        "summary": "Get one inbox item",
        "description": "Authorizes the inbox subject, related refs, backing card and board before returning the item.",
        "why": "Side-effect free read of one materialized inbox row.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ item, generated_at, projection_freshness }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "inbox"
        ],
        "stability": "beta",
        "surface": "projection",
        "path_params": [
            "inbox_id"
        ],
        "adjacent_commands": [
            "inbox.list",
            "inbox.respond",
            "inbox.stream",
            "inbox.summary"
        ],
        "go_method": "InboxGet",
        "ts_method": "inboxGet"
    },
    {
        "command_id": "inbox.list",
        "cli_path": "inbox list",
        "group": "inbox",
        "method": "GET",
        "path": "/inbox",
        "operation_id": "listInboxItems",
        "summary": "List inbox items",
        "why": "Project human_attention_requested events into a queryable inbox view.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ status, items, generated_at }`; completed adds `{ next_cursor }`; open projection adds `{ projection_freshness }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "surface": "projection",
        "adjacent_commands": [
            "inbox.get",
            "inbox.respond",
            "inbox.stream",
            "inbox.summary"
        ],
        "go_method": "InboxList",
        "ts_method": "inboxList"
    },
    {
        "command_id": "inbox.respond",
        "cli_path": "inbox respond",
        "group": "inbox",
        "method": "POST",
        "path": "/inbox/{inbox_id}/respond",
        "operation_id": "respondInboxItem",
        "summary": "Respond to human attention inbox item",
        "description": "For a server-linked access request, approved atomically grants the persisted requested grant and rejected denies it. Other outcomes return 400 invalid_request without mutation. Ordinary attention event metadata never authorizes a grant.",
        "why": "A human principal records one response per request, closes the human attention item, and optionally notifies the selected requester/replacement agent.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ event, notify }`.",
        "error_codes": [
            "auth_required",
            "human_required",
            "invalid_request",
            "invalid_token",
            "notification_target_required",
            "not_found",
            "conflict",
            "idempotency_conflict"
        ],
        "concepts": [
            "inbox",
            "write"
        ],
        "stability": "beta",
        "surface": "projection",
        "body_schema": {
            "required": [
                {
                    "name": "outcome",
                    "type": "string",
                    "enum_values": [
                        "acknowledged",
                        "answered",
                        "approved",
                        "rejected"
                    ]
                },
                {
                    "name": "response_text",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "idempotency_key",
                    "type": "string"
                },
                {
                    "name": "inbox_item_id",
                    "type": "string"
                },
                {
                    "name": "notify_mode",
                    "type": "string",
                    "enum_values": [
                        "none",
                        "original",
                        "replacement"
                    ]
                },
                {
                    "name": "notify_target_actor_id",
                    "type": "string"
                },
                {
                    "name": "notify_target_agent_id",
                    "type": "string"
                },
                {
                    "name": "related_refs",
                    "type": "list\u003cany\u003e"
                }
            ]
        },
        "path_params": [
            "inbox_id"
        ],
        "adjacent_commands": [
            "inbox.get",
            "inbox.list",
            "inbox.stream",
            "inbox.summary"
        ],
        "go_method": "InboxRespond",
        "ts_method": "inboxRespond"
    },
    {
        "command_id": "inbox.stream",
        "cli_path": "inbox stream",
        "group": "inbox",
        "method": "GET",
        "path": "/stream/inbox",
        "operation_id": "streamInboxItems",
        "summary": "Stream inbox items (SSE)",
        "why": "Server-sent events feed of inbox projection updates.",
        "input_mode": "none",
        "streaming": {
            "mode": "sse"
        },
        "output_envelope": "SSE `inbox_item` events with JSON payloads.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "inbox"
        ],
        "stability": "beta",
        "surface": "projection",
        "adjacent_commands": [
            "inbox.get",
            "inbox.list",
            "inbox.respond",
            "inbox.summary"
        ],
        "go_method": "InboxStream",
        "ts_method": "inboxStream"
    },
    {
        "command_id": "inbox.summary",
        "cli_path": "inbox summary",
        "group": "inbox",
        "method": "GET",
        "path": "/inbox/summary",
        "operation_id": "getInboxSummary",
        "summary": "Count open asks and return the top asks visible to the caller",
        "why": "Cheap workspace-local human attention read for UI fan-out across existing workspace sessions.",
        "input_mode": "query",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ open_ask_count, asks, generated_at }`; shared human asks visible to the caller, not agent answer notifications.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "inbox"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Uses current workspace permissions; limit 0 returns the count only. No cross-workspace identity.",
        "adjacent_commands": [
            "inbox.get",
            "inbox.list",
            "inbox.respond",
            "inbox.stream"
        ],
        "go_method": "InboxSummary",
        "ts_method": "inboxSummary"
    },
    {
        "command_id": "meta.commands.get",
        "cli_path": "meta commands get",
        "group": "meta",
        "method": "GET",
        "path": "/meta/commands/{command_id}",
        "operation_id": "getMetaCommandById",
        "summary": "Get one command metadata entry",
        "why": "Resolve command metadata by stable command id.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ command }`.",
        "error_codes": [
            "meta_unavailable",
            "not_found"
        ],
        "concepts": [
            "compatibility"
        ],
        "stability": "stable",
        "surface": "utility",
        "path_params": [
            "command_id"
        ],
        "adjacent_commands": [
            "meta.commands.list",
            "meta.concepts.get",
            "meta.concepts.list",
            "meta.handshake",
            "meta.health",
            "meta.livez",
            "meta.readyz",
            "meta.version"
        ],
        "go_method": "MetaCommandsGet",
        "ts_method": "metaCommandsGet"
    },
    {
        "command_id": "meta.commands.list",
        "cli_path": "meta commands list",
        "group": "meta",
        "method": "GET",
        "path": "/meta/commands",
        "operation_id": "listMetaCommands",
        "summary": "List command registry metadata",
        "why": "Expose embedded Agent Nexus command metadata for discovery and codegen parity.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns generated command registry JSON.",
        "error_codes": [
            "meta_unavailable"
        ],
        "concepts": [
            "compatibility"
        ],
        "stability": "stable",
        "surface": "utility",
        "adjacent_commands": [
            "meta.commands.get",
            "meta.concepts.get",
            "meta.concepts.list",
            "meta.handshake",
            "meta.health",
            "meta.livez",
            "meta.readyz",
            "meta.version"
        ],
        "go_method": "MetaCommandsList",
        "ts_method": "metaCommandsList"
    },
    {
        "command_id": "meta.concepts.get",
        "cli_path": "meta concepts get",
        "group": "meta",
        "method": "GET",
        "path": "/meta/concepts/{concept_name}",
        "operation_id": "getMetaConceptByName",
        "summary": "Get commands grouped by concept",
        "why": "Expand one concept into related commands.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ concept: {...} }`.",
        "error_codes": [
            "meta_unavailable",
            "not_found"
        ],
        "concepts": [
            "compatibility"
        ],
        "stability": "stable",
        "surface": "utility",
        "path_params": [
            "concept_name"
        ],
        "adjacent_commands": [
            "meta.commands.get",
            "meta.commands.list",
            "meta.concepts.list",
            "meta.handshake",
            "meta.health",
            "meta.livez",
            "meta.readyz",
            "meta.version"
        ],
        "go_method": "MetaConceptsGet",
        "ts_method": "metaConceptsGet"
    },
    {
        "command_id": "meta.concepts.list",
        "cli_path": "meta concepts list",
        "group": "meta",
        "method": "GET",
        "path": "/meta/concepts",
        "operation_id": "listMetaConcepts",
        "summary": "List concept index",
        "why": "Group command metadata by concept tags.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ concepts: [...] }`.",
        "error_codes": [
            "meta_unavailable"
        ],
        "concepts": [
            "compatibility"
        ],
        "stability": "stable",
        "surface": "utility",
        "adjacent_commands": [
            "meta.commands.get",
            "meta.commands.list",
            "meta.concepts.get",
            "meta.handshake",
            "meta.health",
            "meta.livez",
            "meta.readyz",
            "meta.version"
        ],
        "go_method": "MetaConceptsList",
        "ts_method": "metaConceptsList"
    },
    {
        "command_id": "meta.handshake",
        "cli_path": "meta handshake",
        "group": "meta",
        "method": "GET",
        "path": "/meta/handshake",
        "operation_id": "getMetaHandshake",
        "summary": "Compatibility handshake",
        "why": "Surface schema version, command registry digest, CLI gates, and instance metadata.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ core_version, api_version, schema_version, command_registry_digest, ... }`.",
        "error_codes": [
            "meta_unavailable"
        ],
        "concepts": [
            "compatibility"
        ],
        "stability": "stable",
        "surface": "utility",
        "adjacent_commands": [
            "meta.commands.get",
            "meta.commands.list",
            "meta.concepts.get",
            "meta.concepts.list",
            "meta.health",
            "meta.livez",
            "meta.readyz",
            "meta.version"
        ],
        "go_method": "MetaHandshake",
        "ts_method": "metaHandshake"
    },
    {
        "command_id": "meta.health",
        "cli_path": "meta health",
        "group": "meta",
        "method": "GET",
        "path": "/health",
        "operation_id": "healthCheck",
        "summary": "Liveness check",
        "why": "Probe whether the core process is alive.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ ok: true }`.",
        "concepts": [
            "health"
        ],
        "stability": "stable",
        "surface": "utility",
        "adjacent_commands": [
            "meta.commands.get",
            "meta.commands.list",
            "meta.concepts.get",
            "meta.concepts.list",
            "meta.handshake",
            "meta.livez",
            "meta.readyz",
            "meta.version"
        ],
        "go_method": "MetaHealth",
        "ts_method": "metaHealth"
    },
    {
        "command_id": "meta.livez",
        "cli_path": "meta livez",
        "group": "meta",
        "method": "GET",
        "path": "/livez",
        "operation_id": "livenessCheck",
        "summary": "Liveness check (process)",
        "why": "Lightweight liveness probe independent of storage readiness.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ ok: true }`.",
        "concepts": [
            "health"
        ],
        "stability": "stable",
        "surface": "utility",
        "adjacent_commands": [
            "meta.commands.get",
            "meta.commands.list",
            "meta.concepts.get",
            "meta.concepts.list",
            "meta.handshake",
            "meta.health",
            "meta.readyz",
            "meta.version"
        ],
        "go_method": "MetaLivez",
        "ts_method": "metaLivez"
    },
    {
        "command_id": "meta.readyz",
        "cli_path": "meta readyz",
        "group": "meta",
        "method": "GET",
        "path": "/readyz",
        "operation_id": "readinessCheck",
        "summary": "Readiness check",
        "why": "Verify storage and projection subsystems are ready for traffic.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ ok: true }` when the workspace is ready.",
        "error_codes": [
            "storage_unavailable"
        ],
        "concepts": [
            "health",
            "readiness"
        ],
        "stability": "stable",
        "surface": "utility",
        "adjacent_commands": [
            "meta.commands.get",
            "meta.commands.list",
            "meta.concepts.get",
            "meta.concepts.list",
            "meta.handshake",
            "meta.health",
            "meta.livez",
            "meta.version"
        ],
        "go_method": "MetaReadyz",
        "ts_method": "metaReadyz"
    },
    {
        "command_id": "meta.version",
        "cli_path": "meta version",
        "group": "meta",
        "method": "GET",
        "path": "/version",
        "operation_id": "getVersion",
        "summary": "Get contract version",
        "why": "Check compatibility between clients and core before writes.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ schema_version, command_registry_digest }`.",
        "concepts": [
            "compatibility"
        ],
        "stability": "stable",
        "surface": "utility",
        "adjacent_commands": [
            "meta.commands.get",
            "meta.commands.list",
            "meta.concepts.get",
            "meta.concepts.list",
            "meta.handshake",
            "meta.health",
            "meta.livez",
            "meta.readyz"
        ],
        "go_method": "MetaVersion",
        "ts_method": "metaVersion"
    },
    {
        "command_id": "ops.blob.usage.rebuild",
        "cli_path": "ops blob usage rebuild",
        "group": "ops",
        "method": "POST",
        "path": "/ops/blob-usage/rebuild",
        "operation_id": "rebuildBlobUsage",
        "summary": "Rebuild blob usage accounting",
        "why": "Repair path for derived blob accounting after storage events.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ ok: true }` or error.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "ops",
            "maintenance"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "ops.health",
            "ops.usage.summary"
        ],
        "go_method": "OpsBlobUsageRebuild",
        "ts_method": "opsBlobUsageRebuild"
    },
    {
        "command_id": "ops.health",
        "cli_path": "ops health",
        "group": "ops",
        "method": "GET",
        "path": "/ops/health",
        "operation_id": "getOpsHealth",
        "summary": "Workspace ops health",
        "why": "Operational readiness for projections, jobs, and operators.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns structured health JSON.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "health",
            "ops"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "ops.blob.usage.rebuild",
            "ops.usage.summary"
        ],
        "go_method": "OpsHealth",
        "ts_method": "opsHealth"
    },
    {
        "command_id": "ops.usage.summary",
        "cli_path": "ops usage summary",
        "group": "ops",
        "method": "GET",
        "path": "/ops/usage-summary",
        "operation_id": "getOpsUsageSummary",
        "summary": "Workspace usage summary",
        "why": "Operator-facing storage and count telemetry for the workspace.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns usage envelope JSON.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "ops",
            "quotas"
        ],
        "stability": "beta",
        "surface": "utility",
        "adjacent_commands": [
            "ops.blob.usage.rebuild",
            "ops.health"
        ],
        "go_method": "OpsUsageSummary",
        "ts_method": "opsUsageSummary"
    },
    {
        "command_id": "overview.changes",
        "cli_path": "overview changes",
        "group": "overview",
        "method": "GET",
        "path": "/overview/changes",
        "operation_id": "getOverviewChanges",
        "summary": "Read bounded changes since this principal last viewed Overview",
        "why": "Read a compact digest of completed steps, health transitions, answered asks and new decisions.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns the principal-scoped bounded change digest.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "home",
            "cards"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Read-only; never advances the baseline. First visit returns null since and empty items.",
        "adjacent_commands": [
            "overview.get"
        ],
        "go_method": "OverviewChanges",
        "ts_method": "overviewChanges"
    },
    {
        "command_id": "overview.get",
        "cli_path": "overview",
        "group": "overview",
        "method": "GET",
        "path": "/overview",
        "operation_id": "getOverview",
        "summary": "Read the same active-work Overview projection used by the web UI.",
        "why": "Read the same active-work Overview projection used by the web UI.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns the workspace Overview projection.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "home",
            "documents",
            "cards"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Includes since_you_last_looked against the prior visit, then records the authenticated viewer baseline. Read /overview/changes without advancing it.",
        "adjacent_commands": [
            "overview.changes"
        ],
        "go_method": "OverviewGet",
        "ts_method": "overviewGet"
    },
    {
        "command_id": "plan.set",
        "cli_path": "plan set",
        "group": "plan",
        "method": "PUT",
        "path": "/cards/{card_id}/plan",
        "operation_id": "setCardPlan",
        "summary": "Replace a card plan with an audited edit",
        "why": "Add linked steps rather than writing progress prose. Never select a view.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns the current principal-scoped read projection.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "cards",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "One plan per initiative. CLI --from-file contains {steps:[...]}; the CLI reads the card token before writing, or accepts --if-updated-at. Step helpers compose the same PUT. Conflicts require reconciliation; never choose a view.",
        "body_schema": {
            "required": [
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                },
                {
                    "name": "plan.steps",
                    "type": "list\u003cany\u003e"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "plan.show"
        ],
        "go_method": "PlanSet",
        "ts_method": "planSet"
    },
    {
        "command_id": "plan.show",
        "cli_path": "plan show",
        "group": "plan",
        "method": "GET",
        "path": "/cards/{card_id}/plan",
        "operation_id": "showCardPlan",
        "summary": "Read a card plan and computed state",
        "why": "Read live initiative steps, progress and health.",
        "input_mode": "flags",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns the current principal-scoped read projection.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "cards",
            "read"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Read plan_state for effective status, progress, shape, critical path and health. An unset plan returns null plan and plan_state.",
        "path_params": [
            "card_id"
        ],
        "adjacent_commands": [
            "plan.set"
        ],
        "go_method": "PlanShow",
        "ts_method": "planShow"
    },
    {
        "command_id": "pm.actions.acknowledge",
        "cli_path": "pm actions acknowledge",
        "group": "pm",
        "method": "POST",
        "path": "/pm/actions/{action_id}/acknowledge",
        "operation_id": "pmActionsAcknowledge",
        "summary": "Acknowledge a failed, unresolvable, or undeliverable action",
        "description": "Only the decision actor may acknowledge a failed action, an unknown action whose current read-back cannot advance, or a pending_delivery action with deliverable false. Acknowledging an undeliverable action retains the approval and no-delivery-path detail, places it under Handled, and prevents delivery even if routing later becomes available; a fresh proposal and approval are required. Idempotent. Human acknowledgement does not block later read-only reconciliation; advancing source results update status and receipt while preserving acknowledged_by and acknowledged_at. Failed actions without a sent attempt remain unreconcilable after acknowledgement. Sets acknowledged_by and acknowledged_at and visible status acknowledged preserving existing receipts and attempts; undeliverable pending actions set closed_without_delivery true and receipt.detail to \"Closed without delivery: no delivery path is configured for \u003csource\u003e; nothing was sent.\". Other states return 409; other actors return 403.",
        "why": "Acknowledge a failed, unresolvable, or undeliverable action.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMAction`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "path_params": [
            "action_id"
        ],
        "adjacent_commands": [
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmActionsAcknowledge",
        "ts_method": "pmActionsAcknowledge"
    },
    {
        "command_id": "pm.actions.get",
        "cli_path": "pm actions get",
        "group": "pm",
        "method": "GET",
        "path": "/pm/actions/{action_id}",
        "operation_id": "pmActionsGet",
        "summary": "Read an action and its receipts",
        "why": "Read an action and its receipts.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMAction`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "path_params": [
            "action_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmActionsGet",
        "ts_method": "pmActionsGet"
    },
    {
        "command_id": "pm.actions.list",
        "cli_path": "pm actions list",
        "group": "pm",
        "method": "GET",
        "path": "/pm/actions",
        "operation_id": "pmActionsList",
        "summary": "List action receipts and attempts",
        "description": "Newest first by created_at descending, then internal rowid descending. Opaque cursors retain both ordering values so newer inserts do not shift subsequent pages. Pagination applies permission filtering before deriving has_more and next_cursor. Legacy actions without created_at use their decision creation time.",
        "why": "List action receipts and attempts.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMActionListResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmActionsList",
        "ts_method": "pmActionsList"
    },
    {
        "command_id": "pm.actions.reconcile",
        "cli_path": "pm actions reconcile",
        "group": "pm",
        "method": "POST",
        "path": "/pm/actions/{action_id}/reconcile",
        "operation_id": "pmActionsReconcile",
        "summary": "Read back an action outcome without resending",
        "description": "Missing work (including trashed or archived work) returns 409 source_revision_changed with reason work_missing, approved_revision, current_revision null, and origin_kind and proposed_by copied from the decision before any source call. The missing-work message is \"Nothing was delivered for this approval: the task it refers to no longer exists, so there is nothing to read back.\" Other unsent failed actions retain the existing \"Nothing has been delivered yet, so there is nothing to read back\" refusal. Human acknowledgement does not stop read-only reconciliation. Advancing source results update action status and receipt while retaining acknowledged_by, acknowledged_at, and attempts. Missing work never changes durable state during reconciliation or resurrects an action failed before send. Unsent failed actions and acknowledged undeliverable pending actions remain unreconcilable.",
        "why": "Read back an action outcome without resending.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMAction`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "path_params": [
            "action_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmActionsReconcile",
        "ts_method": "pmActionsReconcile"
    },
    {
        "command_id": "pm.bindings.create",
        "cli_path": "pm bindings create",
        "group": "pm",
        "method": "POST",
        "path": "/pm/bindings",
        "operation_id": "pmBindingsCreate",
        "summary": "Bind an exact channel identity to a workspace principal",
        "why": "Bind an exact channel identity to a workspace principal.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMBinding`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "can_approve",
                    "type": "boolean"
                },
                {
                    "name": "created_at",
                    "type": "string"
                },
                {
                    "name": "enabled",
                    "type": "boolean"
                },
                {
                    "name": "id",
                    "type": "string"
                },
                {
                    "name": "origin.channel_id",
                    "type": "string"
                },
                {
                    "name": "origin.external_user_id",
                    "type": "string"
                },
                {
                    "name": "origin.tenant_id",
                    "type": "string"
                },
                {
                    "name": "origin.thread_id",
                    "type": "string"
                },
                {
                    "name": "origin.transport",
                    "type": "string"
                },
                {
                    "name": "revision",
                    "type": "integer"
                },
                {
                    "name": "work_ref",
                    "type": "string"
                },
                {
                    "name": "workspace_id",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmBindingsCreate",
        "ts_method": "pmBindingsCreate"
    },
    {
        "command_id": "pm.bindings.list",
        "cli_path": "pm bindings list",
        "group": "pm",
        "method": "GET",
        "path": "/pm/bindings",
        "operation_id": "pmBindingsList",
        "summary": "List channel identity bindings in the workspace",
        "why": "Show which exact channel identities may talk to the PM, and with what authority, without sending anything.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMBindingListResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. A binding is an operator mapping, not proof that the channel is configured or reachable; `anx pm channels doctor` checks configuration without sending.",
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmBindingsList",
        "ts_method": "pmBindingsList"
    },
    {
        "command_id": "pm.context",
        "cli_path": "pm context",
        "group": "pm",
        "method": "GET",
        "path": "/pm/context",
        "operation_id": "pmContext",
        "summary": "Read bounded authorized PM context",
        "why": "Read bounded authorized PM context.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMContextResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmContext",
        "ts_method": "pmContext"
    },
    {
        "command_id": "pm.conversations.create",
        "cli_path": "pm conversations create",
        "group": "pm",
        "method": "POST",
        "path": "/pm/conversations",
        "operation_id": "pmConversationsCreate",
        "summary": "Create a durable PM conversation",
        "why": "Create a durable PM conversation.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMConversation`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "body_schema": {
            "required": [
                {
                    "name": "request_key",
                    "type": "string"
                },
                {
                    "name": "title",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "work_ref",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmConversationsCreate",
        "ts_method": "pmConversationsCreate"
    },
    {
        "command_id": "pm.conversations.get",
        "cli_path": "pm conversations get",
        "group": "pm",
        "method": "GET",
        "path": "/pm/conversations/{conversation_id}",
        "operation_id": "pmConversationsGet",
        "summary": "Read PM conversation and turns",
        "why": "Read PM conversation and turns.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMConversationDetailResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "path_params": [
            "conversation_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmConversationsGet",
        "ts_method": "pmConversationsGet"
    },
    {
        "command_id": "pm.conversations.list",
        "cli_path": "pm conversations list",
        "group": "pm",
        "method": "GET",
        "path": "/pm/conversations",
        "operation_id": "pmConversationsList",
        "summary": "List PM conversations",
        "description": "Newest first by created_at descending, then internal rowid descending. Opaque cursors retain both ordering values so newer inserts do not shift subsequent pages. Pagination applies permission filtering before deriving has_more and next_cursor.",
        "why": "List PM conversations.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMConversationListResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmConversationsList",
        "ts_method": "pmConversationsList"
    },
    {
        "command_id": "pm.conversations.messages.create",
        "cli_path": "pm conversations messages create",
        "group": "pm",
        "method": "POST",
        "path": "/pm/conversations/{conversation_id}/messages",
        "operation_id": "pmConversationsMessagesCreate",
        "summary": "Queue a contextual PM turn",
        "why": "Queue a contextual PM turn.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMTurn`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Channel ingress uses this same turn pipeline; Telegram and Discord messages become turns with `origin` set. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "body_schema": {
            "required": [
                {
                    "name": "request_key",
                    "type": "string"
                },
                {
                    "name": "text",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "conversation_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmConversationsMessagesCreate",
        "ts_method": "pmConversationsMessagesCreate"
    },
    {
        "command_id": "pm.decisions.answer",
        "cli_path": "pm decisions answer",
        "group": "pm",
        "method": "POST",
        "path": "/pm/decisions/{decision_id}/answer",
        "operation_id": "pmDecisionsAnswer",
        "summary": "Answer and authorize a scoped decision",
        "description": "Fresh approval requires target_revision to equal current Work.decision_revision and work not already at the payload phase. Stale, missing (including trashed or archived), already-at-target, or unreadable work returns 409 source_revision_changed without recording an answer or action. The refusal message is \"Proposal target has changed (proposed at \u003crev\u003e, source now \u003crev|unavailable\u003e). Approval refused (\u003creason\u003e); decline it or wait for a fresh proposal.\" Details retain approved_revision (the proposed target revision), current_revision (null if unavailable), reason (revision_changed, work_missing, already_at_target, or work_read_failed), origin_kind, and proposed_by. Every source_revision_changed response on answer, dispatch, or reconcile includes origin_kind and proposed_by as strings copied from the decision (empty for legacy decisions without provenance); origin_kind identifies human, channel, or pm_turn proposals and proposed_by identifies the proposing actor, so clients can direct reproposal to the board or the PM truthfully. Decline remains allowed in all these cases. Identical recorded answers replay with 200 even after work changes; different answers or decision revision conflicts remain 409.",
        "why": "Answer and authorize a scoped decision.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMDecision`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "body_schema": {
            "required": [
                {
                    "name": "approve",
                    "type": "boolean"
                },
                {
                    "name": "revision",
                    "type": "integer"
                },
                {
                    "name": "text",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "decision_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmDecisionsAnswer",
        "ts_method": "pmDecisionsAnswer"
    },
    {
        "command_id": "pm.decisions.create",
        "cli_path": "pm decisions create",
        "group": "pm",
        "method": "POST",
        "path": "/pm/decisions",
        "operation_id": "pmDecisionsCreate",
        "summary": "Propose a scoped PM decision",
        "description": "Proposals require live work; missing, trashed, or archived work returns 404 not_found after principal authorization. For work.annotate, instruction must be a nonempty JSON object containing only labels, roles, project_ref, priority, next_actor, next_action, blockers, wake_condition, start_at, due_at, relations, executions. Priority accepts p0, p1, p2, p3 (or an empty string or null to clear); start_at and due_at accept an RFC 3339 timestamp (or an empty string or null to clear). Blockers accepts an array of strings; relations accepts an array of objects with kind (parent, child, depends_on, related, artifact) and a nonempty string ref of the form \u003ctype\u003e:\u003chandle-or-id\u003e (card refs for parent, child, depends_on, also accepting bare card handles or IDs); executions accepts an array of objects with nonempty string authority and run_id. Labels and roles accept arrays of at most 16 nonempty strings of at most 128 characters (or null to clear). Keys and value shapes are validated before proposal insertion; invalid input returns 400 invalid_request, naming the allowed keys for unknown keys or the accepted values and shape for invalid values.",
        "why": "Propose a scoped PM decision.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMDecision`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "human_proposal_pending",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "body_schema": {
            "required": [
                {
                    "name": "instruction",
                    "type": "string"
                },
                {
                    "name": "request_key",
                    "type": "string"
                },
                {
                    "name": "scope",
                    "type": "string"
                },
                {
                    "name": "target_revision",
                    "type": "string"
                },
                {
                    "name": "work_ref",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "payload.phase",
                    "type": "string",
                    "enum_values": [
                        "backlog",
                        "blocked",
                        "done",
                        "in_progress",
                        "ready",
                        "review"
                    ]
                },
                {
                    "name": "payload.resolution_refs",
                    "type": "list\u003cstring\u003e"
                }
            ]
        },
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmDecisionsCreate",
        "ts_method": "pmDecisionsCreate"
    },
    {
        "command_id": "pm.decisions.dispatch",
        "cli_path": "pm decisions dispatch",
        "group": "pm",
        "method": "POST",
        "path": "/pm/decisions/{decision_id}/dispatch",
        "operation_id": "pmDecisionsDispatch",
        "summary": "Hand off an authorized source action",
        "description": "Missing work (including trashed or archived work) returns 409 source_revision_changed with reason work_missing, approved_revision, current_revision null, and origin_kind and proposed_by copied from the decision before any source call. A changed revision returns the same typed details with reason revision_changed and the observed current_revision. For a pending action with no sent attempt, dispatch durably records a failed attempt without sent_at and advances the action to failed, making it acknowledgeable. Receipt detail is \"The task this approval refers to no longer exists (trashed or purged); nothing was sent\". Transient work_read_failed errors leave durable state unchanged. Executor availability is independent of work lifecycle. Only the authorized decision owner receives this diagnosis; cross-principal access remains forbidden.",
        "why": "Hand off an authorized source action.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMAction`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "path_params": [
            "decision_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmDecisionsDispatch",
        "ts_method": "pmDecisionsDispatch"
    },
    {
        "command_id": "pm.decisions.get",
        "cli_path": "pm decisions get",
        "group": "pm",
        "method": "GET",
        "path": "/pm/decisions/{decision_id}",
        "operation_id": "pmDecisionsGet",
        "summary": "Read a PM decision",
        "why": "Read a PM decision.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMDecision`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "path_params": [
            "decision_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmDecisionsGet",
        "ts_method": "pmDecisionsGet"
    },
    {
        "command_id": "pm.decisions.list",
        "cli_path": "pm decisions list",
        "group": "pm",
        "method": "GET",
        "path": "/pm/decisions",
        "operation_id": "pmDecisionsList",
        "summary": "List durable PM decisions",
        "description": "Newest first by created_at descending, then internal rowid descending. Opaque cursors retain both ordering values so newer inserts do not shift subsequent pages. Pagination applies permission filtering before deriving has_more and next_cursor.",
        "why": "List durable PM decisions.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMDecisionListResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmDecisionsList",
        "ts_method": "pmDecisionsList"
    },
    {
        "command_id": "pm.turns.claim",
        "cli_path": "pm turns claim",
        "group": "pm",
        "method": "POST",
        "path": "/pm/turns/claim",
        "operation_id": "pmTurnsClaim",
        "summary": "Claim or recover a PM turn with an exclusive runner lease",
        "description": "New leases are allocated atomically only while the workspace has fewer than ANX_PM_MAX_CONCURRENT (default 2) claimed turns with unexpired leases. When a waiting turn for the selected PM actor exists but workspace lease capacity is reached, returns 429 busy with error.details {reason: capacity, in_flight: N, limit: M, waiting: K}; waiting counts eligible unleased turns for that actor before their deadline. The message is \"Workspace PM capacity reached (N in flight; limit M)\". Runners retry after one poll interval. With no waiting turn for that actor, returns 204 even at capacity. Expired leases free capacity. Recovers an unexpired lease held by the same selected PM actor and runner_id before allocating new work, returning its existing token without extending expiry or changing claimed_at. Other runners cannot steal an unexpired lease. Leases last ANX_PM_LEASE_TTL (default 60s), bounded by the turn deadline. Runners must POST heartbeat with the token at a cadence strictly less than TTL/2. Expired leases are reclaimable with a fresh token until the turn deadline; the same open turn becomes unclaimed and waiting again, preserving its history rather than failing. A runner_id identifies one serial worker and must not be shared by concurrent workers. Release the lease on SIGINT or SIGTERM before exiting; a restart with the same runner_id can recover after an abrupt kill.",
        "why": "Claim one queued turn for the selected PM agent so two runners never answer it.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMClaimedTurn`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Selected PM agent only. Empty body is allowed. 204 means no waiting turn; 429 busy with reason capacity means waiting work is blocked by the lease cap and should be retried after one poll interval. Claims recover the same runner_id lease first, or allocate a fresh lease. Past-deadline open turns are expired to `failed` on reads, claims, and periodic maintenance. Lease expiry is bounded by the turn deadline and ANX_PM_LEASE_TTL (default 60s). Channel-origin turns use this same claim/complete/fail pipeline.",
        "body_schema": {
            "optional": [
                {
                    "name": "runner_id",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmTurnsClaim",
        "ts_method": "pmTurnsClaim"
    },
    {
        "command_id": "pm.turns.complete",
        "cli_path": "pm turns complete",
        "group": "pm",
        "method": "POST",
        "path": "/pm/turns/{turn_id}/complete",
        "operation_id": "pmTurnsComplete",
        "summary": "Record a selected PM agent response",
        "description": "Requires an active lease. Missing tokens return 409 lease_required; released, expired, or stale tokens return 409 lease_mismatch and require claiming again. Identical terminal completion replays (text and evidence_refs) with the token that completed the turn return 200 without mutation, including after the deadline; different content returns 409 turn_closed; a stale or missing replay token returns 409 lease_mismatch explaining that the turn is already delivered and no retry is needed.",
        "why": "Record a selected PM agent response.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMTurn`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "turn_closed",
            "lease_required",
            "lease_mismatch",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "body_schema": {
            "required": [
                {
                    "name": "lease_token",
                    "type": "string"
                },
                {
                    "name": "text",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "evidence_refs",
                    "type": "list\u003cstring\u003e"
                }
            ]
        },
        "path_params": [
            "turn_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmTurnsComplete",
        "ts_method": "pmTurnsComplete"
    },
    {
        "command_id": "pm.turns.context",
        "cli_path": "pm turns context",
        "group": "pm",
        "method": "POST",
        "path": "/pm/turns/{turn_id}/context",
        "operation_id": "pmTurnsContext",
        "summary": "Read requesting principal context as selected PM agent",
        "description": "Requires the current active lease token in the request body. Missing tokens return 409 lease_required; expired, released, or stale tokens return 409 lease_mismatch and require claiming again.",
        "why": "Read requesting principal context as selected PM agent.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMContextResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "turn_closed",
            "lease_required",
            "lease_mismatch",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "body_schema": {
            "required": [
                {
                    "name": "lease_token",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "cursor",
                    "type": "string"
                },
                {
                    "name": "limit",
                    "type": "integer"
                },
                {
                    "name": "query",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "turn_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmTurnsContext",
        "ts_method": "pmTurnsContext"
    },
    {
        "command_id": "pm.turns.decisions.create",
        "cli_path": "pm turns decisions create",
        "group": "pm",
        "method": "POST",
        "path": "/pm/turns/{turn_id}/decisions",
        "operation_id": "pmTurnsDecisionsCreate",
        "summary": "Record a selected PM agent proposal",
        "description": "For work.annotate, instruction must be a nonempty JSON object containing only labels, roles, project_ref, priority, next_actor, next_action, blockers, wake_condition, start_at, due_at, relations, executions. Priority accepts p0, p1, p2, p3 (or an empty string or null to clear); start_at and due_at accept an RFC 3339 timestamp (or an empty string or null to clear). Blockers accepts an array of strings; relations accepts an array of objects with kind (parent, child, depends_on, related, artifact) and a nonempty string ref of the form \u003ctype\u003e:\u003chandle-or-id\u003e (card refs for parent, child, depends_on, also accepting bare card handles or IDs); executions accepts an array of objects with nonempty string authority and run_id. Labels and roles accept arrays of at most 16 nonempty strings of at most 128 characters (or null to clear). Keys and value shapes are validated before proposal insertion; invalid input returns 400 invalid_request, naming the allowed keys for unknown keys or the accepted values and shape for invalid values. Requires the current active lease token in the request body. Missing tokens return 409 lease_required; expired, released, or stale tokens return 409 lease_mismatch and require claiming again.",
        "why": "Record a selected PM agent proposal.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMDecision`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "human_proposal_pending",
            "turn_closed",
            "lease_required",
            "lease_mismatch",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace principal is authoritative. Decisions do not imply application; receipts distinguish delivery, source reports, and independent verification. Unknown sends must not be blindly retried.",
        "body_schema": {
            "required": [
                {
                    "name": "instruction",
                    "type": "string"
                },
                {
                    "name": "lease_token",
                    "type": "string"
                },
                {
                    "name": "request_key",
                    "type": "string"
                },
                {
                    "name": "scope",
                    "type": "string"
                },
                {
                    "name": "target_revision",
                    "type": "string"
                },
                {
                    "name": "work_ref",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "payload.phase",
                    "type": "string",
                    "enum_values": [
                        "backlog",
                        "blocked",
                        "done",
                        "in_progress",
                        "ready",
                        "review"
                    ]
                },
                {
                    "name": "payload.resolution_refs",
                    "type": "list\u003cstring\u003e"
                }
            ]
        },
        "path_params": [
            "turn_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmTurnsDecisionsCreate",
        "ts_method": "pmTurnsDecisionsCreate"
    },
    {
        "command_id": "pm.turns.fail",
        "cli_path": "pm turns fail",
        "group": "pm",
        "method": "POST",
        "path": "/pm/turns/{turn_id}/fail",
        "operation_id": "pmTurnsFail",
        "summary": "Mark a claimed PM turn failed",
        "why": "Record a selected PM agent failure reason without inventing a reply.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMTurn`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "turn_closed",
            "lease_required",
            "lease_mismatch",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Selected PM agent only. An active lease is required and lease_token must match. Missing tokens return 409 lease_required; stale or expired lease tokens return 409 lease_mismatch and require claiming again. Identical terminal failure replays with the token that failed the turn return 200 without mutation, including after the deadline; a different reason returns 409 turn_closed; a stale or missing replay token returns 409 lease_mismatch explaining that the turn is already failed and no retry is needed.",
        "body_schema": {
            "required": [
                {
                    "name": "lease_token",
                    "type": "string"
                },
                {
                    "name": "reason",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "turn_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.get",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmTurnsFail",
        "ts_method": "pmTurnsFail"
    },
    {
        "command_id": "pm.turns.get",
        "cli_path": "pm turns get",
        "group": "pm",
        "method": "GET",
        "path": "/pm/turns/{turn_id}",
        "operation_id": "pmTurnsGet",
        "summary": "Read a PM conversation turn",
        "description": "The requesting actor with conversation read permission or the configured PM actor with pm.respond permission may read the turn in its workspace. Active lease_owner is the runner ID; lease_token is returned only by claim.",
        "why": "Read a PM conversation turn.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMTurn`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "source_revision_changed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Only the requesting conversation actor can read this turn. Past-deadline open turns are failed before returning.",
        "path_params": [
            "turn_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.heartbeat",
            "pm.turns.release"
        ],
        "go_method": "PmTurnsGet",
        "ts_method": "pmTurnsGet"
    },
    {
        "command_id": "pm.turns.heartbeat",
        "cli_path": "pm turns heartbeat",
        "group": "pm",
        "method": "POST",
        "path": "/pm/turns/{turn_id}/heartbeat",
        "operation_id": "pmTurnsHeartbeat",
        "summary": "Renew a claimed PM turn lease",
        "description": "Selected PM actor only. Requires the current lease_token. Extends lease_expires_at to min(now + ANX_PM_LEASE_TTL, turn deadline). ANX_PM_LEASE_TTL defaults to 60s (valid range 1s to 10m); runners must renew at a cadence strictly less than TTL/2. Missing tokens return 409 lease_required; expired, released, or foreign tokens return 409 lease_mismatch. Terminal turns return 409 turn_closed. Returns PMHeartbeatTurn without exposing the token; preserves status, claimed_at, and existing history.",
        "why": "Keep an active runner lease alive during execution.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMHeartbeatTurn`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "lease_mismatch",
            "lease_required",
            "turn_closed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Renew at a cadence strictly less than TTL/2; stop execution if ownership is lost.",
        "body_schema": {
            "required": [
                {
                    "name": "lease_token",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "turn_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.release"
        ],
        "go_method": "PmTurnsHeartbeat",
        "ts_method": "pmTurnsHeartbeat"
    },
    {
        "command_id": "pm.turns.release",
        "cli_path": "pm turns release",
        "group": "pm",
        "method": "POST",
        "path": "/pm/turns/{turn_id}/release",
        "operation_id": "pmTurnsRelease",
        "summary": "Release a claimed PM turn back to the queue",
        "description": "The selected PM actor releases its unexpired lease using runner_id and lease_token. A different runner or incorrect token returns 409 lease_mismatch; no active lease returns 409 turn_not_claimed explaining that it is already released or expired and no release is needed. Clears lease credentials, preserves claimed_at, and returns status sending with claimed false. CLI runners should stop local execution and release on SIGINT or SIGTERM using a bounded shutdown request. Terminal or expired turns return 409 turn_closed; expired turns include failure_kind expired and a message naming the deadline.",
        "why": "Return interrupted work to the queue for another claim.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `PMTurn`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request",
            "forbidden",
            "not_found",
            "conflict",
            "lease_mismatch",
            "turn_not_claimed",
            "turn_closed",
            "busy",
            "unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Selected PM agent only; runner_id and lease_token must match the unexpired lease owner.",
        "body_schema": {
            "required": [
                {
                    "name": "lease_token",
                    "type": "string"
                },
                {
                    "name": "runner_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "turn_id"
        ],
        "adjacent_commands": [
            "pm.actions.acknowledge",
            "pm.actions.get",
            "pm.actions.list",
            "pm.actions.reconcile",
            "pm.bindings.create",
            "pm.bindings.list",
            "pm.context",
            "pm.conversations.create",
            "pm.conversations.get",
            "pm.conversations.list",
            "pm.conversations.messages.create",
            "pm.decisions.answer",
            "pm.decisions.create",
            "pm.decisions.dispatch",
            "pm.decisions.get",
            "pm.decisions.list",
            "pm.turns.claim",
            "pm.turns.complete",
            "pm.turns.context",
            "pm.turns.decisions.create",
            "pm.turns.fail",
            "pm.turns.get",
            "pm.turns.heartbeat"
        ],
        "go_method": "PmTurnsRelease",
        "ts_method": "pmTurnsRelease"
    },
    {
        "command_id": "ref_edges.list",
        "cli_path": "ref-edges list",
        "group": "ref-edges",
        "method": "GET",
        "path": "/ref-edges",
        "operation_id": "listRefEdges",
        "summary": "List ref edges (forward or reverse indexed lookup)",
        "why": "Query the write-through ref index by source or target typed ref (mutually exclusive); reverse lookup uses target_ref.",
        "input_mode": "query",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ ref_edges }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "refs",
            "inspection"
        ],
        "stability": "beta",
        "surface": "diagnostic",
        "go_method": "RefEdgesList",
        "ts_method": "refEdgesList"
    },
    {
        "command_id": "refs.resolve",
        "cli_path": "refs resolve",
        "group": "refs",
        "method": "POST",
        "path": "/refs/resolve",
        "operation_id": "resolveRefsBatch",
        "summary": "Resolve up to 200 refs in input order",
        "why": "Resolve ref chips and previews in one read request; unknown refs remain in the result.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns the current principal-scoped read projection.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "read"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Read-only POST. CLI accepts positional refs or --from-file containing {refs:[...]}. Preserve unknown refs, order and duplicates; maximum 200.",
        "body_schema": {
            "required": [
                {
                    "name": "refs",
                    "type": "list\u003cstring\u003e"
                }
            ]
        },
        "go_method": "RefsResolve",
        "ts_method": "refsResolve"
    },
    {
        "command_id": "report.preview",
        "cli_path": "report preview",
        "group": "report",
        "method": "POST",
        "path": "/reports/preview",
        "operation_id": "previewReportContent",
        "summary": "Materialize live panels from unsaved visual report content",
        "why": "Preview an unsaved report with current live workspace data.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ observed_at, panels }`; no document or revision is created.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "unavailable"
        ],
        "concepts": [
            "docs",
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Read-only and principal-scoped. Accepts the same bounded version 1 report shape as a saved document and materializes its live queries without writing a document. Each panel reports status, observation time, data and truncation.",
        "body_schema": {
            "required": [
                {
                    "name": "report",
                    "type": "object"
                }
            ]
        },
        "adjacent_commands": [
            "report.render"
        ],
        "go_method": "ReportPreview",
        "ts_method": "reportPreview"
    },
    {
        "command_id": "report.render",
        "cli_path": "report render",
        "group": "report",
        "method": "GET",
        "path": "/docs/{document_id}/report",
        "operation_id": "renderReport",
        "summary": "Materialize live panels from the current visual report revision",
        "why": "Read the same live dashboard data shown to a workspace reader.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document_ref, revision_ref, observed_at, panels }`; every panel includes resolved live/authored provenance and authored review metadata.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "not_found",
            "invalid_request",
            "unavailable"
        ],
        "concepts": [
            "docs",
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Both text and structured version 1 visual reports are supported. Authored review metadata is returned for every static panel. Reading a pinned report checks deadlines and durably deduplicates author-only inbox reminders per panel and revision. Each live or series-bound panel is independently materialized with status ok, stale or unavailable, observation time, data and an explicit truncated flag. Never infer zero work from an unavailable or truncated panel. Native work, event and decision candidates are bounded to 200 per request scope, with truncated marking additional candidates. Archived boards and their work are excluded. Private PM events remain private.",
        "path_params": [
            "document_id"
        ],
        "adjacent_commands": [
            "report.preview"
        ],
        "go_method": "ReportRender",
        "ts_method": "reportRender"
    },
    {
        "command_id": "runs.get",
        "cli_path": "runs get",
        "group": "runs",
        "method": "GET",
        "path": "/runs/{run_id}",
        "operation_id": "getRun",
        "summary": "Get one run",
        "description": "Any authenticated workspace principal.",
        "why": "Inspect execution attribution and outcome.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ run }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "runs",
            "agents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "path_params": [
            "run_id"
        ],
        "adjacent_commands": [
            "runs.upsert",
            "runs.list"
        ],
        "go_method": "RunsGet",
        "ts_method": "runsGet"
    },
    {
        "command_id": "runs.list",
        "cli_path": "runs list",
        "group": "runs",
        "method": "GET",
        "path": "/runs",
        "operation_id": "listRuns",
        "summary": "List launcher runs",
        "description": "Any authenticated workspace principal. Newest started_at first, then ID; cursor is opaque. `active=true` means nonterminal state and alive liveness.",
        "why": "See current and recent agent executions.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ runs, next_cursor? }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request"
        ],
        "concepts": [
            "runs",
            "agents",
            "cards"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "adjacent_commands": [
            "runs.get",
            "runs.upsert"
        ],
        "go_method": "RunsList",
        "ts_method": "runsList"
    },
    {
        "command_id": "runs.upsert",
        "cli_path": "runs ingest",
        "group": "runs",
        "method": "POST",
        "path": "/runs",
        "operation_id": "upsertRun",
        "summary": "Idempotently ingest a launcher run",
        "description": "Derived-agent bearer only. Authenticated agent must equal agent_id and belong to host_id. Composite key `(launcher, host_id, external_id)` is immutable; a conflicting agent or immutable field returns run_identity_conflict. State and observation timestamps cannot regress. Terminal states are absorbing except an identical replay; unknown is provisional and may advance to any observed state. A terminal run never changes a card.",
        "why": "Map one agentctl execution envelope to a durable run.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ run, created, replayed }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "host_revoked",
            "run_identity_conflict",
            "run_state_regression"
        ],
        "concepts": [
            "runs",
            "agents",
            "cards"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Validate workspace identity and route-specific proof before mutation; error codes are stable.",
        "body_schema": {
            "required": [
                {
                    "name": "adapter",
                    "type": "string"
                },
                {
                    "name": "agent_id",
                    "type": "string"
                },
                {
                    "name": "external_id",
                    "type": "string"
                },
                {
                    "name": "host_id",
                    "type": "string"
                },
                {
                    "name": "labels",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "last_observed_at",
                    "type": "datetime"
                },
                {
                    "name": "launcher",
                    "type": "string",
                    "enum_values": [
                        "agentctl"
                    ]
                },
                {
                    "name": "liveness",
                    "type": "string",
                    "enum_values": [
                        "alive",
                        "stale",
                        "unknown"
                    ]
                },
                {
                    "name": "result_collected",
                    "type": "boolean"
                },
                {
                    "name": "state",
                    "type": "string",
                    "enum_values": [
                        "cancelled",
                        "completed",
                        "failed",
                        "running",
                        "starting",
                        "unknown"
                    ]
                }
            ],
            "optional": [
                {
                    "name": "branch",
                    "type": "string"
                },
                {
                    "name": "card_ref",
                    "type": "string"
                },
                {
                    "name": "ended_at",
                    "type": "datetime"
                },
                {
                    "name": "model",
                    "type": "string"
                },
                {
                    "name": "repository",
                    "type": "string"
                },
                {
                    "name": "started_at",
                    "type": "datetime"
                }
            ]
        },
        "adjacent_commands": [
            "runs.get",
            "runs.list"
        ],
        "go_method": "RunsUpsert",
        "ts_method": "runsUpsert"
    },
    {
        "command_id": "secrets.create",
        "cli_path": "secret create",
        "group": "secret",
        "method": "POST",
        "path": "/secrets",
        "operation_id": "createSecret",
        "summary": "Create secret",
        "why": "Store an encrypted workspace credential with metadata.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ secret }` (metadata only, value is not echoed).",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "human_only",
            "invalid_request",
            "resource_exists",
            "secrets_not_configured"
        ],
        "concepts": [
            "secrets",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Only human principals may create secrets.",
        "body_schema": {
            "required": [
                {
                    "name": "name",
                    "type": "string"
                },
                {
                    "name": "value",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "description",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "secrets.delete",
            "secrets.reveal-batch",
            "secrets.reveal",
            "secrets.list",
            "secrets.update"
        ],
        "go_method": "SecretsCreate",
        "ts_method": "secretsCreate"
    },
    {
        "command_id": "secrets.delete",
        "cli_path": "secret delete",
        "group": "secret",
        "method": "DELETE",
        "path": "/secrets/{secret_id}",
        "operation_id": "deleteSecret",
        "summary": "Delete secret",
        "why": "Permanently remove a secret and its encrypted value.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ deleted: true, secret_id }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "human_only",
            "not_found",
            "secrets_not_configured"
        ],
        "concepts": [
            "secrets",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Only human principals may delete secrets.",
        "path_params": [
            "secret_id"
        ],
        "adjacent_commands": [
            "secrets.create",
            "secrets.reveal-batch",
            "secrets.reveal",
            "secrets.list",
            "secrets.update"
        ],
        "go_method": "SecretsDelete",
        "ts_method": "secretsDelete"
    },
    {
        "command_id": "secrets.list",
        "cli_path": "secret list",
        "group": "secret",
        "method": "GET",
        "path": "/secrets",
        "operation_id": "listSecrets",
        "summary": "List secrets",
        "why": "List workspace secret metadata without exposing values.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ secrets }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "secrets"
        ],
        "stability": "beta",
        "surface": "canonical",
        "adjacent_commands": [
            "secrets.create",
            "secrets.delete",
            "secrets.reveal-batch",
            "secrets.reveal",
            "secrets.update"
        ],
        "go_method": "SecretsList",
        "ts_method": "secretsList"
    },
    {
        "command_id": "secrets.reveal",
        "cli_path": "secret get --reveal",
        "group": "secret",
        "method": "POST",
        "path": "/secrets/{secret_id}/reveal",
        "operation_id": "revealSecret",
        "summary": "Reveal secret value",
        "why": "Decrypt and return a secret value. Logged in audit.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ name, value }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found",
            "secrets_not_configured"
        ],
        "concepts": [
            "secrets"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Every reveal is logged in auth audit. POST (not GET) to prevent caching.",
        "path_params": [
            "secret_id"
        ],
        "adjacent_commands": [
            "secrets.create",
            "secrets.delete",
            "secrets.reveal-batch",
            "secrets.list",
            "secrets.update"
        ],
        "go_method": "SecretsReveal",
        "ts_method": "secretsReveal"
    },
    {
        "command_id": "secrets.reveal-batch",
        "cli_path": "secret exec",
        "group": "secret",
        "method": "POST",
        "path": "/secrets/reveal-batch",
        "operation_id": "revealSecretsBatch",
        "summary": "Reveal multiple secrets by name",
        "why": "Batch-fetch secrets for env injection. Each reveal is audited.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ secrets: [{ name, value }] }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found",
            "invalid_request",
            "secrets_not_configured"
        ],
        "concepts": [
            "secrets"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Each resolved secret generates an audit event. Missing names return not_found.",
        "body_schema": {
            "required": [
                {
                    "name": "names",
                    "type": "list\u003cstring\u003e"
                }
            ]
        },
        "adjacent_commands": [
            "secrets.create",
            "secrets.delete",
            "secrets.reveal",
            "secrets.list",
            "secrets.update"
        ],
        "go_method": "SecretsRevealBatch",
        "ts_method": "secretsRevealBatch"
    },
    {
        "command_id": "secrets.update",
        "cli_path": "secret update",
        "group": "secret",
        "method": "PUT",
        "path": "/secrets/{secret_id}",
        "operation_id": "updateSecret",
        "summary": "Update secret value",
        "why": "Replace an encrypted secret value.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ secret }` (metadata only).",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "human_only",
            "not_found",
            "invalid_request",
            "secrets_not_configured"
        ],
        "concepts": [
            "secrets",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Only human principals may update secrets.",
        "body_schema": {
            "required": [
                {
                    "name": "value",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "description",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "secret_id"
        ],
        "adjacent_commands": [
            "secrets.create",
            "secrets.delete",
            "secrets.reveal-batch",
            "secrets.reveal",
            "secrets.list"
        ],
        "go_method": "SecretsUpdate",
        "ts_method": "secretsUpdate"
    },
    {
        "command_id": "series.list",
        "cli_path": "series list",
        "group": "series",
        "method": "GET",
        "path": "/series",
        "operation_id": "series_list",
        "summary": "List pushed series",
        "why": "List pushed series.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns JSON with provenance and explicit freshness.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "series_capacity"
        ],
        "concepts": [
            "documents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Explicit declared push source; core never fetches external data.",
        "adjacent_commands": [
            "series.push",
            "series.query",
            "series.show"
        ],
        "go_method": "SeriesList",
        "ts_method": "seriesList"
    },
    {
        "command_id": "series.push",
        "cli_path": "series push",
        "group": "series",
        "method": "POST",
        "path": "/series/{name}/points",
        "operation_id": "series_push",
        "summary": "Push one declared series point",
        "description": "Scoped credentials have a default-deny series.points.push capability for declared resources only. Exact same-timestamp retries are idempotent. A changed raw observation at the same timestamp replaces its value and consumes daily ingestion budget; rolled-up observations cannot be corrected through push. Safety bounds are independent of commercial quotas. Fixed UTC-minute request budgets are 1200 per adapter and 2400 per workspace, counting retries; accepted points are capped at 100000 per UTC day. In-flight scoped requests are limited to two per adapter and four per workspace. Timestamps may be at most 90 days old or five minutes ahead. Raw retention is 90 days; daily rollups never expire.",
        "why": "Push one declared series point.",
        "input_mode": "flags",
        "http_input_mode": "json-body",
        "cli_input": {
            "mode": "flags",
            "flags": [
                {
                    "name": "adapter",
                    "description": "Declared adapter; omitted means resolve from series inventory."
                },
                {
                    "name": "label",
                    "description": "Repeated exact k=v labels."
                },
                {
                    "name": "ts",
                    "description": "RFC3339 observation timestamp."
                },
                {
                    "name": "from-command",
                    "description": "Read a number or JSON point from the argv after --."
                },
                {
                    "name": "series",
                    "description": "Series name when from-command stdout is a number."
                }
            ]
        },
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns JSON with provenance and explicit freshness.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "series_capacity",
            "series_rate_limited"
        ],
        "concepts": [
            "documents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Explicit declared push source; core never fetches external data.",
        "examples": [
            {
                "title": "Push a number",
                "command": "anx series push builds 12 --label initiative=launch"
            },
            {
                "title": "Push command output",
                "command": "anx series push builds --from-command -- ./count-builds"
            }
        ],
        "body_schema": {
            "optional": [
                {
                    "name": "labels",
                    "type": "object"
                },
                {
                    "name": "state",
                    "type": "string"
                },
                {
                    "name": "ts",
                    "type": "datetime"
                },
                {
                    "name": "value",
                    "type": "number"
                }
            ]
        },
        "path_params": [
            "name"
        ],
        "adjacent_commands": [
            "series.list",
            "series.query",
            "series.show"
        ],
        "go_method": "SeriesPush",
        "ts_method": "seriesPush"
    },
    {
        "command_id": "series.query",
        "cli_path": "series query",
        "group": "series",
        "method": "GET",
        "path": "/series/{name}/query",
        "operation_id": "series_query",
        "summary": "Query a bounded series range",
        "why": "Query a bounded series range.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns JSON with provenance and explicit freshness.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "series_capacity"
        ],
        "concepts": [
            "documents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Explicit declared push source; core never fetches external data.",
        "path_params": [
            "name"
        ],
        "adjacent_commands": [
            "series.list",
            "series.push",
            "series.show"
        ],
        "go_method": "SeriesQuery",
        "ts_method": "seriesQuery"
    },
    {
        "command_id": "series.show",
        "cli_path": "series show",
        "group": "series",
        "method": "GET",
        "path": "/series/{name}",
        "operation_id": "series_show",
        "summary": "Show a series and its provenance",
        "why": "Show a series and its provenance.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns JSON with provenance and explicit freshness.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "series_capacity"
        ],
        "concepts": [
            "documents"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Explicit declared push source; core never fetches external data.",
        "path_params": [
            "name"
        ],
        "adjacent_commands": [
            "series.list",
            "series.push",
            "series.query"
        ],
        "go_method": "SeriesShow",
        "ts_method": "seriesShow"
    },
    {
        "command_id": "sessions.get",
        "cli_path": "sessions get",
        "group": "sessions",
        "method": "GET",
        "path": "/sessions/{session_id}",
        "operation_id": "sessionsGet",
        "summary": "Read your private native session",
        "why": "Read your private native session without assigning, moving, or completing work.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `SessionResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "sessions_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Existing bearer authentication is required, including in development mode. Registration and participation writes require an agent principal and never mint credentials. Sessions are private to that principal. Task reads expose only explicitly shared participation metadata; native session identifiers and other task links are never shared. Upserts refresh server-clock activity leases of 120 seconds. A session heartbeat does not refresh task participation. Session closure is terminal and never changes work state, assignees, source authority, or ownership. Session identity is independent of per-attempt /runs. Capabilities are caller-reported, not server-verified support.",
        "path_params": [
            "session_id"
        ],
        "adjacent_commands": [
            "sessions.register"
        ],
        "go_method": "SessionsGet",
        "ts_method": "sessionsGet"
    },
    {
        "command_id": "sessions.register",
        "cli_path": "sessions register",
        "group": "sessions",
        "method": "POST",
        "path": "/sessions",
        "operation_id": "sessionsRegister",
        "summary": "Register or refresh a private native session",
        "why": "Register or refresh a private native session without assigning, moving, or completing work.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `SessionResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "sessions_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Existing bearer authentication is required, including in development mode. Registration and participation writes require an agent principal and never mint credentials. Sessions are private to that principal. Task reads expose only explicitly shared participation metadata; native session identifiers and other task links are never shared. Upserts refresh server-clock activity leases of 120 seconds. A session heartbeat does not refresh task participation. Session closure is terminal and never changes work state, assignees, source authority, or ownership. Session identity is independent of per-attempt /runs. Capabilities are caller-reported, not server-verified support.",
        "body_schema": {
            "required": [
                {
                    "name": "activity",
                    "type": "string",
                    "enum_values": [
                        "active",
                        "closed",
                        "idle"
                    ]
                },
                {
                    "name": "capabilities.history",
                    "type": "string",
                    "enum_values": [
                        "supported",
                        "unknown",
                        "unsupported"
                    ]
                },
                {
                    "name": "capabilities.logs",
                    "type": "string",
                    "enum_values": [
                        "supported",
                        "unknown",
                        "unsupported"
                    ]
                },
                {
                    "name": "capabilities.resume",
                    "type": "string",
                    "enum_values": [
                        "supported",
                        "unknown",
                        "unsupported"
                    ]
                },
                {
                    "name": "native_session_id",
                    "type": "string"
                },
                {
                    "name": "provider",
                    "type": "string"
                },
                {
                    "name": "sequence",
                    "type": "integer"
                }
            ],
            "optional": [
                {
                    "name": "host_scope",
                    "type": "string"
                },
                {
                    "name": "native_session_id_kind",
                    "type": "string",
                    "enum_values": [
                        "opaque",
                        "provider_session_sha256"
                    ]
                }
            ]
        },
        "adjacent_commands": [
            "sessions.get"
        ],
        "go_method": "SessionsRegister",
        "ts_method": "sessionsRegister"
    },
    {
        "command_id": "threads.context",
        "cli_path": "threads context",
        "group": "threads",
        "method": "GET",
        "path": "/threads/{thread_id}/context",
        "operation_id": "getThreadContext",
        "summary": "Get backing thread coordination context",
        "why": "Load a compact coordination bundle (thread, recent events, key artifacts, cards, documents) for inspection and triage.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ thread, recent_events, key_artifacts, open_cards, documents }` plus forward-compatible fields.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "threads",
            "inspection"
        ],
        "stability": "beta",
        "surface": "projection",
        "path_params": [
            "thread_id"
        ],
        "adjacent_commands": [
            "threads.inspect",
            "threads.list",
            "threads.timeline",
            "threads.workspace"
        ],
        "go_method": "ThreadsContext",
        "ts_method": "threadsContext"
    },
    {
        "command_id": "threads.inspect",
        "cli_path": "threads inspect",
        "group": "threads",
        "method": "GET",
        "path": "/threads/{thread_id}",
        "operation_id": "getThread",
        "summary": "Inspect backing thread",
        "why": "Resolve one backing thread for low-level inspection and diagnostics.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ thread }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "threads",
            "inspection"
        ],
        "stability": "beta",
        "surface": "diagnostic",
        "path_params": [
            "thread_id"
        ],
        "adjacent_commands": [
            "threads.context",
            "threads.list",
            "threads.timeline",
            "threads.workspace"
        ],
        "go_method": "ThreadsInspect",
        "ts_method": "threadsInspect"
    },
    {
        "command_id": "threads.list",
        "cli_path": "threads list",
        "group": "threads",
        "method": "GET",
        "path": "/threads",
        "operation_id": "listThreads",
        "summary": "List backing threads",
        "why": "Inspect backing infrastructure threads without making them the primary planning noun.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ threads }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "threads",
            "inspection"
        ],
        "stability": "beta",
        "surface": "diagnostic",
        "adjacent_commands": [
            "threads.context",
            "threads.inspect",
            "threads.timeline",
            "threads.workspace"
        ],
        "go_method": "ThreadsList",
        "ts_method": "threadsList"
    },
    {
        "command_id": "threads.timeline",
        "cli_path": "threads timeline",
        "group": "threads",
        "method": "GET",
        "path": "/threads/{thread_id}/timeline",
        "operation_id": "getThreadTimeline",
        "summary": "Get backing thread timeline",
        "why": "Retrieve event history plus typed-ref expansions for one backing thread.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ thread, events, artifacts, topics, cards, documents, notification_receipts }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "threads",
            "timeline"
        ],
        "stability": "beta",
        "surface": "projection",
        "path_params": [
            "thread_id"
        ],
        "adjacent_commands": [
            "threads.context",
            "threads.inspect",
            "threads.list",
            "threads.workspace"
        ],
        "go_method": "ThreadsTimeline",
        "ts_method": "threadsTimeline"
    },
    {
        "command_id": "threads.workspace",
        "cli_path": "threads workspace",
        "group": "threads",
        "method": "GET",
        "path": "/threads/{thread_id}/workspace",
        "operation_id": "getThreadWorkspace",
        "summary": "Get backing thread workspace projection (diagnostic)",
        "why": "Read-only diagnostic projection that bundles context, inbox, and related-thread signals for one backing thread. Prefer topics.workspace for normal operator coordination when a topic exists.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ thread, related_topics, cards, documents, board_memberships, inbox, projection_freshness }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "threads",
            "workspace"
        ],
        "stability": "beta",
        "surface": "projection",
        "path_params": [
            "thread_id"
        ],
        "adjacent_commands": [
            "threads.context",
            "threads.inspect",
            "threads.list",
            "threads.timeline"
        ],
        "go_method": "ThreadsWorkspace",
        "ts_method": "threadsWorkspace"
    },
    {
        "command_id": "topics.archive",
        "cli_path": "topics archive",
        "group": "topics",
        "method": "POST",
        "path": "/topics/{topic_id}/archive",
        "operation_id": "archiveTopic",
        "summary": "Archive topic",
        "why": "Soft-archive a topic and derive its lifecycle state from archived_at.",
        "input_mode": "none",
        "http_input_mode": "json-body",
        "cli_input": {
            "mode": "none",
            "body_optional": true
        },
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topic }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "topics",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "topic_id"
        ],
        "adjacent_commands": [
            "topics.create",
            "topics.get",
            "topics.list",
            "topics.patch",
            "topics.restore",
            "topics.timeline",
            "topics.trash",
            "topics.unarchive",
            "topics.workspace"
        ],
        "go_method": "TopicsArchive",
        "ts_method": "topicsArchive"
    },
    {
        "command_id": "topics.create",
        "cli_path": "topics create",
        "group": "topics",
        "method": "POST",
        "path": "/topics",
        "operation_id": "createTopic",
        "summary": "Create topic",
        "why": "Create a first-class durable topic before attaching cards, docs, or artifacts.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topic }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token"
        ],
        "concepts": [
            "topics",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Replay-safe when the same request key and body are reused.",
        "body_schema": {
            "required": [
                {
                    "name": "topic.board_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "topic.document_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "topic.owner_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "topic.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "topic.related_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "topic.summary",
                    "type": "string"
                },
                {
                    "name": "topic.title",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "request_key",
                    "type": "string"
                },
                {
                    "name": "topic.id",
                    "type": "string"
                },
                {
                    "name": "topic.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "topic.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "topic.thread_id",
                    "type": "string"
                },
                {
                    "name": "topic.workspace_move",
                    "type": "object"
                }
            ]
        },
        "adjacent_commands": [
            "topics.archive",
            "topics.get",
            "topics.list",
            "topics.patch",
            "topics.restore",
            "topics.timeline",
            "topics.trash",
            "topics.unarchive",
            "topics.workspace"
        ],
        "go_method": "TopicsCreate",
        "ts_method": "topicsCreate"
    },
    {
        "command_id": "topics.get",
        "cli_path": "topics get",
        "group": "topics",
        "method": "GET",
        "path": "/topics/{topic_id}",
        "operation_id": "getTopic",
        "summary": "Get topic",
        "why": "Resolve one topic and its canonical durable fields.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topic }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "topics"
        ],
        "stability": "beta",
        "surface": "canonical",
        "path_params": [
            "topic_id"
        ],
        "adjacent_commands": [
            "topics.archive",
            "topics.create",
            "topics.list",
            "topics.patch",
            "topics.restore",
            "topics.timeline",
            "topics.trash",
            "topics.unarchive",
            "topics.workspace"
        ],
        "go_method": "TopicsGet",
        "ts_method": "topicsGet"
    },
    {
        "command_id": "topics.list",
        "cli_path": "topics list",
        "group": "topics",
        "method": "GET",
        "path": "/topics",
        "operation_id": "listTopics",
        "summary": "List topics",
        "why": "Scan the durable topic inventory.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topics }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "topics"
        ],
        "stability": "beta",
        "surface": "canonical",
        "adjacent_commands": [
            "topics.archive",
            "topics.create",
            "topics.get",
            "topics.patch",
            "topics.restore",
            "topics.timeline",
            "topics.trash",
            "topics.unarchive",
            "topics.workspace"
        ],
        "go_method": "TopicsList",
        "ts_method": "topicsList"
    },
    {
        "command_id": "topics.patch",
        "cli_path": "topics patch",
        "group": "topics",
        "method": "PATCH",
        "path": "/topics/{topic_id}",
        "operation_id": "patchTopic",
        "summary": "Patch topic",
        "why": "Update topic state with provenance and optimistic concurrency.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topic }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "topics",
            "write",
            "concurrency"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ],
            "optional": [
                {
                    "name": "patch.board_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.document_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.owner_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.provenance.by_field",
                    "type": "object"
                },
                {
                    "name": "patch.provenance.notes",
                    "type": "string"
                },
                {
                    "name": "patch.provenance.sources",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.related_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.summary",
                    "type": "string"
                },
                {
                    "name": "patch.title",
                    "type": "string"
                },
                {
                    "name": "patch.workspace_move",
                    "type": "object"
                }
            ]
        },
        "path_params": [
            "topic_id"
        ],
        "adjacent_commands": [
            "topics.archive",
            "topics.create",
            "topics.get",
            "topics.list",
            "topics.restore",
            "topics.timeline",
            "topics.trash",
            "topics.unarchive",
            "topics.workspace"
        ],
        "go_method": "TopicsPatch",
        "ts_method": "topicsPatch"
    },
    {
        "command_id": "topics.restore",
        "cli_path": "topics restore",
        "group": "topics",
        "method": "POST",
        "path": "/topics/{topic_id}/restore",
        "operation_id": "restoreTopic",
        "summary": "Restore topic from trash",
        "why": "Clear trash lifecycle fields on a topic after an explicit restore action.",
        "input_mode": "none",
        "http_input_mode": "json-body",
        "cli_input": {
            "mode": "none",
            "body_optional": true
        },
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topic }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "topics",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "topic_id"
        ],
        "adjacent_commands": [
            "topics.archive",
            "topics.create",
            "topics.get",
            "topics.list",
            "topics.patch",
            "topics.timeline",
            "topics.trash",
            "topics.unarchive",
            "topics.workspace"
        ],
        "go_method": "TopicsRestore",
        "ts_method": "topicsRestore"
    },
    {
        "command_id": "topics.timeline",
        "cli_path": "topics timeline",
        "group": "topics",
        "method": "GET",
        "path": "/topics/{topic_id}/timeline",
        "operation_id": "getTopicTimeline",
        "summary": "Get topic timeline",
        "why": "Load chronological evidence and related resources for one topic.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topic, events, artifacts, cards, documents, threads, notification_receipts }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "topics",
            "timeline"
        ],
        "stability": "beta",
        "surface": "projection",
        "path_params": [
            "topic_id"
        ],
        "adjacent_commands": [
            "topics.archive",
            "topics.create",
            "topics.get",
            "topics.list",
            "topics.patch",
            "topics.restore",
            "topics.trash",
            "topics.unarchive",
            "topics.workspace"
        ],
        "go_method": "TopicsTimeline",
        "ts_method": "topicsTimeline"
    },
    {
        "command_id": "topics.trash",
        "cli_path": "topics trash",
        "group": "topics",
        "method": "POST",
        "path": "/topics/{topic_id}/trash",
        "operation_id": "trashTopic",
        "summary": "Move topic to trash",
        "why": "Move topic to trash with an explicit operator reason.",
        "input_mode": "flags",
        "http_input_mode": "json-body",
        "cli_input": {
            "mode": "flags",
            "flags": [
                {
                    "name": "reason",
                    "body_path": "reason",
                    "required": true,
                    "description": "Operator-visible trash reason."
                }
            ]
        },
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topic }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "topics",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "required": [
                {
                    "name": "reason",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "topic_id"
        ],
        "adjacent_commands": [
            "topics.archive",
            "topics.create",
            "topics.get",
            "topics.list",
            "topics.patch",
            "topics.restore",
            "topics.timeline",
            "topics.unarchive",
            "topics.workspace"
        ],
        "go_method": "TopicsTrash",
        "ts_method": "topicsTrash"
    },
    {
        "command_id": "topics.unarchive",
        "cli_path": "topics unarchive",
        "group": "topics",
        "method": "POST",
        "path": "/topics/{topic_id}/unarchive",
        "operation_id": "unarchiveTopic",
        "summary": "Unarchive topic",
        "why": "Clear archived_at on a topic (restore default list visibility).",
        "input_mode": "none",
        "http_input_mode": "json-body",
        "cli_input": {
            "mode": "none",
            "body_optional": true
        },
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topic }`.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found",
            "conflict"
        ],
        "concepts": [
            "topics",
            "write"
        ],
        "stability": "beta",
        "surface": "canonical",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "if_updated_at",
                    "type": "datetime"
                }
            ]
        },
        "path_params": [
            "topic_id"
        ],
        "adjacent_commands": [
            "topics.archive",
            "topics.create",
            "topics.get",
            "topics.list",
            "topics.patch",
            "topics.restore",
            "topics.timeline",
            "topics.trash",
            "topics.workspace"
        ],
        "go_method": "TopicsUnarchive",
        "ts_method": "topicsUnarchive"
    },
    {
        "command_id": "topics.workspace",
        "cli_path": "topics workspace",
        "group": "topics",
        "method": "GET",
        "path": "/topics/{topic_id}/workspace",
        "operation_id": "getTopicWorkspace",
        "summary": "Get topic workspace (agent-facing discussion/context primitive)",
        "why": "Agent-facing discussion/context primitive. Load the topic workspace composed from linked cards, docs, backing threads, and inbox items. The operator work projection is `work.list` / `work.get`.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ topic, cards, boards, documents, threads, inbox, projection_freshness, generated_at }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "topics",
            "workspace"
        ],
        "stability": "beta",
        "surface": "projection",
        "path_params": [
            "topic_id"
        ],
        "adjacent_commands": [
            "topics.archive",
            "topics.create",
            "topics.get",
            "topics.list",
            "topics.patch",
            "topics.restore",
            "topics.timeline",
            "topics.trash",
            "topics.unarchive"
        ],
        "go_method": "TopicsWorkspace",
        "ts_method": "topicsWorkspace"
    },
    {
        "command_id": "usage.summary.v1",
        "cli_path": "usage summary --api v1",
        "group": "usage",
        "method": "GET",
        "path": "/v1/usage/summary",
        "operation_id": "getUsageSummaryV1",
        "summary": "Versioned workspace usage summary",
        "why": "Versioned usage envelope for external quota and billing aggregation.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns v1 usage summary JSON.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "ops",
            "quotas"
        ],
        "stability": "beta",
        "surface": "utility",
        "go_method": "UsageSummaryV1",
        "ts_method": "usageSummaryV1"
    },
    {
        "command_id": "work.capabilities",
        "cli_path": "work capabilities",
        "group": "work",
        "method": "GET",
        "path": "/work/capabilities",
        "operation_id": "workCapabilities",
        "summary": "Inspect work tracking capabilities",
        "why": "Inspect work tracking capabilities. `canonical_entity` is `card`; work is a projection over cards, not a second store.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkCapabilitiesResponse`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "conflict",
            "work_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.",
        "adjacent_commands": [
            "work.create",
            "work.get",
            "work.list",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.list",
            "work.participants.register",
            "work.patch",
            "agents.me.presence",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "WorkCapabilities",
        "ts_method": "workCapabilities"
    },
    {
        "command_id": "work.create",
        "cli_path": "work create",
        "group": "work",
        "method": "POST",
        "path": "/work",
        "operation_id": "workCreate",
        "summary": "Register a card-backed commitment",
        "why": "Register a card-backed commitment. board_ref is optional; omitted uses the workspace default board.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkResponse`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "conflict",
            "work_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace authenticated. When board_ref is omitted, the server places the card on the workspace's oldest active board, creating a default Tasks board if none exists. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.",
        "body_schema": {
            "required": [
                {
                    "name": "title",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "blockers",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "board_ref",
                    "type": "string"
                },
                {
                    "name": "definition_of_done",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "document_ref",
                    "type": "string"
                },
                {
                    "name": "due_at",
                    "type": "string"
                },
                {
                    "name": "executions",
                    "type": "list\u003cobject\u003e"
                },
                {
                    "name": "id",
                    "type": "string"
                },
                {
                    "name": "labels",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "next_action",
                    "type": "string"
                },
                {
                    "name": "next_actor",
                    "type": "string"
                },
                {
                    "name": "owner",
                    "type": "string"
                },
                {
                    "name": "phase",
                    "type": "string"
                },
                {
                    "name": "plan",
                    "type": "any"
                },
                {
                    "name": "priority",
                    "type": "string"
                },
                {
                    "name": "project_ref",
                    "type": "string"
                },
                {
                    "name": "related_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "relations",
                    "type": "list\u003cobject\u003e"
                },
                {
                    "name": "risk",
                    "type": "string",
                    "enum_values": [
                        "critical",
                        "high",
                        "low",
                        "medium"
                    ]
                },
                {
                    "name": "roles",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "source.aliases",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "source.authority",
                    "type": "string"
                },
                {
                    "name": "source.connection_id",
                    "type": "string"
                },
                {
                    "name": "source.identifier",
                    "type": "string"
                },
                {
                    "name": "source.identifier_aliases",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "source.native_id",
                    "type": "string"
                },
                {
                    "name": "source.native_status",
                    "type": "string"
                },
                {
                    "name": "source.revision",
                    "type": "string"
                },
                {
                    "name": "source.url",
                    "type": "string"
                },
                {
                    "name": "source_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "start_at",
                    "type": "string"
                },
                {
                    "name": "summary",
                    "type": "string"
                },
                {
                    "name": "topic_ref",
                    "type": "string"
                },
                {
                    "name": "wake_condition",
                    "type": "string"
                },
                {
                    "name": "workspace_move",
                    "type": "object"
                }
            ]
        },
        "adjacent_commands": [
            "work.capabilities",
            "work.get",
            "work.list",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.list",
            "work.participants.register",
            "work.patch",
            "agents.me.presence",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "WorkCreate",
        "ts_method": "workCreate"
    },
    {
        "command_id": "work.get",
        "cli_path": "work get",
        "group": "work",
        "method": "GET",
        "path": "/work/{card_ref}",
        "operation_id": "workGet",
        "summary": "Read a commitment and its evidence",
        "why": "Read a commitment and its evidence. Same card row as `cards.get`, with projection fields (freshness, observations, annotations).",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkResponse`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "conflict",
            "work_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.",
        "path_params": [
            "card_ref"
        ],
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.list",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.list",
            "work.participants.register",
            "work.patch",
            "agents.me.presence",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "WorkGet",
        "ts_method": "workGet"
    },
    {
        "command_id": "work.list",
        "cli_path": "work list",
        "group": "work",
        "method": "GET",
        "path": "/work",
        "operation_id": "workList",
        "summary": "List heterogeneous commitments",
        "description": "Operator Tasks projection over card rows. Most recently updated first by updated_at descending, then card id descending. Opaque cursors retain both ordering values; newer inserts and updates ahead of the cursor are visible on a fresh first page.",
        "why": "List the operator Tasks projection over cards. `work.*` adds acceptance criteria, observations, and freshness on the same rows as `cards.*`; use `cards.*` for the canonical store and card workflow writes.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkListResponse`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "conflict",
            "work_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.",
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.get",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.list",
            "work.participants.register",
            "work.patch",
            "agents.me.presence",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "WorkList",
        "ts_method": "workList"
    },
    {
        "command_id": "work.observations.list",
        "cli_path": "work observations list",
        "group": "work",
        "method": "GET",
        "path": "/work/{card_ref}/observations",
        "operation_id": "workObservationsList",
        "summary": "List append-only work observations",
        "why": "List append-only work observations.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkObservationListResponse`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "conflict",
            "work_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.",
        "path_params": [
            "card_ref"
        ],
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.get",
            "work.list",
            "work.observations.submit",
            "work.participants.list",
            "work.participants.register",
            "work.patch",
            "agents.me.presence",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "WorkObservationsList",
        "ts_method": "workObservationsList"
    },
    {
        "command_id": "work.observations.submit",
        "cli_path": "work observations submit",
        "group": "work",
        "method": "POST",
        "path": "/work/{card_ref}/observations",
        "operation_id": "workObservationsSubmit",
        "summary": "Submit an attributed source observation",
        "why": "Submit an attributed source observation.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkObservationResponse`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "conflict",
            "work_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.",
        "body_schema": {
            "required": [
                {
                    "name": "observation.idempotency_key",
                    "type": "string"
                },
                {
                    "name": "observation.observed_at",
                    "type": "string"
                },
                {
                    "name": "observation.reader_id",
                    "type": "string"
                },
                {
                    "name": "observation.reader_revision",
                    "type": "string"
                },
                {
                    "name": "observation.status",
                    "type": "string",
                    "enum_values": [
                        "error",
                        "reported",
                        "uncertain",
                        "verified"
                    ]
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "observation.actor_id",
                    "type": "string"
                },
                {
                    "name": "observation.coverage",
                    "type": "object"
                },
                {
                    "name": "observation.error.code",
                    "type": "string"
                },
                {
                    "name": "observation.error.message",
                    "type": "string"
                },
                {
                    "name": "observation.evidence",
                    "type": "list\u003cobject\u003e"
                },
                {
                    "name": "observation.facts",
                    "type": "object"
                },
                {
                    "name": "observation.id",
                    "type": "string"
                },
                {
                    "name": "observation.meaningful_progress_at",
                    "type": "string"
                },
                {
                    "name": "observation.received_at",
                    "type": "string"
                },
                {
                    "name": "observation.source_activity_at",
                    "type": "string"
                },
                {
                    "name": "observation.source_revision",
                    "type": "string"
                },
                {
                    "name": "observation.source_sequence",
                    "type": "integer"
                },
                {
                    "name": "observation.stale_after_seconds",
                    "type": "integer"
                },
                {
                    "name": "observation.uncertainty",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "observation.verification",
                    "type": "string",
                    "enum_values": [
                        "reported"
                    ]
                },
                {
                    "name": "observation.work_ref",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "card_ref"
        ],
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.get",
            "work.list",
            "work.observations.list",
            "work.participants.list",
            "work.participants.register",
            "work.patch",
            "agents.me.presence",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "WorkObservationsSubmit",
        "ts_method": "workObservationsSubmit"
    },
    {
        "command_id": "work.participants.list",
        "cli_path": "work participants list",
        "group": "work",
        "method": "GET",
        "path": "/work/{card_ref}/participants",
        "operation_id": "workParticipantsList",
        "summary": "List task-scoped participation",
        "description": "Ordered by task-scoped participant_id ascending. Opaque cursors are bound to this task. session_id is included only on the caller's own participations.",
        "why": "List task-scoped participation without assigning, moving, or completing work.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkParticipantListResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "sessions_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Existing bearer authentication is required, including in development mode. Registration and participation writes require an agent principal and never mint credentials. Sessions are private to that principal. Task reads expose only explicitly shared participation metadata; native session identifiers and other task links are never shared. Upserts refresh server-clock activity leases of 120 seconds. A session heartbeat does not refresh task participation. Session closure is terminal and never changes work state, assignees, source authority, or ownership. Session identity is independent of per-attempt /runs. Capabilities are caller-reported, not server-verified support.",
        "path_params": [
            "card_ref"
        ],
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.get",
            "work.list",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.register",
            "work.patch",
            "agents.me.presence",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "WorkParticipantsList",
        "ts_method": "workParticipantsList"
    },
    {
        "command_id": "work.participants.register",
        "cli_path": "work participants register",
        "group": "work",
        "method": "POST",
        "path": "/work/{card_ref}/participants",
        "operation_id": "workParticipantsRegister",
        "summary": "Register or refresh nonlocking task participation",
        "why": "Register or refresh nonlocking task participation without assigning, moving, or completing work.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkParticipantResponse`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "forbidden",
            "invalid_request",
            "not_found",
            "conflict",
            "sessions_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Existing bearer authentication is required, including in development mode. Registration and participation writes require an agent principal and never mint credentials. Sessions are private to that principal. Task reads expose only explicitly shared participation metadata; native session identifiers and other task links are never shared. Upserts refresh server-clock activity leases of 120 seconds. A session heartbeat does not refresh task participation. Session closure is terminal and never changes work state, assignees, source authority, or ownership. Session identity is independent of per-attempt /runs. Capabilities are caller-reported, not server-verified support.",
        "body_schema": {
            "required": [
                {
                    "name": "activity",
                    "type": "string",
                    "enum_values": [
                        "active",
                        "idle",
                        "left"
                    ]
                },
                {
                    "name": "sequence",
                    "type": "integer"
                },
                {
                    "name": "session_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "card_ref"
        ],
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.get",
            "work.list",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.list",
            "work.patch",
            "agents.me.presence",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "WorkParticipantsRegister",
        "ts_method": "workParticipantsRegister"
    },
    {
        "command_id": "work.patch",
        "cli_path": "work patch",
        "group": "work",
        "method": "PATCH",
        "path": "/work/{card_ref}",
        "operation_id": "workPatch",
        "summary": "Update local commitment annotations",
        "description": "Relation objects preserve additional metadata fields. Invalid relation shapes, kinds, or workspace refs return 400 invalid_request with field-specific guidance. Unmapped store failures return 500 internal_error with a server-generated X-Request-ID that correlates with a secret-safe diagnostic log.",
        "why": "Update local commitment annotations.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkResponse`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "conflict",
            "work_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.",
        "body_schema": {
            "required": [
                {
                    "name": "if_version",
                    "type": "integer"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "patch.blockers",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.due_at",
                    "type": "string"
                },
                {
                    "name": "patch.executions",
                    "type": "list\u003cobject\u003e"
                },
                {
                    "name": "patch.labels",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.next_action",
                    "type": "string"
                },
                {
                    "name": "patch.next_actor",
                    "type": "string"
                },
                {
                    "name": "patch.plan",
                    "type": "any"
                },
                {
                    "name": "patch.priority",
                    "type": "string"
                },
                {
                    "name": "patch.project_ref",
                    "type": "string"
                },
                {
                    "name": "patch.relations",
                    "type": "list\u003cobject\u003e"
                },
                {
                    "name": "patch.roles",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.source_refs",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.start_at",
                    "type": "string"
                },
                {
                    "name": "patch.wake_condition",
                    "type": "string"
                },
                {
                    "name": "patch.workspace_move",
                    "type": "object"
                }
            ]
        },
        "path_params": [
            "card_ref"
        ],
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.get",
            "work.list",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.list",
            "work.participants.register",
            "agents.me.presence",
            "work.refresh.get",
            "work.refresh.request"
        ],
        "go_method": "WorkPatch",
        "ts_method": "workPatch"
    },
    {
        "command_id": "work.refresh.get",
        "cli_path": "work refresh get",
        "group": "work",
        "method": "GET",
        "path": "/work/{card_ref}/refresh",
        "operation_id": "workRefreshGet",
        "summary": "Inspect durable refresh lifecycle",
        "why": "Inspect durable refresh lifecycle.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkRefreshResponse`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "conflict",
            "work_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.",
        "path_params": [
            "card_ref"
        ],
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.get",
            "work.list",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.list",
            "work.participants.register",
            "work.patch",
            "agents.me.presence",
            "work.refresh.request"
        ],
        "go_method": "WorkRefreshGet",
        "ts_method": "workRefreshGet"
    },
    {
        "command_id": "work.refresh.request",
        "cli_path": "work refresh request",
        "group": "work",
        "method": "POST",
        "path": "/work/{card_ref}/refresh",
        "operation_id": "workRefreshRequest",
        "summary": "Queue or coalesce a read-only refresh",
        "why": "Queue or coalesce a read-only refresh.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `WorkRefreshResponse`.",
        "error_codes": [
            "invalid_request",
            "not_found",
            "conflict",
            "work_unavailable"
        ],
        "concepts": [
            "cards",
            "evidence"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Workspace authenticated. Source-backed fields are read-only outside attributed observations; refresh acceptance is not a successful read.",
        "body_schema": {
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "card_ref"
        ],
        "adjacent_commands": [
            "work.capabilities",
            "work.create",
            "work.get",
            "work.list",
            "work.observations.list",
            "work.observations.submit",
            "work.participants.list",
            "work.participants.register",
            "work.patch",
            "agents.me.presence",
            "work.refresh.get"
        ],
        "go_method": "WorkRefreshRequest",
        "ts_method": "workRefreshRequest"
    },
    {
        "command_id": "workspace.dashboard.list",
        "cli_path": "workspace dashboard list",
        "group": "workspace",
        "method": "GET",
        "path": "/workspace/dashboard/reports",
        "operation_id": "listWorkspaceDashboardReports",
        "summary": "Load validated dashboard report candidates when opening the selector.",
        "why": "Defer selector-only report reads until the user or agent requests candidates.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns the dashboard report selector projection.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_request"
        ],
        "concepts": [
            "home",
            "documents"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Overview returns its selected dashboard only. Use this read for other pin candidates.",
        "adjacent_commands": [
            "workspace.dashboard.set"
        ],
        "go_method": "WorkspaceDashboardList",
        "ts_method": "workspaceDashboardList"
    },
    {
        "command_id": "workspace.dashboard.set",
        "cli_path": "workspace dashboard set",
        "group": "workspace",
        "method": "PUT",
        "path": "/workspace/dashboard",
        "operation_id": "setWorkspaceDashboard",
        "summary": "Pin an active visual-report document as the workspace dashboard; null clears the pin.",
        "why": "Pin an active visual-report document as the workspace dashboard; null clears the pin.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns the workspace Overview projection.",
        "error_codes": [
            "auth_required",
            "invalid_request",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "home",
            "documents",
            "cards"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Pass a document ref to pin it; pass null to return to newest-report selection.",
        "body_schema": {
            "required": [
                {
                    "name": "document_ref",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "workspace.dashboard.list"
        ],
        "go_method": "WorkspaceDashboardSet",
        "ts_method": "workspaceDashboardSet"
    }
];
const commandIndex = new Map(commandRegistry.map((command) => [command.command_id, command]));
function renderPath(pathTemplate, pathParams = {}) {
    return pathTemplate.replace(/\{([^{}]+)\}/g, (_match, name) => {
        const value = pathParams[name];
        if (value === undefined) {
            throw new Error(`missing path param ${name}`);
        }
        return encodeURIComponent(value);
    });
}
function withQuery(path, query) {
    if (!query) {
        return path;
    }
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(query)) {
        if (value === undefined) {
            continue;
        }
        if (Array.isArray(value)) {
            for (const entry of value) {
                params.append(key, String(entry));
            }
            continue;
        }
        params.set(key, String(value));
    }
    const encoded = params.toString();
    if (!encoded) {
        return path;
    }
    return `${path}?${encoded}`;
}
export class AnxClient {
    constructor(baseUrl, fetchFn = fetch) {
        this.baseUrl = String(baseUrl || "").replace(/\/+$/, "");
        this.fetchFn = fetchFn;
    }
    async invoke(commandId, pathParams = {}, options = {}) {
        if (!this.baseUrl) {
            throw new Error("baseUrl is required");
        }
        const command = commandIndex.get(commandId);
        if (!command) {
            throw new Error(`unknown command id: ${commandId}`);
        }
        const path = withQuery(renderPath(command.path, pathParams), options.query);
        const response = await this.fetchFn(`${this.baseUrl}${path}`, {
            method: command.method,
            headers: {
                accept: "application/json",
                ...(options.body !== undefined ? { "content-type": "application/json" } : {}),
                ...(options.headers ?? {}),
            },
            body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
        });
        const body = await response.text();
        if (!response.ok) {
            throw new Error(`request failed for ${commandId}: ${response.status} ${response.statusText} ${body}`);
        }
        return { status: response.status, headers: response.headers, body };
    }
    actorsCreate(options = {}) {
        return this.invoke("actors.create", {}, options);
    }
    actorsList(options = {}) {
        return this.invoke("actors.list", {}, options);
    }
    adaptersDeclare(options = {}) {
        return this.invoke("adapters.declare", {}, options);
    }
    adaptersDelete(pathParams, options = {}) {
        return this.invoke("adapters.delete", pathParams, options);
    }
    adaptersList(options = {}) {
        return this.invoke("adapters.list", {}, options);
    }
    adaptersRevoke(pathParams, options = {}) {
        return this.invoke("adapters.revoke", pathParams, options);
    }
    adaptersToken(pathParams, options = {}) {
        return this.invoke("adapters.token", pathParams, options);
    }
    agentInboxAnswersRead(options = {}) {
        return this.invoke("agent.inbox.answers.read", {}, options);
    }
    agentInboxAsksList(options = {}) {
        return this.invoke("agent.inbox.asks.list", {}, options);
    }
    agentNotificationReceiptsStream(options = {}) {
        return this.invoke("agent.notification-receipts.stream", {}, options);
    }
    agentNotificationsDismiss(options = {}) {
        return this.invoke("agent.notifications.dismiss", {}, options);
    }
    agentNotificationsList(options = {}) {
        return this.invoke("agent.notifications.list", {}, options);
    }
    agentNotificationsRead(options = {}) {
        return this.invoke("agent.notifications.read", {}, options);
    }
    agentsGet(pathParams, options = {}) {
        return this.invoke("agents.get", pathParams, options);
    }
    agentsList(options = {}) {
        return this.invoke("agents.list", {}, options);
    }
    agentsMeGet(options = {}) {
        return this.invoke("agents.me.get", {}, options);
    }
    agentsMePresence(options = {}) {
        return this.invoke("agents.me.presence", {}, options);
    }
    agentsStream(options = {}) {
        return this.invoke("agents.stream", {}, options);
    }
    artifactsArchive(pathParams, options = {}) {
        return this.invoke("artifacts.archive", pathParams, options);
    }
    artifactsAttachmentsCreate(options = {}) {
        return this.invoke("artifacts.attachments.create", {}, options);
    }
    artifactsContent(pathParams, options = {}) {
        return this.invoke("artifacts.content", pathParams, options);
    }
    artifactsCreate(options = {}) {
        return this.invoke("artifacts.create", {}, options);
    }
    artifactsGet(pathParams, options = {}) {
        return this.invoke("artifacts.get", pathParams, options);
    }
    artifactsList(options = {}) {
        return this.invoke("artifacts.list", {}, options);
    }
    artifactsPurge(pathParams, options = {}) {
        return this.invoke("artifacts.purge", pathParams, options);
    }
    artifactsRestore(pathParams, options = {}) {
        return this.invoke("artifacts.restore", pathParams, options);
    }
    artifactsTrash(pathParams, options = {}) {
        return this.invoke("artifacts.trash", pathParams, options);
    }
    artifactsUnarchive(pathParams, options = {}) {
        return this.invoke("artifacts.unarchive", pathParams, options);
    }
    authAccessRequestsApprove(pathParams, options = {}) {
        return this.invoke("auth.access-requests.approve", pathParams, options);
    }
    authAccessRequestsDeny(pathParams, options = {}) {
        return this.invoke("auth.access-requests.deny", pathParams, options);
    }
    authAccessRequestsList(options = {}) {
        return this.invoke("auth.access-requests.list", {}, options);
    }
    authAccessRequestsRequest(options = {}) {
        return this.invoke("auth.access-requests.request", {}, options);
    }
    authAccessRequestsSummary(options = {}) {
        return this.invoke("auth.access-requests.summary", {}, options);
    }
    authAdminsGrant(pathParams, options = {}) {
        return this.invoke("auth.admins.grant", pathParams, options);
    }
    authAdminsList(options = {}) {
        return this.invoke("auth.admins.list", {}, options);
    }
    authAdminsRevoke(pathParams, options = {}) {
        return this.invoke("auth.admins.revoke", pathParams, options);
    }
    authAuditList(options = {}) {
        return this.invoke("auth.audit.list", {}, options);
    }
    authBootstrapStatus(options = {}) {
        return this.invoke("auth.bootstrap.status", {}, options);
    }
    authInvitesCreate(options = {}) {
        return this.invoke("auth.invites.create", {}, options);
    }
    authInvitesList(options = {}) {
        return this.invoke("auth.invites.list", {}, options);
    }
    authInvitesRevoke(pathParams, options = {}) {
        return this.invoke("auth.invites.revoke", pathParams, options);
    }
    authPasskeyDevLogin(options = {}) {
        return this.invoke("auth.passkey.dev.login", {}, options);
    }
    authPasskeyDevRegister(options = {}) {
        return this.invoke("auth.passkey.dev.register", {}, options);
    }
    authPasskeyLoginOptions(options = {}) {
        return this.invoke("auth.passkey.login.options", {}, options);
    }
    authPasskeyLoginVerify(options = {}) {
        return this.invoke("auth.passkey.login.verify", {}, options);
    }
    authPasskeyRegisterOptions(options = {}) {
        return this.invoke("auth.passkey.register.options", {}, options);
    }
    authPasskeyRegisterVerify(options = {}) {
        return this.invoke("auth.passkey.register.verify", {}, options);
    }
    authPrincipalsList(options = {}) {
        return this.invoke("auth.principals.list", {}, options);
    }
    authPrincipalsRevoke(pathParams, options = {}) {
        return this.invoke("auth.principals.revoke", pathParams, options);
    }
    authToken(options = {}) {
        return this.invoke("auth.token", {}, options);
    }
    boardsArchive(pathParams, options = {}) {
        return this.invoke("boards.archive", pathParams, options);
    }
    boardsCardsBatchAdd(pathParams, options = {}) {
        return this.invoke("boards.cards.batch_add", pathParams, options);
    }
    boardsCardsGet(pathParams, options = {}) {
        return this.invoke("boards.cards.get", pathParams, options);
    }
    boardsCardsList(pathParams, options = {}) {
        return this.invoke("boards.cards.list", pathParams, options);
    }
    boardsCreate(options = {}) {
        return this.invoke("boards.create", {}, options);
    }
    boardsGet(pathParams, options = {}) {
        return this.invoke("boards.get", pathParams, options);
    }
    boardsList(options = {}) {
        return this.invoke("boards.list", {}, options);
    }
    boardsPatch(pathParams, options = {}) {
        return this.invoke("boards.patch", pathParams, options);
    }
    boardsPurge(pathParams, options = {}) {
        return this.invoke("boards.purge", pathParams, options);
    }
    boardsRestore(pathParams, options = {}) {
        return this.invoke("boards.restore", pathParams, options);
    }
    boardsTrash(pathParams, options = {}) {
        return this.invoke("boards.trash", pathParams, options);
    }
    boardsUnarchive(pathParams, options = {}) {
        return this.invoke("boards.unarchive", pathParams, options);
    }
    boardsWorkspace(pathParams, options = {}) {
        return this.invoke("boards.workspace", pathParams, options);
    }
    cardsArchive(pathParams, options = {}) {
        return this.invoke("cards.archive", pathParams, options);
    }
    cardsCreate(options = {}) {
        return this.invoke("cards.create", {}, options);
    }
    cardsGet(pathParams, options = {}) {
        return this.invoke("cards.get", pathParams, options);
    }
    cardsList(options = {}) {
        return this.invoke("cards.list", {}, options);
    }
    cardsMove(pathParams, options = {}) {
        return this.invoke("cards.move", pathParams, options);
    }
    cardsPatch(pathParams, options = {}) {
        return this.invoke("cards.patch", pathParams, options);
    }
    cardsPurge(pathParams, options = {}) {
        return this.invoke("cards.purge", pathParams, options);
    }
    cardsRestore(pathParams, options = {}) {
        return this.invoke("cards.restore", pathParams, options);
    }
    cardsRevisionsCreate(pathParams, options = {}) {
        return this.invoke("cards.revisions.create", pathParams, options);
    }
    cardsRevisionsGet(pathParams, options = {}) {
        return this.invoke("cards.revisions.get", pathParams, options);
    }
    cardsRevisionsList(pathParams, options = {}) {
        return this.invoke("cards.revisions.list", pathParams, options);
    }
    cardsTimeline(pathParams, options = {}) {
        return this.invoke("cards.timeline", pathParams, options);
    }
    cardsTrash(pathParams, options = {}) {
        return this.invoke("cards.trash", pathParams, options);
    }
    derivedRebuild(options = {}) {
        return this.invoke("derived.rebuild", {}, options);
    }
    docsArchive(pathParams, options = {}) {
        return this.invoke("docs.archive", pathParams, options);
    }
    docsCommentsCreate(pathParams, options = {}) {
        return this.invoke("docs.comments.create", pathParams, options);
    }
    docsCommentsDelete(pathParams, options = {}) {
        return this.invoke("docs.comments.delete", pathParams, options);
    }
    docsCommentsList(pathParams, options = {}) {
        return this.invoke("docs.comments.list", pathParams, options);
    }
    docsCommentsReply(pathParams, options = {}) {
        return this.invoke("docs.comments.reply", pathParams, options);
    }
    docsCommentsUpdate(pathParams, options = {}) {
        return this.invoke("docs.comments.update", pathParams, options);
    }
    docsCreate(options = {}) {
        return this.invoke("docs.create", {}, options);
    }
    docsGet(pathParams, options = {}) {
        return this.invoke("docs.get", pathParams, options);
    }
    docsList(options = {}) {
        return this.invoke("docs.list", {}, options);
    }
    docsPatch(pathParams, options = {}) {
        return this.invoke("docs.patch", pathParams, options);
    }
    docsPurge(pathParams, options = {}) {
        return this.invoke("docs.purge", pathParams, options);
    }
    docsPut(pathParams, options = {}) {
        return this.invoke("docs.put", pathParams, options);
    }
    docsRestore(pathParams, options = {}) {
        return this.invoke("docs.restore", pathParams, options);
    }
    docsRevisionsCreate(pathParams, options = {}) {
        return this.invoke("docs.revisions.create", pathParams, options);
    }
    docsRevisionsGet(pathParams, options = {}) {
        return this.invoke("docs.revisions.get", pathParams, options);
    }
    docsRevisionsList(pathParams, options = {}) {
        return this.invoke("docs.revisions.list", pathParams, options);
    }
    docsSearch(options = {}) {
        return this.invoke("docs.search", {}, options);
    }
    docsTrash(pathParams, options = {}) {
        return this.invoke("docs.trash", pathParams, options);
    }
    docsUnarchive(pathParams, options = {}) {
        return this.invoke("docs.unarchive", pathParams, options);
    }
    eventsArchive(pathParams, options = {}) {
        return this.invoke("events.archive", pathParams, options);
    }
    eventsCreate(options = {}) {
        return this.invoke("events.create", {}, options);
    }
    eventsGet(pathParams, options = {}) {
        return this.invoke("events.get", pathParams, options);
    }
    eventsList(options = {}) {
        return this.invoke("events.list", {}, options);
    }
    eventsRestore(pathParams, options = {}) {
        return this.invoke("events.restore", pathParams, options);
    }
    eventsStream(options = {}) {
        return this.invoke("events.stream", {}, options);
    }
    eventsTrash(pathParams, options = {}) {
        return this.invoke("events.trash", pathParams, options);
    }
    eventsUnarchive(pathParams, options = {}) {
        return this.invoke("events.unarchive", pathParams, options);
    }
    homeRead(options = {}) {
        return this.invoke("home.read", {}, options);
    }
    homeUnread(options = {}) {
        return this.invoke("home.unread", {}, options);
    }
    hostsBridgeCheckIn(pathParams, options = {}) {
        return this.invoke("hosts.bridge.check_in", pathParams, options);
    }
    hostsEnrollApprove(pathParams, options = {}) {
        return this.invoke("hosts.enroll.approve", pathParams, options);
    }
    hostsEnrollComplete(pathParams, options = {}) {
        return this.invoke("hosts.enroll.complete", pathParams, options);
    }
    hostsEnrollDeny(pathParams, options = {}) {
        return this.invoke("hosts.enroll.deny", pathParams, options);
    }
    hostsEnrollHeadless(options = {}) {
        return this.invoke("hosts.enroll.headless", {}, options);
    }
    hostsEnrollPending(options = {}) {
        return this.invoke("hosts.enroll.pending", {}, options);
    }
    hostsEnrollPoll(pathParams, options = {}) {
        return this.invoke("hosts.enroll.poll", pathParams, options);
    }
    hostsEnrollStart(options = {}) {
        return this.invoke("hosts.enroll.start", {}, options);
    }
    hostsGet(pathParams, options = {}) {
        return this.invoke("hosts.get", pathParams, options);
    }
    hostsList(options = {}) {
        return this.invoke("hosts.list", {}, options);
    }
    hostsPatch(pathParams, options = {}) {
        return this.invoke("hosts.patch", pathParams, options);
    }
    hostsRevoke(pathParams, options = {}) {
        return this.invoke("hosts.revoke", pathParams, options);
    }
    hostsTokensCreate(options = {}) {
        return this.invoke("hosts.tokens.create", {}, options);
    }
    hostsTokensList(options = {}) {
        return this.invoke("hosts.tokens.list", {}, options);
    }
    hostsTokensRevoke(pathParams, options = {}) {
        return this.invoke("hosts.tokens.revoke", pathParams, options);
    }
    inboxGet(pathParams, options = {}) {
        return this.invoke("inbox.get", pathParams, options);
    }
    inboxList(options = {}) {
        return this.invoke("inbox.list", {}, options);
    }
    inboxRespond(pathParams, options = {}) {
        return this.invoke("inbox.respond", pathParams, options);
    }
    inboxStream(options = {}) {
        return this.invoke("inbox.stream", {}, options);
    }
    inboxSummary(options = {}) {
        return this.invoke("inbox.summary", {}, options);
    }
    metaCommandsGet(pathParams, options = {}) {
        return this.invoke("meta.commands.get", pathParams, options);
    }
    metaCommandsList(options = {}) {
        return this.invoke("meta.commands.list", {}, options);
    }
    metaConceptsGet(pathParams, options = {}) {
        return this.invoke("meta.concepts.get", pathParams, options);
    }
    metaConceptsList(options = {}) {
        return this.invoke("meta.concepts.list", {}, options);
    }
    metaHandshake(options = {}) {
        return this.invoke("meta.handshake", {}, options);
    }
    metaHealth(options = {}) {
        return this.invoke("meta.health", {}, options);
    }
    metaLivez(options = {}) {
        return this.invoke("meta.livez", {}, options);
    }
    metaReadyz(options = {}) {
        return this.invoke("meta.readyz", {}, options);
    }
    metaVersion(options = {}) {
        return this.invoke("meta.version", {}, options);
    }
    opsBlobUsageRebuild(options = {}) {
        return this.invoke("ops.blob.usage.rebuild", {}, options);
    }
    opsHealth(options = {}) {
        return this.invoke("ops.health", {}, options);
    }
    opsUsageSummary(options = {}) {
        return this.invoke("ops.usage.summary", {}, options);
    }
    overviewChanges(options = {}) {
        return this.invoke("overview.changes", {}, options);
    }
    overviewGet(options = {}) {
        return this.invoke("overview.get", {}, options);
    }
    planSet(pathParams, options = {}) {
        return this.invoke("plan.set", pathParams, options);
    }
    planShow(pathParams, options = {}) {
        return this.invoke("plan.show", pathParams, options);
    }
    pmActionsAcknowledge(pathParams, options = {}) {
        return this.invoke("pm.actions.acknowledge", pathParams, options);
    }
    pmActionsGet(pathParams, options = {}) {
        return this.invoke("pm.actions.get", pathParams, options);
    }
    pmActionsList(options = {}) {
        return this.invoke("pm.actions.list", {}, options);
    }
    pmActionsReconcile(pathParams, options = {}) {
        return this.invoke("pm.actions.reconcile", pathParams, options);
    }
    pmBindingsCreate(options = {}) {
        return this.invoke("pm.bindings.create", {}, options);
    }
    pmBindingsList(options = {}) {
        return this.invoke("pm.bindings.list", {}, options);
    }
    pmContext(options = {}) {
        return this.invoke("pm.context", {}, options);
    }
    pmConversationsCreate(options = {}) {
        return this.invoke("pm.conversations.create", {}, options);
    }
    pmConversationsGet(pathParams, options = {}) {
        return this.invoke("pm.conversations.get", pathParams, options);
    }
    pmConversationsList(options = {}) {
        return this.invoke("pm.conversations.list", {}, options);
    }
    pmConversationsMessagesCreate(pathParams, options = {}) {
        return this.invoke("pm.conversations.messages.create", pathParams, options);
    }
    pmDecisionsAnswer(pathParams, options = {}) {
        return this.invoke("pm.decisions.answer", pathParams, options);
    }
    pmDecisionsCreate(options = {}) {
        return this.invoke("pm.decisions.create", {}, options);
    }
    pmDecisionsDispatch(pathParams, options = {}) {
        return this.invoke("pm.decisions.dispatch", pathParams, options);
    }
    pmDecisionsGet(pathParams, options = {}) {
        return this.invoke("pm.decisions.get", pathParams, options);
    }
    pmDecisionsList(options = {}) {
        return this.invoke("pm.decisions.list", {}, options);
    }
    pmTurnsClaim(options = {}) {
        return this.invoke("pm.turns.claim", {}, options);
    }
    pmTurnsComplete(pathParams, options = {}) {
        return this.invoke("pm.turns.complete", pathParams, options);
    }
    pmTurnsContext(pathParams, options = {}) {
        return this.invoke("pm.turns.context", pathParams, options);
    }
    pmTurnsDecisionsCreate(pathParams, options = {}) {
        return this.invoke("pm.turns.decisions.create", pathParams, options);
    }
    pmTurnsFail(pathParams, options = {}) {
        return this.invoke("pm.turns.fail", pathParams, options);
    }
    pmTurnsGet(pathParams, options = {}) {
        return this.invoke("pm.turns.get", pathParams, options);
    }
    pmTurnsHeartbeat(pathParams, options = {}) {
        return this.invoke("pm.turns.heartbeat", pathParams, options);
    }
    pmTurnsRelease(pathParams, options = {}) {
        return this.invoke("pm.turns.release", pathParams, options);
    }
    refEdgesList(options = {}) {
        return this.invoke("ref_edges.list", {}, options);
    }
    refsResolve(options = {}) {
        return this.invoke("refs.resolve", {}, options);
    }
    reportPreview(options = {}) {
        return this.invoke("report.preview", {}, options);
    }
    reportRender(pathParams, options = {}) {
        return this.invoke("report.render", pathParams, options);
    }
    runsGet(pathParams, options = {}) {
        return this.invoke("runs.get", pathParams, options);
    }
    runsList(options = {}) {
        return this.invoke("runs.list", {}, options);
    }
    runsUpsert(options = {}) {
        return this.invoke("runs.upsert", {}, options);
    }
    secretsCreate(options = {}) {
        return this.invoke("secrets.create", {}, options);
    }
    secretsDelete(pathParams, options = {}) {
        return this.invoke("secrets.delete", pathParams, options);
    }
    secretsList(options = {}) {
        return this.invoke("secrets.list", {}, options);
    }
    secretsReveal(pathParams, options = {}) {
        return this.invoke("secrets.reveal", pathParams, options);
    }
    secretsRevealBatch(options = {}) {
        return this.invoke("secrets.reveal-batch", {}, options);
    }
    secretsUpdate(pathParams, options = {}) {
        return this.invoke("secrets.update", pathParams, options);
    }
    seriesList(options = {}) {
        return this.invoke("series.list", {}, options);
    }
    seriesPush(pathParams, options = {}) {
        return this.invoke("series.push", pathParams, options);
    }
    seriesQuery(pathParams, options = {}) {
        return this.invoke("series.query", pathParams, options);
    }
    seriesShow(pathParams, options = {}) {
        return this.invoke("series.show", pathParams, options);
    }
    sessionsGet(pathParams, options = {}) {
        return this.invoke("sessions.get", pathParams, options);
    }
    sessionsRegister(options = {}) {
        return this.invoke("sessions.register", {}, options);
    }
    threadsContext(pathParams, options = {}) {
        return this.invoke("threads.context", pathParams, options);
    }
    threadsInspect(pathParams, options = {}) {
        return this.invoke("threads.inspect", pathParams, options);
    }
    threadsList(options = {}) {
        return this.invoke("threads.list", {}, options);
    }
    threadsTimeline(pathParams, options = {}) {
        return this.invoke("threads.timeline", pathParams, options);
    }
    threadsWorkspace(pathParams, options = {}) {
        return this.invoke("threads.workspace", pathParams, options);
    }
    topicsArchive(pathParams, options = {}) {
        return this.invoke("topics.archive", pathParams, options);
    }
    topicsCreate(options = {}) {
        return this.invoke("topics.create", {}, options);
    }
    topicsGet(pathParams, options = {}) {
        return this.invoke("topics.get", pathParams, options);
    }
    topicsList(options = {}) {
        return this.invoke("topics.list", {}, options);
    }
    topicsPatch(pathParams, options = {}) {
        return this.invoke("topics.patch", pathParams, options);
    }
    topicsRestore(pathParams, options = {}) {
        return this.invoke("topics.restore", pathParams, options);
    }
    topicsTimeline(pathParams, options = {}) {
        return this.invoke("topics.timeline", pathParams, options);
    }
    topicsTrash(pathParams, options = {}) {
        return this.invoke("topics.trash", pathParams, options);
    }
    topicsUnarchive(pathParams, options = {}) {
        return this.invoke("topics.unarchive", pathParams, options);
    }
    topicsWorkspace(pathParams, options = {}) {
        return this.invoke("topics.workspace", pathParams, options);
    }
    usageSummaryV1(options = {}) {
        return this.invoke("usage.summary.v1", {}, options);
    }
    workCapabilities(options = {}) {
        return this.invoke("work.capabilities", {}, options);
    }
    workCreate(options = {}) {
        return this.invoke("work.create", {}, options);
    }
    workGet(pathParams, options = {}) {
        return this.invoke("work.get", pathParams, options);
    }
    workList(options = {}) {
        return this.invoke("work.list", {}, options);
    }
    workObservationsList(pathParams, options = {}) {
        return this.invoke("work.observations.list", pathParams, options);
    }
    workObservationsSubmit(pathParams, options = {}) {
        return this.invoke("work.observations.submit", pathParams, options);
    }
    workParticipantsList(pathParams, options = {}) {
        return this.invoke("work.participants.list", pathParams, options);
    }
    workParticipantsRegister(pathParams, options = {}) {
        return this.invoke("work.participants.register", pathParams, options);
    }
    workPatch(pathParams, options = {}) {
        return this.invoke("work.patch", pathParams, options);
    }
    workRefreshGet(pathParams, options = {}) {
        return this.invoke("work.refresh.get", pathParams, options);
    }
    workRefreshRequest(pathParams, options = {}) {
        return this.invoke("work.refresh.request", pathParams, options);
    }
    workspaceDashboardList(options = {}) {
        return this.invoke("workspace.dashboard.list", {}, options);
    }
    workspaceDashboardSet(options = {}) {
        return this.invoke("workspace.dashboard.set", {}, options);
    }
}
