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
