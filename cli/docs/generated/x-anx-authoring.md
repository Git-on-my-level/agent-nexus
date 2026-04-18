# x-anx Authoring Rules

The OpenAPI contract uses `x-anx-*` extensions as the single source for CLI/help/meta/doc generation.

Required for every command operation:

- `x-anx-command-id`: stable id (for example `threads.list`)
- `x-anx-cli-path`: CLI path (for example `threads list`)
- `x-anx-why`: non-empty purpose/decision boundary
- `x-anx-input-mode`: one of `none|json-body|raw-stream|file-and-body`
- `x-anx-streaming`: streaming metadata object
- `x-anx-output-envelope`: output notes for CLI consumers
- `x-anx-error-codes`: stable semantic error code list
- `x-anx-concepts`: related concept tags
- `x-anx-stability`: one of `experimental|beta|stable`
- `x-anx-surface`: one of `canonical|projection|diagnostic|utility`
- `x-anx-agent-notes`: idempotency/retry caveats

Recommended:

- include at least one `x-anx-examples` command per operation
- keep `x-anx-command-id` immutable once published
- keep concept labels lower-case and dash-separated

Surface classification:

- `canonical`: CRUD/list/get endpoints over canonical durable resources (topics, cards, artifacts, documents, boards, events)
- `projection`: operator convenience surfaces that aggregate multiple canonical resources (workspace/context endpoints, inbox)
- `diagnostic`: read-only tooling and inspection surfaces over backing infrastructure (for example backing-thread list/get paths)
- `utility`: meta/handshake, auth bootstrap, rebuild/repair, and similar non-domain endpoints
