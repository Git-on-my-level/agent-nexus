export const commandRegistry = [
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
        "output_envelope": "Returns `{ actors, next_cursor? }` ordered by created time ascending. Pagination is optional and backward-compatible.",
        "error_codes": [
            "actor_registry_unavailable"
        ],
        "concepts": [
            "identity"
        ],
        "stability": "stable",
        "agent_notes": "Safe and idempotent. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.",
        "examples": [
            {
                "title": "List actors",
                "command": "oar actors list --json"
            },
            {
                "title": "Search actors by name",
                "command": "oar actors list --q \"bot\" --json"
            },
            {
                "title": "Paginated actor list",
                "command": "oar actors list --limit 50 --json"
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
            "artifacts.list",
            "artifacts.tombstone"
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
            "artifacts.list",
            "artifacts.tombstone"
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
            "artifacts.list",
            "artifacts.tombstone"
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
            "artifacts.get",
            "artifacts.tombstone"
        ],
        "go_method": "ArtifactsList",
        "ts_method": "artifactsList"
    },
    {
        "command_id": "artifacts.tombstone",
        "cli_path": "artifacts tombstone",
        "group": "artifacts",
        "method": "POST",
        "path": "/artifacts/{artifact_id}/tombstone",
        "operation_id": "tombstoneArtifact",
        "summary": "Tombstone an artifact (soft-delete)",
        "why": "Mark an artifact as inactive while preserving provenance; tombstoned artifacts are excluded from list by default.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ artifact }` with updated tombstone metadata.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "artifacts",
            "lifecycle"
        ],
        "stability": "beta",
        "agent_notes": "Idempotent; repeated tombstone calls on the same artifact are safe.",
        "examples": [
            {
                "title": "Tombstone artifact",
                "command": "oar artifacts tombstone --artifact-id artifact_123 --reason \"superseded by newer version\" --json"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "reason",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "artifact_id"
        ],
        "adjacent_commands": [
            "artifacts.content.get",
            "artifacts.create",
            "artifacts.get",
            "artifacts.list"
        ],
        "go_method": "ArtifactsTombstone",
        "ts_method": "artifactsTombstone"
    },
    {
        "command_id": "auth.agents.register",
        "cli_path": "auth agents register",
        "group": "auth",
        "method": "POST",
        "path": "/auth/agents/register",
        "operation_id": "registerAgent",
        "summary": "Register agent principal and initial key",
        "why": "Register an agent principal with a bootstrap token for the first principal or an invite token for later principals.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ agent, key, tokens }`.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "invalid_token",
            "username_taken"
        ],
        "concepts": [
            "auth",
            "identity"
        ],
        "stability": "beta",
        "agent_notes": "Bootstrap is accepted only for the first successful principal registration. Later registrations require an invite token.",
        "examples": [
            {
                "title": "Bootstrap first agent",
                "command": "oar auth agents register --username agent.one --public-key \u003cbase64-ed25519-pubkey\u003e --bootstrap-token \u003ctoken\u003e --json"
            },
            {
                "title": "Register invited agent",
                "command": "oar auth agents register --username agent.two --public-key \u003cbase64-ed25519-pubkey\u003e --invite-token \u003ctoken\u003e --json"
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
            ],
            "optional": [
                {
                    "name": "bootstrap_token",
                    "type": "string"
                },
                {
                    "name": "invite_token",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
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
        "command_id": "auth.bootstrap.status",
        "cli_path": "auth bootstrap status",
        "group": "auth",
        "method": "GET",
        "path": "/auth/bootstrap/status",
        "operation_id": "getBootstrapStatus",
        "summary": "Read bootstrap onboarding availability",
        "why": "Check whether first-principal bootstrap registration is still available for this workspace.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ bootstrap_registration_available }` without exposing token material.",
        "concepts": [
            "auth",
            "onboarding"
        ],
        "stability": "beta",
        "agent_notes": "This endpoint is intentionally non-enumerating beyond the single bootstrap availability boolean.",
        "examples": [
            {
                "title": "Read bootstrap status",
                "command": "oar auth bootstrap status --json"
            }
        ],
        "adjacent_commands": [
            "auth.agents.register",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
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
        "operation_id": "createInvite",
        "summary": "Create onboarding invite",
        "why": "Mint a single-use invite token for a future human or agent registration.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ invite, token }`. The raw token is returned only once at creation time.",
        "error_codes": [
            "auth_required",
            "invalid_json",
            "invalid_request",
            "invalid_token",
            "agent_revoked"
        ],
        "concepts": [
            "auth",
            "onboarding"
        ],
        "stability": "beta",
        "agent_notes": "Requires Bearer access token. `kind` may be `human`, `agent`, or `any`.",
        "examples": [
            {
                "title": "Create agent invite",
                "command": "oar auth invites create --kind agent --note 'ops bot' --json"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "kind",
                    "type": "string",
                    "enum_values": [
                        "agent",
                        "any",
                        "human"
                    ]
                }
            ],
            "optional": [
                {
                    "name": "expires_at",
                    "type": "datetime"
                },
                {
                    "name": "note",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "auth.agents.register",
            "auth.bootstrap.status",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
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
        "operation_id": "listInvites",
        "summary": "List onboarding invites",
        "why": "Inspect current invite state without exposing token secrets.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ invites }` ordered by create time descending.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "agent_revoked"
        ],
        "concepts": [
            "auth",
            "onboarding"
        ],
        "stability": "beta",
        "agent_notes": "Requires Bearer access token. Returned invites contain metadata only, never raw tokens.",
        "examples": [
            {
                "title": "List invites",
                "command": "oar auth invites list --json"
            }
        ],
        "adjacent_commands": [
            "auth.agents.register",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.revoke",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
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
        "operation_id": "revokeInvite",
        "summary": "Revoke onboarding invite",
        "why": "Invalidate an invite token before it is consumed.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ invite }` with updated revoke metadata.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "agent_revoked",
            "not_found"
        ],
        "concepts": [
            "auth",
            "onboarding"
        ],
        "stability": "beta",
        "agent_notes": "Requires Bearer access token.",
        "examples": [
            {
                "title": "Revoke invite",
                "command": "oar auth invites revoke --invite-id invite_123 --json"
            }
        ],
        "path_params": [
            "invite_id"
        ],
        "adjacent_commands": [
            "auth.agents.register",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify",
            "auth.token"
        ],
        "go_method": "AuthInvitesRevoke",
        "ts_method": "authInvitesRevoke"
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
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
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
            ],
            "optional": [
                {
                    "name": "bootstrap_token",
                    "type": "string"
                },
                {
                    "name": "invite_token",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "auth.agents.register",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
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
        "why": "Create a WebAuthn registration challenge for a human principal during managed bootstrap or invite acceptance.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ session_id, options }` where `options` is a WebAuthn registration payload.",
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
        "agent_notes": "Requires a bootstrap token for the first successful human registration or an invite token for later registrations.",
        "body_schema": {
            "required": [
                {
                    "name": "display_name",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "bootstrap_token",
                    "type": "string"
                },
                {
                    "name": "invite_token",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "auth.agents.register",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
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
        "why": "Verify a WebAuthn attestation for managed bootstrap or invite acceptance, create the principal, and issue the initial token bundle.",
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
        "agent_notes": "Session ids are one-time use and expire quickly. The same bootstrap or invite token used to open the registration flow must be presented again here.",
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
            ],
            "optional": [
                {
                    "name": "bootstrap_token",
                    "type": "string"
                },
                {
                    "name": "invite_token",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "auth.agents.register",
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
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
            "auth.bootstrap.status",
            "auth.invites.create",
            "auth.invites.list",
            "auth.invites.revoke",
            "auth.passkey.login.options",
            "auth.passkey.login.verify",
            "auth.passkey.register.options",
            "auth.passkey.register.verify"
        ],
        "go_method": "AuthToken",
        "ts_method": "authToken"
    },
    {
        "command_id": "boards.cards.add",
        "cli_path": "boards cards add",
        "group": "boards",
        "method": "POST",
        "path": "/boards/{board_id}/cards",
        "operation_id": "addBoardCard",
        "summary": "Add existing thread to board as a card",
        "why": "Create explicit board membership for an existing thread with canonical column placement and server-owned rank.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, card }` after membership creation and board concurrency-token advancement.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "unknown_actor_id",
            "conflict",
            "not_found"
        ],
        "concepts": [
            "boards",
            "planning",
            "ordering",
            "concurrency"
        ],
        "stability": "beta",
        "agent_notes": "Replay-safe when `request_key` is reused with the same body. The board primary thread cannot be added as a card.",
        "examples": [
            {
                "title": "Add card to backlog",
                "command": "oar boards cards add --board-id board_product_launch --from-file board-card-add.json --json"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "thread_id",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "after_thread_id",
                    "type": "string"
                },
                {
                    "name": "before_thread_id",
                    "type": "string"
                },
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
                },
                {
                    "name": "pinned_document_id",
                    "type": "string"
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
            "boards.cards.list",
            "boards.cards.move",
            "boards.cards.remove",
            "boards.cards.update",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.update",
            "boards.workspace"
        ],
        "go_method": "BoardsCardsAdd",
        "ts_method": "boardsCardsAdd"
    },
    {
        "command_id": "boards.cards.list",
        "cli_path": "boards cards list",
        "group": "boards",
        "method": "GET",
        "path": "/boards/{board_id}/cards",
        "operation_id": "listBoardCards",
        "summary": "List ordered board cards",
        "why": "Read canonical board membership, column placement, and rank ordering without hydrating the full board workspace.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board_id, cards }` ordered by canonical column sequence and per-column rank.",
        "error_codes": [
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "boards",
            "planning",
            "ordering"
        ],
        "stability": "beta",
        "agent_notes": "Safe and idempotent. Use `boards.workspace` when you also need hydrated thread, document, and summary sections.",
        "examples": [
            {
                "title": "List board cards",
                "command": "oar boards cards list --board-id board_product_launch --json"
            }
        ],
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.cards.add",
            "boards.cards.move",
            "boards.cards.remove",
            "boards.cards.update",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.update",
            "boards.workspace"
        ],
        "go_method": "BoardsCardsList",
        "ts_method": "boardsCardsList"
    },
    {
        "command_id": "boards.cards.move",
        "cli_path": "boards cards move",
        "group": "boards",
        "method": "POST",
        "path": "/boards/{board_id}/cards/{thread_id}/move",
        "operation_id": "moveBoardCard",
        "summary": "Move board card across columns or ranks",
        "why": "Request relative placement for a card while keeping rank tokens opaque and server-owned.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, card }` after the move is applied.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "unknown_actor_id",
            "conflict",
            "not_found"
        ],
        "concepts": [
            "boards",
            "planning",
            "ordering",
            "concurrency"
        ],
        "stability": "beta",
        "agent_notes": "Provide at most one of `before_thread_id` or `after_thread_id`. If neither is set, the card moves to the end of the target column.",
        "examples": [
            {
                "title": "Move card into review",
                "command": "oar boards cards move --board-id board_product_launch --thread-id thread_123 --from-file board-card-move.json --json"
            }
        ],
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
                    "name": "after_thread_id",
                    "type": "string"
                },
                {
                    "name": "before_thread_id",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "board_id",
            "thread_id"
        ],
        "adjacent_commands": [
            "boards.cards.add",
            "boards.cards.list",
            "boards.cards.remove",
            "boards.cards.update",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.update",
            "boards.workspace"
        ],
        "go_method": "BoardsCardsMove",
        "ts_method": "boardsCardsMove"
    },
    {
        "command_id": "boards.cards.remove",
        "cli_path": "boards cards remove",
        "group": "boards",
        "method": "POST",
        "path": "/boards/{board_id}/cards/{thread_id}/remove",
        "operation_id": "removeBoardCard",
        "summary": "Remove board card membership",
        "why": "Delete canonical board membership for a card without introducing a separate archived-card lifecycle in v1.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, removed_thread_id }` after membership removal and board concurrency-token advancement.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "unknown_actor_id",
            "conflict",
            "not_found"
        ],
        "concepts": [
            "boards",
            "planning",
            "concurrency"
        ],
        "stability": "beta",
        "agent_notes": "Removal deletes canonical membership. Cards are not archived separately in v1.",
        "examples": [
            {
                "title": "Remove board card",
                "command": "oar boards cards remove --board-id board_product_launch --thread-id thread_123 --from-file board-card-remove.json --json"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "if_board_updated_at",
                    "type": "datetime"
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
            "board_id",
            "thread_id"
        ],
        "adjacent_commands": [
            "boards.cards.add",
            "boards.cards.list",
            "boards.cards.move",
            "boards.cards.update",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.update",
            "boards.workspace"
        ],
        "go_method": "BoardsCardsRemove",
        "ts_method": "boardsCardsRemove"
    },
    {
        "command_id": "boards.cards.update",
        "cli_path": "boards cards update",
        "group": "boards",
        "method": "PATCH",
        "path": "/boards/{board_id}/cards/{thread_id}",
        "operation_id": "updateBoardCard",
        "summary": "Update board card metadata",
        "why": "Patch mutable board-card metadata, which in v1 is limited to the pinned document convenience link.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board, card }` after metadata update and board concurrency-token advancement.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "unknown_actor_id",
            "conflict",
            "not_found"
        ],
        "concepts": [
            "boards",
            "planning",
            "docs",
            "concurrency"
        ],
        "stability": "beta",
        "agent_notes": "Set `if_board_updated_at` from the current board read before patching card metadata.",
        "examples": [
            {
                "title": "Update pinned document",
                "command": "oar boards cards update --board-id board_product_launch --thread-id thread_123 --from-file board-card-update.json --json"
            }
        ],
        "body_schema": {
            "required": [
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
                    "name": "patch.pinned_document_id",
                    "type": "any|string"
                }
            ]
        },
        "path_params": [
            "board_id",
            "thread_id"
        ],
        "adjacent_commands": [
            "boards.cards.add",
            "boards.cards.list",
            "boards.cards.move",
            "boards.cards.remove",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.update",
            "boards.workspace"
        ],
        "go_method": "BoardsCardsUpdate",
        "ts_method": "boardsCardsUpdate"
    },
    {
        "command_id": "boards.create",
        "cli_path": "boards create",
        "group": "boards",
        "method": "POST",
        "path": "/boards",
        "operation_id": "createBoard",
        "summary": "Create board",
        "why": "Create a first-class coordination board with a canonical primary thread and optional primary document.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board }` with server-owned identity and concurrency metadata.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "unknown_actor_id",
            "conflict"
        ],
        "concepts": [
            "boards",
            "planning",
            "concurrency"
        ],
        "stability": "beta",
        "agent_notes": "Replay-safe when `request_key` is reused with the same body. The primary thread is required and is never created as a card implicitly.",
        "examples": [
            {
                "title": "Create board",
                "command": "oar boards create --from-file board-create.json --json"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "board.primary_thread_id",
                    "type": "string"
                },
                {
                    "name": "board.title",
                    "type": "string"
                }
            ],
            "optional": [
                {
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "board.column_schema",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "board.id",
                    "type": "string"
                },
                {
                    "name": "board.labels",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "board.owners",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "board.pinned_refs",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "board.primary_document_id",
                    "type": "string"
                },
                {
                    "name": "board.status",
                    "type": "string",
                    "enum_values": [
                        "active",
                        "closed",
                        "paused"
                    ]
                },
                {
                    "name": "request_key",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "boards.cards.add",
            "boards.cards.list",
            "boards.cards.move",
            "boards.cards.remove",
            "boards.cards.update",
            "boards.get",
            "boards.list",
            "boards.update",
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
        "summary": "Get board metadata",
        "why": "Resolve one board's canonical metadata and concurrency token without hydrating the full workspace projection.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board }`.",
        "error_codes": [
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "boards",
            "planning"
        ],
        "stability": "beta",
        "agent_notes": "Safe and idempotent.",
        "examples": [
            {
                "title": "Get board",
                "command": "oar boards get --board-id board_product_launch --json"
            }
        ],
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.cards.add",
            "boards.cards.list",
            "boards.cards.move",
            "boards.cards.remove",
            "boards.cards.update",
            "boards.create",
            "boards.list",
            "boards.update",
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
        "summary": "List boards with derived summary data",
        "why": "Discover durable coordination boards with enough summary data for list pages and CLI triage without per-board fan-out.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ boards, next_cursor? }`, where each item includes canonical board metadata plus a derived summary. Pagination is optional and backward-compatible.",
        "error_codes": [
            "invalid_request"
        ],
        "concepts": [
            "boards",
            "planning",
            "summaries"
        ],
        "stability": "beta",
        "agent_notes": "Safe and idempotent. Use repeatable `label` and `owner` filters to narrow the list server-side. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.",
        "examples": [
            {
                "title": "List boards",
                "command": "oar boards list --json"
            },
            {
                "title": "List active boards for an owner",
                "command": "oar boards list --status active --owner actor_ceo --json"
            },
            {
                "title": "Search boards by label",
                "command": "oar boards list --q \"launch\" --json"
            },
            {
                "title": "Paginated board list",
                "command": "oar boards list --limit 30 --json"
            }
        ],
        "adjacent_commands": [
            "boards.cards.add",
            "boards.cards.list",
            "boards.cards.move",
            "boards.cards.remove",
            "boards.cards.update",
            "boards.create",
            "boards.get",
            "boards.update",
            "boards.workspace"
        ],
        "go_method": "BoardsList",
        "ts_method": "boardsList"
    },
    {
        "command_id": "boards.update",
        "cli_path": "boards update",
        "group": "boards",
        "method": "PATCH",
        "path": "/boards/{board_id}",
        "operation_id": "updateBoard",
        "summary": "Update board metadata",
        "why": "Patch mutable board metadata with optimistic concurrency while preserving server-owned identity and timestamps.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board }` after the metadata patch is applied.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "unknown_actor_id",
            "conflict",
            "not_found"
        ],
        "concepts": [
            "boards",
            "planning",
            "concurrency"
        ],
        "stability": "beta",
        "agent_notes": "Set `if_updated_at` from `boards.get` or `boards.workspace` to avoid lost updates.",
        "examples": [
            {
                "title": "Update board metadata",
                "command": "oar boards update --board-id board_product_launch --from-file board-update.json --json"
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
                    "name": "actor_id",
                    "type": "string"
                },
                {
                    "name": "patch.column_schema",
                    "type": "list\u003cany\u003e"
                },
                {
                    "name": "patch.labels",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.owners",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.pinned_refs",
                    "type": "list\u003cstring\u003e"
                },
                {
                    "name": "patch.primary_document_id",
                    "type": "any|string"
                },
                {
                    "name": "patch.status",
                    "type": "string",
                    "enum_values": [
                        "active",
                        "closed",
                        "paused"
                    ]
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
            "boards.cards.add",
            "boards.cards.list",
            "boards.cards.move",
            "boards.cards.remove",
            "boards.cards.update",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.workspace"
        ],
        "go_method": "BoardsUpdate",
        "ts_method": "boardsUpdate"
    },
    {
        "command_id": "boards.workspace",
        "cli_path": "boards workspace",
        "group": "boards",
        "method": "GET",
        "path": "/boards/{board_id}/workspace",
        "operation_id": "getBoardWorkspace",
        "summary": "Get canonical board workspace projection",
        "why": "Load one board's canonical board, primary thread, ordered cards, and aggregated docs/commitments/inbox sections in a single round-trip.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ board_id, board, primary_thread, primary_document, cards, documents, commitments, inbox, board_summary, section_kinds, generated_at }`.",
        "error_codes": [
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "boards",
            "planning",
            "threads",
            "docs",
            "commitments",
            "inbox"
        ],
        "stability": "beta",
        "agent_notes": "Prefer this as the canonical board read path for CLI and web. Cards are already hydrated with backing thread and derived summary data.",
        "examples": [
            {
                "title": "Board workspace",
                "command": "oar boards workspace --board-id board_product_launch --json"
            }
        ],
        "path_params": [
            "board_id"
        ],
        "adjacent_commands": [
            "boards.cards.add",
            "boards.cards.list",
            "boards.cards.move",
            "boards.cards.remove",
            "boards.cards.update",
            "boards.create",
            "boards.get",
            "boards.list",
            "boards.update"
        ],
        "go_method": "BoardsWorkspace",
        "ts_method": "boardsWorkspace"
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
            "unknown_actor_id",
            "conflict"
        ],
        "concepts": [
            "commitments"
        ],
        "stability": "stable",
        "agent_notes": "Replay-safe when `request_key` is reused with the same body; otherwise each create issues a new commitment id.",
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
                },
                {
                    "name": "request_key",
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
        "agent_notes": "Replay-safe when `request_key` is reused with the same body; core can issue the canonical document id when one is omitted.",
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
                },
                {
                    "name": "request_key",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "docs.get",
            "docs.history",
            "docs.list",
            "docs.revision.get",
            "docs.tombstone",
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
            "docs.list",
            "docs.revision.get",
            "docs.tombstone",
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
            "docs.list",
            "docs.revision.get",
            "docs.tombstone",
            "docs.update"
        ],
        "go_method": "DocsHistory",
        "ts_method": "docsHistory"
    },
    {
        "command_id": "docs.list",
        "cli_path": "docs list",
        "group": "docs",
        "method": "GET",
        "path": "/docs",
        "operation_id": "listDocuments",
        "summary": "List documents and their current head metadata",
        "why": "Discover available documents without resolving each head individually, optionally scoped to a single thread.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ documents, next_cursor? }` ordered by `updated_at` descending. Pagination is optional and backward-compatible.",
        "error_codes": [
            "invalid_request"
        ],
        "concepts": [
            "docs",
            "revisions"
        ],
        "stability": "beta",
        "agent_notes": "Safe and idempotent. Use `thread_id` to focus on one thread's docs and `include_tombstoned=true` when auditing superseded documents. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.",
        "examples": [
            {
                "title": "List documents",
                "command": "oar docs list --json"
            },
            {
                "title": "Search documents by title",
                "command": "oar docs list --q \"constitution\" --json"
            },
            {
                "title": "Paginated document list",
                "command": "oar docs list --limit 50 --json"
            }
        ],
        "adjacent_commands": [
            "docs.create",
            "docs.get",
            "docs.history",
            "docs.revision.get",
            "docs.tombstone",
            "docs.update"
        ],
        "go_method": "DocsList",
        "ts_method": "docsList"
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
            "docs.list",
            "docs.tombstone",
            "docs.update"
        ],
        "go_method": "DocsRevisionGet",
        "ts_method": "docsRevisionGet"
    },
    {
        "command_id": "docs.tombstone",
        "cli_path": "docs tombstone",
        "group": "docs",
        "method": "POST",
        "path": "/docs/{document_id}/tombstone",
        "operation_id": "tombstoneDocument",
        "summary": "Tombstone a document (soft-delete)",
        "why": "Mark a document as inactive while preserving revision history and provenance.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ document, revision }` with updated tombstone metadata.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "docs",
            "lifecycle"
        ],
        "stability": "beta",
        "agent_notes": "Idempotent; repeated tombstone calls on the same document are safe.",
        "examples": [
            {
                "title": "Tombstone document",
                "command": "oar docs tombstone --document-id product-constitution --reason \"replaced by v2\" --json"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "actor_id",
                    "type": "string"
                }
            ],
            "optional": [
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
            "docs.create",
            "docs.get",
            "docs.history",
            "docs.list",
            "docs.revision.get",
            "docs.update"
        ],
        "go_method": "DocsTombstone",
        "ts_method": "docsTombstone"
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
            "docs.list",
            "docs.revision.get",
            "docs.tombstone"
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
        "agent_notes": "Replay-safe when `request_key` is reused with the same body.",
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
                        "board_card_added",
                        "board_card_moved",
                        "board_card_removed",
                        "board_card_updated",
                        "board_created",
                        "board_updated",
                        "commitment_created",
                        "commitment_status_changed",
                        "decision_made",
                        "decision_needed",
                        "document_created",
                        "document_tombstoned",
                        "document_updated",
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
                },
                {
                    "name": "request_key",
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
        "output_envelope": "Returns `{ schema_version, command_registry_digest }` for frontend/core compatibility checks.",
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
        "agent_notes": "Replay-safe when `request_key` is reused with the same body. Include evidence refs that satisfy packet conventions.",
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
                },
                {
                    "name": "request_key",
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
                },
                {
                    "name": "request_key",
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
        "agent_notes": "Replay-safe when `request_key` is reused with the same body; packet id fields may be omitted and core will issue the canonical artifact id.",
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
                },
                {
                    "name": "request_key",
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
        "why": "Load one thread's state, recent events, key artifacts, open commitments, and linked documents in a single round-trip; CLI `oar threads context` can aggregate across threads by composing multiple calls.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ thread, recent_events, key_artifacts, open_commitments, documents }`.",
        "error_codes": [
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "threads",
            "events",
            "artifacts",
            "commitments",
            "docs"
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
            "threads.timeline",
            "threads.workspace"
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
            "unknown_actor_id",
            "conflict"
        ],
        "concepts": [
            "threads",
            "snapshots"
        ],
        "stability": "stable",
        "agent_notes": "Replay-safe when `request_key` is reused with the same body; otherwise core issues a new canonical thread id.",
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
                    "name": "request_key",
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
            "threads.timeline",
            "threads.workspace"
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
            "threads.timeline",
            "threads.workspace"
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
        "output_envelope": "Returns `{ threads, next_cursor? }`; query filters are additive. Pagination is optional and backward-compatible.",
        "error_codes": [
            "invalid_request"
        ],
        "concepts": [
            "threads",
            "filtering"
        ],
        "stability": "stable",
        "agent_notes": "Safe and idempotent. Optional pagination with `q` for search, `limit` for page size, and `cursor` for continuation.",
        "examples": [
            {
                "title": "List active p1 threads",
                "command": "oar threads list --status active --priority p1 --json"
            },
            {
                "title": "Search threads by title",
                "command": "oar threads list --q \"launch\" --json"
            },
            {
                "title": "Paginated thread list",
                "command": "oar threads list --limit 20 --json"
            }
        ],
        "adjacent_commands": [
            "threads.context",
            "threads.create",
            "threads.get",
            "threads.patch",
            "threads.timeline",
            "threads.workspace"
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
            "threads.timeline",
            "threads.workspace"
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
            "threads.patch",
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
        "summary": "Get canonical thread workspace projection",
        "why": "Load one thread workspace projection from the server, including canonical thread context plus derived collaboration and inbox summaries, so CLI and web do not need client-side joins.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ thread_id, thread, context, collaboration, board_memberships, inbox, pending_decisions, related_threads, follow_up, section_kinds }`, with explicit section classifications.",
        "error_codes": [
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "threads",
            "events",
            "artifacts",
            "commitments",
            "docs",
            "boards",
            "inbox"
        ],
        "stability": "beta",
        "agent_notes": "Prefer this as the single-thread coordination read path. `section_kinds` distinguishes canonical versus derived sections, including additive board membership joins.",
        "examples": [
            {
                "title": "Workspace with defaults",
                "command": "oar threads workspace --thread-id thread_123 --json"
            },
            {
                "title": "Workspace with hydrated related review events",
                "command": "oar threads workspace --thread-id thread_123 --include-related-event-content --include-artifact-content --json"
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
            "threads.patch",
            "threads.timeline"
        ],
        "go_method": "ThreadsWorkspace",
        "ts_method": "threadsWorkspace"
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
export class OarClient {
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
    actorsList(options = {}) {
        return this.invoke("actors.list", {}, options);
    }
    actorsRegister(options = {}) {
        return this.invoke("actors.register", {}, options);
    }
    agentsMeGet(options = {}) {
        return this.invoke("agents.me.get", {}, options);
    }
    agentsMeKeysRotate(options = {}) {
        return this.invoke("agents.me.keys.rotate", {}, options);
    }
    agentsMePatch(options = {}) {
        return this.invoke("agents.me.patch", {}, options);
    }
    agentsMeRevoke(options = {}) {
        return this.invoke("agents.me.revoke", {}, options);
    }
    artifactsContentGet(pathParams, options = {}) {
        return this.invoke("artifacts.content.get", pathParams, options);
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
    artifactsTombstone(pathParams, options = {}) {
        return this.invoke("artifacts.tombstone", pathParams, options);
    }
    authAgentsRegister(options = {}) {
        return this.invoke("auth.agents.register", {}, options);
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
    authToken(options = {}) {
        return this.invoke("auth.token", {}, options);
    }
    boardsCardsAdd(pathParams, options = {}) {
        return this.invoke("boards.cards.add", pathParams, options);
    }
    boardsCardsList(pathParams, options = {}) {
        return this.invoke("boards.cards.list", pathParams, options);
    }
    boardsCardsMove(pathParams, options = {}) {
        return this.invoke("boards.cards.move", pathParams, options);
    }
    boardsCardsRemove(pathParams, options = {}) {
        return this.invoke("boards.cards.remove", pathParams, options);
    }
    boardsCardsUpdate(pathParams, options = {}) {
        return this.invoke("boards.cards.update", pathParams, options);
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
    boardsUpdate(pathParams, options = {}) {
        return this.invoke("boards.update", pathParams, options);
    }
    boardsWorkspace(pathParams, options = {}) {
        return this.invoke("boards.workspace", pathParams, options);
    }
    commitmentsCreate(options = {}) {
        return this.invoke("commitments.create", {}, options);
    }
    commitmentsGet(pathParams, options = {}) {
        return this.invoke("commitments.get", pathParams, options);
    }
    commitmentsList(options = {}) {
        return this.invoke("commitments.list", {}, options);
    }
    commitmentsPatch(pathParams, options = {}) {
        return this.invoke("commitments.patch", pathParams, options);
    }
    derivedRebuild(options = {}) {
        return this.invoke("derived.rebuild", {}, options);
    }
    docsCreate(options = {}) {
        return this.invoke("docs.create", {}, options);
    }
    docsGet(pathParams, options = {}) {
        return this.invoke("docs.get", pathParams, options);
    }
    docsHistory(pathParams, options = {}) {
        return this.invoke("docs.history", pathParams, options);
    }
    docsList(options = {}) {
        return this.invoke("docs.list", {}, options);
    }
    docsRevisionGet(pathParams, options = {}) {
        return this.invoke("docs.revision.get", pathParams, options);
    }
    docsTombstone(pathParams, options = {}) {
        return this.invoke("docs.tombstone", pathParams, options);
    }
    docsUpdate(pathParams, options = {}) {
        return this.invoke("docs.update", pathParams, options);
    }
    eventsCreate(options = {}) {
        return this.invoke("events.create", {}, options);
    }
    eventsGet(pathParams, options = {}) {
        return this.invoke("events.get", pathParams, options);
    }
    eventsStream(options = {}) {
        return this.invoke("events.stream", {}, options);
    }
    inboxAck(options = {}) {
        return this.invoke("inbox.ack", {}, options);
    }
    inboxGet(pathParams, options = {}) {
        return this.invoke("inbox.get", pathParams, options);
    }
    inboxList(options = {}) {
        return this.invoke("inbox.list", {}, options);
    }
    inboxStream(options = {}) {
        return this.invoke("inbox.stream", {}, options);
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
    metaVersion(options = {}) {
        return this.invoke("meta.version", {}, options);
    }
    packetsReceiptsCreate(options = {}) {
        return this.invoke("packets.receipts.create", {}, options);
    }
    packetsReviewsCreate(options = {}) {
        return this.invoke("packets.reviews.create", {}, options);
    }
    packetsWorkOrdersCreate(options = {}) {
        return this.invoke("packets.work-orders.create", {}, options);
    }
    snapshotsGet(pathParams, options = {}) {
        return this.invoke("snapshots.get", pathParams, options);
    }
    threadsContext(pathParams, options = {}) {
        return this.invoke("threads.context", pathParams, options);
    }
    threadsCreate(options = {}) {
        return this.invoke("threads.create", {}, options);
    }
    threadsGet(pathParams, options = {}) {
        return this.invoke("threads.get", pathParams, options);
    }
    threadsList(options = {}) {
        return this.invoke("threads.list", {}, options);
    }
    threadsPatch(pathParams, options = {}) {
        return this.invoke("threads.patch", pathParams, options);
    }
    threadsTimeline(pathParams, options = {}) {
        return this.invoke("threads.timeline", pathParams, options);
    }
    threadsWorkspace(pathParams, options = {}) {
        return this.invoke("threads.workspace", pathParams, options);
    }
}
