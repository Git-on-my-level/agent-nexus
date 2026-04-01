# OAR Concepts

Generated from `contracts/oar-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.2.3`
- Concepts: `46`

## `append-only`

- Commands: `1`
- Command IDs:
  - `events.create`

## `artifacts`

- Commands: `11`
- Command IDs:
  - `artifacts.archive`
  - `artifacts.content.get`
  - `artifacts.create`
  - `artifacts.get`
  - `artifacts.list`
  - `artifacts.purge`
  - `artifacts.restore`
  - `artifacts.tombstone`
  - `artifacts.unarchive`
  - `threads.context`
  - `threads.workspace`

## `audit`

- Commands: `1`
- Command IDs:
  - `auth.audit.list`

## `auth`

- Commands: `17`
- Command IDs:
  - `agents.me.get`
  - `agents.me.keys.rotate`
  - `agents.me.patch`
  - `agents.me.revoke`
  - `auth.agents.register`
  - `auth.audit.list`
  - `auth.bootstrap.status`
  - `auth.invites.create`
  - `auth.invites.list`
  - `auth.invites.revoke`
  - `auth.passkey.login.options`
  - `auth.passkey.login.verify`
  - `auth.passkey.register.options`
  - `auth.passkey.register.verify`
  - `auth.principals.list`
  - `auth.principals.revoke`
  - `auth.token`

## `boards`

- Commands: `16`
- Command IDs:
  - `boards.archive`
  - `boards.cards.add`
  - `boards.cards.list`
  - `boards.cards.move`
  - `boards.cards.remove`
  - `boards.cards.update`
  - `boards.create`
  - `boards.get`
  - `boards.list`
  - `boards.purge`
  - `boards.restore`
  - `boards.tombstone`
  - `boards.unarchive`
  - `boards.update`
  - `boards.workspace`
  - `threads.workspace`

## `commitments`

- Commands: `7`
- Command IDs:
  - `boards.workspace`
  - `commitments.create`
  - `commitments.get`
  - `commitments.list`
  - `commitments.patch`
  - `threads.context`
  - `threads.workspace`

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

- Commands: `7`
- Command IDs:
  - `boards.cards.add`
  - `boards.cards.move`
  - `boards.cards.remove`
  - `boards.cards.update`
  - `boards.create`
  - `boards.update`
  - `docs.update`

## `content`

- Commands: `1`
- Command IDs:
  - `artifacts.content.get`

## `derived-views`

- Commands: `5`
- Command IDs:
  - `derived.rebuild`
  - `inbox.get`
  - `inbox.list`
  - `inbox.stream`
  - `notifications.list`

## `docs`

- Commands: `15`
- Command IDs:
  - `boards.cards.update`
  - `boards.workspace`
  - `docs.archive`
  - `docs.create`
  - `docs.get`
  - `docs.history`
  - `docs.list`
  - `docs.purge`
  - `docs.restore`
  - `docs.revision.get`
  - `docs.tombstone`
  - `docs.unarchive`
  - `docs.update`
  - `threads.context`
  - `threads.workspace`

## `events`

- Commands: `14`
- Command IDs:
  - `events.archive`
  - `events.create`
  - `events.get`
  - `events.restore`
  - `events.stream`
  - `events.tombstone`
  - `events.unarchive`
  - `inbox.ack`
  - `notifications.dismiss`
  - `notifications.list`
  - `notifications.read`
  - `threads.context`
  - `threads.timeline`
  - `threads.workspace`

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

- Commands: `4`
- Command IDs:
  - `meta.health`
  - `meta.livez`
  - `meta.ops.health`
  - `meta.readyz`

## `identity`

- Commands: `7`
- Command IDs:
  - `actors.list`
  - `actors.register`
  - `agents.me.get`
  - `agents.me.patch`
  - `auth.agents.register`
  - `auth.principals.list`
  - `auth.principals.revoke`

## `inbox`

- Commands: `6`
- Command IDs:
  - `boards.workspace`
  - `inbox.ack`
  - `inbox.get`
  - `inbox.list`
  - `inbox.stream`
  - `threads.workspace`

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

- Commands: `24`
- Command IDs:
  - `artifacts.archive`
  - `artifacts.purge`
  - `artifacts.restore`
  - `artifacts.tombstone`
  - `artifacts.unarchive`
  - `boards.archive`
  - `boards.purge`
  - `boards.restore`
  - `boards.tombstone`
  - `boards.unarchive`
  - `docs.archive`
  - `docs.purge`
  - `docs.restore`
  - `docs.tombstone`
  - `docs.unarchive`
  - `events.archive`
  - `events.restore`
  - `events.tombstone`
  - `events.unarchive`
  - `threads.archive`
  - `threads.purge`
  - `threads.restore`
  - `threads.tombstone`
  - `threads.unarchive`

## `lineage`

- Commands: `1`
- Command IDs:
  - `docs.history`

## `liveness`

- Commands: `2`
- Command IDs:
  - `meta.health`
  - `meta.livez`

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

## `onboarding`

- Commands: `4`
- Command IDs:
  - `auth.bootstrap.status`
  - `auth.invites.create`
  - `auth.invites.list`
  - `auth.invites.revoke`

## `operations`

- Commands: `1`
- Command IDs:
  - `meta.ops.health`

## `ordering`

- Commands: `3`
- Command IDs:
  - `boards.cards.add`
  - `boards.cards.list`
  - `boards.cards.move`

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

## `planning`

- Commands: `10`
- Command IDs:
  - `boards.cards.add`
  - `boards.cards.list`
  - `boards.cards.move`
  - `boards.cards.remove`
  - `boards.cards.update`
  - `boards.create`
  - `boards.get`
  - `boards.list`
  - `boards.update`
  - `boards.workspace`

## `provenance`

- Commands: `2`
- Command IDs:
  - `commitments.patch`
  - `threads.timeline`

## `readiness`

- Commands: `2`
- Command IDs:
  - `meta.ops.health`
  - `meta.readyz`

## `receipts`

- Commands: `1`
- Command IDs:
  - `packets.receipts.create`

## `reviews`

- Commands: `1`
- Command IDs:
  - `packets.reviews.create`

## `revisions`

- Commands: `6`
- Command IDs:
  - `docs.create`
  - `docs.get`
  - `docs.history`
  - `docs.list`
  - `docs.revision.get`
  - `docs.update`

## `revocation`

- Commands: `2`
- Command IDs:
  - `agents.me.revoke`
  - `auth.principals.revoke`

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

## `summaries`

- Commands: `1`
- Command IDs:
  - `boards.list`

## `threads`

- Commands: `13`
- Command IDs:
  - `boards.workspace`
  - `threads.archive`
  - `threads.context`
  - `threads.create`
  - `threads.get`
  - `threads.list`
  - `threads.patch`
  - `threads.purge`
  - `threads.restore`
  - `threads.timeline`
  - `threads.tombstone`
  - `threads.unarchive`
  - `threads.workspace`

## `token-lifecycle`

- Commands: `1`
- Command IDs:
  - `auth.token`

## `work-orders`

- Commands: `1`
- Command IDs:
  - `packets.work-orders.create`

