# Agent Nexus concepts

Generated from `contracts/anx-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.6.0`
- Concepts: `35`

## `actors`

- Commands: `2`
- Command IDs:
  - `actors.create`
  - `actors.list`

## `agents`

- Commands: `18`
- Command IDs:
  - `agent.inbox.answers.read`
  - `agent.inbox.asks.list`
  - `agent.notification-receipts.stream`
  - `agent.notifications.dismiss`
  - `agent.notifications.list`
  - `agent.notifications.read`
  - `agents.get`
  - `agents.list`
  - `agents.me.get`
  - `agents.me.presence`
  - `agents.stream`
  - `hosts.bridge.check_in`
  - `hosts.get`
  - `hosts.list`
  - `hosts.patch`
  - `runs.get`
  - `runs.list`
  - `runs.upsert`

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

- Commands: `36`
- Command IDs:
  - `actors.create`
  - `actors.list`
  - `agents.me.get`
  - `auth.access-requests.approve`
  - `auth.access-requests.deny`
  - `auth.access-requests.list`
  - `auth.access-requests.request`
  - `auth.access-requests.summary`
  - `auth.admins.grant`
  - `auth.admins.list`
  - `auth.admins.revoke`
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
  - `hosts.enroll.approve`
  - `hosts.enroll.complete`
  - `hosts.enroll.deny`
  - `hosts.enroll.headless`
  - `hosts.enroll.pending`
  - `hosts.enroll.poll`
  - `hosts.enroll.start`
  - `hosts.revoke`
  - `hosts.tokens.create`
  - `hosts.tokens.list`
  - `hosts.tokens.revoke`

## `boards`

- Commands: `15`
- Command IDs:
  - `boards.archive`
  - `boards.cards.batch_add`
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

- Commands: `64`
- Command IDs:
  - `agents.get`
  - `agents.me.presence`
  - `boards.cards.batch_add`
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
  - `overview.changes`
  - `overview.get`
  - `plan.set`
  - `plan.show`
  - `pm.actions.acknowledge`
  - `pm.actions.get`
  - `pm.actions.list`
  - `pm.actions.reconcile`
  - `pm.bindings.create`
  - `pm.bindings.list`
  - `pm.context`
  - `pm.conversations.create`
  - `pm.conversations.get`
  - `pm.conversations.list`
  - `pm.conversations.messages.create`
  - `pm.decisions.answer`
  - `pm.decisions.create`
  - `pm.decisions.dispatch`
  - `pm.decisions.get`
  - `pm.decisions.list`
  - `pm.turns.claim`
  - `pm.turns.complete`
  - `pm.turns.context`
  - `pm.turns.decisions.create`
  - `pm.turns.fail`
  - `pm.turns.get`
  - `pm.turns.heartbeat`
  - `pm.turns.release`
  - `report.preview`
  - `report.render`
  - `runs.list`
  - `runs.upsert`
  - `sessions.get`
  - `sessions.register`
  - `work.capabilities`
  - `work.create`
  - `work.get`
  - `work.list`
  - `work.observations.list`
  - `work.observations.submit`
  - `work.participants.list`
  - `work.participants.register`
  - `work.patch`
  - `work.refresh.get`
  - `work.refresh.request`
  - `workspace.dashboard.set`

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

- Commands: `21`
- Command IDs:
  - `docs.archive`
  - `docs.comments.create`
  - `docs.comments.delete`
  - `docs.comments.list`
  - `docs.comments.reply`
  - `docs.comments.update`
  - `docs.create`
  - `docs.get`
  - `docs.list`
  - `docs.patch`
  - `docs.purge`
  - `docs.put`
  - `docs.restore`
  - `docs.revisions.create`
  - `docs.revisions.get`
  - `docs.revisions.list`
  - `docs.search`
  - `docs.trash`
  - `docs.unarchive`
  - `report.preview`
  - `report.render`

## `documents`

- Commands: `12`
- Command IDs:
  - `adapters.declare`
  - `adapters.delete`
  - `adapters.list`
  - `adapters.revoke`
  - `adapters.token`
  - `overview.get`
  - `series.list`
  - `series.push`
  - `series.query`
  - `series.show`
  - `workspace.dashboard.list`
  - `workspace.dashboard.set`

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

## `evidence`

- Commands: `39`
- Command IDs:
  - `pm.actions.acknowledge`
  - `pm.actions.get`
  - `pm.actions.list`
  - `pm.actions.reconcile`
  - `pm.bindings.create`
  - `pm.bindings.list`
  - `pm.context`
  - `pm.conversations.create`
  - `pm.conversations.get`
  - `pm.conversations.list`
  - `pm.conversations.messages.create`
  - `pm.decisions.answer`
  - `pm.decisions.create`
  - `pm.decisions.dispatch`
  - `pm.decisions.get`
  - `pm.decisions.list`
  - `pm.turns.claim`
  - `pm.turns.complete`
  - `pm.turns.context`
  - `pm.turns.decisions.create`
  - `pm.turns.fail`
  - `pm.turns.get`
  - `pm.turns.heartbeat`
  - `pm.turns.release`
  - `report.preview`
  - `report.render`
  - `sessions.get`
  - `sessions.register`
  - `work.capabilities`
  - `work.create`
  - `work.get`
  - `work.list`
  - `work.observations.list`
  - `work.observations.submit`
  - `work.participants.list`
  - `work.participants.register`
  - `work.patch`
  - `work.refresh.get`
  - `work.refresh.request`

## `health`

- Commands: `4`
- Command IDs:
  - `meta.health`
  - `meta.livez`
  - `meta.readyz`
  - `ops.health`

## `home`

- Commands: `6`
- Command IDs:
  - `home.read`
  - `home.unread`
  - `overview.changes`
  - `overview.get`
  - `workspace.dashboard.list`
  - `workspace.dashboard.set`

## `hosts`

- Commands: `15`
- Command IDs:
  - `hosts.bridge.check_in`
  - `hosts.enroll.approve`
  - `hosts.enroll.complete`
  - `hosts.enroll.deny`
  - `hosts.enroll.headless`
  - `hosts.enroll.pending`
  - `hosts.enroll.poll`
  - `hosts.enroll.start`
  - `hosts.get`
  - `hosts.list`
  - `hosts.patch`
  - `hosts.revoke`
  - `hosts.tokens.create`
  - `hosts.tokens.list`
  - `hosts.tokens.revoke`

## `inbox`

- Commands: `8`
- Command IDs:
  - `agent.inbox.answers.read`
  - `agent.inbox.asks.list`
  - `agents.get`
  - `agents.list`
  - `inbox.get`
  - `inbox.respond`
  - `inbox.stream`
  - `inbox.summary`

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

## `read`

- Commands: `2`
- Command IDs:
  - `plan.show`
  - `refs.resolve`

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

## `runs`

- Commands: `6`
- Command IDs:
  - `agents.get`
  - `agents.list`
  - `agents.me.presence`
  - `runs.get`
  - `runs.list`
  - `runs.upsert`

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

- Commands: `56`
- Command IDs:
  - `agent.inbox.answers.read`
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
  - `docs.comments.create`
  - `docs.comments.delete`
  - `docs.comments.reply`
  - `docs.comments.update`
  - `docs.create`
  - `docs.patch`
  - `docs.purge`
  - `docs.put`
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
  - `plan.set`
  - `secrets.create`
  - `secrets.delete`
  - `secrets.update`
  - `topics.archive`
  - `topics.create`
  - `topics.patch`
  - `topics.restore`
  - `topics.trash`
  - `topics.unarchive`

