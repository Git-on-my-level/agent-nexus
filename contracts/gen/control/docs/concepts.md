# OAR Concepts

Generated from `contracts/oar-control-openapi.yaml`.

- OpenAPI version: `3.1.0`
- Contract version: `0.1.0`
- Concepts: `27`

## `access`

- Commands: `5`
- Command IDs:
  - `control.organizations.invites.create`
  - `control.organizations.invites.list`
  - `control.organizations.invites.revoke`
  - `control.organizations.memberships.list`
  - `control.organizations.memberships.update`

## `accounts`

- Commands: `1`
- Command IDs:
  - `control.accounts.passkeys.register.start`

## `backups`

- Commands: `2`
- Command IDs:
  - `control.workspaces.backups.create`
  - `control.workspaces.restore-drills.create`

## `billing`

- Commands: `2`
- Command IDs:
  - `control.organizations.create`
  - `control.organizations.update`

## `control-auth`

- Commands: `5`
- Command IDs:
  - `control.accounts.passkeys.register.finish`
  - `control.accounts.passkeys.register.start`
  - `control.accounts.sessions.finish`
  - `control.accounts.sessions.revoke-current`
  - `control.accounts.sessions.start`

## `drills`

- Commands: `1`
- Command IDs:
  - `control.workspaces.restore-drills.create`

## `grants`

- Commands: `2`
- Command IDs:
  - `control.workspaces.launch-sessions.create`
  - `control.workspaces.session-exchange.create`

## `heartbeat`

- Commands: `1`
- Command IDs:
  - `control.workspaces.heartbeat.record`

## `inventory`

- Commands: `1`
- Command IDs:
  - `control.organizations.workspace-inventory.list`

## `invites`

- Commands: `3`
- Command IDs:
  - `control.organizations.invites.create`
  - `control.organizations.invites.list`
  - `control.organizations.invites.revoke`

## `launch`

- Commands: `2`
- Command IDs:
  - `control.workspaces.launch-sessions.create`
  - `control.workspaces.session-exchange.create`

## `lifecycle`

- Commands: `7`
- Command IDs:
  - `control.organizations.update`
  - `control.provisioning.jobs.get`
  - `control.workspaces.decommission`
  - `control.workspaces.replace`
  - `control.workspaces.restore`
  - `control.workspaces.resume`
  - `control.workspaces.suspend`

## `memberships`

- Commands: `2`
- Command IDs:
  - `control.organizations.memberships.list`
  - `control.organizations.memberships.update`

## `operations`

- Commands: `2`
- Command IDs:
  - `control.organizations.workspace-inventory.list`
  - `control.workspaces.heartbeat.record`

## `organizations`

- Commands: `9`
- Command IDs:
  - `control.organizations.create`
  - `control.organizations.get`
  - `control.organizations.invites.create`
  - `control.organizations.invites.list`
  - `control.organizations.invites.revoke`
  - `control.organizations.list`
  - `control.organizations.memberships.list`
  - `control.organizations.memberships.update`
  - `control.organizations.update`

## `passkeys`

- Commands: `4`
- Command IDs:
  - `control.accounts.passkeys.register.finish`
  - `control.accounts.passkeys.register.start`
  - `control.accounts.sessions.finish`
  - `control.accounts.sessions.start`

## `plans`

- Commands: `1`
- Command IDs:
  - `control.organizations.usage-summary.get`

## `provisioning`

- Commands: `4`
- Command IDs:
  - `control.provisioning.jobs.get`
  - `control.workspaces.backups.create`
  - `control.workspaces.create`
  - `control.workspaces.upgrade.create`

## `quotas`

- Commands: `1`
- Command IDs:
  - `control.organizations.usage-summary.get`

## `registry`

- Commands: `4`
- Command IDs:
  - `control.workspaces.create`
  - `control.workspaces.get`
  - `control.workspaces.list`
  - `control.workspaces.routing-manifest.get`

## `restore`

- Commands: `3`
- Command IDs:
  - `control.workspaces.replace`
  - `control.workspaces.restore`
  - `control.workspaces.restore-drills.create`

## `routing`

- Commands: `4`
- Command IDs:
  - `control.workspaces.decommission`
  - `control.workspaces.resume`
  - `control.workspaces.routing-manifest.get`
  - `control.workspaces.suspend`

## `sessions`

- Commands: `4`
- Command IDs:
  - `control.accounts.passkeys.register.finish`
  - `control.accounts.sessions.finish`
  - `control.accounts.sessions.revoke-current`
  - `control.accounts.sessions.start`

## `tenancy`

- Commands: `4`
- Command IDs:
  - `control.organizations.create`
  - `control.organizations.get`
  - `control.organizations.list`
  - `control.workspaces.list`

## `upgrades`

- Commands: `1`
- Command IDs:
  - `control.workspaces.upgrade.create`

## `usage`

- Commands: `1`
- Command IDs:
  - `control.organizations.usage-summary.get`

## `workspaces`

- Commands: `17`
- Command IDs:
  - `control.organizations.workspace-inventory.list`
  - `control.provisioning.jobs.get`
  - `control.workspaces.backups.create`
  - `control.workspaces.create`
  - `control.workspaces.decommission`
  - `control.workspaces.get`
  - `control.workspaces.heartbeat.record`
  - `control.workspaces.launch-sessions.create`
  - `control.workspaces.list`
  - `control.workspaces.replace`
  - `control.workspaces.restore`
  - `control.workspaces.restore-drills.create`
  - `control.workspaces.resume`
  - `control.workspaces.routing-manifest.get`
  - `control.workspaces.session-exchange.create`
  - `control.workspaces.suspend`
  - `control.workspaces.upgrade.create`

