---
title: "Clean up hosted billing activation polling on route exit"
agent: codex
done: true
ticket_id: "tkt_webui_billing_poll_cleanup_063"
---

## Triage

Priority: P2. This is cleanup for route lifecycle correctness and background API noise; fix after the higher-risk operator state bugs.

## Problem

The hosted billing page starts an async checkout activation polling loop, but the route effect does not return cleanup. If the operator navigates away during the delayed polling sequence, the loop can continue calling the control plane and mutating page state after the component is no longer relevant.

This creates noisy background API traffic and can show stale activation timeout state if the component is reused for another organization.

## Evidence

- `web-ui/src/routes/hosted/organizations/[orgId]/billing/+page.svelte:190`-`:194` creates a `cancelled` flag and assigns `pollStop`.
- `web-ui/src/routes/hosted/organizations/[orgId]/billing/+page.svelte:196`-`:221` awaits delayed polls and mutates `summary`, `activatingBanner`, and `activationTimedOut`.
- `web-ui/src/routes/hosted/organizations/[orgId]/billing/+page.svelte:233`-`:239` starts `load()` from a `$effect`, but does not return cleanup that calls `pollStop()`.
- Command used: `nl -ba 'web-ui/src/routes/hosted/organizations/[orgId]/billing/+page.svelte' | sed -n '138,240p'`.

## Proposed Fix

Return a cleanup function from the billing route `$effect` that calls `pollStop()` and resets `pollStop` to a no-op. Consider moving activation polling into a small helper that accepts an abort signal or cancellation callback, so delayed waits and follow-up `fetchBillingSummary()` calls are skipped after navigation or org id changes.

Ensure `load()` also discards stale results if `orgId` changes while a request is in flight.

## Validation

- Add a unit test around the billing activation poll helper, or a route-level test with fake timers, proving cleanup prevents later summary/timeout mutation.
- Run `web-ui/tests/unit/hosted-billing-activation.test.js` plus any new hosted billing test.

## Progress

- Added an abort-aware `pollBillingActivation()` helper in `web-ui/src/lib/hosted/billingActivation.js`.
- Updated hosted billing route cleanup to abort polling and reset `pollStop`; route loads now capture the org id and discard stale in-flight results.
- Added fake-timer unit coverage for aborting before delayed polls and aborting while a summary fetch is in flight.

## Completed Validation

- `pnpm --dir web-ui exec vitest run tests/unit/hosted-billing-activation.test.js`
- `make -C web-ui check`
