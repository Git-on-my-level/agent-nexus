# OAR Concepts

Generated from `contracts/oar-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.3.0`
- Concepts: `19`

## `artifacts`

- Commands: `1`
- Command IDs:
  - `artifacts.get`

## `boards`

- Commands: `9`
- Command IDs:
  - `boards.cards.create`
  - `boards.cards.get`
  - `boards.cards.list`
  - `boards.create`
  - `boards.get`
  - `boards.list`
  - `boards.patch`
  - `boards.workspace`
  - `cards.move`

## `cards`

- Commands: `9`
- Command IDs:
  - `boards.cards.create`
  - `boards.cards.get`
  - `boards.cards.list`
  - `cards.get`
  - `cards.list`
  - `cards.move`
  - `cards.patch`
  - `cards.purge`
  - `cards.restore`

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

- Commands: `3`
- Command IDs:
  - `packets.receipts.create`
  - `packets.reviews.create`
  - `packets.work-orders.create`

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

- Commands: `2`
- Command IDs:
  - `threads.inspect`
  - `threads.list`

## `packets`

- Commands: `3`
- Command IDs:
  - `packets.receipts.create`
  - `packets.reviews.create`
  - `packets.work-orders.create`

## `readiness`

- Commands: `1`
- Command IDs:
  - `meta.readyz`

## `revisions`

- Commands: `3`
- Command IDs:
  - `docs.revisions.create`
  - `docs.revisions.get`
  - `docs.revisions.list`

## `threads`

- Commands: `4`
- Command IDs:
  - `threads.inspect`
  - `threads.list`
  - `threads.timeline`
  - `threads.workspace`

## `timeline`

- Commands: `2`
- Command IDs:
  - `threads.timeline`
  - `topics.timeline`

## `topics`

- Commands: `6`
- Command IDs:
  - `topics.create`
  - `topics.get`
  - `topics.list`
  - `topics.patch`
  - `topics.timeline`
  - `topics.workspace`

## `workspace`

- Commands: `3`
- Command IDs:
  - `boards.workspace`
  - `threads.workspace`
  - `topics.workspace`

## `write`

- Commands: `13`
- Command IDs:
  - `boards.cards.create`
  - `boards.create`
  - `boards.patch`
  - `cards.move`
  - `cards.patch`
  - `cards.purge`
  - `cards.restore`
  - `docs.create`
  - `docs.revisions.create`
  - `events.create`
  - `inbox.acknowledge`
  - `topics.create`
  - `topics.patch`

