// `anx events` dispatch: list, explain, create and stream.

package app

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/registry"
)

func (a *App) runEventsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "events", eventsSubcommandSpec.requiredError()
	}
	sub := eventsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		result, err := a.runEventsListCommand(ctx, args[1:], cfg)
		return result, "events list", err
	case "get":
		id, err := parseIDArg(args[1:], "event-id", "event id")
		if err != nil {
			return nil, "events get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"events get",
			"events.get",
			"event_id",
			id,
			eventIDLookupSpec,
			nil,
			nil,
		)
		return result, "events get", callErr
	case "create":
		body, dryRun, err := a.parseJSONBodyInputWithOptions(args[1:], "events create", jsonBodyInputOptions{
			allowDryRun: true,
		})
		if err != nil {
			return nil, "events create", err
		}
		if err := validateEventsCreateInput(body, "events create"); err != nil {
			return nil, "events create", err
		}
		if dryRun {
			return dryRunResult("events create", "events.create", nil, nil, body), "events create", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "events create", "events.create", nil, nil, body)
		return result, "events create", callErr
	case "validate":
		body, _, err := a.parseJSONBodyInputWithOptions(args[1:], "events validate", jsonBodyInputOptions{})
		if err != nil {
			return nil, "events validate", err
		}
		if err := validateEventsCreateInput(body, "events validate"); err != nil {
			return nil, "events validate", err
		}
		return validationResult("events validate", "events.create", nil, nil, body), "events validate", nil
	case "stream":
		result, err := a.runEventsStream(ctx, args[1:], cfg, "events stream", false)
		return result, "events stream", err
	case "tail":
		result, err := a.runEventsStream(ctx, args[1:], cfg, "events stream", true)
		return result, "events stream", err
	case "explain":
		result, err := a.runEventsExplainCommand(args[1:])
		return result, "events explain", err
	case "archive", "unarchive", "trash", "restore":
		spec, ok := lifecycleSpecFor("events")
		if !ok {
			return nil, "events", errnorm.Usage("internal_error", "missing lifecycle spec for events")
		}
		return a.runLifecycleVerb(ctx, cfg, spec, sub, args[1:])
	default:
		return nil, "events", eventsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runEventsListCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("events list")
	var threadIDFlags trackedStrings
	var typesCSVFlag trackedString
	var eventGroupFlags trackedStrings
	var backingScopeFlag trackedString
	var actorIDFlag trackedString
	var maxEventsFlag trackedInt
	var mineFlag trackedBool
	var fullIDFlag trackedBool
	var typeFlags trackedStrings
	var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
	fs.Var(&threadIDFlags, "thread-id", "Thread id (repeatable)")
	fs.Var(&typeFlags, "type", "Filter by event type (repeatable)")
	fs.Var(&typesCSVFlag, "types", "Comma-separated event types")
	fs.Var(&eventGroupFlags, "event-group", "Filter by event group (repeatable)")
	fs.Var(&backingScopeFlag, "backing-scope", "Filter backing events: all, standalone, or backing_only")
	fs.Var(&actorIDFlag, "actor-id", "Filter by actor id")
	fs.Var(&mineFlag, "mine", "Filter to events authored by resolved agent actor_id")
	fs.Var(&fullIDFlag, "full-id", "(debug/admin) Render full IDs in default text output (non-JSON)")
	fs.Var(&maxEventsFlag, "max-events", "Return at most N most-recent matching events (0 means unlimited)")
	fs.Var(&maxEventsFlag, "max", "Alias for --max-events")
	fs.BoolVar(&includeArchived, "include-archived", false, "Include archived events")
	fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived events")
	fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed events")
	fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed events")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
		return nil, err
	}

	positionals := append([]string(nil), fs.Args()...)
	threadIDs := make([]string, 0, len(threadIDFlags.values)+len(positionals))
	threadIDs = append(threadIDs, threadIDFlags.values...)
	threadIDs = append(threadIDs, positionals...)
	threadIDs = normalizeIDFilters(threadIDs)
	for _, threadID := range threadIDs {
		if err := validateID(threadID, "thread id"); err != nil {
			return nil, err
		}
	}
	if maxEventsFlag.set && maxEventsFlag.value < 0 {
		return nil, errnorm.Usage("invalid_request", "--max-events must be >= 0")
	}
	if mineFlag.set && mineFlag.value && strings.TrimSpace(actorIDFlag.value) != "" && strings.TrimSpace(actorIDFlag.value) != "me" {
		return nil, errnorm.Usage("invalid_request", "--mine cannot be combined with --actor-id unless --actor-id=me")
	}
	if len(threadIDs) == 0 && (includeArchived || archivedOnly || includeTrashed || trashedOnly) {
		return nil, errnorm.Usage("invalid_flags", "events list lifecycle flags require --thread-id; workspace-wide events list uses GET /events, which does not expose lifecycle state filtering")
	}

	typeFilters := normalizeEventTypeFilters(typeFlags.values, typesCSVFlag.value)
	eventGroupFilters := normalizeCSVFilters(eventGroupFlags.values)
	backingScopeFilter := firstNonEmpty(strings.TrimSpace(backingScopeFlag.value), "all")
	taxonomy, err := registry.LoadEmbeddedTaxonomy()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindInternal, "taxonomy_unavailable", "failed to load embedded taxonomy metadata", err)
	}
	if err := validateEventGroupFilters(eventGroupFilters, taxonomy); err != nil {
		return nil, err
	}
	if err := validateBackingScopeFilter(backingScopeFilter, taxonomy); err != nil {
		return nil, err
	}
	actorFilter := strings.TrimSpace(actorIDFlag.value)
	if mineFlag.set && mineFlag.value {
		actorFilter = "me"
	}
	resolvedActorID, err := resolveActorIDAlias(actorFilter, cfg)
	if err != nil {
		return nil, err
	}

	if len(threadIDs) == 0 {
		query := make([]queryParam, 0, 8)
		addMultiQuery(&query, "type", typeFilters)
		addMultiQuery(&query, "event_group", eventGroupFilters)
		addSingleQuery(&query, "backing_scope", backingScopeFilter)
		addSingleQuery(&query, "actor_id", resolvedActorID)
		if maxEventsFlag.set && maxEventsFlag.value > 0 {
			addSingleQuery(&query, "limit", strconv.Itoa(maxEventsFlag.value))
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "events list", "events.list", nil, query, nil)
		return result, err
	}

	resolvedThreadIDs := make([]string, 0, len(threadIDs))
	allEvents := make([]any, 0, 32)
	matching := make([]any, 0, 32)
	expandedArtifacts := make(map[string]any)
	statusCode := http.StatusOK
	headers := map[string][]string{"Content-Type": {"application/json"}}
	capturedResponse := false
	for _, threadID := range threadIDs {
		// `events list` keeps the user-facing registry identity (`events.list`)
		// even though the current implementation composes backing-thread
		// timelines client-side.
		timelineResult, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"events list",
			"threads.timeline",
			"thread_id",
			threadID,
			threadIDLookupSpec,
			nil,
			nil,
		)
		if callErr != nil {
			return nil, callErr
		}
		data := asMap(timelineResult.Data)
		body := asMap(data["body"])
		if !capturedResponse {
			capturedResponse = true
			if code := intValue(data["status_code"]); code > 0 {
				statusCode = code
			}
			if responseHeaders := headerValues(data["headers"]); len(responseHeaders) > 0 {
				headers = responseHeaders
			}
		}
		threadEvents := asSlice(body["events"])
		allEvents = append(allEvents, threadEvents...)
		filtered := filterEventsByType(threadEvents, typeFilters)
		filtered = filterEventsByEventGroups(filtered, eventGroupFilters, taxonomy)
		filtered = filterEventsByBackingScope(filtered, backingScopeFilter, taxonomy)
		filtered = filterEventsByActorID(filtered, resolvedActorID)
		filtered = filterEventsByLifecycleState(filtered, includeArchived, archivedOnly, includeTrashed, trashedOnly)
		matching = append(matching, filtered...)
		mergeExpandedTimelineObjects(expandedArtifacts, asMap(body["artifacts"]))
		resolvedThreadID := firstNonEmpty(anyString(body["thread_id"]), eventThreadIDFromList(threadEvents), threadID)
		if resolvedThreadID != "" {
			resolvedThreadIDs = append(resolvedThreadIDs, resolvedThreadID)
		}
	}
	resolvedThreadIDs = normalizeIDFilters(resolvedThreadIDs)
	if len(resolvedThreadIDs) > 1 {
		sortEventsByCreatedAt(matching)
	}
	if maxEventsFlag.set && maxEventsFlag.value > 0 && len(matching) > maxEventsFlag.value {
		matching = matching[len(matching)-maxEventsFlag.value:]
	}
	matching = enrichEventsForList(matching)

	listBody := map[string]any{
		"thread_id":       firstNonEmpty(eventThreadIDFromList(matching), eventThreadIDFromList(allEvents)),
		"thread_ids":      resolvedThreadIDs,
		"full_id":         fullIDFlag.set && fullIDFlag.value,
		"events":          matching,
		"total_events":    len(allEvents),
		"returned_events": len(matching),
		"artifacts":       expandedArtifacts,
	}
	if len(resolvedThreadIDs) == 1 {
		listBody["thread_id"] = resolvedThreadIDs[0]
	}
	if len(typeFilters) > 0 {
		listBody["types"] = typeFilters
	}
	if len(eventGroupFilters) > 0 {
		listBody["event_groups"] = eventGroupFilters
	}
	if backingScopeFilter != "" && backingScopeFilter != "all" {
		listBody["backing_scope"] = backingScopeFilter
	}
	if resolvedActorID != "" {
		listBody["actor_id"] = resolvedActorID
	}
	if maxEventsFlag.set {
		listBody["max_events"] = maxEventsFlag.value
	}
	if includeArchived {
		listBody["include_archived"] = true
	}
	if archivedOnly {
		listBody["archived_only"] = true
	}
	if includeTrashed {
		listBody["include_trashed"] = true
	}
	if trashedOnly {
		listBody["trashed_only"] = true
	}

	resultData := map[string]any{
		"status_code": statusCode,
		"headers":     headers,
		"body":        listBody,
	}
	result := &commandResult{Data: resultData}
	result.Text = formatTypedCommandText(
		"events.list",
		intValue(resultData["status_code"]),
		headerValues(resultData["headers"]),
		listBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}

func (a *App) runEventsExplainCommand(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("events explain")
	var eventTypeFlag trackedString
	fs.Var(&eventTypeFlag, "type", "Event type to explain")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	eventType := strings.TrimSpace(eventTypeFlag.value)
	if eventType == "" && len(positionals) > 0 {
		eventType = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx events explain`")
	}
	if eventType == "" {
		textLines := []string{
			"Known event types (strict enum):",
		}
		items := make([]any, 0, len(knownEventTypeGuidance))
		for _, group := range eventTypeGroupOrder {
			groupItems := make([]eventTypeGuidance, 0)
			for _, guidance := range knownEventTypeGuidance {
				if guidance.Group == group {
					groupItems = append(groupItems, guidance)
				}
			}
			if len(groupItems) == 0 {
				continue
			}
			if description := strings.TrimSpace(eventTypeGroupDescriptions[group]); description != "" {
				textLines = append(textLines, "", group+": "+description)
			} else {
				textLines = append(textLines, "", group+":")
			}
			for _, guidance := range groupItems {
				line := "- " + guidance.Type
				summary := strings.TrimSpace(guidance.Summary)
				switch {
				case summary != "" && strings.TrimSpace(guidance.PreferredCommand) != "":
					line += ": " + summary + " Prefer `" + guidance.PreferredCommand + "`."
				case summary != "":
					line += ": " + summary
				case strings.TrimSpace(guidance.PreferredCommand) != "":
					line += ": prefer `" + guidance.PreferredCommand + "`"
				}
				textLines = append(textLines, line)
				items = append(items, map[string]any{
					"type":              guidance.Type,
					"group":             guidance.Group,
					"group_description": eventTypeGroupDescriptions[guidance.Group],
					"summary":           guidance.Summary,
					"preferred_command": guidance.PreferredCommand,
				})
			}
		}
		textLines = append(textLines, "For details: anx events explain <event-type>")
		data := map[string]any{
			"known_event_types": items,
			"hint":              "anx events explain <event-type>",
		}
		return &commandResult{Text: strings.Join(textLines, "\n"), Data: data}, nil
	}

	guidance, ok := eventTypeGuidanceFor(eventType)
	if !ok {
		return nil, errnorm.Usage(
			"invalid_request",
			fmt.Sprintf("unknown event type %q; known types: %s", eventType, strings.Join(knownEventTypeNames(), ", ")),
		)
	}

	textLines := []string{
		"Event type: " + guidance.Type,
		"Group: " + guidance.Group,
	}
	if summary := strings.TrimSpace(guidance.Summary); summary != "" {
		textLines = append(textLines, "Usage hint: "+summary)
	}
	if preferred := strings.TrimSpace(guidance.PreferredCommand); preferred != "" {
		textLines = append(textLines, "Preferred command: "+preferred)
	}
	textLines = append(textLines, "Constraints:")
	for _, constraint := range guidance.Constraints {
		textLines = append(textLines, "- "+constraint)
	}
	data := map[string]any{
		"event_type":        guidance.Type,
		"group":             guidance.Group,
		"group_description": eventTypeGroupDescriptions[guidance.Group],
		"summary":           guidance.Summary,
		"preferred_command": guidance.PreferredCommand,
		"constraints":       append([]string(nil), guidance.Constraints...),
	}
	return &commandResult{Text: strings.Join(textLines, "\n"), Data: data}, nil
}

func (a *App) runEventsStream(ctx context.Context, args []string, cfg config.Resolved, commandName string, defaultFollow bool) (*commandResult, error) {
	fs := newSilentFlagSet(commandName)
	var threadIDFlag, typesCSVFlag, lastEventIDFlag, cursorFlag trackedString
	var followFlag trackedBool
	var maxEventsFlag trackedInt
	var typeFlags trackedStrings
	fs.Var(&threadIDFlag, "thread-id", "Stream events for one thread id")
	fs.Var(&typeFlags, "type", "Filter by event type (repeatable)")
	fs.Var(&typesCSVFlag, "types", "Comma-separated event types")
	fs.Var(&followFlag, "follow", "Keep stream open and reconnect when it drops")
	fs.Var(&lastEventIDFlag, "last-event-id", "Resume stream after this event id")
	fs.Var(&cursorFlag, "cursor", "Alias of --last-event-id")
	fs.Var(&maxEventsFlag, "max-events", "Exit after receiving N events (0 means unlimited)")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}

	query := make([]queryParam, 0, 4)
	addSingleQuery(&query, "thread_id", threadIDFlag.value)
	addMultiQuery(&query, "type", normalizeEventTypeFilters(typeFlags.values, typesCSVFlag.value))
	lastEventID := firstNonEmpty(lastEventIDFlag.value, cursorFlag.value)
	follow := defaultFollow
	if followFlag.set {
		follow = followFlag.value
	}
	return a.runTailStream(ctx, cfg, commandName, "events.stream", query, lastEventID, follow, maxEventsFlag.value)
}

func validateEventsCreateInput(body any, commandName string) error {
	payload, ok := body.(map[string]any)
	if !ok {
		return errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
	}

	if err := validateEventsCreateBody(body); err != nil {
		return err
	}
	issues := validateDraftBody("events.create", payload)
	if len(issues) > 0 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("events payload failed local validation: %s", strings.Join(issues, "; ")))
	}
	return nil
}
