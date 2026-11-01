// `anx artifacts` dispatch, including attachment create and artifact inspect.

package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runArtifactsAttachmentsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "artifacts attachments", errnorm.Usage("invalid_args", "expected a subcommand (e.g. `anx artifacts attachments create`)")
	}
	sub := strings.TrimSpace(strings.ToLower(args[0]))
	switch sub {
	case "create":
		result, callErr := a.runArtifactAttachmentCreateFlags(ctx, cfg, args[1:], "artifacts attachments create")
		return result, "artifacts attachments create", callErr
	default:
		return nil, "artifacts attachments", errnorm.Usage("unknown_command", fmt.Sprintf("unknown artifacts attachments subcommand %q", args[0]))
	}
}

func (a *App) runArtifactAttachmentCreateFlags(ctx context.Context, cfg config.Resolved, args []string, commandName string) (*commandResult, error) {
	fs := newSilentFlagSet(commandName)
	var refsJSON trackedString
	var refs trackedStrings
	var filePath trackedString
	var summary trackedString
	var artifactJSON trackedString
	var actorIDFlag trackedString
	fs.Var(&refsJSON, "refs", `JSON array of typed refs, e.g. ["topic:launch"]`)
	fs.Var(&refs, "ref", "Typed ref to attach to, repeatable")
	fs.Var(&filePath, "file", "Path to file to upload")
	fs.Var(&summary, "summary", "Optional attachment summary")
	fs.Var(&artifactJSON, "artifact", "Optional JSON object merged into artifact metadata")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if strings.TrimSpace(filePath.value) == "" {
		return nil, errnorm.Usage("invalid_request", "--file is required")
	}
	resolvedRefsJSON, err := artifactAttachmentRefsJSON(refsJSON.value, refs.values)
	if err != nil {
		return nil, err
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, err
	}
	return a.invokeArtifactAttachmentCreate(ctx, cfg, resolvedRefsJSON, filePath.value, summary.value, artifactJSON.value, actorID)
}

func artifactAttachmentRefsJSON(refsJSON string, refs []string) (string, error) {
	refsJSON = strings.TrimSpace(refsJSON)
	if refsJSON != "" && len(refs) > 0 {
		return "", errnorm.Usage("invalid_flags", "use either --ref or --refs, not both")
	}
	if len(refs) > 0 {
		encoded, err := json.Marshal(refs)
		if err != nil {
			return "", errnorm.Wrap(errnorm.KindLocal, "refs_encode_failed", "failed to encode refs", err)
		}
		return string(encoded), nil
	}
	if refsJSON == "" {
		return "", errnorm.Usage("invalid_request", "--ref is required (or pass --refs JSON)")
	}
	return refsJSON, nil
}

func (a *App) runArtifactsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "artifacts", artifactsSubcommandSpec.requiredError()
	}
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "attachments") {
		return a.runArtifactsAttachmentsCommand(ctx, args[1:], cfg)
	}
	sub := artifactsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("artifacts list")
		var kindFlag, backingScopeFlag, threadIDFlag, idsFlag, beforeFlag, afterFlag trackedString
		var includeTrashed bool
		var trashedOnly bool
		var includeArchived bool
		var archivedOnly bool
		registerRuntimeStringFlag(fs, &kindFlag, runtimeFlagSpec(artifactsListRuntimeFlags, "kind"))
		registerRuntimeStringFlag(fs, &backingScopeFlag, runtimeFlagSpec(artifactsListRuntimeFlags, "backing-scope"))
		registerRuntimeStringFlag(fs, &threadIDFlag, runtimeFlagSpec(artifactsListRuntimeFlags, "thread-id"))
		registerRuntimeStringFlag(fs, &idsFlag, runtimeFlagSpec(artifactsListRuntimeFlags, "ids"))
		registerRuntimeStringFlag(fs, &beforeFlag, runtimeFlagSpec(artifactsListRuntimeFlags, "created-before"))
		registerRuntimeStringFlag(fs, &afterFlag, runtimeFlagSpec(artifactsListRuntimeFlags, "created-after"))
		registerRuntimeBoolFlag(fs, &includeTrashed, runtimeFlagSpec(artifactsListRuntimeFlags, "include-trashed"))
		registerRuntimeBoolFlag(fs, &trashedOnly, runtimeFlagSpec(artifactsListRuntimeFlags, "trashed-only"))
		registerRuntimeBoolFlag(fs, &includeArchived, runtimeFlagSpec(artifactsListRuntimeFlags, "include-archived"))
		registerRuntimeBoolFlag(fs, &archivedOnly, runtimeFlagSpec(artifactsListRuntimeFlags, "archived-only"))
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts list", errnorm.Usage("invalid_flags", err.Error())
		}
		if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "artifacts list", err
		}
		if len(fs.Args()) > 0 {
			return nil, "artifacts list", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx artifacts list`")
		}
		resolvedThreadID := strings.TrimSpace(threadIDFlag.value)
		if resolvedThreadID != "" {
			resolved, err := a.resolveThreadIDFilters(ctx, cfg, []string{resolvedThreadID})
			if err != nil {
				return nil, "artifacts list", err
			}
			if len(resolved) > 0 {
				resolvedThreadID = resolved[0]
			}
		}
		query := make([]queryParam, 0, 6)
		if err := appendLifecycleStatesForHTTPList(&query, "", includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "artifacts list", err
		}
		addSingleQuery(&query, "kind", kindFlag.value)
		addSingleQuery(&query, "backing_scope", firstNonEmpty(strings.TrimSpace(backingScopeFlag.value), "all"))
		addSingleQuery(&query, "thread_id", resolvedThreadID)
		addSingleQuery(&query, "created_before", beforeFlag.value)
		addSingleQuery(&query, "created_after", afterFlag.value)
		if ids := strings.TrimSpace(idsFlag.value); ids != "" {
			addSingleQuery(&query, "ids", ids)
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "artifacts list", "artifacts.list", nil, query, nil)
		return result, "artifacts list", err
	case "get":
		result, callErr := a.runArtifactsInspectCommand(ctx, args[1:], cfg)
		return result, "artifacts inspect", callErr
	case "create":
		if artifactCreateUsesFileFlags(args[1:]) {
			result, callErr := a.runArtifactAttachmentCreateFlags(ctx, cfg, args[1:], "artifacts create")
			return addResourceURLToResult(cfg, "artifacts.attachments.create", result), "artifacts create", callErr
		}
		body, err := a.parseJSONBodyInput(args[1:], "artifacts create")
		if err != nil {
			return nil, "artifacts create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts create", "artifacts.create", nil, nil, body)
		return addResourceURLToResult(cfg, "artifacts.create", result), "artifacts create", callErr
	case "content", "download":
		commandName := "artifacts content"
		if sub == "download" {
			commandName = "artifacts download"
		}
		fs := newSilentFlagSet(commandName)
		var artifactIDFlag trackedString
		var outputPath string
		fs.Var(&artifactIDFlag, "artifact-id", "Artifact id")
		fs.StringVar(&outputPath, "o", "", "Write raw artifact bytes to file")
		fs.StringVar(&outputPath, "output", "", "Write raw artifact bytes to file")
		contentArgs, reorderErr := reorderArtifactContentFlags(args[1:])
		if reorderErr != nil {
			return nil, commandName, reorderErr
		}
		if err := fs.Parse(contentArgs); err != nil {
			return nil, commandName, errnorm.Usage("invalid_flags", err.Error())
		}
		positionals := fs.Args()
		id := strings.TrimSpace(artifactIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if len(positionals) > 0 {
			return nil, commandName, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
		}
		if err := validateID(id, "artifact id"); err != nil {
			return nil, commandName, err
		}
		result, callErr := a.invokeArtifactContentWithIDResolution(
			ctx,
			cfg,
			commandName,
			"artifact_id",
			id,
			artifactIDLookupSpec,
			strings.TrimSpace(outputPath),
		)
		return result, commandName, callErr
	case "inspect":
		result, callErr := a.runArtifactsInspectCommand(ctx, args[1:], cfg)
		return result, "artifacts inspect", callErr
	case "archive", "unarchive", "trash", "restore", "purge":
		spec, ok := lifecycleSpecFor("artifacts")
		if !ok {
			return nil, "artifacts", errnorm.Usage("internal_error", "missing lifecycle spec for artifacts")
		}
		return a.runLifecycleVerb(ctx, cfg, spec, sub, args[1:])
	default:
		return nil, "artifacts", artifactsSubcommandSpec.unknownError(args[0])
	}
}

func artifactCreateUsesFileFlags(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "--file" || strings.HasPrefix(arg, "--file=") || arg == "--ref" || strings.HasPrefix(arg, "--ref=") || arg == "--refs" || strings.HasPrefix(arg, "--refs=") {
			return true
		}
	}
	return false
}

func reorderArtifactContentFlags(args []string) ([]string, error) {
	knownValueFlags := map[string]struct{}{
		"-o":            {},
		"--output":      {},
		"--artifact-id": {},
	}
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "--output=") || strings.HasPrefix(arg, "--artifact-id=") || strings.HasPrefix(arg, "-o=") {
			flags = append(flags, arg)
			continue
		}
		if _, ok := knownValueFlags[arg]; ok {
			flags = append(flags, arg)
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return nil, errnorm.Usage("invalid_flags", fmt.Sprintf("flag provided but not defined: %s", arg))
		}
		positionals = append(positionals, arg)
	}
	return append(flags, positionals...), nil
}

func (a *App) runArtifactsInspectCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, err := parseResourceIDArg(args, "artifact-id", "artifact id", "artifact")
	if err != nil {
		return nil, err
	}

	metadataResult, callErr := a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"artifacts inspect",
		"artifacts.get",
		"artifact_id",
		id,
		artifactIDLookupSpec,
		nil,
		nil,
	)
	if callErr != nil {
		return nil, callErr
	}

	metadataData := asMap(metadataResult.Data)
	metadataBody := asMap(metadataData["body"])
	artifact := extractNestedMap(metadataBody, "artifact")
	artifactID := strings.TrimSpace(anyString(artifact["id"]))
	if artifactID == "" {
		artifactID = id
	}

	contentCfg := cfg
	contentCfg.JSON = true
	contentResult, contentErr := a.invokeArtifactContentWithIDResolution(
		ctx,
		contentCfg,
		"artifacts inspect",
		"artifact_id",
		artifactID,
		artifactIDLookupSpec,
		"",
	)
	if contentErr != nil {
		return nil, contentErr
	}

	contentData := asMap(contentResult.Data)
	contentBody := map[string]any{
		"status_code": contentData["status_code"],
		"headers":     contentData["headers"],
		"body_text":   contentData["body_text"],
		"body_base64": contentData["body_base64"],
	}
	if decoded, decodeErr := base64.StdEncoding.DecodeString(strings.TrimSpace(anyString(contentData["body_base64"]))); decodeErr == nil {
		contentBody["bytes"] = len(decoded)
	}

	body := map[string]any{
		"artifact": artifact,
		"content":  contentBody,
	}
	metadataData["body"] = body
	metadataResult.Data = metadataData
	metadataResult.Text = formatTypedCommandText(
		"artifacts.inspect",
		intValue(metadataData["status_code"]),
		headerValues(metadataData["headers"]),
		body,
		cfg.Verbose,
		cfg.Headers,
	)
	return metadataResult, nil
}
