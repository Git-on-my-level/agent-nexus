// `anx boards` and `anx boards cards` dispatch.

package app

import (
	"context"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/runcontext"
)

func (a *App) runBoardsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "boards", boardsSubcommandSpec.requiredError()
	}
	sub := boardsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("boards list")
		var stateFlag, queryFlag, cursorFlag trackedString
		var limitFlag trackedInt
		var ownerFlag trackedStrings
		var includeArchived, archivedOnly, includeTrashed, trashedOnly bool
		fs.Var(&stateFlag, "state", "Filter by lifecycle state (active, archived, trashed)")
		fs.Var(&queryFlag, "q", "Search by board id or title")
		fs.Var(&limitFlag, "limit", "Limit the number of returned boards")
		fs.Var(&cursorFlag, "cursor", "Pagination cursor from a previous list response")
		fs.Var(&ownerFlag, "owner", "Filter by owner actor id (repeatable)")
		fs.BoolVar(&includeArchived, "include-archived", false, "Include archived boards")
		fs.BoolVar(&archivedOnly, "archived-only", false, "Show only archived boards")
		fs.BoolVar(&includeTrashed, "include-trashed", false, "Include trashed boards")
		fs.BoolVar(&trashedOnly, "trashed-only", false, "Show only trashed boards")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "boards list", errnorm.Usage("invalid_flags", err.Error())
		}
		if err := validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "boards list", err
		}
		if len(fs.Args()) > 0 {
			return nil, "boards list", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx boards list`")
		}
		if limitFlag.set && (limitFlag.value < 1 || limitFlag.value > 1000) {
			return nil, "boards list", errnorm.Usage("invalid_request", "limit must be between 1 and 1000")
		}
		query := make([]queryParam, 0, 6)
		if err := appendLifecycleStatesForHTTPList(&query, stateFlag.value, includeArchived, archivedOnly, includeTrashed, trashedOnly); err != nil {
			return nil, "boards list", err
		}
		addSingleQuery(&query, "q", queryFlag.value)
		if limitFlag.set {
			addSingleQuery(&query, "limit", strconv.Itoa(limitFlag.value))
		}
		addSingleQuery(&query, "cursor", cursorFlag.value)
		addMultiQuery(&query, "owner", ownerFlag.values)
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards list", "boards.list", nil, query, nil)
		return result, "boards list", callErr
	case "create":
		body, dryRun, err := a.parseBoardCreateInput(args[1:], cfg, "boards create")
		if err != nil {
			return nil, "boards create", err
		}
		if bodyMap, ok := body.(map[string]any); ok {
			ensureEmptyListDefaults(bodyMap, "board", []string{"document_refs", "pinned_refs"})
		}
		if dryRun {
			return dryRunResult("boards create", "boards.create", nil, nil, body), "boards create", nil
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards create", "boards.create", nil, nil, body)
		return addResourceURLToResult(cfg, "boards.create", result), "boards create", callErr
	case "get":
		id, err := parseResourceIDArg(args[1:], "board-id", "board id", "board")
		if err != nil {
			return nil, "boards get", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards get",
			"boards.get",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			nil,
		)
		return result, "boards get", callErr
	case "patch":
		id, body, dryRun, discoverUpdatedAt, err := a.parseIDAndBodyInputWithMetadata(args[1:], "board-id", "board id", "boards patch", jsonBodyInputOptions{allowDryRun: true})
		if err != nil {
			return nil, "boards patch", err
		}
		if dryRun {
			return dryRunResult("boards patch", "boards.patch", map[string]string{"board_id": id}, nil, body), "boards patch", nil
		}
		if discoverUpdatedAt {
			boardResult, getErr := a.invokeTypedJSONWithIDResolution(ctx, cfg, "boards get", "boards.get", "board_id", id, boardIDLookupSpec, nil, nil)
			if getErr != nil {
				return nil, "boards patch", getErr
			}
			boardBody := extractNestedMap(asMap(boardResult.Data), "body")
			board := extractNestedMap(boardBody, "board")
			updatedAt := strings.TrimSpace(anyString(board["updated_at"]))
			if updatedAt == "" {
				return nil, "boards patch", errnorm.Usage("invalid_request", "boards get response did not include board.updated_at; retry with --if-updated-at <updated_at>")
			}
			bodyMap, ok := body.(map[string]any)
			if !ok {
				return nil, "boards patch", errnorm.Usage("invalid_request", "board patch body must be an object")
			}
			bodyMap["if_updated_at"] = updatedAt
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards patch",
			"boards.patch",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			body,
		)
		return result, "boards patch", callErr
	case "workspace":
		id, err := parseResourceIDArg(args[1:], "board-id", "board id", "board")
		if err != nil {
			return nil, "boards workspace", err
		}
		result, callErr := a.invokeTypedJSONWithIDResolution(
			ctx,
			cfg,
			"boards workspace",
			"boards.workspace",
			"board_id",
			id,
			boardIDLookupSpec,
			nil,
			nil,
		)
		return result, "boards workspace", callErr
	case "archive", "unarchive", "trash", "restore", "purge":
		spec, ok := lifecycleSpecFor("boards")
		if !ok {
			return nil, "boards", errnorm.Usage("internal_error", "missing lifecycle spec for boards")
		}
		return a.runLifecycleVerb(ctx, cfg, spec, sub, args[1:])
	case "cards":
		return a.runBoardCardsCommand(ctx, args[1:], cfg)
	default:
		return nil, "boards", boardsSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runBoardCardsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "boards cards", boardsCardsSubcommandSpec.requiredError()
	}
	sub := boardsCardsSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		boardID, fullID, err := parseBoardCardsListInput(args[1:])
		if err != nil {
			return nil, "boards cards list", err
		}
		result, callErr := a.invokeTypedJSON(
			ctx,
			cfg,
			"boards cards list",
			"boards.cards.list",
			map[string]string{"board_id": boardID},
			nil,
			nil,
		)
		if callErr == nil && fullID {
			data := asMap(result.Data)
			body := asMap(data["body"])
			if body != nil {
				body["full_id"] = true
				result.Text = formatTypedCommandText(
					"boards.cards.list",
					intValue(data["status_code"]),
					headerValues(data["headers"]),
					body,
					cfg.Verbose,
					cfg.Headers,
				)
			}
		}
		return result, "boards cards list", callErr
	case "create-batch":
		boardID, body, topLevel, err := a.parseBoardBatchCardCreateInput(ctx, args[1:], cfg, "boards cards create-batch")
		if err != nil {
			return nil, "boards cards create-batch", err
		}
		ctx = runcontext.WithTopLevel(ctx, topLevel)
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards cards create-batch", "boards.cards.batch_add", map[string]string{"board_id": boardID}, nil, body)
		return result, "boards cards create-batch", callErr
	case "get":
		boardID, cardID, err := a.parseBoardCardBoardScopedTarget(args[1:], "boards cards get")
		if err != nil {
			return nil, "boards cards get", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "boards cards get", "boards.cards.get", map[string]string{"board_id": boardID, "card_id": cardID}, nil, nil)
		return result, "boards cards get", callErr
	default:
		return nil, "boards cards", boardsCardsSubcommandSpec.unknownError(args[0])
	}
}
