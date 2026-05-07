# x-anx Authoring Rules

The OpenAPI contract uses `x-anx-*` extensions as the single source for CLI/help/meta/doc generation.

Required now for every command operation:

- `x-anx-command-id`: stable id (for example `threads.list`)
- `x-anx-cli-path`: CLI path (for example `threads list`)
- `x-anx-why`: non-empty purpose/decision boundary
- `x-anx-input-mode`: HTTP/input-shape mode, one of `none|query|json-body|flags|flags-or-json|raw-stream|file-and-body|multipart-form`
- `x-anx-streaming`: streaming metadata object
- `x-anx-output-envelope`: output notes for CLI consumers
- `x-anx-error-codes`: stable semantic error code list
- `x-anx-concepts`: related concept tags
- `x-anx-stability`: one of `experimental|beta|stable`
- `x-anx-surface`: one of `canonical|projection|diagnostic|utility`
- `x-anx-agent-notes`: idempotency/retry caveats

Generator enforcement:

- invalid enum values fail immediately
- missing required-now fields fail unless listed in `contracts/x-anx-validation-baseline.yaml`
- baseline entries are temporary migration debt and must be removed when fixed

Recommended/backlog:

- include at least one `x-anx-examples` command per operation
- use `x-anx-cli-input` when the agent-facing CLI affordance differs from the HTTP body shape; supported fields are `mode`, `body_optional`, and `flags` entries with `name`, `body_path`, `required`, and `description`
- keep `x-anx-command-id` immutable once published
- keep concept labels lower-case and dash-separated
- use `contracts/gen/docs/x-anx-validation.md` to audit baseline debt and missing examples

Surface classification:

- `canonical`: CRUD/list/get endpoints over canonical durable resources (topics, cards, artifacts, documents, boards, events)
- `projection`: operator convenience surfaces that aggregate multiple canonical resources (workspace/context endpoints, inbox)
- `diagnostic`: read-only tooling and inspection surfaces over backing infrastructure (for example backing-thread list/get paths)
- `utility`: meta/handshake, auth bootstrap, rebuild/repair, and similar non-domain endpoints
