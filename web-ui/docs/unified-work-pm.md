# Unified work and PM operator surface

The Work table and board project the same canonical `/work` records. Work stays
backed by existing cards; projects remain topics. Filters are shareable URL
parameters, and list pagination uses opaque server cursors. External source
statuses remain visible even when their normalized phase is unfamiliar. The
board has no drag/drop mutation. Source workflow changes go through authorized
PM decisions and source executors.

## Operator routes

All routes are under the current `/o/{organization}/w/{workspace}` prefix.

- `/work?view=table|board`: commitments, source status, next actor/action,
  meaningful progress and evidence freshness. Search and project/source/owner/
  phase/freshness filters call the canonical list endpoint.
- `/work/new`: native commitment creation on an existing board, with required
  acceptance criteria. External work registration belongs to source integration
  tooling. Save errors retain the form.
- `/work/{card_ref}`: source authority, acceptance criteria, blockers, next
  action, observation history, read failures, uncertainty, coverage, linked
  executions, relationships and source refresh requests.
- `/integrations`: collection health grouped by source connection across loaded
  work, with pagination and explicit coverage limits. Connections with no visible
  tracked work cannot be inferred from this projection.
- `/pm`: private durable conversation history, scoped work context, queued turn
  states, retained failed drafts, replay-safe request keys, conversation and
  message pagination. Suggested questions populate the composer without sending.
- `/decisions`: email-style Needs you / Watching / All decision mailboxes,
  explicit mobile reader navigation, source scope/revision, exact answers,
  delivery attempts and read-back receipts. Decisions and receipts are paged;
  a direct decision link loads its record even outside the current list page.
- `/inbox`: existing event-oriented triage remains intact. The clear-inbox state
  does not claim that all work or integrations are healthy.

Work and PM are primary navigation destinations. Topics, legacy boards,
Integration health, and Decisions & receipts remain available under More and the
desktop secondary navigation.

## Boundaries

All persistent actions use the existing authenticated workspace core client and
generated command registry. There is no browser database, alternate authority,
provider/model registry, worker launcher, or fabricated PM reply. Observed time,
source activity, and meaningful progress are displayed separately. An observation
that claims verification remains a reported claim unless core supplies trusted
verification. A queued refresh only records a request. Delivered and acknowledged
instructions are not outcomes; an outcome badge requires independently verified
receipt evidence.

Approvals carry the current decision revision. Delivery is a separate explicit
operation; receipt checking never resends. Unknown or sending actions have no
blind-retry button. A draft navigation guard protects unsent messages and answers;
pending writes block navigation until their result is known. External links must
be absolute HTTP(S) without embedded credentials.

## Validation

Run the normal module gate and production build:

```sh
make -C web-ui check
pnpm --dir web-ui run build
```

The dedicated browser suite starts an isolated schema fixture server and Vite
on configurable test ports. It never starts or writes to a real core workspace:

```sh
pnpm --dir web-ui exec playwright test --config playwright.pm.config.js
```

Its fixtures are synthetic. It covers board/table parity, preserved source
states, failed refreshes, retained list data, PM request replay, failed delivery,
and desktop/mobile overflow and axe accessibility checks. Component tests cover
these interaction boundaries independently, including out-of-order work loads,
revision-bound answers, navigation during writes and later-page discovery.

The implementation run passed the production build and unit/module checks.
Chromium launch was blocked by the macOS execution sandbox before any browser
assertion (`MachPortRendezvousServer` registration denied). Browser visual,
responsive and axe results therefore remain **unverified** until this suite runs
in a browser-capable environment. No real source, channel or deployment proof is
claimed by UI fixtures.

The runtime requires the canonical Work/PM contract and corresponding core
handlers. An unconfigured reader, PM bridge or source executor produces an
explicit unavailable/queued state. Real-source central import, authorized live
channel conversations and source-action verification require integrated dogfood;
they cannot be established by a frontend build.
