// `anx threads` dispatch plus the thread/workspace context selection parsing
// that thread-scoped subcommands share.

package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

type threadContextSelection struct {
	threadIDs              []string
	discoveryQuery         []queryParam
	maxEventsSet           bool
	maxEvents              int
	includeArtifactContent bool
	fullID                 bool
}

type threadWorkspaceSelection struct {
	threadContextSelection
}

func (a *App) runThreadsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "threads", threadsSubcommandSpec.requiredError()
	}
	sub := threadsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("threads list")
		var stateFlag, queryFlag, cursorFlag trackedString
		var limitFlag trackedInt
		var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
		registerRuntimeStringFlag(fs, &stateFlag, runtimeFlagSpec(threadsListRuntimeFlags, "state"))
		registerRuntimeStringFlag(fs, &queryFlag, runtimeFlagSpec(threadsListRuntimeFlags, "q"))
		registerRuntimeIntFlag(fs, &limitFlag, runtimeFlagSpec(threadsListRuntimeFlags, "limit"))
		registerRuntimeStringFlag(fs, &cursorFlag, runtimeFlagSpec(threadsListRuntimeFlags, "cursor"))
		registerRuntimeBoolFlag(fs, &includeArchived, runtimeFlagSpec(threadsListRuntimeFlags, "include-archived"))
		registerRuntimeBoolFlag(fs, &archivedOnly, runtimeFlagSpec(threadsListRuntimeFlags, "archived-only"))
		registerRuntimeBoolFlag(fs, &includeTrashed, runtimeFlagSpec(threadsListRuntimeFlags, "include-trashed"))
		registerRuntimeBoolFlag(fs, &trashedOnly, runtimeFlagSpec(threadsListRuntimeFlags, "trashed-only"))
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "threads list", errnorm.Usage("invalid_flags", err.Error())
		}
		if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "threads list", err
		}
		if len(fs.Args()) > 0 {
			return nil, "threads list", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx threads list`")
		}
		if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
			return nil, "threads list", errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
		}
		query := make([]queryParam, 0, 8)
		if err := appendLifecycleStatesForHTTPList(&query, stateFlag.value, includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "threads list", err
		}
		addSingleQuery(&query, "q", queryFlag.value)
		if limitFlag.set {
			addSingleQuery(&query, "limit", strconv.Itoa(limitFlag.value))
		}
		addSingleQuery(&query, "cursor", cursorFlag.value)
		result, err := a.invokeTypedJSON(ctx, cfg, "threads list", "threads.list", nil, query, nil)
		return result, "threads list", err
	case "get":
		id, err := parseResourceIDArg(args[1:], "thread-id", "thread id", "thread")
		if err != nil {
			return nil, "threads get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"threads get",
			"threads.inspect",
			"thread_id",
			id,
			threadIDLookupSpec,
			nil,
			nil,
		)
		return result, "threads get", callErr
	case "message":
		body, target, dryRun, err := a.parseThreadMessageInput(ctx, args[1:], cfg, "threads message", "")
		if err != nil {
			return nil, "threads message", err
		}
		if err := validateEventsCreateInput(body, "threads message"); err != nil {
			return nil, "threads message", err
		}
		if dryRun {
			return dryRunResult("threads message", "events.create", nil, nil, body), "threads message", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "threads message", "events.create", nil, nil, body)
		return decorateThreadMessageWriteResult(result, callErr, cfg, "threads.message", target), "threads message", callErr
	case "reply":
		body, target, dryRun, err := a.parseThreadReplyInput(ctx, args[1:], cfg, "threads reply")
		if err != nil {
			return nil, "threads reply", err
		}
		if err := validateEventsCreateInput(body, "threads reply"); err != nil {
			return nil, "threads reply", err
		}
		if dryRun {
			return dryRunResult("threads reply", "events.create", nil, nil, body), "threads reply", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "threads reply", "events.create", nil, nil, body)
		return decorateThreadMessageWriteResult(result, callErr, cfg, "threads.reply", target), "threads reply", callErr
	case "create":
		return nil, "threads create", threadsMutationUnsupportedErr("create")
	case "patch":
		return nil, "threads patch", threadsMutationUnsupportedErr("patch")
	case "propose-patch":
		return nil, "threads propose-patch", threadsMutationUnsupportedErr("propose-patch")
	case "apply":
		return nil, "threads apply", threadsMutationUnsupportedErr("apply")
	case "timeline":
		fs := newSilentFlagSet("threads timeline")
		var threadIDFlag trackedString
		var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
		fs.Var(&threadIDFlag, "thread-id", "Thread id")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived events")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived events")
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed events")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed events")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "threads timeline", errnorm.Usage("invalid_flags", err.Error())
		}
		if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "threads timeline", err
		}
		positionals := fs.Args()
		id := strings.TrimSpace(threadIDFlag.value)
		if id == "" && len(positionals) > 0 {
			id = strings.TrimSpace(positionals[0])
			positionals = positionals[1:]
		}
		if err := validateID(id, "thread id"); err != nil {
			return nil, "threads timeline", err
		}
		if len(positionals) > 0 {
			return nil, "threads timeline", errnorm.Usage("invalid_args", "too many positional arguments")
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"threads timeline",
			"threads.timeline",
			"thread_id",
			id,
			threadIDLookupSpec,
			nil,
			nil,
		)
		if callErr != nil {
			return result, "threads timeline", callErr
		}
		data := asMap(result.Data)
		body := asMap(data["body"])
		events := asSlice(body["events"])
		filtered := filterEventsByLifecycleState(events, includeArchived, archivedOnly, includeTrashed, trashedOnly)
		body["events"] = filtered
		data["body"] = body
		result.Data = data
		result.Text = formatTypedCommandText(
			"threads.timeline",
			intValue(data["status_code"]),
			headerValues(data["headers"]),
			body,
			cfg.Verbose,
			cfg.Headers,
		)
		return result, "threads timeline", nil
	case "context":
		result, err := a.runThreadsContextCommand(ctx, args[1:], cfg)
		return result, "threads context", err
	case "inspect":
		result, err := a.runThreadsInspectCommand(ctx, args[1:], cfg)
		return result, "threads inspect", err
	case "workspace":
		result, err := a.runThreadsWorkspaceCommand(ctx, args[1:], cfg)
		return result, "threads workspace", err
	case "review":
		result, err := a.runThreadsReviewCommand(ctx, args[1:], cfg)
		return result, "threads review", err
	case "archive":
		return nil, "threads archive", threadsMutationUnsupportedErr("archive")
	case "unarchive":
		return nil, "threads unarchive", threadsMutationUnsupportedErr("unarchive")
	case "trash":
		return nil, "threads trash", threadsMutationUnsupportedErr("trash")
	case "restore":
		return nil, "threads restore", threadsMutationUnsupportedErr("restore")
	case "purge":
		return nil, "threads purge", threadsMutationUnsupportedErr("purge")
	default:
		return nil, "threads", threadsSubcommandSpec.unknownError(args[0])
	}
}

func parseThreadContextSelectionArgs(args []string, commandName string) (threadContextSelection, error) {
	fs := newSilentFlagSet(commandName)
	var threadIDFlags trackedStrings
	var stateFlag trackedString
	var maxEventsFlag trackedInt
	var includeArtifactContentFlag trackedBool
	var fullIDFlag trackedBool
	fs.Var(&threadIDFlags, "thread-id", "Thread id (repeatable)")
	fs.Var(&stateFlag, "state", "Discover threads by lifecycle state (active, archived, trashed)")
	fs.Var(&maxEventsFlag, "max-events", "Maximum recent events to include")
	fs.Var(&includeArtifactContentFlag, "include-artifact-content", "Include key artifact content previews")
	fs.Var(&fullIDFlag, "full-id", "(debug/admin) Render full ids in default text output (non-JSON)")
	if err := fs.Parse(args); err != nil {
		return threadContextSelection{}, errnorm.Usage("invalid_flags", err.Error())
	}

	positionals := append([]string(nil), fs.Args()...)
	threadIDs := make([]string, 0, len(threadIDFlags.values)+len(positionals))
	threadIDs = append(threadIDs, threadIDFlags.values...)
	threadIDs = append(threadIDs, positionals...)
	threadIDs = normalizeIDFilters(threadIDs)

	discoveryQuery := make([]queryParam, 0, 2)
	addSingleQuery(&discoveryQuery, "state", stateFlag.value)

	if maxEventsFlag.set && maxEventsFlag.value < 0 {
		return threadContextSelection{}, errnorm.Usage("invalid_request", "--max-events must be >= 0")
	}

	return threadContextSelection{
		threadIDs:              threadIDs,
		discoveryQuery:         discoveryQuery,
		maxEventsSet:           maxEventsFlag.set,
		maxEvents:              maxEventsFlag.value,
		includeArtifactContent: includeArtifactContentFlag.set && includeArtifactContentFlag.value,
		fullID:                 fullIDFlag.set && fullIDFlag.value,
	}, nil
}

func parseThreadWorkspaceArgs(args []string) (threadWorkspaceSelection, error) {
	fs := newSilentFlagSet("threads workspace")
	var threadIDFlags trackedStrings
	var stateFlag trackedString
	var maxEventsFlag trackedInt
	var includeArtifactContentFlag trackedBool
	var fullIDFlag trackedBool

	fs.Var(&threadIDFlags, "thread-id", "Thread id (repeatable)")
	fs.Var(&stateFlag, "state", "Discover threads by lifecycle state (active, archived, trashed)")
	fs.Var(&maxEventsFlag, "max-events", "Maximum recent events to include")
	fs.Var(&includeArtifactContentFlag, "include-artifact-content", "Include key artifact content previews")
	fs.Var(&fullIDFlag, "full-id", "(debug/admin) Render full ids in default text output (non-JSON)")
	if err := fs.Parse(args); err != nil {
		return threadWorkspaceSelection{}, errnorm.Usage("invalid_flags", err.Error())
	}

	positionals := append([]string(nil), fs.Args()...)
	threadIDs := make([]string, 0, len(threadIDFlags.values)+len(positionals))
	threadIDs = append(threadIDs, threadIDFlags.values...)
	threadIDs = append(threadIDs, positionals...)
	threadIDs = normalizeIDFilters(threadIDs)

	discoveryQuery := make([]queryParam, 0, 2)
	addSingleQuery(&discoveryQuery, "state", stateFlag.value)

	if maxEventsFlag.set && maxEventsFlag.value < 0 {
		return threadWorkspaceSelection{}, errnorm.Usage("invalid_request", "--max-events must be >= 0")
	}

	return threadWorkspaceSelection{
		threadContextSelection: threadContextSelection{
			threadIDs:              threadIDs,
			discoveryQuery:         discoveryQuery,
			maxEventsSet:           maxEventsFlag.set,
			maxEvents:              maxEventsFlag.value,
			includeArtifactContent: includeArtifactContentFlag.set && includeArtifactContentFlag.value,
			fullID:                 fullIDFlag.set && fullIDFlag.value,
		},
	}, nil
}

func (a *App) resolveThreadContextSelection(ctx context.Context, cfg config.Resolved, commandName string, selection threadContextSelection, allowMultiple bool) ([]string, error) {
	hasDiscoveryFilters := len(selection.discoveryQuery) > 0
	if len(selection.threadIDs) > 0 && hasDiscoveryFilters {
		return nil, errnorm.Usage("invalid_request", mixedThreadSelectionMessage(commandName))
	}

	threadIDs := append([]string(nil), selection.threadIDs...)
	if len(threadIDs) == 0 {
		if !hasDiscoveryFilters {
			return nil, errnorm.Usage(
				"invalid_request",
				"thread id is required (provide --thread-id <thread-id>) or use discovery filters (--state)",
			)
		}
		listResult, err := a.invokeTypedJSON(ctx, cfg, "threads list", "threads.list", nil, selection.discoveryQuery, nil)
		if err != nil {
			return nil, err
		}
		threadIDs = threadIDsFromThreadsList(listResult)
		if len(threadIDs) == 0 {
			return nil, errnorm.Usage(
				"invalid_request",
				commandName+" discovery returned no matching threads; run `anx threads list` and refine filters",
			)
		}
	}

	for _, threadID := range threadIDs {
		if err := validateID(threadID, "thread id"); err != nil {
			return nil, err
		}
	}
	if allowMultiple {
		return threadIDs, nil
	}
	if len(threadIDs) != 1 {
		return nil, errnorm.Usage(
			"invalid_request",
			fmt.Sprintf("%s requires exactly one thread; refine filters or pass one --thread-id. For topic context, use `anx topics list` and `anx topics workspace`. For a multi-thread diagnostic backing projection, use `anx threads workspace` with discovery filters.", commandName),
		)
	}
	return threadIDs, nil
}

func mixedThreadSelectionMessage(commandName string) string {
	base := "--thread-id cannot be combined with discovery filters (--state). Choose one mode."
	discoveryExample := "anx threads inspect --state active"
	switch strings.TrimSpace(commandName) {
	case "threads context":
		return base + " For one thread, use `anx threads inspect --thread-id <thread-id>` or `anx threads workspace --thread-id <thread-id>` for backing-thread diagnostics. Prefer `anx topics workspace topic:<handle>` for agent-facing topic context. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
	case "threads workspace":
		return base + " For one thread, use `anx threads workspace --thread-id <thread-id>`. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
	case "threads inspect":
		return base + " For one thread, use `anx threads inspect --thread-id <thread-id>`. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
	default:
		return base + " For one thread, use `anx " + strings.TrimSpace(commandName) + " --thread-id <thread-id>`. For discovery, remove `--thread-id` and use `" + discoveryExample + "`."
	}
}

func threadContextQuery(selection threadContextSelection) []queryParam {
	query := make([]queryParam, 0, 2)
	if selection.maxEventsSet {
		addSingleQuery(&query, "max_events", fmt.Sprintf("%d", selection.maxEvents))
	}
	if selection.includeArtifactContent {
		addSingleQuery(&query, "include_artifact_content", "true")
	}
	return query
}

func (a *App) runThreadsInspectCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	selection, err := parseThreadContextSelectionArgs(args, "threads inspect")
	if err != nil {
		return nil, err
	}
	threadIDs, err := a.resolveThreadContextSelection(ctx, cfg, "threads inspect", selection, false)
	if err != nil {
		return nil, err
	}

	statusCode, headers, body, callErr := a.loadThreadContextEnvelope(ctx, cfg, threadIDs[0], selection)
	if callErr != nil {
		return nil, callErr
	}

	thread := extractNestedMap(body, "thread")
	if thread == nil {
		thread = map[string]any{}
	}
	resolvedThreadID := firstNonEmpty(strings.TrimSpace(anyString(body["thread_id"])), strings.TrimSpace(anyString(thread["id"])), strings.TrimSpace(threadIDs[0]))
	contextSection := cloneMap(body)
	if contextSection == nil {
		contextSection = map[string]any{}
	}
	contextSection["thread_id"] = resolvedThreadID
	contextSection["thread"] = thread
	contextSection["full_id"] = selection.fullID
	addThreadContextCollaborationSummary(contextSection)

	inboxResult, err := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	inboxData := asMap(inboxResult.Data)
	inboxBody := extractNestedMap(inboxData, "body")
	inboxItems := filteredInboxItems(asSlice(inboxBody["items"]), []string{resolvedThreadID}, nil)

	inspectBody := map[string]any{
		"thread_id":      resolvedThreadID,
		"full_id":        selection.fullID,
		"thread":         thread,
		"context":        contextSection,
		"collaboration":  asMap(contextSection["collaboration_summary"]),
		"inbox":          map[string]any{"thread_id": resolvedThreadID, "items": inboxItems, "count": len(inboxItems), "full_id": selection.fullID},
		"context_source": "threads.context",
		"inbox_source":   "inbox.list",
	}

	data := map[string]any{
		"status_code": statusCode,
		"headers":     headers,
		"body":        inspectBody,
	}
	result := &commandResult{Data: data}
	result.Text = formatTypedCommandText(
		"threads.inspect",
		statusCode,
		headers,
		inspectBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}
