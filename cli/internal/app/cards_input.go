// Input parsing for `anx cards` mutations: create, patch, revise, assign,
// move, reopen and card id resolution from flags/positionals.

package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) parseCardCreateInput(args []string, cfg config.Resolved, commandName string) (map[string]any, bool, bool, error) {
	fs := newSilentFlagSet(commandName)
	var boardFlag, titleFlag, bodyFlag, contentFileFlag, fromFileFlag, handleFlag trackedString
	var actorIDFlag, requestKeyFlag, ifBoardUpdatedAtFlag, underFlag, afterStepFlag trackedString
	var columnFlag, topicFlag, documentRefFlag, riskFlag, dueAtFlag trackedString
	var beforeCardIDFlag, afterCardIDFlag trackedString
	var assigneeRefFlags, relatedRefFlags, doneFlags trackedStrings
	var dryRunFlag, topLevelFlag trackedBool
	fs.Var(&topLevelFlag, "top-level", "Ignore the run-label parent default")
	fs.Var(&underFlag, "under", "Parent card ref; adds a plan step atomically")
	fs.Var(&afterStepFlag, "after", "Preceding plan step id")
	fs.Var(&boardFlag, "board", "Parent board ref, handle, or id for the new card")
	fs.Var(&boardFlag, "board-id", "Parent board ref, handle, or id for the new card")
	fs.Var(&titleFlag, "title", "Card title")
	fs.Var(&handleFlag, "handle", "Client-chosen lowercase slug (1-64 characters); conflicts fail without renaming")
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
		return nil, false, false, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, false, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if topLevelFlag.value && underFlag.set {
		return nil, false, false, errnorm.Usage("invalid_request", "--top-level and --under cannot be combined")
	}
	fieldFlagsSet := handleFlag.set || strings.TrimSpace(boardFlag.value) != "" ||
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
			return nil, false, false, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		body, dryRun, err := a.parseJSONBodyInputWithOptions(withoutGroupingFlags(args), commandName, jsonBodyInputOptions{allowDryRun: true})
		if err != nil {
			return nil, false, false, err
		}
		bodyMap, ok := body.(map[string]any)
		if !ok {
			return nil, false, false, errnorm.Usage("invalid_request", "JSON body for `anx cards create` must be an object")
		}
		ensureEmptyListDefaults(bodyMap, "card", []string{"assignee_refs", "resolution_refs", "related_refs"})
		if err := finalizeOptionalMutationBodyActorID(bodyMap, cfg); err != nil {
			return nil, false, false, err
		}
		applyCardGroupingFlags(bodyMap, underFlag.value, afterStepFlag.value)
		return bodyMap, dryRun, topLevelFlag.value, nil
	}
	if err := validatePlacementFlags(beforeCardIDFlag.value, afterCardIDFlag.value, commandName); err != nil {
		return nil, false, false, err
	}
	boardID := strings.TrimSpace(boardFlag.value)
	if err := validateID(boardID, "board id"); err != nil {
		return nil, false, false, err
	}
	title := strings.TrimSpace(titleFlag.value)
	if title == "" {
		return nil, false, false, errnorm.Usage("invalid_request", "`--title` is required for `anx cards create`")
	}
	if strings.TrimSpace(bodyFlag.value) != "" && strings.TrimSpace(contentFileFlag.value) != "" {
		return nil, false, false, errnorm.Usage("invalid_request", "`--body` and `--body-file` cannot be combined for `anx cards create`")
	}
	if strings.TrimSpace(bodyFlag.value) == "" && strings.TrimSpace(contentFileFlag.value) == "" {
		return nil, false, false, errnorm.Usage("invalid_request", "`--body` or `--body-file` is required for `anx cards create`")
	}
	content := bodyFlag.value
	if strings.TrimSpace(contentFileFlag.value) != "" {
		var err error
		content, err = a.readTextFlagFile(contentFileFlag.value, commandName, "--body-file")
		if err != nil {
			return nil, false, false, err
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
	if handleFlag.set {
		if err := validateCardHandle(handleFlag.value); err != nil {
			return nil, false, false, err
		}
		card["handle"] = handleFlag.value
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
		"board_id":       boardID,
		"create_surface": "cards.create",
		"card":           card,
	}
	applyCardGroupingFlags(body, underFlag.value, afterStepFlag.value)
	if requestKey := strings.TrimSpace(requestKeyFlag.value); requestKey != "" {
		body["request_key"] = requestKey
	}
	if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
		body["if_board_updated_at"] = ifBoardUpdatedAt
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, false, false, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := finalizeOptionalMutationBodyActorID(body, cfg); err != nil {
		return nil, false, false, err
	}
	return body, dryRunFlag.set && dryRunFlag.value, topLevelFlag.value, nil
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
	var destinationBoard, reason trackedString
	var dryRunFlag trackedBool
	fs.Var(&cardIDFlag, "card-id", "Card ref, handle, or id")
	fs.Var(&destinationBoard, "board", "Destination board ref; changes primary membership")
	fs.Var(&reason, "reason", "Audit reason for board membership change")
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
		if destinationBoard.set {
			bodyMap["board_ref"] = destinationBoard.value
		}
		if reason.set {
			bodyMap["reason"] = reason.value
		}
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
	if destinationBoard.set {
		body["board_ref"] = destinationBoard.value
	}
	if reason.set {
		body["reason"] = reason.value
	}
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
