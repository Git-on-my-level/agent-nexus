package app

import (
	"fmt"
	"sort"
	"strings"

	"organization-autorunner-cli/internal/registry"
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
	JSONShape   string
	Composition string
	Examples    []string
	Flags       []localHelperFlag
}

var runtimeGeneratedTopics = []runtimeHelpTopic{
	{Path: "actors", Description: "List and register actor identities"},
	{Path: "auth", Description: "Register, inspect, and manage auth state"},
	{Path: "threads", Description: "Manage thread resources"},
	{Path: "commitments", Description: "Manage commitment resources"},
	{Path: "artifacts", Description: "Manage artifact resources and content"},
	{Path: "boards", Description: "Manage board resources and ordered cards"},
	{Path: "docs", Description: "Manage long-lived docs and revisions"},
	{Path: "events", Description: "Manage events and event streams"},
	{Path: "inbox", Description: "List/get/ack/stream inbox items"},
	{Path: "work-orders", Description: "Create work-order packets"},
	{Path: "receipts", Description: "Create receipt packets"},
	{Path: "reviews", Description: "Create review packets"},
	{Path: "derived", Description: "Run derived-view maintenance actions"},
	{Path: "meta", Description: "Inspect generated command/concept metadata"},
}

var runtimeGeneratedPacketResources = []string{"work-orders", "receipts", "reviews"}

var localHelperTopics = []localHelperTopic{
	{
		Path:        "events list",
		Summary:     "Compose `threads timeline` responses with client-side thread/type/actor filters and preview summaries.",
		JSONShape:   "`thread_id`, `thread_ids`, `events`, `total_events`, `returned_events`",
		Composition: "Fetches one or more thread timelines locally, then filters and summarizes the events without changing contracts or core behavior.",
		Examples: []string{
			"oar events list --thread-id <thread-id> --type actor_statement --mine --full-id",
			"oar events list --thread-id <thread-id> --max-events 10",
		},
		Flags: []localHelperFlag{
			{Name: "--thread-id <thread-id>", Description: "Thread id to inspect (repeatable)."},
			{Name: "--type <event-type>", Description: "Repeatable event type filter."},
			{Name: "--types <csv>", Description: "Comma-separated event types."},
			{Name: "--actor-id <actor-id>", Description: "Filter to one actor id."},
			{Name: "--mine", Description: "Resolve to the active profile actor_id."},
			{Name: "--max-events <n>", Description: "Keep the most recent matching events."},
			{Name: "--full-id", Description: "Render full event ids in human output."},
			{Name: "--include-archived", Description: "Include archived events in results."},
			{Name: "--archived-only", Description: "Show only archived events."},
			{Name: "--include-tombstoned", Description: "Include tombstoned events in results."},
			{Name: "--tombstoned-only", Description: "Show only tombstoned events."},
		},
	},
	{
		Path:        "events validate",
		Summary:     "Validate an `events create` payload locally from stdin or `--from-file` without sending it.",
		JSONShape:   "`command`, `command_id`, `path_params`, `query`, `body`, `valid`",
		Composition: "Parses the same JSON body accepted by `events create`, runs local validation rules, and returns a validation preview envelope without contacting core.",
		Examples: []string{
			`cat event.json | oar events validate`,
			`oar events validate --from-file event.json`,
		},
		Flags: []localHelperFlag{
			{Name: "--from-file <path>", Description: "Load the request body from a JSON file instead of stdin."},
		},
	},
	{
		Path:        "events explain",
		Summary:     "Explain known event-type conventions, required refs, and validation hints, including which type surfaces as a visible thread message.",
		JSONShape:   "`event_type`, `known`, `required_refs`, `payload_requirements`, `examples`, `hint`",
		Composition: "Formats the embedded event reference and validation guidance into a human-readable reference without sending a request. Use it to confirm when `message_posted` is required for a visible thread message in the web UI Messages tab.",
		Examples: []string{
			`oar events explain`,
			`oar events explain message_posted`,
			`oar events explain review_completed`,
		},
		Flags: []localHelperFlag{
			{Name: "<event-type>", Description: "Optional event type to focus on; omit it to list known event types."},
		},
	},
	{
		Path:        "artifacts inspect",
		Summary:     "Fetch artifact metadata and resolved content in one command for operator inspection.",
		JSONShape:   "`artifact`, `content`, `content_headers`, `content_text`, `content_base64`",
		Composition: "Loads artifact metadata with `artifacts get`, then fetches content with `artifacts content` using the resolved artifact id.",
		Examples: []string{
			`oar artifacts inspect --artifact-id <artifact-id>`,
			`oar artifacts inspect <artifact-id-or-alias>`,
		},
		Flags: []localHelperFlag{
			{Name: "--artifact-id <artifact-id>", Description: "Artifact id or unique alias to inspect."},
		},
	},
	{
		Path:        "threads inspect",
		Summary:     "Canonical thread coordination read path: compose one view from `threads context` and related `inbox list` items.",
		JSONShape:   "`thread`, `context`, `collaboration`, `inbox`",
		Composition: "Resolves one thread by id or discovery filters, loads `threads context`, then filters inbox items client-side by `thread_id` for one operator-focused coordination view.",
		Examples: []string{
			"oar threads inspect --thread-id <thread-id>",
			"oar threads inspect --status active --type initiative --full-id",
		},
		Flags: []localHelperFlag{
			{Name: "--thread-id <thread-id>", Description: "Thread id to inspect."},
			{Name: "--status <status>", Description: "Discover one thread by status."},
			{Name: "--priority <priority>", Description: "Discover one thread by priority."},
			{Name: "--stale <bool>", Description: "Discover one thread by stale state."},
			{Name: "--tag <tag>", Description: "Repeatable discovery tag filter."},
			{Name: "--cadence <cadence>", Description: "Repeatable discovery cadence filter."},
			{Name: "--type <thread-type>", Description: "Local discovery filter after `threads list`."},
			{Name: "--max-events <n>", Description: "Maximum recent context events to include."},
			{Name: "--include-artifact-content", Description: "Include artifact content previews from `threads context`."},
			{Name: "--full-id", Description: "Render full event and inbox ids in human output."},
		},
	},
	{
		Path:        "threads workspace",
		Summary:     "Single holistic thread coordination read: combine context, inbox, recommendation review, and related-thread signals in one command.",
		JSONShape:   "`thread`, `context`, `collaboration`, `inbox`, `pending_decisions`, `related_threads`, `related_recommendations`, `related_decisions`, `follow_up`",
		Composition: "Resolves one thread by id or discovery filters, loads `threads context`, adds thread-scoped inbox items from `inbox list`, and follows related thread refs for additional review context.",
		Examples: []string{
			"oar threads workspace --thread-id <thread-id> --full-id",
			"oar threads workspace --thread-id <thread-id> --include-related-event-content --verbose",
			"oar threads workspace --status active --type initiative --full-summary",
		},
		Flags: []localHelperFlag{
			{Name: "--thread-id <thread-id>", Description: "Thread id to inspect."},
			{Name: "--status <status>", Description: "Discover one thread by status."},
			{Name: "--priority <priority>", Description: "Discover one thread by priority."},
			{Name: "--stale <bool>", Description: "Discover one thread by stale state."},
			{Name: "--tag <tag>", Description: "Repeatable discovery tag filter."},
			{Name: "--cadence <cadence>", Description: "Repeatable discovery cadence filter."},
			{Name: "--type <thread-type>", Description: "Local discovery filter after `threads list`."},
			{Name: "--max-events <n>", Description: "Maximum recent context events to include."},
			{Name: "--include-artifact-content", Description: "Include artifact content previews from `threads context`."},
			{Name: "--include-related-event-content", Description: "Hydrate related review items with full `events get` content in one command."},
			{Name: "--full-summary", Description: "Show full recommendation/decision summaries in human output."},
			{Name: "--full-id", Description: "Render full event and inbox ids in human output."},
		},
	},
	{
		Path:        "threads review",
		Summary:     "Opinionated deep-read helper: run the holistic workspace view with related-event hydration and full summaries enabled by default.",
		JSONShape:   "`thread`, `context`, `collaboration`, `inbox`, `pending_decisions`, `related_threads`, `related_recommendations`, `related_decisions`, `follow_up`",
		Composition: "Uses the same aggregate view as `threads workspace`, but defaults to a review-oriented read by hydrating related review items with `events get` content and expanding recommendation summaries in one command.",
		Examples: []string{
			"oar threads review --thread-id <thread-id>",
			"oar threads review --thread-id <thread-id> --full-id",
			"oar threads review --status active --type initiative",
		},
		Flags: []localHelperFlag{
			{Name: "--thread-id <thread-id>", Description: "Thread id to review."},
			{Name: "--status <status>", Description: "Discover one thread by status."},
			{Name: "--priority <priority>", Description: "Discover one thread by priority."},
			{Name: "--stale <bool>", Description: "Discover one thread by stale state."},
			{Name: "--tag <tag>", Description: "Repeatable discovery tag filter."},
			{Name: "--cadence <cadence>", Description: "Repeatable discovery cadence filter."},
			{Name: "--type <thread-type>", Description: "Local discovery filter after `threads list`."},
			{Name: "--max-events <n>", Description: "Maximum recent context events to include."},
			{Name: "--include-artifact-content", Description: "Include artifact content previews from `threads context`."},
			{Name: "--full-id", Description: "Render full event and inbox ids in human output."},
		},
	},
	{
		Path:        "threads recommendations",
		Summary:     "Review one thread's recommendation/decision inputs plus related-thread signals with provenance and follow-up hints.",
		JSONShape:   "`thread`, `recommendations`, `decision_requests`, `decisions`, `pending_decisions`, `related_threads`, `related_recommendations`, `follow_up`",
		Composition: "Resolves one thread by id or discovery filters, loads `threads context`, adds thread-scoped pending decision inbox items from `inbox list`, and follows related thread refs for additional review context.",
		Examples: []string{
			"oar threads recommendations --thread-id <thread-id> --full-id",
			"oar threads recommendations --thread-id <thread-id> --include-related-event-content --verbose",
			"oar threads recommendations --status active --type initiative --full-summary",
		},
		Flags: []localHelperFlag{
			{Name: "--thread-id <thread-id>", Description: "Thread id to review."},
			{Name: "--status <status>", Description: "Discover one thread by status."},
			{Name: "--priority <priority>", Description: "Discover one thread by priority."},
			{Name: "--stale <bool>", Description: "Discover one thread by stale state."},
			{Name: "--tag <tag>", Description: "Repeatable discovery tag filter."},
			{Name: "--cadence <cadence>", Description: "Repeatable discovery cadence filter."},
			{Name: "--type <thread-type>", Description: "Local discovery filter after `threads list`."},
			{Name: "--max-events <n>", Description: "Maximum recent context events to include."},
			{Name: "--include-artifact-content", Description: "Include artifact content previews from `threads context`."},
			{Name: "--include-related-event-content", Description: "Hydrate related review items with full `events get` content in one command."},
			{Name: "--full-summary", Description: "Show full recommendation/decision summaries in human output."},
			{Name: "--full-id", Description: "Render full event and inbox ids in human output."},
		},
	},
	{
		Path:        "threads propose-patch",
		Summary:     "Stage a thread patch proposal locally and show the diff before applying it.",
		JSONShape:   "`proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`",
		Composition: "Resolves the thread id, fetches current state with `threads get`, computes a local diff, and persists a proposal file instead of sending the patch immediately.",
		Examples: []string{
			"oar threads propose-patch --thread-id <thread-id> --from-file patch.json",
			"cat patch.json | oar threads propose-patch --thread-id <thread-id>",
		},
		Flags: []localHelperFlag{
			{Name: "--thread-id <thread-id>", Description: "Thread id to patch."},
			{Name: "--from-file <path>", Description: "Load the patch body from a JSON file."},
		},
	},
	{
		Path:        "threads apply",
		Summary:     "Apply a previously staged thread patch proposal.",
		JSONShape:   "`proposal_id`, `target_command_id`, `applied`, `kept`, `result`",
		Composition: "Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `threads.patch` request.",
		Examples: []string{
			"oar threads apply --proposal-id <proposal-id>",
			"oar threads apply <proposal-id-prefix>",
		},
		Flags: []localHelperFlag{
			{Name: "--proposal-id <proposal-id>", Description: "Proposal id or unique prefix to apply."},
		},
	},
	{
		Path:        "commitments propose-patch",
		Summary:     "Stage a commitment patch proposal locally and show the diff before applying it.",
		JSONShape:   "`proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`",
		Composition: "Resolves the commitment id, fetches current state with `commitments get`, computes a local diff, and persists a proposal file instead of sending the patch immediately.",
		Examples: []string{
			"oar commitments propose-patch --commitment-id <commitment-id> --from-file patch.json",
		},
		Flags: []localHelperFlag{
			{Name: "--commitment-id <commitment-id>", Description: "Commitment id to patch."},
			{Name: "--from-file <path>", Description: "Load the patch body from a JSON file."},
		},
	},
	{
		Path:        "commitments apply",
		Summary:     "Apply a previously staged commitment update proposal.",
		JSONShape:   "`proposal_id`, `target_command_id`, `applied`, `kept`, `result`",
		Composition: "Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `commitments.patch` request.",
		Examples: []string{
			"oar commitments apply --proposal-id <proposal-id>",
		},
		Flags: []localHelperFlag{
			{Name: "--proposal-id <proposal-id>", Description: "Proposal id or unique prefix to apply."},
		},
	},
	{
		Path:        "boards workspace",
		Summary:     "Canonical board read path: load one board's full state including primary thread, primary document, and all cards grouped by column.",
		JSONShape:   "`board_id`, `board`, `primary_thread`, `primary_document`, `cards`, `board_summary`, `generated_at`",
		Composition: "Resolves a board by id, fetches the canonical workspace view with hydrated thread summaries, and renders cards grouped by canonical column order (backlog, ready, in_progress, blocked, review, done).",
		Examples: []string{
			"oar boards workspace --board-id <board-id>",
			"oar boards workspace --board-id board_product_launch",
		},
		Flags: []localHelperFlag{
			{Name: "--board-id <board-id>", Description: "Board id or unique prefix to load."},
		},
	},
	{
		Path:        "boards cards list",
		Summary:     "List all cards on a board in canonical column order without hydrating thread details.",
		JSONShape:   "`board_id`, `cards`",
		Composition: "Fetches the raw card list for a board ordered by canonical column sequence and per-column rank.",
		Examples: []string{
			"oar boards cards list --board-id <board-id>",
		},
		Flags: []localHelperFlag{
			{Name: "--board-id <board-id>", Description: "Board id to list cards for."},
		},
	},
	{
		Path:        "docs propose-update",
		Summary:     "Stage a document update proposal locally and show the content diff before applying it.",
		JSONShape:   "`proposal_id`, `target_command_id`, `path`, `body`, `diff`, `apply_command`",
		Composition: "Fetches the current document revision with `docs get`, computes a local diff against the proposed update, and persists a proposal file instead of sending the update immediately.",
		Examples: []string{
			"oar docs propose-update --document-id <document-id> --content-file <path>",
			"cat update.json | oar docs propose-update --document-id <document-id>",
		},
		Flags: []localHelperFlag{
			{Name: "--document-id <document-id>", Description: "Document id to update."},
			{Name: "--content-file <path>", Description: "Load multiline content from a file into the JSON payload."},
			{Name: "--from-file <path>", Description: "Load the full JSON update body from a file."},
		},
	},
	{
		Path:        "docs content",
		Summary:     "Show the current document content together with authoritative head revision metadata.",
		JSONShape:   "`document`, `revision`, `content`, `status_code`, `headers`",
		Composition: "Loads `docs get`, then renders the current revision content and metadata in one operator-friendly response.",
		Examples: []string{
			`oar docs content --document-id <document-id>`,
			`oar docs content <document-id-or-alias>`,
		},
		Flags: []localHelperFlag{
			{Name: "--document-id <document-id>", Description: "Document id or unique alias to inspect."},
		},
	},
	{
		Path:        "docs validate-update",
		Summary:     "Validate a `docs update` payload locally from stdin or file without sending the mutation.",
		JSONShape:   "`command`, `command_id`, `path_params`, `query`, `body`, `valid`",
		Composition: "Parses the same body accepted by `docs update`, expands `--content-file` when present, and returns a validation preview envelope without contacting core.",
		Examples: []string{
			`cat update.json | oar docs validate-update --document-id <document-id>`,
			`oar docs validate-update --document-id <document-id> --content-file body.md`,
		},
		Flags: []localHelperFlag{
			{Name: "--document-id <document-id>", Description: "Document id to validate against."},
			{Name: "--content-file <path>", Description: "Load multiline content from a file into the JSON payload."},
			{Name: "--from-file <path>", Description: "Load the full JSON update body from a file."},
		},
	},
	{
		Path:        "docs apply",
		Summary:     "Apply a previously staged document update proposal.",
		JSONShape:   "`proposal_id`, `target_command_id`, `applied`, `kept`, `result`",
		Composition: "Loads the local proposal by exact id or unique prefix, validates it again, then sends the underlying `docs.update` request.",
		Examples: []string{
			"oar docs apply --proposal-id <proposal-id>",
			"oar docs apply <proposal-id-prefix>",
		},
		Flags: []localHelperFlag{
			{Name: "--proposal-id <proposal-id>", Description: "Proposal id or unique prefix to apply."},
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
	b.WriteString(strings.TrimSpace(`oar - Organization Autorunner CLI

Usage:
  oar [global flags] <command>

Core Commands:
  version       Print CLI/runtime version details
  doctor        Validate local and network preconditions
  update        Replace the installed CLI binary with the recommended or requested release
  bridge        Install, manage, and inspect the Python wake-routing bridge runtime
  auth          Manage agent registration, profile auth, and token lifecycle
  import        Bootstrap a precision-first workspace import and run local import helpers
  draft         Stage write requests locally and commit them later
  provenance    Walk refs/provenance links as a deterministic graph
  api call      Perform an arbitrary HTTP API request
  help [topic]  Show onboarding help or generated command help
`) + "\n")

	meta, err := registry.LoadEmbedded()
	if err == nil {
		b.WriteString("\nGenerated Command Groups:\n")
		for _, topic := range runtimeGeneratedTopics {
			count := len(runtimeCommandsForTopic(meta, topic.Path))
			if count == 0 {
				continue
			}
			b.WriteString(fmt.Sprintf("  %-12s %s (%d)\n", topic.Path, topic.Description, count))
		}
	}

	b.WriteString(strings.TrimSpace(`

Onboarding:
  `+"`oar help onboarding`"+` for the offline quick-start topic.
  `+"`oar meta doc agent-guide`"+` for the prescriptive bundled agent guide.
  `+"`oar meta skill cursor --write-dir ~/.cursor/skills/oar-cli-onboard`"+` to export a Cursor skill file.

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
	if topic == "import" {
		return importUsageText() + "\n", true
	}
	if topic == "onboarding" {
		return onboardingHelpText(), true
	}
	if topic == "update" {
		return updateUsageText() + "\n", true
	}
	if topic == "bridge" {
		return bridgeUsageText(), true
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
		return formatLocalHelperHelp(helper), true
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
		return formatGeneratedCommandHelp(topic, exact), true
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
	b.WriteString("\nTip: `oar help <command path>` for full command-level generated details.\n")
	return strings.TrimSpace(b.String())
}

func localGroupHelpSupplement(topic string) string {
	switch strings.TrimSpace(topic) {
	case "threads":
		return strings.TrimSpace(`Canonical coordination read path:
  threads review              Deep-read one thread workspace with review hydration enabled by default.
  threads workspace           Compose one holistic thread workspace from context + inbox + related-thread review.
  threads inspect             Compose one thread coordination view from context + inbox in one command.
  threads recommendations     Focus recommendation/decision review with actor+timestamp provenance.
  Mutation flow:
  threads patch               Send the thread patch to core immediately.
  threads propose-patch       Stage a thread patch proposal and inspect the diff before applying.
  threads apply               Apply a staged thread patch proposal.
  Tip: start with ` + "`oar threads review`" + ` when you want one deep review read, use ` + "`oar threads workspace`" + ` for the canonical coordination view, use ` + "`--status/--tag/--type initiative`" + ` to discover one thread, use ` + "`oar threads context`" + ` for cross-thread aggregates, and ` + "`oar threads get`" + ` for raw snapshot-only reads. Add ` + "`--full-id`" + ` for copy/paste ids.`)
	case "commitments":
		return strings.TrimSpace(`Mutation flow:
  commitments patch          Send the commitment patch to core immediately.
  commitments propose-patch  Stage a commitment patch proposal and inspect the diff before applying.
  commitments apply          Apply a staged commitment update proposal.`)
	case "events":
		return strings.TrimSpace(`Local inspection helpers:
  events list              List timeline events with thread/type/actor filters, id mode, and preview summaries.
  events explain           Explain known event-type conventions and local validation constraints.
  events validate          Validate an events.create payload from stdin/--from-file without sending a request.
  Tip: use ` + "`--mine`" + ` or ` + "`--actor-id <id>`" + ` to audit one actor; add ` + "`--full-id`" + ` for copy/paste IDs.
  For details: ` + "`oar events explain <event-type>`")
	case "artifacts":
		return strings.TrimSpace(`Local inspection helper:
  artifacts inspect        Fetch artifact metadata and content in one call.`)
	case "docs":
		return strings.TrimSpace(`Local inspection helpers:
  docs content             Show current document content with revision metadata.
  Mutation flow:
  docs update              Send the document update to core immediately.
  docs propose-update      Stage an update proposal and inspect its diff before applying it.
  docs apply               Apply a staged document update proposal.
  docs validate-update     Validate a docs.update payload from stdin/--from-file.
  Tip: add ` + "`--content-file <path>`" + ` to avoid hand-escaping multiline content.`)
	case "meta":
		return strings.TrimSpace(`Shipped reference docs:
  meta docs               Print the bundled Markdown runtime reference.
  meta doc                Print one bundled Markdown topic, for example ` + "`oar meta doc agent-guide`" + `.
  meta skill              Render a bundled editor-specific skill file, for example ` + "`oar meta skill cursor`" + `.
  Tip: use ` + "`oar help meta`" + ` for the short runtime surface, ` + "`oar meta docs`" + ` for the full shipped reference, and ` + "`oar meta skill cursor --write-dir ~/.cursor/skills/oar-cli-onboard`" + ` to export a Cursor skill.`)
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
  Tip: use ` + "`oar auth bootstrap status`" + ` before first registration, ` + "`oar auth register --username <username> --bootstrap-token <token>`" + ` for the first principal, and ` + "`oar auth invites create --kind human|agent`" + ` before later registrations.`)
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

func formatLocalHelperHelp(topic localHelperTopic) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Local Help: %s\n\n", strings.TrimSpace(topic.Path)))
	b.WriteString("- Kind: `local helper`\n")
	b.WriteString(fmt.Sprintf("- Summary: %s\n", strings.TrimSpace(topic.Summary)))
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
	b.WriteString("\n\n")
	b.WriteString(formatGlobalFlagUsage(topic.Path))
	return strings.TrimSpace(b.String())
}

func formatGeneratedCommandHelp(topic string, cmd registry.Command) string {
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
	if schemaBlock := formatBodySchemaBlock(cmd.BodySchema); strings.TrimSpace(schemaBlock) != "" {
		b.WriteString("\n")
		b.WriteString(schemaBlock)
	}
	if extra := formatCommandSpecificHelpBlock(cmd); strings.TrimSpace(extra) != "" {
		b.WriteString("\n\n")
		b.WriteString(extra)
	}
	b.WriteString("\n\n")
	b.WriteString(formatGlobalFlagUsage(topic))
	return strings.TrimSpace(b.String())
}

func formatGlobalFlagUsage(topic string) string {
	path := strings.Join(strings.Fields(strings.TrimSpace(topic)), " ")
	if path == "" {
		path = "<command>"
	}
	return strings.TrimSpace(fmt.Sprintf(`Global flags:
  Global flags can appear before or after the command path.
  Examples: oar --json %s ... ; oar %s ... --json
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>`, path, path))
}

func formatBodySchemaBlock(schema *registry.BodySchema) string {
	if schema == nil {
		return ""
	}
	required := formatBodyFieldList(schema.Required)
	optional := formatBodyFieldList(schema.Optional)
	if required == "" && optional == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Body schema:\n")
	if required == "" {
		b.WriteString("  Required: none\n")
	} else {
		b.WriteString("  Required: " + required + "\n")
	}
	if optional == "" {
		b.WriteString("  Optional: none\n")
	} else {
		b.WriteString("  Optional: " + optional + "\n")
	}
	if enumLine := formatEnumFieldList(schema.Required, schema.Optional); enumLine != "" {
		b.WriteString("  Enum values: " + enumLine + "\n")
	}
	return strings.TrimSpace(b.String())
}

func formatCommandSpecificHelpBlock(cmd registry.Command) string {
	switch strings.TrimSpace(cmd.CommandID) {
	case "events.create":
		return strings.TrimSpace(`Common authoring types:
  Communication: direct communication or important non-structured information
  - ` + "`message_posted`" + `
  Decisions: request or record decisions on the thread
  - ` + "`decision_needed`" + `
  - ` + "`decision_made`" + `
  Interventions: single clear path exists, but a human must act to complete it
  - ` + "`intervention_needed`" + `
  State and commitments: track state changes and commitments
  - ` + "`snapshot_updated`" + `
  - ` + "`commitment_created`" + `
  - ` + "`commitment_status_changed`" + `
  Exceptions: surface problems, risks, or escalations
  - ` + "`exception_raised`" + `

Usually emitted by higher-level commands:
  - ` + "`work_order_created`" + `: prefer ` + "`oar work-orders create`" + `
  - ` + "`receipt_added`" + `: prefer ` + "`oar receipts create`" + `
  - ` + "`review_completed`" + `: prefer ` + "`oar reviews create`" + `
  - ` + "`inbox_item_acknowledged`" + `: prefer ` + "`oar inbox ack`" + `

Specialized raw-event type:
  - ` + "`work_order_claimed`" + `: claim marker for work-order flows

Local CLI notes:
  - Common open ` + "`event.type`" + ` values include ` + "`actor_statement`" + `; the enum list above is illustrative, not exhaustive.
  - Use ` + "`--dry-run`" + ` with ` + "`--from-file`" + ` to validate and preview the request without sending the mutation.`)
	case "threads.timeline":
		return strings.TrimSpace(`Local CLI flags:
  --include-archived        Include archived events in the timeline.
  --archived-only           Show only archived events.
  --include-tombstoned      Include tombstoned events in the timeline.
  --tombstoned-only         Show only tombstoned events.

Note: by default, archived and tombstoned events are excluded from the timeline output.`)
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
		actorsSubcommandSpec,
		{
			command:  "auth",
			valid:    []string{"register"},
			examples: authSubcommandSpec.examples,
			aliases:  authSubcommandSpec.aliases,
		},
		authInvitesSubcommandSpec,
		authBootstrapSubcommandSpec,
		threadsSubcommandSpec,
		commitmentsSubcommandSpec,
		artifactsSubcommandSpec,
		boardsSubcommandSpec,
		boardsCardsSubcommandSpec,
		docsSubcommandSpec,
		docsRevisionSubcommandSpec,
		eventsSubcommandSpec,
		inboxSubcommandSpec,
		derivedSubcommandSpec,
		{
			command:  "meta",
			valid:    []string{"commands", "command", "concepts", "concept"},
			examples: metaSubcommandSpec.examples,
			aliases:  metaSubcommandSpec.aliases,
		},
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
	return paths
}

func onboardingHelpText() string {
	return strings.TrimSpace(`Onboarding: first steps

Use onboarding to get a working session quickly. For the fuller operating model, read ` + "`oar meta doc agent-guide`" + `.

1. Point the CLI at the core API with ` + "`--base-url`" + ` or ` + "`OAR_BASE_URL`" + `.
2. Register or select a reusable agent/profile with ` + "`--agent`" + `.
3. Confirm connectivity and identity with ` + "`oar doctor`" + ` and ` + "`oar auth whoami`" + `.
4. Run a cheap read command before any mutation.
5. Use ` + "`oar meta skill cursor`" + ` if you want a bundled Cursor skill file generated from the shipped guide.
6. Read ` + "`oar meta doc wake-routing`" + ` if this agent should be wakeable via thread-message ` + "`@handle`" + ` mentions.

First commands to run

  oar --base-url http://127.0.0.1:8000 --agent <agent> doctor
  oar --base-url http://127.0.0.1:8000 --agent <agent> auth bootstrap status
  oar --base-url http://127.0.0.1:8000 --agent <agent> auth register --username <username> --bootstrap-token <token>
  oar --agent <agent> auth whoami
  oar --agent <agent> threads list
  oar --agent <agent> inbox stream --max-events 1

Next step

  oar meta doc agent-guide
  oar meta doc wake-routing`)
}

func mapRuntimePathToRegistryPath(path string) string {
	parts := strings.Fields(strings.TrimSpace(path))
	if len(parts) == 0 {
		return ""
	}
	switch parts[0] {
	case "work-orders":
		parts[0] = "packets"
		parts = append([]string{"packets", "work-orders"}, parts[1:]...)
	case "receipts":
		parts = append([]string{"packets", "receipts"}, parts[1:]...)
	case "reviews":
		parts = append([]string{"packets", "reviews"}, parts[1:]...)
	}
	path = strings.Join(parts, " ")
	rewrites := map[string]string{
		"threads update":    "threads patch",
		"events tail":       "events stream",
		"inbox tail":        "inbox stream",
		"artifacts content": "artifacts content get",
		"meta commands":     "meta commands list",
		"meta command":      "meta commands get",
		"meta concepts":     "meta concepts list",
		"meta concept":      "meta concepts get",
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
	if len(parts) >= 2 && parts[0] == "packets" {
		switch parts[1] {
		case "work-orders", "receipts", "reviews":
			parts = append([]string{parts[1]}, parts[2:]...)
		}
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
	command = strings.ReplaceAll(command, "oar packets work-orders", "oar work-orders")
	command = strings.ReplaceAll(command, "oar packets receipts", "oar receipts")
	command = strings.ReplaceAll(command, "oar packets reviews", "oar reviews")
	command = strings.ReplaceAll(command, "oar auth agents register", "oar auth register")
	command = strings.ReplaceAll(command, "oar events stream", "oar events tail")
	command = strings.ReplaceAll(command, "oar inbox stream", "oar inbox tail")
	command = strings.ReplaceAll(command, "oar meta commands get", "oar meta command")
	command = strings.ReplaceAll(command, "oar meta commands list", "oar meta commands")
	command = strings.ReplaceAll(command, "oar meta concepts get", "oar meta concept")
	command = strings.ReplaceAll(command, "oar meta concepts list", "oar meta concepts")
	return command
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
			usage:    "oar auth whoami",
			examples: []string{"oar auth whoami", "oar --json auth whoami"},
		},
		"auth list": {
			summary:  "List local CLI profiles and identify the active one.",
			usage:    "oar auth list",
			examples: []string{"oar auth list", "oar --json auth list"},
		},
		"auth default": {
			summary:  "Persist the default profile used when no explicit agent is selected.",
			usage:    "oar auth default <profile>",
			examples: []string{"oar auth default agent-a", "oar --json auth default agent-a"},
		},
		"auth update-username": {
			summary:  "Update the authenticated agent username and sync the local profile copy.",
			usage:    "oar auth update-username --username <username>",
			examples: []string{"oar auth update-username --username renamed_agent"},
		},
		"auth rotate": {
			summary:  "Rotate the active agent key and refresh stored credentials.",
			usage:    "oar auth rotate",
			examples: []string{"oar auth rotate", "oar --json auth rotate"},
		},
		"auth revoke": {
			summary:  "Revoke the active agent and mark the local profile revoked.",
			usage:    "oar auth revoke",
			examples: []string{"oar auth revoke", "oar --json auth revoke"},
		},
		"auth token-status": {
			summary:  "Inspect whether the local profile still has refreshable token material.",
			usage:    "oar auth token-status",
			examples: []string{"oar auth token-status", "oar --json auth token-status"},
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
		b.WriteString("  If this agent should be wakeable by `@handle`, read `oar meta doc wake-routing`.\n")
	}
	b.WriteString("\n")
	b.WriteString(formatGlobalFlagUsage(topic))
	return strings.TrimSpace(b.String()), true
}
