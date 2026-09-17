# JIT executable isolation and lifecycle

Generated adapters are host-native executables that read a JSON `Report` from
stdin and write one bounded JSON `TransformOutput` to stdout. The generated
language is C compiled with the host `cc`: Darwin emits a thin Mach-O that may
link only `/usr/lib` and `/System/Library` (Seatbelt `process-exec` of that one
binary; no fork, so no interpreter). Linux emits a static ELF (`cc -static`)
because bubblewrap binds only the artifact at `/reader` and mounts no libc.
Generated findings are stored under `facts.generated_findings`; they cannot replace source title,
status, owner, phase or timestamps. Added evidence must reference the exact
brokered source evidence and remain `reported`. Unknown output fields, invalid
JSON, invented references and self-verified evidence are rejected.

## Enforced envelope

The production executor is Linux Bubblewrap plus `prlimit`, or Darwin
`sandbox-exec` with a deny-default Seatbelt profile. There is no shell/host-execution
fallback. The manager fails validation, canary and activation when that executor
is unavailable. Bubblewrap or Seatbelt setup failure also fails closed; merely
finding binaries is not evidence that isolation is enforced.

On Linux, the runner uses new user, PID and network namespaces; disables further
user namespaces; drops capabilities; clears environment; and binds only the
immutable executable and harmless devices. Its root (including `/tmp`) is
read-only, with no host home, credentials, Unix sockets, runtime libraries or
`/proc`. `prlimit --as=memory_bytes` enforces per-process virtual address space;
`--cpu`, `--nproc`, `--nofile`, `--fsize`, and `--core` apply the other rlimits.
These are not cgroup aggregate-memory or aggregate-process quotas.

On macOS, Seatbelt permits the artifact and required system runtime reads,
blocks network access and process forks, and clears the child environment.
**Same-uid process argv/environment remain readable**: numeric `KERN_PROCARGS2`
MIB reads bypass the profile's name-based sysctl filters. Go's runtime also needs
numeric hardware sysctl reads, which a named allowlist cannot satisfy. Named
process sysctl denials remain in place but cannot filter those numeric MIB reads.
Use a separate uid/host for the reader host, separated from secret-bearing
processes, and never carry secrets in anx-core's environment on a shared-uid host.
The runner rejects root but does not enforce deployment separation.

macOS scratch is writable only within the runner-created private scratch
directory and is deleted after execution. `ulimit -f` bounds each file, not total
disk bytes or inode count; there is no aggregate scratch quota. `memory_bytes` is
validated (16 MiB through 1 GiB) but **not enforced on Darwin**: the existing
wrapper cannot set `RLIMIT_AS`, `DATA`, or `RSS` (Invalid argument). CPU, file size,
open files and core dumps are bounded by the trusted `ulimit` wrapper; process
forking is denied instead of using user-global `RLIMIT_NPROC`. Both platforms
bound wall time and output bytes in Go, with piped standard input/output.

Manifest requests for scratch paths, credential handles, host read paths,
network destinations and dependency installation are rejected. The macOS
runner-owned scratch grant is distinct from arbitrary manifest scratch paths.
Brokered source reads happen through trusted selected-target built-ins; generated
code has no source network access. No claim of kernel-exploit resistance is made.

The [Bubblewrap implementation](https://github.com/containers/bubblewrap) defines
the required namespace, read-only mount, environment, size and nested-user-namespace
options. Unsupported versions/options fail execution rather than silently omitting
a boundary. No sandbox software is installed automatically by this package.

## Durable transitions

`NewJITManager(privateRoot, approvedPolicy)` requires an absolute nonsymlink,
private 0700 directory. It uses cross-process locking and fsynced atomic state
replacement. That directory is never visible inside generated readers.

1. `Stage(manifest, artifact)` validates the approved envelope and host executable
   format (static ELF on Linux, thin Mach-O on Darwin), rejects dynamic interpreter/dependency
   requirements outside the Darwin system libraries, and stores a
   content-addressed immutable artifact plus manifest. An adapter ID is bound to
   one exact target. Artifact and manifest digests are verified again before use.
2. `Validate(ctx, id, revision, cases)` requires both successful and rejection
   fixtures, with bounded input and output. Every fixture executes in isolation.
   A sandbox setup failure cannot count as a passing negative fixture.
3. `Canary(ctx, id, revision, builtInReader)` requires validated fixtures, then
   obtains a real selected-target source report through a trusted built-in and
   executes the transform against that snapshot. Partial source coverage cannot
   certify the canary. Canary time and source digest are persisted.
4. `Activate(id, revision)` requires a canary no older than one hour. It atomically
   installs the version and retains the former active revision as standby.
5. `Read(ctx, id, builtInReader)` revalidates the artifact and current capability
   policy each time. Policy/isolation violations suspend immediately; repeated
   ordinary failures suspend at the approved threshold. Last-good work evidence
   belongs to the canonical observation store and is retained there.
6. `Suspend` disables the active version. `Rollback` atomically restores a prior
   validated/canaried standby version after rechecking its artifact and policy.
   There is no silent model change, source repair or upstream rollback action.

Only a trusted server/operator calls these management methods. They are not an
authentication layer or public HTTP API. Core must authorize any future lifecycle
route against the registered connection/target and approved capability envelope.
Generated programs cannot call management methods or access their state files.

The coding agent supplies a built static artifact and manifest through the
existing approved generation workflow. This implementation does not execute an
unrestricted compiler/install script on the host and does not invent a coding
agent or model registry. A deployment generation bridge and real-source Linux
canary still need qualification; importing an artifact is not proof of generation.

## Operator actions

Build `anx-observe` as described in the parent README. Flags precede the action:

```sh
anx-observe --state-root /var/lib/anx/readers --policy policy.json --manifest manifest.json --artifact reader jit-stage
anx-observe --state-root /var/lib/anx/readers --policy policy.json --adapter example --revision HASH --fixtures fixtures.json jit-validate
anx-observe --state-root /var/lib/anx/readers --policy policy.json --adapter example --revision HASH --config source.json jit-canary
anx-observe --state-root /var/lib/anx/readers --policy policy.json --adapter example --revision HASH jit-activate
anx-observe --state-root /var/lib/anx/readers --policy policy.json --adapter example jit-status
anx-observe --state-root /var/lib/anx/readers --policy policy.json --adapter example jit-suspend
anx-observe --state-root /var/lib/anx/readers --policy policy.json --adapter example jit-rollback
```

Durations inside the Go-native JIT policy/manifest JSON are integer nanoseconds;
registry refresh policies use explicit `*_seconds` fields. Fixture cases have
`Name`, `Input` (base64 bytes), and `WantValid`. A rejection fixture should return
schema-invalid JSON with exit zero; process/sandbox startup failure never certifies
a negative security test. No credentials belong in fixtures or generated output.

The unit lifecycle tests use an explicitly labeled package-private fake executor
to test persistence and transitions. `TestSeatbelt…`,
`TestIsolationNegativeDenials`, `TestIsolationConformance`, and
`TestIsolatedTransformLifecycle` compile and run harmless fixtures against the
real host runner. Those two evidence classes must remain separate. From the
worktree root, run the qualification gate (unavailable isolation is a failure):

```sh
export GOCACHE=$PWD/.tmp/gocache PATH=/opt/homebrew/bin:$PATH; unset GOROOT
(cd core && ANX_OBSERVATION_ISOLATION_TEST=1 go test ./internal/observation -run 'Test(Seatbelt|IsolationNegative|IsolationConformance|IsolatedTransform)' -count=1 -v)
```

Run that gate separately on macOS and Linux; a pass on one is not proof for the
other. The by-name sysctl test does not establish numeric MIB confidentiality.
