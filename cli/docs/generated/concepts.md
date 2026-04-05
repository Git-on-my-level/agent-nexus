# OAR Concepts

Generated from `contracts/oar-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.3.0`
- Concepts: `20`

## `artifacts`

- Commands: `8`
- Command IDs:
  - `artifacts.archive`
  - `artifacts.create`
  - `artifacts.get`
  - `artifacts.list`
  - `artifacts.purge`
  - `artifacts.restore`
  - `artifacts.trash`
  - `artifacts.unarchive`

## `boards`

- Commands: `15`
- Command IDs:
  - `boards.archive`
  - `boards.cards.create`
  - `boards.cards.get`
  - `boards.cards.list`
  - `boards.create`
  - `boards.get`
  - `boards.list`
  - `boards.patch`
  - `boards.purge`
  - `boards.restore`
  - `boards.trash`
  - `boards.unarchive`
  - `boards.workspace`
  - `cards.create`
  - `cards.move`

## `cards`

- Commands: `13`
- Command IDs:
  - `boards.cards.create`
  - `boards.cards.get`
  - `boards.cards.list`
  - `cards.archive`
  - `cards.create`
  - `cards.get`
  - `cards.list`
  - `cards.move`
  - `cards.patch`
  - `cards.purge`
  - `cards.restore`
  - `cards.timeline`
  - `cards.trash`

## `compatibility`

- Commands: `1`
- Command IDs:
  - `meta.version`

## `concurrency`

- Commands: `3`
- Command IDs:
  - `boards.patch`
  - `cards.patch`
  - `topics.patch`

## `docs`

- Commands: `11`
- Command IDs:
  - `docs.archive`
  - `docs.create`
  - `docs.get`
  - `docs.list`
  - `docs.purge`
  - `docs.restore`
  - `docs.revisions.create`
  - `docs.revisions.get`
  - `docs.revisions.list`
  - `docs.trash`
  - `docs.unarchive`

## `events`

- Commands: `6`
- Command IDs:
  - `events.archive`
  - `events.create`
  - `events.list`
  - `events.restore`
  - `events.trash`
  - `events.unarchive`

## `evidence`

- Commands: `2`
- Command IDs:
  - `packets.receipts.create`
  - `packets.reviews.create`

## `health`

- Commands: `2`
- Command IDs:
  - `meta.health`
  - `meta.readyz`

## `inbox`

- Commands: `2`
- Command IDs:
  - `inbox.acknowledge`
  - `inbox.list`

## `inspection`

- Commands: `4`
- Command IDs:
  - `ref_edges.list`
  - `threads.context`
  - `threads.inspect`
  - `threads.list`

## `packets`

- Commands: `2`
- Command IDs:
  - `packets.receipts.create`
  - `packets.reviews.create`

## `readiness`

- Commands: `1`
- Command IDs:
  - `meta.readyz`

## `refs`

- Commands: `1`
- Command IDs:
  - `ref_edges.list`

## `revisions`

- Commands: `3`
- Command IDs:
  - `docs.revisions.create`
  - `docs.revisions.get`
  - `docs.revisions.list`

## `threads`

- Commands: `5`
- Command IDs:
  - `threads.context`
  - `threads.inspect`
  - `threads.list`
  - `threads.timeline`
  - `threads.workspace`

## `timeline`

- Commands: `3`
- Command IDs:
  - `cards.timeline`
  - `threads.timeline`
  - `topics.timeline`

## `topics`

- Commands: `10`
- Command IDs:
  - `topics.archive`
  - `topics.create`
  - `topics.get`
  - `topics.list`
  - `topics.patch`
  - `topics.restore`
  - `topics.timeline`
  - `topics.trash`
  - `topics.unarchive`
  - `topics.workspace`

## `workspace`

- Commands: `3`
- Command IDs:
  - `boards.workspace`
  - `threads.workspace`
  - `topics.workspace`

## `write`

- Commands: `40`
- Command IDs:
  - `artifacts.archive`
  - `artifacts.create`
  - `artifacts.purge`
  - `artifacts.restore`
  - `artifacts.trash`
  - `artifacts.unarchive`
  - `boards.archive`
  - `boards.cards.create`
  - `boards.create`
  - `boards.patch`
  - `boards.purge`
  - `boards.restore`
  - `boards.trash`
  - `boards.unarchive`
  - `cards.archive`
  - `cards.create`
  - `cards.move`
  - `cards.patch`
  - `cards.purge`
  - `cards.restore`
  - `cards.trash`
  - `docs.archive`
  - `docs.create`
  - `docs.purge`
  - `docs.restore`
  - `docs.revisions.create`
  - `docs.trash`
  - `docs.unarchive`
  - `events.archive`
  - `events.create`
  - `events.restore`
  - `events.trash`
  - `events.unarchive`
  - `inbox.acknowledge`
  - `topics.archive`
  - `topics.create`
  - `topics.patch`
  - `topics.restore`
  - `topics.trash`
  - `topics.unarchive`

