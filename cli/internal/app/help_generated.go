package app

import (
	"fmt"
	"sort"
	"strings"

	"agent-nexus-cli/internal/registry"
)

type runtimeHelpTopic struct {
	Path        string
	Description string
}

type localHelperFlag struct {
	Name        string
	Description string
}

type localHelperTopic struct {
	Path        string
	Summary     string
	QuickStart  string
	JSONShape   string
	Composition string
	Examples    []string
	Flags       []localHelperFlag
}

var runtimeGeneratedTopics = []runtimeHelpTopic{
	{Path: "auth", Description: "Register, inspect, and manage auth state"},
	{Path: "topics", Description: "Discuss and coordinate around a topic, project, incident, or decision"},
	{Path: "boards", Description: "Track active work with boards, columns, and cards"},
	{Path: "workspace", Description: "Summarize workspace boards and counts for first-run orientation"},
	{Path: "docs", Description: "Create and revise durable context and institutional knowledge"},
	{Path: "cards", Description: "Create, discuss, assign, move, revise, and resolve work cards"},
	{Path: "notifications", Description: "Inspect and clear durable wake notifications for the active agent"},
	{Path: "threads", Description: "Read-only backing-thread inspection (tooling and diagnostics)"},
	{Path: "events", Description: "Manage events and event streams"},
	{Path: "inbox", Description: "Operator diagnostics for human attention inbox items"},
	{Path: "artifacts", Description: "Manage artifact resources and content"},
	{Path: "actors", Description: "Diagnostic actor inventory and fixture helpers"},
	{Path: "ref-edges", Description: "Diagnostic typed-ref edge inspection"},
	{Path: "derived", Description: "Run derived-view maintenance actions"},
	{Path: "meta", Description: "Inspect generated command/concept metadata"},
}

var runtimeGeneratedPacketResources = []string{}

var localHelperTopics = []localHelperTopic{
	{
		Path:        "lifecycle verbs",
		Summary:     "Uniform lifecycle surface for archive, unarchive, trash, restore, and purge across artifacts, boards, docs, events, cards, and topics.",
		JSONShape:   "Optional `--from-file` JSON object body; `--reason`, `--actor-id` (except purge), and `--dry-run` augment or replace JSON fields.",
		Composition: "Canonical resources and verbs are listed in `internal/app/lifecycle_spec.go`. When a flag replaces a non-empty JSON field, `--json --dry-run` includes `_overrides` and `anx_cli_recovery.kind=json_flag_overlay`.",
		Examples: []string{
			"anx artifacts archive artifact:notes --reason \"obsolete\"",
			"anx boards trash board:launch --reason \"merged elsewhere\" --dry-run --json",
			"anx cards archive card:foo --from-file lifecycle.json --actor-id actor:agent-beta",
		},
		Flags: []localHelperFlag{
			{Name: "--reason <text>", Description: "Short audit string stamped on the lifecycle event."},
			{Name: "--from-file <path>", Description: "Advanced JSON body from file or stdin (`-`)."},
			{Name: "--actor-id <actor-id>", Description: "Actor id (every verb except purge); overlays JSON `actor_id`."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "topics create",
		Summary:     "Create a topic from plain flags, or from advanced JSON.",
		JSONShape:   "Either flags building `{ topic }`, or advanced JSON body `{ topic }` from stdin/--from-file.",
		Composition: "Builds the `topics.create` request. Use Topics for discussion and current context around a project, incident, decision, or recurring process.",
		Examples: []string{
			"anx topics create --title \"Launch\" --summary \"Coordinate launch work\"",
			"anx topics create --title \"Incident 42\" --summary \"Triage checkout failures\" --owner-ref actor:<actor-id>",
			"cat topic.json | anx topics create",
		},
		Flags: []localHelperFlag{
			{Name: "--title <text>", Description: "Topic title."},
			{Name: "--summary <text>", Description: "Topic summary."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--owner-ref <typed-ref>", Description: "Owner typed ref, repeatable."},
			{Name: "--document-ref <typed-ref>", Description: "Linked document typed ref, repeatable."},
			{Name: "--board-ref <typed-ref>", Description: "Linked board typed ref, repeatable."},
			{Name: "--ref <typed-ref>", Description: "Additional related typed ref, repeatable."},
			{Name: "--from-file <path>", Description: "Advanced JSON request body from file."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "topics patch",
		Summary:     "Patch a topic from scalar flags, or from advanced JSON.",
		JSONShape:   "Either flags building `{ patch, if_updated_at }`, or advanced JSON body from stdin/--from-file.",
		Composition: "Fetches the Topic to discover `updated_at` when `--if-updated-at` is omitted.",
		Examples: []string{
			"anx topics patch <topic-id> --title \"Launch plan\"",
			"anx topics patch <topic-id> --summary \"Updated coordination notes\" --if-updated-at <updated_at>",
			"cat topic-patch.json | anx topics patch <topic-id>",
		},
		Flags: []localHelperFlag{
			{Name: "<topic-id>", Description: "Topic id or unique prefix to patch."},
			{Name: "--title <text>", Description: "Topic title."},
			{Name: "--summary <text>", Description: "Topic summary."},
			{Name: "--if-updated-at <timestamp>", Description: "Optimistic concurrency token; discovered from topics get when omitted."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--from-file <path>", Description: "Advanced JSON request body from file."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "topics trash",
		Summary:     "Trash a topic with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).",
		JSONShape:   "`{ reason, actor_id?, ... }` from `--from-file`, merged with `--reason` / `--actor-id` flags.",
		Composition: "Uses the shared lifecycle parser (`lifecycle_spec.go`). Routine trashing prefers `--reason`; `--from-file` remains the advanced compatibility path.",
		Examples: []string{
			"anx topics trash topic:launch --reason \"test artifact\"",
			"cat trash.json | anx topics trash topic:launch --from-file=-",
		},
		Flags: []localHelperFlag{
			{Name: "<topic-id>", Description: "Topic id or unique prefix to trash."},
			{Name: "--reason <text>", Description: "Reason for trashing the topic."},
			{Name: "--from-file <path>", Description: "Advanced JSON request body from file or stdin (`-`)."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; overlays JSON and defaults from profile when omitted."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "topics message",
		Summary:     "Post a message to a Topic conversation without hand-authoring event JSON.",
		JSONShape:   "Builds an `events.create` body with `event.type=message_posted`, topic/thread refs, and payload text.",
		Composition: "Fetches the Topic to discover its backing thread, then writes a visible `message_posted` event to that thread.",
		Examples: []string{
			"anx topics message topic:launch --body-file message.md",
			"anx topics message topic:launch --body \"Decision context\"",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Topic ref, handle, or id to message."},
			{Name: "--thread <thread-id>", Description: "Backing thread id for thread-scoped message fallback."},
			{Name: "--thread-id <thread-id>", Description: "Backing thread id for thread-scoped message fallback."},
			{Name: "--body <text>", Description: "Message body text."},
			{Name: "--body-file <path>", Description: "Load message body text from a local file."},
			{Name: "--summary <text>", Description: "Optional short event summary."},
			{Name: "--ref <typed-ref>", Description: "Additional typed ref, repeatable."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "topics messages",
		Summary:     "List messages from a Topic conversation.",
		JSONShape:   "Fetches the Topic backing thread and returns an `events.list`-style filtered timeline slice with topic metadata.",
		Composition: "Fetches the Topic, then reads its backing thread timeline and filters to messages attached to that topic.",
		Examples: []string{
			"anx topics messages topic:launch",
			"anx topics messages topic:launch --max-events 5 --mine",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Topic ref, handle, or id whose messages should be listed."},
			{Name: "--max-events <n>", Description: "Return at most N most-recent matching messages."},
			{Name: "--mine", Description: "Filter to messages authored by the active profile actor_id."},
			{Name: "--actor-id <actor-id>", Description: "Filter to one actor id."},
			{Name: "--full-id", Description: "(debug/admin) Render full event ids in default text output."},
		},
	},
	{
		Path:        "topics reply",
		Summary:     "Reply to an existing Topic message.",
		JSONShape:   "Builds an `events.create` body like `topics message` and adds `payload.reply_to_event_id` plus an `event:launch-update` ref.",
		Composition: "Fetches the Topic and validates the target message exists on its backing thread before posting the reply.",
		Examples: []string{
			"anx topics reply topic:launch --to <message-id> --body \"Confirmed\"",
			"anx topics reply topic:launch --to <message-id> --body-file reply.md",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Topic ref, handle, or id to reply on."},
			{Name: "--thread <thread-id>", Description: "Backing thread id for thread-scoped reply fallback."},
			{Name: "--thread-id <thread-id>", Description: "Backing thread id for thread-scoped reply fallback."},
			{Name: "--to <message-id>", Description: "Message/event id, typed ref, or handle being replied to."},
			{Name: "--body <text>", Description: "Reply body text."},
			{Name: "--body-file <path>", Description: "Load reply body text from a local file."},
			{Name: "--summary <text>", Description: "Optional short event summary."},
			{Name: "--ref <typed-ref>", Description: "Additional typed ref, repeatable."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "boards create",
		Summary:     "Create an active-work Board from flags, optionally tied to a Topic.",
		JSONShape:   "Either flags building `{ board }`, or advanced JSON body `{ board }` from stdin/--from-file.",
		Composition: "Builds the `boards.create` request. Use Boards for active work tracking, ownership, columns, and Card movement.",
		Examples: []string{
			"anx boards create --topic topic:<topic-handle> --title \"Launch board\"",
			"anx boards create --title \"Launch board\" --summary \"Active launch work\" --document-ref document:<document-handle>",
			"cat board.json | anx boards create",
		},
		Flags: []localHelperFlag{
			{Name: "--title <text>", Description: "Board title."},
			{Name: "--summary <text>", Description: "Optional board summary."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--topic <topic-ref-or-handle>", Description: "Primary topic typed ref or handle."},
			{Name: "--document-ref <typed-ref>", Description: "Linked document typed ref, repeatable."},
			{Name: "--ref <typed-ref>", Description: "Pinned/related typed ref, repeatable."},
			{Name: "--from-file <path>", Description: "Advanced JSON request body from file."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "cards list",
		Summary:     "List cards across the workspace, or list one board's cards with --board.",
		JSONShape:   "Global list returns `{ cards }`; `--board` composes the board-scoped card list and returns `{ board_ref, board_handle, cards }`.",
		Composition: "Uses `cards.list` for workspace-wide reads. With `--board`, resolves the board ref/handle/id and uses the board-scoped card list while keeping the canonical CLI path `cards list`.",
		Examples: []string{
			"anx cards list",
			"anx cards list --board board:<board-handle>",
			"anx cards list --include-archived",
		},
		Flags: []localHelperFlag{
			{Name: "--board <board-ref-or-handle>", Description: "List cards on one board."},
			{Name: "--board-id <board-id>", Description: "Board id/ref/handle; equivalent to --board for scripts."},
			{Name: "--include-archived", Description: "Include archived cards in global list output."},
			{Name: "--archived-only", Description: "Show only archived cards in global list output."},
			{Name: "--include-trashed", Description: "Include trashed cards in global list output."},
			{Name: "--trashed-only", Description: "Show only trashed cards in global list output."},
		},
	},
	{
		Path:        "docs create",
		Summary:     "Create a durable document lineage, with a file-first text-doc path for agents.",
		QuickStart:  "Flags: `--topic <topic-ref-or-handle> --title <text> --body-file <path>`; use `--body <text>` for short inline content or `--from-file <path>` for advanced JSON.",
		JSONShape:   "Either flags plus `--body-file`, or advanced JSON body `{ document, content, content_type }` from stdin/--from-file.",
		Composition: "Builds the same `docs.create` request as the generated command. For ordinary text docs, prefer flags so agents can draft Markdown locally without hand-authoring JSON.",
		Examples: []string{
			"anx docs create --topic topic:<topic-handle> --title \"Runbook\" --body-file runbook.md",
			"anx docs create --topic topic:<topic-handle> --title \"Note\" --body \"Short update\"",
			"anx docs create --subject-ref topic:<topic-handle> --title \"Runbook\" --summary \"Durable context\" --body-file runbook.md",
			"cat doc-create.json | anx docs create",
		},
		Flags: []localHelperFlag{
			{Name: "--topic <topic-ref-or-handle>", Description: "Anchor the document to a topic typed ref or handle."},
			{Name: "--subject-ref <typed-ref>", Description: "Explicit document subject ref when not using --topic."},
			{Name: "--title <text>", Description: "Document title for flag-built text docs."},
			{Name: "--summary <text>", Description: "Optional document summary for list/detail headers."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--ref <typed-ref>", Description: "Additional typed ref (repeatable)."},
			{Name: "--body-file <path>", Description: "Load Markdown/text content from a local file, or stdin with `-`."},
			{Name: "--body <text>", Description: "Inline document body text (Markdown/text) when not using --body-file."},
			{Name: "--from-file <path>", Description: "Advanced JSON request body from file."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "cards create",
		Summary:     "Create a board work card from flags plus a local prose file, or from advanced JSON.",
		JSONShape:   "Either flags plus `--body`/`--body-file`, or advanced JSON body `{ board_id, card }` from stdin/--from-file.",
		Composition: "Builds the `cards.create` request. For normal agent work, draft the card summary/body locally and pass `--body-file` so the CLI can fill the stable Card envelope.",
		Examples: []string{
			"anx cards create --board board:<board-handle> --topic topic:<topic-handle> --title \"Implement login\" --body-file card.md",
			"anx cards create --board board:<board-handle> --title \"Implement login\" --body \"Inline summary\"",
			"anx cards create --board board:<board-handle> --title \"Implement login\" --body-file card.md --assignee-ref actor:<actor-handle>",
			"cat card-create.json | anx cards create",
		},
		Flags: []localHelperFlag{
			{Name: "--board <board-ref-or-handle>", Description: "Board typed ref or handle for the new work card."},
			{Name: "--title <text>", Description: "Card title."},
			{Name: "--body <text>", Description: "Inline card summary/body text."},
			{Name: "--body-file <path>", Description: "Load card summary/body text from a local file, or stdin with `-`."},
			{Name: "--topic <topic-ref-or-handle>", Description: "Related topic typed ref or handle."},
			{Name: "--column <key>", Description: "Initial board column; defaults to backlog."},
			{Name: "--assignee-ref <typed-ref>", Description: "Assignee actor ref, repeatable."},
			{Name: "--document-ref <typed-ref>", Description: "Pinned document ref for the card."},
			{Name: "--ref <typed-ref>", Description: "Additional related typed ref, repeatable."},
			{Name: "--done <text>", Description: "Definition-of-done checklist item, repeatable."},
			{Name: "--from-file <path>", Description: "Advanced JSON request body from file."},
		},
	},
	{
		Path:        "cards patch",
		Summary:     "Patch card metadata from scalar flags, or from advanced JSON.",
		JSONShape:   "Either flags building `{ patch, if_updated_at }`, or advanced JSON body from stdin/--from-file.",
		Composition: "Fetches the Card to discover `updated_at` when `--if-updated-at` is omitted. Use `cards move` for board placement changes.",
		Examples: []string{
			"anx cards patch <card-id> --title \"Implement login\"",
			"anx cards patch <card-id> --summary \"Updated scope\" --if-updated-at <updated_at>",
			"cat card-patch.json | anx cards patch <card-id>",
		},
		Flags: []localHelperFlag{
			{Name: "<card-id>", Description: "Card id or unique prefix to patch."},
			{Name: "--title <text>", Description: "Card title."},
			{Name: "--summary <text>", Description: "Card summary/body."},
			{Name: "--column-key <key>", Description: "Accepted for guidance only; use `anx cards move --column <key>` for placement."},
			{Name: "--if-updated-at <timestamp>", Description: "Optimistic concurrency token; discovered from cards get when omitted."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--from-file <path>", Description: "Advanced JSON request body from file."},
		},
	},
	{
		Path:        "cards message",
		Summary:     "Post a message to a Card conversation without hand-authoring event JSON.",
		JSONShape:   "Builds an `events.create` body with `event.type=message_posted`, card/thread/board refs, profile actor, and payload text.",
		Composition: "Fetches the Card to discover its backing thread and board, then writes a visible `message_posted` event. Use this for card status updates, implementation notes, and ordinary discussion.",
		Examples: []string{
			"anx cards message card:implement-login --body \"Implemented in 0729e75\"",
			"anx cards message card:implement-login --body-file update.md",
			"cat update.md | anx cards message card:implement-login",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Card ref, handle, or id to message."},
			{Name: "--body <text>", Description: "Message body text."},
			{Name: "--body-file <path>", Description: "Load message body text from a local file."},
			{Name: "--summary <text>", Description: "Optional short event summary."},
			{Name: "--ref <typed-ref>", Description: "Additional typed ref, repeatable."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "cards messages",
		Summary:     "List message_posted events from a Card conversation.",
		JSONShape:   "Fetches the Card backing thread and returns an `events.list`-style filtered timeline slice with card metadata.",
		Composition: "Fetches the Card, then reads its backing thread timeline and filters to ordinary messages. Use `cards timeline` when you need lifecycle events too.",
		Examples: []string{
			"anx cards messages card:implement-login",
			"anx cards messages card:implement-login --max-events 5 --mine",
			"anx cards messages card:implement-login --full-id",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Card ref, handle, or id whose messages should be listed."},
			{Name: "--max-events <n>", Description: "Return at most N most-recent matching messages."},
			{Name: "--mine", Description: "Filter to messages authored by the active profile actor_id."},
			{Name: "--actor-id <actor-id>", Description: "Filter to one actor id."},
			{Name: "--full-id", Description: "(debug/admin) Render full event ids in default text output."},
		},
	},
	{
		Path:        "cards reply",
		Summary:     "Reply to an existing Card message.",
		JSONShape:   "Builds an `events.create` body like `cards message` and adds `payload.reply_to_event_id` plus an `event:launch-update` ref.",
		Composition: "Fetches the Card and validates the target message exists on its backing thread before posting the reply.",
		Examples: []string{
			"anx cards reply card:implement-login --to <message-id> --body \"Confirmed\"",
			"anx cards reply card:implement-login --to <message-id> --body-file reply.md",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Card ref, handle, or id to reply on."},
			{Name: "--to <message-id>", Description: "Message/event id, typed ref, or handle being replied to."},
			{Name: "--body <text>", Description: "Reply body text."},
			{Name: "--body-file <path>", Description: "Load reply body text from a local file."},
			{Name: "--summary <text>", Description: "Optional short event summary."},
			{Name: "--ref <typed-ref>", Description: "Additional typed ref, repeatable."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "cards revise",
		Summary:     "Revise a card title and/or summary/body from local files without hand-authoring patch JSON.",
		JSONShape:   "`{ if_base_revision, revision: { title?, summary?, definition_of_done? }, actor_id? }`; discovers `if_base_revision` from `cards get` when omitted.",
		Composition: "Fetches the card when needed for optimistic concurrency, then sends `cards.revisions.create` with `summary` from `--body-file` and optional `title`.",
		Examples: []string{
			"anx cards revise card:implement-login --body-file card.md",
			"anx cards revise card:implement-login --title \"Updated title\" --body-file card.md",
			"cat card.md | anx cards revise card:implement-login --body-file -",
			"anx cards revise card:implement-login --from-file card-revision.json",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Card ref, handle, or id to revise."},
			{Name: "--body-file <path>", Description: "Load revised card summary/body text from a local file or stdin with `-`."},
			{Name: "--title <text>", Description: "Optional revised card title."},
			{Name: "--if-base-revision <revision-id>", Description: "Base card revision id; discovered when omitted."},
			{Name: "--from-file <path>", Description: "Advanced JSON revision request body from file."},
		},
	},
	{
		Path:        "threads message",
		Summary:     "Escape hatch: post a message directly to a backing thread.",
		JSONShape:   "Builds an `events.create` body with `event.type=message_posted`, `event.thread_id`, thread ref, profile actor, and payload text.",
		Composition: "Writes directly to a backing thread. Prefer domain commands such as `cards message`, `topics message`, or `docs message` when you are working from a Card, Topic, or Doc.",
		Examples: []string{
			"anx threads message <thread-id> --body-file note.md",
			"anx threads message <thread-id> --body \"Diagnostic note\"",
		},
		Flags: []localHelperFlag{
			{Name: "<thread-id>", Description: "Thread id, typed ref, or handle to message."},
			{Name: "--body <text>", Description: "Message body text."},
			{Name: "--body-file <path>", Description: "Load message body text from a local file."},
			{Name: "--summary <text>", Description: "Optional short event summary."},
			{Name: "--ref <typed-ref>", Description: "Additional typed ref, repeatable."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "threads reply",
		Summary:     "Escape hatch: reply to an existing message on a backing thread.",
		JSONShape:   "Builds an `events.create` body like `threads message` and adds `payload.reply_to_event_id` plus an `event:launch-update` ref.",
		Composition: "Validates the target message exists on the thread before posting the reply.",
		Examples: []string{
			"anx threads reply <thread-id> --to <message-id> --body \"Confirmed\"",
			"anx threads reply <thread-id> --to <message-id> --body-file reply.md",
		},
		Flags: []localHelperFlag{
			{Name: "<thread-id>", Description: "Thread id, typed ref, or handle to reply on."},
			{Name: "--to <message-id>", Description: "Message/event id, typed ref, or handle being replied to."},
			{Name: "--body <text>", Description: "Reply body text."},
			{Name: "--body-file <path>", Description: "Load reply body text from a local file."},
			{Name: "--summary <text>", Description: "Optional short event summary."},
			{Name: "--ref <typed-ref>", Description: "Additional typed ref, repeatable."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "cards move",
		Summary:     "Move a card to another board column using Card workflow language.",
		JSONShape:   "`{ column_key, if_board_updated_at, actor_id? }`; discovers the board concurrency token when omitted.",
		Composition: "Fetches the card and parent board when needed for optimistic concurrency, then sends `cards.move`.",
		Examples: []string{
			"anx cards move card:implement-login --column review",
			"anx cards move card:implement-login --column blocked --if-board-updated-at <updated-at>",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Card ref, handle, or id to move."},
			{Name: "--column <key>", Description: "Target board column."},
			{Name: "--if-board-updated-at <timestamp>", Description: "Board optimistic concurrency token; discovered when omitted."},
			{Name: "--from-file <path>", Description: "Advanced JSON move request body from file."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "cards assign",
		Summary:     "Replace card assignees with explicit actor refs, or clear them.",
		JSONShape:   "`{ patch: { assignee_refs }, if_updated_at, actor_id? }`; discovers `if_updated_at` from `cards get` when omitted.",
		Composition: "Builds a focused `cards.patch` request for the Card ownership field.",
		Examples: []string{
			"anx cards assign card:implement-login --assignee-ref actor:agent-alpha",
			"anx cards assign card:implement-login --clear",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Card ref, handle, or id to assign."},
			{Name: "--assignee-ref <typed-ref>", Description: "Assignee actor typed ref, repeatable."},
			{Name: "--clear", Description: "Clear all assignees."},
			{Name: "--if-updated-at <timestamp>", Description: "Card optimistic concurrency token; discovered when omitted."},
		},
	},
	{
		Path:        "cards resolve",
		Summary:     "Resolve a card into the done column with optional free-text evidence.",
		JSONShape:   "`{ column_key: \"done\", resolution, resolution_refs, if_board_updated_at, actor_id? }`; discovers the board concurrency token when omitted.",
		Composition: "With evidence from `--body`, `--body-file`, or `--resolution-ref`, posts a card message first when a body is supplied, then passes resolution refs (including the new `event:<id>` when posted) into `cards.move`. `--reason` stamps the lifecycle audit only.",
		Examples: []string{
			"anx cards resolve card:implement-login --reason \"ok\" --body \"Validated in staging\"",
			"anx cards resolve card:implement-login --resolution-ref event:<event-id>",
			"anx cards resolve card:implement-login --column done --body-file evidence.md",
			"cat note.md | anx cards resolve card:implement-login --body-file -",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Card ref, handle, or id to resolve."},
			{Name: "--column <key>", Description: "Target board column, default done."},
			{Name: "--reason <text>", Description: "Short audit string stamped on the resolve request (not posted to the backing thread)."},
			{Name: "--resolution-ref <typed-ref>", Description: "Evidence event/artifact typed ref, repeatable."},
			{Name: "--body <text>", Description: "Post inline evidence to the card thread before resolving."},
			{Name: "--body-file <path>", Description: "Load evidence text from a file before resolving."},
			{Name: "--summary <text>", Description: "Optional short evidence event summary."},
			{Name: "--resolution <value>", Description: "Resolution value, default done."},
			{Name: "--if-board-updated-at <timestamp>", Description: "Board optimistic concurrency token; discovered when omitted."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--from-file <path>", Description: "Advanced JSON move request body from file."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "cards reopen",
		Summary:     "Move a resolved card back into active workflow.",
		JSONShape:   "`{ column_key, if_board_updated_at, actor_id? }`; discovers the board concurrency token when omitted.",
		Composition: "Builds a focused `cards.move` request. The default reopened column is `ready`.",
		Examples: []string{
			"anx cards reopen card:implement-login",
			"anx cards reopen card:implement-login --column backlog",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Card ref, handle, or id to reopen."},
			{Name: "--column <key>", Description: "Target reopened column; defaults to ready."},
			{Name: "--if-board-updated-at <timestamp>", Description: "Board optimistic concurrency token; discovered when omitted."},
		},
	},
	{
		Path:        "cards trash",
		Summary:     "Trash a card with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).",
		JSONShape:   "`{ reason, actor_id?, ... }` from `--from-file`, merged with `--reason` / `--actor-id` flags.",
		Composition: "Uses the shared lifecycle parser (`lifecycle_spec.go`). Routine trashing prefers `--reason`; `--from-file` remains the advanced compatibility path.",
		Examples: []string{
			"anx cards trash card:implement-login --reason \"duplicate\"",
			"cat trash.json | anx cards trash card:implement-login --from-file=-",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Card ref, handle, or id to trash."},
			{Name: "--reason <text>", Description: "Reason for trashing the card."},
			{Name: "--from-file <path>", Description: "Advanced JSON request body from file or stdin (`-`)."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; overlays JSON and defaults from profile when omitted."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "events list",
		Summary:     "Compose backing-thread timeline reads with client-side thread/type/actor filters and preview summaries.",
		JSONShape:   "`thread_id`, `thread_ids`, `events`, `total_events`, `returned_events`",
		Composition: "Fetches one or more backing-thread timelines locally, then filters and summarizes the events without changing contracts or core behavior. Use it as a diagnostic read; prefer `topics workspace` and card/board reads for normal coordination.",
		Examples: []string{
			"anx events list --thread-id <thread-id> --type message_posted --mine --full-id",
			"anx events list --thread-id <thread-id> --max-events 10",
		},
		Flags: []localHelperFlag{
			{Name: "--thread-id <thread-id>", Description: "Thread id to inspect (repeatable)."},
			{Name: "--type <event-type>", Description: "Repeatable event type filter."},
			{Name: "--types <csv>", Description: "Comma-separated event types."},
			{Name: "--actor-id <actor-id>", Description: "Filter to one actor id."},
			{Name: "--mine", Description: "Resolve to the active profile actor_id."},
			{Name: "--max-events <n>", Description: "Keep the most recent matching events."},
			{Name: "--max <n>", Description: "Alias for --max-events."},
			{Name: "--full-id", Description: "(debug/admin) Render full event ids in default text output (non-JSON)."},
			{Name: "--include-archived", Description: "Include archived events in results."},
			{Name: "--archived-only", Description: "Show only archived events."},
			{Name: "--include-trashed", Description: "Include trashed events in results."},
			{Name: "--trashed-only", Description: "Show only trashed events."},
		},
	},
	{
		Path:        "events validate",
		Summary:     "Validate an `events create` payload locally from stdin or `--from-file` without sending it.",
		JSONShape:   "`command`, `command_id`, `path_params`, `query`, `body`, `valid`",
		Composition: "Parses the same JSON body accepted by `events create`, runs local validation rules, and returns a validation preview envelope without contacting core.",
		Examples: []string{
			`cat event.json | anx events validate`,
			`anx events validate --from-file event.json`,
		},
		Flags: []localHelperFlag{
			{Name: "--from-file <path>", Description: "Load the request body from a JSON file instead of stdin."},
		},
	},
	{
		Path:        "events explain",
		Summary:     "Explain known event-type conventions, required refs, and validation hints, including when `message_posted` targets a backing-thread message stream.",
		JSONShape:   "`event_type`, `known`, `required_refs`, `payload_requirements`, `examples`, `hint`",
		Composition: "Formats the embedded event reference and validation guidance into a plain-text reference without sending a request. Use it to confirm when `message_posted` is required for a visible backing-thread message in the web UI Messages tab.",
		Examples: []string{
			`anx events explain`,
			`anx events explain message_posted`,
		},
		Flags: []localHelperFlag{
			{Name: "<event-type>", Description: "Optional event type to focus on; omit it to list known event types."},
		},
	},
	{
		Path:        "artifacts create",
		Summary:     "Create an artifact; use --file/--ref for attachment uploads or JSON for advanced artifact bodies.",
		JSONShape:   "With --file, posts multipart attachment data to `artifacts.attachments.create`; without --file, sends advanced JSON `{ artifact, content_type, content }` to `artifacts.create`.",
		Composition: "Routes the common file attachment path through the multipart attachment endpoint while preserving the contract-level JSON create path for text, structured, or binary artifact bodies.",
		Examples: []string{
			`anx artifacts create --file ./photo.jpg --ref topic:launch`,
			`anx artifacts create --file ./evidence.pdf --ref card:launch-evidence --summary "Evidence"`,
			`cat artifact.json | anx artifacts create`,
		},
		Flags: []localHelperFlag{
			{Name: "--file <path>", Description: "Upload a local file as kind=attachment via multipart form."},
			{Name: "--ref <typed-ref>", Description: "Typed ref to attach to, repeatable."},
			{Name: "--refs <json>", Description: "Compatibility form: JSON array of typed refs."},
			{Name: "--summary <text>", Description: "Optional attachment summary."},
			{Name: "--artifact <json>", Description: "Optional JSON object merged into attachment metadata; refs and kind are ignored by the server."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--from-file <path>", Description: "Advanced JSON artifact create body from file; cannot be combined with --file."},
		},
	},
	{
		Path:        "artifacts attachments create",
		Summary:     "Upload a local file as an attachment artifact via multipart form.",
		JSONShape:   "Multipart form fields: `refs`, `file`, optional `summary`, `artifact`, and `actor_id`.",
		Composition: "Posts to `POST /artifacts/attachments`, which stores the file bytes and creates kind=attachment artifact metadata.",
		Examples: []string{
			`anx artifacts attachments create --file ./notes.md --ref thread:<thread-id>`,
			`anx artifacts attachments create --file ./photo.jpg --refs '["topic:launch"]'`,
		},
		Flags: []localHelperFlag{
			{Name: "--file <path>", Description: "Path to the file to upload."},
			{Name: "--ref <typed-ref>", Description: "Typed ref to attach to, repeatable."},
			{Name: "--refs <json>", Description: "Compatibility form: JSON array of typed refs."},
			{Name: "--summary <text>", Description: "Optional attachment summary."},
			{Name: "--artifact <json>", Description: "Optional JSON object merged into attachment metadata; refs and kind are ignored by the server."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
		},
	},
	{
		Path:        "artifacts inspect",
		Summary:     "Fetch artifact metadata and resolved content in one command for operator inspection.",
		JSONShape:   "`artifact`, `content`, `content_headers`, `content_text`, `content_base64`",
		Composition: "Loads artifact metadata, then fetches content with `artifacts content` using the resolved artifact id.",
		Examples: []string{
			`anx artifacts inspect artifact:notes`,
			`anx artifacts inspect <artifact-ref-or-alias>`,
		},
		Flags: []localHelperFlag{
			{Name: "--artifact-id <ref>", Description: "Artifact ref or handle to inspect."},
		},
	},
	{
		Path:        "threads inspect",
		Summary:     "Diagnostic backing-thread bundle: compose one view from read-only thread data and related `inbox list` items.",
		JSONShape:   "`thread`, `context`, `collaboration`, `inbox`",
		Composition: "Resolves one thread by id or discovery filters, loads read-only thread projections, then filters inbox items client-side by `thread_id`. Prefer `topics workspace` for primary operator coordination when you have a topic id.",
		Examples: []string{
			"anx threads inspect --thread-id <thread-id>",
			"anx threads inspect --state active --full-id",
		},
		Flags: []localHelperFlag{
			{Name: "--thread-id <thread-id>", Description: "Thread id to inspect."},
			{Name: "--state <state>", Description: "Discover one thread by lifecycle state (active, archived, trashed)."},
			{Name: "--max-events <n>", Description: "Maximum recent context events to include."},
			{Name: "--include-artifact-content", Description: "Include artifact content previews from the underlying read-only thread views."},
			{Name: "--full-id", Description: "(debug/admin) Render full event and inbox ids in default text output (non-JSON)."},
		},
	},
	{
		Path:        "threads workspace",
		Summary:     "Read-only backing-thread workspace projection: context, inbox, board membership, and related-thread signals in one command.",
		JSONShape:   "`thread`, `context`, `collaboration`, `inbox`, `pending_attention`, `related_threads`, `follow_up`",
		Composition: "Resolves one thread by id or discovery filters, loads read-only thread projections, adds thread-scoped inbox items, and follows related thread refs for diagnostic review. Prefer `topics workspace` for normal operator coordination.",
		Examples: []string{
			"anx threads workspace --thread-id <thread-id> --full-id",
			"anx threads workspace --state active",
		},
		Flags: []localHelperFlag{
			{Name: "--thread-id <thread-id>", Description: "Thread id to inspect."},
			{Name: "--state <state>", Description: "Discover one thread by lifecycle state (active, archived, trashed)."},
			{Name: "--max-events <n>", Description: "Maximum recent context events to include."},
			{Name: "--include-artifact-content", Description: "Include artifact content previews from the underlying read-only thread views."},
			{Name: "--full-id", Description: "(debug/admin) Render full event and inbox ids in default text output (non-JSON)."},
		},
	},
	{
		Path:        "boards workspace",
		Summary:     "Canonical board read path: load one board's workspace: optional primary topic, cards by column, linked documents, inbox items, and summary.",
		JSONShape:   "`board_id`, `board`, `primary_topic`, `cards`, `documents`, `inbox`, `board_summary`, `projection_freshness`, `board_summary_freshness`, `warnings`, `section_kinds`, `generated_at`",
		Composition: "Resolves a board by typed ref or handle, fetches the projection workspace with per-card thread backing, and renders cards grouped by canonical column order (backlog, ready, in_progress, blocked, review, done).",
		Examples: []string{
			"anx boards workspace board:<board-handle>",
			"anx boards workspace board_product_launch",
		},
		Flags: []localHelperFlag{
			{Name: "<board-ref-or-handle>", Description: "Board typed ref or handle to load."},
		},
	},
	{
		Path:        "boards cards list",
		Summary:     "List all cards on a board in canonical column order without hydrating thread details.",
		JSONShape:   "`board_id`, `cards`",
		Composition: "Fetches the raw card list for a board ordered by canonical column sequence and per-column rank. Default text leads with card refs and titles; thread refs are secondary context.",
		Examples: []string{
			"anx boards cards list board:<board-handle>",
			"anx boards cards list board:<board-handle> --full-id",
		},
		Flags: []localHelperFlag{
			{Name: "<board-ref-or-handle>", Description: "Board typed ref or handle to list cards for."},
			{Name: "--full-id", Description: "(debug/admin) Render full card ids in default text output."},
		},
	},
	{
		Path:        "workspace summary",
		Summary:     "First-run workspace orientation: boards plus compact card/doc/inbox counts.",
		JSONShape:   "`boards`, `counts`, `generated_at`, optional `warnings`",
		Composition: "Local CLI helper that composes existing list reads without changing the core contract. Boards are required; card, document, and inbox counts are best-effort and surface warnings on partial read failures.",
		Examples: []string{
			"anx workspace summary",
			"anx --json workspace summary",
		},
	},
	{
		Path:        "docs revise",
		Summary:     "Revise a durable document from a local file or JSON body; stages a diff proposal by default.",
		QuickStart:  "Flags: `docs revise <doc-ref> --body-file <path>` stages a proposal; add `--apply` to write immediately.",
		JSONShape:   "Proposal mode returns `proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`; `--apply` sends the revision immediately or applies a staged proposal.",
		Composition: "Fetches the current document revision, discovers the base revision when omitted, computes a local diff, and stages a proposal. Add `--apply` to direct-write the revision; use `--apply --proposal-id <id>` to apply a staged proposal.",
		Examples: []string{
			"anx docs revise doc:runbook --body-file notes.md",
			"anx docs revise --apply --proposal-id <proposal-id>",
			"anx docs revise doc:runbook --apply --body-file notes.md",
			"cat revision.json | anx docs revise doc:runbook",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Document ref, alias, or id to revise."},
			{Name: "--body-file <path>", Description: "Load revised Markdown/text content from a local file or stdin with `-`."},
			{Name: "--from-file <path>", Description: "Advanced JSON revision body from a file."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--apply", Description: "Apply immediately, or apply a staged proposal when combined with --proposal-id."},
			{Name: "--proposal-id <proposal-id>", Description: "Staged proposal id to apply; must be combined with --apply."},
			{Name: "--propose", Description: "Stage a proposal (default; included for explicitness)."},
		},
	},
	{
		Path:        "docs trash",
		Summary:     "Trash a document lineage with `--reason`, advanced JSON via `--from-file`, or both (flags overlay JSON).",
		QuickStart:  "Flags: `docs trash <doc-ref> --reason <text>`; `--from-file <path>` remains the advanced JSON compatibility path.",
		JSONShape:   "`{ reason, actor_id?, ... }` from `--from-file`, merged with `--reason` / `--actor-id` flags.",
		Composition: "Uses the shared lifecycle parser (`lifecycle_spec.go`). Routine trashing prefers `--reason`; JSON input remains supported for existing automation.",
		Examples: []string{
			"anx docs trash doc:runbook --reason \"superseded\"",
			"cat trash.json | anx docs trash doc:runbook --from-file=-",
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Document ref, handle, or id to trash."},
			{Name: "--reason <text>", Description: "Reason for trashing the document."},
			{Name: "--from-file <path>", Description: "Advanced JSON request body from file or stdin (`-`)."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; overlays JSON and defaults from profile when omitted."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "docs content",
		Summary:     "Show the current document content together with authoritative head revision metadata.",
		JSONShape:   "`document`, `revision`, `content`, `status_code`, `headers`",
		Composition: "Loads `docs get`, then renders the current revision content and metadata in one operator-friendly response.",
		Examples: []string{
			`anx docs content doc:runbook`,
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Document ref, alias, or id to inspect."},
		},
	},
	{
		Path:        "docs messages",
		Summary:     "List messages from a Document conversation.",
		JSONShape:   "Fetches the Document backing thread and returns an `events.list`-style filtered timeline slice with document metadata.",
		Composition: "Fetches the Document, then reads its backing thread timeline and filters to messages attached to that document.",
		Examples: []string{
			`anx docs messages doc:runbook`,
			`anx docs messages doc:runbook --max-events 5 --mine`,
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Document ref, alias, or id."},
			{Name: "--max-events <n>", Description: "Return at most N most-recent matching messages."},
			{Name: "--mine", Description: "Filter to messages authored by the active profile actor_id."},
			{Name: "--actor-id <actor-id>", Description: "Filter to one actor id."},
			{Name: "--full-id", Description: "(debug/admin) Render full event ids in default text output."},
			{Name: "--include-archived", Description: "Include archived message events."},
			{Name: "--archived-only", Description: "Show only archived message events."},
			{Name: "--include-trashed", Description: "Include trashed message events."},
			{Name: "--trashed-only", Description: "Show only trashed message events."},
		},
	},
	{
		Path:        "docs message",
		Summary:     "Post a message to a Document conversation without hand-authoring event JSON.",
		QuickStart:  "Flags: `docs message <doc-ref> --body-file <path>` or `--body <text>` for short updates.",
		JSONShape:   "Builds an `events.create` body with `event.type=message_posted`, document/thread refs, profile actor, and payload text.",
		Composition: "Fetches the Document to discover its backing thread, then writes a visible `message_posted` event attached to that document.",
		Examples: []string{
			`anx docs message doc:runbook --body-file note.md`,
			`anx docs message doc:runbook --body "Reviewed the current revision"`,
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Document ref, alias, or id to message."},
			{Name: "--body <text>", Description: "Message body text."},
			{Name: "--body-file <path>", Description: "Load message body text from a local file."},
			{Name: "--summary <text>", Description: "Optional short event summary."},
			{Name: "--ref <typed-ref>", Description: "Additional typed ref, repeatable."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
	{
		Path:        "docs reply",
		Summary:     "Reply to an existing Document message.",
		QuickStart:  "Flags: `docs reply <doc-ref> --to <message-id> --body-file <path>` or `--body <text>`.",
		JSONShape:   "Builds an `events.create` body like `docs message` and adds `payload.reply_to_event_id` plus an `event:launch-update` ref.",
		Composition: "Fetches the Document and validates the target message exists on its backing thread before posting the reply.",
		Examples: []string{
			`anx docs reply doc:runbook --to <message-id> --body "Confirmed"`,
			`anx docs reply doc:runbook --to <message-id> --body-file reply.md`,
		},
		Flags: []localHelperFlag{
			{Name: "<ref>", Description: "Document ref, alias, or id to reply on."},
			{Name: "--to <message-id>", Description: "Message/event id, typed ref, or handle being replied to."},
			{Name: "--body <text>", Description: "Reply body text."},
			{Name: "--body-file <path>", Description: "Load reply body text from a local file."},
			{Name: "--summary <text>", Description: "Optional short event summary."},
			{Name: "--ref <typed-ref>", Description: "Additional typed ref, repeatable."},
			{Name: "--actor-id <actor-id>", Description: "Actor id; defaults from the active profile when available."},
			{Name: "--dry-run", Description: "Validate and render the request without sending it."},
		},
	},
}

func isHelpToken(value string) bool {
	value = strings.TrimSpace(value)
	switch value {
	case "help", "--help", "-h":
		return true
	default:
		return false
	}
}

func (a *App) rootUsageText() string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(`anx - Agent Nexus CLI

Domain model:
  topics   Discuss and coordinate around a topic.
  boards   Track active work with columns and cards.
  docs     Maintain durable context and institutional knowledge.
  threads  Inspect backing timelines only when diagnosing low-level state.

Usage:
  anx [global flags] <command>

Core Commands:
  version       Print CLI/runtime version details
  doctor        Validate local and network preconditions
  update        Replace the installed CLI binary with the recommended or requested release
  concepts      Explain the core ANX primitives and when to use them
  bridge        Install, manage, and inspect the Python wake-routing bridge runtime
  auth          Manage agent registration, profile auth, and token lifecycle
  config        Inspect effective CLI config and set the active profile (base URL, agent)
  import        Bootstrap a precision-first workspace import and run local import helpers
  install       Install local ANX helper artifacts, including the opinionated agent skill
  draft         Stage write requests locally and commit them later
  human         Surface ask, review, or escalation items to the human Inbox
  provenance    Walk refs/provenance links as a deterministic graph
  secret        Manage workspace secrets for agent credential injection
  workspace     Summarize workspace boards and counts for first-run orientation
  read          Read an ANX resource from a URL or typed ref
  url           Print a shareable ANX URL for a resource
  api call      Perform an arbitrary HTTP API request
  help [topic]  Show onboarding help or generated command help
`) + "\n")

	meta, err := registry.LoadEmbedded()
	if err == nil {
		b.WriteString("\nGenerated Command Groups:\n")
		for _, topic := range runtimeGeneratedTopics {
			if topic.Path == "auth" {
				// Listed under Core Commands; omit duplicate row here.
				continue
			}
			count := len(runtimeCommandsForTopic(meta, topic.Path))
			if count == 0 {
				continue
			}
			b.WriteString(fmt.Sprintf("  %-12s %s (%d)\n", topic.Path, topic.Description, count))
		}
	}

	b.WriteString(strings.TrimSpace(`

Onboarding:
  `+"`anx concepts`"+` for a quick primitive-selection guide.
  `+"`anx help onboarding`"+` for the offline quick-start topic.
  `+"`anx meta doc agent-guide`"+` for the prescriptive bundled agent guide.
  `+"`anx install skill --path ./SKILL.md`"+` to install the opinionated ANX agent skill.

Global Flags:
  --json
  --base-url <url>
  --agent <name>
  --no-color
  --verbose
  --headers
  --timeout <duration>
`) + "\n")

	return b.String()
}

func helpTopicText(topic string) (string, bool) {
	topic = strings.TrimSpace(topic)
	if dotConverted := strings.ReplaceAll(topic, ".", " "); dotConverted != topic {
		if text, ok := helpTopicText(dotConverted); ok {
			return text, true
		}
	}
	if topic == "draft" {
		return draftUsageText(), true
	}
	if topic == "human" {
		return humanUsageText() + "\n", true
	}
	if topic == "import" {
		return importUsageText() + "\n", true
	}
	if topic == "install" {
		return installUsageText() + "\n", true
	}
	if topic == "install skill" {
		return installSkillUsageText() + "\n", true
	}
	if topic == "onboarding" {
		return onboardingHelpText(), true
	}
	if topic == "concepts" || topic == "primitives" || topic == "primitives guide" {
		return conceptsGuideText() + "\n", true
	}
	if topic == "profiles" {
		return profilesDocText() + "\n", true
	}
	if topic == "env" {
		return envDocText() + "\n", true
	}
	if topic == "config" {
		return strings.TrimSpace(`Config surface for the active CLI profile

Use this group to set or inspect which local profile supplies base URL and auth when you omit --agent / --base-url.

Core commands:
  config use <profile>   Persist the active profile (equivalent to auth default).
  config show            Print effective settings and per-field sources (tokens redacted).
  config unset           Remove the default profile marker (~/.config/anx/default-profile).

Related:
  auth list              List profiles and which is active.
  auth default <profile> Same selection as config use.

Docs:
  anx meta doc profiles
  anx meta doc env`) + "\n", true
	}
	if topic == "auth" {
		return strings.TrimSpace(`Auth lifecycle and registration surface

Use this group to register a profile, inspect the active identity, and manage local auth state.

Core commands:
  auth register       Create or register a profile.
  auth whoami         Inspect the active profile.
  auth list           List local profiles.
  auth default        Select the default profile.
  auth update-username  Rename the current principal locally.
  auth rotate         Rotate the active agent key.
  auth revoke         Revoke the current profile.
  auth token-status   Inspect whether the profile still has refreshable token material.

	Related commands:
  auth invites        Manage invite tokens and invite-backed registration.
  auth bootstrap      Inspect bootstrap status before first registration.
  auth principals     Inspect or revoke principals.
  auth audit          Inspect audit records for auth activity.`) + "\n", true
	}
	if topic == "auth principals" {
		return strings.TrimSpace(`Auth principal administration

Use this group to inspect workspace-local principals and revoke principals when you are acting with auth-admin authority.

Core commands:
  auth principals list      List principals; use --handles-only for mentionable handles.
  auth principals revoke    Revoke a principal by agent id.

Examples:
  anx auth principals list --taggable
  anx auth principals list --handles-only
  anx auth principals revoke --agent-id <agent-id>

Safety:
  Revoking another principal is administrative. Human lockout paths require explicit break-glass flags and a reason.`) + "\n", true
	}
	if topic == "auth audit" {
		return strings.TrimSpace(`Auth audit inspection

Use this group to read auth and principal lifecycle audit records.

Core commands:
  auth audit list       List recent auth audit records.

Examples:
  anx auth audit list
  anx auth audit list --limit 50`) + "\n", true
	}
	if topic == "notifications" {
		return strings.TrimSpace(`Agent notification surface

Use this group to inspect and clear durable wake notifications for the active agent profile.

Core commands:
  notifications list       List queued notifications, usually with --status unread.
  notifications read       Mark one wake notification read.
  notifications dismiss    Dismiss one wake notification.

Examples:
  anx notifications list --status unread
  anx notifications read --wakeup-id <wakeup-id>
  anx notifications dismiss --wakeup-id <wakeup-id>

Mental model:
  Inbox is for human attention items. Notifications are durable agent wake/routing signals.`) + "\n", true
	}
	if topic == "secret" {
		return strings.TrimSpace(`Workspace secrets surface

Use this group to create, inspect, reveal, update, delete, or inject workspace secrets for agent execution.

Core commands:
  secret list              List secret metadata.
  secret create <name>     Create a secret value from stdin.
  secret get <name>        Show secret metadata; add --reveal before the name to print the value.
  secret update <name>     Replace a secret value from stdin.
  secret delete <name>     Delete a secret.
  secret exec              Run a command with selected secrets in its environment.

Examples:
  printf %s "$VALUE" | anx secret create OPENAI_API_KEY --from-stdin
  anx secret get OPENAI_API_KEY
  anx secret get --reveal OPENAI_API_KEY
  anx secret exec --secret OPENAI_API_KEY -- ./my-agent.sh`) + "\n", true
	}
	if topic == "secret get" {
		return strings.TrimSpace(`Secret metadata and reveal

Usage:
  anx secret get <name-or-id>
  anx secret get --reveal <name-or-id>

Without --reveal, the command prints metadata only. With --reveal, it prints the decrypted secret value and records the reveal in auth audit.`) + "\n", true
	}
	if topic == "derived" {
		return strings.TrimSpace(`Derived maintenance surface

Use this group to refresh or inspect derived views that are computed from canonical state.

Core commands:
  derived rebuild     Rebuild derived state from the canonical records.

Tip: derived commands are operational helpers, not the source of truth.`) + "\n", true
	}
	if topic == "workspace" {
		return strings.TrimSpace(`Workspace orientation surface

Use this group for first-run workspace orientation before drilling into topics, boards, cards, docs, or inbox.

Core commands:
  workspace summary    Summarize boards plus compact card/doc/inbox counts.

Tip: default text is intended for quick agent readbacks. Use `+"`--json`"+` only when code or scripts need to parse the summary.`) + "\n", true
	}
	if topic == "meta" {
		return strings.TrimSpace(`Metadata and shipped reference surface

Use this group to inspect CLI/runtime metadata and to print the bundled runtime reference docs.

Core commands:
  meta health     Inspect overall CLI/runtime health.
  meta livez      Check liveness.
  meta readyz     Check readiness.
  meta version    Print version information.

Reference commands:
  meta docs       Print the bundled runtime help reference.
  meta doc        Print one bundled runtime help topic.
  meta skill      Render the bundled opinionated ANX agent skill.
  meta commands   Inspect generated command metadata.
  meta concepts   Inspect generated concepts metadata.
  meta ops        Inspect operational metadata helpers.`) + "\n", true
	}
	if topic == "meta ops" {
		return strings.TrimSpace(`Metadata operations helpers

Core commands:
  meta ops health      Inspect low-level operational health metadata.

Tip: this is a diagnostic surface; normal agents usually start with anx doctor, anx workspace summary, and anx meta docs.`) + "\n", true
	}
	if topic == "update" {
		return updateUsageText() + "\n", true
	}
	if topic == "bridge" {
		return bridgeUsageText(), true
	}
	if topic == "api call" {
		return apiCallUsageText() + "\n", true
	}
	if topic == "read" {
		return readCommandHelpText() + "\n", true
	}
	if topic == "url" {
		return urlCommandHelpText() + "\n", true
	}
	if topic == "agent-guide" {
		return agentGuideText(), true
	}
	if topic == "agent-bridge" || topic == "agent bridge" {
		return agentBridgeGuideText(), true
	}
	if topic == "wake-routing" || topic == "wake routing" {
		return wakeRoutingGuideText(), true
	}
	if topic == "provenance" || topic == "provenance walk" {
		return provenanceUsageText() + "\n", true
	}
	if topic == "meta docs" {
		return metaDocsUsageText() + "\n", true
	}
	if topic == "meta doc" {
		return metaDocUsageText() + "\n", true
	}
	if text, ok := configLocalHelpText(topic); ok {
		return text + "\n", true
	}
	if text, ok := authLocalHelpText(topic); ok {
		return text + "\n", true
	}
	text, ok := generatedHelpText(topic)
	if !ok {
		return "", false
	}
	return text + "\n", true
}

func generatedHelpText(topic string) (string, bool) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return "", false
	}
	if rewritten, ok := applyCommandShapeCompatibilityAlias(strings.Fields(topic)); ok {
		topic = strings.Join(rewritten, " ")
	}
	if helper, ok := localHelperTopicByPath(topic); ok {
		meta, err := registry.LoadEmbedded()
		if err != nil {
			return formatLocalHelperHelp(helper, true), true
		}
		mapped := mapRuntimePathToRegistryPath(topic)
		exact, exactOK := commandByCLIPath(meta.Commands, mapped)
		if exactOK && runtimeSupportsCommand(exact.CommandID) {
			gen := formatGeneratedCommandHelp(topic, exact, false)
			local := formatLocalHelperHelp(helper, false)
			if strings.HasPrefix(topic, "docs ") {
				return strings.TrimSpace(local + "\n\n" + gen + "\n\n" + formatGlobalFlagUsage(topic)), true
			}
			return strings.TrimSpace(gen + "\n\n" + local + "\n\n" + formatGlobalFlagUsage(topic)), true
		}
		return formatLocalHelperHelp(helper, true), true
	}
	meta, err := registry.LoadEmbedded()
	if err != nil {
		return "", false
	}

	mapped := mapRuntimePathToRegistryPath(topic)
	exact, exactOK := commandByCLIPath(meta.Commands, mapped)
	if exactOK {
		if !runtimeSupportsCommand(exact.CommandID) {
			return "", false
		}
		return formatGeneratedCommandHelp(topic, exact, true), true
	}

	commands := runtimeCommandsForTopic(meta, topic)
	if len(commands) == 0 {
		return "", false
	}
	return formatGeneratedGroupHelp(topic, commands), true
}

func formatGeneratedGroupHelp(topic string, commands []registry.Command) string {
	topic = strings.TrimSpace(topic)
	subcommands := make([]registry.Command, 0)
	prefix := mapRuntimePathToRegistryPath(topic)
	prefixParts := strings.Fields(prefix)
	prefixLen := len(prefixParts)
	for _, cmd := range commands {
		parts := strings.Fields(strings.TrimSpace(cmd.CLIPath))
		if len(parts) <= prefixLen {
			continue
		}
		if strings.Join(parts[:prefixLen], " ") != prefix {
			continue
		}
		if len(parts) == prefixLen+1 {
			subcommands = append(subcommands, cmd)
		}
	}
	if len(subcommands) == 0 {
		subcommands = commands
	}
	sort.Slice(subcommands, func(i, j int) bool {
		left := strings.TrimSpace(subcommands[i].CLIPath)
		right := strings.TrimSpace(subcommands[j].CLIPath)
		return left < right
	})

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Generated Help: %s\n\n", topic))
	b.WriteString("Commands:\n")
	for _, cmd := range subcommands {
		cliPath := runtimePathFromRegistryPath(strings.TrimSpace(cmd.CLIPath))
		summary := strings.TrimSpace(cmd.Summary)
		if summary == "" {
			summary = strings.TrimSpace(cmd.Why)
		}
		if summary == "" {
			summary = "no summary"
		}
		b.WriteString(fmt.Sprintf("  %-24s %s\n", cliPath, summary))
	}
	if supplement := localGroupHelpSupplement(topic); supplement != "" {
		b.WriteString("\n")
		b.WriteString(supplement)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(formatGlobalFlagUsage(topic))
	b.WriteString("\n")
	b.WriteString("\nTip: `anx help <command path>` for full command-level generated details.\n")
	return strings.TrimSpace(b.String())
}

func localGroupHelpSupplement(topic string) string {
	switch strings.TrimSpace(topic) {
	case "topics":
		return strings.TrimSpace(`Primary operator coordination:
  topics create           Create a topic from plain flags or advanced JSON.
  topics message          Post a topic conversation message.
  topics messages         List topic conversation messages.
  topics reply            Reply to a specific topic message.
  topics workspace        Load the topic workspace (cards, docs, backing threads, inbox).
  topics list / topics get   Discover and resolve topic ids (` + "`--state`" + `, ` + "`--q`" + `, pagination, archive/trash visibility flags).
	  Tip: use Topics for discussion/current context; use Boards for active work and Docs for durable knowledge. Start triage with ` + "`anx topics workspace topic:<handle>`" + `.`)
	case "threads":
		return strings.TrimSpace(`Read-only backing-thread diagnostics and direct thread messages:
  threads message         Post directly to a backing thread; prefer domain commands like ` + "`anx cards message`" + ` or ` + "`anx topics message`" + `.
  threads reply           Reply to an existing message on a backing thread.
  threads workspace       Diagnostic workspace projection (context + inbox + related threads).
  threads inspect          Smaller diagnostic bundle (context + inbox).
  threads timeline         Backing thread timeline and expansions.
	  Tip: prefer domain commands like ` + "`anx cards message card:<handle>`" + ` for normal authoring and ` + "`anx topics workspace topic:<handle>`" + ` for primary coordination reads. Use ` + "`anx threads workspace --full-id`" + ` (debug/admin) when you need the backing-thread projection with full ids in default text; use ` + "`--state active`" + ` to discover backing threads by lifecycle state. For a minimal ` + "`{thread}`" + ` read, use ` + "`anx threads get`" + ` (contract: ` + "`threads.inspect`" + `).`)
	case "events":
		return strings.TrimSpace(`Local inspection helpers:
  events list              List timeline events with thread/type/actor filters, id mode, and preview summaries.
  events explain           Explain known event-type conventions and local validation constraints.
  events validate          Validate an events.create payload from stdin/--from-file without sending a request.
	  Tip: use ` + "`--mine`" + ` or ` + "`--actor-id <id>`" + ` to audit one actor; add ` + "`--full-id`" + ` (debug/admin) for copy/paste IDs.
	  Raw ` + "`events create`" + ` is a contract escape hatch. For ordinary discussion, use ` + "`anx topics message topic:<handle>`" + `, ` + "`anx docs message doc:<handle>`" + `, or ` + "`anx cards message card:<handle>`" + ` instead of hand-authoring a ` + "`message_posted`" + ` event.
  For details: ` + "`anx events explain <event-type>`")
	case "artifacts":
		return strings.TrimSpace(`Common attachment flow:
  artifacts create --file <path> --ref <typed-ref>
                            Upload a file attachment with repeatable typed refs.
  artifacts content <id>   With no --output, streams raw artifact bytes to stdout.
  artifacts content <id> --output <path>
                            Download raw artifact bytes to a file (` + "`--output -`" + ` is an explicit alias for stdout).
  artifacts content <id> --output .
                            Download using the server-provided filename.
  artifacts download       Alias for raw byte download (same as artifacts content).

Aliases and inspection:
  artifacts get            Compatibility alias for artifacts inspect.
  artifacts inspect        Fetch artifact metadata and content in one call.

Lower-level helpers:
  artifacts attachments create
                            Explicit multipart attachment upload path.`)
	case "docs":
		return strings.TrimSpace(`Local inspection helpers:
  docs content             Show current document content with revision metadata.
  docs message             Post a document conversation message.
  docs messages            List document conversation messages.
  docs reply               Reply to a specific document message.
  Mutation flow:
  docs create              Create durable context from flags plus ` + "`--body`" + ` / ` + "`--body-file`" + `, or from advanced JSON.
  docs revise              Revise from ` + "`--body-file`" + `; stages a diff proposal by default, or direct-writes with ` + "`--apply`" + `.
   Tip: agents should draft Markdown locally and pass ` + "`--body-file <path>`" + `. ` + "`docs revise doc:<handle> --body-file <path>`" + ` discovers the base revision and returns an apply command for the staged proposal.`)
	case "meta":
		return strings.TrimSpace(`Shipped reference docs:
  meta docs               Print the bundled Markdown runtime reference.
  meta doc                Print one bundled Markdown topic, for example ` + "`anx meta doc agent-guide`" + `.
  meta skill              Render the bundled opinionated ANX agent skill, for example ` + "`anx meta skill anx`" + `.
  Tip: use ` + "`anx help meta`" + ` for the short runtime surface, ` + "`anx meta docs`" + ` for the full shipped reference, and ` + "`anx install skill --path ./SKILL.md`" + ` to install the agent skill.`)
	case "boards":
		return strings.TrimSpace(`Active work tracking:
  boards create           Create a Board from flags, optionally tied to ` + "`--topic`" + `.
  boards patch            Patch Board metadata from JSON; use ` + "`--dry-run`" + ` to preview.
  boards workspace        Inspect board context, cards, documents, and inbox.

Card mental model:
  Cards are first-class work items. Use ` + "`anx cards ...`" + ` for card create, list, get, message, assign, move, revise, resolve, reopen, and lifecycle verbs.
  Use ` + "`anx cards list --board <board-ref>`" + ` when you want one board's cards.

Read paths:
  boards get / boards workspace   Board metadata including ` + "`updated_at`" + ` for optimistic concurrency.
  cards list --board              Existing cards and titles before adding more.
  boards cards list/get           Board-scoped reads for board-local inspection.
  boards cards create-batch       Board-scoped batch creation from JSON.

  Examples:
    anx cards list --board board:<board-handle>
    anx cards create --board board:<board-handle> --title "Buy groceries" --body-file card.md
    anx cards move card:<card-handle> --column review
    anx cards resolve card:<card-handle> --reason "ok" --body-file evidence.md`)
	case "boards cards":
		return strings.TrimSpace(`Board-scoped card read and batch surface:
  boards cards list          List cards on one board.
  boards cards get           Get a card through its board context.
  boards cards create-batch  Create many cards on one board from JSON.

Canonical card workflow:
  Use ` + "`anx cards`" + ` for normal card operations. Cards are first-class work items, not subordinate board commands.
  - ` + "`anx cards list --board <board-ref>`" + ` lists one board's cards.
  - ` + "`anx cards create --board <board-ref>`" + ` creates one card.
  - ` + "`anx cards get|message|assign|move|revise|resolve|reopen|archive|trash|restore|purge`" + ` operate on the card ref directly.`)
	case "cards":
		return strings.TrimSpace(`Agent-facing Card workflow:
  cards list               List all cards, or use ` + "`--board <board-ref>`" + ` for one board.
  cards get                Get one card by ref, handle, or id.
  cards create             Create a board work card from flags plus ` + "`--body`" + ` / ` + "`--body-file`" + `.
  cards message            Post a card conversation update without event JSON.
  cards messages           List card conversation messages.
  cards reply              Reply to a specific card message.
  cards revise             Revise card title/body from ` + "`--body-file`" + `; discovers ` + "`if_base_revision`" + ` when omitted.
  cards move               Move workflow column; discovers the parent board concurrency token when omitted.
  cards assign             Replace or clear assignees.
  cards resolve            Move to done with resolution evidence refs or an evidence body.
  cards reopen             Move a resolved card back to active workflow.
   Tip: use ` + "`cards message card:<handle> --body-file update.md`" + ` for ordinary status updates. Use raw ` + "`events create`" + ` only for contract-level writes or unusual integrations.
   Board context is an input (` + "`--board`" + `) or filter (` + "`--board`" + `), not the card command namespace.`)
	case "auth":
		return strings.TrimSpace(`Local auth lifecycle helpers:
  auth whoami             Validate the active profile against the server and show resolved identity.
  auth list               List local CLI profiles and which one is active.
  auth default            Persist the default CLI profile used when no explicit agent is selected.
  auth update-username    Update the current principal username and sync the local profile.
  auth rotate             Rotate the active agent key and refresh stored credentials.
  auth revoke             Revoke the active agent and mark the local profile revoked. Use explicit human-lockout flags only for break-glass recovery.
  auth principals revoke  Revoke another principal by id, with explicit human-lockout flags and a required reason for the break-glass path.
  auth token-status       Inspect whether the local profile still has refreshable token material.
  Tip: use ` + "`anx auth bootstrap status`" + ` before first registration, ` + "`anx auth register --username <username> --bootstrap-token <token>`" + ` for the first principal, and ` + "`anx auth invites create --kind human|agent`" + ` before later registrations.`)
	default:
		return ""
	}
}

func localHelperTopicByPath(path string) (localHelperTopic, bool) {
	path = strings.Join(strings.Fields(strings.TrimSpace(path)), " ")
	for _, topic := range localHelperTopics {
		if strings.Join(strings.Fields(strings.TrimSpace(topic.Path)), " ") == path {
			return topic, true
		}
	}
	return localHelperTopic{}, false
}

func formatLocalHelperHelp(topic localHelperTopic, includeGlobalFlags bool) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Local Help: %s\n\n", strings.TrimSpace(topic.Path)))
	b.WriteString("- Kind: `local helper`\n")
	b.WriteString(fmt.Sprintf("- Summary: %s\n", strings.TrimSpace(topic.Summary)))
	if strings.TrimSpace(topic.QuickStart) != "" {
		b.WriteString(fmt.Sprintf("- Quick start: %s\n", strings.TrimSpace(topic.QuickStart)))
	}
	if strings.TrimSpace(topic.Composition) != "" {
		b.WriteString(fmt.Sprintf("- Composition: %s\n", strings.TrimSpace(topic.Composition)))
	}
	if strings.TrimSpace(topic.JSONShape) != "" {
		b.WriteString(fmt.Sprintf("- JSON body: %s\n", strings.TrimSpace(topic.JSONShape)))
	}
	if len(topic.Examples) > 0 {
		b.WriteString("- Examples:\n")
		for _, example := range topic.Examples {
			b.WriteString(fmt.Sprintf("  - `%s`\n", strings.TrimSpace(example)))
		}
	}
	if len(topic.Flags) > 0 {
		b.WriteString("\nFlags:\n")
		for _, flag := range topic.Flags {
			b.WriteString(fmt.Sprintf("  %-28s %s\n", strings.TrimSpace(flag.Name), strings.TrimSpace(flag.Description)))
		}
	}
	if includeGlobalFlags {
		b.WriteString("\n\n")
		b.WriteString(formatGlobalFlagUsage(topic.Path))
	}
	return strings.TrimSpace(b.String())
}

func formatGeneratedCommandHelp(topic string, cmd registry.Command, includeGlobalFlags bool) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Generated Help: %s\n\n", topic))
	b.WriteString(fmt.Sprintf("- Command ID: `%s`\n", cmd.CommandID))
	b.WriteString(fmt.Sprintf("- CLI path: `%s`\n", runtimePathFromRegistryPath(cmd.CLIPath)))
	b.WriteString(fmt.Sprintf("- HTTP: `%s %s`\n", cmd.Method, cmd.Path))
	if strings.TrimSpace(cmd.Stability) != "" {
		b.WriteString(fmt.Sprintf("- Stability: `%s`\n", strings.TrimSpace(cmd.Stability)))
	}
	if strings.TrimSpace(cmd.InputMode) != "" {
		b.WriteString(fmt.Sprintf("- Input mode: `%s`\n", strings.TrimSpace(cmd.InputMode)))
	}
	if strings.TrimSpace(cmd.Why) != "" {
		b.WriteString(fmt.Sprintf("- Why: %s\n", strings.TrimSpace(cmd.Why)))
	}
	if strings.TrimSpace(cmd.OutputEnvelope) != "" {
		b.WriteString(fmt.Sprintf("- Output: %s\n", strings.TrimSpace(cmd.OutputEnvelope)))
	}
	if len(cmd.ErrorCodes) > 0 {
		b.WriteString(fmt.Sprintf("- Error codes: `%s`\n", strings.Join(cmd.ErrorCodes, "`, `")))
	}
	if len(cmd.Concepts) > 0 {
		b.WriteString(fmt.Sprintf("- Concepts: `%s`\n", strings.Join(cmd.Concepts, "`, `")))
	}
	if strings.TrimSpace(cmd.AgentNotes) != "" {
		b.WriteString(fmt.Sprintf("- Agent notes: %s\n", strings.TrimSpace(cmd.AgentNotes)))
	}
	if len(cmd.Adjacent) > 0 {
		adj := make([]string, 0, len(cmd.Adjacent))
		for _, item := range cmd.Adjacent {
			adj = append(adj, runtimePathFromRegistryPath(commandIDToCLIPath(item)))
		}
		b.WriteString(fmt.Sprintf("- Adjacent commands: `%s`\n", strings.Join(adj, "`, `")))
	}
	if len(cmd.Examples) > 0 {
		b.WriteString("- Examples:\n")
		for _, example := range cmd.Examples {
			title := strings.TrimSpace(example.Title)
			if title == "" {
				title = "Example"
			}
			b.WriteString(fmt.Sprintf("  - %s: `%s`\n", title, runtimeCommandFromRegistryCommand(example.Command)))
		}
	}
	if schemaBlock := formatInputSchemaBlock(cmd); strings.TrimSpace(schemaBlock) != "" {
		b.WriteString("\n")
		b.WriteString(schemaBlock)
	}
	if cliInputBlock := formatCLIInputBlock(cmd); strings.TrimSpace(cliInputBlock) != "" {
		b.WriteString("\n\n")
		b.WriteString(cliInputBlock)
	}
	if extra := formatCommandSpecificHelpBlock(cmd); strings.TrimSpace(extra) != "" {
		b.WriteString("\n\n")
		b.WriteString(extra)
	}
	if includeGlobalFlags {
		b.WriteString("\n\n")
		b.WriteString(formatGlobalFlagUsage(topic))
	}
	return strings.TrimSpace(b.String())
}

func formatGlobalFlagUsage(topic string) string {
	path := strings.Join(strings.Fields(strings.TrimSpace(topic)), " ")
	if path == "" {
		path = "<command>"
	}
	return strings.TrimSpace(fmt.Sprintf(`Global flags:
  Global flags can appear before or after the command path.
  Examples: anx %s ... ; anx --json %s ... ; anx %s ... --json (last two: JSON envelope on stdout)
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>`, path, path, path))
}

func formatInputSchemaBlock(cmd registry.Command) string {
	schema := cmd.BodySchema
	hasRequiredPath := len(cmd.PathParams) > 0
	hasBodyFields := schema != nil && (len(schema.Required) > 0 || len(schema.Optional) > 0)
	if !hasRequiredPath && !hasBodyFields {
		return ""
	}
	var b strings.Builder
	b.WriteString("Inputs:\n")
	if hasRequiredPath || (schema != nil && len(schema.Required) > 0) {
		b.WriteString("  Required:\n")
		for _, field := range cmd.PathParams {
			b.WriteString("  - path `")
			b.WriteString(strings.TrimSpace(field))
			b.WriteString("`")
			if note := fieldHelpText(strings.TrimSpace(cmd.CommandID), strings.TrimSpace(field)); note != "" {
				b.WriteString(": ")
				b.WriteString(note)
			}
			b.WriteString("\n")
		}
		if schema != nil {
			for _, field := range schema.Required {
				b.WriteString("  - ")
				b.WriteString(formatBodyFieldLine(strings.TrimSpace(cmd.CommandID), "body", field))
				b.WriteString("\n")
			}
		}
	}
	if schema != nil && len(schema.Optional) > 0 {
		b.WriteString("  Optional:\n")
		for _, field := range schema.Optional {
			b.WriteString("  - ")
			b.WriteString(formatBodyFieldLine(strings.TrimSpace(cmd.CommandID), "body", field))
			b.WriteString("\n")
		}
	}
	if schema != nil {
		if enumLine := formatEnumFieldList(schema.Required, schema.Optional); enumLine != "" {
			b.WriteString("  Enum values: " + enumLine + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func formatCLIInputBlock(cmd registry.Command) string {
	if cmd.CLIInput == nil {
		return ""
	}
	var b strings.Builder
	if cmd.CLIInput.BodyOptional {
		b.WriteString("CLI input:\n")
		b.WriteString("  - JSON body is optional; `--from-file` remains available for advanced request bodies.\n")
	}
	if len(cmd.CLIInput.Flags) > 0 {
		if b.Len() == 0 {
			b.WriteString("CLI input:\n")
		}
		b.WriteString("  Flags:\n")
		for _, flag := range cmd.CLIInput.Flags {
			name := strings.TrimSpace(flag.Name)
			if name == "" {
				continue
			}
			required := ""
			if flag.Required {
				required = " required"
			}
			target := strings.TrimSpace(flag.BodyPath)
			if target != "" {
				target = " -> body `" + target + "`"
			}
			desc := strings.TrimSpace(flag.Description)
			if desc != "" {
				desc = ": " + desc
			}
			b.WriteString(fmt.Sprintf("  - `--%s`%s%s%s\n", name, required, target, desc))
		}
	}
	return strings.TrimSpace(b.String())
}

func formatBodyFieldLine(commandID string, location string, field registry.BodyField) string {
	name := strings.TrimSpace(field.Name)
	fieldType := strings.TrimSpace(field.Type)
	if fieldType == "" {
		fieldType = "any"
	}
	line := fmt.Sprintf("%s `%s` (%s)", strings.TrimSpace(location), name, fieldType)
	if note := fieldHelpText(commandID, name); note != "" {
		line += ": " + note
	}
	return line
}

func fieldHelpText(commandID string, name string) string {
	commandID = strings.TrimSpace(commandID)
	name = strings.TrimSpace(name)
	switch {
	case name == "if_board_updated_at" && commandID == "boards.cards.batch_add":
		return "Optimistic concurrency token. Copy `board.updated_at` from `anx boards get <board-ref-or-handle>`, `anx boards workspace <board-ref-or-handle>`, or the latest board mutation response. You may pass `--if-board-updated-at` instead of embedding it in JSON."
	case name == "if_board_updated_at":
		return "Optimistic concurrency token. Copy `board.updated_at` from `anx boards get <board-ref-or-handle>`, `anx boards workspace <board-ref-or-handle>`, or the latest board mutation response."
	case name == "actor_id" && commandID == "boards.cards.batch_add":
		return "Defaults from the active CLI profile when omitted. Non-empty `--actor-id` overrides `actor_id` in the JSON body."
	case name == "request_key" && commandID == "boards.cards.batch_add":
		return "Idempotency key for the whole batch. Non-empty `--request-key` overrides `request_key` in the JSON body."
	case name == "if_base_revision" && strings.HasPrefix(commandID, "cards."):
		return "Optimistic concurrency token. Copy the current head revision ref from `anx cards get <card-ref-or-handle>` before updating."
	case name == "if_base_revision":
		return "Optimistic concurrency token. Copy the current head revision ref from `anx docs get <doc-ref-or-handle>` before updating."
	case strings.HasPrefix(name, "if_"):
		return "Optimistic concurrency token. Read the latest value from the corresponding read command before mutating."
	case commandID == "inbox.get" && name == "inbox_item_id":
		return "Canonical inbox id or alias from `anx inbox list`."
	default:
		return ""
	}
}

func formatBodyFieldList(fields []registry.BodyField) string {
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		fieldType := strings.TrimSpace(field.Type)
		if name == "" {
			continue
		}
		if fieldType == "" {
			fieldType = "any"
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", name, fieldType))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func formatEnumFieldList(required []registry.BodyField, optional []registry.BodyField) string {
	joined := append([]registry.BodyField{}, required...)
	joined = append(joined, optional...)
	parts := make([]string, 0, len(joined))
	seen := map[string]struct{}{}
	for _, field := range joined {
		name := strings.TrimSpace(field.Name)
		if name == "" || len(field.EnumValues) == 0 {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		enumValues := strings.Join(field.EnumValues, ", ")
		policy := strings.TrimSpace(field.EnumPolicy)
		if policy != "" {
			parts = append(parts, fmt.Sprintf("%s (%s): %s", name, policy, enumValues))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", name, enumValues))
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}

func formatCommandSpecificHelpBlock(cmd registry.Command) string {
	switch strings.TrimSpace(cmd.CommandID) {
	case "events.create":
		return strings.TrimSpace(`Common authoring types:
  Communication: direct communication or important non-structured information
  - ` + "`message_posted`" + `
  Human attention: request and close operator attention
  - ` + "`human_attention_requested`" + `
  - ` + "`human_attention_responded`" + `
  Topics and documents: durable subject and document lifecycle signals
  - ` + "`topic_created`" + `, ` + "`topic_updated`" + `, ` + "`topic_archived`" + `, ` + "`topic_trashed`" + `
  - ` + "`document_created`" + `, ` + "`document_revised`" + `, ` + "`document_trashed`" + `
  Boards and cards: workflow placement and movement
  - ` + "`board_created`" + `, ` + "`board_updated`" + `
  - ` + "`card_created`" + `, ` + "`card_updated`" + `, ` + "`card_moved`" + `, ` + "`card_resolved`" + `
  Exceptions: surface problems, risks, or escalations
  - ` + "`exception_raised`" + `

Usually emitted by higher-level commands:
  - ` + "`human_attention_requested`" + `: prefer ` + "`anx human ask|review|escalate`" + `

Local CLI notes:
  - Prefer higher-level commands for topic, board, card, doc, and human-attention lifecycle writes.
  - Use ` + "`--dry-run`" + ` with ` + "`--from-file`" + ` to validate and preview the request without sending the mutation.`)
	case "threads.timeline":
		return strings.TrimSpace(`Local CLI flags:
  --include-archived        Include archived events in the timeline.
  --archived-only           Show only archived events.
  --include-trashed      Include trashed events in the timeline.
  --trashed-only         Show only trashed events in the timeline.

Note: by default, archived and trashed events are excluded from the timeline output.`)
	case "inbox.list":
		return strings.TrimSpace(`View scoping:
  - ` + "`inbox list`" + ` is read from the active CLI identity's perspective.
  - The response includes ` + "`viewing_as`" + ` so you can confirm the resolved profile, username, and actor_id.
  - Switch perspective with ` + "`--agent <profile>`" + ` or ` + "`ANX_AGENT`" + ` before reading or acting.

Inbox kinds:
  - ` + "`ask`" + `: A requesting agent needs an answer, judgment, or missing context.
  - ` + "`review`" + `: A requesting agent wants review of generated work or a proposed action.
  - ` + "`escalate`" + `: A requesting agent surfaced a risk or abnormal condition.`)
	case "inbox.respond":
		return strings.TrimSpace(`CLI flags (` + "`inbox respond`" + `):
  --inbox-item-id <id>    Inbox item id or list alias (see ` + "`inbox list`" + `).
  --response-text <text>  Freeform response text.
  --notify-mode <mode>    original, target, or none.
  --actor-id <id>         Actor id (` + "`me`" + ` uses the active profile's actor when configured).
  --from-file <path>      JSON body file (API request shape).
  Positional: inbox item id when not given via ` + "`--inbox-item-id`" + `.
  Otherwise: JSON object on stdin (` + "`inbox_item_id`" + `, ` + "`response_text`" + `, optional fields).`)
	case "boards.cards.batch_add":
		return strings.TrimSpace(`CLI input:
  - Provide a JSON object on stdin or via ` + "`--from-file`" + `; it must include ` + "`items`" + ` (array of card create payloads).
  - Board target: a single positional ` + "`<board-ref-or-handle>`" + ` before flags (preferred), or ` + "`--board-id <board-ref-or-handle>`" + ` for compatibility.
  - ` + "`actor_id`" + ` defaults from the active profile when omitted from JSON; ` + "`--actor-id`" + ` sets or overrides it.
  - ` + "`--request-key`" + ` and ` + "`--if-board-updated-at`" + `, when non-empty, override the same keys in the JSON body.

Agent tip: run ` + "`anx boards get <board-ref-or-handle> --json`" + ` (or ` + "`boards workspace`" + `) first, copy ` + "`board.updated_at`" + ` into ` + "`if_board_updated_at`" + `, or pass ` + "`--if-board-updated-at`" + ` from that value. Each item's ` + "`related_refs`" + ` must reference source threads not already backing another card on this board, or the server returns ` + "`conflict`" + `.`)
	default:
		return ""
	}
}

func commandByCLIPath(commands []registry.Command, path string) (registry.Command, bool) {
	path = strings.TrimSpace(path)
	for _, cmd := range commands {
		if strings.TrimSpace(cmd.CLIPath) == path {
			return cmd, true
		}
	}
	return registry.Command{}, false
}

func runtimeCommandsForTopic(meta registry.MetaRegistry, topic string) []registry.Command {
	mapped := mapRuntimePathToRegistryPath(topic)
	commands := meta.CommandsByCLIPathPrefix(mapped)
	filtered := make([]registry.Command, 0, len(commands))
	for _, cmd := range commands {
		if !runtimeSupportsCommand(cmd.CommandID) {
			continue
		}
		filtered = append(filtered, cmd)
	}
	return filtered
}

func runtimeSupportsCommand(commandID string) bool {
	_, ok := runtimeSupportedCommandIDs()[strings.TrimSpace(commandID)]
	return ok
}

func runtimeSupportedCommandIDs() map[string]struct{} {
	return runtimeHelpCatalogSnapshot().SupportedCommandIDs
}

func runtimeGeneratedHelpSpecs() []subcommandSpec {
	return []subcommandSpec{
		{
			command:  "auth",
			valid:    []string{"register"},
			examples: authSubcommandSpec.examples,
			aliases:  authSubcommandSpec.aliases,
		},
		authInvitesSubcommandSpec,
		authBootstrapSubcommandSpec,
		authPrincipalsSubcommandSpec,
		authAuditSubcommandSpec,
		notificationsSubcommandSpec,
		actorsSubcommandSpec,
		topicsSubcommandSpec,
		boardsSubcommandSpec,
		boardsCardsSubcommandSpec,
		docsSubcommandSpec,
		docsRevisionSubcommandSpec,
		cardsSubcommandSpec,
		threadsSubcommandSpec,
		eventsSubcommandSpec,
		inboxSubcommandSpec,
		artifactsSubcommandSpec,
		refEdgesSubcommandSpec,
		derivedSubcommandSpec,
		{
			command:  "meta",
			valid:    []string{"commands", "command", "concepts", "concept"},
			examples: metaSubcommandSpec.examples,
			aliases:  metaSubcommandSpec.aliases,
		},
		metaOpsSubcommandSpec,
	}
}

func runtimeGeneratedRegistryPaths() []string {
	paths := make([]string, 0, 40)
	for _, spec := range runtimeGeneratedHelpSpecs() {
		command := strings.TrimSpace(spec.command)
		if command == "" {
			continue
		}
		for _, subcommand := range spec.valid {
			path := strings.Join(strings.Fields(command+" "+strings.TrimSpace(subcommand)), " ")
			if path == "" {
				continue
			}
			paths = append(paths, path)
		}
	}
	for _, resource := range runtimeGeneratedPacketResources {
		path := strings.Join(strings.Fields(strings.TrimSpace(resource)+" create"), " ")
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	for _, path := range runtimeRegistrySecretHelpPaths {
		path = strings.Join(strings.Fields(strings.TrimSpace(path)), " ")
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// runtimeRegistrySecretHelpPaths are contract x-anx-cli-path values for secret commands (registry 1:1).
var runtimeRegistrySecretHelpPaths = []string{
	"secret list",
	"secret create",
	"secret delete",
	"secret get --reveal",
	"secret exec",
	"secret update",
}

func onboardingHelpText() string {
	return strings.TrimSpace(`Onboarding: first steps (agents / automation)

This CLI is for agent principals. After registration, use ANX as the default durable tracker for work, knowledge, asks, and collaboration. For the full operating model, read ` + "`anx meta doc agent-guide`" + `.

1. Point the CLI at the core API with ` + "`--base-url`" + ` or ` + "`ANX_BASE_URL`" + `.
2. Choose a profile name and pass it with ` + "`--agent`" + ` (or ` + "`ANX_AGENT`" + `) for registration and first checks below.
3. Run ` + "`anx doctor`" + `, then ` + "`anx auth bootstrap status`" + ` to see whether first-principal bootstrap is still open on this workspace.
4. Register the agent profile:
   - If bootstrap is available: ` + "`anx auth register --username <username> --bootstrap-token <token>`" + ` (token comes from workspace operators / deployment).
   - If bootstrap is closed: obtain a one-time invite (` + "`anx auth invites create --kind agent`" + ` from an already-authorized principal on that workspace), then ` + "`anx auth register --username <username> --invite-token <token>`" + `.
5. On a machine where ` + "`~/.config`" + ` persists, set the active profile once: ` + "`anx config use <agent>`" + ` (same as ` + "`anx auth default <agent>`" + `). Later commands can omit ` + "`--base-url`" + ` / ` + "`--agent`" + `; use ` + "`anx config show`" + ` to verify. For CI or ephemeral environments, keep using env vars or flags instead.
6. Confirm with ` + "`anx auth whoami`" + `, run ` + "`anx workspace summary`" + `, then attach work to the right Topic/Card/Doc before mutating.
7. Install the opinionated ANX agent skill into the local agent instruction path when available: ` + "`anx install skill --path <path>`" + `.
8. If this agent runtime can be programmatically prompted or resumed, ask the human: "Should I set up an ANX bridge so humans and agents can wake me with ` + "`@handle`" + ` mentions?" If yes, read ` + "`anx meta doc agent-bridge`" + ` and ` + "`anx meta doc wake-routing`" + `. If this runtime cannot be externally woken, skip bridge setup and say why.

Default behavior after onboarding

- Start non-trivial work with ` + "`anx workspace summary`" + ` and inspect likely related Topics, Cards, Docs, Inbox items, and notifications.
- Use Topics for current work conversation and coordination.
- Use Cards on Boards for concrete trackable tasks, ownership, status, review, and completion evidence.
- Use Docs for durable knowledge, plans, decisions, investigation notes, runbooks, and handoffs.
- Use Topic/Doc/Card messages and replies for collaboration; use raw ` + "`events create`" + ` only for unusual contract-level records.
- Use ` + "`anx human ask|review|escalate`" + ` when blocked, high consequence, or low confidence.

First commands to run

  anx --base-url http://127.0.0.1:8000 --agent <agent> doctor
  anx --base-url http://127.0.0.1:8000 --agent <agent> auth bootstrap status
  anx --base-url http://127.0.0.1:8000 --agent <agent> auth register --username <username> --bootstrap-token <token>   # only when bootstrap is open
  anx --base-url http://127.0.0.1:8000 --agent <new-agent> auth register --username <username> --invite-token <token>   # when bootstrap is closed
  anx config use <agent>   # optional after register: shorter commands on this machine (same as: anx auth default <agent>)
  anx --agent <agent> auth whoami
  anx --agent <agent> workspace summary
  anx --agent <agent> topics list
  anx --agent <agent> inbox stream --max-events 1
  anx install skill --path ./SKILL.md

Next step

  anx meta doc agent-guide
  anx install skill --path ./SKILL.md
  anx meta doc wake-routing`)
}

func mapRuntimePathToRegistryPath(path string) string {
	parts := strings.Fields(strings.TrimSpace(path))
	if len(parts) == 0 {
		return ""
	}
	path = strings.Join(parts, " ")
	rewrites := map[string]string{
		"events tail":           "events stream",
		"inbox tail":            "inbox stream",
		"threads get":           "threads inspect",
		"artifacts get":         "artifacts inspect",
		"artifacts content get": "artifacts content",
		"artifacts download":    "artifacts content",
		"secret get":            "secret get --reveal",
		"meta commands":         "meta commands list",
		"meta command":          "meta commands get",
		"meta concepts":         "meta concepts list",
		"meta concept":          "meta concepts get",
	}
	if rewritten, ok := rewrites[path]; ok {
		return rewritten
	}
	return path
}

func runtimePathFromRegistryPath(path string) string {
	path = strings.TrimSpace(path)
	parts := strings.Fields(path)
	if len(parts) == 0 {
		return ""
	}
	path = strings.Join(parts, " ")
	rewrites := map[string]string{
		"auth agents register": "auth register",
		"meta commands list":   "meta commands",
		"meta commands get":    "meta command",
		"meta concepts list":   "meta concepts",
		"meta concepts get":    "meta concept",
	}
	if rewritten, ok := rewrites[path]; ok {
		return rewritten
	}
	return path
}

func commandIDToCLIPath(commandID string) string {
	cmd, ok := generatedCommandByID(commandID)
	if !ok {
		return strings.TrimSpace(commandID)
	}
	return strings.TrimSpace(cmd.CLIPath)
}

func generatedCommandByID(commandID string) (registry.Command, bool) {
	meta, err := registry.LoadEmbedded()
	if err != nil {
		return registry.Command{}, false
	}
	return meta.CommandByID(commandID)
}

func runtimeCommandFromRegistryCommand(command string) string {
	command = strings.TrimSpace(command)
	command = strings.ReplaceAll(command, "anx auth agents register", "anx auth register")
	command = strings.ReplaceAll(command, "anx events stream", "anx events tail")
	command = strings.ReplaceAll(command, "anx inbox stream", "anx inbox tail")
	command = strings.ReplaceAll(command, "anx meta commands get", "anx meta command")
	command = strings.ReplaceAll(command, "anx meta commands list", "anx meta commands")
	command = strings.ReplaceAll(command, "anx meta concepts get", "anx meta concept")
	command = strings.ReplaceAll(command, "anx meta concepts list", "anx meta concepts")
	return command
}

func configLocalHelpText(topic string) (string, bool) {
	type configTopic struct {
		summary  string
		usage    string
		examples []string
	}
	topics := map[string]configTopic{
		"config use": {
			summary:  "Persist the named profile as the active default used when --agent and ANX_AGENT are omitted.",
			usage:    "anx config use <profile>",
			examples: []string{"anx config use agent-a", "anx --json config use agent-a"},
		},
		"config show": {
			summary:  "Print effective CLI settings and the source of each field (access tokens are redacted).",
			usage:    "anx config show",
			examples: []string{"anx config show", "anx --json config show"},
		},
		"config unset": {
			summary:  "Remove the default profile marker file so the CLI falls back to single-profile auto-select or explicit flags/env.",
			usage:    "anx config unset",
			examples: []string{"anx config unset", "anx --json config unset"},
		},
	}
	entry, ok := topics[strings.Join(strings.Fields(strings.TrimSpace(topic)), " ")]
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString("Local Help: " + strings.TrimSpace(topic) + "\n\n")
	b.WriteString(strings.TrimSpace(entry.summary) + "\n\n")
	b.WriteString("Usage:\n")
	b.WriteString("  " + strings.TrimSpace(entry.usage) + "\n")
	if len(entry.examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, example := range entry.examples {
			b.WriteString("  " + strings.TrimSpace(example) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(formatGlobalFlagUsage(topic))
	return strings.TrimSpace(b.String()), true
}

func authLocalHelpText(topic string) (string, bool) {
	type authTopic struct {
		summary  string
		usage    string
		examples []string
	}
	topics := map[string]authTopic{
		"auth whoami": {
			summary:  "Validate the active profile against the server, print resolved identity metadata, and point to wake-registration next steps.",
			usage:    "anx auth whoami",
			examples: []string{"anx auth whoami", "anx --json auth whoami"},
		},
		"auth list": {
			summary:  "List local CLI profiles and identify the active one.",
			usage:    "anx auth list",
			examples: []string{"anx auth list", "anx --json auth list"},
		},
		"auth default": {
			summary:  "Persist the default profile used when no explicit agent is selected.",
			usage:    "anx auth default <profile>",
			examples: []string{"anx auth default agent-a", "anx --json auth default agent-a"},
		},
		"auth invites": {
			summary:  "Manage invite tokens and invite-backed registration for later principals.",
			usage:    "anx auth invites",
			examples: []string{"anx auth invites create --kind human", "anx auth invites revoke --invite-id <id>"},
		},
		"auth bootstrap": {
			summary:  "Inspect whether bootstrap registration is still available for the first principal.",
			usage:    "anx auth bootstrap status",
			examples: []string{"anx auth bootstrap status", "anx --json auth bootstrap status"},
		},
		"auth update-username": {
			summary:  "Update the authenticated agent username and sync the local profile copy.",
			usage:    "anx auth update-username --username <username>",
			examples: []string{"anx auth update-username --username renamed_agent"},
		},
		"auth rotate": {
			summary:  "Rotate the active agent key and refresh stored credentials.",
			usage:    "anx auth rotate",
			examples: []string{"anx auth rotate", "anx --json auth rotate"},
		},
		"auth revoke": {
			summary:  "Revoke the active agent and mark the local profile revoked.",
			usage:    "anx auth revoke",
			examples: []string{"anx auth revoke", "anx --json auth revoke"},
		},
		"auth token-status": {
			summary:  "Inspect whether the local profile still has refreshable token material.",
			usage:    "anx auth token-status",
			examples: []string{"anx auth token-status", "anx --json auth token-status"},
		},
	}
	entry, ok := topics[strings.Join(strings.Fields(strings.TrimSpace(topic)), " ")]
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString("Local Help: " + strings.TrimSpace(topic) + "\n\n")
	b.WriteString(strings.TrimSpace(entry.summary) + "\n\n")
	b.WriteString("Usage:\n")
	b.WriteString("  " + strings.TrimSpace(entry.usage) + "\n")
	if len(entry.examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, example := range entry.examples {
			b.WriteString("  " + strings.TrimSpace(example) + "\n")
		}
	}
	if strings.Join(strings.Fields(strings.TrimSpace(topic)), " ") == "auth whoami" {
		b.WriteString("\nNext steps:\n")
		b.WriteString("  If this agent should be wakeable by `@handle`, read `anx meta doc wake-routing`.\n")
	}
	b.WriteString("\n")
	b.WriteString(formatGlobalFlagUsage(topic))
	return strings.TrimSpace(b.String()), true
}
