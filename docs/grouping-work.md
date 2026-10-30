# Work tree

A card is the only work unit. Each card has at most one stored parent.
**Initiatives are exactly top-level active cards outside the Unsorted tray.**
Board roles and plan links do not determine initiative identity or parentage.
Boards keep their existing layout behavior in this phase.

The tree has at most three levels: initiative → workstream → task. Children
are ordered by creation time. One owner (assignee) is responsible for a card;
“awaiting an owner” means that owner is empty, regardless of internal leases.

## Ordinary agent loop

```sh
anx orient
anx tree
anx where "billing retry failures"
anx work create --title "Retry webhooks" --under card:billing
anx work start card:retry-webhooks
anx note card:retry-webhooks "Fix verified" --evidence https://github.com/example/project/pull/512
anx done card:retry-webhooks --evidence https://github.com/example/project/pull/512
```

`where` is read-only: it suggests homes and gives confirmation commands. An
agent confirms by explicitly naming the parent. It never files work on a
confidence score. Use `ask` and `await` when a human decision gates the work.

Creating without `--under` or `--initiative` succeeds in **Unsorted**, with
`needs_home=true` and a warning. It never accidentally becomes an initiative.
Run labels do not supply an implicit parent to `work create`. For an explicit
independent outcome, use `anx work create --title "Billing reliability" --initiative`.

## Move and address work

```sh
anx mv card:retry-webhooks --under card:billing
anx work move unsorted/retry-webhooks --under initiatives/billing
anx mv card:billing --root --reason "An independent outcome"
anx mv card:billing --under card:payments --reason "One shared outcome"
anx mv card:retry-webhooks --unsorted
anx tree initiatives/payments --depth 3 --include-done
anx work ls initiatives/payments
```

`mv` and `work move` change stored parentage, never plan steps. Exactly one of
`--under`, `--root`, or `--unsorted` is required. A reason (at most 2000
characters) is required for `--root` and for moving an initiative under another
card. Cycles, closed parents, and depth greater than three are rejected; depth
errors name the nearest legal parent. `--after` accepts a sibling plan step
id for compatibility; children remain ordered by creation time.

Stable `card:<handle-or-id>` refs are canonical. Paths are derived aliases:
`initiatives/<segment>[/<segment>[/<segment>]]` or `unsorted/<segment>`. Each
segment is the card's handle, or its id when it has no handle. Moves and handle
renames change paths; persist canonical refs in evidence and integrations.

`tree` defaults to depth two, excludes completed work unless requested, and
reports truncation. Read a narrower subtree when expansion is truncated.
`work ls` reads immediate children through a depth-two subtree and preserves
`has_more` and `truncated`. The API in this phase has no child cursor, so the CLI
cannot continue a truncated sibling page automatically. JSON output uses the
normal CLI envelope. An older server missing `/work/tree` produces an explicit
update-your-server/CLI diagnostic rather than an empty tree.

## Plans and compatibility

`anx plan` is a power verb for checklists: external steps and legacy card steps
may retain `after` dependencies. A card step is a reference, never a stored
parent. Moving a card does not rewrite the checklist. Existing `work group`,
`initiative promote`, and `initiative retire` remain deprecated compatibility
commands implemented over moves. Use `work move` for new workflows.
`top_level` metadata is deprecated on create; `work patch` rejects it. Fold/unfold are separate legacy
merge operations, outside the ordinary tree workflow.

## Human surfaces and safe writes

Watch and Initiatives use core's `is_initiative` predicate. Watch's lanes use
the brief projection and its visible child rollups. The Unsorted tray admits
only `needs_home` cards; suggested homes are explicit move buttons. Move actions
on lanes and children can return work to Unsorted or make it top level. Board
layout is unchanged.

Move drafts and submitted intents are saved on the device before sending, scoped
to workspace, principal and card. Storage failure prevents sending. The UI sends
the saved key as body `request_key`. Core commits moves and their original-result
receipts atomically; changed-body reuse conflicts. Uncertain sends retry with
bounded backoff using the same persisted body and key, including after reload.
Already queued `work.top_level` entries translate to root or Unsorted moves,
keeping their saved key and using an editable/default reason rather than a
rejected metadata patch. Existing saved legacy grouping requests retain their
original adapters.

## HTTP routes and read cost

- `POST /work/{card_ref}/move`: exactly one of `under`, `root:true`,
  `unsorted:true`, plus optional `reason`, `after`, `actor_id` and replay `request_key`; returns `{work}`.
- `GET /work/tree?root=...&depth=1..3&include_done=false`: `{node,truncated}`
  with a root, otherwise `{initiatives,unsorted,truncated}`.
- `GET /work/resolve?path=...`: `{ref,path}`, or 404.
- `GET /work/where?ref=...` or `?q=...`: current location, advisory suggestions
  and explicit confirmation commands.

The surface performs one tree request (one preceding resolve per supplied path),
one where request (plus path resolution), or a depth-two subtree for `ls`.
It performs no per-node requests, workspace inventory scans, or local parent
reconstruction. Initiative filtering is linear in the already bounded work page;
no board-role read is required. Watch retains its one bounded brief and one
20-row tray page per refresh. Core owns visibility admission, indexed parent
lookups, expansion bounds, suggestion budgets and SQL scale validation. See
[core performance gates](../core/docs/performance.md); those changes belong to
the parallel server implementation.
