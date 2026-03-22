# SaaS v-next

This document fixes the authoritative SaaS direction for Organization
Autorunner. It is separate from hosted v1.

## Status

This is the product and architecture cut line for self-serve SaaS work. Later
implementation tickets should treat this as the source of truth rather than
reopening the hosting model.

## Core shape

SaaS v-next is:

- one shared control plane for human accounts, organizations, workspace
  registry, provisioning/lifecycle jobs, usage/quota envelopes, and fleet
  operations metadata
- one isolated workspace core per workspace for durable OAR truth and
  workspace-local authz decisions
- one shared human entry surface that authenticates against the control plane
  and then launches into a specific workspace

SaaS v-next is not:

- shared row-level multitenancy inside `core`
- a design where the control plane becomes the system of record for workspace
  threads, commitments, documents, boards, or artifacts
- a design where agents authenticate once globally and then roam across
  workspaces

## Control plane responsibilities

The control plane owns shared SaaS concerns that exist above any single
workspace:

- human accounts and passkey-backed control-plane sessions
- organizations and organization membership
- organization invite issuance and lifecycle
- workspace registry records and workspace discovery
- provisioning, replacement, repair, and readiness jobs for isolated
  workspaces
- launch brokering and session exchange into workspace-scoped human grants
- usage, plan, and quota envelopes
- fleet operations metadata needed to operate many isolated workspaces

This means the control plane is the right place for self-serve onboarding,
organization administration, workspace creation, and quota enforcement. It is
not the right place for workspace-local durable truth.

## Workspace core responsibilities

Each workspace keeps the existing OAR isolation boundary:

- one workspace core remains the system of record for its own threads,
  commitments, documents, boards, events, artifacts, and projections
- workspace storage remains isolated per workspace
- workspace-local auth continues to gate canonical and projection APIs inside
  that workspace
- agents remain workspace-local principals and authenticate directly against the
  workspace core

There is no shared control-plane table that replaces isolated workspace data.
The workspace noun remains the top-level isolated unit.

## Human auth and launch flow

Humans authenticate to the control plane, not separately to each workspace.

The intended SaaS flow is:

1. The human signs in to the control plane with a passkey-backed account.
2. The control plane authorizes organization and workspace access.
3. The control plane creates a one-time launch session for a target workspace.
4. The human app preserves the workspace noun and path-based shape where
   possible, then exchanges the launch token for a workspace-scoped session
   grant.
5. The isolated workspace core accepts that workspace-scoped grant and serves
   the workspace UI and APIs.

The human app should preserve the current workspace-oriented URL model where
possible. The control plane may host organization selection and launch
screens, but once a workspace is chosen the UI should still look and route like
"this workspace", not like a global row in a shared tenant table.

## Agents

Agents remain workspace-local.

Implications:

- agent keys are created, rotated, and revoked within a workspace
- agent sessions do not depend on control-plane human login state
- control-plane contracts do not replace workspace agent-auth contracts
- any future cross-workspace automation must still be explicit about which
  workspace it is acting in

## Contract boundary

The control plane contract should cover:

- account passkey registration and session flows
- organization CRUD plus membership and invite lifecycle
- workspace registry CRUD needed for list/create/read
- provisioning job status
- workspace launch and session exchange
- usage and plan summary

The existing workspace contract stays authoritative for workspace-local OAR
behavior.

## ADR: human auth split

Hosted v1 and SaaS v-next intentionally split human authentication:

- hosted v1 may keep workspace-local human auth inside one isolated workspace
  deployment
- SaaS v-next uses control-plane-managed human auth and then issues
  workspace-scoped grants or tokens into one isolated workspace
- agents remain workspace-local in both tracks

This split is intentional. It lets hosted v1 stay operationally simple while
making SaaS self-serve identity coherent.

## Operational notes

### Control-plane startup

Start the control plane from the repo root:

```bash
make serve-control-plane
# or directly:
go run ./core/cmd/oar-control-plane --listen-addr 127.0.0.1:8100 --workspace-root .oar-control-plane
```

Required environment for session-exchange grants:

- `OAR_CONTROL_PLANE_WORKSPACE_GRANT_ISSUER` - issuer URL (defaults to control-plane listen address)
- `OAR_CONTROL_PLANE_WORKSPACE_GRANT_AUDIENCE` - audience (e.g., `oar-core`)
- `OAR_CONTROL_PLANE_WORKSPACE_GRANT_SIGNING_KEY` - Ed25519 private key in base64

### Test commands

CI runs these gates automatically on SaaS-sensitive changes:

```bash
make saas-smoke      # Multi-workspace control-plane smoke
make saas-e2e        # Extended flow with isolation and session revocation
make saas-load-smoke # Basic load with concurrent reads/writes
```

For local iteration, each script can be run directly:

```bash
./scripts/saas-smoke
./scripts/saas-e2e
SAAS_LOAD_NUM_WORKSPACES=2 ./scripts/saas-load-smoke
```

### What is intentionally out of scope

This ticket pack does not include:

- Billing and subscription integration
- External identity provider federation
- Multi-region workspace placement
- Automated traffic cutover or DNS management
- Tiered storage or archival policies
- Public signup flows

These are future work after the core SaaS path stabilizes.
