# Organization Autorunner Foundation

This document captures the durable product and architecture decisions that define Organization Autorunner (OAR). These decisions form the stable boundary for all implementation work. When in doubt, this document is the authoritative source of product truth.

## Product Position

### OAR is a manager / executive operating system

OAR is primarily a **manager and executive operating system**, not a generic work-management tool or a general-purpose collaboration clone.

The system is designed to help managers and executives maintain organizational memory, track open work on topics and cards, and ensure follow-through on critical decisions. It optimizes for:
- **High-leverage oversight**: giving managers visibility into the state of topics, cards, risks, and decisions across their scope of responsibility.
- **Evidence-based progress**: ensuring that claims of completion or status change are grounded in receipts, decisions, or verifiable artifacts.
- **Minimal active maintenance**: the system should surface what needs attention without requiring constant manual grooming.

This positioning shapes what OAR does not try to be: a ticket tracker, a chat platform, a project management system, or a general-purpose workspace where any kind of work can happen. Those tools exist. OAR focuses on the manager's specific problem: maintaining durable institutional memory and ensuring follow-through in environments where agents and humans collaborate.

## Core Architecture Decisions

### The workspace is the top-level isolated unit

The **workspace** is the primary isolation boundary in OAR. A workspace contains its own set of topics, cards, boards, documents, events, artifacts, and actors, plus backing **threads** used for timelines and packet subject resolution (threads are infrastructure, not the primary operator noun). Workspaces are isolated from one another; there is no cross-workspace visibility or shared state at the storage layer.

This decision supports:
- **Clean multi-tenant isolation**: each workspace is an independent organizational context.
- **Simple reasoning about scope**: actors, data, and provenance are all scoped to a single workspace.
- **Clear hosting model**: workspaces can be provisioned, backed up, and migrated independently.

### Canonical runtime truth is SQLite + filesystem blobs

The canonical source of truth at runtime is **SQLite for structured data** and **filesystem blobs for content**. This hybrid model replaces earlier designs that treated the filesystem itself as the primary runtime substrate.

Implications:
- **SQLite** stores events, topics, cards, boards, artifact metadata, documents, actor registry, backing thread rows, and derived views.
- **Filesystem** stores artifact content, referenced by path from SQLite metadata.
- **File-first** approaches (if mentioned at all) describe import/export/backup shapes, not the live runtime source of truth.
- All query, mutation, and projection logic operates against the SQLite + blob substrate.

This decision ensures:
- Reliable transactional semantics for state mutations.
- Efficient querying and indexing for derived views.
- Clear separation between structured metadata and opaque content.
- Simple backup and restore (SQLite dump + blob directory).

### Boards and documents are canonical organizing layers

**Boards** and **documents** are canonical organizing layers in OAR. They are not disposable UI sugar or optional projections.

Implications:
- **Boards** provide structured views over topics, cards, and related backing threads. They are first-class entities with their own storage, lifecycle, and identity. Boards can be persisted, shared, and referenced independently of any particular UI session.
- **Documents** provide a first-class lifecycle with revision history, trash/archive semantics (per contract field names), and Merkle-chain integrity. They are distinct from generic artifacts and have their own API surface.
- Both boards and documents exist in the core data model and can be manipulated through the canonical API, not just through the UI.

This does not mean boards and documents replace threads, artifacts, or events. Those primitives remain the foundation. Boards and documents sit on top as durable organizing layers that help humans and agents navigate, curate, and reason about the underlying data.

### Authentication model: passkeys for humans, key pairs for agents

OAR distinguishes between human and agent authentication:

- **Humans** authenticate with **passkeys** (WebAuthn). This provides phishing-resistant authentication without passwords.
- **Agents** authenticate with **public/private key pairs**. The agent presents a signed challenge proving possession of the private key.
- **Legacy actor-mode** (where the system treats all actors uniformly without authentication type distinction) exists only as a development convenience. It is not a product identity story and should not be treated as the default in production or hosted environments.

This separation supports:
- Clear audit trails distinguishing human vs. agent actions.
- Different security postures for different actor types.
- Future evolution (e.g., hardware-bound agent keys, scoped agent permissions).

**Hosted v1** keeps human passkeys and agent keys as workspace-local principals inside one isolated workspace deployment.

### Canonical APIs and projection APIs are distinct surfaces

OAR exposes two distinct API surfaces:

1. **Canonical APIs**: The core API for reading and writing durable state. Operations on events, topics, cards, boards, documents, artifacts, and actors, plus read-only inspection of backing threads where the contract exposes them. These APIs are stable, versioned, and contract-enforced.

2. **Projection APIs**: Derived views that aggregate or transform canonical data for specific use cases. Examples include inbox items, staleness indicators, and board views. These APIs are convenience layers that may evolve independently of canonical storage.

Implications:
- Projection APIs are regenerable from canonical data.
- Projection API changes do not require storage migrations.
- Clients can choose whether to consume canonical APIs directly (for maximum control and auditability) or projection APIs (for convenience).

### Hosted track: hosted v1

**Hosted v1** is the managed offering shipping on the current workspace-core contract. Each customer/workspace gets one isolated workspace deployment and isolated storage domain. Operators provision, back up, restore, and replace those deployments with managed workflows.

Invariants:

- **Workspace isolation stays in core**: durable workspace truth remains inside each workspace core.
- **No shared row-level multitenancy in core**: the control plane coordinates isolated workspaces rather than replacing them with one shared tenant table.
- **Background projection materialization remains valid**: derived views can still be computed per workspace to keep reads responsive.
- **Agents remain workspace-local**: the control plane is not the durable home for agent identity or agent execution.

This split preserves operational simplicity in hosted v1 while fixing a clear forward direction for SaaS.

## Relationship to Implementation Specs

This foundation document defines product-level and architecture-level decisions. Implementation specs for individual modules (core, CLI, web-ui) describe how those decisions are realized in code:

- **Core spec** (`core/docs/anx-core-spec.md`): defines how canonical state, evidence, and primitives are implemented and enforced.
- **UI spec** (`web-ui/docs/anx-ui-spec.md`): defines how the operator interface exposes the foundation decisions.
- **Contract specs** (`contracts/`): define the schema and API boundaries that all modules must honor.

When this document conflicts with module specs, **this document wins**. Module specs should be updated to align with the foundation.
