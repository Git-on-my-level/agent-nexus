# Agent Nexus concepts

Generated from `contracts/anx-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.6.0`
- Concepts: `30`

## `actors`

- Commands: `2`
- Command IDs:
  - `actors.create`
  - `actors.list`

## `agents`

- Commands: `9`
- Command IDs:
  - `agent.notification-receipts.stream`
  - `agent.notifications.dismiss`
  - `agent.notifications.list`
  - `agent.notifications.read`
  - `agents.me.get`
  - `agents.me.keys.rotate`
  - `agents.me.patch`
  - `agents.me.revoke`
  - `auth.agents.register`

## `artifacts`

- Commands: `10`
- Command IDs:
  - `artifacts.archive`
  - `artifacts.attachments.create`
  - `artifacts.content`
  - `artifacts.create`
  - `artifacts.get`
  - `artifacts.list`
  - `artifacts.purge`
  - `artifacts.restore`
  - `artifacts.trash`
  - `artifacts.unarchive`

## `audit`

- Commands: `1`
- Command IDs:
  - `auth.audit.list`

## `auth`

- Commands: `21`
- Command IDs:
  - `actors.create`
  - `actors.list`
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
  - `auth.passkey.dev.login`
  - `auth.passkey.dev.register`
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
  - `boards.cards.batch_add`
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

- Commands: `17`
- Command IDs:
  - `boards.cards.batch_add`
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
  - `cards.revisions.create`
  - `cards.revisions.get`
  - `cards.revisions.list`
  - `cards.timeline`
  - `cards.trash`

## `compatibility`

- Commands: `6`
- Command IDs:
  - `meta.commands.get`
  - `meta.commands.list`
  - `meta.concepts.get`
  - `meta.concepts.list`
  - `meta.handshake`
  - `meta.version`

## `concurrency`

- Commands: `4`
- Command IDs:
  - `boards.patch`
  - `cards.patch`
  - `docs.patch`
  - `topics.patch`

## `docs`

- Commands: `12`
- Command IDs:
  - `docs.archive`
  - `docs.create`
  - `docs.get`
  - `docs.list`
  - `docs.patch`
  - `docs.purge`
  - `docs.restore`
  - `docs.revisions.create`
  - `docs.revisions.get`
  - `docs.revisions.list`
  - `docs.trash`
  - `docs.unarchive`

## `events`

- Commands: `10`
- Command IDs:
  - `events.archive`
  - `events.create`
  - `events.get`
  - `events.list`
  - `events.restore`
  - `events.stream`
  - `events.trash`
  - `events.unarchive`
  - `home.read`
  - `home.unread`

## `health`

- Commands: `4`
- Command IDs:
  - `meta.health`
  - `meta.livez`
  - `meta.readyz`
  - `ops.health`

## `home`

- Commands: `2`
- Command IDs:
  - `home.read`
  - `home.unread`

## `inbox`

- Commands: `3`
- Command IDs:
  - `inbox.get`
  - `inbox.respond`
  - `inbox.stream`

## `inspection`

- Commands: `4`
- Command IDs:
  - `ref_edges.list`
  - `threads.context`
  - `threads.inspect`
  - `threads.list`

## `maintenance`

- Commands: `2`
- Command IDs:
  - `derived.rebuild`
  - `ops.blob.usage.rebuild`

## `notifications`

- Commands: `4`
- Command IDs:
  - `agent.notification-receipts.stream`
  - `agent.notifications.dismiss`
  - `agent.notifications.list`
  - `agent.notifications.read`

## `ops`

- Commands: `4`
- Command IDs:
  - `ops.blob.usage.rebuild`
  - `ops.health`
  - `ops.usage.summary`
  - `usage.summary.v1`

## `passkeys`

- Commands: `6`
- Command IDs:
  - `auth.passkey.dev.login`
  - `auth.passkey.dev.register`
  - `auth.passkey.login.options`
  - `auth.passkey.login.verify`
  - `auth.passkey.register.options`
  - `auth.passkey.register.verify`

## `projections`

- Commands: `1`
- Command IDs:
  - `derived.rebuild`

## `quotas`

- Commands: `2`
- Command IDs:
  - `ops.usage.summary`
  - `usage.summary.v1`

## `readiness`

- Commands: `1`
- Command IDs:
  - `meta.readyz`

## `refs`

- Commands: `1`
- Command IDs:
  - `ref_edges.list`

## `revisions`

- Commands: `6`
- Command IDs:
  - `cards.revisions.create`
  - `cards.revisions.get`
  - `cards.revisions.list`
  - `docs.revisions.create`
  - `docs.revisions.get`
  - `docs.revisions.list`

## `secrets`

- Commands: `6`
- Command IDs:
  - `secrets.create`
  - `secrets.delete`
  - `secrets.list`
  - `secrets.reveal`
  - `secrets.reveal-batch`
  - `secrets.update`

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

- Commands: `50`
- Command IDs:
  - `agent.notifications.dismiss`
  - `agent.notifications.read`
  - `artifacts.archive`
  - `artifacts.attachments.create`
  - `artifacts.create`
  - `artifacts.purge`
  - `artifacts.restore`
  - `artifacts.trash`
  - `artifacts.unarchive`
  - `boards.archive`
  - `boards.cards.batch_add`
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
  - `cards.revisions.create`
  - `cards.trash`
  - `docs.archive`
  - `docs.create`
  - `docs.patch`
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
  - `home.read`
  - `inbox.respond`
  - `secrets.create`
  - `secrets.delete`
  - `secrets.update`
  - `topics.archive`
  - `topics.create`
  - `topics.patch`
  - `topics.restore`
  - `topics.trash`
  - `topics.unarchive`

