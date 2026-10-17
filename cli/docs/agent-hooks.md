# Optional context hooks and Git progress

Everything here is off until enabled. The bridge is optional: hooks read core
directly, and Git capture works with post-commit or manual sync. Background drain,
host-service consolidation and file transport belong to subsequent changes.

## Context

Choose workspace and agent using ordinary CLI preferences. Preview first:

```sh
anx --workspace work --as worker integrations enable context.inject --harness codex --scope project --dry-run
anx --workspace work --as worker integrations enable context.inject --harness codex --scope project
anx integrations status context.inject --harness codex --scope project
```

The supported adapters are `generic`, `claude-code`, `codex`, `cursor`.
Claude supports user/project/local settings; others user/project. No harness is
installed automatically. Existing settings and unrelated handlers survive enable,
upgrade and disable; a one-time `.anx-backup` preserves the original settings.
Codex still requires its own hook trust review. Claude/Codex use SessionStart,
including compact; Cursor sessionStart is asynchronous and has no guaranteed
post-compaction hook. Keep `anx context <card>` as the manual path.

Card selection is explicit `--card-id` or payload, then a session binding, an
installation default (`enable --card-id`), then the longest directory rule.
Workspace/principal changes cannot reuse a binding. No matching card means no
injection. An inaccessible explicit card does not fall back to another target.

```sh
anx --workspace work --as worker integrations bind --harness codex --session-id local-session --card-id card:work
anx --workspace work --as worker integrations map ./project --card-id card:work
```

The binding stores a provider-scoped hash, not the raw session ID. These commands
never create participation, assignments or presence. Hooks do not read transcripts,
prompts, compaction summaries or tool output.

For a harness without an adapter, enable `generic`, then call `anx hook
session.start` or `anx hook context.compacted` with one JSON object on stdin:

```json
{
  "schema_version": 1,
  "cwd": "/absolute/project",
  "session_id": "local-session",
  "card_ref": "card:work"
}
```

Only schema_version and absolute cwd are required. Generic output contains
schema_version, context, nullable card_ref/as_of, stale. Native adapters emit
only their context-injection protocol. Hooks fail open within 1.5 seconds, with
sanitized stderr diagnostics; no feature enabled means no auth/network work.
Maximum input is 64 KiB, output context 6 KiB. Reads compose the existing scoped work, ten-observation page and plan/promise
GETs concurrently under one budget. Unused refresh status is not requested. Missing newer context is
visible as an empty result, never fabricated task progress.

Offline cache reuse is off. `enable --allow-stale` allows a marked owner-only
cache for at most five minutes, only while the local credential is unexpired.
A denied/missing target clears the cache. Offline revocation cannot be checked;
use the default for sensitive work. Disable with the original harness/scope:

```sh
anx integrations disable context.inject --harness codex --scope project
```

## Git notes and promises

Start with a repository containing at least one commit. One enrolled host/agent
is elected explicitly; no initial history is uploaded:

```sh
anx --workspace work --as worker git enable --writer --install-hook --dry-run
anx --workspace work --as worker git enable --writer --install-hook
anx git binding export > capture-binding.json
```

Export is an ordinary envelope; `--binding capture-binding.json` accepts its result
or bare binding JSON. Distribute it to other clones without credentials. Those
hosts report `not_capture_writer` with the elected host/actor and never capture.
Unprovisioned clones cannot discover one another; this is an operator election,
not a server lease. Handoff requires disabling/reconciling the previous writer
and explicitly importing the replacement binding. No automatic failover. Linked worktrees share the common repository's writer,
FIFO and replay records, with separate frozen cursors. Register each worktree
explicitly with the same writer before capture; disabling applies to the repo.
Different clones must import the same binding, including clones on the writer host.

Optional trailers are parsed from Git's final trailer block, not prose/diffs:

```text
Anx-Card: card:work
Anx-Note: Added the capture path
Anx-Next: 2026-10-20 Review behavior
```

Anx-Card is required to publish. Note defaults to subject. Next accepts a UTC date
(end of that day), zoned RFC3339 or positive note duration such as 2d, followed by
promise text. Duration is anchored to the commit time and normalized once. The
note and promise are one atomic event. Same-writer commit order wins; other authors
retain their ordinary promises. No trailer completes a step/card or changes status.
Anx-Step/Anx-Status, duplicate ANX keys, multiline/control values and invalid Next
quarantine the capture. Unknown trailer names are ignored; inline comments are
not syntax. Limits: 64 KiB message, 4 KiB note, 256-byte card ref; promise text uses
`anx note`'s 160-rune limit and its existing warnings.

Capture never fetches. An explicit `--commit-url 'https://forge.example.test/team/repo/commit/{sha}'`
adds evidence; credentials/query/fragment are refused. Without it the SHA is plain
text, not an invented typed ref. Diffs, author emails and local paths are not posted.
Tool-after hooks are optional scan hints (`integrations enable git.capture`);
command strings are not parsed for `git commit`.

```sh
anx git sync
anx git status
anx git sync --since <commit> --to <commit>
anx git disable
```

The installer refuses an occupied post-commit hook and prints a chaining command;
it never changes core.hooksPath. Disable removes only the exact generated hook.
Manual sync works even without a Git hook. Scans freeze HEAD and collect at most
100 positions per invocation, then publish oldest first. Repeat sync to continue;
maximum collected walk is 10,000 positions. `--restart-scan --since <commit>`
explicitly restarts an unfinished walk when its queue is empty, retaining all
existing commit records. Shallow/divergent histories require
explicit reconciliation. Historical `--since` backfill omits promise/structured
evidence so old notes cannot reset a newer promise. Rebase/cherry-pick produce new
object IDs and can produce new notes; no patch-ID or cross-principal deduplication.

Requests freeze workspace, host, actor, key and complete body before send. Same
commit/card/writer retries reuse the event replay key. One FIFO lane prevents an
uncertain note from being overtaken by a newer promise. Network/timeout/429/5xx
back off; auth/access/not-found pauses for repair; validation/conflict fails visibly.
After inspecting a failed head entry, `git sync --skip <full-sha>` acknowledges it
without deleting the record. It is never an automatic error bypass. Failed and
unconfirmed records, including original commit-message bytes, stay under the
private CLI git configuration; capacity refuses
new intake rather than evicting old work. Git source remains available.
Automatic uncertain retries stop at 25 days before core's 30-day receipt expiry;
reconcile rather than assume exactly-once forever. Losing local state requires
explicit backfill. No production or live bridge is needed for the test suite.

## Validation and harness acceptance

Automated coverage uses disposable Git repositories, temporary enrolled identities
and a fresh local core. It checks settings round trips and native start/compact
payloads, denial/cache isolation, deadlines, frozen replay after a committed but
unrecorded response, per-author promises, linked-worktree deduplication, backfills,
paged merges, capacity and replay expiry. It requires no live bridge.

Before claiming model-visible support for an installed harness version, use an
isolated harness profile and disposable enrolled workspace: give a card a unique
checkpoint, enable its native adapter, review/trust the generated hook, and verify
the model can repeat the checkpoint at startup and after compaction. Inspect the
actual model request/context, not just successful script stdout. Cursor has only
the documented asynchronous session-start check; retain manual context fallback.
This external model smoke is separate from the deterministic adapter tests.

Protocol references: [Claude Code](https://code.claude.com/docs/en/hooks),
[Codex](https://developers.openai.com/codex/hooks),
[Cursor](https://cursor.com/docs/hooks), checked 2026-10-17. Native support depends
on a harness version implementing these contracts; unsupported versions use the
generic adapter or manual context. Enabling hooks never changes inline Codex TOML.
