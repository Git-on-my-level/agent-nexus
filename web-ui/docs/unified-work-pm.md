# Unified work and Decide operator surface

Primary navigation is Inbox, Tasks, and Docs. The built-in Ask PM conversation
and its `/pm` routes were removed in 2026-11; any ordinary agent can support project PM work with
the `anx-pm` skill and the normal CLI. Tasks table and board project the same
canonical `/work` records. Work stays backed by existing cards. Filters are shareable URL
parameters, and list pagination uses opaque server cursors. External source
statuses remain visible even when their normalized phase is unfamiliar.

Nexus-owned Tasks board drops call `cards.move` with public `card:` refs.
Source-owned drops file a PM decision; they never silently mutate the source.

## Operator routes

All routes are under `/o/{organization}/w/{workspace}`.

- `/inbox`, `/inbox/{id}`: Needs you / Watching / Handled. Selection is the URL
  (`?item=`, defaulting to the first row), `?mailbox=` picks the mailbox and
  `?work_ref=` narrows it to one task. Decisions awaiting an answer live here,
  not on a separate page. Keyboard triage, undo and live updates: UI spec §3.1.
- `/tasks?view=table|board`: commitments, source status, next actor/action,
  evidence freshness. Search and project/source/owner/phase/freshness filters
  call `work.list`.
- `/tasks/new`: native commitment creation with required acceptance criteria.
  Boards are plumbing: `work.create` defaults the backing board, so the page
  asks the operator to pick one only when the workspace already has several.
- `/tasks/{card_ref}`: source authority, acceptance criteria, blockers, next
  action, observation history, and refresh requests.
- `/docs`, `/docs/{document_ref}`: shared knowledge. Comments are first-class;
  a doc can become a discussion room.
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
provider/model registry, worker launcher, or fabricated reply. Observed time,
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

The runtime requires the canonical Work/Decide contract and corresponding core
handlers. An unconfigured reader or source executor produces an explicit
unavailable state.
