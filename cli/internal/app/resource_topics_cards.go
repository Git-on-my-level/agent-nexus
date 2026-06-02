package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

var topicsSubcommandSpec = subcommandSpec{
	command: "topics",
	valid: []string{
		"list", "get", "create", "patch", "message", "messages", "reply", "timeline", "workspace",
		"archive", "unarchive", "trash", "restore",
	},
	examples: []string{
		"anx topics list",
		"anx topics create --title \"Launch\" --summary \"Coordinate launch work\"",
		"anx topics message topic:launch --body-file message.md",
		"anx topics messages topic:launch",
		"anx topics reply topic:launch --to <message-id> --body-file reply.md",
		"anx topics workspace topic:launch",
		"anx topics archive topic:launch --reason \"wrapped up\"",
		"anx topics trash topic:launch --reason \"obsolete\"",
		"anx topics archive topic:launch --from-file lifecycle.json",
	},
}

var cardsSubcommandSpec = subcommandSpec{
	command: "cards",
	valid:   []string{"list", "get", "create", "message", "messages", "reply", "revise", "history", "revision", "patch", "move", "assign", "resolve", "reopen", "archive", "trash", "purge", "restore", "timeline"},
	examples: []string{
		"anx cards list",
		"anx cards create --board board:launch --title \"Implement login\" --body-file card.md",
		"anx cards message card:implement-login --body-file update.md",
		"anx cards messages card:implement-login",
		"anx cards reply card:implement-login --to <message-id> --body-file reply.md",
		"anx cards revise card:implement-login --body-file card.md",
		"anx cards history card:implement-login",
		"anx cards assign card:implement-login --assignee-ref actor:agent-alpha",
		"anx cards resolve card:implement-login --reason \"ok\" --body \"Validated in staging\"",
		"anx cards move card:implement-login --column review",
		"anx cards archive card:implement-login --reason \"blocked external\"",
		"anx cards trash card:implement-login --reason \"duplicate\"",
		"anx cards archive card:implement-login --from-file lifecycle.json",
		"anx cards get card:implement-login",
	},
}

func (a *App) runTopicsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("topics"); ok {
			return &commandResult{Text: text}, "topics", nil
		}
		return nil, "topics", topicsSubcommandSpec.requiredError()
	}
	sub := topicsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("topics list")
		var stateFlag, queryFlag, cursorFlag trackedString
		var limitFlag trackedInt
		var fullIDFlag trackedBool
		var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
		fs.Var(&stateFlag, "state", "Filter by lifecycle state (active, archived, trashed)")
		fs.Var(&queryFlag, "q", "Search topics by id or title substring")
		fs.Var(&limitFlag, "limit", "Page size (1–1000)")
		fs.Var(&cursorFlag, "cursor", "Pagination cursor from a previous list response")
		fs.Var(&fullIDFlag, "full-id", "(debug/admin) Render full topic ids in default text output (non-JSON)")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived topics")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived topics")
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed topics")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed topics")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "topics list", errnorm.Usage("invalid_flags", err.Error())
		}
		if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "topics list", err
		}
		if len(fs.Args()) > 0 {
			return nil, "topics list", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx topics list`")
		}
		if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
			return nil, "topics list", errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
		}
		query := make([]queryParam, 0, 8)
		if err := appendLifecycleStatesForHTTPList(&query, stateFlag.value, includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "topics list", err
		}
		addSingleQuery(&query, "q", queryFlag.value)
		if limitFlag.set {
			addSingleQuery(&query, "limit", strconv.Itoa(limitFlag.value))
		}
		addSingleQuery(&query, "cursor", cursorFlag.value)
		result, err := a.invokeTypedJSON(ctx, cfg, "topics list", "topics.list", nil, query, nil)
		if err != nil {
			return nil, "topics list", err
		}
		if fullIDFlag.set && fullIDFlag.value {
			data := asMap(result.Data)
			body := asMap(data["body"])
			if body != nil {
				body["full_id"] = true
				result.Text = formatTypedCommandText(
					"topics.list",
					intValue(data["status_code"]),
					headerValues(data["headers"]),
					body,
					cfg.Verbose,
					cfg.Headers,
				)
			}
		}
		return result, "topics list", nil
	case "get":
		id, err := parseResourceIDArg(args[1:], "topic-id", "topic id", "topic")
		if err != nil {
			return nil, "topics get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics get", "topics.get", "topic_id", id, topicIDLookupSpec, nil, nil)
		return result, "topics get", callErr
	case "create":
		body, dryRun, err := a.parseTopicCreateInput(args[1:], cfg, "topics create")
		if err != nil {
			return nil, "topics create", err
		}
		if bodyMap, ok := body.(map[string]any); ok {
			ensureEmptyListDefaults(bodyMap, "topic", []string{"owner_refs", "document_refs", "board_refs", "related_refs"})
		}
		if dryRun {
			return dryRunResult("topics create", "topics.create", nil, nil, body), "topics create", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "topics create", "topics.create", nil, nil, body)
		return addResourceURLToResult(cfg, "topics.create", result), "topics create", callErr
	case "patch":
		id, body, dryRun, err := a.parseTopicPatchInput(ctx, args[1:], cfg, "topics patch")
		if err != nil {
			return nil, "topics patch", err
		}
		if dryRun {
			return dryRunResult("topics patch", "topics.patch", map[string]string{"topic_id": id}, nil, body), "topics patch", nil
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics patch", "topics.patch", "topic_id", id, topicIDLookupSpec, nil, body)
		return result, "topics patch", callErr
	case "message":
		body, target, dryRun, err := a.parseTopicMessageInput(ctx, args[1:], cfg, "topics message", "")
		if err != nil {
			return nil, "topics message", err
		}
		if err := validateEventsCreateInput(body, "topics message"); err != nil {
			return nil, "topics message", err
		}
		if dryRun {
			return dryRunResult("topics message", "events.create", nil, nil, body), "topics message", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "topics message", "events.create", nil, nil, body)
		return decorateThreadMessageWriteResult(result, callErr, cfg, "topics.message", target), "topics message", callErr
	case "messages":
		result, err := a.runTopicMessagesCommand(ctx, args[1:], cfg)
		return result, "topics messages", err
	case "reply":
		body, target, dryRun, err := a.parseTopicReplyInput(ctx, args[1:], cfg, "topics reply")
		if err != nil {
			return nil, "topics reply", err
		}
		if err := validateEventsCreateInput(body, "topics reply"); err != nil {
			return nil, "topics reply", err
		}
		if dryRun {
			return dryRunResult("topics reply", "events.create", nil, nil, body), "topics reply", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "topics reply", "events.create", nil, nil, body)
		return decorateThreadMessageWriteResult(result, callErr, cfg, "topics.reply", target), "topics reply", callErr
	case "timeline":
		id, err := parseResourceIDArg(args[1:], "topic-id", "topic id", "topic")
		if err != nil {
			return nil, "topics timeline", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics timeline", "topics.timeline", "topic_id", id, topicIDLookupSpec, nil, nil)
		return result, "topics timeline", callErr
	case "workspace":
		id, err := parseResourceIDArg(args[1:], "topic-id", "topic id", "topic")
		if err != nil {
			return nil, "topics workspace", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics workspace", "topics.workspace", "topic_id", id, topicIDLookupSpec, nil, nil)
		return result, "topics workspace", callErr
	case "archive", "unarchive", "trash", "restore":
		spec, ok := lifecycleSpecFor("topics")
		if !ok {
			return nil, "topics", errnorm.Usage("internal_error", "missing lifecycle spec for topics")
		}
		return a.runLifecycleVerb(ctx, cfg, spec, sub, args[1:])
	default:
		return nil, "topics", topicsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runCardsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("cards"); ok {
			return &commandResult{Text: text}, "cards", nil
		}
		return nil, "cards", cardsSubcommandSpec.requiredError()
	}
	sub := cardsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("cards list")
		var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
		var boardFlag trackedString
		var boardIDFlag trackedString
		fs.Var(&boardFlag, "board", "Board ref, handle, or id; lists cards on one board")
		fs.Var(&boardIDFlag, "board-id", "Board id; equivalent to --board for scripts")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived cards")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived cards")
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed cards")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed cards")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "cards list", errnorm.Usage("invalid_flags", err.Error())
		}
		if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "cards list", err
		}
		if len(fs.Args()) > 0 {
			return nil, "cards list", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx cards list`")
		}
		boardID := strings.TrimSpace(boardIDFlag.value)
		if strings.TrimSpace(boardFlag.value) != "" {
			if boardID != "" {
				return nil, "cards list", errnorm.Usage("invalid_flags", "`--board` and `--board-id` cannot be combined")
			}
			boardID = strings.TrimSpace(boardFlag.value)
		}
		if boardID != "" {
			if includeArchived || archivedOnly || includeTrashed || trashedOnly {
				return nil, "cards list", errnorm.Usage("invalid_flags", "lifecycle filters are not supported with `anx cards list --board`; use global `anx cards list` for lifecycle filtering")
			}
			resolvedBoard, err := a.resolveMaybeBoardID(ctx, cfg, boardID)
			if err != nil {
				return nil, "cards list", err
			}
			result, callErr := a.invokeTypedJSON(ctx, cfg, "cards list", "boards.cards.list", map[string]string{"board_id": resolvedBoard}, nil, nil)
			return result, "cards list", callErr
		}
		query := make([]queryParam, 0, 4)
		if err := appendLifecycleStatesForHTTPList(&query, "", includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "cards list", err
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "cards list", "cards.list", nil, query, nil)
		return result, "cards list", err
	case "create":
		body, dryRun, err := a.parseCardCreateInput(args[1:], cfg, "cards create")
		if err != nil {
			return nil, "cards create", err
		}
		if dryRun {
			return dryRunResult("cards create", "cards.create", nil, nil, body), "cards create", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "cards create", "cards.create", nil, nil, body)
		return addResourceURLToResult(cfg, "cards.create", result), "cards create", callErr
	case "get":
		id, err := parseResourceIDArg(args[1:], "card-id", "card id", "card")
		if err != nil {
			return nil, "cards get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards get", "cards.get", "card_id", id, cardIDLookupSpec, nil, nil)
		return result, "cards get", callErr
	case "timeline":
		id, err := parseResourceIDArg(args[1:], "card-id", "card id", "card")
		if err != nil {
			return nil, "cards timeline", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards timeline", "cards.timeline", "card_id", id, cardIDLookupSpec, nil, nil)
		return result, "cards timeline", callErr
	case "message":
		body, card, dryRun, err := a.parseCardMessageInput(ctx, args[1:], cfg, "cards message", "")
		if err != nil {
			return nil, "cards message", err
		}
		if err := validateEventsCreateInput(body, "cards message"); err != nil {
			return nil, "cards message", err
		}
		if dryRun {
			return dryRunResult("cards message", "events.create", nil, nil, body), "cards message", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "cards message", "events.create", nil, nil, body)
		return a.decorateMessageWriteResult(result, callErr, cfg, "cards.message", card), "cards message", callErr
	case "messages":
		result, err := a.runCardMessagesCommand(ctx, args[1:], cfg)
		return result, "cards messages", err
	case "reply":
		body, card, dryRun, err := a.parseCardReplyInput(ctx, args[1:], cfg, "cards reply")
		if err != nil {
			return nil, "cards reply", err
		}
		if err := validateEventsCreateInput(body, "cards reply"); err != nil {
			return nil, "cards reply", err
		}
		if dryRun {
			return dryRunResult("cards reply", "events.create", nil, nil, body), "cards reply", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "cards reply", "events.create", nil, nil, body)
		return a.decorateMessageWriteResult(result, callErr, cfg, "cards.reply", card), "cards reply", callErr
	case "history":
		id, err := parseResourceIDArg(args[1:], "card-id", "card id", "card")
		if err != nil {
			return nil, "cards history", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards history", "cards.revisions.list", "card_id", id, cardIDLookupSpec, nil, nil)
		return result, "cards history", callErr
	case "revision":
		if len(args) < 2 || isHelpToken(args[1]) {
			return nil, "cards revision", errnorm.Usage("missing_subcommand", "`anx cards revision` requires subcommand `get`")
		}
		if args[1] != "get" {
			return nil, "cards revision", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown `anx cards revision` subcommand %q; expected `get`", args[1]))
		}
		cardID, revisionID, err := parseCardRevisionGetInput(args[2:], "cards revision get")
		if err != nil {
			return nil, "cards revision get", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "cards revision get", "cards.revisions.get", map[string]string{"card_id": cardID, "revision_id": revisionID}, nil, nil)
		return result, "cards revision get", callErr
	case "patch":
		id, body, err := a.parseCardPatchInput(ctx, args[1:], cfg, "cards patch")
		if err != nil {
			return nil, "cards patch", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards patch", "cards.patch", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards patch", callErr
	case "revise":
		id, body, err := a.parseCardReviseInput(ctx, args[1:], cfg, "cards revise")
		if err != nil {
			return nil, "cards revise", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards revise", "cards.revisions.create", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards revise", callErr
	case "move":
		id, body, dryRun, flagOverwrites, err := a.parseCardMoveInput(ctx, args[1:], cfg, "cards move")
		if err != nil {
			return nil, "cards move", err
		}
		if dryRun {
			return dryRunResultWithFlagOverlays("cards move", "cards.move", map[string]string{"card_id": id}, nil, body, flagOverwrites), "cards move", nil
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards move", "cards.move", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards move", callErr
	case "assign":
		id, body, err := a.parseCardAssignInput(ctx, args[1:], cfg, "cards assign")
		if err != nil {
			return nil, "cards assign", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards assign", "cards.patch", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards assign", callErr
	case "resolve":
		plan, err := a.parseCardResolveInput(args[1:], cfg, "cards resolve")
		if err != nil {
			return nil, "cards resolve", err
		}
		if plan.dryRun {
			return dryRunResultWithFlagOverlays("cards resolve", "cards.move", map[string]string{"card_id": plan.cardID}, nil, plan.moveBody, plan.dryRunFlagOverwrites), "cards resolve", nil
		}
		if plan.hasEvidenceMessage {
			ref, err := a.postCardResolveEvidence(ctx, cfg, plan)
			if err != nil {
				return nil, "cards resolve", err
			}
			plan.resolutionRefs = normalizeStringFilters(append(plan.resolutionRefs, ref))
			plan.moveBody["resolution_refs"] = plan.resolutionRefs
		}
		if err := a.ensureCardMoveConcurrency(ctx, cfg, plan.cardID, plan.moveBody); err != nil {
			return nil, "cards resolve", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards resolve", "cards.move", "card_id", plan.cardID, cardIDLookupSpec, nil, plan.moveBody)
		return result, "cards resolve", callErr
	case "reopen":
		id, body, err := a.parseCardReopenInput(ctx, args[1:], cfg, "cards reopen")
		if err != nil {
			return nil, "cards reopen", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards reopen", "cards.move", "card_id", id, cardIDLookupSpec, nil, body)
		return result, "cards reopen", callErr
	case "archive", "trash", "restore", "purge":
		spec, ok := lifecycleSpecFor("cards")
		if !ok {
			return nil, "cards", errnorm.Usage("internal_error", "missing lifecycle spec for cards")
		}
		return a.runLifecycleVerb(ctx, cfg, spec, sub, args[1:])
	default:
		return nil, "cards", cardsSubcommandSpec.unknownError(args[0])
	}
}

// Review guideline: commands with at most one required scalar request-body field
// must expose flag-based non-JSON input and keep JSON stdin/--from-file as the
// advanced compatibility path.

func (a *App) parseTopicCreateInput(args []string, cfg config.Resolved, commandName string) (any, bool, error) {
	fs := newSilentFlagSet(commandName)
	var fromFileFlag, titleFlag, summaryFlag, actorIDFlag trackedString
	var ownerRefFlags, documentRefFlags, boardRefFlags, relatedRefFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON request body from file or stdin with -")
	fs.Var(&titleFlag, "title", "Topic title")
	fs.Var(&summaryFlag, "summary", "Topic summary")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&ownerRefFlags, "owner-ref", "Owner typed ref, repeatable")
	fs.Var(&documentRefFlags, "document-ref", "Linked document typed ref, repeatable")
	fs.Var(&boardRefFlags, "board-ref", "Linked board typed ref, repeatable")
	fs.Var(&relatedRefFlags, "ref", "Additional related typed ref, repeatable")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	fieldFlagsSet := strings.TrimSpace(titleFlag.value) != "" ||
		strings.TrimSpace(summaryFlag.value) != "" ||
		len(ownerRefFlags.values) > 0 ||
		len(documentRefFlags.values) > 0 ||
		len(boardRefFlags.values) > 0 ||
		len(relatedRefFlags.values) > 0 ||
		strings.TrimSpace(actorIDFlag.value) != ""
	if strings.TrimSpace(fromFileFlag.value) != "" || !fieldFlagsSet {
		if fieldFlagsSet {
			return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		body, _, err := a.parseJSONBodyInputWithOptions(args, commandName, jsonBodyInputOptions{allowDryRun: true})
		if err != nil {
			return nil, false, err
		}
		if bodyMap, ok := body.(map[string]any); ok {
			if err := finalizeOptionalMutationBodyActorID(bodyMap, cfg); err != nil {
				return nil, false, err
			}
		}
		return body, dryRunFlag.set && dryRunFlag.value, nil
	}
	title := strings.TrimSpace(titleFlag.value)
	if title == "" {
		return nil, false, errnorm.Usage("invalid_request", "`--title` is required for `anx topics create`")
	}
	summary := strings.TrimSpace(summaryFlag.value)
	if summary == "" {
		return nil, false, errnorm.Usage("invalid_request", "`--summary` is required for `anx topics create`")
	}
	body := map[string]any{
		"topic": map[string]any{
			"title":         title,
			"summary":       summary,
			"owner_refs":    normalizedStringsOrEmpty(ownerRefFlags.values),
			"document_refs": normalizedStringsOrEmpty(documentRefFlags.values),
			"board_refs":    normalizedStringsOrEmpty(boardRefFlags.values),
			"related_refs":  normalizedStringsOrEmpty(relatedRefFlags.values),
			"provenance":    map[string]any{"sources": []any{"event:anx-cli"}},
		},
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, false, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := finalizeOptionalMutationBodyActorID(body, cfg); err != nil {
		return nil, false, err
	}
	return body, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseCardCreateInput(args []string, cfg config.Resolved, commandName string) (map[string]any, bool, error) {
	fs := newSilentFlagSet(commandName)
	var boardFlag, titleFlag, bodyFlag, contentFileFlag, fromFileFlag trackedString
	var actorIDFlag, requestKeyFlag, ifBoardUpdatedAtFlag trackedString
	var columnFlag, topicFlag, documentRefFlag, riskFlag, dueAtFlag trackedString
	var beforeCardIDFlag, afterCardIDFlag trackedString
	var assigneeRefFlags, relatedRefFlags, doneFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&boardFlag, "board", "Parent board ref, handle, or id for the new card")
	fs.Var(&boardFlag, "board-id", "Parent board ref, handle, or id for the new card")
	fs.Var(&titleFlag, "title", "Card title")
	fs.Var(&bodyFlag, "body", "Inline card summary/body text")
	fs.Var(&contentFileFlag, "body-file", "Load card summary/body text from a local file or stdin with -")
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON request body from file")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&requestKeyFlag, "request-key", "Request key for idempotency")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token")
	fs.Var(&columnFlag, "column", "Initial board column key")
	fs.Var(&topicFlag, "topic", "Related topic typed ref or handle")
	fs.Var(&documentRefFlag, "document-ref", "Pinned document typed ref")
	fs.Var(&riskFlag, "risk", "Risk level: low, medium, high, critical")
	fs.Var(&dueAtFlag, "due-at", "Due timestamp")
	fs.Var(&beforeCardIDFlag, "before-card-id", "Place before this card id")
	fs.Var(&afterCardIDFlag, "after-card-id", "Place after this card id")
	fs.Var(&assigneeRefFlags, "assignee-ref", "Assignee actor typed reference (repeatable)")
	fs.Var(&relatedRefFlags, "ref", "Additional related typed ref (repeatable)")
	fs.Var(&doneFlags, "done", "Definition-of-done checklist item (repeatable)")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	fieldFlagsSet := strings.TrimSpace(boardFlag.value) != "" ||
		strings.TrimSpace(titleFlag.value) != "" ||
		strings.TrimSpace(bodyFlag.value) != "" ||
		strings.TrimSpace(contentFileFlag.value) != "" ||
		strings.TrimSpace(actorIDFlag.value) != "" ||
		strings.TrimSpace(requestKeyFlag.value) != "" ||
		strings.TrimSpace(ifBoardUpdatedAtFlag.value) != "" ||
		strings.TrimSpace(columnFlag.value) != "" ||
		strings.TrimSpace(topicFlag.value) != "" ||
		strings.TrimSpace(documentRefFlag.value) != "" ||
		strings.TrimSpace(riskFlag.value) != "" ||
		strings.TrimSpace(dueAtFlag.value) != "" ||
		strings.TrimSpace(beforeCardIDFlag.value) != "" ||
		strings.TrimSpace(afterCardIDFlag.value) != "" ||
		len(assigneeRefFlags.values) > 0 ||
		len(relatedRefFlags.values) > 0 ||
		len(doneFlags.values) > 0
	if strings.TrimSpace(fromFileFlag.value) != "" || (!fieldFlagsSet && strings.TrimSpace(contentFileFlag.value) == "") {
		if fieldFlagsSet {
			return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		body, dryRun, err := a.parseJSONBodyInputWithOptions(args, commandName, jsonBodyInputOptions{allowDryRun: true})
		if err != nil {
			return nil, false, err
		}
		bodyMap, ok := body.(map[string]any)
		if !ok {
			return nil, false, errnorm.Usage("invalid_request", "JSON body for `anx cards create` must be an object")
		}
		ensureEmptyListDefaults(bodyMap, "card", []string{"assignee_refs", "resolution_refs", "related_refs"})
		if err := finalizeOptionalMutationBodyActorID(bodyMap, cfg); err != nil {
			return nil, false, err
		}
		return bodyMap, dryRun, nil
	}
	if err := validatePlacementFlags(beforeCardIDFlag.value, afterCardIDFlag.value, commandName); err != nil {
		return nil, false, err
	}
	boardID := strings.TrimSpace(boardFlag.value)
	if err := validateID(boardID, "board id"); err != nil {
		return nil, false, err
	}
	title := strings.TrimSpace(titleFlag.value)
	if title == "" {
		return nil, false, errnorm.Usage("invalid_request", "`--title` is required for `anx cards create`")
	}
	if strings.TrimSpace(bodyFlag.value) != "" && strings.TrimSpace(contentFileFlag.value) != "" {
		return nil, false, errnorm.Usage("invalid_request", "`--body` and `--body-file` cannot be combined for `anx cards create`")
	}
	if strings.TrimSpace(bodyFlag.value) == "" && strings.TrimSpace(contentFileFlag.value) == "" {
		return nil, false, errnorm.Usage("invalid_request", "`--body` or `--body-file` is required for `anx cards create`")
	}
	content := bodyFlag.value
	if strings.TrimSpace(contentFileFlag.value) != "" {
		var err error
		content, err = a.readTextFlagFile(contentFileFlag.value, commandName, "--body-file")
		if err != nil {
			return nil, false, err
		}
	}
	card := map[string]any{
		"title":           title,
		"summary":         content,
		"column_key":      firstNonEmpty(strings.TrimSpace(columnFlag.value), "backlog"),
		"assignee_refs":   normalizedStringsOrEmpty(assigneeRefFlags.values),
		"resolution_refs": []any{},
		"related_refs":    normalizedStringsOrEmpty(relatedRefFlags.values),
		"risk":            firstNonEmpty(strings.TrimSpace(riskFlag.value), "low"),
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	}
	if topicRef := normalizeTopicFlagRef(topicFlag.value); topicRef != "" {
		card["topic_ref"] = topicRef
	}
	if documentRef := strings.TrimSpace(documentRefFlag.value); documentRef != "" {
		card["document_ref"] = documentRef
	}
	if dueAt := strings.TrimSpace(dueAtFlag.value); dueAt != "" {
		card["due_at"] = dueAt
	}
	if done := normalizeStringFilters(doneFlags.values); len(done) > 0 {
		card["definition_of_done"] = done
	}
	if beforeCardID := strings.TrimSpace(beforeCardIDFlag.value); beforeCardID != "" {
		card["before_card_id"] = beforeCardID
	}
	if afterCardID := strings.TrimSpace(afterCardIDFlag.value); afterCardID != "" {
		card["after_card_id"] = afterCardID
	}
	body := map[string]any{
		"board_id": boardID,
		"card":     card,
	}
	if requestKey := strings.TrimSpace(requestKeyFlag.value); requestKey != "" {
		body["request_key"] = requestKey
	}
	if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
		body["if_board_updated_at"] = ifBoardUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, false, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := finalizeOptionalMutationBodyActorID(body, cfg); err != nil {
		return nil, false, err
	}
	return body, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseTopicPatchInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, map[string]any, bool, error) {
	leadingTopicID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var topicIDFlag, fromFileFlag, titleFlag, summaryFlag, ifUpdatedAtFlag, actorIDFlag trackedString
	var dryRunFlag trackedBool
	fs.Var(&topicIDFlag, "topic-id", "Topic id")
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON patch request body from file")
	fs.Var(&titleFlag, "title", "Topic title")
	fs.Var(&summaryFlag, "summary", "Topic summary")
	fs.Var(&ifUpdatedAtFlag, "if-updated-at", "Topic updated_at concurrency token; discovered from topics get when omitted")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return "", nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	topicID := firstNonEmpty(strings.TrimSpace(topicIDFlag.value), leadingTopicID)
	if topicID == "" && len(positionals) > 0 {
		topicID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateID(topicID, "topic id"); err != nil {
		return "", nil, false, err
	}
	if len(positionals) > 0 {
		return "", nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}

	// Only patch-field flags select JSON vs flag mode; metadata flags stay compatible with stdin/--from-file.
	fieldFlagsSet := strings.TrimSpace(titleFlag.value) != "" ||
		strings.TrimSpace(summaryFlag.value) != ""
	if strings.TrimSpace(fromFileFlag.value) != "" || !fieldFlagsSet {
		if fieldFlagsSet {
			return "", nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
		if err != nil {
			return "", nil, false, err
		}
		if len(payload) == 0 {
			return "", nil, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `anx %s` (provide stdin or --from-file)", commandName))
		}
		bodyAny, err := decodeJSONPayload(payload)
		if err != nil {
			return "", nil, false, err
		}
		bodyMap, ok := bodyAny.(map[string]any)
		if !ok {
			return "", nil, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		return topicID, bodyMap, dryRunFlag.set && dryRunFlag.value, nil
	}

	patch := map[string]any{}
	if title := strings.TrimSpace(titleFlag.value); title != "" {
		patch["title"] = title
	}
	if summary := strings.TrimSpace(summaryFlag.value); summary != "" {
		patch["summary"] = summary
	}
	if len(patch) == 0 {
		return "", nil, false, errnorm.Usage("invalid_request", "`anx topics patch` requires --title, --summary, or --from-file")
	}
	body := map[string]any{"patch": patch}
	if ifUpdatedAt := strings.TrimSpace(ifUpdatedAtFlag.value); ifUpdatedAt != "" {
		body["if_updated_at"] = ifUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", nil, false, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := a.ensureTopicPatchConcurrency(ctx, cfg, topicID, body); err != nil {
		return "", nil, false, err
	}
	return topicID, body, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseCardPatchInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, map[string]any, error) {
	leadingCardID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, fromFileFlag, titleFlag, summaryFlag, columnKeyFlag, ifUpdatedAtFlag, actorIDFlag trackedString
	fs.Var(&cardIDFlag, "card-id", "Card ref, handle, or id")
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON patch request body from file")
	fs.Var(&titleFlag, "title", "Card title")
	fs.Var(&summaryFlag, "summary", "Card summary/body")
	fs.Var(&columnKeyFlag, "column-key", "Board column key; use `anx cards move --column` for placement changes")
	fs.Var(&ifUpdatedAtFlag, "if-updated-at", "Card updated_at concurrency token; discovered from cards get when omitted")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	cardID, err := parseCardIDFromFlagOrPositionals(firstNonEmpty(cardIDFlag.value, leadingCardID), fs.Args(), commandName)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(columnKeyFlag.value) != "" {
		return "", nil, errnorm.Usage("invalid_request", "`--column-key` is not writable with `anx cards patch`; use `anx cards move <card-id> --column <column-key>`")
	}
	// Only patch-field flags select JSON vs flag mode; metadata flags stay compatible with stdin/--from-file.
	fieldFlagsSet := strings.TrimSpace(titleFlag.value) != "" ||
		strings.TrimSpace(summaryFlag.value) != ""
	if strings.TrimSpace(fromFileFlag.value) != "" || !fieldFlagsSet {
		if fieldFlagsSet {
			return "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
		if err != nil {
			return "", nil, err
		}
		if len(payload) == 0 {
			return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `anx %s` (provide stdin or --from-file)", commandName))
		}
		bodyAny, err := decodeJSONPayload(payload)
		if err != nil {
			return "", nil, err
		}
		bodyMap, ok := bodyAny.(map[string]any)
		if !ok {
			return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		return cardID, bodyMap, nil
	}
	patch := map[string]any{}
	if title := strings.TrimSpace(titleFlag.value); title != "" {
		patch["title"] = title
	}
	if summary := strings.TrimSpace(summaryFlag.value); summary != "" {
		patch["summary"] = summary
	}
	if len(patch) == 0 {
		return "", nil, errnorm.Usage("invalid_request", "`anx cards patch` requires --title, --summary, or --from-file")
	}
	body := map[string]any{"patch": patch}
	if ifUpdatedAt := strings.TrimSpace(ifUpdatedAtFlag.value); ifUpdatedAt != "" {
		body["if_updated_at"] = ifUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := a.ensureCardPatchConcurrency(ctx, cfg, cardID, body); err != nil {
		return "", nil, err
	}
	return cardID, body, nil
}

func (a *App) parseCardReviseInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, map[string]any, error) {
	leadingCardID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, contentFileFlag, titleFlag, ifBaseRevisionFlag, actorIDFlag, fromFileFlag trackedString
	fs.Var(&cardIDFlag, "card-id", "Card ref, handle, or id")
	fs.Var(&contentFileFlag, "body-file", "Load revised card summary/body text from a local file or stdin with -")
	fs.Var(&titleFlag, "title", "Optional revised card title")
	fs.Var(&ifBaseRevisionFlag, "if-base-revision", "Base card revision id; discovered from cards get when omitted")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON revision request body from file")
	if err := fs.Parse(args); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	cardID := firstNonEmpty(strings.TrimSpace(cardIDFlag.value), leadingCardID)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(cardID, "card id"); err != nil {
		return "", nil, err
	}
	fieldFlagsSet := strings.TrimSpace(contentFileFlag.value) != "" || strings.TrimSpace(titleFlag.value) != "" || strings.TrimSpace(ifBaseRevisionFlag.value) != "" || strings.TrimSpace(actorIDFlag.value) != ""
	if strings.TrimSpace(fromFileFlag.value) != "" {
		if fieldFlagsSet {
			return "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		_, bodyAny, err := a.parseIDAndBodyInput(append([]string{"--card-id", cardID, "--from-file", fromFileFlag.value}, []string{}...), "card-id", "card id", commandName)
		if err != nil {
			return "", nil, err
		}
		bodyMap, ok := bodyAny.(map[string]any)
		if !ok {
			return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		if err := a.ensureCardRevisionBase(ctx, cfg, cardID, bodyMap); err != nil {
			return "", nil, err
		}
		return cardID, bodyMap, nil
	}
	revision := map[string]any{}
	if contentFile := strings.TrimSpace(contentFileFlag.value); contentFile != "" {
		content, err := a.readTextFlagFile(contentFile, commandName, "--body-file")
		if err != nil {
			return "", nil, err
		}
		revision["summary"] = content
	}
	if title := strings.TrimSpace(titleFlag.value); title != "" {
		revision["title"] = title
	}
	if len(revision) == 0 {
		return "", nil, errnorm.Usage("invalid_request", "`anx cards revise` requires --body-file, --title, or --from-file")
	}
	body := map[string]any{"revision": revision}
	if ifBaseRevision := strings.TrimSpace(ifBaseRevisionFlag.value); ifBaseRevision != "" {
		body["if_base_revision"] = ifBaseRevision
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := a.ensureCardRevisionBase(ctx, cfg, cardID, body); err != nil {
		return "", nil, err
	}
	return cardID, body, nil
}

func parseCardRevisionGetInput(args []string, commandName string) (string, string, error) {
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, revisionIDFlag trackedString
	fs.Var(&cardIDFlag, "card-id", "Card ref, handle, or id")
	fs.Var(&revisionIDFlag, "revision", "Revision id")
	fs.Var(&revisionIDFlag, "revision-id", "Revision id")
	if err := fs.Parse(args); err != nil {
		return "", "", errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	cardID := strings.TrimSpace(cardIDFlag.value)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	revisionID := strings.TrimSpace(revisionIDFlag.value)
	if revisionID == "" && len(positionals) > 0 {
		revisionID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", "", errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(cardID, "card id"); err != nil {
		return "", "", err
	}
	if err := validateID(revisionID, "revision id"); err != nil {
		return "", "", err
	}
	return cardID, revisionID, nil
}

func (a *App) parseCardAssignInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, map[string]any, error) {
	leadingCardID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, ifUpdatedAtFlag, actorIDFlag trackedString
	var assigneeRefFlags trackedStrings
	var clearFlag trackedBool
	fs.Var(&cardIDFlag, "card-id", "Card ref, handle, or id")
	fs.Var(&assigneeRefFlags, "assignee-ref", "Assignee actor typed reference (repeatable)")
	fs.Var(&ifUpdatedAtFlag, "if-updated-at", "Card updated_at concurrency token; discovered from cards get when omitted")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&clearFlag, "clear", "Clear assignees")
	if err := fs.Parse(args); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	cardID, err := parseCardIDFromFlagOrPositionals(firstNonEmpty(cardIDFlag.value, leadingCardID), fs.Args(), commandName)
	if err != nil {
		return "", nil, err
	}
	if clearFlag.value && len(assigneeRefFlags.values) > 0 {
		return "", nil, errnorm.Usage("invalid_request", "--clear cannot be combined with --assignee-ref")
	}
	assignees := normalizedStringsOrEmpty(assigneeRefFlags.values)
	if !clearFlag.value && len(assignees) == 0 {
		return "", nil, errnorm.Usage("invalid_request", "`anx cards assign` requires --assignee-ref or --clear")
	}
	body := map[string]any{"patch": map[string]any{"assignee_refs": assignees}}
	if ifUpdatedAt := strings.TrimSpace(ifUpdatedAtFlag.value); ifUpdatedAt != "" {
		body["if_updated_at"] = ifUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := a.ensureCardPatchConcurrency(ctx, cfg, cardID, body); err != nil {
		return "", nil, err
	}
	return cardID, body, nil
}

func (a *App) parseCardMoveInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, map[string]any, bool, map[string]any, error) {
	leadingCardID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, columnFlag, ifBoardUpdatedAtFlag, actorIDFlag, fromFileFlag trackedString
	var dryRunFlag trackedBool
	fs.Var(&cardIDFlag, "card-id", "Card ref, handle, or id")
	fs.Var(&columnFlag, "column", "Target board column key")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token; use boards get for the parent board when omitted")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON move request body from file or stdin with -")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return "", nil, false, nil, errnorm.Usage("invalid_flags", err.Error())
	}
	cardID, err := parseCardIDFromFlagOrPositionals(firstNonEmpty(cardIDFlag.value, leadingCardID), fs.Args(), commandName)
	if err != nil {
		return "", nil, false, nil, err
	}
	dryRun := dryRunFlag.set && dryRunFlag.value
	recordOverwrite := func(bodyMap map[string]any, overrides map[string]any, key string, newVal any) map[string]any {
		if prev, ok := bodyMap[key]; ok && strings.TrimSpace(anyString(prev)) != "" {
			if overrides == nil {
				overrides = map[string]any{}
			}
			overrides[key] = newVal
		}
		return overrides
	}
	if strings.TrimSpace(fromFileFlag.value) != "" {
		_, bodyAny, err := a.parseIDAndBodyInput(append([]string{"--card-id", cardID, "--from-file", fromFileFlag.value}, []string{}...), "card-id", "card id", commandName)
		if err != nil {
			return "", nil, false, nil, err
		}
		bodyMap, ok := bodyAny.(map[string]any)
		if !ok {
			return "", nil, false, nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		var flagOverwrites map[string]any
		if column := strings.TrimSpace(columnFlag.value); column != "" {
			flagOverwrites = recordOverwrite(bodyMap, flagOverwrites, "column_key", column)
			bodyMap["column_key"] = column
		}
		if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
			flagOverwrites = recordOverwrite(bodyMap, flagOverwrites, "if_board_updated_at", ifBoardUpdatedAt)
			bodyMap["if_board_updated_at"] = ifBoardUpdatedAt
		}
		if rawActorID := strings.TrimSpace(actorIDFlag.value); rawActorID != "" {
			actorID, err := resolveActorIDAlias(rawActorID, cfg)
			if err != nil {
				return "", nil, false, nil, err
			}
			if actorID != "" {
				flagOverwrites = recordOverwrite(bodyMap, flagOverwrites, "actor_id", actorID)
				bodyMap["actor_id"] = actorID
			}
		}
		if dryRun {
			return cardID, bodyMap, true, flagOverwrites, nil
		}
		if err := a.ensureCardMoveConcurrency(ctx, cfg, cardID, bodyMap); err != nil {
			return "", nil, false, nil, err
		}
		return cardID, bodyMap, false, flagOverwrites, nil
	}
	column := strings.TrimSpace(columnFlag.value)
	if column == "" {
		return "", nil, false, nil, errnorm.Usage("invalid_request", "`--column` is required for `anx cards move`")
	}
	body := map[string]any{"column_key": column}
	if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
		body["if_board_updated_at"] = ifBoardUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", nil, false, nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if dryRun {
		return cardID, body, true, nil, nil
	}
	if err := a.ensureCardMoveConcurrency(ctx, cfg, cardID, body); err != nil {
		return "", nil, false, nil, err
	}
	return cardID, body, false, nil, nil
}

type cardResolvePlan struct {
	cardID               string
	moveBody             map[string]any
	resolutionRefs       []string
	evidenceBody         string
	evidenceSummary      string
	evidenceActorIDRaw   string
	hasEvidenceMessage   bool
	dryRun               bool
	dryRunFlagOverwrites map[string]any
}

func (a *App) parseCardResolveInput(args []string, cfg config.Resolved, commandName string) (cardResolvePlan, error) {
	leadingCardID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, columnFlag, resolutionFlag, ifBoardUpdatedAtFlag, actorIDFlag, bodyFlag, bodyFileFlag, reasonFlag, summaryFlag, fromFileFlag trackedString
	var resolutionRefFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&cardIDFlag, "card-id", "Card ref, handle, or id")
	fs.Var(&columnFlag, "column", "Target board column key; defaults to done")
	fs.Var(&resolutionFlag, "resolution", "Resolution value: done (abandon work with trash, not resolution)")
	fs.Var(&resolutionRefFlags, "resolution-ref", "Evidence event/artifact typed ref (repeatable)")
	fs.Var(&reasonFlag, "reason", "Short audit string stamped on the resolve request (not posted to the backing thread)")
	fs.Var(&bodyFlag, "body", "Evidence body posted to the card thread before resolving")
	fs.Var(&bodyFileFlag, "body-file", "Load evidence body text from a local file or stdin with - before resolving")
	fs.Var(&summaryFlag, "summary", "Optional short evidence event summary")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token; use boards get for the parent board when omitted")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON move request body from file or stdin with -")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return cardResolvePlan{}, errnorm.Usage("invalid_flags", err.Error())
	}
	cardID, err := parseCardIDFromFlagOrPositionals(firstNonEmpty(cardIDFlag.value, leadingCardID), fs.Args(), commandName)
	if err != nil {
		return cardResolvePlan{}, err
	}
	dryRun := dryRunFlag.set && dryRunFlag.value
	column := firstNonEmpty(strings.TrimSpace(columnFlag.value), "done")
	resolution := firstNonEmpty(strings.TrimSpace(resolutionFlag.value), "done")
	refs := normalizeStringFilters(resolutionRefFlags.values)
	moveBody := map[string]any{
		"column_key":      column,
		"resolution":      resolution,
		"resolution_refs": refs,
	}
	var dryRunFlagOverwrites map[string]any
	recordOverwrite := func(bodyMap map[string]any, overrides map[string]any, key string, newVal any) map[string]any {
		if prev, ok := bodyMap[key]; ok && strings.TrimSpace(anyString(prev)) != "" {
			if overrides == nil {
				overrides = map[string]any{}
			}
			overrides[key] = newVal
		}
		return overrides
	}
	if strings.TrimSpace(fromFileFlag.value) != "" {
		payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
		if err != nil {
			return cardResolvePlan{}, err
		}
		decoded, err := decodeJSONPayload(payload)
		if err != nil {
			return cardResolvePlan{}, err
		}
		bodyMap, ok := decoded.(map[string]any)
		if !ok {
			return cardResolvePlan{}, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		moveBody = bodyMap
		if strings.TrimSpace(columnFlag.value) != "" {
			dryRunFlagOverwrites = recordOverwrite(moveBody, dryRunFlagOverwrites, "column_key", column)
			moveBody["column_key"] = column
		} else if strings.TrimSpace(anyString(moveBody["column_key"])) == "" {
			moveBody["column_key"] = "done"
		}
		if strings.TrimSpace(resolutionFlag.value) != "" {
			dryRunFlagOverwrites = recordOverwrite(moveBody, dryRunFlagOverwrites, "resolution", resolution)
			moveBody["resolution"] = resolution
		} else if strings.TrimSpace(anyString(moveBody["resolution"])) == "" {
			moveBody["resolution"] = "done"
		}
		refs = normalizeStringFilters(append(stringList(moveBody["resolution_refs"]), resolutionRefFlags.values...))
		moveBody["resolution_refs"] = refs
	}
	if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
		dryRunFlagOverwrites = recordOverwrite(moveBody, dryRunFlagOverwrites, "if_board_updated_at", ifBoardUpdatedAt)
		moveBody["if_board_updated_at"] = ifBoardUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return cardResolvePlan{}, err
	}
	if actorID != "" {
		dryRunFlagOverwrites = recordOverwrite(moveBody, dryRunFlagOverwrites, "actor_id", actorID)
		moveBody["actor_id"] = actorID
	}
	reason := strings.TrimSpace(reasonFlag.value)
	bodyInline := strings.TrimSpace(bodyFlag.value)
	bodyFile := strings.TrimSpace(bodyFileFlag.value)
	hasBodyInput := bodyInline != "" || bodyFile != ""
	if bodyInline != "" && bodyFile != "" {
		return cardResolvePlan{}, errnorm.Usage("invalid_request", "`--body` and `--body-file` cannot be combined for `anx cards resolve`")
	}
	refs = normalizeStringFilters(stringList(moveBody["resolution_refs"]))
	moveBody["resolution_refs"] = refs
	hasResolutionRefs := len(refs) > 0
	hasAuditReason := reason != ""
	if !hasAuditReason && !hasBodyInput && !hasResolutionRefs {
		return cardResolvePlan{}, errnorm.Usage("invalid_request", "`anx cards resolve` requires at least one of `--reason`, `--body`, `--body-file`, or `--resolution-ref`")
	}
	if !hasBodyInput && !hasResolutionRefs {
		return cardResolvePlan{}, errnorm.Usage("invalid_request", "`anx cards resolve` requires typed resolution evidence: use `--body`, `--body-file`, or `--resolution-ref` (optional `--reason` stamps the lifecycle audit only)")
	}
	if hasAuditReason {
		moveBody["reason"] = reason
	}
	var evidenceBody string
	hasEvidenceMessage := hasBodyInput
	if hasBodyInput {
		evidenceBody, err = a.readExplicitMessageText(bodyFlag.value, bodyFileFlag.value, commandName)
		if err != nil {
			return cardResolvePlan{}, err
		}
	}
	evidenceActorRaw := strings.TrimSpace(actorIDFlag.value)
	if evidenceActorRaw == "" {
		evidenceActorRaw = strings.TrimSpace(anyString(moveBody["actor_id"]))
	}
	return cardResolvePlan{
		cardID:               cardID,
		moveBody:             moveBody,
		resolutionRefs:       refs,
		evidenceBody:         evidenceBody,
		evidenceSummary:      summaryFlag.value,
		evidenceActorIDRaw:   evidenceActorRaw,
		hasEvidenceMessage:   hasEvidenceMessage,
		dryRun:               dryRun,
		dryRunFlagOverwrites: dryRunFlagOverwrites,
	}, nil
}

func (a *App) postCardResolveEvidence(ctx context.Context, cfg config.Resolved, plan cardResolvePlan) (string, error) {
	card, err := a.fetchCardBody(ctx, cfg, plan.cardID)
	if err != nil {
		return "", err
	}
	target, err := cardMessageTarget(card, plan.cardID)
	if err != nil {
		return "", err
	}
	body, err := buildMessagePostedBody(cfg, plan.evidenceActorIDRaw, target, plan.evidenceBody, plan.evidenceSummary, nil, "")
	if err != nil {
		return "", err
	}
	if err := validateEventsCreateInput(body, "cards resolve"); err != nil {
		return "", err
	}
	result, err := a.invokeTypedJSON(ctx, cfg, "cards resolve", "events.create", nil, nil, body)
	if err != nil {
		return "", err
	}
	event := extractNestedMap(extractNestedMap(asMap(result.Data), "body"), "event")
	eventID := strings.TrimSpace(anyString(event["id"]))
	if eventID == "" {
		return "", errnorm.Usage("invalid_response", "cards resolve evidence event response did not include event id")
	}
	return "event:" + eventID, nil
}

func (a *App) parseCardReopenInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, map[string]any, error) {
	leadingCardID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(commandName)
	var cardIDFlag, columnFlag, ifBoardUpdatedAtFlag, actorIDFlag trackedString
	fs.Var(&cardIDFlag, "card-id", "Card ref, handle, or id")
	fs.Var(&columnFlag, "column", "Target reopened column; defaults to ready")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token; discovered when omitted")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	cardID, err := parseCardIDFromFlagOrPositionals(firstNonEmpty(cardIDFlag.value, leadingCardID), fs.Args(), commandName)
	if err != nil {
		return "", nil, err
	}
	body := map[string]any{
		"column_key": firstNonEmpty(strings.TrimSpace(columnFlag.value), "ready"),
	}
	if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
		body["if_board_updated_at"] = ifBoardUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := a.ensureCardMoveConcurrency(ctx, cfg, cardID, body); err != nil {
		return "", nil, err
	}
	return cardID, body, nil
}

func parseCardIDFromFlagOrPositionals(raw string, positionals []string, commandName string) (string, error) {
	cardID := strings.TrimSpace(raw)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(cardID, "card id"); err != nil {
		return "", err
	}
	return cardID, nil
}

func normalizeTopicFlagRef(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ":") {
		return raw
	}
	return "topic:" + raw
}

func normalizedStringsOrEmpty(values []string) []string {
	normalized := normalizeStringFilters(values)
	if normalized == nil {
		return []string{}
	}
	return normalized
}

func (a *App) readTextFlagFile(path string, commandName string, flagName string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errnorm.Usage("invalid_request", fmt.Sprintf("`%s` is required for `anx %s`", flagName, commandName))
	}
	content, err := a.readRawFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func (a *App) ensureCardPatchConcurrency(ctx context.Context, cfg config.Resolved, cardID string, body map[string]any) error {
	if strings.TrimSpace(anyString(body["if_updated_at"])) != "" {
		return finalizeOptionalMutationBodyActorID(body, cfg)
	}
	card, err := a.fetchCardBody(ctx, cfg, cardID)
	if err != nil {
		return err
	}
	if updatedAt := strings.TrimSpace(anyString(card["updated_at"])); updatedAt != "" {
		body["if_updated_at"] = updatedAt
	}
	if strings.TrimSpace(anyString(body["if_updated_at"])) == "" {
		return errnorm.Usage("invalid_request", "`if_updated_at` is required; run `anx cards get "+cardID+"` and retry with --if-updated-at <updated_at>")
	}
	return finalizeOptionalMutationBodyActorID(body, cfg)
}

func (a *App) ensureTopicPatchConcurrency(ctx context.Context, cfg config.Resolved, topicID string, body map[string]any) error {
	if strings.TrimSpace(anyString(body["if_updated_at"])) != "" {
		return finalizeOptionalMutationBodyActorID(body, cfg)
	}
	topic, err := a.fetchTopicBody(ctx, cfg, topicID)
	if err != nil {
		return err
	}
	if updatedAt := strings.TrimSpace(anyString(topic["updated_at"])); updatedAt != "" {
		body["if_updated_at"] = updatedAt
	}
	if strings.TrimSpace(anyString(body["if_updated_at"])) == "" {
		return errnorm.Usage("invalid_request", "`if_updated_at` is required; run `anx topics get "+topicID+"` and retry with --if-updated-at <updated_at>")
	}
	return finalizeOptionalMutationBodyActorID(body, cfg)
}

func (a *App) ensureCardRevisionBase(ctx context.Context, cfg config.Resolved, cardID string, body map[string]any) error {
	if strings.TrimSpace(anyString(body["if_base_revision"])) != "" {
		return finalizeOptionalMutationBodyActorID(body, cfg)
	}
	card, err := a.fetchCardBody(ctx, cfg, cardID)
	if err != nil {
		return err
	}
	baseRevision := strings.TrimSpace(anyString(card["head_revision_id"]))
	if baseRevision == "" {
		baseRevision = strings.TrimSpace(anyString(card["head_revision_ref"]))
		baseRevision = strings.TrimPrefix(baseRevision, "card_revision:")
	}
	if baseRevision != "" {
		body["if_base_revision"] = baseRevision
	}
	if strings.TrimSpace(anyString(body["if_base_revision"])) == "" {
		return errnorm.Usage("invalid_request", "`if_base_revision` is required; run `anx cards get "+cardID+"` and retry with --if-base-revision <revision-ref-or-handle>")
	}
	return finalizeOptionalMutationBodyActorID(body, cfg)
}

func (a *App) ensureCardMoveConcurrency(ctx context.Context, cfg config.Resolved, cardID string, body map[string]any) error {
	if strings.TrimSpace(anyString(body["if_board_updated_at"])) != "" {
		return finalizeOptionalMutationBodyActorID(body, cfg)
	}
	card, err := a.fetchCardBody(ctx, cfg, cardID)
	if err != nil {
		return err
	}
	boardID := strings.TrimSpace(anyString(card["board_id"]))
	if boardID == "" {
		boardRef := strings.TrimSpace(anyString(card["board_ref"]))
		if strings.HasPrefix(boardRef, "board:") {
			boardID = strings.TrimSpace(strings.TrimPrefix(boardRef, "board:"))
		}
	}
	if boardID == "" {
		return errnorm.Usage("invalid_request", "`if_board_updated_at` is required because the card response did not include board_id or board_ref")
	}
	boardResult, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "boards get", "boards.get", "board_id", boardID, boardIDLookupSpec, nil, nil)
	if err != nil {
		return err
	}
	boardBody := extractNestedMap(asMap(boardResult.Data), "body")
	board := extractNestedMap(boardBody, "board")
	if updatedAt := strings.TrimSpace(anyString(board["updated_at"])); updatedAt != "" {
		body["if_board_updated_at"] = updatedAt
	}
	if strings.TrimSpace(anyString(body["if_board_updated_at"])) == "" {
		return errnorm.Usage("invalid_request", "`if_board_updated_at` is required; run `anx boards get "+boardID+"` and retry with --if-board-updated-at <updated_at>")
	}
	return finalizeOptionalMutationBodyActorID(body, cfg)
}

func (a *App) fetchCardBody(ctx context.Context, cfg config.Resolved, cardID string) (map[string]any, error) {
	result, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards get", "cards.get", "card_id", cardID, cardIDLookupSpec, nil, nil)
	if err != nil {
		return nil, err
	}
	body := extractNestedMap(asMap(result.Data), "body")
	card := extractNestedMap(body, "card")
	if len(card) == 0 {
		return nil, errnorm.Usage("invalid_request", "cards get response did not include card metadata")
	}
	return card, nil
}

func (a *App) fetchTopicBody(ctx context.Context, cfg config.Resolved, topicID string) (map[string]any, error) {
	result, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "topics get", "topics.get", "topic_id", topicID, topicIDLookupSpec, nil, nil)
	if err != nil {
		return nil, err
	}
	body := extractNestedMap(asMap(result.Data), "body")
	topic := extractNestedMap(body, "topic")
	if len(topic) == 0 {
		return nil, errnorm.Usage("invalid_request", "topics get response did not include topic metadata")
	}
	return topic, nil
}

func (a *App) normalizeTopicMutationBody(ctx context.Context, cfg config.Resolved, commandID string, body map[string]any) error {
	switch commandID {
	case "topics.create":
		topic := nestedMutationMap(body, "topic")
		if err := a.normalizeMutationFields(ctx, cfg, topic, []mutationFieldSpec{
			{key: "owner_refs", kind: mutationFieldTypedRefList},
			{key: "document_refs", kind: mutationFieldTypedRefList},
			{key: "board_refs", kind: mutationFieldTypedRefList},
			{key: "related_refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return nil
	case "topics.patch":
		patch := nestedMutationMap(body, "patch")
		if err := a.normalizeMutationFields(ctx, cfg, patch, []mutationFieldSpec{
			{key: "owner_refs", kind: mutationFieldTypedRefList},
			{key: "document_refs", kind: mutationFieldTypedRefList},
			{key: "board_refs", kind: mutationFieldTypedRefList},
			{key: "related_refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return nil
	case "topics.trash":
		return nil
	default:
		return nil
	}
}

func (a *App) normalizeCardMutationBody(ctx context.Context, cfg config.Resolved, commandID string, pathParams map[string]string, body map[string]any) error {
	switch commandID {
	case "cards.create":
		card := nestedMutationMap(body, "card")
		if card == nil {
			return nil
		}
		if rawBoardID := strings.TrimSpace(anyString(body["board_id"])); rawBoardID != "" {
			resolvedBoard, err := a.resolveMaybeBoardID(ctx, cfg, rawBoardID)
			if err != nil {
				return err
			}
			body["board_id"] = resolvedBoard
		}
		if err := a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "board_ref", kind: mutationFieldTypedRef},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, card, []mutationFieldSpec{
			{key: "assignee_refs", kind: mutationFieldTypedRefList},
			{key: "related_refs", kind: mutationFieldTypedRefList},
			{key: "topic_ref", kind: mutationFieldTypedRef},
			{key: "document_ref", kind: mutationFieldTypedRef},
		})
	case "cards.patch":
		patch := nestedMutationMap(body, "patch")
		if patch == nil {
			return nil
		}
		return a.normalizeMutationFields(ctx, cfg, patch, []mutationFieldSpec{
			{key: "assignee_refs", kind: mutationFieldTypedRefList},
			{key: "related_refs", kind: mutationFieldTypedRefList},
			{key: "resolution_refs", kind: mutationFieldTypedRefList},
			{key: "topic_ref", kind: mutationFieldTypedRef},
			{key: "document_ref", kind: mutationFieldTypedRef},
		})
	case "cards.move":
		move := effectiveCardMoveMutationMap(body)
		if move == nil {
			return nil
		}
		if err := a.normalizeMutationFields(ctx, cfg, move, []mutationFieldSpec{
			{key: "resolution_refs", kind: mutationFieldTypedRefList},
			{key: "after_thread_id", kind: mutationFieldThreadID},
			{key: "before_thread_id", kind: mutationFieldThreadID},
		}); err != nil {
			return err
		}
		if pathParams == nil {
			return nil
		}
		rawBoardID := strings.TrimSpace(pathParams["board_id"])
		if rawBoardID == "" {
			return nil
		}
		resolvedBoard, err := a.resolveMaybeBoardID(ctx, cfg, rawBoardID)
		if err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "before_card_id"); err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "after_card_id"); err != nil {
			return err
		}
		if moveNest, ok := body["move"].(map[string]any); ok && moveNest != nil {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, moveNest, "before_card_id"); err != nil {
				return err
			}
			return a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, moveNest, "after_card_id")
		}
		return nil
	default:
		return nil
	}
}

func (a *App) normalizeMutationCommandBody(ctx context.Context, cfg config.Resolved, commandID string, pathParams map[string]string, body map[string]any) error {
	if strings.HasPrefix(commandID, "topics.") {
		return a.normalizeTopicMutationBody(ctx, cfg, commandID, body)
	}
	if strings.HasPrefix(commandID, "cards.") {
		return a.normalizeCardMutationBody(ctx, cfg, commandID, pathParams, body)
	}
	return a.normalizeMutationCommandBodyLegacy(ctx, cfg, commandID, pathParams, body)
}

func (a *App) normalizeMutationCommandBodyLegacy(ctx context.Context, cfg config.Resolved, commandID string, pathParams map[string]string, body map[string]any) error {
	switch commandID {
	case "boards.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "board"), []mutationFieldSpec{
			{key: "primary_topic_ref", kind: mutationFieldTypedRef},
			{key: "pinned_refs", kind: mutationFieldTypedRefList},
		})
	case "boards.patch":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "patch"), []mutationFieldSpec{
			{key: "primary_topic_ref", kind: mutationFieldTypedRef},
			{key: "pinned_refs", kind: mutationFieldTypedRefList},
		})
	case "cards.create":
		rawBoardID := strings.TrimSpace(anyString(body["board_id"]))
		if rawBoardID == "" {
			refStr := strings.TrimSpace(anyString(body["board_ref"]))
			if strings.HasPrefix(refStr, "board:") {
				rawBoardID = strings.TrimSpace(strings.TrimPrefix(refStr, "board:"))
			}
		}
		if rawBoardID == "" {
			return nil
		}
		resolvedBoard, err := a.resolveMaybeBoardID(ctx, cfg, rawBoardID)
		if err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "before_card_id"); err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "after_card_id"); err != nil {
			return err
		}
		if cardNest, ok := body["card"].(map[string]any); ok && cardNest != nil {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "before_card_id"); err != nil {
				return err
			}
			return a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "after_card_id")
		}
		return nil
	case "boards.cards.add":
		if pathParams == nil {
			return nil
		}
		rawBoardID := strings.TrimSpace(pathParams["board_id"])
		if rawBoardID == "" {
			return nil
		}
		resolvedBoard, err := a.resolveMaybeBoardID(ctx, cfg, rawBoardID)
		if err != nil {
			return err
		}
		if err := a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "after_thread_id", kind: mutationFieldThreadID},
			{key: "parent_thread", kind: mutationFieldThreadID},
		}); err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "before_card_id"); err != nil {
			return err
		}
		if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, body, "after_card_id"); err != nil {
			return err
		}
		if cardNest, ok := body["card"].(map[string]any); ok && cardNest != nil {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "before_card_id"); err != nil {
				return err
			}
			return a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "after_card_id")
		}
		return nil
	case "boards.cards.batch_add":
		if pathParams == nil {
			return nil
		}
		rawBoardID := strings.TrimSpace(pathParams["board_id"])
		if rawBoardID == "" {
			return nil
		}
		resolvedBoard, err := a.resolveMaybeBoardID(ctx, cfg, rawBoardID)
		if err != nil {
			return err
		}
		rawItems, ok := body["items"].([]any)
		if !ok {
			return nil
		}
		for _, el := range rawItems {
			item, ok := el.(map[string]any)
			if !ok {
				continue
			}
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, item, "before_card_id"); err != nil {
				return err
			}
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, item, "after_card_id"); err != nil {
				return err
			}
			if cardNest, ok := item["card"].(map[string]any); ok && cardNest != nil {
				if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "before_card_id"); err != nil {
					return err
				}
				if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoard, cardNest, "after_card_id"); err != nil {
					return err
				}
			}
		}
		return nil
	case "docs.create", "docs.update", "docs.revisions.create":
		if rev, ok := body["revision"].(map[string]any); ok && rev != nil {
			if err := a.normalizeMutationFields(ctx, cfg, rev, []mutationFieldSpec{
				{key: "refs", kind: mutationFieldTypedRefList},
			}); err != nil {
				return err
			}
		}
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "events.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "event"), []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "inbox.respond":
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "related_refs", kind: mutationFieldTypedRefList},
		})
	case "agent.notifications.read", "agent.notifications.dismiss":
		raw := strings.TrimSpace(anyString(body["wakeup_id"]))
		if raw != "" {
			body["wakeup_id"] = raw
		}
		return nil
	default:
		return nil
	}
}

func (a *App) parseTopicIDAndBodyInput(args []string, commandName string) (string, map[string]any, error) {
	id, body, err := a.parseIDAndBodyInput(args, "topic-id", "topic id", commandName)
	if err != nil {
		return "", nil, err
	}
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
	}
	return id, bodyMap, nil
}

var refEdgesSubcommandSpec = subcommandSpec{
	command:  "ref-edges",
	valid:    []string{"list"},
	examples: []string{`anx ref-edges list --target-ref card:implement-login`},
}

func (a *App) runRefEdgesCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		if text, ok := generatedHelpText("ref-edges"); ok {
			return &commandResult{Text: text}, "ref-edges", nil
		}
		return nil, "ref-edges", refEdgesSubcommandSpec.requiredError()
	}
	sub := refEdgesSubcommandSpec.normalize(args[0])
	if sub != "list" {
		return nil, "ref-edges", refEdgesSubcommandSpec.unknownError(args[0])
	}
	fs := newSilentFlagSet("ref-edges list")
	var sourceRef, targetRef, relation trackedString
	fs.Var(&sourceRef, "source-ref", "Typed source ref for forward lookup")
	fs.Var(&targetRef, "target-ref", "Typed target ref for reverse lookup")
	fs.Var(&relation, "relation", "Optional relation filter")
	if err := fs.Parse(args[1:]); err != nil {
		return nil, "ref-edges list", errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, "ref-edges list", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx ref-edges list`")
	}
	sourceRefValue := strings.TrimSpace(sourceRef.value)
	targetRefValue := strings.TrimSpace(targetRef.value)
	if (sourceRefValue == "" && targetRefValue == "") || (sourceRefValue != "" && targetRefValue != "") {
		return nil, "ref-edges list", errnorm.Usage("invalid_request", "specify exactly one of --source-ref or --target-ref")
	}
	q := make([]queryParam, 0, 3)
	addSingleQuery(&q, "source_ref", sourceRefValue)
	addSingleQuery(&q, "target_ref", targetRefValue)
	addSingleQuery(&q, "relation", strings.TrimSpace(relation.value))
	result, err := a.invokeTypedJSON(ctx, cfg, "ref-edges list", "ref_edges.list", nil, q, nil)
	return result, "ref-edges list", err
}
