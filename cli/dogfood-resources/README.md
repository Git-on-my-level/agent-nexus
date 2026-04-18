# CLI dogfood resources (local dev)

This directory holds **machine-local artifacts** produced by `make serve` when dev fixture identities are seeded (`ANX_DEV_SEED_IDENTITIES=1`, the default).

Fixture seed + bootstrap vs invites: `cli/docs/runbook.md` (**Local `make serve` (fixture seed)**). `anx secret` CLI notes: `cli/README.md`.

## Invite tokens

After the seeded **human operator** completes bootstrap via passkey dev registration, the seed script issues a few **agent** invites through the normal `POST /auth/invites` API and writes them to:

- `invites.generated.json` (gitignored)

Each `make serve` run **removes** `*.generated.json` here before seeding, then repopulates when identity seeding succeeds.

### Example

```bash
INV="$(jq -r '.invites[0].token' cli/dogfood-resources/invites.generated.json)"
anx --base-url http://127.0.0.1:8000 --agent my-cli auth register \
  --username "cli.dogfood" \
  --invite-token "$INV"
```

Use distinct `--agent` profile names if you consume more than one invite. Each token is single-use.
