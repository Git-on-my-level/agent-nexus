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

The runner uses a new user, PID and network namespace; disables further user
namespaces; drops capabilities; clears environment; binds only the immutable
executable and harmless `/dev/null` and `/dev/urandom` devices; and makes its root
filesystem read-only. It mounts no host home, credential store, Unix socket,
network filesystem, runtime libraries or `/proc`. Standard input/output are pipes.
CPU time, address space, processes, open files, file size, core dumps and wall time
are bounded. Address-space and CPU limits are per process; use a dedicated account
and select process/memory budgets together. This is not a cgroup aggregate-memory
quota or a claim of kernel-exploit resistance.

V1 permits **no filesystem scratch writes**, credential handles, host read paths,
network destinations or dependency installation inside generated code. These are
explicitly rejected capability requests. Brokered reads happen through trusted
selected-target built-ins with their own configuration and bounds. Network access
is not restricted by a prompt or by a GET convention inside arbitrary code: the
generated process has no source network at all. Scratch writes remain unsupported
until an approved runner enforces aggregate disk and inode quotas.

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
to test persistence and transitions. `TestLinuxIsolationEnforcement` compiles and
runs a harmless static C fixture to test real kernel/runtime denials when opted
in. Those two evidence classes must remain separate.
