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

var runtimeGeneratedTopics = []runtimeHelpTopic{
	{Path: "threads", Description: "Manage thread resources"},
	{Path: "commitments", Description: "Manage commitment resources"},
	{Path: "artifacts", Description: "Manage artifact resources and content"},
	{Path: "events", Description: "Manage events and event streams"},
	{Path: "inbox", Description: "List/ack/stream inbox items"},
	{Path: "work-orders", Description: "Create work-order packets"},
	{Path: "receipts", Description: "Create receipt packets"},
	{Path: "reviews", Description: "Create review packets"},
	{Path: "derived", Description: "Run derived-view maintenance actions"},
	{Path: "meta", Description: "Inspect generated command/concept metadata"},
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
	b.WriteString("\nTip: `oar help <command path>` for full command-level generated details.\n")
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
	return strings.TrimSpace(b.String())
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
		"events.get":                 {},
		"events.create":              {},
		"events.stream":              {},
		"inbox.list":                 {},
		"inbox.ack":                  {},
		"inbox.stream":               {},
		"packets.work-orders.create": {},
		"packets.receipts.create":    {},
		"packets.reviews.create":     {},
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
2. Read current state before mutating it: ` + "`oar threads get <thread-id>`" + ` and related list/get commands.
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
	meta, err := registry.LoadEmbedded()
	if err != nil {
		return strings.TrimSpace(commandID)
	}
	cmd, ok := meta.CommandByID(commandID)
	if !ok {
		return strings.TrimSpace(commandID)
	}
	return strings.TrimSpace(cmd.CLIPath)
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
