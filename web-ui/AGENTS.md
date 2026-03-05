# AGENTS

## Project purpose
organization-autorunner-ui is the frontend/client application for the Organization Autorunner system.

## Primary spec
- Repo spec: `docs/oar-ui-spec.md`
- HTTP contract: `docs/http-api.md`
- Shared schema: `contracts/oar-schema.yaml`
- Spec compliance matrix: `docs/spec-compliance.md`
- Build/serve/integration runbook: `docs/runbook.md`

## Architecture at a glance
- Framework/runtime: SvelteKit app (`src/routes`, `src/lib`, `src/hooks.server.js`).
- Core API client: `src/lib/oarCoreClient.js` (HTTP contract wrapper, actor injection, error normalization).
- App-level client binding: `src/lib/coreClient.js` (injects selected actor ID from session store).
- Connectivity modes:
  - Browser-direct via `PUBLIC_OAR_CORE_BASE_URL` (`src/lib/config.js`).
  - Server-side proxy via `OAR_CORE_BASE_URL` in `src/hooks.server.js`.
  - Same-origin mock routes when no proxy/base URL is set (`src/routes/**/+server.js` + `src/lib/mockCoreData.js`).
- Startup guard: schema version is verified in `src/routes/+layout.js` via `verifyCoreSchemaVersion(...)`.
- Actor gate: enforced in `src/routes/+layout.svelte`, state managed in `src/lib/actorSession.js`.

## UI surface map
- Shell/landing: `src/routes/+page.svelte`
- App frame and actor selection gate: `src/routes/+layout.svelte`
- Inbox: `src/routes/inbox/+page.svelte`
- Thread list: `src/routes/threads/+page.svelte`
- Thread detail (snapshot, commitments, timeline, work orders, receipts, message posting): `src/routes/threads/[threadId]/+page.svelte`
- Artifact list/detail: `src/routes/artifacts/+page.svelte`, `src/routes/artifacts/[artifactId]/+page.svelte`
- Snapshot placeholder for unknown/non-thread snapshot links: `src/routes/snapshots/[snapshotId]/+page.svelte`

## Domain logic modules
- Typed refs:
  - Parse/render/prefix set: `src/lib/typedRefs.js`
  - Link resolution model: `src/lib/refLinkModel.js`
  - UI renderer: `src/lib/components/RefLink.svelte`
- Timeline shaping and unknown-event fallback: `src/lib/timelineUtils.js`
- Thread patch semantics and list parsing helpers: `src/lib/threadPatch.js`
- Commitment patch + restricted transition validation: `src/lib/commitmentUtils.js`
- Inbox grouping/sorting: `src/lib/inboxUtils.js`
- Work order draft validation: `src/lib/workOrderUtils.js`
- Receipt draft validation: `src/lib/receiptUtils.js`
- Review draft/payload builder: `src/lib/reviewUtils.js`
- Provenance display model: `src/lib/provenanceUtils.js`, `src/lib/components/ProvenanceBadge.svelte`
- Raw/unknown object visibility component: `src/lib/components/UnknownObjectPanel.svelte`

## API and route conventions
- Mutating calls require `actor_id`; client injects this automatically via selected actor (`createOarCoreClient` + `withActorId`).
- Mock API handlers under `src/routes/**/+server.js` mirror core endpoints and delegate state to `src/lib/mockCoreData.js`.
- Proxy allowlist for core paths is explicit in `shouldProxyToCore(...)` in `src/hooks.server.js`.
- `src/routes/version/+server.js` reports expected schema version used by startup checks.

## Testing and quality gates
- Unit tests: `tests/unit/**/*.test.js` (pure module behavior).
- E2E tests (mocked/network-routed): `tests/e2e/*.spec.js`.
- Real-core integration golden path: `tests/e2e/integration-core-golden-path.spec.js` + `playwright.integration.config.js`.
- Main commands:
  - Dev: `./scripts/dev`
  - Full local checks: `./scripts/test`
  - Core integration run: `OAR_CORE_BASE_URL=http://127.0.0.1:8000 ./scripts/e2e-with-core`
  - Build and serve: `./scripts/build`, `./scripts/serve`

## High-value invariants (easy to break)
- Unknown event/artifact types and unknown refs must render safely, not crash/hide data.
- Snapshot edits must use patch/merge behavior and preserve unknown fields by omission.
- List-valued patch fields are wholesale replacement when present.
- Restricted commitment status transitions (`done`/`canceled`) require typed evidence refs.
- Actor gate must block writes when no actor is selected.
- Core schema mismatch must fail fast at startup.

## Change-impact checklist
- If you add/rename core endpoints:
  - Update `src/lib/oarCoreClient.js` methods.
  - Update proxy allowlist in `src/hooks.server.js`.
  - Update mock `+server.js` handlers and `src/lib/mockCoreData.js`.
  - Update `docs/http-api.md` and tests.
- If you add ref prefixes:
  - Update `src/lib/typedRefs.js` and `src/lib/refLinkModel.js`.
  - Add/adjust tests in `tests/unit/typedRefs.test.js` and `tests/unit/refLinkModel.test.js`.
- If you add event types:
  - Update known type handling in `src/lib/timelineUtils.js`.
  - Ensure timeline UI has safe fallback behavior and add e2e coverage.
- If schema version changes:
  - Update `EXPECTED_SCHEMA_VERSION` in `src/lib/config.js`.
  - Confirm `/version` behavior and startup check in `src/routes/+layout.js`.
  - Align docs (`README.md`, `docs/*`) and integration tests.
- If actor/session behavior changes:
  - Update `src/lib/actorSession.js`, actor gate in `+layout.svelte`, and actor-related tests.

## Maintenance guidance
Treat this file as a timeless index for the codebase.
As the project evolves, update this file with stable pointers to architecture docs, conventions, subsystems, and operational runbooks.
