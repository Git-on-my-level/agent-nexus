# OAR Concepts

Generated from `contracts/oar-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.2.2`
- Concepts: `38`

## `append-only`

- Commands: `1`
- Command IDs:
  - `events.create`

## `artifacts`

- Commands: `6`
- Command IDs:
  - `artifacts.content.get`
  - `artifacts.create`
  - `artifacts.get`
  - `artifacts.list`
  - `artifacts.tombstone`
  - `threads.context`

## `auth`

- Commands: `10`
- Command IDs:
  - `agents.me.get`
  - `agents.me.keys.rotate`
  - `agents.me.patch`
  - `agents.me.revoke`
  - `auth.agents.register`
  - `auth.passkey.login.options`
  - `auth.passkey.login.verify`
  - `auth.passkey.register.options`
  - `auth.passkey.register.verify`
  - `auth.token`

## `commitments`

- Commands: `5`
- Command IDs:
  - `commitments.create`
  - `commitments.get`
  - `commitments.list`
  - `commitments.patch`
  - `threads.context`

## `compatibility`

- Commands: `2`
- Command IDs:
  - `meta.handshake`
  - `meta.version`

## `concepts`

- Commands: `2`
- Command IDs:
  - `meta.concepts.get`
  - `meta.concepts.list`

## `concurrency`

- Commands: `1`
- Command IDs:
  - `docs.update`

## `content`

- Commands: `1`
- Command IDs:
  - `artifacts.content.get`

## `derived-views`

- Commands: `4`
- Command IDs:
  - `derived.rebuild`
  - `inbox.get`
  - `inbox.list`
  - `inbox.stream`

## `docs`

- Commands: `6`
- Command IDs:
  - `docs.create`
  - `docs.get`
  - `docs.history`
  - `docs.revision.get`
  - `docs.tombstone`
  - `docs.update`

## `events`

- Commands: `6`
- Command IDs:
  - `events.create`
  - `events.get`
  - `events.stream`
  - `inbox.ack`
  - `threads.context`
  - `threads.timeline`

## `evidence`

- Commands: `1`
- Command IDs:
  - `artifacts.create`

## `filtering`

- Commands: `3`
- Command IDs:
  - `artifacts.list`
  - `commitments.list`
  - `threads.list`

## `handshake`

- Commands: `1`
- Command IDs:
  - `meta.handshake`

## `health`

- Commands: `1`
- Command IDs:
  - `meta.health`

## `identity`

- Commands: `5`
- Command IDs:
  - `actors.list`
  - `actors.register`
  - `agents.me.get`
  - `agents.me.patch`
  - `auth.agents.register`

## `inbox`

- Commands: `4`
- Command IDs:
  - `inbox.ack`
  - `inbox.get`
  - `inbox.list`
  - `inbox.stream`

## `introspection`

- Commands: `2`
- Command IDs:
  - `meta.commands.get`
  - `meta.commands.list`

## `key-management`

- Commands: `1`
- Command IDs:
  - `agents.me.keys.rotate`

## `lifecycle`

- Commands: `2`
- Command IDs:
  - `artifacts.tombstone`
  - `docs.tombstone`

## `lineage`

- Commands: `1`
- Command IDs:
  - `docs.history`

## `maintenance`

- Commands: `1`
- Command IDs:
  - `derived.rebuild`

## `meta`

- Commands: `4`
- Command IDs:
  - `meta.commands.get`
  - `meta.commands.list`
  - `meta.concepts.get`
  - `meta.concepts.list`

## `packets`

- Commands: `3`
- Command IDs:
  - `packets.receipts.create`
  - `packets.reviews.create`
  - `packets.work-orders.create`

## `passkey`

- Commands: `4`
- Command IDs:
  - `auth.passkey.login.options`
  - `auth.passkey.login.verify`
  - `auth.passkey.register.options`
  - `auth.passkey.register.verify`

## `patch`

- Commands: `2`
- Command IDs:
  - `commitments.patch`
  - `threads.patch`

## `provenance`

- Commands: `2`
- Command IDs:
  - `commitments.patch`
  - `threads.timeline`

## `readiness`

- Commands: `1`
- Command IDs:
  - `meta.health`

## `receipts`

- Commands: `1`
- Command IDs:
  - `packets.receipts.create`

## `reviews`

- Commands: `1`
- Command IDs:
  - `packets.reviews.create`

## `revisions`

- Commands: `5`
- Command IDs:
  - `docs.create`
  - `docs.get`
  - `docs.history`
  - `docs.revision.get`
  - `docs.update`

## `revocation`

- Commands: `1`
- Command IDs:
  - `agents.me.revoke`

## `schema`

- Commands: `1`
- Command IDs:
  - `meta.version`

## `snapshots`

- Commands: `2`
- Command IDs:
  - `snapshots.get`
  - `threads.create`

## `streaming`

- Commands: `2`
- Command IDs:
  - `events.stream`
  - `inbox.stream`

## `threads`

- Commands: `6`
- Command IDs:
  - `threads.context`
  - `threads.create`
  - `threads.get`
  - `threads.list`
  - `threads.patch`
  - `threads.timeline`

## `token-lifecycle`

- Commands: `1`
- Command IDs:
  - `auth.token`

## `work-orders`

- Commands: `1`
- Command IDs:
  - `packets.work-orders.create`

