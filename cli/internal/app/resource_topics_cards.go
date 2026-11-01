// `anx topics`, `anx cards` and `anx ref-edges` command dispatch for the typed
// resource surface. Input parsing lives in topics_input.go / cards_input.go,
// mutation body handling in topics_cards_mutations.go.

package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
	"agent-nexus-cli/runcontext"
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
		"anx cards assign card:implement-login --assignee-ref \"${ANX_ASSIGNEE_REF:?set this to the intended actor ref}\"",
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
			result, callErr := a.invokeTypedJSON(ctx, cfg, "cards list", "boards.cards.list", map[string]string{"board_id": boardID}, nil, nil)
			return result, "cards list", callErr
		}
		query := make([]queryParam, 0, 4)
		if err := appendLifecycleStatesForHTTPList(&query, "", includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "cards list", err
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "cards list", "cards.list", nil, query, nil)
		return result, "cards list", err
	case "create":
		body, dryRun, topLevel, err := a.parseCardCreateInput(args[1:], cfg, "cards create")
		if err != nil {
			return nil, "cards create", err
		}
		ctx = runcontext.WithTopLevel(ctx, topLevel)
		if dryRun {
			hint := a.prepareRunParent(ctx, cfg, body, false)
			result := dryRunResult("cards create", "cards.create", nil, nil, body)
			for _, warning := range hint.Warnings {
				result.Warnings = append(result.Warnings, output.Warning{Code: warning.Code, Message: warning.Message})
			}
			return result, "cards create", nil
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
