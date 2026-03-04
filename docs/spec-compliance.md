# oar-ui v0.2.2 Spec Compliance

Last updated: 2026-03-04

This document maps `docs/oar-ui-spec.md` requirements to current implementation,
known limitations, and test coverage.

## Integration Execution Notes

Backend (Terminal A):

```bash
cd ../organization-autorunner-core
./scripts/dev
```

UI integration test (Terminal B):

```bash
cd ../organization-autorunner-ui
OAR_CORE_BASE_URL=http://127.0.0.1:8000 ./scripts/e2e-with-core
```

This ticket validates behavior against a real `oar-core` instance via
`./scripts/e2e-with-core` as a complement to mocked Playwright and unit tests.

## Legend

- Status: `Met`, `Partial` (with explicit limitation), or `Deferred`
- Tests reference file paths under `tests/`

## 0. Purpose

| Requirement                                     | Status | Implementation                                                                            | Known limitations                                                            | Tests                                                                                          |
| ----------------------------------------------- | ------ | ----------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| Human-facing visibility + intervention UI       | Met    | Thread, inbox, artifact, and shell surfaces in `src/routes/*/+page.svelte`                | None                                                                         | `tests/e2e/shell.spec.js`, `tests/e2e/threads.spec.js`, `tests/e2e/thread-detail.spec.js`      |
| Must not maintain independent organizational DB | Met    | UI state is local-only; persistence goes through API clients (`src/lib/oarCoreClient.js`) | Mock routes are still used when core proxy is not configured (test/dev mode) | `tests/unit/actorClient.integration.test.js`, `tests/e2e/integration-core-golden-path.spec.js` |
| Must not perform real-world side effects        | Met    | No integrations for external side effects in route/components                             | None                                                                         | N/A (code-path audit)                                                                          |
| Must not orchestrate agents                     | Met    | No agent orchestration subsystem in this repo                                             | None                                                                         | N/A (code-path audit)                                                                          |

## 1. Integration Contract

| Requirement                                                                             | Status | Implementation                                                                                                                                                | Known limitations                                                                           | Tests                                                                                                                                        |
| --------------------------------------------------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| 1.1 Treat oar-core as source of truth                                                   | Met    | All reads/writes via `coreClient`/`oarCoreClient`; optional server proxy in `src/hooks.server.js`                                                             | No offline cache invalidation strategy beyond fresh fetches                                 | `tests/e2e/integration-core-golden-path.spec.js`                                                                                             |
| 1.2 Support schema primitives and typed conventions                                     | Met    | Threads, commitments, artifacts, events, work orders, receipts, reviews supported in routes/forms                                                             | Snapshot details for non-thread snapshots are placeholder-only                              | `tests/e2e/*.spec.js`, `tests/unit/*Utils*.test.js`                                                                                          |
| 1.2 Handle unknown event/artifact types gracefully                                      | Met    | Unknown timeline event handling in `src/lib/timelineUtils.js`; unknown artifact kind warning + raw panels in `src/routes/artifacts/[artifactId]/+page.svelte` | Unknown snapshot types render in placeholder route only                                     | `tests/e2e/headless-smoke.spec.js`, `tests/e2e/artifacts.spec.js`                                                                            |
| 1.2 Preserve/show unknown fields                                                        | Met    | Raw JSON panels via `UnknownObjectPanel` across major surfaces                                                                                                | Preservation is by patch omission for unknown fields, not explicit schema-aware merge in UI | `tests/unit/threadPatch.test.js`, `tests/e2e/headless-smoke.spec.js`                                                                         |
| 1.3 Parse and navigate typed refs                                                       | Met    | `src/lib/typedRefs.js`, `src/lib/refLinkModel.js`, `src/lib/components/RefLink.svelte`                                                                        | Unknown prefixes intentionally non-clickable raw text                                       | `tests/unit/typedRefs.test.js`, `tests/unit/refLinkModel.test.js`, `tests/unit/artifactRefs.test.js`                                         |
| 1.4 Actor identity required for writes                                                  | Met    | Actor gate + local selection in `src/routes/+layout.svelte` and `src/lib/actorSession.js`; `withActorId` in `oarCoreClient`                                   | Actor selection stored in browser local storage only                                        | `tests/e2e/shell.spec.js`, `tests/unit/actorSession.test.js`, `tests/unit/actorClient.integration.test.js`                                   |
| 1.5 Provenance rendering + inferred distinction + by_field                              | Met    | `ProvenanceBadge` with inferred/evidence styling and `by_field` expansion                                                                                     | `by_field` is shown as JSON details, not fully custom per-field UI widgets                  | `tests/unit/provenanceUtils.test.js`, `tests/e2e/commitments.spec.js`                                                                        |
| 1.6 Snapshot patch/merge semantics + list replacement + no core-maintained field writes | Met    | `buildThreadPatch` only emits editable fields; list fields wholesale replaced; `open_commitments` not editable and explicitly labeled read-only               | No dedicated visual diff preview before patch submit                                        | `tests/unit/threadPatch.test.js`, `tests/e2e/thread-detail.spec.js`                                                                          |
| 1.7 Follow reference conventions when creating events                                   | Met    | UI event/work-order/receipt/review payload construction includes deterministic refs (`thread detail`, `artifact detail`, `inbox`)                             | Convention enforcement ultimately server-side; UI validates only key ref fields             | `tests/e2e/work-orders.spec.js`, `tests/e2e/receipts.spec.js`, `tests/e2e/reviews.spec.js`, `tests/e2e/integration-core-golden-path.spec.js` |

## 2. Core UX Model (Thread-first)

| Requirement                                                                    | Status | Implementation                                                                                   | Known limitations            | Tests                                                                  |
| ------------------------------------------------------------------------------ | ------ | ------------------------------------------------------------------------------------------------ | ---------------------------- | ---------------------------------------------------------------------- |
| Threads as primary navigation                                                  | Met    | Sidebar + thread list/detail routes                                                              | None                         | `tests/e2e/threads.spec.js`, `tests/e2e/thread-detail.spec.js`         |
| Thread detail shows snapshot + timeline distinction                            | Met    | Snapshot section and timeline section in `src/routes/threads/[threadId]/+page.svelte`            | None                         | `tests/e2e/thread-detail.spec.js`                                      |
| Timeline time-ordered/stable rendering + typed ref links + unknown type safety | Met    | Timeline transform in `toTimelineView`; per-event rendering with typed refs and unknown fallback | No custom icons per type yet | `tests/unit/timelineUtils.test.js`, `tests/e2e/headless-smoke.spec.js` |

## 3. Required UI Surfaces

| Requirement                                                   | Status | Implementation                                                             | Known limitations                                                                   | Tests                                                                                                                          |
| ------------------------------------------------------------- | ------ | -------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| Inbox grouping, navigation, ack, decision recording           | Met    | `src/routes/inbox/+page.svelte`, grouping utils in `src/lib/inboxUtils.js` | No ranking engine (expected v0 behavior)                                            | `tests/e2e/inbox.spec.js`, `tests/unit/inboxUtils.test.js`                                                                     |
| Thread list with filters and creation                         | Met    | `src/routes/threads/+page.svelte`, `src/lib/threadFilters.js`              | No server-side pagination yet                                                       | `tests/e2e/threads.spec.js`, `tests/unit/threadFilters.test.js`                                                                |
| Thread detail edit + commitments + timeline + message posting | Met    | `src/routes/threads/[threadId]/+page.svelte`                               | Artifact linking is via editable typed refs in `key_artifacts`, no dedicated picker | `tests/e2e/thread-detail.spec.js`, `tests/e2e/commitments.spec.js`                                                             |
| Work order composer                                           | Met    | Work order form in thread detail + `validateWorkOrderDraft`                | Context suggestions are basic (thread + optional prefill)                           | `tests/e2e/work-orders.spec.js`, `tests/unit/workOrderUtils.test.js`                                                           |
| Receipt viewer + intake + review action                       | Met    | Receipt submission in thread detail and review workflow in artifact detail | Review rubric intentionally lightweight per v0                                      | `tests/e2e/receipts.spec.js`, `tests/e2e/reviews.spec.js`, `tests/unit/receiptUtils.test.js`, `tests/unit/reviewUtils.test.js` |

## 4. Grounding and Restricted Updates

| Requirement                                                                    | Status  | Implementation                                                             | Known limitations                                                          | Tests                                                                                      |
| ------------------------------------------------------------------------------ | ------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Restrict commitment status transitions (`done`/`canceled`) until refs provided | Met     | `validateCommitmentStatusTransition` + UI blocking in commitment edit form | UI does not offer chooser for existing receipt/decision IDs (manual entry) | `tests/unit/commitmentUtils.test.js`, `tests/e2e/commitments.spec.js`                      |
| Show per-field provenance on restricted status updates                         | Met     | Status provenance panel in commitment cards (`provenance.by_field.status`) | Display is summary-oriented, not timeline-joined                           | `tests/e2e/commitments.spec.js`                                                            |
| Evidence affordances                                                           | Partial | Typed ref entry available across forms/events                              | No dedicated evidence graph/aggregate commitment evidence panel yet        | `tests/e2e/work-orders.spec.js`, `tests/e2e/receipts.spec.js`, `tests/e2e/reviews.spec.js` |

## 5. Messages

| Requirement                                      | Status | Implementation                                         | Known limitations                                              | Tests                                                                               |
| ------------------------------------------------ | ------ | ------------------------------------------------------ | -------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| Message posting as `message_posted` thread event | Met    | Message form in thread detail creates `message_posted` | None                                                           | `tests/e2e/thread-detail.spec.js`, `tests/e2e/integration-core-golden-path.spec.js` |
| Replies reference parent event via `event:<id>`  | Met    | Reply selector adds `event:<replyToEventId>` ref       | No nested threaded visual grouping (explicitly optional in v0) | `tests/e2e/thread-detail.spec.js`, `tests/e2e/integration-core-golden-path.spec.js` |

## 6. Concurrency

| Requirement                                 | Status  | Implementation                                                              | Known limitations                             | Tests                             |
| ------------------------------------------- | ------- | --------------------------------------------------------------------------- | --------------------------------------------- | --------------------------------- |
| Optimistic locking on thread snapshot edits | Met     | Sends `if_updated_at`; 409 reload warning path in thread snapshot edit flow | No merge UI; user re-applies changes manually | `tests/e2e/thread-detail.spec.js` |
| Optimistic locking on commitment edits      | Met     | Sends `if_updated_at`; 409 shows warning and reloads latest snapshot        | No side-by-side diff for conflict resolution  | `tests/e2e/commitments.spec.js`   |
| Poll/subscribe for external changes         | Partial | Manual refresh/reload behavior only                                         | No active polling or subscription channel yet | N/A (known limitation)            |

## 7. Extensibility

| Requirement                               | Status  | Implementation                                                        | Known limitations                                                     | Tests                                                                                                             |
| ----------------------------------------- | ------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Unknown event types render safely         | Met     | Unknown event label + raw payload/refs in timeline                    | None                                                                  | `tests/e2e/headless-smoke.spec.js`                                                                                |
| Unknown artifact kinds render safely      | Met     | Unknown kind warning + raw metadata/content panels in artifact detail | No custom visualization for future artifact kinds (raw fallback only) | `tests/e2e/integration-core-golden-path.spec.js` (typed-ref nav), manual unknown-kind path via fallback component |
| Unknown fields preserved on round-trip    | Met     | Patch generation only includes editable known fields                  | Preservation depends on backend patch semantics (as intended by spec) | `tests/unit/threadPatch.test.js`, `tests/unit/commitmentUtils.test.js`                                            |
| Unknown ref prefixes rendered raw         | Met     | `resolveRefLink` unknown path returns non-link label                  | None                                                                  | `tests/unit/refLinkModel.test.js`, `tests/unit/inboxUtils.test.js`                                                |
| Unknown snapshot type degrades gracefully | Partial | Placeholder route at `src/routes/snapshots/[snapshotId]/+page.svelte` | No real dynamic snapshot-type detail rendering yet                    | N/A (placeholder behavior)                                                                                        |

## 8. v0 Release Definition Checklist

| v0 release item                                      | Status | Evidence                                                                                               |
| ---------------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------ |
| Inbox grouped + ack + navigation                     | Met    | `src/routes/inbox/+page.svelte`, `tests/e2e/inbox.spec.js`                                             |
| List and filter threads                              | Met    | `src/routes/threads/+page.svelte`, `tests/e2e/threads.spec.js`                                         |
| Thread detail editable snapshot + timeline refs      | Met    | `src/routes/threads/[threadId]/+page.svelte`, `tests/e2e/thread-detail.spec.js`                        |
| Commitments with restricted transitions + provenance | Met    | `commitmentUtils`, thread detail commitments section, `tests/e2e/commitments.spec.js`                  |
| Create work orders with typed refs                   | Met    | Work order composer + validation, `tests/e2e/work-orders.spec.js`                                      |
| View receipts + typed evidence refs                  | Met    | Artifact detail receipt view, `tests/e2e/receipts.spec.js`                                             |
| Perform lightweight review                           | Met    | Artifact detail review form, `tests/e2e/reviews.spec.js`                                               |
| Post messages on thread                              | Met    | Thread detail message form, `tests/e2e/thread-detail.spec.js`                                          |
| Inferred vs evidence-backed provenance distinction   | Met    | `ProvenanceBadge`, `tests/unit/provenanceUtils.test.js`                                                |
| Parse and navigate typed refs across surfaces        | Met    | `typedRefs`/`refLinkModel`/`RefLink`, `tests/unit/refLinkModel.test.js`, `tests/e2e/artifacts.spec.js` |
