// Usage and help text for `anx draft` commands and the draft create wizard
// copy.

package app

import (
	"strings"
)

func draftUsageText() string {
	return strings.TrimSpace(`
Draft staging

Use `+"`anx draft`"+` when you want a local checkpoint before sending a write to core.

Choose the right path:

- Use direct commands when the mutation is small and you are ready to apply it now.
- Prefer command-specific proposal flows when they exist, such as `+"`docs revise`"+`, because they add domain-aware diff/review helpers.
- Use `+"`draft`"+` for lower-level commands, generic JSON bodies, or cases where you want to stage the exact request before commit.

Standard workflow

1. Build the exact payload for the target command.
2. Stage it with `+"`draft create`"+`.
3. Inspect staged drafts with `+"`draft list`"+`.
4. Commit when ready, or discard if the request should not be sent.

Usage:
  anx draft create --command <command-id> [--from-file <path>]
  anx draft list
  anx draft commit <draft-id> [--keep]
  anx draft discard <draft-id>

Heuristics

- Keep drafts short-lived; they are a checkpoint, not durable state.
- Prefer one clear intent per draft.
- Use `+"`--from-file`"+` or stdin for non-trivial JSON bodies so requests stay reproducible.
- Re-read current state before committing older drafts if the target may have changed.

Examples:
  cat payload.json | anx draft create --command topics.create
  anx draft list
  anx draft commit draft-20260305T103000-a1b2c3d4e5f6
`) + "\n"
}

func draftCreateHelpText(args []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(`
Draft create stages a write request locally for later commit.

Usage:
  anx draft create --command <command-id> [--from-file <path>] [--draft-id <id>]

Flags:
  --command <id|path>   Command ID or CLI path (for example, events.create)
  --from-file <path>    Read JSON body from file instead of stdin
  --draft-id <id>       Optional deterministic draft ID

Examples:
  cat payload.json | anx draft create --command events.create
  anx draft create --command topics.create --from-file topic.json
`))

	commandValue := draftCreateHelpCommandValue(args)
	if strings.TrimSpace(commandValue) == "" {
		return b.String() + "\n"
	}
	commandID, err := resolveDraftCommandID(commandValue)
	if err != nil {
		b.WriteString("\n\nTarget command hint: " + strings.TrimSpace(commandValue) + " (unrecognized)")
		return b.String() + "\n"
	}
	cmd, ok := generatedCommandByID(commandID)
	if !ok {
		b.WriteString("\n\nTarget command hint: " + commandID + " (metadata unavailable)")
		return b.String() + "\n"
	}
	b.WriteString("\n\nTarget command: " + commandID)
	if path := strings.TrimSpace(runtimePathFromRegistryPath(cmd.CLIPath)); path != "" {
		b.WriteString(" (`" + path + "`)")
	}
	if schemaBlock := formatInputSchemaBlock(cmd); strings.TrimSpace(schemaBlock) != "" {
		b.WriteString("\n\n")
		b.WriteString(schemaBlock)
	}
	if extra := formatCommandSpecificHelpBlock(cmd); strings.TrimSpace(extra) != "" {
		b.WriteString("\n\n")
		b.WriteString(extra)
	}
	return b.String() + "\n"
}

func draftCreateHelpCommandValue(args []string) string {
	value := ""
	for idx := 0; idx < len(args); idx++ {
		arg := strings.TrimSpace(args[idx])
		switch {
		case arg == "--command" && idx+1 < len(args):
			value = strings.TrimSpace(args[idx+1])
			idx++
		case strings.HasPrefix(arg, "--command="):
			value = strings.TrimSpace(strings.TrimPrefix(arg, "--command="))
		}
	}
	return value
}

func stripDraftCreateHelpFlags(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	helpRequested := false
	expectsValue := false
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if expectsValue {
			filtered = append(filtered, arg)
			expectsValue = false
			continue
		}
		switch trimmed {
		case "--command", "--from-file", "--draft-id":
			expectsValue = true
			filtered = append(filtered, arg)
			continue
		case "-h", "--help":
			helpRequested = true
			continue
		}
		if strings.HasPrefix(trimmed, "--help=") || strings.HasPrefix(trimmed, "-h=") {
			value := strings.TrimSpace(strings.SplitN(trimmed, "=", 2)[1])
			switch strings.ToLower(value) {
			case "1", "t", "true", "y", "yes", "on":
				helpRequested = true
			}
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered, helpRequested
}
