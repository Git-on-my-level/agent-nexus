# Observation lane qualification

Local implementation checks on 2026-09-08:

- Focused package tests were written before implementation and failed on missing
  APIs, then passed after implementation. Tests cover workspace/connection scope,
  replay/fresh-read identity, preserved native states, partial pagination, rate
  limits, sanitized errors, redirect/private-network denial, fixed SSH arguments,
  refresh coalescing/concurrency/backoff/restart, durable claim/finish callbacks,
  strict operator configuration, saved investigation versions and JIT lifecycle.
- `go test -race ./internal/observation` passed.
- `go test ./...` and `go vet ./...` passed in core with the observation lane.
- `go build ./cmd/anx-observe` passed.
- `TestLinuxIsolationEnforcement` is opt-in and was **not run** on this macOS host.
  The host has no Bubblewrap/prlimit or available Docker daemon. The unsupported
  platform test confirms fail-closed behavior, not Linux isolation capability.

Real read-only source checks using the compiled operator runner at approximately
00:54 UTC, 2026-09-08:

- GitHub: selected [Agent Nexus PR 223](https://github.com/Git-on-my-level/agent-nexus/pull/223)
  through the HTTPS built-in, without a token. Returned native state `open`, source
  revision timestamp `2026-09-08T00:25:12Z`, complete requested endpoint coverage,
  four requests, issue evidence and eleven check-run references. This proves the
  selected PR/check reader, not an independently issue-owned work item, artifact
  downloads, serving deployment or product acceptance.
- Multica: selected existing coordination work through an approved installed CLI
  profile. Returned `in_progress`, three commands and complete requested endpoint
  coverage. That item had no linked runs/PRs, so this is **not** proof of the
  linked-run/handoff acceptance scenario. Default profile was unconfigured and a
  second profile was expired; the pre-existing Tailnet profile succeeded. No
  tokens were extracted/copied and no source writes occurred. Private host,
  workspace identifiers, titles and raw source bodies are omitted here.

Remaining qualification/integration gaps:

- Core server must load approved registrations and own the Runtime lifecycle,
  service authorization and canonical lease callbacks. The package provides that
  callable runtime; no existing server/store files were edited in this lane.
- Real approved SSH repository/second-machine evidence was unavailable to this
  worker. Fixed-command and output-parsing tests use harmless local fixtures.
- Real Linux isolation tests, model-generated artifact build, real-source canary
  and production activation remain unperformed. macOS execution is denied.
- Saved investigation specifications are durable, but an enforced read-only model
  investigation/generation bridge is not configured. A prompt-only bridge is not
  accepted as a substitute.
- Direct Multica HTTPS was fixture-tested; the real account used the existing CLI
  profile without credential extraction. CLI auth/network policy remains the
  responsibility of that approved installation.
- No source writes, production mutations, deployment or service changes were made.
  The single PR and cross-lane end-to-end product acceptance belong to the coordinator.
