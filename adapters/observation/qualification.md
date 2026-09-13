# Observation lane qualification

Historical implementation checks reported by the observation lane on 2026-09-08
(not rerun by this document update):

- Focused package tests were written before implementation and failed on missing
  APIs, then passed after implementation. Tests cover workspace/connection scope,
  replay/fresh-read identity, preserved native states, partial pagination, rate
  limits, sanitized errors, redirect/private-network denial, fixed SSH arguments,
  refresh coalescing/concurrency/backoff/restart, durable claim/finish callbacks,
  strict operator configuration, saved investigation versions and JIT lifecycle.
- `go test -race ./internal/observation` passed.
- `go test ./...` and `go vet ./...` passed in core with the observation lane.
- `go build ./cmd/anx-observe` passed.
- Linux kernel isolation was not qualified by those macOS checks. The historical
  test name was incorrect; see the real test names and commands below.

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

Historical qualification/integration gaps (current runner behavior below supersedes
the earlier platform-denial claim):

- Core server must load approved registrations and own the Runtime lifecycle,
  service authorization and canonical lease callbacks. The package provides that
  callable runtime; no existing server/store files were edited in this lane.
- Real approved SSH repository/second-machine evidence was unavailable to this
  worker. Fixed-command and output-parsing tests use harmless local fixtures.
- Real Linux isolation tests, model-generated artifact build, real-source canary
  and production activation were unperformed in that report. macOS now has a
  fail-closed Seatbelt executable runner; availability is host-qualified, not assumed.
- Saved investigation specifications are durable, but an enforced read-only model
  investigation/generation bridge is not configured. A prompt-only bridge is not
  accepted as a substitute.
- Direct Multica HTTPS was fixture-tested; the real account used the existing CLI
  profile without credential extraction. CLI auth/network policy remains the
  responsibility of that approved installation.
- No source writes, production mutations, deployment or service changes were made.
  The single PR and cross-lane end-to-end product acceptance belong to the coordinator.

## Current sandbox boundary and qualification command

The macOS runner permits writes within its private, bounded-path scratch
directory, removed after execution. File size is bounded per file by `ulimit -f`;
there is no aggregate scratch byte/inode quota. Linux keeps its root and `/tmp`
read-only. Arbitrary manifest scratch paths are rejected on both platforms.

On macOS, generated readers can read **same-uid process argv/environment** through
numeric `KERN_PROCARGS2` MIB reads. Seatbelt sysctl filters match names and cannot
filter numeric MIB reads; a named allowlist also fails Go's numeric hardware
sysctl requirements. By-name process denials remain enforced. Use a separate
uid/host for the reader host and secret-bearing processes, and never carry secrets
in anx-core's environment on a shared-uid host. Root rejection does not verify
that deployment separation.

`memory_bytes` is validated (16 MiB through 1 GiB), not enforced on Darwin:
`RLIMIT_AS`/`DATA`/`RSS` fail with Invalid argument in the existing wrapper.
On Linux, `prlimit --as=memory_bytes` enforces per-process virtual address space,
not aggregate RSS or a cgroup memory quota. Darwin applies CPU, per-file size,
open-file and core-dump rlimits and denies fork. Linux applies CPU, nproc,
open-file, file-size and core-dump rlimits. Both runners enforce wall time and
output bytes in Go. The profile has not been weakened to address these gaps.

Real tests are `TestSeatbelt…`, `TestIsolationNegativeDenials`,
`TestIsolationConformance`, and `TestIsolatedTransformLifecycle`. Run separately
on each host from the worktree root:

```sh
export GOCACHE=$PWD/.tmp/gocache PATH=/opt/homebrew/bin:$PATH; unset GOROOT
(cd core && ANX_OBSERVATION_ISOLATION_TEST=1 go test ./internal/observation -run 'Test(Seatbelt|IsolationNegative|IsolationConformance|IsolatedTransform)' -count=1 -v)
```

The opt-in gate fails when isolation is unavailable; default-suite skips do not
qualify the host. By-name process-argument denial is not proof of numeric MIB
confidentiality. See [JIT envelope details](jit.md) and
[core placement guidance](../../core/docs/unified-work.md#dogfood-placement).
