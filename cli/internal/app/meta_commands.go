package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/registry"
)

func (a *App) runMeta(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("meta"); ok {
			return &commandResult{Text: text}, "meta", nil
		}
		return nil, "meta", metaSubcommandSpec.requiredError()
	}
	sub := metaSubcommandSpec.normalize(args[0])
	switch sub {
	case "health":
		result, err := a.runMetaUtility(ctx, cfg, "meta health", "meta.health")
		return result, "meta health", err
	case "livez":
		result, err := a.runMetaRawUtility(ctx, cfg, "meta livez", "/livez")
		return result, "meta livez", err
	case "readyz":
		result, err := a.runMetaUtility(ctx, cfg, "meta readyz", "meta.readyz")
		return result, "meta readyz", err
	case "version":
		result, err := a.runMetaUtility(ctx, cfg, "meta version", "meta.version")
		return result, "meta version", err
	case "handshake":
		result, err := a.runMetaUtility(ctx, cfg, "meta handshake", "meta.handshake")
		return result, "meta handshake", err
	case "ops":
		result, name, err := a.runMetaOps(ctx, cfg, args[1:])
		return result, name, err
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
	case "docs":
		result, err := a.runMetaDocs(args[1:])
		return result, "meta docs", err
	case "doc":
		result, err := a.runMetaDoc(args[1:])
		return result, "meta doc", err
	case "skill":
		result, err := a.runMetaSkill(args[1:])
		return result, "meta skill", err
	default:
		return nil, "meta", metaSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runMetaUtility(ctx context.Context, cfg config.Resolved, commandName string, commandID string) (*commandResult, error) {
	return a.invokeTypedJSON(ctx, cfg, commandName, commandID, nil, nil, nil)
}

func (a *App) runMetaRawUtility(ctx context.Context, cfg config.Resolved, commandName string, path string) (*commandResult, error) {
	client, err := httpclient.New(cfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	callCtx, cancel := httpclient.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	resp, callErr := client.RawCall(callCtx, httpclient.RawRequest{Method: "GET", Path: path})
	if callErr != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", commandName+" request failed", callErr)
	}
	parsedBody := parseResponseBody(resp.Body)
	data := map[string]any{
		"status_code": resp.StatusCode,
		"headers":     normalizedHeaders(resp.Headers),
		"body":        parsedBody,
	}
	text := fmt.Sprintf("%s status: %d", commandName, resp.StatusCode)
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runMetaOps(ctx context.Context, cfg config.Resolved, args []string) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "meta ops", metaOpsSubcommandSpec.requiredError()
	}
	switch metaOpsSubcommandSpec.normalize(args[0]) {
	case "health":
		if len(args) > 1 {
			return nil, "meta ops health", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx meta ops health`")
		}
		result, err := a.runMetaRawUtility(ctx, cfg, "meta ops health", "/ops/health")
		return result, "meta ops health", err
	default:
		return nil, "meta ops", metaOpsSubcommandSpec.unknownError(args[0])
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
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx meta commands`")
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
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx meta command`")
	}

	meta, err := registry.LoadEmbedded()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindInternal, "registry_unavailable", "failed to load generated command metadata", err)
	}
	cmd, ok := meta.CommandByID(commandID)
	if !ok {
		return nil, errnorm.Local("not_found", "command metadata not found")
	}
	if strings.TrimSpace(cmd.Why) == "" {
		return nil, errnorm.Internal("registry_invalid", "generated command metadata is missing required why field")
	}

	text := formatGeneratedCommandHelp(runtimePathFromRegistryPath(cmd.CLIPath), cmd, true)
	return &commandResult{Text: text, Data: map[string]any{"command": cmd, "source": "embedded-generated-registry"}}, nil
}

func (a *App) runMetaConcepts(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("meta concepts")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx meta concepts`")
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
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx meta concept`")
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

func (a *App) runMetaDocs(args []string) (*commandResult, error) {
	if len(args) > 0 && isHelpToken(args[0]) {
		return &commandResult{Text: metaDocsUsageText()}, nil
	}

	fs := newSilentFlagSet("meta docs")
	var writeDir trackedString
	var listFlag trackedBool
	var searchFlag trackedString
	fs.Var(&writeDir, "write-dir", "Write bundled runtime help docs to the target directory")
	fs.Var(&listFlag, "list", "List bundled runtime help topics")
	fs.Var(&searchFlag, "search", "Search bundled runtime help topics")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx meta docs`")
	}
	if listFlag.value && strings.TrimSpace(searchFlag.value) != "" {
		return nil, errnorm.Usage("invalid_flags", "use either --list or --search, not both")
	}
	if strings.TrimSpace(writeDir.value) != "" && (listFlag.value || strings.TrimSpace(searchFlag.value) != "") {
		return nil, errnorm.Usage("invalid_flags", "use --write-dir only with full `anx meta docs` output, not --list or --search")
	}
	if listFlag.value {
		topics := runtimeHelpDocTopics()
		return &commandResult{Text: RuntimeHelpDocsIndexMarkdown(topics), Data: map[string]any{
			"topics": topics,
			"source": "runtime-help-catalog",
			"count":  len(topics),
		}}, nil
	}
	if strings.TrimSpace(searchFlag.value) != "" {
		matches := SearchRuntimeHelpDocTopics(searchFlag.value)
		text := RuntimeHelpDocsIndexMarkdown(matches)
		if len(matches) == 0 {
			text = "No runtime help topics matched: " + strings.TrimSpace(searchFlag.value)
		}
		return &commandResult{Text: text, Data: map[string]any{
			"query":   strings.TrimSpace(searchFlag.value),
			"topics":  matches,
			"source":  "runtime-help-catalog",
			"matches": len(matches),
		}}, nil
	}

	markdown, err := RuntimeHelpDocsMarkdown()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindInternal, "runtime_docs_unavailable", "failed to build shipped runtime docs", err)
	}

	data := map[string]any{
		"markdown":    markdown,
		"source":      "runtime-help-catalog",
		"topic_count": len(runtimeHelpDocTopics()),
	}
	if strings.TrimSpace(writeDir.value) != "" {
		writtenPath, err := WriteRuntimeHelpDocs(strings.TrimSpace(writeDir.value))
		if err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "runtime_docs_write_failed", "failed to write runtime docs", err)
		}
		data["write_dir"] = strings.TrimSpace(writeDir.value)
		data["written_files"] = []string{filepath.Clean(writtenPath)}
	}

	return &commandResult{Text: markdown, Data: data}, nil
}

func (a *App) runMetaDoc(args []string) (*commandResult, error) {
	if len(args) > 0 && isHelpToken(args[0]) {
		return &commandResult{Text: metaDocUsageText()}, nil
	}

	fs := newSilentFlagSet("meta doc")
	var topicFlag trackedString
	fs.Var(&topicFlag, "topic", "Help topic path")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	topic := strings.TrimSpace(topicFlag.value)
	if topic == "" && len(positionals) > 0 {
		topic = strings.Join(positionals, " ")
		positionals = nil
	}
	if topic == "" {
		return nil, errnorm.Usage("invalid_request", "topic is required for `anx meta doc`")
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx meta doc`")
	}

	markdown, err := RuntimeHelpDocMarkdown(topic)
	if err != nil {
		return nil, errnorm.Local("not_found", err.Error())
	}
	return &commandResult{Text: markdown, Data: map[string]any{
		"topic":    strings.Join(strings.Fields(topic), " "),
		"markdown": markdown,
		"source":   "runtime-help-catalog",
	}}, nil
}

func (a *App) runMetaSkill(args []string) (*commandResult, error) {
	if len(args) > 0 && isHelpToken(args[0]) {
		return &commandResult{Text: metaSkillUsageText()}, nil
	}

	fs := newSilentFlagSet("meta skill")
	var targetFlag trackedString
	var writeFile trackedString
	var writeDir trackedString
	fs.Var(&targetFlag, "target", "Skill target, for example anx")
	fs.Var(&writeFile, "write-file", "Write the rendered skill to this exact path")
	fs.Var(&writeDir, "write-dir", "Write the rendered skill into this directory")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	target := strings.TrimSpace(targetFlag.value)
	if target == "" && len(positionals) > 0 {
		target = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
		if len(positionals) > 0 {
			trailing := newSilentFlagSet("meta skill")
			trailing.Var(&writeFile, "write-file", "Write the rendered skill to this exact path")
			trailing.Var(&writeDir, "write-dir", "Write the rendered skill into this directory")
			if err := trailing.Parse(positionals); err != nil {
				return nil, errnorm.Usage("invalid_flags", err.Error())
			}
			positionals = trailing.Args()
		}
	}
	if target == "" {
		return nil, errnorm.Usage("invalid_request", "skill target is required for `anx meta skill`")
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx meta skill`")
	}

	var (
		content         string
		defaultFileName string
	)
	normalizedTarget := normalizeSkillTarget(target)
	switch normalizedTarget {
	case "anx":
		content = renderOpinionatedANXSkillMarkdown()
		defaultFileName = "SKILL.md"
	default:
		return nil, errnorm.Local("not_found", "unknown skill target")
	}

	data := map[string]any{
		"target":       normalizedTarget,
		"content":      content,
		"default_file": defaultFileName,
		"source":       "bundled-agent-guide",
		"guide_topic":  "agent-guide",
		"skill_name":   agentGuideSkillName,
	}
	writtenPath, err := writeRenderedFile(content, writeFile.value, writeDir.value, defaultFileName)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "failed to write rendered skill", err)
	}
	if writtenPath != "" {
		data["written_files"] = []string{writtenPath}
	}
	return &commandResult{Text: content, Data: data}, nil
}

func normalizeSkillTarget(target string) string {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "", "anx", "agent-nexus":
		return "anx"
	case "cursor":
		return "anx"
	default:
		return strings.ToLower(strings.TrimSpace(target))
	}
}

func (a *App) runInstallCommand(args []string) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: installUsageText()}, "install", nil
	}
	switch strings.TrimSpace(args[0]) {
	case "skill":
		result, err := a.runInstallSkill(args[1:])
		return result, "install skill", err
	default:
		return nil, "install", installSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runInstallSkill(args []string) (*commandResult, error) {
	if len(args) > 0 && isHelpToken(args[0]) {
		return &commandResult{Text: installSkillUsageText()}, nil
	}
	fs := newSilentFlagSet("install skill")
	var pathFlag trackedString
	var writeFileFlag trackedString
	var forceFlag trackedBool
	fs.Var(&pathFlag, "path", "Destination file path")
	fs.Var(&writeFileFlag, "write-file", "Compatibility spelling for --path")
	fs.Var(&forceFlag, "force", "Overwrite an existing destination file")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	path := firstNonEmpty(pathFlag.value, writeFileFlag.value)
	if strings.TrimSpace(path) == "" && len(positionals) > 0 {
		path = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if strings.TrimSpace(path) == "" {
		return nil, errnorm.Usage("invalid_request", "`anx install skill` requires --path <file>")
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx install skill`")
	}
	if _, err := os.Stat(strings.TrimSpace(path)); err == nil && !forceFlag.value {
		return nil, errnorm.Usage("file_exists", "`anx install skill` refuses to overwrite an existing file; pass --force to replace it")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "failed to inspect rendered skill destination", err)
	}
	content := renderOpinionatedANXSkillMarkdown()
	writtenPath, err := writeRenderedFile(content, strings.TrimSpace(path), "", "SKILL.md")
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "skill_write_failed", "failed to write rendered skill", err)
	}
	text := strings.Join([]string{
		"Installed ANX agent skill.",
		"Path: " + writtenPath,
		"Next: read the installed skill, then run `anx workspace summary` before starting durable workspace work.",
	}, "\n")
	return &commandResult{Text: text, Data: map[string]any{
		"path":          writtenPath,
		"content":       content,
		"written_files": []string{writtenPath},
		"source":        "bundled-agent-guide",
		"guide_topic":   "agent-guide",
		"skill_name":    agentGuideSkillName,
	}}, nil
}

func metaDocsUsageText() string {
	return strings.TrimSpace(`Shipped runtime docs reference

Usage:
  anx meta docs [--write-dir <dir>]
  anx meta docs --list
  anx meta docs --search <query>

Print the bundled Markdown reference built from the same runtime help catalog used by ` + "`anx help`" + `.

Options:
  --write-dir <dir>   Write ` + "`runtime-help.md`" + ` into the target directory.
  --list              List available topic names with summaries.
  --search <query>    Search topic names, summaries, and bundled help text.

Examples:
  anx meta docs
  anx meta docs --list
  anx meta docs --search profile
  anx meta docs --write-dir ./docs/generated`)
}

func metaDocUsageText() string {
	return strings.TrimSpace(`Shipped runtime doc topic

Usage:
  anx meta doc <topic>
  anx meta doc --topic <topic>

Print one bundled Markdown topic from the runtime help catalog.

Examples:
  anx meta doc agent-guide
  anx meta doc profiles
  anx meta doc env
  anx meta doc "docs trash"`)
}

func metaSkillUsageText() string {
	return strings.TrimSpace(`Shipped agent skill export

Usage:
  anx meta skill <target> [--write-file <path> | --write-dir <dir>]
  anx meta skill --target <target> [--write-file <path> | --write-dir <dir>]

Render the bundled opinionated ANX agent skill.

Targets:
  anx                    Render the generic opinionated ANX ` + "`SKILL.md`" + ` file.
  cursor                 Compatibility alias for ` + "`anx`" + `.

Options:
  --write-file <path>    Write the rendered skill to this exact path.
  --write-dir <dir>      Write the rendered skill into this directory using its default filename.

Examples:
  anx meta skill anx
  anx meta skill anx --write-file ./SKILL.md
  anx meta skill --target cursor --write-file ./SKILL.md`)
}

func installUsageText() string {
	return strings.TrimSpace(`Install local ANX helper artifacts

Usage:
  anx install skill --path <file>

Subcommands:
  skill   Install the bundled opinionated ANX agent skill to a file path.

Examples:
  anx install skill --path ./SKILL.md`)
}

func installSkillUsageText() string {
	return strings.TrimSpace(`Install the bundled opinionated ANX agent skill

Usage:
  anx install skill --path <file>
  anx install skill <file>
  anx install skill --path <file> --force

Writes a generic ` + "`SKILL.md`" + `-compatible Markdown file that teaches agents how to use ANX as the default durable tracker for topics, cards, docs, asks, and collaboration.

Options:
  --path <file>          Destination file path.
  --write-file <file>    Compatibility spelling for --path.
  --force                Overwrite an existing destination file.

Examples:
  anx install skill --path ./SKILL.md
  anx install skill ~/.codex/skills/anx-opinionated-onboarding/SKILL.md`)
}
