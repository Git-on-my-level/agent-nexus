# OAR Command Groups

Generated from `contracts/anx-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.3.0`
- Groups: `19`

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

## `agent`

- Commands: `3`
- Command IDs:
  - `agent.notifications.dismiss` (`agent notifications dismiss`)
  - `agent.notifications.list` (`agent notifications list`)
  - `agent.notifications.read` (`agent notifications read`)

## `agents`

- Commands: `4`
- Command IDs:
  - `agents.me.get` (`agents me`)
  - `agents.me.keys.rotate` (`agents me keys rotate`)
  - `agents.me.patch` (`agents me patch`)
  - `agents.me.revoke` (`agents me revoke`)

## `artifacts`

- Commands: `9`
- Command IDs:
  - `artifacts.archive` (`artifacts archive`)
  - `artifacts.content` (`artifacts content`)
  - `artifacts.create` (`artifacts create`)
  - `artifacts.get` (`artifacts get`)
  - `artifacts.list` (`artifacts list`)
  - `artifacts.purge` (`artifacts purge`)
  - `artifacts.restore` (`artifacts restore`)
  - `artifacts.trash` (`artifacts trash`)
  - `artifacts.unarchive` (`artifacts unarchive`)

## `auth`

- Commands: `15`
- Command IDs:
  - `auth.agents.register` (`auth agents register`)
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

- Commands: `14`
- Command IDs:
  - `boards.archive` (`boards archive`)
  - `boards.cards.batch_add` (`boards cards create-batch`)
  - `boards.cards.create` (`boards cards create`)
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

- Commands: `10`
- Command IDs:
  - `cards.archive` (`cards archive`)
  - `cards.create` (`cards create`)
  - `cards.get` (`cards get`)
  - `cards.list` (`cards list`)
  - `cards.move` (`cards move`)
  - `cards.patch` (`cards patch`)
  - `cards.purge` (`cards purge`)
  - `cards.restore` (`cards restore`)
  - `cards.timeline` (`cards timeline`)
  - `cards.trash` (`cards trash`)

## `derived`

- Commands: `1`
- Command IDs:
  - `derived.rebuild` (`derived rebuild`)

## `docs`

- Commands: `11`
- Command IDs:
  - `docs.archive` (`docs archive`)
  - `docs.create` (`docs create`)
  - `docs.get` (`docs get`)
  - `docs.list` (`docs list`)
  - `docs.purge` (`docs purge`)
  - `docs.restore` (`docs restore`)
  - `docs.revisions.create` (`docs revisions create`)
  - `docs.revisions.get` (`docs revisions get`)
  - `docs.revisions.list` (`docs revisions list`)
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

## `inbox`

- Commands: `4`
- Command IDs:
  - `inbox.acknowledge` (`inbox acknowledge`)
  - `inbox.get` (`inbox get`)
  - `inbox.list` (`inbox list`)
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

## `packets`

- Commands: `2`
- Command IDs:
  - `packets.receipts.create` (`packets receipts create`)
  - `packets.reviews.create` (`packets reviews create`)

## `ref-edges`

- Commands: `1`
- Command IDs:
  - `ref_edges.list` (`ref-edges list`)

## `secret`

- Commands: `6`
- Command IDs:
  - `secrets.create` (`secret create`)
  - `secrets.delete` (`secret delete`)
  - `secrets.list` (`secret list`)
  - `secrets.reveal` (`secret get --reveal`)
  - `secrets.reveal-batch` (`secret exec`)
  - `secrets.update` (`secret update`)

## `usage`

- Commands: `1`
- Command IDs:
  - `usage.summary.v1` (`usage summary --api v1`)

