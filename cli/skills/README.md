# ANX maintained skill sources

The CLI owns local installation and clean refresh of these bundled skills across
detected agent harnesses. Host enrollment offers `anx skills sync`; this does not
prove a live session loaded the skill, connect an existing PM endpoint, or
discover/upload project history.

## One source, two roles

- `anx-participant/SKILL.md`: lean participation, clear project matching, explicit
  task refs, source authority, privacy, provenance and meaningful status updates
- `anx-pm/SKILL.md`: additional discovery, synthesis and coordination guidance
  for an explicitly designated **existing ordinary agent**. It grants no extra
  permissions, creates no runtime, and is not an independent goal driver
- `catalog.json`: schema-v1 role, name, skill-contract version and SHA-256 over
  the exact UTF-8 `SKILL.md` bytes. The CLI embeds these same source files and
  rejects mismatched metadata. Change the skill-contract version and digest
  together whenever a skill's behavior changes
- `participant.json` and `pm.json`: agentctl schema-v1 pack manifests. The PM
  pack includes both skills. Paths resolve from the repository/archive root

Release archives preserve `cli/skills/` and contain these exact sources,
manifests, catalog and README alongside the binary. Archive checksums cover
all of them. The files are portable instructions, with no install hooks,
credentials, prompts, session records or executable code.

## ANX harness delivery

Preview and apply the participant skill to detected harnesses:

```sh
anx skills sync --dry-run
anx skills sync
anx skills status
```

The CLI uses the shared `~/.agents/skills` root for Codex and OMP, plus the
canonical user skill roots for Claude Code, Cursor, Hermes, Gemini CLI, Pi,
Continue and OpenCode. It creates a skill directory only for a detected harness.
The participant skill is installed by default. `anx skills sync --pm` also
installs and remembers the optional PM skill; PM availability does not designate
every agent on the host.

ANX refreshes only clean marker-owned files. The marker records skill version,
CLI version, source revision and content digest. Edited and unmanaged files stay
untouched. `anx skills status` and `anx doctor` report per-harness state. Automatic
refresh runs detached after a successful CLI upgrade and once per day after a
mutating command. Disable it with `anx skills sync --no-auto-sync` or
`ANX_SKILLS_AUTO_SYNC=0`.

Known old skill names (`anx`, `anx-cli-agent`, `anx-cli-onboard`) and stale ANX
content markers are reported with a digest. Adoption is plan-first:

```sh
anx skills adopt ~/.cursor/skills/anx-cli-agent
anx skills adopt ~/.cursor/skills/anx-cli-agent --expected-digest sha256:<digest>
```

The first command reports the digest without writing. The confirmed command
moves the old `SKILL.md` to a timestamped backup, then installs the managed
participant skill.

If a destination contains `.agentctl-skill.json`, ANX reports it as deferred and
leaves it to agentctl. This allows an existing Skill Hub pack to remain the
owner of that copy.

## Optional agentctl Skill Hub delivery

The inspected integration is agentctl source
`7b2644ae8124ab5affbe7134eaaf45a0ad8e7dee`, whose managed-pack schema is v1.
This is source compatibility evidence, not a minimum published release claim.
Check the installed agentctl's help/schema before configuring it.

A reviewed agentctl config bundle may select this repository as its Skill Hub:

```json
{
  "skills": {
    "source": {
      "remote": "https://github.com/Git-on-my-level/agent-nexus.git",
      "ref": "<reviewed published revision>",
      "manifest_path": "cli/skills/participant.json"
    },
    "update_policy": "auto-clean"
  }
}
```

Use `cli/skills/pm.json` only for hosts on which the user wants the richer PM
instructions available. Availability of a skill is not designation of every
agent on that host. The skill still requires explicit designation.

The placeholder above is deliberately not an executable configuration. Wait
until the selected ANX revision contains these packs. Pin a reviewed commit
for fixed content, or explicitly select a reviewed tracking branch for future
fast-forward auto-clean updates. ANX does not write the bundle, replace an
existing Skill Hub selection, fetch a repository, or run agentctl implicitly.
If a hub is already selected, incorporate these sources and entries into that
reviewed hub instead of silently replacing the user's other packs.

Then use agentctl's existing plan-first workflow:

```sh
agentctl skills update --plan
agentctl skills update
agentctl skills status
```

The manifests target agentctl's current Claude, Codex, Cursor, Hermes and OMP
delivery adapters; Codex/OMP share one root. Agentctl and ANX use independent
ownership markers. If agentctl owns a copy, ANX defers to it. Neither manager's
file digest proves actual load/activation in a native conversation. Multica and
arbitrary providers can continue to use explicit manual delivery.

Agentctl `auto-clean` updates unchanged owned content, preserving edited
(`drifted`) and unmarked (`conflict`) content. Removed pack entries are not
deleted. Follow `agentctl skills diff`, plan-first `restore` or `propose` to
resolve drift. Never use ANX manual configure inside a directory carrying
`.agentctl-skill.json`; ANX refuses the ownership collision.

## Generic manual delivery

No agentctl, supported-harness name, enrollment, server, home directory or
credentials are required. Choose an explicit skill directory for any provider:

```sh
anx skills configure --path ./anx-participant --role participant --dry-run
anx skills configure --path ./anx-participant --role participant
anx skills status --path ./anx-participant --role participant
anx skills verify --path ./anx-participant --role participant
# Only for an explicitly designated PM; load alongside the participant skill:
anx skills configure --path ./anx-pm --role pm
```

Configure writes only `SKILL.md` and its schema-v1 `.anx-skill.json` ownership
marker. The marker records role, skill name, skill version, CLI version, source
revision and content SHA-256. Existing parent aliases (including macOS `/tmp` and `/var`) are resolved
once without creating missing parents. Results expose the absolute
`requested_path` and canonical `path`; all operations use that canonical path.
The destination directory and managed files themselves must not be symlinks. Unrelated files, including shared `AGENTS.md` instructions, are never
read or changed. The marker is local maintenance evidence, not an authentication
or permission grant. Re-running configure refreshes only unchanged owned content;
there is no force flag. Automatic refresh is a separate detached sync described
above; status and verify never write.

| State | Meaning | Configure | Verify exit |
| --- | --- | --- | --- |
| `missing` | No skill or marker | Installs | 3 |
| `current` | Owned bytes and version match this CLI | No write | 0 |
| `outdated` | Owned bytes match marker, differ from this CLI | Refreshes | 7 |
| `unmanaged` | Existing skill has no ANX marker | Refuses | 4 |
| `drifted` | Owned content was edited or removed | Refuses | 4 |
| `conflict` | Invalid marker, wrong role, other manager or unsafe path | Refuses | 4 |

Status and configure `--dry-run` return an inspection envelope (exit 0 for these
states); filesystem read errors exit 1. Dry-run adds `would_change` and performs
no writes. Invalid flags/roles exit 2 before identity resolution. All machine
results use the standard CLI envelope v2 and result schema v1. Verify validates
only the named local file; PM verification does not verify its separately loaded
participant dependency.

`delivery=manual`, `harness_configuration=unknown`, and
`session_activation=unknown` remain explicit even after a successful verify.
Neither a file digest, a detected executable nor an agentctl `current` row
proves an existing native conversation loaded that version. Actual activation
needs a future provider-specific, session-scoped evidence contract. Reload/read
the skill through the provider's supported flow before using it.

## Migration and interrupted updates

`anx install skill --path <file>` and `anx debug meta skill participant|pm`
remain explicit unmanaged exports. Their optional writes retain the legacy
semantics; `install skill --force` is a deliberate replacement of that exact
file. They never create ownership markers or opt old installs into refresh.
The old `anx-opinionated-onboarding` name is now `anx-participant`; there is no
implicit deletion or relocation. Review custom instructions before explicitly
adopting a recognized legacy copy. Do not leave both old and new skills enabled
without reviewing duplicate instructions.

Manual updates serialize ANX writers with `.anx-skill.lock`, validate bounded
regular files and reject symlinks in the canonical destination or managed files. Do not edit a skill concurrently with
configure. Each file is staged and replaced separately (rename atomicity is platform
dependent). The skill and marker are two writes: interruption can leave `unmanaged` or `drifted` state, which fails closed
rather than guessing ownership. Inspect and preserve any custom content, then
export/configure in a new directory. A stale lock needs manual review before
removal. ANX does not auto-delete locks or user files.

## Verification

```sh
cd cli
go test ./skills ./internal/app
go test -race ./internal/app -run TestManagedSkills
go run ./cmd/anx-docs-gen
```

Tests use isolated temporary directories, including clean updates, idempotence,
legacy/edited content preservation, wrong-manager and invalid-manifest refusal,
symlinks, oversized files, locks, no-home/offline usage, harness detection,
dry-run, adoption backups, auto-refresh scheduling and agentctl coexistence.
Real harness activation, live-user configuration and end-to-end native PM
connection remain later qualification gates.
