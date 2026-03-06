package app

import (
	"context"
	"fmt"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/registry"
)

func (a *App) runMeta(_ context.Context, args []string, _ config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("meta"); ok {
			return &commandResult{Text: text}, "meta", nil
		}
		return nil, "meta", metaSubcommandSpec.requiredError()
	}
	sub := metaSubcommandSpec.normalize(args[0])
	switch sub {
	case "commands":
		result, err := a.runMetaCommands(args[1:])
		return result, "meta commands", err
	case "command":
		result, err := a.runMetaCommand(args[1:])
		return result, "meta command", err
	case "concepts":
		result, err := a.runMetaConcepts(args[1:])
		return result, "meta concepts", err
	case "concept":
		result, err := a.runMetaConcept(args[1:])
		return result, "meta concept", err
	default:
		return nil, "meta", metaSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runMetaCommands(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("meta commands")
	var groupFlag trackedString
	fs.Var(&groupFlag, "group", "Filter by top-level runtime command group")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar meta commands`")
	}

	meta, err := registry.LoadEmbedded()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindInternal, "registry_unavailable", "failed to load generated command metadata", err)
	}

	commands := make([]registry.Command, 0, len(meta.Commands))
	for _, cmd := range meta.Commands {
		if strings.TrimSpace(groupFlag.value) == "" {
			commands = append(commands, cmd)
			continue
		}
		runtimePath := runtimePathFromRegistryPath(cmd.CLIPath)
		parts := strings.Fields(runtimePath)
		if len(parts) == 0 {
			continue
		}
		if strings.TrimSpace(parts[0]) == strings.TrimSpace(groupFlag.value) {
			commands = append(commands, cmd)
		}
	}

	data := map[string]any{
		"openapi_version":    meta.OpenAPIVersion,
		"contract_version":   meta.ContractVersion,
		"generated_by":       meta.GeneratedBy,
		"extension_prefix":   meta.ExtensionPrefix,
		"command_count":      len(commands),
		"commands":           commands,
		"source":             "embedded-generated-registry",
		"group_filter":       strings.TrimSpace(groupFlag.value),
		"full_command_count": meta.CommandCount,
	}
	text := fmt.Sprintf("Generated commands: %d", len(commands))
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runMetaCommand(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("meta command")
	var idFlag trackedString
	var commandIDFlag trackedString
	fs.Var(&idFlag, "id", "Command id")
	fs.Var(&commandIDFlag, "command-id", "Command id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	commandID := firstNonEmpty(idFlag.value, commandIDFlag.value)
	if commandID == "" && len(positionals) > 0 {
		commandID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if commandID == "" {
		return nil, errnorm.Usage("invalid_request", "command id is required")
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar meta command`")
	}

	meta, err := registry.LoadEmbedded()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindInternal, "registry_unavailable", "failed to load generated command metadata", err)
	}
	cmd, ok := meta.CommandByID(commandID)
	if !ok {
		return nil, errnorm.Local("not_found", "command metadata not found")
	}
	if strings.TrimSpace(cmd.Why) == "" || len(cmd.Examples) == 0 {
		return nil, errnorm.Internal("registry_invalid", "generated command metadata is missing required why/examples fields")
	}

	text := formatGeneratedCommandHelp(runtimePathFromRegistryPath(cmd.CLIPath), cmd)
	return &commandResult{Text: text, Data: map[string]any{"command": cmd, "source": "embedded-generated-registry"}}, nil
}

func (a *App) runMetaConcepts(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("meta concepts")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar meta concepts`")
	}

	meta, err := registry.LoadEmbeddedConcepts()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindInternal, "registry_unavailable", "failed to load generated concepts metadata", err)
	}

	text := fmt.Sprintf("Generated concepts: %d", meta.ConceptCount)
	data := map[string]any{
		"openapi_version":  meta.OpenAPIVersion,
		"contract_version": meta.ContractVersion,
		"generated_by":     meta.GeneratedBy,
		"concept_count":    meta.ConceptCount,
		"concepts":         meta.Concepts,
		"source":           "embedded-generated-registry",
	}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runMetaConcept(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("meta concept")
	var nameFlag trackedString
	var conceptNameFlag trackedString
	fs.Var(&nameFlag, "name", "Concept name")
	fs.Var(&conceptNameFlag, "concept-name", "Concept name")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	conceptName := strings.ToLower(firstNonEmpty(nameFlag.value, conceptNameFlag.value))
	if conceptName == "" && len(positionals) > 0 {
		conceptName = strings.ToLower(strings.TrimSpace(positionals[0]))
		positionals = positionals[1:]
	}
	if conceptName == "" {
		return nil, errnorm.Usage("invalid_request", "concept name is required")
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar meta concept`")
	}

	meta, err := registry.LoadEmbeddedConcepts()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindInternal, "registry_unavailable", "failed to load generated concepts metadata", err)
	}
	for _, concept := range meta.Concepts {
		if strings.ToLower(strings.TrimSpace(concept.Name)) != conceptName {
			continue
		}
		text := fmt.Sprintf("Concept `%s` commands: %d", concept.Name, concept.CommandCount)
		return &commandResult{Text: text, Data: map[string]any{"concept": concept, "source": "embedded-generated-registry"}}, nil
	}
	return nil, errnorm.Local("not_found", "concept metadata not found")
}
