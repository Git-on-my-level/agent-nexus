export const commandRegistry = [
    {
        "command_id": "control.accounts.passkeys.register.finish",
        "cli_path": "accounts passkeys register finish",
        "group": "accounts",
        "method": "POST",
        "path": "/account/passkeys/registrations/finish",
        "operation_id": "finishControlPasskeyRegistration",
        "summary": "Finish control-plane passkey registration",
        "why": "Verify the WebAuthn attestation and issue the initial control-plane account session.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ account, session }` after successful attestation.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "session_expired",
            "credential_invalid"
        ],
        "concepts": [
            "control-auth",
            "passkeys",
            "sessions"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Registration session ids are short-lived and one-time use.",
        "examples": [
            {
                "title": "Finish account registration",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /account/passkeys/registrations/finish --body @registration-finish.json"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "credential",
                    "type": "object"
                },
                {
                    "name": "registration_session_id",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "control.accounts.passkeys.register.start",
            "control.accounts.sessions.finish",
            "control.accounts.sessions.revoke-current",
            "control.accounts.sessions.start"
        ],
        "go_method": "ControlAccountsPasskeysRegisterFinish",
        "ts_method": "controlAccountsPasskeysRegisterFinish"
    },
    {
        "command_id": "control.accounts.passkeys.register.start",
        "cli_path": "accounts passkeys register start",
        "group": "accounts",
        "method": "POST",
        "path": "/account/passkeys/registrations/start",
        "operation_id": "startControlPasskeyRegistration",
        "summary": "Start control-plane passkey registration",
        "why": "Begin managed human-account registration in the control plane before any workspace-specific grant is issued.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ registration_session_id, public_key_options, account }`.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "account_exists"
        ],
        "concepts": [
            "control-auth",
            "passkeys",
            "accounts"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Human-driven WebAuthn ceremony. Retry by starting a new registration session when the browser ceremony expires.",
        "examples": [
            {
                "title": "Start account registration",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /account/passkeys/registrations/start --body '{\"email\":\"ops@example.com\",\"display_name\":\"Ops Lead\"}'"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "display_name",
                    "type": "string"
                },
                {
                    "name": "email",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "control.accounts.passkeys.register.finish",
            "control.accounts.sessions.finish",
            "control.accounts.sessions.revoke-current",
            "control.accounts.sessions.start"
        ],
        "go_method": "ControlAccountsPasskeysRegisterStart",
        "ts_method": "controlAccountsPasskeysRegisterStart"
    },
    {
        "command_id": "control.accounts.sessions.finish",
        "cli_path": "accounts sessions finish",
        "group": "accounts",
        "method": "POST",
        "path": "/account/sessions/finish",
        "operation_id": "finishControlAccountSession",
        "summary": "Finish control-plane account session sign-in",
        "why": "Verify the WebAuthn assertion and issue a control-plane session for later organization and workspace actions.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ account, session }`.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "session_expired",
            "credential_invalid"
        ],
        "concepts": [
            "control-auth",
            "sessions",
            "passkeys"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Short-lived one-time session ids. Do not retry with the same signed assertion after a transport failure unless the caller is sure it was not accepted.",
        "examples": [
            {
                "title": "Finish control-plane sign-in",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /account/sessions/finish --body @session-finish.json"
            }
        ],
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
            "control.accounts.passkeys.register.finish",
            "control.accounts.passkeys.register.start",
            "control.accounts.sessions.revoke-current",
            "control.accounts.sessions.start"
        ],
        "go_method": "ControlAccountsSessionsFinish",
        "ts_method": "controlAccountsSessionsFinish"
    },
    {
        "command_id": "control.accounts.sessions.revoke-current",
        "cli_path": "accounts sessions revoke-current",
        "group": "accounts",
        "method": "DELETE",
        "path": "/account/sessions/current",
        "operation_id": "revokeCurrentControlAccountSession",
        "summary": "Revoke current control-plane account session",
        "why": "Allow a human to explicitly end their current control-plane browser or API session.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ revoked: true }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "control-auth",
            "sessions"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Idempotent from the caller perspective. Safe to call during logout cleanup.",
        "examples": [
            {
                "title": "Revoke current session",
                "command": "oar api call --base-url https://control.oar.example --method DELETE --path /account/sessions/current --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "adjacent_commands": [
            "control.accounts.passkeys.register.finish",
            "control.accounts.passkeys.register.start",
            "control.accounts.sessions.finish",
            "control.accounts.sessions.start"
        ],
        "go_method": "ControlAccountsSessionsRevokeCurrent",
        "ts_method": "controlAccountsSessionsRevokeCurrent"
    },
    {
        "command_id": "control.accounts.sessions.start",
        "cli_path": "accounts sessions start",
        "group": "accounts",
        "method": "POST",
        "path": "/account/sessions/start",
        "operation_id": "startControlAccountSession",
        "summary": "Start control-plane account session sign-in",
        "why": "Create a short-lived control-plane sign-in ceremony for an existing human account.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ session_id, public_key_options, account_hint }`.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "not_found",
            "account_disabled"
        ],
        "concepts": [
            "control-auth",
            "sessions",
            "passkeys"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Use only for human account sign-in. Agents remain workspace-local and do not use this flow.",
        "examples": [
            {
                "title": "Start control-plane sign-in",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /account/sessions/start --body '{\"email\":\"ops@example.com\"}'"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "email",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "control.accounts.passkeys.register.finish",
            "control.accounts.passkeys.register.start",
            "control.accounts.sessions.finish",
            "control.accounts.sessions.revoke-current"
        ],
        "go_method": "ControlAccountsSessionsStart",
        "ts_method": "controlAccountsSessionsStart"
    },
    {
        "command_id": "control.organizations.create",
        "cli_path": "organizations create",
        "group": "organizations",
        "method": "POST",
        "path": "/organizations",
        "operation_id": "createControlOrganization",
        "summary": "Create control-plane organization",
        "why": "Create the durable top-level SaaS organization record before provisioning any isolated workspace core.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ organization, membership }` for the creator.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_json",
            "invalid_request",
            "slug_conflict"
        ],
        "concepts": [
            "organizations",
            "tenancy",
            "billing"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Not idempotent by default. Repeat only with an application-level idempotency key outside this contract.",
        "examples": [
            {
                "title": "Create organization",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /organizations --body '{\"slug\":\"acme\",\"display_name\":\"Acme\",\"plan_tier\":\"team\"}' --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "display_name",
                    "type": "string"
                },
                {
                    "name": "plan_tier",
                    "type": "string",
                    "enum_values": [
                        "enterprise",
                        "scale",
                        "starter",
                        "team"
                    ]
                },
                {
                    "name": "slug",
                    "type": "string"
                }
            ]
        },
        "adjacent_commands": [
            "control.organizations.get",
            "control.organizations.invites.create",
            "control.organizations.invites.list",
            "control.organizations.invites.revoke",
            "control.organizations.list",
            "control.organizations.memberships.list",
            "control.organizations.memberships.update",
            "control.organizations.update",
            "control.organizations.usage-summary.get"
        ],
        "go_method": "ControlOrganizationsCreate",
        "ts_method": "controlOrganizationsCreate"
    },
    {
        "command_id": "control.organizations.get",
        "cli_path": "organizations get",
        "group": "organizations",
        "method": "GET",
        "path": "/organizations/{organization_id}",
        "operation_id": "getControlOrganization",
        "summary": "Get control-plane organization",
        "why": "Read one organization's control-plane configuration, plan, and lifecycle state.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ organization }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "organizations",
            "tenancy"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Safe and idempotent.",
        "examples": [
            {
                "title": "Get organization",
                "command": "oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123 --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "organization_id"
        ],
        "adjacent_commands": [
            "control.organizations.create",
            "control.organizations.invites.create",
            "control.organizations.invites.list",
            "control.organizations.invites.revoke",
            "control.organizations.list",
            "control.organizations.memberships.list",
            "control.organizations.memberships.update",
            "control.organizations.update",
            "control.organizations.usage-summary.get"
        ],
        "go_method": "ControlOrganizationsGet",
        "ts_method": "controlOrganizationsGet"
    },
    {
        "command_id": "control.organizations.invites.create",
        "cli_path": "organizations invites create",
        "group": "organizations",
        "method": "POST",
        "path": "/organizations/{organization_id}/invites",
        "operation_id": "createControlOrganizationInvite",
        "summary": "Create organization invite",
        "why": "Invite a control-plane human account into an organization before that human launches any isolated workspace.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ invite, invite_url }`. The invite URL is returned only at creation time.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_json",
            "invalid_request",
            "not_found",
            "invite_conflict"
        ],
        "concepts": [
            "organizations",
            "invites",
            "access"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Treat invite URLs as secrets. Reissuing an invite should create a new record instead of mutating the old secret.",
        "examples": [
            {
                "title": "Invite organization admin",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/invites --body '{\"email\":\"finance@example.com\",\"role\":\"admin\"}' --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "email",
                    "type": "string"
                },
                {
                    "name": "role",
                    "type": "string",
                    "enum_values": [
                        "admin",
                        "member",
                        "viewer"
                    ]
                }
            ]
        },
        "path_params": [
            "organization_id"
        ],
        "adjacent_commands": [
            "control.organizations.create",
            "control.organizations.get",
            "control.organizations.invites.list",
            "control.organizations.invites.revoke",
            "control.organizations.list",
            "control.organizations.memberships.list",
            "control.organizations.memberships.update",
            "control.organizations.update",
            "control.organizations.usage-summary.get"
        ],
        "go_method": "ControlOrganizationsInvitesCreate",
        "ts_method": "controlOrganizationsInvitesCreate"
    },
    {
        "command_id": "control.organizations.invites.list",
        "cli_path": "organizations invites list",
        "group": "organizations",
        "method": "GET",
        "path": "/organizations/{organization_id}/invites",
        "operation_id": "listControlOrganizationInvites",
        "summary": "List organization invites",
        "why": "Inspect pending or completed control-plane organization invites without exposing secrets beyond the invite link created at issuance time.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ invites }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "organizations",
            "invites",
            "access"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Safe and idempotent.",
        "examples": [
            {
                "title": "List org invites",
                "command": "oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/invites --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "organization_id"
        ],
        "adjacent_commands": [
            "control.organizations.create",
            "control.organizations.get",
            "control.organizations.invites.create",
            "control.organizations.invites.revoke",
            "control.organizations.list",
            "control.organizations.memberships.list",
            "control.organizations.memberships.update",
            "control.organizations.update",
            "control.organizations.usage-summary.get"
        ],
        "go_method": "ControlOrganizationsInvitesList",
        "ts_method": "controlOrganizationsInvitesList"
    },
    {
        "command_id": "control.organizations.invites.revoke",
        "cli_path": "organizations invites revoke",
        "group": "organizations",
        "method": "POST",
        "path": "/organizations/{organization_id}/invites/{invite_id}/revoke",
        "operation_id": "revokeControlOrganizationInvite",
        "summary": "Revoke organization invite",
        "why": "Invalidate a pending control-plane organization invite before it is accepted.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ invite }` with updated lifecycle fields.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "organizations",
            "invites",
            "access"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Idempotent if the invite is already revoked.",
        "examples": [
            {
                "title": "Revoke org invite",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/invites/inv_123/revoke --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "organization_id",
            "invite_id"
        ],
        "adjacent_commands": [
            "control.organizations.create",
            "control.organizations.get",
            "control.organizations.invites.create",
            "control.organizations.invites.list",
            "control.organizations.list",
            "control.organizations.memberships.list",
            "control.organizations.memberships.update",
            "control.organizations.update",
            "control.organizations.usage-summary.get"
        ],
        "go_method": "ControlOrganizationsInvitesRevoke",
        "ts_method": "controlOrganizationsInvitesRevoke"
    },
    {
        "command_id": "control.organizations.list",
        "cli_path": "organizations list",
        "group": "organizations",
        "method": "GET",
        "path": "/organizations",
        "operation_id": "listControlOrganizations",
        "summary": "List control-plane organizations",
        "why": "Load the organization registry visible to the current human account.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ organizations }` ordered by create time ascending.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "organizations",
            "tenancy"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Safe and idempotent.",
        "examples": [
            {
                "title": "List organizations",
                "command": "oar api call --base-url https://control.oar.example --method GET --path /organizations --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "adjacent_commands": [
            "control.organizations.create",
            "control.organizations.get",
            "control.organizations.invites.create",
            "control.organizations.invites.list",
            "control.organizations.invites.revoke",
            "control.organizations.memberships.list",
            "control.organizations.memberships.update",
            "control.organizations.update",
            "control.organizations.usage-summary.get"
        ],
        "go_method": "ControlOrganizationsList",
        "ts_method": "controlOrganizationsList"
    },
    {
        "command_id": "control.organizations.memberships.list",
        "cli_path": "organizations memberships list",
        "group": "organizations",
        "method": "GET",
        "path": "/organizations/{organization_id}/memberships",
        "operation_id": "listControlOrganizationMemberships",
        "summary": "List organization memberships",
        "why": "Inspect which control-plane human accounts can access an organization and at what role.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ memberships }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "organizations",
            "memberships",
            "access"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Safe and idempotent.",
        "examples": [
            {
                "title": "List memberships",
                "command": "oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/memberships --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "organization_id"
        ],
        "adjacent_commands": [
            "control.organizations.create",
            "control.organizations.get",
            "control.organizations.invites.create",
            "control.organizations.invites.list",
            "control.organizations.invites.revoke",
            "control.organizations.list",
            "control.organizations.memberships.update",
            "control.organizations.update",
            "control.organizations.usage-summary.get"
        ],
        "go_method": "ControlOrganizationsMembershipsList",
        "ts_method": "controlOrganizationsMembershipsList"
    },
    {
        "command_id": "control.organizations.memberships.update",
        "cli_path": "organizations memberships update",
        "group": "organizations",
        "method": "PATCH",
        "path": "/organizations/{organization_id}/memberships/{membership_id}",
        "operation_id": "updateControlOrganizationMembership",
        "summary": "Update organization membership",
        "why": "Change an existing member's role or disable their organization grant without touching workspace-local principals.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ membership }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_json",
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "organizations",
            "memberships",
            "access"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Patch semantics. Workspace access after update still depends on launch/session exchange grants.",
        "examples": [
            {
                "title": "Promote organization member",
                "command": "oar api call --base-url https://control.oar.example --method PATCH --path /organizations/org_123/memberships/mem_123 --body '{\"role\":\"owner\"}' --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "body_schema": {
            "optional": [
                {
                    "name": "role",
                    "type": "string",
                    "enum_values": [
                        "admin",
                        "member",
                        "owner",
                        "viewer"
                    ]
                },
                {
                    "name": "status",
                    "type": "string",
                    "enum_values": [
                        "active",
                        "disabled"
                    ]
                }
            ]
        },
        "path_params": [
            "organization_id",
            "membership_id"
        ],
        "adjacent_commands": [
            "control.organizations.create",
            "control.organizations.get",
            "control.organizations.invites.create",
            "control.organizations.invites.list",
            "control.organizations.invites.revoke",
            "control.organizations.list",
            "control.organizations.memberships.list",
            "control.organizations.update",
            "control.organizations.usage-summary.get"
        ],
        "go_method": "ControlOrganizationsMembershipsUpdate",
        "ts_method": "controlOrganizationsMembershipsUpdate"
    },
    {
        "command_id": "control.organizations.update",
        "cli_path": "organizations update",
        "group": "organizations",
        "method": "PATCH",
        "path": "/organizations/{organization_id}",
        "operation_id": "updateControlOrganization",
        "summary": "Update control-plane organization",
        "why": "Adjust organization display, plan, or lifecycle flags without changing workspace-local data.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ organization }` with updated control-plane fields.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_json",
            "invalid_request",
            "not_found"
        ],
        "concepts": [
            "organizations",
            "billing",
            "lifecycle"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Patch semantics. Omitted fields are left unchanged.",
        "examples": [
            {
                "title": "Update organization plan",
                "command": "oar api call --base-url https://control.oar.example --method PATCH --path /organizations/org_123 --body '{\"plan_tier\":\"scale\"}' --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "body_schema": {
            "optional": [
                {
                    "name": "display_name",
                    "type": "string"
                },
                {
                    "name": "plan_tier",
                    "type": "string",
                    "enum_values": [
                        "enterprise",
                        "scale",
                        "starter",
                        "team"
                    ]
                },
                {
                    "name": "status",
                    "type": "string",
                    "enum_values": [
                        "active",
                        "suspended"
                    ]
                }
            ]
        },
        "path_params": [
            "organization_id"
        ],
        "adjacent_commands": [
            "control.organizations.create",
            "control.organizations.get",
            "control.organizations.invites.create",
            "control.organizations.invites.list",
            "control.organizations.invites.revoke",
            "control.organizations.list",
            "control.organizations.memberships.list",
            "control.organizations.memberships.update",
            "control.organizations.usage-summary.get"
        ],
        "go_method": "ControlOrganizationsUpdate",
        "ts_method": "controlOrganizationsUpdate"
    },
    {
        "command_id": "control.organizations.usage-summary.get",
        "cli_path": "organizations usage-summary get",
        "group": "organizations",
        "method": "GET",
        "path": "/organizations/{organization_id}/usage-summary",
        "operation_id": "getControlOrganizationUsageSummary",
        "summary": "Get organization usage and plan summary",
        "why": "Expose plan and quota envelopes from the control plane without mixing them into workspace-local durable truth.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ summary }` with plan, usage, and remaining quota fields.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "usage",
            "plans",
            "quotas"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Safe and idempotent. This is a control-plane summary, not a billing invoice.",
        "examples": [
            {
                "title": "Get usage summary",
                "command": "oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/usage-summary --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "organization_id"
        ],
        "adjacent_commands": [
            "control.organizations.create",
            "control.organizations.get",
            "control.organizations.invites.create",
            "control.organizations.invites.list",
            "control.organizations.invites.revoke",
            "control.organizations.list",
            "control.organizations.memberships.list",
            "control.organizations.memberships.update",
            "control.organizations.update"
        ],
        "go_method": "ControlOrganizationsUsageSummaryGet",
        "ts_method": "controlOrganizationsUsageSummaryGet"
    },
    {
        "command_id": "control.provisioning.jobs.get",
        "cli_path": "provisioning jobs get",
        "group": "provisioning",
        "method": "GET",
        "path": "/provisioning/jobs/{job_id}",
        "operation_id": "getControlProvisioningJob",
        "summary": "Get provisioning job status",
        "why": "Poll provisioning and lifecycle jobs that create, repair, or replace isolated workspace cores.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ job }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "provisioning",
            "lifecycle",
            "workspaces"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Safe and idempotent. Use polling or backoff; the contract does not require a watch stream yet.",
        "examples": [
            {
                "title": "Poll provisioning job",
                "command": "oar api call --base-url https://control.oar.example --method GET --path /provisioning/jobs/job_123 --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "job_id"
        ],
        "go_method": "ControlProvisioningJobsGet",
        "ts_method": "controlProvisioningJobsGet"
    },
    {
        "command_id": "control.workspaces.create",
        "cli_path": "workspaces create",
        "group": "workspaces",
        "method": "POST",
        "path": "/workspaces",
        "operation_id": "createControlWorkspace",
        "summary": "Create workspace registry entry and provisioning job",
        "why": "Allocate a new isolated workspace core under an organization and queue its provisioning lifecycle.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ workspace, provisioning_job }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_json",
            "invalid_request",
            "not_found",
            "slug_conflict",
            "quota_exceeded"
        ],
        "concepts": [
            "workspaces",
            "provisioning",
            "registry"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Creates registry state and queues background provisioning. The workspace is not ready for launch until the job succeeds.",
        "examples": [
            {
                "title": "Provision workspace",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /workspaces --body '{\"organization_id\":\"org_123\",\"slug\":\"ops\",\"display_name\":\"Ops\",\"region\":\"us-central1\",\"workspace_tier\":\"standard\"}' --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "display_name",
                    "type": "string"
                },
                {
                    "name": "organization_id",
                    "type": "string"
                },
                {
                    "name": "region",
                    "type": "string"
                },
                {
                    "name": "slug",
                    "type": "string"
                },
                {
                    "name": "workspace_tier",
                    "type": "string",
                    "enum_values": [
                        "dedicated",
                        "plus",
                        "standard"
                    ]
                }
            ]
        },
        "adjacent_commands": [
            "control.workspaces.decommission",
            "control.workspaces.get",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.list",
            "control.workspaces.replace",
            "control.workspaces.restore",
            "control.workspaces.resume",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.session-exchange.create",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesCreate",
        "ts_method": "controlWorkspacesCreate"
    },
    {
        "command_id": "control.workspaces.decommission",
        "cli_path": "workspaces decommission",
        "group": "workspaces",
        "method": "POST",
        "path": "/workspaces/{workspace_id}/decommission",
        "operation_id": "decommissionControlWorkspace",
        "summary": "Decommission workspace deployment",
        "why": "Retire a workspace deployment while preserving the control-plane registry and job history for later audit and retry decisions.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ workspace, provisioning_job }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "workspaces",
            "lifecycle",
            "routing"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Idempotent from a user perspective once the workspace is archived.",
        "examples": [
            {
                "title": "Decommission workspace",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/decommission --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "workspace_id"
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.get",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.list",
            "control.workspaces.replace",
            "control.workspaces.restore",
            "control.workspaces.resume",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.session-exchange.create",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesDecommission",
        "ts_method": "controlWorkspacesDecommission"
    },
    {
        "command_id": "control.workspaces.get",
        "cli_path": "workspaces get",
        "group": "workspaces",
        "method": "GET",
        "path": "/workspaces/{workspace_id}",
        "operation_id": "getControlWorkspace",
        "summary": "Get workspace registry entry",
        "why": "Read one workspace registry record and its current lifecycle summary.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ workspace }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "workspaces",
            "registry"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Safe and idempotent.",
        "examples": [
            {
                "title": "Read workspace",
                "command": "oar api call --base-url https://control.oar.example --method GET --path /workspaces/ws_123 --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "workspace_id"
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.decommission",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.list",
            "control.workspaces.replace",
            "control.workspaces.restore",
            "control.workspaces.resume",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.session-exchange.create",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesGet",
        "ts_method": "controlWorkspacesGet"
    },
    {
        "command_id": "control.workspaces.launch-sessions.create",
        "cli_path": "workspaces launch-sessions create",
        "group": "workspaces",
        "method": "POST",
        "path": "/workspaces/{workspace_id}/launch-sessions",
        "operation_id": "createControlWorkspaceLaunchSession",
        "summary": "Create workspace launch session",
        "why": "Broker human entry into an isolated workspace UI from the control plane without moving workspace identity into the control plane data plane.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ launch_session }` including `workspace_path` and one-time exchange token metadata.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_json",
            "invalid_request",
            "not_found",
            "workspace_not_ready"
        ],
        "concepts": [
            "workspaces",
            "launch",
            "grants"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "Launch sessions are for humans. Agents stay workspace-local and should authenticate directly against the workspace core.",
        "examples": [
            {
                "title": "Launch workspace UI",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/launch-sessions --body '{\"return_path\":\"/ws/ops/threads\"}' --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "body_schema": {
            "optional": [
                {
                    "name": "return_path",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "workspace_id"
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.decommission",
            "control.workspaces.get",
            "control.workspaces.list",
            "control.workspaces.replace",
            "control.workspaces.restore",
            "control.workspaces.resume",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.session-exchange.create",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesLaunchSessionsCreate",
        "ts_method": "controlWorkspacesLaunchSessionsCreate"
    },
    {
        "command_id": "control.workspaces.list",
        "cli_path": "workspaces list",
        "group": "workspaces",
        "method": "GET",
        "path": "/workspaces",
        "operation_id": "listControlWorkspaces",
        "summary": "List workspace registry entries",
        "why": "Read the control-plane registry of isolated workspaces without crossing the workspace data boundary.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ workspaces }`.",
        "error_codes": [
            "auth_required",
            "invalid_token"
        ],
        "concepts": [
            "workspaces",
            "registry",
            "tenancy"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Safe and idempotent.",
        "examples": [
            {
                "title": "List workspaces for an organization",
                "command": "oar api call --base-url https://control.oar.example --method GET --path '/workspaces?organization_id=org_123' --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.decommission",
            "control.workspaces.get",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.replace",
            "control.workspaces.restore",
            "control.workspaces.resume",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.session-exchange.create",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesList",
        "ts_method": "controlWorkspacesList"
    },
    {
        "command_id": "control.workspaces.replace",
        "cli_path": "workspaces replace",
        "group": "workspaces",
        "method": "POST",
        "path": "/workspaces/{workspace_id}/replace",
        "operation_id": "replaceControlWorkspace",
        "summary": "Replace workspace deployment from backup",
        "why": "Replace a workspace deployment in place by restoring a backup bundle into its registry-owned instance root.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ workspace, provisioning_job }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_json",
            "invalid_request",
            "not_found",
            "workspace_not_ready"
        ],
        "concepts": [
            "workspaces",
            "lifecycle",
            "restore"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "The backup bundle must already exist and pass validation. The job is retryable if the prior attempt failed.",
        "examples": [
            {
                "title": "Replace workspace",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/replace --body '{\"backup_dir\":\"/var/backups/ws_123-20260321T000000Z\"}' --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "backup_dir",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "workspace_id"
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.decommission",
            "control.workspaces.get",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.list",
            "control.workspaces.restore",
            "control.workspaces.resume",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.session-exchange.create",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesReplace",
        "ts_method": "controlWorkspacesReplace"
    },
    {
        "command_id": "control.workspaces.restore",
        "cli_path": "workspaces restore",
        "group": "workspaces",
        "method": "POST",
        "path": "/workspaces/{workspace_id}/restore",
        "operation_id": "restoreControlWorkspace",
        "summary": "Restore workspace deployment from backup",
        "why": "Restore a workspace deployment from a validated backup bundle using the hosted restore workflow.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ workspace, provisioning_job }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "invalid_json",
            "invalid_request",
            "not_found",
            "workspace_not_ready"
        ],
        "concepts": [
            "workspaces",
            "lifecycle",
            "restore"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "The backup bundle must already exist and pass validation. Failed restores must retain the script stderr tail.",
        "examples": [
            {
                "title": "Restore workspace",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/restore --body '{\"backup_dir\":\"/var/backups/ws_123-20260321T000000Z\"}' --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "backup_dir",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "workspace_id"
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.decommission",
            "control.workspaces.get",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.list",
            "control.workspaces.replace",
            "control.workspaces.resume",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.session-exchange.create",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesRestore",
        "ts_method": "controlWorkspacesRestore"
    },
    {
        "command_id": "control.workspaces.resume",
        "cli_path": "workspaces resume",
        "group": "workspaces",
        "method": "POST",
        "path": "/workspaces/{workspace_id}/resume",
        "operation_id": "resumeControlWorkspace",
        "summary": "Resume workspace deployment",
        "why": "Return a suspended workspace deployment to the ready state so shared routing can direct traffic to it again.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ workspace, provisioning_job }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found",
            "workspace_not_ready"
        ],
        "concepts": [
            "workspaces",
            "lifecycle",
            "routing"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Safe to retry only after checking the previous job result.",
        "examples": [
            {
                "title": "Resume workspace",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/resume --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "workspace_id"
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.decommission",
            "control.workspaces.get",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.list",
            "control.workspaces.replace",
            "control.workspaces.restore",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.session-exchange.create",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesResume",
        "ts_method": "controlWorkspacesResume"
    },
    {
        "command_id": "control.workspaces.routing-manifest.get",
        "cli_path": "workspaces routing-manifest get",
        "group": "workspaces",
        "method": "GET",
        "path": "/workspaces/{workspace_id}/routing-manifest",
        "operation_id": "getControlWorkspaceRoutingManifest",
        "summary": "Get workspace routing manifest",
        "why": "Load machine-readable routing metadata for a single isolated workspace deployment so shared apps and proxies can resolve the correct backend.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ routing_manifest }` for the requested workspace.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found"
        ],
        "concepts": [
            "workspaces",
            "registry",
            "routing"
        ],
        "stability": "beta",
        "surface": "projection",
        "agent_notes": "Safe and idempotent. The manifest is derived from registry state and is suitable for proxy consumption.",
        "examples": [
            {
                "title": "Read routing manifest",
                "command": "oar api call --base-url https://control.oar.example --method GET --path /workspaces/ws_123/routing-manifest --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "workspace_id"
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.decommission",
            "control.workspaces.get",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.list",
            "control.workspaces.replace",
            "control.workspaces.restore",
            "control.workspaces.resume",
            "control.workspaces.session-exchange.create",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesRoutingManifestGet",
        "ts_method": "controlWorkspacesRoutingManifestGet"
    },
    {
        "command_id": "control.workspaces.session-exchange.create",
        "cli_path": "workspaces session-exchange create",
        "group": "workspaces",
        "method": "POST",
        "path": "/workspaces/{workspace_id}/session-exchange",
        "operation_id": "exchangeControlWorkspaceSession",
        "summary": "Exchange launch token for workspace-scoped grant",
        "why": "Convert a control-plane launch token into a workspace-scoped session grant that the isolated workspace core can trust.",
        "input_mode": "json-body",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ workspace, grant }` for the target workspace.",
        "error_codes": [
            "invalid_json",
            "invalid_request",
            "not_found",
            "exchange_expired",
            "exchange_invalid",
            "workspace_not_ready"
        ],
        "concepts": [
            "workspaces",
            "grants",
            "launch"
        ],
        "stability": "beta",
        "surface": "utility",
        "agent_notes": "One-time token exchange. The returned grant is scoped to one workspace and must not be reused across workspaces.",
        "examples": [
            {
                "title": "Exchange launch token",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/session-exchange --body '{\"exchange_token\":\"\u003ctoken\u003e\"}'"
            }
        ],
        "body_schema": {
            "required": [
                {
                    "name": "exchange_token",
                    "type": "string"
                }
            ]
        },
        "path_params": [
            "workspace_id"
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.decommission",
            "control.workspaces.get",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.list",
            "control.workspaces.replace",
            "control.workspaces.restore",
            "control.workspaces.resume",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.suspend"
        ],
        "go_method": "ControlWorkspacesSessionExchangeCreate",
        "ts_method": "controlWorkspacesSessionExchangeCreate"
    },
    {
        "command_id": "control.workspaces.suspend",
        "cli_path": "workspaces suspend",
        "group": "workspaces",
        "method": "POST",
        "path": "/workspaces/{workspace_id}/suspend",
        "operation_id": "suspendControlWorkspace",
        "summary": "Suspend workspace deployment",
        "why": "Mark a workspace deployment as suspended and stop routing new launches to it without deleting the registry record.",
        "input_mode": "none",
        "streaming": {
            "mode": "none"
        },
        "output_envelope": "Returns `{ workspace, provisioning_job }`.",
        "error_codes": [
            "auth_required",
            "invalid_token",
            "not_found",
            "workspace_not_ready"
        ],
        "concepts": [
            "workspaces",
            "lifecycle",
            "routing"
        ],
        "stability": "beta",
        "surface": "canonical",
        "agent_notes": "Safe to retry only after checking the previous job result. Suspension is a registry-level state transition.",
        "examples": [
            {
                "title": "Suspend workspace",
                "command": "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/suspend --header 'Authorization: Bearer \u003ccontrol-session\u003e'"
            }
        ],
        "path_params": [
            "workspace_id"
        ],
        "adjacent_commands": [
            "control.workspaces.create",
            "control.workspaces.decommission",
            "control.workspaces.get",
            "control.workspaces.launch-sessions.create",
            "control.workspaces.list",
            "control.workspaces.replace",
            "control.workspaces.restore",
            "control.workspaces.resume",
            "control.workspaces.routing-manifest.get",
            "control.workspaces.session-exchange.create"
        ],
        "go_method": "ControlWorkspacesSuspend",
        "ts_method": "controlWorkspacesSuspend"
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
    controlAccountsPasskeysRegisterFinish(options = {}) {
        return this.invoke("control.accounts.passkeys.register.finish", {}, options);
    }
    controlAccountsPasskeysRegisterStart(options = {}) {
        return this.invoke("control.accounts.passkeys.register.start", {}, options);
    }
    controlAccountsSessionsFinish(options = {}) {
        return this.invoke("control.accounts.sessions.finish", {}, options);
    }
    controlAccountsSessionsRevokeCurrent(options = {}) {
        return this.invoke("control.accounts.sessions.revoke-current", {}, options);
    }
    controlAccountsSessionsStart(options = {}) {
        return this.invoke("control.accounts.sessions.start", {}, options);
    }
    controlOrganizationsCreate(options = {}) {
        return this.invoke("control.organizations.create", {}, options);
    }
    controlOrganizationsGet(pathParams, options = {}) {
        return this.invoke("control.organizations.get", pathParams, options);
    }
    controlOrganizationsInvitesCreate(pathParams, options = {}) {
        return this.invoke("control.organizations.invites.create", pathParams, options);
    }
    controlOrganizationsInvitesList(pathParams, options = {}) {
        return this.invoke("control.organizations.invites.list", pathParams, options);
    }
    controlOrganizationsInvitesRevoke(pathParams, options = {}) {
        return this.invoke("control.organizations.invites.revoke", pathParams, options);
    }
    controlOrganizationsList(options = {}) {
        return this.invoke("control.organizations.list", {}, options);
    }
    controlOrganizationsMembershipsList(pathParams, options = {}) {
        return this.invoke("control.organizations.memberships.list", pathParams, options);
    }
    controlOrganizationsMembershipsUpdate(pathParams, options = {}) {
        return this.invoke("control.organizations.memberships.update", pathParams, options);
    }
    controlOrganizationsUpdate(pathParams, options = {}) {
        return this.invoke("control.organizations.update", pathParams, options);
    }
    controlOrganizationsUsageSummaryGet(pathParams, options = {}) {
        return this.invoke("control.organizations.usage-summary.get", pathParams, options);
    }
    controlProvisioningJobsGet(pathParams, options = {}) {
        return this.invoke("control.provisioning.jobs.get", pathParams, options);
    }
    controlWorkspacesCreate(options = {}) {
        return this.invoke("control.workspaces.create", {}, options);
    }
    controlWorkspacesDecommission(pathParams, options = {}) {
        return this.invoke("control.workspaces.decommission", pathParams, options);
    }
    controlWorkspacesGet(pathParams, options = {}) {
        return this.invoke("control.workspaces.get", pathParams, options);
    }
    controlWorkspacesLaunchSessionsCreate(pathParams, options = {}) {
        return this.invoke("control.workspaces.launch-sessions.create", pathParams, options);
    }
    controlWorkspacesList(options = {}) {
        return this.invoke("control.workspaces.list", {}, options);
    }
    controlWorkspacesReplace(pathParams, options = {}) {
        return this.invoke("control.workspaces.replace", pathParams, options);
    }
    controlWorkspacesRestore(pathParams, options = {}) {
        return this.invoke("control.workspaces.restore", pathParams, options);
    }
    controlWorkspacesResume(pathParams, options = {}) {
        return this.invoke("control.workspaces.resume", pathParams, options);
    }
    controlWorkspacesRoutingManifestGet(pathParams, options = {}) {
        return this.invoke("control.workspaces.routing-manifest.get", pathParams, options);
    }
    controlWorkspacesSessionExchangeCreate(pathParams, options = {}) {
        return this.invoke("control.workspaces.session-exchange.create", pathParams, options);
    }
    controlWorkspacesSuspend(pathParams, options = {}) {
        return this.invoke("control.workspaces.suspend", pathParams, options);
    }
}
