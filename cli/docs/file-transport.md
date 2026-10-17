# Scoped file transport

File transport is off by default. It is an independent job of the one optional
host bridge. It accepts notes and next promises, never completion, status changes,
arbitrary API calls or shell commands.

```sh
anx --as worker --workspace <alias> sync enable <directory> --card-id card:<handle> --dry-run
anx --as worker --workspace <alias> sync enable <directory> --card-id card:<handle>
anx sync status <directory>
anx sync disable <directory>
```

Repeat `--card-id` for the explicit allowlist (at most 100 cards). Enable resolves
canonical cards and pins workspace, enrollment and actor. It registers the files
job without installing or starting a service. Use the recommended `anx bridge
install`, then `anx bridge doctor`. Direct commands and await/batch work without it.

The directory's owner-only `.anx/` must stay outside Git. Enable refuses tracked
`.anx` paths and adds a local repository exclude; it never edits shared Git config.
The producer's authority is exactly this directory's agent and allowlist. Mount
**only `.anx/outbox` writable**, and optionally the mirror read-only, into a sandbox.
Mount `.anx/mirror` read-only as a directory and read `current/INDEX.md` and
`current/cards/` within it, so generation switches remain visible. Do not bind
only a single resolved generation if you expect live updates.
Do not mount the ANX config directory, host keys, tokens, `.anx/intake` or other
agent state. Same-UID programs with unrestricted host access are already trusted;
file permissions are not a substitute for the sandbox/mount boundary.

## Producer contract, version 1

Close and fsync a temp file, then rename it to `outbox/<uuid>.json`. Use a new,
canonical lowercase UUID for each new intent; reuse it only for identical bytes.
A producer must never edit a published file. The file contains only:

```json
{
  "schema_version": 1,
  "id": "<producer-uuid>",
  "card_ref": "card:<allowlisted-handle>",
  "text": "Progress note",
  "evidence_refs": [],
  "next": { "at": "2026-10-21", "what": "Validate the next checkpoint" }
}
```

`next` is optional. Its date/time or positive duration uses `anx note --next`
validation, normalized once from durable intake time; `what` is truncated to 160
characters. Horizon/moved-without-evidence warning codes are retained in the
private record and receipt. Evidence follows the same
validation, at most eight refs. Entries are at most 64 KiB; note text at most 4 KiB.
Unknown/duplicate JSON keys, unsupported operations and filename/ID disagreement
fail without sending. No actor, credentials, URL override or command is accepted.

The bridge atomically claims completed files into protected `.anx/intake/` before
reading them. Disappearance from `outbox` means **accepted for inspection**, not
confirmed delivery. Private state retains original bytes, intake sequence, frozen
request, identity, first attempt and retry time. It never prunes unconfirmed work.
Uncertain claims remain pending in intake/private state. Crashes recover claimed
files before new arrivals. Simultaneous arrivals are admitted in filename order;
the durable intake sequence then defines FIFO, including promises. Ordering is
per directory; use one producer binding per card when cross-producer promise
ordering matters. Manual/Git writes and other directories remain independent.

`outbox/.done/<uuid>.json` retains original bytes only after confirmation, beside
`<uuid>.receipt.json`. `.failed` retains definitive failures with a bounded reason.
Changed UUID bytes conflict and both versions survive; they never replace a frozen
request. Request key is `anx-file-v1-<directory_uuid>-<entry_uuid>`, actor-scoped by
core. Network/timeouts/429/5xx retry the same body. Identity changes never reroute
it. Uncertain requests stop before the 25-day cutoff; inspect/export and reconcile
against core before repairing retained state. Never reset an uncertain request's
identity, first attempt or key to force a resend.

Limits: 100 pending entries and 10,000 lifetime records per directory. Capacity
leaves producer files untouched and becomes visible in status. There is no silent
eviction or automatic directory-UUID replacement. Archive/export confirmed history
and reconcile pending work before intentionally creating a new directory binding.

## Read-only mirror

`.anx/STATUS.json`, `INDEX.md` and `cards/<canonical-id>.md` point into one complete,
immutable generation. `mirror/current` is switched atomically only after files
and directories are synced. Pin its target while reading several files, or check
the generation ID before and after; retry missing/changed generations. Normal
publication keeps one prior generation, and revocation removes them all. Editing the mirror never writes back.

STATUS includes `schema_version`, `generation`, `as_of`, `last_server_contact`,
`stale_after`, `stale`, `enabled`, pending/failed counts and `last_error_code`.
Cards include summary, promise, bounded plan and ten recent notes, per-card
`as_of`, `stale_after` and resource version. No invented global sequence is exposed.
A dead bridge cannot update banners: **readers must compare stale_after with their
own clock**. Refresh admits ten allowlisted cards per tick; it never lists a
workspace's cards. Large/offline bindings may be stale while other jobs continue.

403/404 removes that card's cached content and old generations. Host/actor
revocation or a changed enrollment clears generated caches but retains pending
writes. Offline revocation cannot be observed until contact resumes, and already
copied/opened files cannot be recalled. Disable clears generated content and
preserves all original/frozen work. Re-enable the same binding to resume it;
identity/allowlist changes with pending work require explicit reconciliation.

Use `anx sync status` and `anx bridge doctor` for binding, capacity, retry-cutoff,
intake safety and mirror health. Raw errors, paths, queue counts, note bodies and
credentials never enter bridge heartbeats; only the files job's readiness does.

Read cost is bounded by the explicit allowlist: at most ten card refreshes per
five-second directory tick, twenty emitted plan steps and ten thread-filtered
notes per card, with at most 60 KiB rendered card text. Card/plan reads use the
existing scoped ID lookup; event reads use the existing thread filter and limit,
never a workspace card or event scan. Directory admission is bounded by 100 pending
and 10,000 journal records; at most four notes drain per tick. Directories rotate
so one offline root cannot starve another. Mirror timestamps are cache metadata,
not an authorization lease: offline cached data can outlive an unseen revocation.
