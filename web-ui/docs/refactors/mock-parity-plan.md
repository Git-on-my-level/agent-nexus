# Web UI mock / proxy parity plan (superseded)

The in-process **mock-core SvelteKit routes** are removed. Local development uses the **workspace proxy** to a real `anx-core` plus **dev fixture seeding** (`web-ui/scripts/seed-core-from-mock.mjs` and `web-ui/src/lib/devSeedData.js`).

Keep contract-driven routing (`coreRouteCatalog.js`, generated commands) as the primary allowlist; use a **small documented set** of direct proxy paths in `hooks.server.js` only when necessary.
