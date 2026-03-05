export type HttpMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

export interface Example {
  title: string;
  command: string;
  description?: string;
}

export interface CommandSpec {
  command_id: string;
  cli_path: string;
  method: HttpMethod;
  path: string;
  operation_id: string;
  summary?: string;
  description?: string;
  why?: string;
  group?: string;
  path_params?: string[];
  input_mode?: string;
  streaming?: unknown;
  output_envelope?: string;
  error_codes?: string[];
  stability?: string;
  agent_notes?: string;
  concepts?: string[];
  adjacent_commands?: string[];
  examples?: Example[];
  go_method: string;
  ts_method: string;
}

export interface RequestOptions {
  query?: Record<string, string | number | boolean | Array<string | number | boolean> | undefined>;
  headers?: Record<string, string>;
  body?: unknown;
}

export interface InvokeResult {
  status: number;
  headers: Headers;
  body: string;
}

export const commandRegistry: CommandSpec[] = [
  {
    "command_id": "actors.list",
    "cli_path": "actors list",
    "group": "actors",
    "method": "GET",
    "path": "/actors",
    "operation_id": "listActors",
    "summary": "List actors",
    "why": "Resolve available actor identities for routing writes.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ actors }` ordered by created time ascending.",
    "error_codes": [
      "actor_registry_unavailable"
    ],
    "concepts": [
      "identity"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "List actors",
        "command": "oar actors list --json"
      }
    ],
    "adjacent_commands": [
      "actors.register"
    ],
    "go_method": "ActorsList",
    "ts_method": "actorsList"
  },
  {
    "command_id": "actors.register",
    "cli_path": "actors register",
    "group": "actors",
    "method": "POST",
    "path": "/actors",
    "operation_id": "registerActor",
    "summary": "Register actor identity metadata",
    "why": "Bootstrap an authenticated caller identity before mutating thread state.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ actor }` with canonicalized stored values.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "actor_exists"
    ],
    "concepts": [
      "identity"
    ],
    "stability": "stable",
    "agent_notes": "Not idempotent by default; repeated creates with same id return conflict.",
    "examples": [
      {
        "title": "Register actor",
        "command": "oar actors register --id bot-1 --display-name \"Bot 1\" --created-at 2026-03-04T10:00:00Z --json"
      }
    ],
    "adjacent_commands": [
      "actors.list"
    ],
    "go_method": "ActorsRegister",
    "ts_method": "actorsRegister"
  },
  {
    "command_id": "agents.me.get",
    "cli_path": "agents me get",
    "group": "agents",
    "method": "GET",
    "path": "/agents/me",
    "operation_id": "getCurrentAgent",
    "summary": "Read authenticated agent profile",
    "why": "Inspect current principal metadata and active/revoked keys.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ agent, keys }`.",
    "error_codes": [
      "auth_required",
      "invalid_token",
      "agent_revoked"
    ],
    "concepts": [
      "auth",
      "identity"
    ],
    "stability": "beta",
    "agent_notes": "Requires Bearer access token.",
    "examples": [
      {
        "title": "Get current profile",
        "command": "oar agents me get --json"
      }
    ],
    "adjacent_commands": [
      "agents.me.keys.rotate",
      "agents.me.patch",
      "agents.me.revoke"
    ],
    "go_method": "AgentsMeGet",
    "ts_method": "agentsMeGet"
  },
  {
    "command_id": "agents.me.keys.rotate",
    "cli_path": "agents me keys rotate",
    "group": "agents",
    "method": "POST",
    "path": "/agents/me/keys/rotate",
    "operation_id": "rotateCurrentAgentKey",
    "summary": "Rotate authenticated agent key",
    "why": "Replace the assertion key and invalidate the old key path.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ key }` for the new active key.",
    "error_codes": [
      "auth_required",
      "invalid_token",
      "agent_revoked",
      "invalid_request"
    ],
    "concepts": [
      "auth",
      "key-management"
    ],
    "stability": "beta",
    "agent_notes": "Old keys are marked revoked and cannot mint assertion tokens.",
    "examples": [
      {
        "title": "Rotate key",
        "command": "oar agents me keys rotate --public-key \u003cbase64-ed25519-pubkey\u003e --json"
      }
    ],
    "adjacent_commands": [
      "agents.me.get",
      "agents.me.patch",
      "agents.me.revoke"
    ],
    "go_method": "AgentsMeKeysRotate",
    "ts_method": "agentsMeKeysRotate"
  },
  {
    "command_id": "agents.me.patch",
    "cli_path": "agents me patch",
    "group": "agents",
    "method": "PATCH",
    "path": "/agents/me",
    "operation_id": "patchCurrentAgent",
    "summary": "Update authenticated agent profile",
    "why": "Rename the authenticated agent without re-registration.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ agent }`.",
    "error_codes": [
      "auth_required",
      "invalid_token",
      "agent_revoked",
      "invalid_request",
      "username_taken"
    ],
    "concepts": [
      "auth",
      "identity"
    ],
    "stability": "beta",
    "agent_notes": "Requires Bearer access token.",
    "examples": [
      {
        "title": "Rename current agent",
        "command": "oar agents me patch --username renamed_agent --json"
      }
    ],
    "adjacent_commands": [
      "agents.me.get",
      "agents.me.keys.rotate",
      "agents.me.revoke"
    ],
    "go_method": "AgentsMePatch",
    "ts_method": "agentsMePatch"
  },
  {
    "command_id": "agents.me.revoke",
    "cli_path": "agents me revoke",
    "group": "agents",
    "method": "POST",
    "path": "/agents/me/revoke",
    "operation_id": "revokeCurrentAgent",
    "summary": "Self-revoke current agent principal",
    "why": "Permanently revoke the authenticated agent so future mint/refresh calls fail.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ ok: true }` on first successful revoke.",
    "error_codes": [
      "auth_required",
      "invalid_token",
      "agent_revoked"
    ],
    "concepts": [
      "auth",
      "revocation"
    ],
    "stability": "beta",
    "agent_notes": "Requires Bearer access token.",
    "examples": [
      {
        "title": "Revoke self",
        "command": "oar agents me revoke --json"
      }
    ],
    "adjacent_commands": [
      "agents.me.get",
      "agents.me.keys.rotate",
      "agents.me.patch"
    ],
    "go_method": "AgentsMeRevoke",
    "ts_method": "agentsMeRevoke"
  },
  {
    "command_id": "artifacts.content.get",
    "cli_path": "artifacts content get",
    "group": "artifacts",
    "method": "GET",
    "path": "/artifacts/{artifact_id}/content",
    "operation_id": "getArtifactContent",
    "summary": "Get artifact raw content",
    "why": "Fetch opaque artifact bytes for downstream processors.",
    "input_mode": "none",
    "streaming": {
      "mode": "raw"
    },
    "output_envelope": "Raw bytes; content type mirrors stored artifact media.",
    "error_codes": [
      "not_found"
    ],
    "concepts": [
      "artifacts",
      "content"
    ],
    "stability": "stable",
    "agent_notes": "Stream to file for large payloads.",
    "examples": [
      {
        "title": "Download content",
        "command": "oar artifacts content get --artifact-id artifact_123 \u003e artifact.bin"
      }
    ],
    "path_params": [
      "artifact_id"
    ],
    "adjacent_commands": [
      "artifacts.create",
      "artifacts.get",
      "artifacts.list"
    ],
    "go_method": "ArtifactsContentGet",
    "ts_method": "artifactsContentGet"
  },
  {
    "command_id": "artifacts.create",
    "cli_path": "artifacts create",
    "group": "artifacts",
    "method": "POST",
    "path": "/artifacts",
    "operation_id": "createArtifact",
    "summary": "Create artifact",
    "why": "Persist immutable evidence blobs and metadata for references and review.",
    "input_mode": "file-and-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ artifact }` metadata after content write.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id"
    ],
    "concepts": [
      "artifacts",
      "evidence"
    ],
    "stability": "stable",
    "agent_notes": "Treat as non-idempotent unless caller controls artifact id collisions.",
    "examples": [
      {
        "title": "Create structured artifact",
        "command": "oar artifacts create --from-file artifact-create.json --json"
      }
    ],
    "adjacent_commands": [
      "artifacts.content.get",
      "artifacts.get",
      "artifacts.list"
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
    "summary": "Get artifact metadata by id",
    "why": "Resolve artifact refs before downloading or rendering content.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ artifact }` metadata.",
    "error_codes": [
      "not_found"
    ],
    "concepts": [
      "artifacts"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Get artifact",
        "command": "oar artifacts get --artifact-id artifact_123 --json"
      }
    ],
    "path_params": [
      "artifact_id"
    ],
    "adjacent_commands": [
      "artifacts.content.get",
      "artifacts.create",
      "artifacts.list"
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
    "summary": "List artifact metadata",
    "why": "Discover evidence and packets attached to threads.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ artifacts }` metadata only.",
    "error_codes": [
      "invalid_request"
    ],
    "concepts": [
      "artifacts",
      "filtering"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "List work orders for a thread",
        "command": "oar artifacts list --kind work_order --thread-id thread_123 --json"
      }
    ],
    "adjacent_commands": [
      "artifacts.content.get",
      "artifacts.create",
      "artifacts.get"
    ],
    "go_method": "ArtifactsList",
    "ts_method": "artifactsList"
  },
  {
    "command_id": "auth.agents.register",
    "cli_path": "auth agents register",
    "group": "auth",
    "method": "POST",
    "path": "/auth/agents/register",
    "operation_id": "registerAgent",
    "summary": "Register agent principal and initial key",
    "why": "Bootstrap an authenticated agent identity and obtain initial access + refresh tokens.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ agent, key, tokens }`.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "username_taken"
    ],
    "concepts": [
      "auth",
      "identity"
    ],
    "stability": "beta",
    "agent_notes": "Registration is open in v0; future invite/secret gating can wrap this endpoint.",
    "examples": [
      {
        "title": "Register agent",
        "command": "oar auth agents register --username agent.one --public-key \u003cbase64-ed25519-pubkey\u003e --json"
      }
    ],
    "adjacent_commands": [
      "auth.token"
    ],
    "go_method": "AuthAgentsRegister",
    "ts_method": "authAgentsRegister"
  },
  {
    "command_id": "auth.token",
    "cli_path": "auth token",
    "group": "auth",
    "method": "POST",
    "path": "/auth/token",
    "operation_id": "issueAuthToken",
    "summary": "Mint or refresh token bundle",
    "why": "Exchange a refresh token or key assertion for a fresh token bundle.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ tokens }`.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "invalid_token",
      "key_mismatch",
      "agent_revoked"
    ],
    "concepts": [
      "auth",
      "token-lifecycle"
    ],
    "stability": "beta",
    "agent_notes": "Refresh tokens are one-time use and rotated on successful exchange.",
    "examples": [
      {
        "title": "Refresh token grant",
        "command": "oar auth token --grant-type refresh_token --refresh-token \u003ctoken\u003e --json"
      },
      {
        "title": "Assertion grant",
        "command": "oar auth token --grant-type assertion --agent-id \u003cid\u003e --key-id \u003cid\u003e --signed-at \u003crfc3339\u003e --signature \u003cbase64\u003e --json"
      }
    ],
    "adjacent_commands": [
      "auth.agents.register"
    ],
    "go_method": "AuthToken",
    "ts_method": "authToken"
  },
  {
    "command_id": "commitments.create",
    "cli_path": "commitments create",
    "group": "commitments",
    "method": "POST",
    "path": "/commitments",
    "operation_id": "createCommitment",
    "summary": "Create commitment snapshot",
    "why": "Track accountable work items tied to a thread.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ commitment }` with generated id.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id"
    ],
    "concepts": [
      "commitments"
    ],
    "stability": "stable",
    "agent_notes": "Non-idempotent unless caller controls external dedupe.",
    "examples": [
      {
        "title": "Create commitment",
        "command": "oar commitments create --from-file commitment.json --json"
      }
    ],
    "adjacent_commands": [
      "commitments.get",
      "commitments.list",
      "commitments.patch"
    ],
    "go_method": "CommitmentsCreate",
    "ts_method": "commitmentsCreate"
  },
  {
    "command_id": "commitments.get",
    "cli_path": "commitments get",
    "group": "commitments",
    "method": "GET",
    "path": "/commitments/{commitment_id}",
    "operation_id": "getCommitment",
    "summary": "Get commitment by id",
    "why": "Read commitment status/details before status transitions.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ commitment }`.",
    "error_codes": [
      "not_found"
    ],
    "concepts": [
      "commitments"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Get commitment",
        "command": "oar commitments get --commitment-id commitment_123 --json"
      }
    ],
    "path_params": [
      "commitment_id"
    ],
    "adjacent_commands": [
      "commitments.create",
      "commitments.list",
      "commitments.patch"
    ],
    "go_method": "CommitmentsGet",
    "ts_method": "commitmentsGet"
  },
  {
    "command_id": "commitments.list",
    "cli_path": "commitments list",
    "group": "commitments",
    "method": "GET",
    "path": "/commitments",
    "operation_id": "listCommitments",
    "summary": "List commitments",
    "why": "Monitor open/blocked work and due windows.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ commitments }`.",
    "error_codes": [
      "invalid_request"
    ],
    "concepts": [
      "commitments",
      "filtering"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "List open commitments for a thread",
        "command": "oar commitments list --thread-id thread_123 --status open --json"
      }
    ],
    "adjacent_commands": [
      "commitments.create",
      "commitments.get",
      "commitments.patch"
    ],
    "go_method": "CommitmentsList",
    "ts_method": "commitmentsList"
  },
  {
    "command_id": "commitments.patch",
    "cli_path": "commitments patch",
    "group": "commitments",
    "method": "PATCH",
    "path": "/commitments/{commitment_id}",
    "operation_id": "patchCommitment",
    "summary": "Patch commitment snapshot",
    "why": "Update ownership, due date, or status with evidence-aware transition rules.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ commitment }` and emits a status-change event when applicable.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id",
      "conflict",
      "not_found"
    ],
    "concepts": [
      "commitments",
      "patch",
      "provenance"
    ],
    "stability": "stable",
    "agent_notes": "Provide `refs` for restricted transitions and use `if_updated_at` to avoid lost updates.",
    "examples": [
      {
        "title": "Mark commitment done",
        "command": "oar commitments patch --commitment-id commitment_123 --from-file commitment-patch.json --json"
      }
    ],
    "path_params": [
      "commitment_id"
    ],
    "adjacent_commands": [
      "commitments.create",
      "commitments.get",
      "commitments.list"
    ],
    "go_method": "CommitmentsPatch",
    "ts_method": "commitmentsPatch"
  },
  {
    "command_id": "derived.rebuild",
    "cli_path": "derived rebuild",
    "group": "derived",
    "method": "POST",
    "path": "/derived/rebuild",
    "operation_id": "rebuildDerivedViews",
    "summary": "Rebuild derived views",
    "why": "Force deterministic recomputation of derived views after maintenance or migration.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ ok: true }`.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id"
    ],
    "concepts": [
      "derived-views",
      "maintenance"
    ],
    "stability": "beta",
    "agent_notes": "Mutating admin command; serialize with other writes.",
    "examples": [
      {
        "title": "Rebuild derived",
        "command": "oar derived rebuild --actor-id system --json"
      }
    ],
    "go_method": "DerivedRebuild",
    "ts_method": "derivedRebuild"
  },
  {
    "command_id": "events.create",
    "cli_path": "events create",
    "group": "events",
    "method": "POST",
    "path": "/events",
    "operation_id": "createEvent",
    "summary": "Append event",
    "why": "Record append-only narrative or protocol state changes that complement snapshots.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ event }` with generated id and timestamp.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id"
    ],
    "concepts": [
      "events",
      "append-only"
    ],
    "stability": "stable",
    "agent_notes": "Non-idempotent unless external dedupe keying is used.",
    "examples": [
      {
        "title": "Append event",
        "command": "oar events create --from-file event.json --json"
      }
    ],
    "adjacent_commands": [
      "events.get",
      "events.stream"
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
    "operation_id": "getEvent",
    "summary": "Get event by id",
    "why": "Resolve event references and evidence links.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ event }`.",
    "error_codes": [
      "not_found"
    ],
    "concepts": [
      "events"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Get event",
        "command": "oar events get --event-id event_123 --json"
      }
    ],
    "path_params": [
      "event_id"
    ],
    "adjacent_commands": [
      "events.create",
      "events.stream"
    ],
    "go_method": "EventsGet",
    "ts_method": "eventsGet"
  },
  {
    "command_id": "events.stream",
    "cli_path": "events stream",
    "group": "events",
    "method": "GET",
    "path": "/events/stream",
    "operation_id": "streamEvents",
    "summary": "Stream events via Server-Sent Events (SSE)",
    "why": "Follow live event updates with resumable SSE semantics.",
    "input_mode": "none",
    "streaming": {
      "event_type": "event",
      "mode": "sse",
      "resumable": true
    },
    "output_envelope": "SSE stream where each event carries `{ event }` and uses event id for resume.",
    "error_codes": [
      "internal_error",
      "cli_outdated"
    ],
    "concepts": [
      "events",
      "streaming"
    ],
    "stability": "beta",
    "agent_notes": "Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.",
    "examples": [
      {
        "title": "Stream all events",
        "command": "oar events stream --json"
      },
      {
        "title": "Resume by id",
        "command": "oar events stream --last-event-id \u003cevent_id\u003e --json"
      }
    ],
    "adjacent_commands": [
      "events.create",
      "events.get"
    ],
    "go_method": "EventsStream",
    "ts_method": "eventsStream"
  },
  {
    "command_id": "inbox.ack",
    "cli_path": "inbox ack",
    "group": "inbox",
    "method": "POST",
    "path": "/inbox/ack",
    "operation_id": "ackInboxItem",
    "summary": "Acknowledge an inbox item",
    "why": "Suppress already-acted-on derived inbox signals.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ event }` representing acknowledgment.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id"
    ],
    "concepts": [
      "inbox",
      "events"
    ],
    "stability": "stable",
    "agent_notes": "Idempotent at semantic level; repeated acks should not duplicate active inbox items.",
    "examples": [
      {
        "title": "Ack inbox item",
        "command": "oar inbox ack --thread-id thread_123 --inbox-item-id inbox:item-1 --json"
      }
    ],
    "adjacent_commands": [
      "inbox.list",
      "inbox.stream"
    ],
    "go_method": "InboxAck",
    "ts_method": "inboxAck"
  },
  {
    "command_id": "inbox.list",
    "cli_path": "inbox list",
    "group": "inbox",
    "method": "GET",
    "path": "/inbox",
    "operation_id": "listInbox",
    "summary": "List derived inbox items",
    "why": "Surface derived actionable risk and decision signals.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ items, generated_at }`.",
    "concepts": [
      "inbox",
      "derived-views"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "List inbox",
        "command": "oar inbox list --json"
      }
    ],
    "adjacent_commands": [
      "inbox.ack",
      "inbox.stream"
    ],
    "go_method": "InboxList",
    "ts_method": "inboxList"
  },
  {
    "command_id": "inbox.stream",
    "cli_path": "inbox stream",
    "group": "inbox",
    "method": "GET",
    "path": "/inbox/stream",
    "operation_id": "streamInbox",
    "summary": "Stream derived inbox items via SSE",
    "why": "Follow live derived inbox updates without repeated polling.",
    "input_mode": "none",
    "streaming": {
      "event_type": "inbox_item",
      "mode": "sse",
      "resumable": true
    },
    "output_envelope": "SSE stream where each event carries `{ item }` derived inbox metadata.",
    "error_codes": [
      "internal_error",
      "cli_outdated"
    ],
    "concepts": [
      "inbox",
      "derived-views",
      "streaming"
    ],
    "stability": "beta",
    "agent_notes": "Supports `Last-Event-ID` header or `last_event_id` query for resumable reads.",
    "examples": [
      {
        "title": "Stream inbox updates",
        "command": "oar inbox stream --json"
      },
      {
        "title": "Resume inbox stream",
        "command": "oar inbox stream --last-event-id \u003cid\u003e --json"
      }
    ],
    "adjacent_commands": [
      "inbox.ack",
      "inbox.list"
    ],
    "go_method": "InboxStream",
    "ts_method": "inboxStream"
  },
  {
    "command_id": "meta.commands.get",
    "cli_path": "meta commands get",
    "group": "meta",
    "method": "GET",
    "path": "/meta/commands/{command_id}",
    "operation_id": "getMetaCommand",
    "summary": "Get generated metadata for a command id",
    "why": "Resolve a stable command id to full generated metadata and guidance.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ command }` metadata for the requested command id.",
    "error_codes": [
      "not_found",
      "meta_unavailable",
      "cli_outdated"
    ],
    "concepts": [
      "meta",
      "introspection"
    ],
    "stability": "beta",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Read command metadata",
        "command": "oar meta commands get --command-id threads.list --json"
      }
    ],
    "path_params": [
      "command_id"
    ],
    "adjacent_commands": [
      "meta.commands.list",
      "meta.concepts.get",
      "meta.concepts.list",
      "meta.handshake",
      "meta.health",
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
    "summary": "List generated command metadata",
    "why": "Load generated command metadata used for help, docs, and agent introspection.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns generated command registry metadata from the canonical contract.",
    "error_codes": [
      "meta_unavailable",
      "cli_outdated"
    ],
    "concepts": [
      "meta",
      "introspection"
    ],
    "stability": "beta",
    "agent_notes": "Safe and idempotent. Response shape matches committed generated artifacts.",
    "examples": [
      {
        "title": "List command metadata",
        "command": "oar meta commands list --json"
      }
    ],
    "adjacent_commands": [
      "meta.commands.get",
      "meta.concepts.get",
      "meta.concepts.list",
      "meta.handshake",
      "meta.health",
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
    "operation_id": "getMetaConcept",
    "summary": "Get generated metadata for one concept",
    "why": "Resolve one concept tag to the commands that implement that concept.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ concept }` including matched command ids and command metadata.",
    "error_codes": [
      "not_found",
      "meta_unavailable",
      "cli_outdated"
    ],
    "concepts": [
      "meta",
      "concepts"
    ],
    "stability": "beta",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Read one concept",
        "command": "oar meta concepts get --concept-name compatibility --json"
      }
    ],
    "path_params": [
      "concept_name"
    ],
    "adjacent_commands": [
      "meta.commands.get",
      "meta.commands.list",
      "meta.concepts.list",
      "meta.handshake",
      "meta.health",
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
    "summary": "List generated concept metadata",
    "why": "Discover conceptual groupings of commands generated from contract metadata.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ concepts }` summary metadata for all known concepts.",
    "error_codes": [
      "meta_unavailable",
      "cli_outdated"
    ],
    "concepts": [
      "meta",
      "concepts"
    ],
    "stability": "beta",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "List concepts",
        "command": "oar meta concepts list --json"
      }
    ],
    "adjacent_commands": [
      "meta.commands.get",
      "meta.commands.list",
      "meta.concepts.get",
      "meta.handshake",
      "meta.health",
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
    "summary": "Get compatibility handshake metadata",
    "why": "Discover compatibility, upgrade, and instance identity metadata before command execution.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns compatibility fields including minimum supported CLI version.",
    "concepts": [
      "compatibility",
      "handshake"
    ],
    "stability": "beta",
    "agent_notes": "Safe and idempotent. Use this endpoint to proactively gate incompatible CLI versions.",
    "examples": [
      {
        "title": "Read handshake metadata",
        "command": "oar meta handshake --json"
      }
    ],
    "adjacent_commands": [
      "meta.commands.get",
      "meta.commands.list",
      "meta.concepts.get",
      "meta.concepts.list",
      "meta.health",
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
    "summary": "Health check",
    "why": "Probe whether core storage is available before issuing stateful commands.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ ok: true }` when the service and storage are healthy.",
    "error_codes": [
      "storage_unavailable"
    ],
    "concepts": [
      "health",
      "readiness"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent; retry with backoff on transport failures.",
    "examples": [
      {
        "title": "Health check",
        "command": "oar meta health --json"
      }
    ],
    "adjacent_commands": [
      "meta.commands.get",
      "meta.commands.list",
      "meta.concepts.get",
      "meta.concepts.list",
      "meta.handshake",
      "meta.version"
    ],
    "go_method": "MetaHealth",
    "ts_method": "metaHealth"
  },
  {
    "command_id": "meta.version",
    "cli_path": "meta version",
    "group": "meta",
    "method": "GET",
    "path": "/version",
    "operation_id": "getVersion",
    "summary": "Get schema contract version",
    "why": "Verify compatibility between core and generated clients before performing writes.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ schema_version }` only.",
    "concepts": [
      "compatibility",
      "schema"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Read version",
        "command": "oar meta version --json"
      }
    ],
    "adjacent_commands": [
      "meta.commands.get",
      "meta.commands.list",
      "meta.concepts.get",
      "meta.concepts.list",
      "meta.handshake",
      "meta.health"
    ],
    "go_method": "MetaVersion",
    "ts_method": "metaVersion"
  },
  {
    "command_id": "packets.receipts.create",
    "cli_path": "packets receipts create",
    "group": "packets",
    "method": "POST",
    "path": "/receipts",
    "operation_id": "createReceipt",
    "summary": "Create receipt packet artifact",
    "why": "Record execution output and verification evidence for a work order.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ artifact, event }`.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id"
    ],
    "concepts": [
      "packets",
      "receipts"
    ],
    "stability": "stable",
    "agent_notes": "Include evidence refs that satisfy packet conventions.",
    "examples": [
      {
        "title": "Create receipt",
        "command": "oar packets receipts create --from-file receipt.json --json"
      }
    ],
    "adjacent_commands": [
      "packets.reviews.create",
      "packets.work-orders.create"
    ],
    "go_method": "PacketsReceiptsCreate",
    "ts_method": "packetsReceiptsCreate"
  },
  {
    "command_id": "packets.reviews.create",
    "cli_path": "packets reviews create",
    "group": "packets",
    "method": "POST",
    "path": "/reviews",
    "operation_id": "createReview",
    "summary": "Create review packet artifact",
    "why": "Record acceptance/revision decisions over a receipt.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ artifact, event }`.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id"
    ],
    "concepts": [
      "packets",
      "reviews"
    ],
    "stability": "stable",
    "agent_notes": "Include refs to both receipt and work order artifacts.",
    "examples": [
      {
        "title": "Create review",
        "command": "oar packets reviews create --from-file review.json --json"
      }
    ],
    "adjacent_commands": [
      "packets.receipts.create",
      "packets.work-orders.create"
    ],
    "go_method": "PacketsReviewsCreate",
    "ts_method": "packetsReviewsCreate"
  },
  {
    "command_id": "packets.work-orders.create",
    "cli_path": "packets work-orders create",
    "group": "packets",
    "method": "POST",
    "path": "/work_orders",
    "operation_id": "createWorkOrder",
    "summary": "Create work-order packet artifact",
    "why": "Create structured action packets with deterministic schema enforcement.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ artifact, event }`.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id"
    ],
    "concepts": [
      "packets",
      "work-orders"
    ],
    "stability": "stable",
    "agent_notes": "Treat as non-idempotent unless artifact ids are controlled.",
    "examples": [
      {
        "title": "Create work order",
        "command": "oar packets work-orders create --from-file work-order.json --json"
      }
    ],
    "adjacent_commands": [
      "packets.receipts.create",
      "packets.reviews.create"
    ],
    "go_method": "PacketsWorkOrdersCreate",
    "ts_method": "packetsWorkOrdersCreate"
  },
  {
    "command_id": "snapshots.get",
    "cli_path": "snapshots get",
    "group": "snapshots",
    "method": "GET",
    "path": "/snapshots/{snapshot_id}",
    "operation_id": "getSnapshot",
    "summary": "Get snapshot by id",
    "why": "Resolve arbitrary snapshot references encountered in event refs.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ snapshot }`.",
    "error_codes": [
      "not_found"
    ],
    "concepts": [
      "snapshots"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Get snapshot",
        "command": "oar snapshots get --snapshot-id snapshot_123 --json"
      }
    ],
    "path_params": [
      "snapshot_id"
    ],
    "go_method": "SnapshotsGet",
    "ts_method": "snapshotsGet"
  },
  {
    "command_id": "threads.create",
    "cli_path": "threads create",
    "group": "threads",
    "method": "POST",
    "path": "/threads",
    "operation_id": "createThread",
    "summary": "Create thread snapshot",
    "why": "Open a new thread for tracking ongoing organizational work.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ thread }` including generated id and audit fields.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id"
    ],
    "concepts": [
      "threads",
      "snapshots"
    ],
    "stability": "stable",
    "agent_notes": "Non-idempotent unless caller enforces a deterministic id strategy externally.",
    "examples": [
      {
        "title": "Create thread",
        "command": "oar threads create --from-file thread.json --json"
      }
    ],
    "adjacent_commands": [
      "threads.get",
      "threads.list",
      "threads.patch",
      "threads.timeline"
    ],
    "go_method": "ThreadsCreate",
    "ts_method": "threadsCreate"
  },
  {
    "command_id": "threads.get",
    "cli_path": "threads get",
    "group": "threads",
    "method": "GET",
    "path": "/threads/{thread_id}",
    "operation_id": "getThread",
    "summary": "Get thread snapshot by id",
    "why": "Resolve authoritative thread state before patching or composing packets.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ thread }`.",
    "error_codes": [
      "not_found"
    ],
    "concepts": [
      "threads"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Read thread",
        "command": "oar threads get --thread-id thread_123 --json"
      }
    ],
    "path_params": [
      "thread_id"
    ],
    "adjacent_commands": [
      "threads.create",
      "threads.list",
      "threads.patch",
      "threads.timeline"
    ],
    "go_method": "ThreadsGet",
    "ts_method": "threadsGet"
  },
  {
    "command_id": "threads.list",
    "cli_path": "threads list",
    "group": "threads",
    "method": "GET",
    "path": "/threads",
    "operation_id": "listThreads",
    "summary": "List thread snapshots",
    "why": "Retrieve current thread state for triage and scheduling decisions.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ threads }`; query filters are additive.",
    "error_codes": [
      "invalid_request"
    ],
    "concepts": [
      "threads",
      "filtering"
    ],
    "stability": "stable",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "List active p1 threads",
        "command": "oar threads list --status active --priority p1 --json"
      }
    ],
    "adjacent_commands": [
      "threads.create",
      "threads.get",
      "threads.patch",
      "threads.timeline"
    ],
    "go_method": "ThreadsList",
    "ts_method": "threadsList"
  },
  {
    "command_id": "threads.patch",
    "cli_path": "threads patch",
    "group": "threads",
    "method": "PATCH",
    "path": "/threads/{thread_id}",
    "operation_id": "patchThread",
    "summary": "Patch thread snapshot",
    "why": "Update mutable thread fields while preserving unknown data and auditability.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ thread }` after patch merge and emitted event side effect.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id",
      "conflict",
      "not_found"
    ],
    "concepts": [
      "threads",
      "patch"
    ],
    "stability": "stable",
    "agent_notes": "Use `if_updated_at` for optimistic concurrency.",
    "examples": [
      {
        "title": "Patch thread",
        "command": "oar threads patch --thread-id thread_123 --from-file patch.json --json"
      }
    ],
    "path_params": [
      "thread_id"
    ],
    "adjacent_commands": [
      "threads.create",
      "threads.get",
      "threads.list",
      "threads.timeline"
    ],
    "go_method": "ThreadsPatch",
    "ts_method": "threadsPatch"
  },
  {
    "command_id": "threads.timeline",
    "cli_path": "threads timeline",
    "group": "threads",
    "method": "GET",
    "path": "/threads/{thread_id}/timeline",
    "operation_id": "getThreadTimeline",
    "summary": "Get thread timeline events and referenced entities",
    "why": "Retrieve narrative event history plus referenced snapshots/artifacts in one call.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ events, snapshots, artifacts }` where snapshot/artifact maps are sparse.",
    "error_codes": [
      "not_found"
    ],
    "concepts": [
      "threads",
      "events",
      "provenance"
    ],
    "stability": "stable",
    "agent_notes": "Events stay time ordered; missing refs are omitted from expansion maps.",
    "examples": [
      {
        "title": "Timeline",
        "command": "oar threads timeline --thread-id thread_123 --json"
      }
    ],
    "path_params": [
      "thread_id"
    ],
    "adjacent_commands": [
      "threads.create",
      "threads.get",
      "threads.list",
      "threads.patch"
    ],
    "go_method": "ThreadsTimeline",
    "ts_method": "threadsTimeline"
  }
] as CommandSpec[];

const commandIndex = new Map(commandRegistry.map((command) => [command.command_id, command] as const));

function renderPath(pathTemplate: string, pathParams: Record<string, string> = {}): string {
  return pathTemplate.replace(/\{([^{}]+)\}/g, (_match, name: string) => {
    const value = pathParams[name];
    if (value === undefined) {
      throw new Error(`missing path param ${name}`);
    }
    return encodeURIComponent(value);
  });
}

function withQuery(path: string, query: RequestOptions["query"]): string {
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

export class OarClient {
  private readonly baseUrl: string;
  private readonly fetchFn: typeof fetch;

  constructor(baseUrl: string, fetchFn: typeof fetch = fetch) {
    this.baseUrl = String(baseUrl || "").replace(/\/+$/, "");
    this.fetchFn = fetchFn;
  }

  async invoke(commandId: string, pathParams: Record<string, string> = {}, options: RequestOptions = {}): Promise<InvokeResult> {
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

  actorsList(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("actors.list", {}, options);
  }

  actorsRegister(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("actors.register", {}, options);
  }

  agentsMeGet(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("agents.me.get", {}, options);
  }

  agentsMeKeysRotate(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("agents.me.keys.rotate", {}, options);
  }

  agentsMePatch(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("agents.me.patch", {}, options);
  }

  agentsMeRevoke(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("agents.me.revoke", {}, options);
  }

  artifactsContentGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("artifacts.content.get", pathParams, options);
  }

  artifactsCreate(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("artifacts.create", {}, options);
  }

  artifactsGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("artifacts.get", pathParams, options);
  }

  artifactsList(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("artifacts.list", {}, options);
  }

  authAgentsRegister(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("auth.agents.register", {}, options);
  }

  authToken(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("auth.token", {}, options);
  }

  commitmentsCreate(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("commitments.create", {}, options);
  }

  commitmentsGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("commitments.get", pathParams, options);
  }

  commitmentsList(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("commitments.list", {}, options);
  }

  commitmentsPatch(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("commitments.patch", pathParams, options);
  }

  derivedRebuild(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("derived.rebuild", {}, options);
  }

  eventsCreate(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("events.create", {}, options);
  }

  eventsGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("events.get", pathParams, options);
  }

  eventsStream(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("events.stream", {}, options);
  }

  inboxAck(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("inbox.ack", {}, options);
  }

  inboxList(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("inbox.list", {}, options);
  }

  inboxStream(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("inbox.stream", {}, options);
  }

  metaCommandsGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("meta.commands.get", pathParams, options);
  }

  metaCommandsList(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("meta.commands.list", {}, options);
  }

  metaConceptsGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("meta.concepts.get", pathParams, options);
  }

  metaConceptsList(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("meta.concepts.list", {}, options);
  }

  metaHandshake(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("meta.handshake", {}, options);
  }

  metaHealth(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("meta.health", {}, options);
  }

  metaVersion(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("meta.version", {}, options);
  }

  packetsReceiptsCreate(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("packets.receipts.create", {}, options);
  }

  packetsReviewsCreate(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("packets.reviews.create", {}, options);
  }

  packetsWorkOrdersCreate(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("packets.work-orders.create", {}, options);
  }

  snapshotsGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("snapshots.get", pathParams, options);
  }

  threadsCreate(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("threads.create", {}, options);
  }

  threadsGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("threads.get", pathParams, options);
  }

  threadsList(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("threads.list", {}, options);
  }

  threadsPatch(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("threads.patch", pathParams, options);
  }

  threadsTimeline(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("threads.timeline", pathParams, options);
  }

}
