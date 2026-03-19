# Hosted v1 gate

Use these as fixed assumptions for the hosted-v1 ticket pack:

- managed hosted offering now, control plane later
- one isolated workspace deployment per customer/workspace
- no shared row-level multitenancy
- auth required on workspace data routes outside development mode
- public registration closed; onboarding is bootstrap/invite-gated
- passkey humans and Ed25519 key-pair agents are both workspace principals
- no fine-grained RBAC in v1
- any authenticated principal may issue and revoke invites in v1
- agents prefer CLI/generated clients over hand-authored HTTP
- projection APIs are convenience reads only
- stale exceptions are background-maintenance output, not GET side effects
- blob storage is a backend seam; filesystem is only the first backend
- hosted ops use managed provisioning plus backup/restore scripts
