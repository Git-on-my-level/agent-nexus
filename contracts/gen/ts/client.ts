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
    "body_schema": {
      "required": [
        {
          "name": "actor",
          "type": "object"
        }
      ]
    },
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
    "body_schema": {
      "required": [
        {
          "name": "public_key",
          "type": "string"
        }
      ]
    },
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
    "body_schema": {
      "required": [
        {
          "name": "username",
          "type": "string"
        }
      ]
    },
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
    "body_schema": {
      "required": [
        {
          "name": "artifact",
          "type": "object"
        },
        {
          "name": "content",
          "type": "object|string"
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
        }
      ]
    },
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
    "body_schema": {
      "required": [
        {
          "name": "public_key",
          "type": "string"
        },
        {
          "name": "username",
          "type": "string"
        }
      ]
    },
    "adjacent_commands": [
      "auth.passkey.login.options",
      "auth.passkey.login.verify",
      "auth.passkey.register.options",
      "auth.passkey.register.verify",
      "auth.token"
    ],
    "go_method": "AuthAgentsRegister",
    "ts_method": "authAgentsRegister"
  },
  {
    "command_id": "auth.passkey.login.options",
    "cli_path": "auth passkey login options",
    "group": "auth",
    "method": "POST",
    "path": "/auth/passkey/login/options",
    "operation_id": "passkeyLoginOptions",
    "summary": "Begin passkey login ceremony",
    "why": "Create a WebAuthn assertion challenge for passkey authentication.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ session_id, options }` where `options` is a WebAuthn assertion payload.",
    "error_codes": [
      "invalid_json",
      "not_found"
    ],
    "concepts": [
      "auth",
      "passkey"
    ],
    "stability": "beta",
    "agent_notes": "Provide `username` to scope login to one principal, or omit it for discoverable login.",
    "body_schema": {
      "optional": [
        {
          "name": "username",
          "type": "string"
        }
      ]
    },
    "adjacent_commands": [
      "auth.agents.register",
      "auth.passkey.login.verify",
      "auth.passkey.register.options",
      "auth.passkey.register.verify",
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
    "summary": "Verify passkey login and issue tokens",
    "why": "Verify a WebAuthn assertion and issue a fresh token bundle.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ agent, tokens }` when passkey verification succeeds.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "invalid_token",
      "agent_revoked"
    ],
    "concepts": [
      "auth",
      "passkey"
    ],
    "stability": "beta",
    "agent_notes": "Session ids are one-time use and expire quickly.",
    "body_schema": {
      "required": [
        {
          "name": "credential",
          "type": "object"
        },
        {
          "name": "session_id",
          "type": "string"
        }
      ]
    },
    "adjacent_commands": [
      "auth.agents.register",
      "auth.passkey.login.options",
      "auth.passkey.register.options",
      "auth.passkey.register.verify",
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
    "summary": "Begin passkey registration ceremony",
    "why": "Create a WebAuthn registration challenge for a new human principal.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ session_id, options }` where `options` is a WebAuthn registration payload.",
    "error_codes": [
      "invalid_json",
      "invalid_request"
    ],
    "concepts": [
      "auth",
      "passkey"
    ],
    "stability": "beta",
    "agent_notes": "Intended for browser-based WebAuthn clients.",
    "body_schema": {
      "required": [
        {
          "name": "display_name",
          "type": "string"
        }
      ]
    },
    "adjacent_commands": [
      "auth.agents.register",
      "auth.passkey.login.options",
      "auth.passkey.login.verify",
      "auth.passkey.register.verify",
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
    "summary": "Verify passkey registration and issue tokens",
    "why": "Verify a WebAuthn attestation, create a principal, and issue the initial token bundle.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ agent, tokens }` for the newly registered passkey principal.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "invalid_token"
    ],
    "concepts": [
      "auth",
      "passkey"
    ],
    "stability": "beta",
    "agent_notes": "Session ids are one-time use and expire quickly.",
    "body_schema": {
      "required": [
        {
          "name": "credential",
          "type": "object"
        },
        {
          "name": "session_id",
          "type": "string"
        }
      ]
    },
    "adjacent_commands": [
      "auth.agents.register",
      "auth.passkey.login.options",
      "auth.passkey.login.verify",
      "auth.passkey.register.options",
      "auth.token"
    ],
    "go_method": "AuthPasskeyRegisterVerify",
    "ts_method": "authPasskeyRegisterVerify"
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
    "body_schema": {
      "required": [
        {
          "name": "grant_type",
          "type": "string"
        }
      ],
      "optional": [
        {
          "name": "agent_id",
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
      "auth.agents.register",
      "auth.passkey.login.options",
      "auth.passkey.login.verify",
      "auth.passkey.register.options",
      "auth.passkey.register.verify"
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
    "body_schema": {
      "required": [
        {
          "name": "commitment.definition_of_done",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "commitment.due_at",
          "type": "datetime"
        },
        {
          "name": "commitment.links",
          "type": "list\u003ctyped_ref\u003e"
        },
        {
          "name": "commitment.owner",
          "type": "string"
        },
        {
          "name": "commitment.provenance.sources",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "commitment.status",
          "type": "string",
          "enum_values": [
            "blocked",
            "canceled",
            "done",
            "open"
          ],
          "enum_policy": "strict"
        },
        {
          "name": "commitment.thread_id",
          "type": "string"
        },
        {
          "name": "commitment.title",
          "type": "string"
        }
      ],
      "optional": [
        {
          "name": "actor_id",
          "type": "string"
        },
        {
          "name": "commitment.provenance.by_field",
          "type": "map\u003cstring, list\u003cstring\u003e\u003e"
        },
        {
          "name": "commitment.provenance.notes",
          "type": "string"
        }
      ]
    },
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
    "body_schema": {
      "optional": [
        {
          "name": "actor_id",
          "type": "string"
        },
        {
          "name": "if_updated_at",
          "type": "datetime"
        },
        {
          "name": "patch.definition_of_done",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "patch.due_at",
          "type": "datetime"
        },
        {
          "name": "patch.links",
          "type": "list\u003ctyped_ref\u003e"
        },
        {
          "name": "patch.owner",
          "type": "string"
        },
        {
          "name": "patch.provenance.by_field",
          "type": "map\u003cstring, list\u003cstring\u003e\u003e"
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
          "name": "patch.status",
          "type": "string",
          "enum_values": [
            "blocked",
            "canceled",
            "done",
            "open"
          ],
          "enum_policy": "strict"
        },
        {
          "name": "patch.title",
          "type": "string"
        },
        {
          "name": "refs",
          "type": "list\u003cstring\u003e"
        }
      ]
    },
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
    "body_schema": {
      "optional": [
        {
          "name": "actor_id",
          "type": "string"
        }
      ]
    },
    "go_method": "DerivedRebuild",
    "ts_method": "derivedRebuild"
  },
  {
    "command_id": "docs.create",
    "cli_path": "docs create",
    "group": "docs",
    "method": "POST",
    "path": "/docs",
    "operation_id": "createDocument",
    "summary": "Create document with initial immutable revision",
    "why": "Bootstrap a first-class document identity and initial revision without manual head-pointer management.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ document, revision }` where `revision` is the new head.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id",
      "conflict"
    ],
    "concepts": [
      "docs",
      "revisions"
    ],
    "stability": "beta",
    "agent_notes": "Non-idempotent unless caller provides a deterministic document id and dedupes retries.",
    "examples": [
      {
        "title": "Create document",
        "command": "oar docs create --from-file doc-create.json --json"
      }
    ],
    "body_schema": {
      "required": [
        {
          "name": "content",
          "type": "object|string"
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
          "name": "document",
          "type": "object"
        }
      ],
      "optional": [
        {
          "name": "actor_id",
          "type": "string"
        },
        {
          "name": "refs",
          "type": "list\u003cstring\u003e"
        }
      ]
    },
    "adjacent_commands": [
      "docs.get",
      "docs.history",
      "docs.revision.get",
      "docs.update"
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
    "summary": "Get document and authoritative head revision",
    "why": "Resolve the current authoritative document head without client-side lineage traversal.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ document, revision }` where `revision` is the current head.",
    "error_codes": [
      "invalid_request",
      "not_found"
    ],
    "concepts": [
      "docs",
      "revisions"
    ],
    "stability": "beta",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Get document head",
        "command": "oar docs get --document-id product-constitution --json"
      }
    ],
    "path_params": [
      "document_id"
    ],
    "adjacent_commands": [
      "docs.create",
      "docs.history",
      "docs.revision.get",
      "docs.update"
    ],
    "go_method": "DocsGet",
    "ts_method": "docsGet"
  },
  {
    "command_id": "docs.history",
    "cli_path": "docs history",
    "group": "docs",
    "method": "GET",
    "path": "/docs/{document_id}/history",
    "operation_id": "listDocumentHistory",
    "summary": "List ordered immutable revisions for a document",
    "why": "Traverse full document lineage in canonical revision-number order.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ document_id, revisions }` ordered by ascending `revision_number`.",
    "error_codes": [
      "invalid_request",
      "not_found"
    ],
    "concepts": [
      "docs",
      "revisions",
      "lineage"
    ],
    "stability": "beta",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "List document history",
        "command": "oar docs history --document-id product-constitution --json"
      }
    ],
    "path_params": [
      "document_id"
    ],
    "adjacent_commands": [
      "docs.create",
      "docs.get",
      "docs.revision.get",
      "docs.update"
    ],
    "go_method": "DocsHistory",
    "ts_method": "docsHistory"
  },
  {
    "command_id": "docs.revision.get",
    "cli_path": "docs revision get",
    "group": "docs",
    "method": "GET",
    "path": "/docs/{document_id}/revisions/{revision_id}",
    "operation_id": "getDocumentRevision",
    "summary": "Get one immutable document revision",
    "why": "Read a specific historical revision payload without mutating document head.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ revision }` including metadata and revision content.",
    "error_codes": [
      "invalid_request",
      "not_found"
    ],
    "concepts": [
      "docs",
      "revisions"
    ],
    "stability": "beta",
    "agent_notes": "Safe and idempotent.",
    "examples": [
      {
        "title": "Get revision",
        "command": "oar docs revision get --document-id product-constitution --revision-id 019f... --json"
      }
    ],
    "path_params": [
      "document_id",
      "revision_id"
    ],
    "adjacent_commands": [
      "docs.create",
      "docs.get",
      "docs.history",
      "docs.update"
    ],
    "go_method": "DocsRevisionGet",
    "ts_method": "docsRevisionGet"
  },
  {
    "command_id": "docs.update",
    "cli_path": "docs update",
    "group": "docs",
    "method": "PATCH",
    "path": "/docs/{document_id}",
    "operation_id": "updateDocument",
    "summary": "Create a new immutable revision for an existing document",
    "why": "Append a revision and atomically advance document head with optimistic concurrency.",
    "input_mode": "json-body",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ document, revision }` for the newly-created head revision.",
    "error_codes": [
      "invalid_json",
      "invalid_request",
      "unknown_actor_id",
      "conflict",
      "not_found"
    ],
    "concepts": [
      "docs",
      "revisions",
      "concurrency"
    ],
    "stability": "beta",
    "agent_notes": "Set `if_base_revision` from `docs.get` to prevent lost updates.",
    "examples": [
      {
        "title": "Update document",
        "command": "oar docs update --document-id product-constitution --from-file doc-update.json --json"
      }
    ],
    "body_schema": {
      "required": [
        {
          "name": "content",
          "type": "object|string"
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
          "name": "refs",
          "type": "list\u003cstring\u003e"
        }
      ]
    },
    "path_params": [
      "document_id"
    ],
    "adjacent_commands": [
      "docs.create",
      "docs.get",
      "docs.history",
      "docs.revision.get"
    ],
    "go_method": "DocsUpdate",
    "ts_method": "docsUpdate"
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
    "body_schema": {
      "required": [
        {
          "name": "event.provenance.sources",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "event.refs",
          "type": "list\u003ctyped_ref\u003e"
        },
        {
          "name": "event.summary",
          "type": "string"
        },
        {
          "name": "event.type",
          "type": "string",
          "enum_values": [
            "commitment_created",
            "commitment_status_changed",
            "decision_made",
            "decision_needed",
            "exception_raised",
            "inbox_item_acknowledged",
            "message_posted",
            "receipt_added",
            "review_completed",
            "snapshot_updated",
            "work_order_claimed",
            "work_order_created"
          ],
          "enum_policy": "open"
        }
      ],
      "optional": [
        {
          "name": "actor_id",
          "type": "string"
        },
        {
          "name": "event.actor_id",
          "type": "string"
        },
        {
          "name": "event.payload",
          "type": "object"
        },
        {
          "name": "event.provenance.by_field",
          "type": "map\u003cstring, list\u003cstring\u003e\u003e"
        },
        {
          "name": "event.provenance.notes",
          "type": "string"
        },
        {
          "name": "event.thread_id",
          "type": "string"
        }
      ]
    },
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
      },
      {
        "title": "Ack inbox item by id",
        "command": "oar inbox ack inbox:decision_needed:thread_123:none:event_1 --json"
      }
    ],
    "body_schema": {
      "required": [
        {
          "name": "inbox_item_id",
          "type": "string"
        },
        {
          "name": "thread_id",
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
      "inbox.get",
      "inbox.list",
      "inbox.stream"
    ],
    "go_method": "InboxAck",
    "ts_method": "inboxAck"
  },
  {
    "command_id": "inbox.get",
    "cli_path": "inbox get",
    "group": "inbox",
    "method": "GET",
    "path": "/inbox/{inbox_item_id}",
    "operation_id": "getInboxItem",
    "summary": "Get derived inbox item detail",
    "why": "Inspect one inbox item in detail before acting on it.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ item, generated_at }` for the requested inbox item.",
    "error_codes": [
      "not_found"
    ],
    "concepts": [
      "inbox",
      "derived-views"
    ],
    "stability": "stable",
    "agent_notes": "CLI supports canonical ids, aliases, and unique prefixes.",
    "examples": [
      {
        "title": "Get inbox item by canonical id",
        "command": "oar inbox get --id inbox:decision_needed:thread_123:none:event_123 --json"
      },
      {
        "title": "Get inbox item by alias",
        "command": "oar inbox get --id ibx_abcd1234ef56 --json"
      }
    ],
    "path_params": [
      "inbox_item_id"
    ],
    "adjacent_commands": [
      "inbox.ack",
      "inbox.list",
      "inbox.stream"
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
      "inbox.get",
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
      "inbox.get",
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
    "body_schema": {
      "required": [
        {
          "name": "artifact",
          "type": "object"
        },
        {
          "name": "packet.changes_summary",
          "type": "string"
        },
        {
          "name": "packet.known_gaps",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "packet.outputs",
          "type": "list\u003ctyped_ref\u003e"
        },
        {
          "name": "packet.receipt_id",
          "type": "string"
        },
        {
          "name": "packet.thread_id",
          "type": "string"
        },
        {
          "name": "packet.verification_evidence",
          "type": "list\u003ctyped_ref\u003e"
        },
        {
          "name": "packet.work_order_id",
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
    "body_schema": {
      "required": [
        {
          "name": "artifact",
          "type": "object"
        },
        {
          "name": "packet.evidence_refs",
          "type": "list\u003ctyped_ref\u003e"
        },
        {
          "name": "packet.notes",
          "type": "string"
        },
        {
          "name": "packet.outcome",
          "type": "string",
          "enum_values": [
            "accept",
            "escalate",
            "revise"
          ],
          "enum_policy": "strict"
        },
        {
          "name": "packet.receipt_id",
          "type": "string"
        },
        {
          "name": "packet.review_id",
          "type": "string"
        },
        {
          "name": "packet.work_order_id",
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
    "body_schema": {
      "required": [
        {
          "name": "artifact",
          "type": "object"
        },
        {
          "name": "packet.acceptance_criteria",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "packet.constraints",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "packet.context_refs",
          "type": "list\u003ctyped_ref\u003e"
        },
        {
          "name": "packet.definition_of_done",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "packet.objective",
          "type": "string"
        },
        {
          "name": "packet.thread_id",
          "type": "string"
        },
        {
          "name": "packet.work_order_id",
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
    "command_id": "threads.context",
    "cli_path": "threads context",
    "group": "threads",
    "method": "GET",
    "path": "/threads/{thread_id}/context",
    "operation_id": "getThreadContext",
    "summary": "Get bundled thread context for agent callers",
    "why": "Load one thread's state, recent events, key artifacts, and open commitments in a single round-trip; CLI `oar threads context` can aggregate across threads by composing multiple calls.",
    "input_mode": "none",
    "streaming": {
      "mode": "none"
    },
    "output_envelope": "Returns `{ thread, recent_events, key_artifacts, open_commitments }`.",
    "error_codes": [
      "invalid_request",
      "not_found"
    ],
    "concepts": [
      "threads",
      "events",
      "artifacts",
      "commitments"
    ],
    "stability": "beta",
    "agent_notes": "Use include_artifact_content for prompt-ready previews; default mode keeps payloads lighter. Prefer `oar threads inspect` as the first single-thread coordination read.",
    "examples": [
      {
        "title": "Context with defaults",
        "command": "oar threads context --thread-id thread_123 --json"
      },
      {
        "title": "Context with artifact previews",
        "command": "oar threads context --thread-id thread_123 --include-artifact-content --max-events 50 --json"
      }
    ],
    "path_params": [
      "thread_id"
    ],
    "adjacent_commands": [
      "threads.create",
      "threads.get",
      "threads.list",
      "threads.patch",
      "threads.timeline"
    ],
    "go_method": "ThreadsContext",
    "ts_method": "threadsContext"
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
    "body_schema": {
      "required": [
        {
          "name": "thread.cadence",
          "type": "string"
        },
        {
          "name": "thread.current_summary",
          "type": "string"
        },
        {
          "name": "thread.key_artifacts",
          "type": "list\u003ctyped_ref\u003e"
        },
        {
          "name": "thread.next_actions",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "thread.priority",
          "type": "string",
          "enum_values": [
            "p0",
            "p1",
            "p2",
            "p3"
          ],
          "enum_policy": "strict"
        },
        {
          "name": "thread.provenance.sources",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "thread.status",
          "type": "string",
          "enum_values": [
            "active",
            "closed",
            "paused"
          ],
          "enum_policy": "strict"
        },
        {
          "name": "thread.tags",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "thread.title",
          "type": "string"
        },
        {
          "name": "thread.type",
          "type": "string",
          "enum_values": [
            "case",
            "incident",
            "initiative",
            "other",
            "process",
            "relationship"
          ],
          "enum_policy": "strict"
        }
      ],
      "optional": [
        {
          "name": "actor_id",
          "type": "string"
        },
        {
          "name": "thread.next_check_in_at",
          "type": "datetime"
        },
        {
          "name": "thread.provenance.by_field",
          "type": "map\u003cstring, list\u003cstring\u003e\u003e"
        },
        {
          "name": "thread.provenance.notes",
          "type": "string"
        }
      ]
    },
    "adjacent_commands": [
      "threads.context",
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
    "why": "Resolve a raw authoritative thread snapshot for low-level reads before patching or composing packets.",
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
    "agent_notes": "Safe and idempotent. Prefer `oar threads inspect` for operator coordination reads.",
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
      "threads.context",
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
      "threads.context",
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
    "body_schema": {
      "optional": [
        {
          "name": "actor_id",
          "type": "string"
        },
        {
          "name": "if_updated_at",
          "type": "datetime"
        },
        {
          "name": "patch.cadence",
          "type": "string"
        },
        {
          "name": "patch.current_summary",
          "type": "string"
        },
        {
          "name": "patch.key_artifacts",
          "type": "list\u003ctyped_ref\u003e"
        },
        {
          "name": "patch.next_actions",
          "type": "list\u003cstring\u003e"
        },
        {
          "name": "patch.next_check_in_at",
          "type": "datetime"
        },
        {
          "name": "patch.priority",
          "type": "string",
          "enum_values": [
            "p0",
            "p1",
            "p2",
            "p3"
          ],
          "enum_policy": "strict"
        },
        {
          "name": "patch.provenance.by_field",
          "type": "map\u003cstring, list\u003cstring\u003e\u003e"
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
          "name": "patch.status",
          "type": "string",
          "enum_values": [
            "active",
            "closed",
            "paused"
          ],
          "enum_policy": "strict"
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
          "name": "patch.type",
          "type": "string",
          "enum_values": [
            "case",
            "incident",
            "initiative",
            "other",
            "process",
            "relationship"
          ],
          "enum_policy": "strict"
        }
      ]
    },
    "path_params": [
      "thread_id"
    ],
    "adjacent_commands": [
      "threads.context",
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
      "threads.context",
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

  authPasskeyLoginOptions(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("auth.passkey.login.options", {}, options);
  }

  authPasskeyLoginVerify(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("auth.passkey.login.verify", {}, options);
  }

  authPasskeyRegisterOptions(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("auth.passkey.register.options", {}, options);
  }

  authPasskeyRegisterVerify(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("auth.passkey.register.verify", {}, options);
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

  docsCreate(options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("docs.create", {}, options);
  }

  docsGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("docs.get", pathParams, options);
  }

  docsHistory(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("docs.history", pathParams, options);
  }

  docsRevisionGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("docs.revision.get", pathParams, options);
  }

  docsUpdate(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("docs.update", pathParams, options);
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

  inboxGet(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("inbox.get", pathParams, options);
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

  threadsContext(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {
    return this.invoke("threads.context", pathParams, options);
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
