# organization-autorunner-core

Go-first bootstrap for the Organization Autorunner core backend.

This repo currently includes:
- `docs/`: spec + HTTP contract
- `contracts/oar-schema.yaml`: shared schema
- `cmd/oar-core`: minimal HTTP server (`/health`, `/version`)
- `scripts/dev`, `scripts/lint`, `scripts/test`: local workflows

## Quickstart

```bash
./scripts/test
./scripts/dev
```
