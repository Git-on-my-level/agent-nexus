# Hosted v1

Hosted v1 is a managed hosted offering built from one isolated workspace
deployment per customer/workspace. This document is the authoritative cut line
for the current ticket pack.

## Status

This is the shipped hosted-v1 cut line for the current branch.

## Hosted cut line

- One deployment equals one isolated workspace and one isolated storage domain.
- Hosted v1 does not introduce shared row-level multitenancy.
- Hosted v1 does not require a self-service control plane. Provisioning is
  managed by operators using deployment and recovery scripts.
- A future control plane may wrap this later, but it is explicitly outside this
  ticket pack.

## Auth and onboarding

- Outside development mode, all workspace data routes require authentication.
- `OAR_ALLOW_UNAUTHENTICATED_WRITES` and UI actor-selection/dev-actor style
  flows are development-only escape hatches.
- Hosted v1 is not open signup. New principals enter through managed bootstrap
  or invite-gated onboarding.
- Passkey-authenticated humans and Ed25519 key-pair agents are both workspace
  principals.
- Hosted v1 intentionally has no fine-grained RBAC. Any authenticated
  principal has the same workspace authority, including invite issuance and
  invite revocation.

## Client and data contract

- Agents should prefer the CLI and generated clients over hand-authoring HTTP
  calls.
- Projection APIs are convenience reads for operators and tools. They are not
  the durable automation substrate.
- Stale-thread exceptions come from background maintenance or deterministic
  derived rebuilds, not from GET requests.
- Blob storage stays behind a backend seam. Filesystem storage is only the
  first backend, not a hosted-v1 architectural commitment.

## Operations

- Hosted ops in v1 rely on managed provisioning plus backup/restore scripts.
- Backup, restore, and workspace replacement happen per isolated deployment.
- Deploy docs should talk about managed instance provisioning now, not a
  required control plane that does not exist yet.

See `hosted-gate.md` for the short assumption list that downstream tickets
should treat as fixed.
