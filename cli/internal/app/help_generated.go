package app

import (
	"fmt"
	"io"
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
	{Path: "threads", Description: "Manage thread resources"},
	{Path: "commitments", Description: "Manage commitment resources"},
	{Path: "artifacts", Description: "Manage artifact resources and content"},
	{Path: "docs", Description: "Manage long-lived docs and revisions"},
	{Path: "events", Description: "Manage events and event streams"},
	{Path: "inbox", Description: "List/get/ack/stream inbox items"},
	{Path: "work-orders", Description: "Create work-order packets"},
	{Path: "receipts", Description: "Create receipt packets"},
	{Path: "reviews", Description: "Create review packets"},
	{Path: "derived", Description: "Run derived-view maintenance actions"},
	{Path: "meta", Description: "Inspect generated command/concept metadata"},
}

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
		Path:        "threads recommendations",
		Summary:     "Review one thread's recommendation/decision inputs with provenance and follow-up event inspection hints.",
		JSONShape:   "`thread`, `recommendations`, `decision_requests`, `decisions`, `pending_decisions`, `follow_up`",
		Composition: "Resolves one thread by id or discovery filters, loads `threads context`, and adds thread-scoped pending decision inbox items from `inbox list`.",
		Examples: []string{
			"oar threads recommendations --thread-id <thread-id> --full-id",
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
			{Name: "--full-summary", Description: "Show full recommendation/decision summaries in human output."},
			{Name: "--full-id", Description: "Render full event and inbox ids in human output."},
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

func (a *App) printRootUsage() {
	_, _ = io.WriteString(a.Stdout, strings.TrimSpace(`oar - Organization Autorunner CLI

Usage:
  oar [global flags] <command>

Core Commands:
  version       Print CLI/runtime version details
  doctor        Validate local and network preconditions
  auth          Manage agent registration, profile auth, and token lifecycle
  draft         Stage write requests locally and commit them later
  provenance    Walk refs/provenance links as a deterministic graph
  api call      Perform an arbitrary HTTP API request
  help [topic]  Show onboarding help or generated command help
`)+"\n")

	meta, err := registry.LoadEmbedded()
	if err == nil {
		_, _ = io.WriteString(a.Stdout, "\nGenerated Command Groups:\n")
		for _, topic := range runtimeGeneratedTopics {
			count := len(runtimeCommandsForTopic(meta, topic.Path))
			if count == 0 {
				continue
			}
			_, _ = io.WriteString(a.Stdout, fmt.Sprintf("  %-12s %s (%d)\n", topic.Path, topic.Description, count))
		}
	}

	_, _ = io.WriteString(a.Stdout, strings.TrimSpace(`

Onboarding:
  `+"`oar help onboarding`"+` for the offline quick-start topic.

Global Flags:
  --json
  --base-url <url>
  --agent <name>
  --no-color
  --verbose
  --headers
  --timeout <duration>
`)+"\n")
}

func (a *App) printHelpTopic(topic string) bool {
	topic = strings.TrimSpace(topic)
	if topic == "draft" {
		_, _ = io.WriteString(a.Stdout, draftUsageText())
		return true
	}
	if topic == "onboarding" {
		_, _ = io.WriteString(a.Stdout, onboardingHelpText())
		return true
	}
	if topic == "provenance" || topic == "provenance walk" {
		_, _ = io.WriteString(a.Stdout, provenanceUsageText()+"\n")
		return true
	}
	text, ok := generatedHelpText(topic)
	if !ok {
		return false
	}
	_, _ = io.WriteString(a.Stdout, text+"\n")
	return true
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
  threads inspect             Compose one thread coordination view from context + inbox in one command.
  threads recommendations     Focus recommendation/decision review with actor+timestamp provenance.
  Tip: start with ` + "`oar threads inspect`" + ` for one initiative/thread, use ` + "`--status/--tag/--type initiative`" + ` to discover one thread, use ` + "`oar threads context`" + ` for cross-thread aggregates, and ` + "`oar threads get`" + ` for raw snapshot-only reads. Add ` + "`--full-id`" + ` for copy/paste ids.`)
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
  docs validate-update     Validate a docs.update payload from stdin/--from-file.
  Tip: add ` + "`--content-file <path>`" + ` to avoid hand-escaping multiline content.`)
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
  Place global flags before the command path.
  Example: oar --json %s ...
  Available: --json, --base-url <url>, --agent <name>, --no-color, --verbose, --headers, --timeout <duration>`, path))
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
	return map[string]struct{}{
		"threads.list":               {},
		"threads.get":                {},
		"threads.create":             {},
		"threads.patch":              {},
		"threads.timeline":           {},
		"commitments.list":           {},
		"commitments.get":            {},
		"commitments.create":         {},
		"commitments.patch":          {},
		"artifacts.list":             {},
		"artifacts.get":              {},
		"artifacts.create":           {},
		"artifacts.content.get":      {},
		"docs.create":                {},
		"docs.get":                   {},
		"docs.update":                {},
		"docs.history":               {},
		"docs.revision.get":          {},
		"events.get":                 {},
		"events.create":              {},
		"events.stream":              {},
		"inbox.list":                 {},
		"inbox.get":                  {},
		"inbox.ack":                  {},
		"inbox.stream":               {},
		"packets.work-orders.create": {},
		"packets.receipts.create":    {},
		"packets.reviews.create":     {},
		"threads.context":            {},
		"derived.rebuild":            {},
		"meta.commands.list":         {},
		"meta.commands.get":          {},
		"meta.concepts.list":         {},
		"meta.concepts.get":          {},
	}
}

func onboardingHelpText() string {
	return strings.TrimSpace(`Onboarding: mental model

1. ` + "`oar`" + ` is a non-interactive CLI that maps stable command paths to core HTTP endpoints and emits plain text or a single JSON envelope.
2. Each command should be safe for automation, so defaults, errors, and output shapes are designed for scripts first.
3. Profiles (` + "`--agent`" + `) hold reusable auth and base URL settings so repeated commands stay short and consistent.
4. Typed commands (` + "`threads`" + `, ` + "`events`" + `, ` + "`inbox`" + `, and packet creators) are the primary surface, while ` + "`api call`" + ` is the escape hatch.
5. The fastest way to stay aligned is to run health/auth checks first, then execute the work-order loop one step at a time.

Work-order loop

1. Inspect inbound work and context: ` + "`oar inbox list`" + ` or ` + "`oar inbox stream --max-events 1`" + `.
2. Read current state before mutating it: ` + "`oar threads inspect --thread-id <thread-id>`" + `.
   Use ` + "`oar threads context`" + ` for cross-thread aggregates and ` + "`oar threads get`" + ` for raw snapshot-only reads.
3. Stage a mutation as a draft when you need reviewable intent: ` + "`oar draft create --command <command-id>`" + `.
4. Commit the draft (or send a direct typed create/patch command) and capture returned IDs.
5. Confirm outcomes in timeline/events and ack inbox items to close the loop.

First 5 commands to run

  oar --base-url http://127.0.0.1:8000 --agent <agent> doctor
  oar --base-url http://127.0.0.1:8000 --agent <agent> auth register --username <username>
  oar --agent <agent> auth whoami
  oar --agent <agent> threads list
  oar --agent <agent> inbox stream --max-events 1

Optional full runbook (local, offline)

  cli/docs/runbook.md`)
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
		"threads update":     "threads patch",
		"commitments update": "commitments patch",
		"events tail":        "events stream",
		"inbox tail":         "inbox stream",
		"artifacts content":  "artifacts content get",
		"meta commands":      "meta commands list",
		"meta command":       "meta commands get",
		"meta concepts":      "meta concepts list",
		"meta concept":       "meta concepts get",
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
		"commitments patch":  "commitments update",
		"meta commands list": "meta commands",
		"meta commands get":  "meta command",
		"meta concepts list": "meta concepts",
		"meta concepts get":  "meta concept",
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
	command = strings.ReplaceAll(command, "oar events stream", "oar events tail")
	command = strings.ReplaceAll(command, "oar inbox stream", "oar inbox tail")
	command = strings.ReplaceAll(command, "oar meta commands get", "oar meta command")
	command = strings.ReplaceAll(command, "oar meta commands list", "oar meta commands")
	command = strings.ReplaceAll(command, "oar meta concepts get", "oar meta concept")
	command = strings.ReplaceAll(command, "oar meta concepts list", "oar meta concepts")
	return command
}
