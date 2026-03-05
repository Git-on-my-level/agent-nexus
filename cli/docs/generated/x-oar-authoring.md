# x-oar Authoring Rules

The OpenAPI contract uses `x-oar-*` extensions as the single source for CLI/help/meta/doc generation.

Required for every command operation:

- `x-oar-command-id`: stable id (for example `threads.list`)
- `x-oar-cli-path`: CLI path (for example `threads list`)
- `x-oar-why`: non-empty purpose/decision boundary
- `x-oar-input-mode`: one of `none|json-body|raw-stream|file-and-body`
- `x-oar-streaming`: streaming metadata object
- `x-oar-output-envelope`: output notes for CLI consumers
- `x-oar-error-codes`: stable semantic error code list
- `x-oar-concepts`: related concept tags
- `x-oar-stability`: one of `experimental|beta|stable`
- `x-oar-agent-notes`: idempotency/retry caveats

Recommended:

- include at least one `x-oar-examples` command per operation
- keep `x-oar-command-id` immutable once published
- keep concept labels lower-case and dash-separated
