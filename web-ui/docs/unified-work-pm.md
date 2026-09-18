# Unified work and PM operator surface

Primary navigation is Inbox, Tasks, and Docs. Ask PM is an action, not a
destination category. Tasks table and board project the same canonical `/work`
records. Work stays backed by existing cards. Filters are shareable URL
parameters, and list pagination uses opaque server cursors. External source
statuses remain visible even when their normalized phase is unfamiliar.

Nexus-owned Tasks board drops call `cards.move` with public `card:` refs.
Source-owned drops file a PM decision; they never silently mutate the source.

## Operator routes

All routes are under `/o/{organization}/w/{workspace}`.

- `/inbox`, `/inbox/{id}`: Needs you / Watching / Handled. Selection is the URL
  (`?item=`). Decisions awaiting an answer live here, not on a separate page.
- `/tasks?view=table|board`: commitments, source status, next actor/action,
  evidence freshness. Search and project/source/owner/phase/freshness filters
  call `work.list`.
- `/tasks/new`: native commitment creation on an existing board, with required
  acceptance criteria.
- `/tasks/{card_ref}`: source authority, acceptance criteria, blockers, next
  action, observation history, and refresh requests.
- `/docs`, `/docs/{document_ref}`: shared knowledge. Comments are first-class;
  a doc can become a discussion room.
- `/pm`: Ask PM — private durable conversation, queued turns, replay-safe
  request keys. Suggested questions populate the composer without sending.
- `/integrations`, `/access`, `/secrets`, `/events`: settings (also under
  `/more` on mobile). Events is the audit log.

There are no legacy route aliases. `/work`, `/work/{card_ref}`, `/work/new`,
`/decisions` and `/settings` were removed rather than left as redirects; the
canonical paths above are the only ones. `/work` remains a **core API** path
(`work.list` / `work.get`) and is unrelated to the removed UI route.

`/threads` and `/threads/{threadId}` remain inspection surfaces for backing
conversations and inbox deep links. They are not a fourth primitive: `/threads`
is listed under a "Diagnostics" group in the sidebar footer / `/more` hub,
never in primary nav.

## Boundaries

All persistent actions use the authenticated workspace core client and generated
command registry. There is no browser database, alternate authority,
provider/model registry, worker launcher, or fabricated PM reply. Observed time,
source activity, and meaningful progress are displayed separately. An observation
that claims verification remains a reported claim unless core supplies trusted
verification. A queued refresh only records a request. Delivered and acknowledged
instructions are not outcomes.

Approvals carry the current decision revision. Delivery is a separate explicit
operation; receipt checking never resends. External links must be absolute
HTTP(S) without embedded credentials.

## Validation

```sh
make -C web-ui check
pnpm --dir web-ui run build
```

The runtime requires the canonical Work/PM contract and corresponding core
handlers. An unconfigured reader or source executor produces an explicit
unavailable/queued state. `anx pm serve` is the PM runner; it does not require
`ANX_PM_BRIDGE_ENABLED`.
