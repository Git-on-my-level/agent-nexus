# Scenario Harness

`oar-scenario` executes scenario manifests against a live OAR core using real CLI calls.

Goals:
- provide deterministic, CI-friendly multi-agent scenario tests
- support pluggable external LLM drivers for realistic agent behavior
- capture scenario run reports in machine-readable JSON

This package is intentionally thin: orchestration state machine + command execution + assertion checks.

## Error Expectations

Use `expect_error` on a step when a command is expected to fail in a specific way.

```json
{
  "name": "stale update should conflict",
  "args": ["docs", "update", "--document-id", "{{coordinator.document_id}}"],
  "stdin": { "if_base_revision": "{{coordinator.initial_revision_id}}", "content": "..." },
  "expect_error": {
    "code": "conflict",
    "status": 409,
    "message_contains": "updated"
  }
}
```

Notes:
- `expect_error` and `allow_failure` are mutually exclusive.
- `expect_error` must define at least one matcher (`exit_code`, `status`, `code`, `message_contains`).

## Preflight Validation

The runner validates scenario shape before executing commands and fails fast on known schema traps.
Current preflight checks include:
- duplicate/missing agent names
- missing step args
- invalid `expect_error` usage
- `events create` with `event.type=review_completed` must include required `artifact:*` refs (unless refs are dynamic templates)
