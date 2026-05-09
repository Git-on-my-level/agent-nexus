---
title: "Align x-anx authoring requirements with generator validation"
agent: codex
done: true
ticket_id: "tkt_x_anx_authoring_validation_004"
---

## Triage

Priority: P2. This is important contract hygiene, but it needs a policy decision because strict enforcement currently exposes many existing metadata gaps.

## Problem

The generated x-anx authoring guide says many `x-anx-*` fields are required for every command operation, but `core/cmd/contract-gen` currently collects commands with missing fields and emits empty or omitted metadata. The contract source already has many operations missing `x-anx-agent-notes` and examples, so future contract authors have no automated guardrail for the documented rules.

## Evidence

- `core/cmd/contract-gen/main.go:398-427` skips operations without `x-anx-command-id`, then copies other `x-anx-*` fields into generated metadata without validating required presence or allowed values.
- `core/cmd/contract-gen/main.go:1256-1268` writes generated docs saying `x-anx-cli-path`, `x-anx-why`, `x-anx-input-mode`, `x-anx-streaming`, `x-anx-output-envelope`, `x-anx-error-codes`, `x-anx-concepts`, `x-anx-stability`, `x-anx-surface`, and `x-anx-agent-notes` are required.
- A read-only Python scan of `contracts/anx-openapi.yaml` found 126 missing documented required fields and 124 commands without `x-anx-examples`.
- Command used:

```bash
python3 - <<'PY'
import yaml
from pathlib import Path
p=Path('contracts/anx-openapi.yaml')
doc=yaml.safe_load(p.read_text())
required=['x-anx-command-id','x-anx-cli-path','x-anx-why','x-anx-input-mode','x-anx-streaming','x-anx-output-envelope','x-anx-error-codes','x-anx-concepts','x-anx-stability','x-anx-surface','x-anx-agent-notes']
missing=[]
examples=[]
for path,item in doc.get('paths',{}).items():
    for method,op in item.items():
        if not isinstance(op,dict) or not op.get('x-anx-command-id'):
            continue
        for k in required:
            if k not in op or op.get(k) in (None,'') or op.get(k)==[]:
                missing.append((op.get('x-anx-command-id'),method.upper(),path,k))
        if not op.get('x-anx-examples'):
            examples.append((op.get('x-anx-command-id'),method.upper(),path))
print('missing required fields', len(missing))
print('missing examples', len(examples))
PY
```

## Proposed Fix

Choose and implement one explicit policy:

- Strict policy: make `contract-gen` fail with clear path/method/command errors for missing documented required fields and invalid enum values, then backfill the current OpenAPI metadata.
- Transitional policy: add an allowlisted audit command or report for existing gaps, make new gaps fail, and update the authoring docs to distinguish required-now from recommended/backlog fields.

Keep the final policy reflected in `contracts/gen/docs/x-anx-authoring.md`.

## Validation

- Run `./scripts/contract-gen` and confirm the new validation output is deterministic and actionable.
- Run `./scripts/contract-check`.
- Add or update generator tests if the contract generator has an existing test pattern for validation failures.

## Progress

Implemented the transitional policy:

- Added `contracts/x-anx-validation-baseline.yaml` for the existing required-field gaps.
- Updated `core/cmd/contract-gen` to fail new required-field gaps, stale baseline entries, and invalid `x-anx-*` enum values with method/path/command/field details.
- Generated `contracts/gen/docs/x-anx-validation.md` and mirrored it to `cli/docs/generated/x-anx-validation.md` as the deterministic audit report.
- Updated `contracts/gen/docs/x-anx-authoring.md` and the CLI mirror to distinguish required-now enforcement from recommended/backlog examples.
- Added focused generator tests for validation failure, baselined gaps, and enum validation.

Validation run:

- `go test ./cmd/contract-gen` from `core/`
- `./scripts/contract-gen`
- `./scripts/contract-check`
