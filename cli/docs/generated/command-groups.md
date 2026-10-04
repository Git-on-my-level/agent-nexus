# Agent Nexus command groups

Generated from `contracts/anx-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.6.0`
- Groups: `30`

## `topics`

- Commands: `10`
- Command IDs:
  - `topics.archive` (`topics archive`)
  - `topics.create` (`topics create`)
  - `topics.get` (`topics get`)
  - `topics.list` (`topics list`)
  - `topics.patch` (`topics patch`)
  - `topics.restore` (`topics restore`)
  - `topics.timeline` (`topics timeline`)
  - `topics.trash` (`topics trash`)
  - `topics.unarchive` (`topics unarchive`)
  - `topics.workspace` (`topics workspace`)

## `threads`

- Commands: `5`
- Command IDs:
  - `threads.context` (`threads context`)
  - `threads.inspect` (`threads inspect`)
  - `threads.list` (`threads list`)
  - `threads.timeline` (`threads timeline`)
  - `threads.workspace` (`threads workspace`)

## `actors`

- Commands: `2`
- Command IDs:
  - `actors.create` (`actors create`)
  - `actors.list` (`actors list`)

## `adapters`

- Commands: `5`
- Command IDs:
  - `adapters.declare` (`adapters declare`)
  - `adapters.delete` (`adapters delete`)
  - `adapters.list` (`adapters list`)
  - `adapters.revoke` (`adapters revoke`)
  - `adapters.token` (`adapters token`)

## `agent`

- Commands: `5`
- Command IDs:
  - `agent.inbox.answers.read` (`agent inbox answers read`)
  - `agent.inbox.asks.list` (`agent inbox asks list`)
  - `agent.notifications.dismiss` (`agent notifications dismiss`)
  - `agent.notifications.list` (`agent notifications list`)
  - `agent.notifications.read` (`agent notifications read`)

## `agents`

- Commands: `4`
- Command IDs:
  - `agents.get` (`agents get`)
  - `agents.list` (`agents list`)
  - `agents.me.get` (`agents me`)
  - `agents.stream` (`agents stream`)

## `artifacts`

- Commands: `10`
- Command IDs:
  - `artifacts.archive` (`artifacts archive`)
  - `artifacts.attachments.create` (`artifacts attachments create`)
  - `artifacts.content` (`artifacts content`)
  - `artifacts.create` (`artifacts create`)
  - `artifacts.get` (`artifacts get`)
  - `artifacts.list` (`artifacts list`)
  - `artifacts.purge` (`artifacts purge`)
  - `artifacts.restore` (`artifacts restore`)
  - `artifacts.trash` (`artifacts trash`)
  - `artifacts.unarchive` (`artifacts unarchive`)

## `auth`

- Commands: `17`
- Command IDs:
  - `auth.admins.grant` (`auth admins grant`)
  - `auth.admins.list` (`auth admins list`)
  - `auth.admins.revoke` (`auth admins revoke`)
  - `auth.audit.list` (`auth audit list`)
  - `auth.bootstrap.status` (`auth bootstrap status`)
  - `auth.invites.create` (`auth invites create`)
  - `auth.invites.list` (`auth invites list`)
  - `auth.invites.revoke` (`auth invites revoke`)
  - `auth.passkey.dev.login` (`auth passkey dev login`)
  - `auth.passkey.dev.register` (`auth passkey dev register`)
  - `auth.passkey.login.options` (`auth passkey login options`)
  - `auth.passkey.login.verify` (`auth passkey login verify`)
  - `auth.passkey.register.options` (`auth passkey register options`)
  - `auth.passkey.register.verify` (`auth passkey register verify`)
  - `auth.principals.list` (`auth principals list`)
  - `auth.principals.revoke` (`auth principals revoke`)
  - `auth.token` (`auth token`)

## `boards`

- Commands: `13`
- Command IDs:
  - `boards.archive` (`boards archive`)
  - `boards.cards.batch_add` (`boards cards create-batch`)
  - `boards.cards.get` (`boards cards get`)
  - `boards.cards.list` (`boards cards list`)
  - `boards.create` (`boards create`)
  - `boards.get` (`boards get`)
  - `boards.list` (`boards list`)
  - `boards.patch` (`boards patch`)
  - `boards.purge` (`boards purge`)
  - `boards.restore` (`boards restore`)
  - `boards.trash` (`boards trash`)
  - `boards.unarchive` (`boards unarchive`)
  - `boards.workspace` (`boards workspace`)

## `cards`

- Commands: `13`
- Command IDs:
  - `cards.archive` (`cards archive`)
  - `cards.create` (`cards create`)
  - `cards.get` (`cards get`)
  - `cards.list` (`cards list`)
  - `cards.move` (`cards move`)
  - `cards.patch` (`cards patch`)
  - `cards.purge` (`cards purge`)
  - `cards.restore` (`cards restore`)
  - `cards.revisions.create` (`cards revise`)
  - `cards.revisions.get` (`cards revision get`)
  - `cards.revisions.list` (`cards history`)
  - `cards.timeline` (`cards timeline`)
  - `cards.trash` (`cards trash`)

## `derived`

- Commands: `1`
- Command IDs:
  - `derived.rebuild` (`derived rebuild`)

## `docs`

- Commands: `19`
- Command IDs:
  - `docs.archive` (`docs archive`)
  - `docs.comments.create` (`docs comment`)
  - `docs.comments.delete` (`docs comments delete`)
  - `docs.comments.list` (`docs comments`)
  - `docs.comments.reply` (`docs comments reply`)
  - `docs.comments.update` (`docs comments edit`)
  - `docs.create` (`docs create`)
  - `docs.get` (`docs get`)
  - `docs.list` (`docs list`)
  - `docs.patch` (`docs patch`)
  - `docs.purge` (`docs purge`)
  - `docs.put` (`docs put`)
  - `docs.restore` (`docs restore`)
  - `docs.revisions.create` (`docs revise`)
  - `docs.revisions.get` (`docs revision get`)
  - `docs.revisions.list` (`docs history`)
  - `docs.search` (`docs search`)
  - `docs.trash` (`docs trash`)
  - `docs.unarchive` (`docs unarchive`)

## `events`

- Commands: `8`
- Command IDs:
  - `events.archive` (`events archive`)
  - `events.create` (`events create`)
  - `events.get` (`events get`)
  - `events.list` (`events list`)
  - `events.restore` (`events restore`)
  - `events.stream` (`events stream`)
  - `events.trash` (`events trash`)
  - `events.unarchive` (`events unarchive`)

## `host`

- Commands: `15`
- Command IDs:
  - `hosts.bridge.check_in` (`host bridge check-in`)
  - `hosts.enroll.approve` (`host enrollments approve`)
  - `hosts.enroll.complete` (`host enroll complete`)
  - `hosts.enroll.deny` (`host enrollments deny`)
  - `hosts.enroll.headless` (`host enroll headless`)
  - `hosts.enroll.pending` (`host enrollments list`)
  - `hosts.enroll.poll` (`host enroll poll`)
  - `hosts.enroll.start` (`host enroll start`)
  - `hosts.get` (`host get`)
  - `hosts.list` (`host list`)
  - `hosts.patch` (`host patch`)
  - `hosts.revoke` (`host revoke`)
  - `hosts.tokens.create` (`host tokens create`)
  - `hosts.tokens.list` (`host tokens list`)
  - `hosts.tokens.revoke` (`host tokens revoke`)

## `inbox`

- Commands: `4`
- Command IDs:
  - `inbox.get` (`inbox get`)
  - `inbox.list` (`inbox list`)
  - `inbox.respond` (`inbox respond`)
  - `inbox.stream` (`inbox stream`)

## `meta`

- Commands: `9`
- Command IDs:
  - `meta.commands.get` (`meta commands get`)
  - `meta.commands.list` (`meta commands list`)
  - `meta.concepts.get` (`meta concepts get`)
  - `meta.concepts.list` (`meta concepts list`)
  - `meta.handshake` (`meta handshake`)
  - `meta.health` (`meta health`)
  - `meta.livez` (`meta livez`)
  - `meta.readyz` (`meta readyz`)
  - `meta.version` (`meta version`)

## `ops`

- Commands: `3`
- Command IDs:
  - `ops.blob.usage.rebuild` (`ops blob usage rebuild`)
  - `ops.health` (`ops health`)
  - `ops.usage.summary` (`ops usage summary`)

## `overview`

- Commands: `2`
- Command IDs:
  - `overview.changes` (`overview changes`)
  - `overview.get` (`overview`)

## `plan`

- Commands: `2`
- Command IDs:
  - `plan.set` (`plan set`)
  - `plan.show` (`plan show`)

## `pm`

- Commands: `24`
- Command IDs:
  - `pm.actions.acknowledge` (`pm actions acknowledge`)
  - `pm.actions.get` (`pm actions get`)
  - `pm.actions.list` (`pm actions list`)
  - `pm.actions.reconcile` (`pm actions reconcile`)
  - `pm.bindings.create` (`pm bindings create`)
  - `pm.bindings.list` (`pm bindings list`)
  - `pm.context` (`pm context`)
  - `pm.conversations.create` (`pm conversations create`)
  - `pm.conversations.get` (`pm conversations get`)
  - `pm.conversations.list` (`pm conversations list`)
  - `pm.conversations.messages.create` (`pm conversations messages create`)
  - `pm.decisions.answer` (`pm decisions answer`)
  - `pm.decisions.create` (`pm decisions create`)
  - `pm.decisions.dispatch` (`pm decisions dispatch`)
  - `pm.decisions.get` (`pm decisions get`)
  - `pm.decisions.list` (`pm decisions list`)
  - `pm.turns.claim` (`pm turns claim`)
  - `pm.turns.complete` (`pm turns complete`)
  - `pm.turns.context` (`pm turns context`)
  - `pm.turns.decisions.create` (`pm turns decisions create`)
  - `pm.turns.fail` (`pm turns fail`)
  - `pm.turns.get` (`pm turns get`)
  - `pm.turns.heartbeat` (`pm turns heartbeat`)
  - `pm.turns.release` (`pm turns release`)

## `ref-edges`

- Commands: `1`
- Command IDs:
  - `ref_edges.list` (`ref-edges list`)

## `refs`

- Commands: `1`
- Command IDs:
  - `refs.resolve` (`refs resolve`)

## `report`

- Commands: `1`
- Command IDs:
  - `report.render` (`report render`)

## `runs`

- Commands: `3`
- Command IDs:
  - `runs.get` (`runs get`)
  - `runs.list` (`runs list`)
  - `runs.upsert` (`runs ingest`)

## `secret`

- Commands: `6`
- Command IDs:
  - `secrets.create` (`secret create`)
  - `secrets.delete` (`secret delete`)
  - `secrets.list` (`secret list`)
  - `secrets.reveal` (`secret get --reveal`)
  - `secrets.reveal-batch` (`secret exec`)
  - `secrets.update` (`secret update`)

## `series`

- Commands: `4`
- Command IDs:
  - `series.list` (`series list`)
  - `series.push` (`series push`)
  - `series.query` (`series query`)
  - `series.show` (`series show`)

## `sessions`

- Commands: `2`
- Command IDs:
  - `sessions.get` (`sessions get`)
  - `sessions.register` (`sessions register`)

## `usage`

- Commands: `1`
- Command IDs:
  - `usage.summary.v1` (`usage summary --api v1`)

## `work`

- Commands: `12`
- Command IDs:
  - `agents.me.presence` (`work presence`)
  - `work.capabilities` (`work capabilities`)
  - `work.create` (`work create`)
  - `work.get` (`work get`)
  - `work.list` (`work list`)
  - `work.observations.list` (`work observations list`)
  - `work.observations.submit` (`work observations submit`)
  - `work.participants.list` (`work participants list`)
  - `work.participants.register` (`work participants register`)
  - `work.patch` (`work patch`)
  - `work.refresh.get` (`work refresh get`)
  - `work.refresh.request` (`work refresh request`)

## `workspace`

- Commands: `2`
- Command IDs:
  - `workspace.dashboard.list` (`workspace dashboard list`)
  - `workspace.dashboard.set` (`workspace dashboard set`)

