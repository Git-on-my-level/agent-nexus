# OAR Concepts

Generated from `contracts/oar-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.3.0`
- Concepts: `20`

## `artifacts`

- Commands: `3`
- Command IDs:
  - `artifacts.create`
  - `artifacts.get`
  - `artifacts.list`

## `boards`

- Commands: `10`
- Command IDs:
  - `boards.cards.create`
  - `boards.cards.get`
  - `boards.cards.list`
  - `boards.create`
  - `boards.get`
  - `boards.list`
  - `boards.patch`
  - `boards.workspace`
  - `cards.create`
  - `cards.move`

## `cards`

- Commands: `12`
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

- Commands: `6`
- Command IDs:
  - `docs.create`
  - `docs.get`
  - `docs.list`
  - `docs.revisions.create`
  - `docs.revisions.get`
  - `docs.revisions.list`

## `events`

- Commands: `2`
- Command IDs:
  - `events.create`
  - `events.list`

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
  - `topics.tombstone`
  - `topics.unarchive`
  - `topics.workspace`

## `workspace`

- Commands: `3`
- Command IDs:
  - `boards.workspace`
  - `threads.workspace`
  - `topics.workspace`

## `write`

- Commands: `20`
- Command IDs:
  - `artifacts.create`
  - `boards.cards.create`
  - `boards.create`
  - `boards.patch`
  - `cards.archive`
  - `cards.create`
  - `cards.move`
  - `cards.patch`
  - `cards.purge`
  - `cards.restore`
  - `docs.create`
  - `docs.revisions.create`
  - `events.create`
  - `inbox.acknowledge`
  - `topics.archive`
  - `topics.create`
  - `topics.patch`
  - `topics.restore`
  - `topics.tombstone`
  - `topics.unarchive`

