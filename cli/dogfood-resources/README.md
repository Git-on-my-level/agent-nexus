# CLI dogfood resources (local dev)

This directory holds **machine-local artifacts** produced by `make serve` when dev fixture identities are seeded (`ANX_DEV_SEED_IDENTITIES=1`, the default).

Fixture seed + bootstrap vs invites: `cli/docs/runbook.md` (**Local `make serve` (fixture seed)**). `anx secret` CLI notes: `cli/README.md`.

## Invite tokens

After the seeded **human operator** completes bootstrap via passkey dev registration, the seed script issues a few **agent** invites through the normal `POST /auth/invites` API and writes them to:

- `invites.generated.json` (gitignored)

Each `make serve` run **removes** `*.generated.json` here before seeding, then repopulates when identity seeding succeeds.

Agent identity now comes from an enrolled host. Use `anx host enroll --token <headless-token>` for unattended setup, then select a derived agent with `anx --as <name>`. The invite fixtures in this directory are retained for the older dogfood seed data and are not host enrollment tokens.
