package app

const gitCaptureHelp = `Opt-in Git progress capture. Everything is off until explicitly enabled.

anx git enable [repo] --writer [--install-hook] [--commit-url <https-url-with-{sha}>] [--dry-run]
anx git enable [repo] --binding <file> [--dry-run]
anx git binding export [repo]
anx git sync [repo] [--since <commit>] [--to <commit>] [--restart-scan] [--skip <failed-sha>]
anx git status [repo]
anx git disable [repo]

Use --workspace and --as to select the capture writer when enabling. Export its
binding and import it on other clones; those hosts report not_capture_writer.
No automatic election/failover. Existing post-commit hooks are never overwritten.
Anx-Card: card:<handle>, optional Anx-Note, optional Anx-Next: <date> <promise>.
Anx-Next also accepts a zoned timestamp or note duration anchored to commit time.
Step/status/completion trailers are rejected. Rewrites need explicit --since
reconciliation; backfill omits promises/structured evidence. Re-run sync to page
large frozen scans/drain offline requests. Failed/uncertain bytes are retained.
--skip explicitly acknowledges an inspected failed head entry; it never deletes it.
`
const agentHooksHelp = `Optional context and capture hooks, off by default.

anx integrations enable context.inject --harness generic|claude-code|codex|cursor [--scope project|user|local] [--card-id card:<ref>] [--allow-stale] [--dry-run]
anx integrations enable git.capture --harness <harness> [--scope <scope>] [--dry-run]
anx integrations status context.inject|git.capture [--harness <harness>] [--scope <scope>]
anx integrations disable context.inject|git.capture [--harness <harness>] [--scope <scope>] [--dry-run]
anx integrations bind --session-id <id> --card-id card:<ref> [--harness <harness>] [--dry-run]
anx integrations map <directory> --card-id card:<ref> [--dry-run]
anx hook session.start|context.compacted|tool.after [--harness <harness>] [--card-id <ref>] < payload.json
anx hook git.commit --harness git

Specify --as when enabling; --workspace selects an explicit workspace. A project
installation applies within that directory; user installs apply everywhere in
the pinned workspace. Card order: explicit argument/payload, session binding,
installation default, longest directory rule, none. No implicit presence writes.
Generic payload: {schema_version:1,cwd:<absolute>,session_id?:<id>,card_ref?:<ref>}.
Hooks fail open within 1.5s. Stale cache is off unless enabled, at most five minutes,
and cannot verify offline revocation. Claude/Codex SessionStart(compact) injects
context; Cursor sessionStart is asynchronous and has no post-compaction promise.
Codex trust review is required; inspect hooks in the harness. Unsupported versions
must use generic/manual anx context. Tool-after is a scan hint, never commit proof.
`

func init() {
	for _, v := range []string{"git", "git enable", "git sync", "git status", "git disable", "git binding export", "hook", "integrations bind", "integrations map"} {
		localHelperTopics = append(localHelperTopics, localHelperTopic{Path: v, Summary: "Opt-in agent context and Git progress integration.", Composition: "Disabled by default; scoped, replay-safe and bounded.", Examples: []string{"anx integrations enable context.inject --harness generic --as worker", "anx git enable --writer --dry-run"}})
	}
}
