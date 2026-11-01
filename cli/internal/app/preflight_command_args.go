// Command-specific argument preflight for commands whose subcommands or flags
// need custom checks beyond the spec tables (skills, threads, workspace,
// docs ingest, config, config forge).

package app

import (
	"fmt"
	"strings"

	"agent-nexus-cli/internal/errnorm"
)

func preflightSkillsArgs(args []string) error {
	if err := preflightSubcommand(args, skillsSubcommandSpec); err != nil {
		return err
	}
	if len(args) == 0 {
		return nil
	}
	command := skillsSubcommandSpec.normalize(args[0])
	return preflightFlagUsage(args[1:], skillsPreflightFlagSpecs()["skills "+command])
}

func preflightThreadsSubcommand(args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		return nil
	}
	if strings.TrimSpace(args[0]) == "patch" {
		return nil
	}
	return preflightSubcommand(args, threadsSubcommandSpec)
}

func preflightWorkspaceSubcommand(args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		return nil
	}
	if args[0] == "dashboard" {
		if len(args) >= 2 && args[1] == "pins" {
			return validateDashboardPinsArgs(args[2:])
		}

		if len(args) == 2 && args[1] == "list" {
			return nil
		}
		if len(args) != 3 || args[1] != "set" || strings.HasPrefix(args[2], "-") {
			return errnorm.Usage("invalid_args", "usage: anx workspace dashboard set <doc|none>")
		}
		return nil
	}
	sub := strings.TrimSpace(args[0])
	if sub == "summary" {
		return nil
	}
	return errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown workspace subcommand %q; valid subcommands: summary; examples: `anx workspace summary`", sub))
}

func preflightDocsIngestArgs(args []string) error {
	hasPath := false
	hasSource := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if arg == "--source" {
			hasSource = true
			if i+1 < len(args) {
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--source=") {
			hasSource = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		hasPath = true
	}
	if !hasPath {
		return errnorm.Usage("invalid_request", "path is required; pass a directory of markdown files")
	}
	if !hasSource {
		return errnorm.Usage("invalid_request", "`--source` is required; pass a URL prefix joined with each relative path")
	}
	return nil
}

func preflightConfigArgs(args []string) error {
	if len(args) == 0 {
		return nil
	}
	if args[0] == "ask-delivery" {
		if len(args) > 2 {
			return errnorm.Usage("invalid_args", "use anx help config ask-delivery for command arguments")
		}
		return nil
	}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			return errnorm.Usage("invalid_flags", "config commands accept positional arguments only")
		}
	}
	if args[0] == "forge" {
		return preflightConfigForgeArgs(args[1:])
	}
	expected := map[string]int{"show": 0, "workspaces": 0, "use": 1, "map": 2, "unmap": 1}
	if len(args)-1 != expected[args[0]] {
		return errnorm.Usage("invalid_args", "use anx help config "+args[0]+" for command arguments")
	}
	return nil
}

func preflightConfigForgeArgs(args []string) error {
	if len(args) == 0 {
		return configForgeSubcommandSpec.requiredError()
	}
	bounds := map[string][2]int{"add": {3, 3}, "remove": {2, 2}, "list": {0, 1}}
	count, ok := bounds[args[0]]
	if !ok {
		return configForgeSubcommandSpec.unknownError(args[0])
	}
	if given := len(args) - 1; given < count[0] || given > count[1] {
		return errnorm.Usage("invalid_args", "use anx help config forge "+args[0]+" for command arguments")
	}
	return nil
}
