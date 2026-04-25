# Hosted v1 (historical)

> **Superseded for the private hosted SaaS product.** The **agent-nexus-saas** monorepo (root `ARCHITECTURE.md`, `controlplane/`, and `docs/legal/`) is the source of truth for the current **multi-tenant control plane** + per-workspace `anx-core` model, OAuth, and org/workspace registry. This file is **historical**: it records an **OSS-repo-era, operator-managed “one isolated workspace per deployment”** cut line kept for public repo history. For current deployment runbooks, use the private monorepo’s `controlplane/runbooks/`.

## What this document was

**Hosted v1** described a managed offering built from **one** isolated workspace deployment per customer/workspace—**without** the later private control plane (OAuth org registry, launch brokering across many workspaces, SaaS billing). That model was a **shipped cut line for its time** in the OSS repo; it is **not** the current Agent Nexus hosted SaaS architecture.

## Status (frozen)

- **Shipped cut line (historical):** single-workspace-per-deployment, operator scripts, no shared multitenancy inside `anx-core`.
- **Explicitly out of scope for this doc (then and now):** shared organizations, self-serve workspace creation, launch brokering, quota envelopes—these belong to the **current** monorepo design, not to this v1 snapshot.

The statements below describe that **historical** pack only.

## Hosted cut line (historical)

- One deployment equals one isolated workspace and one isolated storage domain.
- Hosted v1 does not introduce shared row-level multitenancy.
- Hosted v1 does not require a self-service control plane. Provisioning is
  managed by operators using deployment and recovery scripts.

## Auth and onboarding (historical)

- Outside development mode, all workspace data routes require authentication.
- Hosted v1 is not open signup. New principals enter through managed bootstrap
  or invite-gated onboarding.
- Hosted v1 may keep passkey-authenticated humans and Ed25519 key-pair agents
  as workspace principals inside the isolated workspace.
- Hosted v1 intentionally has no fine-grained RBAC. Any authenticated
  principal has the same workspace authority, including invite issuance and
  invite revocation.

## Client and data contract (historical)

- Agents should prefer the CLI and generated clients over hand-authoring HTTP
  calls.
- Projection APIs are convenience reads for operators and tools. They are not
  the durable automation substrate.
- Stale-topic exceptions come from background maintenance or deterministic
  derived rebuilds, not from GET requests.
- Blob storage stays behind a backend seam. Filesystem storage is only the
  first backend, not a hosted-v1 long-term architectural guarantee.

## Operations (historical)

- Hosted ops in v1 rely on managed provisioning plus backup/restore scripts.
- Backup, restore, and workspace replacement happen per isolated deployment.
- Deploy docs for **this** era described managed instance provisioning **without** requiring the **later** private multi-tenant control plane product (which is documented in **agent-nexus-saas**).

See `hosted-gate.md` for the short assumption list that downstream tickets
should treat as fixed (in the historical OSS context).
