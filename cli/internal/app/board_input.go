// Input parsing for board and board-card mutations: create, batch create,
// update, move, archive, and board-scoped list targeting.

package app

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) parseBoardCreateInput(args []string, cfg config.Resolved, commandName string) (any, bool, error) {
	fs := newSilentFlagSet(commandName)
	var fromFileFlag, titleFlag, summaryFlag, topicFlag, actorIDFlag, roleFlag trackedString
	var documentRefFlags, pinnedRefFlags trackedStrings
	var dryRunFlag trackedBool
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON request body from file or stdin with -")
	fs.Var(&roleFlag, "role", "Board role; initiatives selects Overview cards")
	fs.Var(&titleFlag, "title", "Board title")
	fs.Var(&summaryFlag, "summary", "Board summary")
	fs.Var(&topicFlag, "topic", "Primary topic typed ref or handle")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&documentRefFlags, "document-ref", "Linked document typed ref, repeatable")
	fs.Var(&pinnedRefFlags, "ref", "Pinned/related typed ref, repeatable")
	fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	if err := fs.Parse(args); err != nil {
		return nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	fieldFlagsSet := roleFlag.set || strings.TrimSpace(titleFlag.value) != "" ||
		strings.TrimSpace(summaryFlag.value) != "" ||
		strings.TrimSpace(topicFlag.value) != "" ||
		strings.TrimSpace(actorIDFlag.value) != "" ||
		len(documentRefFlags.values) > 0 ||
		len(pinnedRefFlags.values) > 0
	if strings.TrimSpace(fromFileFlag.value) != "" || !fieldFlagsSet {
		if fieldFlagsSet {
			return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		body, dryRun, err := a.parseJSONBodyInputWithOptions(args, commandName, jsonBodyInputOptions{allowDryRun: true})
		if err != nil {
			return nil, false, err
		}
		if bodyMap, ok := body.(map[string]any); ok {
			if err := finalizeOptionalMutationBodyActorID(bodyMap, cfg); err != nil {
				return nil, false, err
			}
		}
		return body, dryRun, nil
	}
	title := strings.TrimSpace(titleFlag.value)
	if title == "" {
		return nil, false, errnorm.Usage("invalid_request", "`--title` is required for `anx boards create`")
	}
	board := map[string]any{
		"title":         title,
		"document_refs": normalizedStringsOrEmpty(documentRefFlags.values),
		"pinned_refs":   normalizedStringsOrEmpty(pinnedRefFlags.values),
		"provenance":    map[string]any{"sources": []any{"event:anx-cli"}},
	}
	if roleFlag.set {
		board["role"] = strings.TrimSpace(roleFlag.value)
	}
	if summary := strings.TrimSpace(summaryFlag.value); summary != "" {
		board["summary"] = summary
	}
	if topicRef := normalizeTypedRefFlag(topicFlag.value, "topic"); topicRef != "" {
		board["primary_topic_ref"] = topicRef
		board["pinned_refs"] = uniqueStrings(append(stringList(board["pinned_refs"]), topicRef))
	}
	body := map[string]any{"board": board}
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

func (a *App) parseBoardCardBoardScopedTarget(args []string, commandName string) (string, string, error) {
	fs := newSilentFlagSet(commandName)
	var boardIDFlag, cardIDFlag trackedString
	fs.Var(&boardIDFlag, "board-id", "Board id")
	fs.Var(&cardIDFlag, "card-id", "Card id")
	if err := fs.Parse(args); err != nil {
		return "", "", errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	boardID := strings.TrimSpace(boardIDFlag.value)
	if boardID == "" && len(positionals) > 0 {
		boardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	cardID := strings.TrimSpace(cardIDFlag.value)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", "", errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(boardID, "board id"); err != nil {
		return "", "", err
	}
	if err := validateID(cardID, "card id"); err != nil {
		return "", "", err
	}
	return boardID, cardID, nil
}

func (a *App) parseBoardBatchCardCreateInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, any, bool, error) {
	fs := newSilentFlagSet(commandName)
	var boardIDFlag, fromFileFlag, underFlag trackedString
	var topLevelFlag trackedBool
	fs.Var(&underFlag, "under", "Parent for every item in the batch")
	fs.Var(&topLevelFlag, "top-level", "Ignore the run-label parent default")
	var actorIDFlag, requestKeyFlag, ifBoardUpdatedAtFlag trackedString
	fs.Var(&boardIDFlag, "board-id", "Board id")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path or stdin with -")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&requestKeyFlag, "request-key", "Request key")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token")
	if err := fs.Parse(args); err != nil {
		return "", nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	if topLevelFlag.value && underFlag.set {
		return "", nil, false, errnorm.Usage("invalid_request", "--top-level and --under cannot be combined")
	}
	positionals := fs.Args()
	boardID := strings.TrimSpace(boardIDFlag.value)
	if boardID == "" && len(positionals) > 0 {
		boardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(boardID, "board id"); err != nil {
		return "", nil, false, err
	}
	payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return "", nil, false, err
	}
	if len(payload) == 0 {
		return "", nil, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `anx %s` (provide stdin or --from-file)", commandName))
	}
	body, err := decodeJSONPayload(payload)
	if err != nil {
		return "", nil, false, err
	}
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return "", nil, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
	}
	if _, has := bodyMap["items"]; !has {
		return "", nil, false, errnorm.Usage("invalid_request", "JSON body must include an items array")
	}
	if underFlag.set {
		for _, raw := range asSlice(bodyMap["items"]) {
			if item := asMap(raw); item != nil {
				item["under"] = underFlag.value
			}
		}
	}
	// After JSON decode, non-empty CLI flags override the same keys in the body (explicit CLI wins).
	if v := strings.TrimSpace(requestKeyFlag.value); v != "" {
		bodyMap["request_key"] = v
	}
	if v := strings.TrimSpace(ifBoardUpdatedAtFlag.value); v != "" {
		bodyMap["if_board_updated_at"] = v
	}
	if strings.TrimSpace(actorIDFlag.value) != "" {
		aid, err := resolveActorIDAlias(actorIDFlag.value, cfg)
		if err != nil {
			return "", nil, false, err
		}
		if aid != "" {
			bodyMap["actor_id"] = aid
		}
	}
	if err := finalizeMutationActorID(bodyMap, cfg); err != nil {
		return "", nil, false, err
	}
	resolvedBoardID := boardID
	return resolvedBoardID, bodyMap, topLevelFlag.value, nil
}

func (a *App) parseBoardCardUpdateInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (map[string]string, any, error) {
	fs := newSilentFlagSet(commandName)
	var cardIDFlag trackedString
	var titleFlag, summaryFlag trackedString
	var actorIDFlag, ifUpdatedAtFlag, fromFileFlag trackedString
	var assigneeRefFlags trackedStrings
	var resolutionFlag trackedString
	var documentRefFlag trackedString
	var clearDocumentRefFlag trackedBool
	fs.Var(&cardIDFlag, "card-id", "Card id")
	fs.Var(&titleFlag, "title", "Card title")
	fs.Var(&summaryFlag, "summary", "Card summary")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&ifUpdatedAtFlag, "if-updated-at", "Card updated_at concurrency token")
	fs.Var(&assigneeRefFlags, "assignee-ref", "Assignee actor typed reference (repeatable)")
	fs.Var(&resolutionFlag, "resolution", "Terminal resolution (done only); card must already be in the done column (use `anx cards move` to change columns)")
	fs.Var(&documentRefFlag, "document-ref", "Document typed reference")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path or stdin with -")
	fs.Var(&clearDocumentRefFlag, "clear-document-ref", "Clear the document ref")
	if err := fs.Parse(args); err != nil {
		return nil, nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	cardID := strings.TrimSpace(cardIDFlag.value)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(cardID, "card id"); err != nil {
		return nil, nil, err
	}

	skipStdin := hasAnyBoardMutationFieldFlags(titleFlag, summaryFlag, actorIDFlag, ifUpdatedAtFlag, assigneeRefFlags, resolutionFlag, documentRefFlag, clearDocumentRefFlag)
	payload, err := a.readBoardCardBodyInput(fromFileFlag.value, skipStdin)
	if err != nil {
		return nil, nil, err
	}
	if len(payload) > 0 {
		if hasAnyBoardMutationFieldFlags(titleFlag, summaryFlag, actorIDFlag, ifUpdatedAtFlag, assigneeRefFlags, resolutionFlag, documentRefFlag, clearDocumentRefFlag) {
			return nil, nil, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		body, err := decodeJSONPayload(payload)
		if err != nil {
			return nil, nil, err
		}
		bodyMap, _ := body.(map[string]any)
		if bodyMap == nil {
			return nil, nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		pathParams, err := buildBoardCardCommandPathParams(cardID)
		if err != nil {
			return nil, nil, err
		}
		return pathParams, bodyMap, nil
	}

	patch := map[string]any{}
	if title := strings.TrimSpace(titleFlag.value); title != "" {
		patch["title"] = title
	}
	if summary := strings.TrimSpace(summaryFlag.value); summary != "" {
		patch["summary"] = summary
	}
	if assigneeRefs := normalizeStringFilters(assigneeRefFlags.values); len(assigneeRefs) > 0 {
		patch["assignee_refs"] = assigneeRefs
	}
	if resolution := strings.TrimSpace(resolutionFlag.value); resolution != "" {
		patch["resolution"] = resolution
	}
	if clearDocumentRefFlag.set && clearDocumentRefFlag.value {
		if strings.TrimSpace(documentRefFlag.value) != "" {
			return nil, nil, errnorm.Usage("invalid_request", fmt.Sprintf("--document-ref and --clear-document-ref cannot be combined for `anx %s`", commandName))
		}
		patch["document_ref"] = nil
	} else if documentRef := strings.TrimSpace(documentRefFlag.value); documentRef != "" {
		patch["document_ref"] = documentRef
	}
	body := map[string]any{
		"patch": patch,
	}
	if ifUpdated := strings.TrimSpace(ifUpdatedAtFlag.value); ifUpdated != "" {
		body["if_updated_at"] = ifUpdated
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	pathParams, err := buildBoardCardCommandPathParams(cardID)
	if err != nil {
		return nil, nil, err
	}
	return pathParams, body, nil
}

func (a *App) parseBoardCardMoveInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (string, string, any, error) {
	fs := newSilentFlagSet(commandName)
	var boardIDFlag, cardIDFlag trackedString
	var actorIDFlag, ifBoardUpdatedAtFlag, fromFileFlag trackedString
	var columnFlag, beforeCardIDFlag, afterCardIDFlag trackedString
	fs.Var(&boardIDFlag, "board-id", "Board id")
	fs.Var(&cardIDFlag, "card-id", "Card id")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path or stdin with -")
	fs.Var(&columnFlag, "column", "Target board column key")
	fs.Var(&beforeCardIDFlag, "before-card-id", "Place before this card id")
	fs.Var(&afterCardIDFlag, "after-card-id", "Place after this card id")
	if err := fs.Parse(args); err != nil {
		return "", "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	boardID := strings.TrimSpace(boardIDFlag.value)
	if boardID == "" && len(positionals) > 0 {
		boardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	cardID := strings.TrimSpace(cardIDFlag.value)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", "", nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validatePlacementFlags(beforeCardIDFlag.value, afterCardIDFlag.value, commandName); err != nil {
		return "", "", nil, err
	}
	if err := validateID(boardID, "board id"); err != nil {
		return "", "", nil, err
	}
	identifier := strings.TrimSpace(cardID)
	if err := validateID(identifier, "card id"); err != nil {
		return "", "", nil, err
	}
	resolvedBoardID := boardID
	skipStdin := hasAnyBoardMutationFieldFlags(actorIDFlag, ifBoardUpdatedAtFlag, columnFlag, beforeCardIDFlag, afterCardIDFlag)
	payload, err := a.readBoardCardBodyInput(fromFileFlag.value, skipStdin)
	if err != nil {
		return "", "", nil, err
	}
	if len(payload) > 0 {
		body, err := decodeJSONPayload(payload)
		if err != nil {
			return "", "", nil, err
		}
		bodyMap, _ := body.(map[string]any)
		if bodyMap == nil {
			return "", "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		if strings.TrimSpace(beforeCardIDFlag.value) == "" {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoardID, bodyMap, "before_card_id"); err != nil {
				return "", "", nil, err
			}
		}
		if strings.TrimSpace(afterCardIDFlag.value) == "" {
			if err := a.normalizeBoardMutationCardAnchorField(ctx, cfg, resolvedBoardID, bodyMap, "after_card_id"); err != nil {
				return "", "", nil, err
			}
		}
		if column := strings.TrimSpace(columnFlag.value); column != "" {
			bodyMap["column_key"] = column
		}
		if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
			bodyMap["if_board_updated_at"] = ifBoardUpdatedAt
		}
		if beforeCardID := strings.TrimSpace(beforeCardIDFlag.value); beforeCardID != "" {
			bodyMap["before_card_id"] = beforeCardID
		}
		if afterCardID := strings.TrimSpace(afterCardIDFlag.value); afterCardID != "" {
			bodyMap["after_card_id"] = afterCardID
		}
		if rawActorID := strings.TrimSpace(actorIDFlag.value); rawActorID != "" {
			actorID, err := resolveActorIDAlias(rawActorID, cfg)
			if err != nil {
				return "", "", nil, err
			}
			if actorID != "" {
				bodyMap["actor_id"] = actorID
			}
		}
		if err := finalizeMutationActorID(bodyMap, cfg); err != nil {
			return "", "", nil, err
		}
		return resolvedBoardID, identifier, bodyMap, nil
	}

	body := map[string]any{
		"if_board_updated_at": strings.TrimSpace(ifBoardUpdatedAtFlag.value),
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return "", "", nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if column := strings.TrimSpace(columnFlag.value); column != "" {
		body["column_key"] = column
	}
	if beforeCardID := strings.TrimSpace(beforeCardIDFlag.value); beforeCardID != "" {
		body["before_card_id"] = beforeCardID
	}
	if afterCardID := strings.TrimSpace(afterCardIDFlag.value); afterCardID != "" {
		body["after_card_id"] = afterCardID
	}
	return resolvedBoardID, identifier, body, nil
}

func (a *App) parseBoardCardArchiveInput(ctx context.Context, args []string, cfg config.Resolved, commandName string) (map[string]string, any, error) {
	fs := newSilentFlagSet(commandName)
	var cardIDFlag trackedString
	var actorIDFlag, ifBoardUpdatedAtFlag, fromFileFlag trackedString
	fs.Var(&cardIDFlag, "card-id", "Card id")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	fs.Var(&ifBoardUpdatedAtFlag, "if-board-updated-at", "Board updated_at concurrency token")
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path or stdin with -")
	if err := fs.Parse(args); err != nil {
		return nil, nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	cardID := strings.TrimSpace(cardIDFlag.value)
	if cardID == "" && len(positionals) > 0 {
		cardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, nil, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	if err := validateID(cardID, "card id"); err != nil {
		return nil, nil, err
	}

	skipStdin := hasAnyBoardMutationFieldFlags(actorIDFlag, ifBoardUpdatedAtFlag)
	payload, err := a.readBoardCardBodyInput(fromFileFlag.value, skipStdin)
	if err != nil {
		return nil, nil, err
	}
	if len(payload) > 0 {
		if hasAnyBoardMutationFieldFlags(actorIDFlag, ifBoardUpdatedAtFlag) {
			return nil, nil, errnorm.Usage("invalid_args", fmt.Sprintf("field flags cannot be combined with JSON body input for `anx %s`", commandName))
		}
		body, err := decodeJSONPayload(payload)
		if err != nil {
			return nil, nil, err
		}
		bodyMap, _ := body.(map[string]any)
		if bodyMap == nil {
			return nil, nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object", commandName))
		}
		if err := finalizeMutationActorID(bodyMap, cfg); err != nil {
			return nil, nil, err
		}
		pathParams, err := buildBoardCardCommandPathParams(cardID)
		if err != nil {
			return nil, nil, err
		}
		return pathParams, bodyMap, nil
	}

	body := map[string]any{}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if ifBoardUpdatedAt := strings.TrimSpace(ifBoardUpdatedAtFlag.value); ifBoardUpdatedAt != "" {
		body["if_board_updated_at"] = ifBoardUpdatedAt
	}
	pathParams, err := buildBoardCardCommandPathParams(cardID)
	if err != nil {
		return nil, nil, err
	}
	return pathParams, body, nil
}

func buildBoardCardCommandPathParams(cardID string) (map[string]string, error) {
	cardID = strings.TrimSpace(cardID)
	if cardID == "" {
		return nil, errnorm.Usage("invalid_request", "card id is required")
	}
	return map[string]string{"card_id": cardID}, nil
}

func (a *App) normalizeBoardMutationCardAnchorField(ctx context.Context, cfg config.Resolved, boardID string, body map[string]any, field string) error {
	_ = ctx
	_ = cfg
	_ = boardID
	rawID := strings.TrimSpace(anyString(body[field]))
	if rawID == "" {
		return nil
	}
	body[field] = rawID
	return nil
}

func parseBoardCardsListInput(args []string) (string, bool, error) {
	fs := newSilentFlagSet("boards cards list")
	var boardIDFlag trackedString
	var fullIDFlag trackedBool
	fs.Var(&boardIDFlag, "board-id", "Board id")
	fs.Var(&fullIDFlag, "full-id", "(debug/admin) Render full card ids in default text output (non-JSON)")
	if err := fs.Parse(args); err != nil {
		return "", false, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	boardID := strings.TrimSpace(boardIDFlag.value)
	if boardID == "" && len(positionals) > 0 {
		boardID = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", false, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx boards cards list`")
	}
	if err := validateID(boardID, "board id"); err != nil {
		return "", false, err
	}
	return boardID, fullIDFlag.set && fullIDFlag.value, nil
}

func validatePlacementFlags(before string, after string, commandName string) error {
	if strings.TrimSpace(before) != "" && strings.TrimSpace(after) != "" {
		return errnorm.Usage("invalid_request", fmt.Sprintf("--before-card-id and --after-card-id cannot be combined for `anx %s`", commandName))
	}
	return nil
}

func hasAnyBoardMutationFieldFlags(values ...any) bool {
	for _, value := range values {
		switch typed := value.(type) {
		case trackedString:
			if strings.TrimSpace(typed.value) != "" {
				return true
			}
		case trackedStrings:
			if len(normalizeStringFilters(typed.values)) > 0 {
				return true
			}
		case trackedBool:
			if typed.set {
				return true
			}
		}
	}
	return false
}

// readBoardCardBodyInput loads JSON from --from-file or stdin. When skipStdin is true and
// from-file is empty, stdin is not read so flag-only invocations stay safe under non-TTY
// stdin (for example Make recipes) without blocking or failing stdin probing.
func (a *App) readBoardCardBodyInput(fromFile string, skipStdin bool) ([]byte, error) {
	fromFile = strings.TrimSpace(fromFile)
	if fromFile != "" {
		return a.readBodyInput(fromFile)
	}
	if skipStdin {
		return nil, nil
	}
	return a.readBodyInput("")
}
