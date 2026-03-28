# OAR Command Registry

Generated from `contracts/oar-control-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.1.0`
- Commands: `36`

## `control.accounts.passkeys.register.finish`

- CLI path: `accounts passkeys register finish`
- HTTP: `POST /account/passkeys/registrations/finish`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Verify the WebAuthn attestation and issue the initial control-plane account session.
- Concepts: `control-auth`, `passkeys`, `sessions`
- Error codes: `invalid_json`, `invalid_request`, `session_expired`, `credential_invalid`
- Output: Returns `{ account, session }` after successful attestation.
- Agent notes: Registration session ids are short-lived and one-time use.
- Examples:
  - Finish account registration: `oar api call --base-url https://control.oar.example --method POST --path /account/passkeys/registrations/finish --body @registration-finish.json`

## `control.accounts.passkeys.register.start`

- CLI path: `accounts passkeys register start`
- HTTP: `POST /account/passkeys/registrations/start`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Begin managed human-account registration in the control plane before any workspace-specific grant is issued.
- Concepts: `control-auth`, `passkeys`, `accounts`
- Error codes: `invalid_json`, `invalid_request`, `account_exists`
- Output: Returns `{ registration_session_id, public_key_options, account }`.
- Agent notes: Human-driven WebAuthn ceremony. Retry by starting a new registration session when the browser ceremony expires.
- Examples:
  - Start account registration: `oar api call --base-url https://control.oar.example --method POST --path /account/passkeys/registrations/start --body '{"email":"ops@example.com","display_name":"Ops Lead"}'`

## `control.accounts.sessions.finish`

- CLI path: `accounts sessions finish`
- HTTP: `POST /account/sessions/finish`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Verify the WebAuthn assertion and issue a control-plane session for later organization and workspace actions.
- Concepts: `control-auth`, `sessions`, `passkeys`
- Error codes: `invalid_json`, `invalid_request`, `session_expired`, `credential_invalid`
- Output: Returns `{ account, session }`.
- Agent notes: Short-lived one-time session ids. Do not retry with the same signed assertion after a transport failure unless the caller is sure it was not accepted.
- Examples:
  - Finish control-plane sign-in: `oar api call --base-url https://control.oar.example --method POST --path /account/sessions/finish --body @session-finish.json`

## `control.accounts.sessions.revoke-current`

- CLI path: `accounts sessions revoke-current`
- HTTP: `DELETE /account/sessions/current`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Allow a human to explicitly end their current control-plane browser or API session.
- Concepts: `control-auth`, `sessions`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ revoked: true }`.
- Agent notes: Idempotent from the caller perspective. Safe to call during logout cleanup.
- Examples:
  - Revoke current session: `oar api call --base-url https://control.oar.example --method DELETE --path /account/sessions/current --header 'Authorization: Bearer <control-session>'`

## `control.accounts.sessions.start`

- CLI path: `accounts sessions start`
- HTTP: `POST /account/sessions/start`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Create a short-lived control-plane sign-in ceremony for an existing human account.
- Concepts: `control-auth`, `sessions`, `passkeys`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `account_disabled`
- Output: Returns `{ session_id, public_key_options, account_hint }`.
- Agent notes: Use only for human account sign-in. Agents remain workspace-local and do not use this flow.
- Examples:
  - Start control-plane sign-in: `oar api call --base-url https://control.oar.example --method POST --path /account/sessions/start --body '{"email":"ops@example.com"}'`

## `control.billing.webhooks.stripe.receive`

- CLI path: `billing webhooks stripe receive`
- HTTP: `POST /billing/webhooks/stripe`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Accept Stripe subscription events into the control plane so billing state can map cleanly onto org entitlements.
- Concepts: `billing`, `webhooks`, `subscriptions`
- Error codes: `invalid_json`, `invalid_request`, `invalid_signature`
- Output: Returns `{ webhook }` with receipt details for the accepted event.
- Agent notes: Public endpoint. Stripe signature verification should be enabled before production traffic.
- Examples:
  - Stripe webhook: `curl -X POST https://control.oar.example/billing/webhooks/stripe -H 'Stripe-Signature: <signature>' -H 'Content-Type: application/json' --data-binary @event.json`

## `control.organizations.billing.checkout-session.create`

- CLI path: `organizations billing checkout-session create`
- HTTP: `POST /organizations/{organization_id}/billing/checkout-session`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create an org-scoped Stripe checkout flow while keeping product entitlements inside the control plane.
- Concepts: `organizations`, `billing`, `checkout`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `access_denied`
- Output: Returns `{ session }` with a Stripe checkout URL or missing configuration details.
- Agent notes: Owner/admin only. Stripe manages payment collection, but entitlements still live in the control plane.
- Examples:
  - Create checkout session: `oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/billing/checkout-session --body '{"plan_tier":"team"}' --header 'Authorization: Bearer <control-session>'`

## `control.organizations.billing.customer-portal-session.create`

- CLI path: `organizations billing customer-portal-session create`
- HTTP: `POST /organizations/{organization_id}/billing/customer-portal-session`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create an org-scoped Stripe customer portal flow without moving entitlement logic out of the control plane.
- Concepts: `organizations`, `billing`, `portal`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `access_denied`
- Output: Returns `{ session }` with a Stripe customer-portal URL or missing configuration details.
- Agent notes: Owner/admin only. Stripe manages the portal surface, but entitlements still live in the control plane.
- Examples:
  - Create customer portal session: `oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/billing/customer-portal-session --body '{}' --header 'Authorization: Bearer <control-session>'`

## `control.organizations.billing.get`

- CLI path: `organizations billing get`
- HTTP: `GET /organizations/{organization_id}/billing`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Expose org-scoped billing state and Stripe configuration readiness without overloading usage summaries.
- Concepts: `organizations`, `billing`, `plans`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ summary }` with billing state, usage summary, and Stripe configuration readiness.
- Agent notes: Safe and idempotent. This is an entitlement and billing control-plane view, not an invoice feed.
- Examples:
  - Get billing summary: `oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/billing --header 'Authorization: Bearer <control-session>'`

## `control.organizations.create`

- CLI path: `organizations create`
- HTTP: `POST /organizations`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Create the durable top-level SaaS organization record before provisioning any isolated workspace core.
- Concepts: `organizations`, `tenancy`, `billing`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `slug_conflict`
- Output: Returns `{ organization, membership }` for the creator.
- Agent notes: Not idempotent by default. Repeat only with an application-level idempotency key outside this contract.
- Examples:
  - Create organization: `oar api call --base-url https://control.oar.example --method POST --path /organizations --body '{"slug":"acme","display_name":"Acme","plan_tier":"team"}' --header 'Authorization: Bearer <control-session>'`

## `control.organizations.get`

- CLI path: `organizations get`
- HTTP: `GET /organizations/{organization_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Read one organization's control-plane configuration, plan, and lifecycle state.
- Concepts: `organizations`, `tenancy`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ organization }`.
- Agent notes: Safe and idempotent.
- Examples:
  - Get organization: `oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123 --header 'Authorization: Bearer <control-session>'`

## `control.organizations.invites.create`

- CLI path: `organizations invites create`
- HTTP: `POST /organizations/{organization_id}/invites`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Invite a control-plane human account into an organization before that human launches any isolated workspace.
- Concepts: `organizations`, `invites`, `access`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `invite_conflict`
- Output: Returns `{ invite, invite_url }`. The invite URL is returned only at creation time.
- Agent notes: Treat invite URLs as secrets. Reissuing an invite should create a new record instead of mutating the old secret.
- Examples:
  - Invite organization admin: `oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/invites --body '{"email":"finance@example.com","role":"admin"}' --header 'Authorization: Bearer <control-session>'`

## `control.organizations.invites.list`

- CLI path: `organizations invites list`
- HTTP: `GET /organizations/{organization_id}/invites`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Inspect pending or completed control-plane organization invites without exposing secrets beyond the invite link created at issuance time.
- Concepts: `organizations`, `invites`, `access`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ invites }`.
- Agent notes: Safe and idempotent.
- Examples:
  - List org invites: `oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/invites --header 'Authorization: Bearer <control-session>'`

## `control.organizations.invites.revoke`

- CLI path: `organizations invites revoke`
- HTTP: `POST /organizations/{organization_id}/invites/{invite_id}/revoke`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Invalidate a pending control-plane organization invite before it is accepted.
- Concepts: `organizations`, `invites`, `access`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ invite }` with updated lifecycle fields.
- Agent notes: Idempotent if the invite is already revoked.
- Examples:
  - Revoke org invite: `oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/invites/inv_123/revoke --header 'Authorization: Bearer <control-session>'`

## `control.organizations.list`

- CLI path: `organizations list`
- HTTP: `GET /organizations`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Load the organization registry visible to the current human account.
- Concepts: `organizations`, `tenancy`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ organizations }` ordered by create time ascending.
- Agent notes: Safe and idempotent.
- Examples:
  - List organizations: `oar api call --base-url https://control.oar.example --method GET --path /organizations --header 'Authorization: Bearer <control-session>'`

## `control.organizations.memberships.list`

- CLI path: `organizations memberships list`
- HTTP: `GET /organizations/{organization_id}/memberships`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Inspect which control-plane human accounts can access an organization and at what role.
- Concepts: `organizations`, `memberships`, `access`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ memberships }`.
- Agent notes: Safe and idempotent.
- Examples:
  - List memberships: `oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/memberships --header 'Authorization: Bearer <control-session>'`

## `control.organizations.memberships.update`

- CLI path: `organizations memberships update`
- HTTP: `PATCH /organizations/{organization_id}/memberships/{membership_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Change an existing member's role or disable their organization grant without touching workspace-local principals.
- Concepts: `organizations`, `memberships`, `access`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ membership }`.
- Agent notes: Patch semantics. Workspace access after update still depends on launch/session exchange grants.
- Examples:
  - Promote organization member: `oar api call --base-url https://control.oar.example --method PATCH --path /organizations/org_123/memberships/mem_123 --body '{"role":"owner"}' --header 'Authorization: Bearer <control-session>'`

## `control.organizations.update`

- CLI path: `organizations update`
- HTTP: `PATCH /organizations/{organization_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Adjust organization display, plan, or lifecycle flags without changing workspace-local data.
- Concepts: `organizations`, `billing`, `lifecycle`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`
- Output: Returns `{ organization }` with updated control-plane fields.
- Agent notes: Patch semantics. Omitted fields are left unchanged.
- Examples:
  - Update organization plan: `oar api call --base-url https://control.oar.example --method PATCH --path /organizations/org_123 --body '{"plan_tier":"scale"}' --header 'Authorization: Bearer <control-session>'`

## `control.organizations.usage-summary.get`

- CLI path: `organizations usage-summary get`
- HTTP: `GET /organizations/{organization_id}/usage-summary`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Expose plan and quota envelopes from the control plane without mixing them into workspace-local durable truth.
- Concepts: `usage`, `plans`, `quotas`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ summary }` with plan, usage, and remaining quota fields.
- Agent notes: Safe and idempotent. This is a control-plane summary, not a billing invoice.
- Examples:
  - Get usage summary: `oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/usage-summary --header 'Authorization: Bearer <control-session>'`

## `control.organizations.workspace-inventory.list`

- CLI path: `organizations workspace-inventory list`
- HTTP: `GET /organizations/{organization_id}/workspace-inventory`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Inspect workspace health, heartbeats, backups, and failed jobs from one support-friendly surface.
- Concepts: `workspaces`, `inventory`, `operations`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ organization_id, summary, workspaces, next_cursor }`.
- Agent notes: Safe and idempotent. The inventory is paginated by workspace creation order.
- Examples:
  - List workspace inventory: `oar api call --base-url https://control.oar.example --method GET --path '/organizations/org_123/workspace-inventory' --header 'Authorization: Bearer <control-session>'`

## `control.provisioning.jobs.get`

- CLI path: `provisioning jobs get`
- HTTP: `GET /provisioning/jobs/{job_id}`
- Stability: `beta`
- Surface: `utility`
- Input mode: `none`
- Why: Poll provisioning and lifecycle jobs that create, repair, or replace isolated workspace cores.
- Concepts: `provisioning`, `lifecycle`, `workspaces`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ job }`.
- Agent notes: Safe and idempotent. Use polling or backoff; the contract does not require a watch stream yet.
- Examples:
  - Poll provisioning job: `oar api call --base-url https://control.oar.example --method GET --path /provisioning/jobs/job_123 --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.backups.create`

- CLI path: `workspaces backups create`
- HTTP: `POST /workspaces/{workspace_id}/backups`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Queue a durable backup job for the workspace and record the resulting backup history and retention metadata.
- Concepts: `workspaces`, `backups`, `provisioning`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `workspace_not_ready`
- Output: Returns `{ workspace, provisioning_job }`.
- Agent notes: Backups are recorded as durable jobs. Retry only after checking the prior job result.
- Examples:
  - Run backup job: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/backups --body '{"schedule_name":"nightly","retention_days":30}' --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.create`

- CLI path: `workspaces create`
- HTTP: `POST /workspaces`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Allocate a new isolated workspace core under an organization and queue its provisioning lifecycle.
- Concepts: `workspaces`, `provisioning`, `registry`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `slug_conflict`, `quota_exceeded`
- Output: Returns `{ workspace, provisioning_job }`.
- Agent notes: Creates registry state and queues background provisioning. The workspace is not ready for launch until the job succeeds.
- Examples:
  - Provision workspace: `oar api call --base-url https://control.oar.example --method POST --path /workspaces --body '{"organization_id":"org_123","slug":"ops","display_name":"Ops","region":"us-central1","workspace_tier":"standard"}' --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.decommission`

- CLI path: `workspaces decommission`
- HTTP: `POST /workspaces/{workspace_id}/decommission`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Retire a workspace deployment while preserving the control-plane registry and job history for later audit and retry decisions.
- Concepts: `workspaces`, `lifecycle`, `routing`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ workspace, provisioning_job }`.
- Agent notes: Idempotent from a user perspective once the workspace is archived.
- Examples:
  - Decommission workspace: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/decommission --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.get`

- CLI path: `workspaces get`
- HTTP: `GET /workspaces/{workspace_id}`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Read one workspace registry record and its current lifecycle summary.
- Concepts: `workspaces`, `registry`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ workspace }`.
- Agent notes: Safe and idempotent.
- Examples:
  - Read workspace: `oar api call --base-url https://control.oar.example --method GET --path /workspaces/ws_123 --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.heartbeat.record`

- CLI path: `workspaces heartbeat record`
- HTTP: `POST /workspaces/{workspace_id}/heartbeat`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Let the isolated workspace instance report version, health, projection maintenance, usage, and backup telemetry back to the control plane.
- Concepts: `workspaces`, `heartbeat`, `operations`
- Error codes: `invalid_json`, `invalid_request`, `invalid_token`, `not_found`, `service_unavailable`
- Output: Returns `{ workspace }` with refreshed fleet telemetry.
- Agent notes: Workspace-service authenticated. Do not replay with stale assertions.
- Examples:
  - Record heartbeat: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/heartbeat --header 'Authorization: Bearer <workspace-service-token>' --body @heartbeat.json`

## `control.workspaces.launch-sessions.create`

- CLI path: `workspaces launch-sessions create`
- HTTP: `POST /workspaces/{workspace_id}/launch-sessions`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Broker human entry into an isolated workspace UI from the control plane without moving workspace identity into the control plane data plane.
- Concepts: `workspaces`, `launch`, `grants`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `workspace_not_ready`
- Output: Returns `{ launch_session }` including `workspace_path` and one-time exchange token metadata.
- Agent notes: Launch sessions are for humans. Agents stay workspace-local and should authenticate directly against the workspace core.
- Examples:
  - Launch workspace UI: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/launch-sessions --body '{"return_path":"/ws/ops/threads"}' --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.list`

- CLI path: `workspaces list`
- HTTP: `GET /workspaces`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Read the control-plane registry of isolated workspaces without crossing the workspace data boundary.
- Concepts: `workspaces`, `registry`, `tenancy`
- Error codes: `auth_required`, `invalid_token`
- Output: Returns `{ workspaces }`.
- Agent notes: Safe and idempotent.
- Examples:
  - List workspaces for an organization: `oar api call --base-url https://control.oar.example --method GET --path '/workspaces?organization_id=org_123' --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.replace`

- CLI path: `workspaces replace`
- HTTP: `POST /workspaces/{workspace_id}/replace`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Replace a workspace deployment in place by restoring a backup bundle into its registry-owned instance root.
- Concepts: `workspaces`, `lifecycle`, `restore`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `workspace_not_ready`
- Output: Returns `{ workspace, provisioning_job }`.
- Agent notes: The backup bundle must already exist and pass validation. The job is retryable if the prior attempt failed.
- Examples:
  - Replace workspace: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/replace --body '{"backup_dir":"/var/backups/ws_123-20260321T000000Z"}' --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.restore`

- CLI path: `workspaces restore`
- HTTP: `POST /workspaces/{workspace_id}/restore`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Restore a workspace deployment from a validated backup bundle using the hosted restore workflow.
- Concepts: `workspaces`, `lifecycle`, `restore`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `workspace_not_ready`
- Output: Returns `{ workspace, provisioning_job }`.
- Agent notes: The backup bundle must already exist and pass validation. Failed restores must retain the script stderr tail.
- Examples:
  - Restore workspace: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/restore --body '{"backup_dir":"/var/backups/ws_123-20260321T000000Z"}' --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.restore-drills.create`

- CLI path: `workspaces restore-drills create`
- HTTP: `POST /workspaces/{workspace_id}/restore-drills`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Verify that a representative backup can be restored and booted without touching the live workspace registry record.
- Concepts: `workspaces`, `backups`, `restore`, `drills`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `workspace_not_ready`
- Output: Returns `{ workspace, provisioning_job }`.
- Agent notes: Restore drills use a temporary deployment root and retain job stderr/stdout on failure.
- Examples:
  - Run restore drill: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/restore-drills --body '{"backup_dir":"/var/backups/ws_123-20260321T000000Z"}' --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.resume`

- CLI path: `workspaces resume`
- HTTP: `POST /workspaces/{workspace_id}/resume`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Return a suspended workspace deployment to the ready state so shared routing can direct traffic to it again.
- Concepts: `workspaces`, `lifecycle`, `routing`
- Error codes: `auth_required`, `invalid_token`, `not_found`, `workspace_not_ready`
- Output: Returns `{ workspace, provisioning_job }`.
- Agent notes: Safe to retry only after checking the previous job result.
- Examples:
  - Resume workspace: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/resume --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.routing-manifest.get`

- CLI path: `workspaces routing-manifest get`
- HTTP: `GET /workspaces/{workspace_id}/routing-manifest`
- Stability: `beta`
- Surface: `projection`
- Input mode: `none`
- Why: Load machine-readable routing metadata for a single isolated workspace deployment so shared apps and proxies can resolve the correct backend.
- Concepts: `workspaces`, `registry`, `routing`
- Error codes: `auth_required`, `invalid_token`, `not_found`
- Output: Returns `{ routing_manifest }` for the requested workspace.
- Agent notes: Safe and idempotent. The manifest is derived from registry state and is suitable for proxy consumption.
- Examples:
  - Read routing manifest: `oar api call --base-url https://control.oar.example --method GET --path /workspaces/ws_123/routing-manifest --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.session-exchange.create`

- CLI path: `workspaces session-exchange create`
- HTTP: `POST /workspaces/{workspace_id}/session-exchange`
- Stability: `beta`
- Surface: `utility`
- Input mode: `json-body`
- Why: Convert a control-plane launch token into a workspace-scoped session grant that the isolated workspace core can trust.
- Concepts: `workspaces`, `grants`, `launch`
- Error codes: `invalid_json`, `invalid_request`, `not_found`, `exchange_expired`, `exchange_invalid`, `workspace_not_ready`
- Output: Returns `{ workspace, grant }` for the target workspace.
- Agent notes: One-time token exchange. The returned grant is scoped to one workspace and must not be reused across workspaces.
- Examples:
  - Exchange launch token: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/session-exchange --body '{"exchange_token":"<token>"}'`

## `control.workspaces.suspend`

- CLI path: `workspaces suspend`
- HTTP: `POST /workspaces/{workspace_id}/suspend`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `none`
- Why: Mark a workspace deployment as suspended and stop routing new launches to it without deleting the registry record.
- Concepts: `workspaces`, `lifecycle`, `routing`
- Error codes: `auth_required`, `invalid_token`, `not_found`, `workspace_not_ready`
- Output: Returns `{ workspace, provisioning_job }`.
- Agent notes: Safe to retry only after checking the previous job result. Suspension is a registry-level state transition.
- Examples:
  - Suspend workspace: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/suspend --header 'Authorization: Bearer <control-session>'`

## `control.workspaces.upgrade.create`

- CLI path: `workspaces upgrade create`
- HTTP: `POST /workspaces/{workspace_id}/upgrade`
- Stability: `beta`
- Surface: `canonical`
- Input mode: `json-body`
- Why: Track desired version versus current version and record pre/post health checks for a workspace upgrade.
- Concepts: `workspaces`, `upgrades`, `provisioning`
- Error codes: `auth_required`, `invalid_token`, `invalid_json`, `invalid_request`, `not_found`, `workspace_not_ready`
- Output: Returns `{ workspace, provisioning_job }`.
- Agent notes: Fail closed if the workspace has not reported a healthy heartbeat.
- Examples:
  - Run upgrade job: `oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/upgrade --body '{"desired_version":"hosted-instance/v2"}' --header 'Authorization: Bearer <control-session>'`

