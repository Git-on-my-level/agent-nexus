# Foundation

This directory defines the high-level architecture story for Organization
Autorunner. It is intentionally about boundaries and target state, not a claim
that every hosted-v1 detail is already implemented in code.

## Stable boundaries

- `core/` is the system of record. Durable truth lives in canonical events,
  snapshots, documents, and artifact metadata there.
- `cli/` is the preferred automation surface for agents. Generated clients and
  generated command metadata are the stable machine-facing integration path.
- `web-ui/` is the human operator surface. It exists for visibility, triage,
  and explicit intervention rather than agent orchestration.
- Contracts under `contracts/` define the cross-module handshake. Shared
  behavior changes start there.

## Read models and automation

- Workspace projection reads exist for convenience. They help the UI and CLI
  avoid client-side joins, but they are read-side materializations over
  canonical state, not a durable automation substrate.
- Durable automation should anchor on the canonical contract plus generated
  clients/CLI commands instead of hand-authoring HTTP flows against projection
  payloads.
- Stale-thread exceptions are emitted by background maintenance or deterministic
  rebuild work, not by GET handlers.

## Storage and deployment seams

- Blob storage is an internal backend seam. The first backend is the local
  filesystem, but hosted architecture must not depend on filesystem access as a
  public contract.
- Hosted v1 uses managed single-workspace deployments with isolated storage and
  backup domains.
- A control plane may exist later, but hosted v1 does not require one to ship.

See `hosted-v1.md` for the hosted cut line and `hosted-gate.md` for the
non-negotiable assumptions the rest of this ticket pack uses.
