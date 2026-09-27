# CLI dogfood resources (local dev)

This directory holds **machine-local artifacts** produced by `make serve` when dev fixture identities are seeded (`ANX_DEV_SEED_IDENTITIES=1`, the default).

Fixture seed + bootstrap vs invites: `cli/docs/runbook.md` (**Local `make serve` (fixture seed)**). `anx secret` CLI notes: `cli/README.md`.

Fixture identity seeding bootstraps a human, enrolls one local dev host, and
derives the fixture agents under that host. The seed output is maintained by
`web-ui/scripts/seed-core-from-mock.mjs`; it contains local test-session data
and is not a source of production credentials. Agent invite fixtures are no
longer used.
