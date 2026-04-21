# AGENTS

## Scope
Guide for work inside `web-ui/`.

## User-facing copy
- Do not put internal codenames or engineer jargon in UI strings users see.
- Avoid long em-dash explainers; prefer short, direct sentences.
- Avoid generic progress labels like `Working...`; name the action (`Signing in…`, `Confirming passkey…`).
- Prefer a single ellipsis style (`…` or `...`) and use it consistently in loading states.

Read this after the root [AGENTS.md](../AGENTS.md). Keep this file focused on durable operator-facing purpose, UI boundaries, and the invariants that protect safe interaction with `anx-core`.

## Module Purpose
`web-ui` is the operator control surface for Organization Autorunner.

It gives operators fast, glanceable visibility into the shared workspace maintained by `anx-core` and provides explicit paths for operator intervention such as decisions, reviews, resource edits (topics, docs, boards, cards), acknowledgments, and message posting. It is a client of `anx-core`, not an agent runtime or orchestration layer.

## UI Responsibilities
- Treat `anx-core` as the single source of truth for all durable state.
- Optimize for operator usability: clear status, triage context, provenance visibility, and at-a-glance understanding of what needs attention.
- Provide the main operator workflow surfaces for inbox triage, topic and backing-thread inspection, boards and cards, artifacts, documents, and review flows.
- Handle forward-compatible data safely: unknown event types, artifact kinds, refs, and fields must remain visible rather than breaking the UI.
- Inbox items from the API use `related_refs` only (see shared `inbox_item` schema). Do not read `item.refs` on inbox rows; legacy stored rows are normalized in core before they reach clients.
- Decision lifecycle writes (`decision_needed`, `intervention_needed`, `decision_made`) are thread-grounded in core: event `refs` must include `thread:<thread_id>`. Prefer reflecting that in any client-side validation or examples even when the operator navigates by topic.
- Gate writes safely through actor-aware and workspace-aware flows while preserving core contract semantics.

## High-Value Invariants
- Persistent writes go through `anx-core`; the UI must not invent its own source of truth.
- Unknown or newer data must degrade gracefully and remain inspectable.
- Resource patches use merge/patch semantics and must not overwrite fields the UI does not understand.
- Restricted transitions and provenance-sensitive fields must remain clearly evidence-backed versus inferred.
- The UI should favor glanceable inspection and targeted intervention over exhaustive agent-facing control surfaces.

## What Web UI Does Not Own
- Canonical storage or schema authority.
- Agent orchestration or automation workflows.
- Real-world side effects outside the OAR workspace.

## Canonical References
- Workspace auth (modes, cookies, errors, limitations): `docs/auth.md` (includes refresh-token **replay window** limitation — do not paper over with retries)
- Product and UX spec: `docs/anx-ui-spec.md`
- HTTP contract: `docs/http-api.md`
- Shared schema: `../contracts/anx-schema.yaml`
- Spec compliance matrix: `docs/spec-compliance.md`
- Runbook: `docs/runbook.md`
- Visual style guidance: `docs/style-guide.md`

## Edit Routing
- Shared API or schema changes start in [../contracts/AGENTS.md](../contracts/AGENTS.md).
- Changes to `scripts/seed-core-from-mock.mjs` (or env it relies on) can affect **CLI-local artifacts** under `../cli/dogfood-resources/`; coordinate with [../cli/AGENTS.md](../cli/AGENTS.md) and `../cli/docs/runbook.md`.
- UI behavior changes should be checked against operator clarity first, then contract compatibility.
- Routing and **core proxy** behavior must preserve the single-source-of-truth model (no in-UI synthetic core APIs) and startup compatibility checks.
- Presentation changes should preserve glanceability and safe fallback behavior for unknown data.

## Validation
- `make -C web-ui check`
- `./scripts/test`
- Add or update unit and e2e coverage for affected operator flows.
- When contracts change, run `make contract-gen` and `make contract-check` from repo root.

## Maintenance Guidance
- Keep this file centered on operator-facing purpose and durable UI boundaries.
- Put route-by-route details and implementation specifics in specs, runbooks, or code-local docs.
- Update this guide when the operator surface or its boundaries materially change.
