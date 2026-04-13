package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

const boardCardsBatchMaxItems = 100

func handleListBoards(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	query := r.URL.Query()
	status := strings.TrimSpace(query.Get("status"))
	if status != "" {
		if err := validateBoardStatus(status); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}

	var limitFilter *int
	limitRaw := strings.TrimSpace(query.Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 1000")
			return
		}
		limitFilter = &parsed
	}

	items, nextCursor, err := opts.primitiveStore.ListBoards(r.Context(), primitives.BoardListFilter{
		Status:          status,
		Labels:          normalizedQueryValues(query["label"]),
		Owners:          normalizedQueryValues(query["owner"]),
		Query:           strings.TrimSpace(query.Get("q")),
		Limit:           limitFilter,
		Cursor:          strings.TrimSpace(query.Get("cursor")),
		IncludeArchived: strings.TrimSpace(query.Get("include_archived")) == "true",
		ArchivedOnly:    strings.TrimSpace(query.Get("archived_only")) == "true",
		IncludeTrashed:  strings.TrimSpace(query.Get("include_trashed")) == "true",
		TrashedOnly:     strings.TrimSpace(query.Get("trashed_only")) == "true",
	})
	if err != nil {
		if errors.Is(err, primitives.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list boards")
		return
	}

	response := map[string]any{
		"boards": boardListItemsResponse(items),
	}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}

func handleCreateBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID    string         `json:"actor_id"`
		RequestKey string         `json:"request_key"`
		Board      map[string]any `json:"board"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Board == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "board is required")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	derivedBoardID := false
	if strings.TrimSpace(req.RequestKey) != "" && firstNonEmptyString(req.Board["id"]) == "" {
		req.Board["id"] = deriveRequestScopedID("boards.create", actorID, req.RequestKey, "bd")
		derivedBoardID = true
	}

	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.create", actorID, req.RequestKey, req)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load idempotency replay")
		return
	}
	if replayed {
		writeJSON(w, replayStatus, replayPayload)
		return
	}

	if err := validateBoardCreateRequest(opts.contract, req.Board); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	boardInput := make(map[string]any, len(req.Board))
	for key, value := range req.Board {
		boardInput[key] = value
	}
	mergeBoardHTTPConvenienceFields(boardInput)

	board, err := opts.primitiveStore.CreateBoard(r.Context(), actorID, boardInput)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" && derivedBoardID {
			boardID := firstNonEmptyString(req.Board["id"])
			existing, loadErr := opts.primitiveStore.GetBoard(r.Context(), boardID)
			if loadErr == nil {
				summary, summaryErr := opts.primitiveStore.GetBoardSummary(r.Context(), boardID)
				if summaryErr == nil {
					response := map[string]any{"board": existing, "summary": summary}
					status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.create", actorID, req.RequestKey, req, http.StatusCreated, response)
					if writeIdempotencyError(w, replayErr) {
						return
					}
					if replayErr != nil {
						writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
						return
					}
					writeJSON(w, status, payload)
					return
				}
				response := map[string]any{"board": existing}
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.create", actorID, req.RequestKey, req, http.StatusCreated, response)
				if writeIdempotencyError(w, replayErr) {
					return
				}
				if replayErr != nil {
					writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
					return
				}
				writeJSON(w, status, payload)
				return
			}
		}
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board already exists")
		default:
			message := "failed to create board"
			if opts.allowUnauthenticatedWrites {
				message = fmt.Sprintf("%s: %v", message, err)
			}
			writeError(w, http.StatusInternalServerError, "internal_error", message)
		}
		return
	}

	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCreatedEvent(board))

	summary, summaryErr := opts.primitiveStore.GetBoardSummary(r.Context(), board["id"].(string))
	response := map[string]any{"board": board}
	if summaryErr == nil {
		response["summary"] = summary
	}
	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.create", actorID, req.RequestKey, req, http.StatusCreated, response)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, payload)
}

func handleGetBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	board, err := opts.primitiveStore.GetBoard(r.Context(), boardID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board")
		return
	}

	summary, summaryErr := opts.primitiveStore.GetBoardSummary(r.Context(), boardID)
	response := map[string]any{"board": board}
	if summaryErr == nil {
		response["summary"] = summary
	}
	writeJSON(w, http.StatusOK, response)
}

func handleUpdateBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID     string         `json:"actor_id"`
		IfUpdatedAt *string        `json:"if_updated_at"`
		Patch       map[string]any `json:"patch"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.IfUpdatedAt == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "if_updated_at is required")
		return
	}
	ifUpdatedAt, ok := normalizeRequiredTimestamp(w, req.IfUpdatedAt, "if_updated_at")
	if !ok {
		return
	}
	if req.Patch == nil || len(req.Patch) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	currentBoard, err := opts.primitiveStore.GetBoard(r.Context(), boardID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board")
		return
	}

	if err := validateBoardPatchRequest(opts.contract, req.Patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	updatedBoard, err := opts.primitiveStore.UpdateBoard(r.Context(), actorID, boardID, req.Patch, &ifUpdatedAt)
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or referenced document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to update board")
		}
		return
	}

	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardUpdatedEvent(currentBoard, updatedBoard, req.Patch))

	summary, summaryErr := opts.primitiveStore.GetBoardSummary(r.Context(), boardID)
	response := map[string]any{"board": updatedBoard}
	if summaryErr == nil {
		response["summary"] = summary
	}
	writeJSON(w, http.StatusOK, response)
}

func writeBoardLifecycleStoreError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "board not found")
		return true
	case errors.Is(err, primitives.ErrNotTrashed):
		writeError(w, http.StatusConflict, "not_trashed", "board is not currently trashed")
		return true
	case errors.Is(err, primitives.ErrNotArchived):
		writeError(w, http.StatusConflict, "not_archived", "board is not archived")
		return true
	case errors.Is(err, primitives.ErrAlreadyTrashed):
		writeError(w, http.StatusConflict, "already_trashed", "board is trashed")
		return true
	case errors.Is(err, primitives.ErrInvalidBoardRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return true
	default:
		return false
	}
}

func handleArchiveBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	board, err := opts.primitiveStore.ArchiveBoard(r.Context(), actorID, boardID)
	if err != nil {
		if writeBoardLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to archive board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": board})
}

func handleUnarchiveBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	board, err := opts.primitiveStore.UnarchiveBoard(r.Context(), actorID, boardID)
	if err != nil {
		if writeBoardLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to unarchive board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": board})
}

func handleTrashBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID string `json:"actor_id"`
		Reason  string `json:"reason"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	board, err := opts.primitiveStore.TrashBoard(r.Context(), actorID, boardID, req.Reason)
	if err != nil {
		if writeBoardLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to trash board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": board})
}

func handleRestoreBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	board, err := opts.primitiveStore.RestoreBoard(r.Context(), actorID, boardID)
	if err != nil {
		if writeBoardLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to restore board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": board})
}

func handlePurgeBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isHumanPrincipal(principal) {
		writeError(w, http.StatusForbidden, "human_only", "only human principals may permanently delete boards")
		return
	}
	if err := opts.primitiveStore.PurgeBoard(r.Context(), boardID); err != nil {
		if writeBoardLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to permanently delete board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"purged": true, "board_id": boardID})
}

func handleGetBoardWorkspace(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	body, err := buildBoardWorkspacePayload(r.Context(), opts, boardID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board workspace")
		return
	}

	writeJSON(w, http.StatusOK, body)
}

func handleListBoardCards(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	cards, err := opts.primitiveStore.ListBoardCards(r.Context(), boardID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list board cards")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"board_id": boardID,
		"cards":    publicCardsView(cards),
	})
}

func handleGetBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, identifier string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	card, err := opts.primitiveStore.GetBoardCard(r.Context(), boardID, identifier)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board card not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board card")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"card": publicCardPayload(card)})
}

func handleAddBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var raw map[string]any
	if !decodeJSONBody(w, r, &raw) {
		return
	}
	addBoardCardFromRaw(w, r, opts, boardID, raw, "boards.cards.add")
}

// addBoardCardFromRaw executes board-scoped card creation. idempotencyOp is the durable scope
// key ("boards.cards.add" for POST /boards/{id}/cards, "cards.create" for POST /cards).
func addBoardCardFromRaw(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string, raw map[string]any, idempotencyOp string) {
	req, ok := parseAddBoardCardJSON(w, raw)
	if !ok {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	replayRequest := raw
	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, idempotencyOp, actorID, req.RequestKey, replayRequest)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load idempotency replay")
		return
	}
	if replayed {
		writeJSON(w, replayStatus, normalizeBoardCardMutationReplayPayload(replayPayload))
		return
	}

	derivedCardID := false
	if strings.TrimSpace(req.RequestKey) != "" && strings.TrimSpace(req.CardID) == "" {
		deriveScope := "boards.cards.create"
		if idempotencyOp == "cards.create" {
			deriveScope = "cards.create"
		}
		req.CardID = deriveRequestScopedID(deriveScope, actorID, req.RequestKey, "cd")
		derivedCardID = true
	}
	explicitReplayCardID := strings.TrimSpace(req.CardID)
	if derivedCardID {
		explicitReplayCardID = ""
	}

	createStatus := "todo"
	if strings.TrimSpace(req.ColumnKey) == "done" {
		createStatus = "done"
	}

	result, err := opts.primitiveStore.CreateBoardCard(r.Context(), actorID, boardID, addBoardCardStoreInput(req, createStatus))
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			existingCard, loadCardErr := loadExistingBoardCardForCreateReplay(r.Context(), opts, boardID, req.CardID, req.ParentThread, req.ThreadID)
			existingBoard, loadBoardErr := opts.primitiveStore.GetBoard(r.Context(), boardID)
			if loadCardErr == nil && loadBoardErr == nil && boardCardReplayPreconditionMatches(existingBoard, req.IfBoardUpdatedAt) && boardCardMatchesCreateReplay(
				existingCard,
				explicitReplayCardID,
				req.Title,
				req.Body,
				req.ParentThread,
				req.ThreadID,
				req.ColumnKey,
				createStatus,
				req.Assignee,
				req.PinnedDocumentID,
				req.DueAt,
				req.Resolution,
				req.DefinitionOfDone,
				req.ResolutionRefs,
				req.Refs,
				req.Risk,
			) {
				response := map[string]any{"board": existingBoard, "card": existingCard}
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, idempotencyOp, actorID, req.RequestKey, replayRequest, http.StatusCreated, response)
				if writeIdempotencyError(w, replayErr) {
					return
				}
				if replayErr == nil {
					writeJSON(w, status, normalizeBoardCardMutationReplayPayload(payload))
					return
				}
			}
		}
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board, thread, or document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board membership already exists or board has changed; refresh and retry")
		default:
			message := "failed to add board card"
			if opts.allowUnauthenticatedWrites {
				message = fmt.Sprintf("%s: %v", message, err)
			}
			writeError(w, http.StatusInternalServerError, "internal_error", message)
		}
		return
	}

	emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardCreatedEvent(result.Board, result.Card))
	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardAddedEvent(result.Board, result.Card))

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, idempotencyOp, actorID, req.RequestKey, replayRequest, http.StatusCreated, map[string]any{
		"board": result.Board,
		"card":  result.Card,
	})
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, normalizeBoardCardMutationReplayPayload(payload))
}

func handleBatchAddBoardCards(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var raw map[string]any
	if !decodeJSONBody(w, r, &raw) {
		return
	}
	rawItems, has := raw["items"]
	if !has {
		writeError(w, http.StatusBadRequest, "invalid_request", "items is required")
		return
	}
	list, ok := rawItems.([]any)
	if !ok || len(list) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "items must be a non-empty array")
		return
	}
	if len(list) > boardCardsBatchMaxItems {
		writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("items exceeds maximum batch size of %d", boardCardsBatchMaxItems))
		return
	}

	actorIDField := strings.TrimSpace(anyString(raw["actor_id"]))
	batchRequestKey := strings.TrimSpace(anyString(raw["request_key"]))
	var ifBoardTok *string
	if rawIf, ok := raw["if_board_updated_at"]; ok && rawIf != nil {
		s := strings.TrimSpace(anyString(rawIf))
		if s != "" {
			if _, err := time.Parse(time.RFC3339, s); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "if_board_updated_at must be an RFC3339 datetime string")
				return
			}
			ifBoardTok = &s
		}
	}

	actorID, ok := resolveWriteActorID(w, r, opts, actorIDField)
	if !ok {
		return
	}

	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.cards.batch_add", actorID, batchRequestKey, raw)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load idempotency replay")
		return
	}
	if replayed {
		writeJSON(w, replayStatus, normalizeBatchBoardCardCreatePayload(replayPayload))
		return
	}

	inputs := make([]primitives.AddBoardCardInput, 0, len(list))
	for i, elem := range list {
		itemMap, ok := elem.(map[string]any)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("items[%d] must be an object", i))
			return
		}
		m, err := mergeAddBoardCardRaw(itemMap)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("items[%d]: %s", i, err.Error()))
			return
		}
		if strings.TrimSpace(batchRequestKey) != "" && strings.TrimSpace(m.CardID) == "" {
			m.CardID = deriveRequestScopedID("boards.cards.batch_create", actorID, fmt.Sprintf("%s#%d", batchRequestKey, i), "cd")
		}
		createStatus := "todo"
		if strings.TrimSpace(m.ColumnKey) == "done" {
			createStatus = "done"
		}
		in := addBoardCardStoreInput(m, createStatus)
		in.IfBoardUpdatedAt = nil
		inputs = append(inputs, in)
	}

	results, err := opts.primitiveStore.CreateBoardCardsBatch(r.Context(), actorID, boardID, ifBoardTok, inputs)
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board, thread, or document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board membership already exists or board has changed; refresh and retry")
		default:
			message := "failed to create board cards batch"
			if opts.allowUnauthenticatedWrites {
				message = fmt.Sprintf("%s: %v", message, err)
			}
			writeError(w, http.StatusInternalServerError, "internal_error", message)
		}
		return
	}

	for _, res := range results {
		emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardCreatedEvent(res.Board, res.Card))
		emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardAddedEvent(res.Board, res.Card))
	}

	finalBoard := results[len(results)-1].Board
	cardPayloads := make([]map[string]any, len(results))
	for i := range results {
		cardPayloads[i] = results[i].Card
	}
	response := map[string]any{
		"board": finalBoard,
		"cards": cardPayloads,
	}
	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.cards.batch_add", actorID, batchRequestKey, raw, http.StatusCreated, response)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, normalizeBatchBoardCardCreatePayload(payload))
}

func handleUpdateBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, cardKey string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID          string         `json:"actor_id"`
		IfBoardUpdatedAt *string        `json:"if_board_updated_at"`
		Patch            map[string]any `json:"patch"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Patch == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
		return
	}
	if req.IfBoardUpdatedAt == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "if_board_updated_at is required")
		return
	}
	ifBoardUpdatedAt, ok := normalizeRequiredTimestamp(w, req.IfBoardUpdatedAt, "if_board_updated_at")
	if !ok {
		return
	}
	patchInput, changedFields, ok := parseBoardCardPatchInput(w, req.Patch)
	if !ok {
		return
	}
	patchInput.IfBoardUpdatedAt = &ifBoardUpdatedAt

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	beforeCard, err := loadBoardCardForEvent(r.Context(), opts, boardID, cardKey)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board card")
		return
	}
	if len(changedFields) == 0 {
		currentBoard, err := opts.primitiveStore.GetBoard(r.Context(), anyString(beforeCard["board_id"]))
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "board or card not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board")
			return
		}
		if !boardCardReplayPreconditionMatches(currentBoard, patchInput.IfBoardUpdatedAt) {
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"board": currentBoard, "card": publicCardView(beforeCard)})
		return
	}

	result, err := opts.primitiveStore.UpdateBoardCard(r.Context(), actorID, boardID, cardKey, patchInput)
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board, card, or document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to update board card")
		}
		return
	}

	if anyString(result.Card["updated_at"]) != anyString(beforeCard["updated_at"]) || anyString(result.Card["version"]) != anyString(beforeCard["version"]) {
		emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardUpdatedEvent(result.Board, beforeCard, result.Card, changedFields))
	}

	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "card": publicCardView(result.Card)})
}

func handleMoveBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, cardKey string) {
	handleMoveCardMutation(w, r, opts, boardID, cardKey, "board or card not found")
}

func handleMoveCardMutation(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, cardKey, notFoundMessage string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID          string   `json:"actor_id"`
		IfBoardUpdatedAt *string  `json:"if_board_updated_at"`
		ColumnKey        string   `json:"column_key"`
		BeforeCardID     string   `json:"before_card_id"`
		AfterCardID      string   `json:"after_card_id"`
		BeforeThreadID   string   `json:"before_thread_id"`
		AfterThreadID    string   `json:"after_thread_id"`
		Resolution       *string  `json:"resolution"`
		ResolutionRefs   []string `json:"resolution_refs"`
	}
	if !decodeMoveCardHTTPPayload(w, r, &req) {
		return
	}
	if req.IfBoardUpdatedAt == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "if_board_updated_at is required")
		return
	}
	ifBoardUpdatedAt, ok := normalizeRequiredTimestamp(w, req.IfBoardUpdatedAt, "if_board_updated_at")
	if !ok {
		return
	}
	if strings.TrimSpace(req.ColumnKey) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "column_key is required")
		return
	}
	if err := validateBoardCardMoveRequest(req.ColumnKey, req.BeforeCardID, req.AfterCardID, req.BeforeThreadID, req.AfterThreadID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Resolution != nil {
		normalizedResolution := strings.TrimSpace(*req.Resolution)
		if normalizedResolution == "completed" || normalizedResolution == "superseded" {
			normalizedResolution = "done"
		}
		if normalizedResolution == "unresolved" {
			normalizedResolution = ""
		}
		if err := validateCardResolution(normalizedResolution, false); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		req.Resolution = &normalizedResolution
	}
	req.ResolutionRefs = uniqueSortedStrings(req.ResolutionRefs)
	if req.Resolution == nil && len(req.ResolutionRefs) > 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "resolution_refs require resolution")
		return
	}
	if req.Resolution != nil && strings.TrimSpace(req.ColumnKey) != "done" {
		writeError(w, http.StatusBadRequest, "invalid_request", "resolution requires column_key done")
		return
	}
	if req.Resolution != nil {
		if len(req.ResolutionRefs) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "resolution_refs are required when resolution is set")
			return
		}
		if err := schema.ValidateTypedRefs(opts.contract, req.ResolutionRefs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if err := validateMoveCardResolutionRefs(*req.Resolution, req.ResolutionRefs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	beforeCard, err := loadBoardCardForEvent(r.Context(), opts, boardID, cardKey)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", notFoundMessage)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board card")
		return
	}

	result, err := opts.primitiveStore.MoveBoardCard(r.Context(), actorID, boardID, cardKey, primitives.MoveBoardCardInput{
		ColumnKey:        strings.TrimSpace(req.ColumnKey),
		BeforeCardID:     strings.TrimSpace(req.BeforeCardID),
		AfterCardID:      strings.TrimSpace(req.AfterCardID),
		Resolution:       req.Resolution,
		ResolutionRefs:   &req.ResolutionRefs,
		IfBoardUpdatedAt: &ifBoardUpdatedAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to move board card")
		}
		return
	}

	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardMovedEvent(result.Board, beforeCard, result.Card, req.BeforeCardID, req.AfterCardID, req.BeforeThreadID, req.AfterThreadID))
	emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardMovedEvent(result.Board, beforeCard, result.Card, req.BeforeCardID, req.AfterCardID, req.BeforeThreadID, req.AfterThreadID))
	if anyString(result.Card["updated_at"]) != anyString(beforeCard["updated_at"]) || anyString(result.Card["version"]) != anyString(beforeCard["version"]) {
		emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardUpdatedEvent(result.Board, beforeCard, result.Card, []string{"resolution", "resolution_refs"}))
	}

	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "card": publicCardView(result.Card)})
}

func handleRemoveBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, identifier string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID          string  `json:"actor_id"`
		IfBoardUpdatedAt *string `json:"if_board_updated_at"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.IfBoardUpdatedAt == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "if_board_updated_at is required")
		return
	}
	ifBoardUpdatedAt, ok := normalizeRequiredTimestamp(w, req.IfBoardUpdatedAt, "if_board_updated_at")
	if !ok {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	result, err := opts.primitiveStore.RemoveBoardCard(r.Context(), actorID, boardID, identifier, primitives.RemoveBoardCardInput{
		IfBoardUpdatedAt: &ifBoardUpdatedAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to remove board card")
		}
		return
	}

	emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardArchivedEvent(result.Board, result.Card))
	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardArchivedEvent(result.Board, result.Card))

	writeJSON(w, http.StatusOK, map[string]any{
		"board":             result.Board,
		"card":              publicCardView(result.Card),
		"removed_thread_id": result.RemovedThreadID,
	})
}

func handleArchiveBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, identifier string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID          string  `json:"actor_id"`
		IfBoardUpdatedAt *string `json:"if_board_updated_at"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.IfBoardUpdatedAt != nil {
		normalized, ok := normalizeRequiredTimestamp(w, req.IfBoardUpdatedAt, "if_board_updated_at")
		if !ok {
			return
		}
		req.IfBoardUpdatedAt = &normalized
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	result, err := opts.primitiveStore.ArchiveBoardCard(r.Context(), actorID, boardID, identifier, primitives.RemoveBoardCardInput{
		IfBoardUpdatedAt: req.IfBoardUpdatedAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
		case errors.Is(err, primitives.ErrAlreadyTrashed):
			writeError(w, http.StatusConflict, "already_trashed", "card is trashed")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to archive board card")
		}
		return
	}
	emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardArchivedEvent(result.Board, result.Card))
	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardArchivedEvent(result.Board, result.Card))
	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "card": publicCardView(result.Card)})
}

func handleTrashBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, identifier string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID          string  `json:"actor_id"`
		Reason           string  `json:"reason"`
		IfBoardUpdatedAt *string `json:"if_board_updated_at"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.IfBoardUpdatedAt != nil {
		normalized, ok := normalizeRequiredTimestamp(w, req.IfBoardUpdatedAt, "if_board_updated_at")
		if !ok {
			return
		}
		req.IfBoardUpdatedAt = &normalized
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "reason is required")
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	result, err := opts.primitiveStore.TrashBoardCard(r.Context(), actorID, boardID, identifier, req.Reason, primitives.RemoveBoardCardInput{
		IfBoardUpdatedAt: req.IfBoardUpdatedAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to trash board card")
		}
		return
	}
	if result.Card == nil || result.Card["_mutation_applied"] != false {
		emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardTrashedEvent(result.Board, result.Card))
		emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardTrashedEvent(result.Board, result.Card))
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "card": publicCardView(result.Card)})
}

func buildBoardWorkspacePayload(ctx context.Context, opts handlerOptions, boardID string) (map[string]any, error) {
	board, err := opts.primitiveStore.GetBoard(ctx, boardID)
	if err != nil {
		return nil, err
	}

	warnings := make([]map[string]any, 0)

	backingThreadID := strings.TrimSpace(anyString(board["thread_id"]))
	if backingThreadID == "" {
		backingThreadID = boardID
	}

	if backingThreadID != "" {
		_, threadErr := opts.primitiveStore.GetThread(ctx, backingThreadID)
		if threadErr != nil {
			if errors.Is(threadErr, primitives.ErrNotFound) {
				warnings = append(warnings, map[string]any{
					"thread_id": backingThreadID,
					"message":   "board backing thread is no longer available",
				})
			} else {
				return nil, threadErr
			}
		}
	}

	if docRefs := boardDocumentRefs(board); len(docRefs) > 0 {
		_, docID, refErr := schema.SplitTypedRef(docRefs[0])
		if refErr == nil && docID != "" {
			_, _, docErr := opts.primitiveStore.GetDocument(ctx, docID)
			if docErr != nil {
				if errors.Is(docErr, primitives.ErrNotFound) {
					warnings = append(warnings, map[string]any{
						"document_id": docID,
						"message":     "board primary document is no longer available",
					})
				} else {
					return nil, docErr
				}
			}
		}
	}

	var primaryTopic any
	if topicRef := strings.TrimSpace(anyString(board["primary_topic_ref"])); topicRef != "" {
		_, topicID, refErr := schema.SplitTypedRef(topicRef)
		if refErr == nil && topicID != "" {
			topic, topicErr := opts.primitiveStore.GetTopic(ctx, topicID)
			if topicErr != nil {
				if errors.Is(topicErr, primitives.ErrNotFound) {
					warnings = append(warnings, map[string]any{
						"topic_id": topicID,
						"message":  "board primary topic is no longer available",
					})
				} else {
					return nil, topicErr
				}
			} else {
				primaryTopic = topic
			}
		}
	}

	cards, err := opts.primitiveStore.ListBoardCards(ctx, boardID)
	if err != nil {
		return nil, err
	}

	threadIDs := collectBoardWorkspaceThreadIDs(backingThreadID, board, cards)
	now := time.Now().UTC()
	states, err := loadTopicProjectionStates(ctx, opts, threadIDs)
	if err != nil {
		return nil, err
	}

	cardSection, cardWarnings, err := buildBoardWorkspaceCardsSection(ctx, opts, board, cards, states)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, cardWarnings...)

	documentsSection, err := buildBoardWorkspaceDocumentsSection(ctx, opts, board, threadIDs)
	if err != nil {
		return nil, err
	}
	inboxSection, err := buildBoardWorkspaceInboxSection(ctx, opts, threadIDs, now, states)
	if err != nil {
		return nil, err
	}

	boardSummary := buildBoardWorkspaceSummary(board, cards, states, documentsSection, now)
	freshness := aggregateTopicProjectionFreshness(states, threadIDs)
	return map[string]any{
		"board_id":                boardID,
		"board":                   board,
		"primary_topic":           primaryTopic,
		"cards":                   cardSection,
		"documents":               documentsSection,
		"inbox":                   inboxSection,
		"board_summary":           boardSummary,
		"projection_freshness":    freshness,
		"board_summary_freshness": cloneWorkspaceMap(freshness),
		"warnings":                map[string]any{"items": warnings, "count": len(warnings)},
		"section_kinds":           map[string]any{"board": "canonical", "primary_topic": "canonical", "cards": "convenience", "documents": "derived", "inbox": "derived", "board_summary": "derived"},
		"generated_at":            now.Format(time.RFC3339Nano),
	}, nil
}

func buildBoardWorkspaceCardsSection(ctx context.Context, opts handlerOptions, board map[string]any, cards []map[string]any, states map[string]topicProjectionState) (map[string]any, []map[string]any, error) {
	items := make([]map[string]any, 0, len(cards))
	warnings := make([]map[string]any, 0)
	for _, card := range cards {
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		var (
			thread any
			err    error
		)
		if threadID != "" {
			thread, err = opts.primitiveStore.GetThread(ctx, threadID)
			if err != nil {
				if errors.Is(err, primitives.ErrNotFound) {
					warnings = append(warnings, map[string]any{"thread_id": threadID, "message": "board card backing thread was not found"})
					thread = nil
				} else {
					return nil, nil, err
				}
			}
		}

		pinnedDocument, err := loadBoardCardPinnedDocument(ctx, opts, card)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				warnings = append(warnings, map[string]any{"thread_id": threadID, "message": "card pinned document is no longer available"})
				pinnedDocument = nil
			} else {
				return nil, nil, err
			}
		}

		summary := boardCardDerivedSummary(card, threadID, states, time.Now().UTC())
		freshness := boardCardDerivedFreshness(threadID, states)
		pubCard := publicCardView(card)
		items = append(items, map[string]any{
			"board_ref":            "board:" + anyString(board["id"]),
			"card":                 pubCard,
			"summary":              summary,
			"projection_freshness": freshness,
			"membership":           pubCard,
			"backing": map[string]any{
				"thread_id":           nullableStringValue(threadID),
				"thread":              thread,
				"pinned_document_ref": nullableTypedRef("document", pinnedDocumentIDFromCard(card)),
				"pinned_document":     pinnedDocument,
			},
			"derived": map[string]any{
				"summary":   summary,
				"freshness": freshness,
			},
		})
	}

	return map[string]any{"items": items, "count": len(items)}, warnings, nil
}

func buildBoardWorkspaceDocumentsSection(ctx context.Context, opts handlerOptions, board map[string]any, threadIDs []string) (map[string]any, error) {
	seen := map[string]map[string]any{}
	for _, documentRef := range boardDocumentRefs(board) {
		documentID := strings.TrimPrefix(documentRef, "document:")
		if documentID == "" {
			continue
		}
		document, _, err := opts.primitiveStore.GetDocument(ctx, documentID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			return nil, err
		}
		seen[documentID] = document
	}
	for _, threadID := range threadIDs {
		documents, err := buildThreadContextDocuments(ctx, opts, threadID)
		if err != nil {
			return nil, err
		}
		for _, document := range documents {
			documentID := strings.TrimSpace(anyString(document["id"]))
			if documentID == "" {
				continue
			}
			seen[documentID] = document
		}
	}

	items := mapValues(seen)
	sort.SliceStable(items, func(i int, j int) bool {
		leftUpdated := strings.TrimSpace(anyString(items[i]["updated_at"]))
		rightUpdated := strings.TrimSpace(anyString(items[j]["updated_at"]))
		if leftUpdated != rightUpdated {
			return leftUpdated > rightUpdated
		}
		return strings.TrimSpace(anyString(items[i]["id"])) < strings.TrimSpace(anyString(items[j]["id"]))
	})
	return map[string]any{"items": items, "count": len(items)}, nil
}

func buildBoardWorkspaceInboxSection(ctx context.Context, opts handlerOptions, threadIDs []string, now time.Time, states map[string]topicProjectionState) (map[string]any, error) {
	items := make([]map[string]any, 0)
	for _, threadID := range threadIDs {
		threadItems, err := opts.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
		if err != nil {
			return nil, err
		}
		for _, item := range threadItems {
			copy := payloadFromDerivedInboxItem(item)
			if strings.TrimSpace(anyString(copy["thread_id"])) == "" {
				copy["thread_id"] = threadID
			}
			items = append(items, copy)
		}
	}

	sort.SliceStable(items, func(i int, j int) bool {
		leftTrigger := strings.TrimSpace(anyString(items[i]["trigger_at"]))
		rightTrigger := strings.TrimSpace(anyString(items[j]["trigger_at"]))
		if leftTrigger != rightTrigger {
			return leftTrigger > rightTrigger
		}
		return strings.TrimSpace(anyString(items[i]["id"])) < strings.TrimSpace(anyString(items[j]["id"]))
	})
	return map[string]any{
		"items":                items,
		"count":                len(items),
		"generated_at":         now.Format(time.RFC3339Nano),
		"projection_freshness": aggregateTopicProjectionFreshness(states, threadIDs),
	}, nil
}

func buildBoardWorkspaceSummary(board map[string]any, cards []map[string]any, states map[string]topicProjectionState, documentsSection map[string]any, now time.Time) map[string]any {
	cardsByColumn := map[string]any{
		"backlog":     0,
		"ready":       0,
		"in_progress": 0,
		"blocked":     0,
		"review":      0,
		"done":        0,
	}

	threadIDs := map[string]struct{}{}
	backingThreadID := strings.TrimSpace(anyString(board["thread_id"]))
	if backingThreadID != "" {
		threadIDs[backingThreadID] = struct{}{}
	}
	for _, threadRef := range boardThreadRefs(board) {
		_, threadID, err := schema.SplitTypedRef(threadRef)
		if err != nil || threadID == "" {
			continue
		}
		threadIDs[threadID] = struct{}{}
	}
	for _, card := range cards {
		columnKey := strings.TrimSpace(anyString(card["column_key"]))
		if _, ok := cardsByColumn[columnKey]; ok {
			cardsByColumn[columnKey] = workspaceIntValue(cardsByColumn[columnKey]) + 1
		}
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		if threadID != "" {
			threadIDs[threadID] = struct{}{}
		}
	}

	documentCount := workspaceIntValue(documentsSection["count"])
	latestActivityAt := strings.TrimSpace(anyString(board["updated_at"]))
	unresolvedCardCount := 0
	resolvedCardCount := 0
	atRiskCardCount := 0
	dueSoonCardCount := 0
	overdueCardCount := 0
	blockedCardCount := 0
	staleCardCount := 0
	for threadID := range threadIDs {
		projection := states[threadID].Projection
		latestActivityAt = laterTimestamp(latestActivityAt, projection.LastActivityAt)
	}
	for _, card := range cards {
		if !boardCardCountsAsOpenWorkItem(card) {
			resolvedCardCount++
		} else {
			unresolvedCardCount++
		}
		riskState, _, _ := boardCardRiskState(card, now, defaultInboxRiskHorizon)
		switch riskState {
		case "overdue":
			atRiskCardCount++
			overdueCardCount++
		case "due_soon":
			atRiskCardCount++
			dueSoonCardCount++
		case "blocked":
			atRiskCardCount++
			blockedCardCount++
		}
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		if state, ok := states[threadID]; ok && state.Projection.Stale && boardCardCountsAsOpenWorkItem(card) {
			staleCardCount++
		}
		if updatedAt := strings.TrimSpace(anyString(card["updated_at"])); updatedAt != "" {
			latestActivityAt = laterTimestamp(latestActivityAt, updatedAt)
		}
	}

	return map[string]any{
		"card_count":            len(cards),
		"cards_by_column":       cardsByColumn,
		"unresolved_card_count": unresolvedCardCount,
		"resolved_card_count":   resolvedCardCount,
		"at_risk_card_count":    atRiskCardCount,
		"due_soon_card_count":   dueSoonCardCount,
		"overdue_card_count":    overdueCardCount,
		"blocked_card_count":    blockedCardCount,
		"stale_card_count":      staleCardCount,
		"document_count":        documentCount,
		"latest_activity_at":    nullableStringValue(latestActivityAt),
		"has_document_refs":     len(boardDocumentRefs(board)) > 0,
		"thread_id":             nullableStringValue(backingThreadID),
		"document_refs":         boardDocumentRefs(board),
	}
}

func buildBoardCreatedEvent(board map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":      anyString(board["id"]),
		"board_ref":     "board:" + anyString(board["id"]),
		"thread_id":     nullableStringValue(anyString(board["thread_id"])),
		"document_refs": boardDocumentRefs(board),
		"refs":          boardRefs(board),
	}
	return buildBoardLifecycleEvent("board_created", board, nil, payload, "Board created: "+boardDisplayName(board))
}

func buildBoardUpdatedEvent(previousBoard, updatedBoard, patch map[string]any) map[string]any {
	changedFields := sortedMapKeys(patch)
	payload := map[string]any{
		"board_id":        anyString(updatedBoard["id"]),
		"changed_fields":  changedFields,
		"previous_status": nullableStringValue(anyString(previousBoard["status"])),
		"status":          nullableStringValue(anyString(updatedBoard["status"])),
	}
	return buildBoardLifecycleEvent("board_updated", updatedBoard, nil, payload, "Board updated: "+boardDisplayName(updatedBoard))
}

func buildBoardCardAddedEvent(board, card map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":      anyString(board["id"]),
		"card_id":       anyString(card["id"]),
		"thread_id":     nullableStringValue(anyString(card["thread_id"])),
		"column_key":    anyString(card["column_key"]),
		"status":        nullableStringValue(anyString(card["status"])),
		"assignee_refs": cardAssigneeRefsAny(card),
		"document_ref":  nullableStringValue(documentRefForPublicCard(card)),
		"related_refs":  typedRefStringsToAnyList(cardRelatedRefs(card)),
	}
	return buildBoardLifecycleEvent("board_card_added", board, card, payload, "Board card added: "+cardDisplayName(card))
}

func buildCardCreatedEvent(board, card map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":      anyString(board["id"]),
		"card_id":       anyString(card["id"]),
		"thread_id":     nullableStringValue(anyString(card["thread_id"])),
		"column_key":    anyString(card["column_key"]),
		"status":        nullableStringValue(anyString(card["status"])),
		"assignee_refs": cardAssigneeRefsAny(card),
		"document_ref":  nullableStringValue(documentRefForPublicCard(card)),
		"related_refs":  typedRefStringsToAnyList(cardRelatedRefs(card)),
	}
	return buildCardLifecycleEvent("card_created", board, card, payload, "Card created: "+cardDisplayName(card))
}

func buildCardUpdatedEvent(board, previousCard, updatedCard map[string]any, changedFields []string) map[string]any {
	payload := map[string]any{
		"board_id":       anyString(board["id"]),
		"card_id":        anyString(updatedCard["id"]),
		"thread_id":      nullableStringValue(anyString(updatedCard["thread_id"])),
		"related_refs":   typedRefStringsToAnyList(cardRelatedRefs(updatedCard)),
		"document_ref":   nullableStringValue(documentRefForPublicCard(updatedCard)),
		"changed_fields": changedFields,
	}
	for _, field := range changedFields {
		switch field {
		case "assignee_refs":
			payload["previous_assignee_refs"] = cardAssigneeRefsAny(previousCard)
			payload["assignee_refs"] = cardAssigneeRefsAny(updatedCard)
		case "related_refs":
			payload["previous_related_refs"] = mergeRelatedRefsForPublicView(previousCard)
			payload["related_refs"] = mergeRelatedRefsForPublicView(updatedCard)
		default:
			payload["previous_"+field] = nullableStringValue(anyString(previousCard[field]))
			payload[field] = nullableStringValue(anyString(updatedCard[field]))
		}
	}
	return buildCardLifecycleEvent("card_updated", board, updatedCard, payload, "Card updated: "+cardDisplayName(updatedCard))
}

func buildCardArchivedEvent(board, card map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":     anyString(board["id"]),
		"card_id":      anyString(card["id"]),
		"thread_id":    nullableStringValue(anyString(card["thread_id"])),
		"column_key":   nullableStringValue(anyString(card["column_key"])),
		"document_ref": nullableStringValue(documentRefForPublicCard(card)),
		"related_refs": typedRefStringsToAnyList(cardRelatedRefs(card)),
	}
	return buildCardLifecycleEvent("card_archived", board, card, payload, "Card archived: "+cardDisplayName(card))
}

func buildCardTrashedEvent(board, card map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":     anyString(board["id"]),
		"card_id":      anyString(card["id"]),
		"thread_id":    nullableStringValue(anyString(card["thread_id"])),
		"column_key":   nullableStringValue(anyString(card["column_key"])),
		"document_ref": nullableStringValue(documentRefForPublicCard(card)),
		"related_refs": typedRefStringsToAnyList(cardRelatedRefs(card)),
		"trash_reason": nullableStringValue(anyString(card["trash_reason"])),
	}
	return buildCardLifecycleEvent("card_trashed", board, card, payload, "Card trashed: "+cardDisplayName(card))
}

func buildBoardCardMovedEvent(board, previousCard, updatedCard map[string]any, beforeCardID, afterCardID, beforeThreadID, afterThreadID string) map[string]any {
	payload := map[string]any{
		"board_id":         anyString(board["id"]),
		"card_id":          anyString(updatedCard["id"]),
		"from_column_key":  nullableStringValue(anyString(previousCard["column_key"])),
		"column_key":       nullableStringValue(anyString(updatedCard["column_key"])),
		"before_card_id":   nullableStringValue(strings.TrimSpace(beforeCardID)),
		"after_card_id":    nullableStringValue(strings.TrimSpace(afterCardID)),
		"before_thread_id": nullableStringValue(strings.TrimSpace(beforeThreadID)),
		"after_thread_id":  nullableStringValue(strings.TrimSpace(afterThreadID)),
	}
	return buildBoardLifecycleEvent("board_card_moved", board, updatedCard, payload, "Board card moved: "+cardDisplayName(updatedCard))
}

func buildCardMovedEvent(board, previousCard, updatedCard map[string]any, beforeCardID, afterCardID, beforeThreadID, afterThreadID string) map[string]any {
	payload := map[string]any{
		"board_id":         anyString(board["id"]),
		"card_id":          anyString(updatedCard["id"]),
		"from_column_key":  nullableStringValue(anyString(previousCard["column_key"])),
		"column_key":       nullableStringValue(anyString(updatedCard["column_key"])),
		"before_card_id":   nullableStringValue(strings.TrimSpace(beforeCardID)),
		"after_card_id":    nullableStringValue(strings.TrimSpace(afterCardID)),
		"before_thread_id": nullableStringValue(strings.TrimSpace(beforeThreadID)),
		"after_thread_id":  nullableStringValue(strings.TrimSpace(afterThreadID)),
	}
	return buildCardLifecycleEvent("card_moved", board, updatedCard, payload, "Card moved: "+cardDisplayName(updatedCard))
}

func buildBoardCardArchivedEvent(board, card map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":     anyString(board["id"]),
		"card_id":      anyString(card["id"]),
		"column_key":   nullableStringValue(anyString(card["column_key"])),
		"document_ref": nullableStringValue(documentRefForPublicCard(card)),
		"related_refs": typedRefStringsToAnyList(cardRelatedRefs(card)),
	}
	return buildBoardLifecycleEvent("board_card_archived", board, card, payload, "Board card archived: "+cardDisplayName(card))
}

func buildBoardCardTrashedEvent(board, card map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":     anyString(board["id"]),
		"card_id":      anyString(card["id"]),
		"column_key":   nullableStringValue(anyString(card["column_key"])),
		"document_ref": nullableStringValue(documentRefForPublicCard(card)),
		"related_refs": typedRefStringsToAnyList(cardRelatedRefs(card)),
		"trash_reason": nullableStringValue(anyString(card["trash_reason"])),
	}
	return buildBoardLifecycleEvent("board_card_trashed", board, card, payload, "Board card trashed: "+cardDisplayName(card))
}

func buildCardLifecycleEvent(eventType string, board, card map[string]any, payload map[string]any, summary string) map[string]any {
	refs := append([]string{"board:" + anyString(board["id"])}, boardRefs(board)...)
	if card != nil {
		if cardID := strings.TrimSpace(anyString(card["id"])); cardID != "" {
			refs = append(refs, "card:"+cardID)
		}
		if threadID := strings.TrimSpace(anyString(card["thread_id"])); threadID != "" {
			refs = append(refs, "thread:"+threadID)
		}
		if parentThreadID := primaryRelatedThreadID(card); parentThreadID != "" {
			refs = append(refs, "thread:"+parentThreadID)
		}
		if documentRef := documentRefForPublicCard(card); documentRef != "" {
			refs = append(refs, documentRef)
		}
	}
	refs = uniqueSortedStrings(refs)

	threadID := strings.TrimSpace(anyString(card["thread_id"]))
	event := map[string]any{
		"type":       eventType,
		"thread_id":  threadID,
		"refs":       refs,
		"summary":    strings.TrimSpace(summary),
		"payload":    payload,
		"provenance": actorStatementProvenance(),
	}
	return event
}

func buildBoardLifecycleEvent(eventType string, board, card map[string]any, payload map[string]any, summary string) map[string]any {
	refs := append([]string{"board:" + anyString(board["id"])}, boardRefs(board)...)
	backingThreadID := strings.TrimSpace(anyString(board["thread_id"]))
	if backingThreadID != "" {
		refs = append(refs, "thread:"+backingThreadID)
	}
	if card != nil {
		if cardID := strings.TrimSpace(anyString(card["id"])); cardID != "" {
			refs = append(refs, "card:"+cardID)
		}
		if threadID := strings.TrimSpace(anyString(card["thread_id"])); threadID != "" {
			refs = append(refs, "thread:"+threadID)
		}
		if documentRef := documentRefForPublicCard(card); documentRef != "" {
			refs = append(refs, documentRef)
		}
	}
	refs = uniqueSortedStrings(refs)

	event := map[string]any{
		"type":       eventType,
		"thread_id":  backingThreadID,
		"refs":       refs,
		"summary":    strings.TrimSpace(summary),
		"payload":    payload,
		"provenance": actorStatementProvenance(),
	}
	return event
}

func emitBoardLifecycleEvent(ctx context.Context, opts handlerOptions, actorID string, event map[string]any) error {
	if opts.primitiveStore == nil || event == nil {
		return nil
	}
	stored, err := opts.primitiveStore.AppendEvent(ctx, actorID, event)
	if err != nil {
		return err
	}
	enqueueTopicProjectionsBestEffort(ctx, opts, []string{anyString(stored["thread_id"])}, time.Now().UTC())
	return nil
}

func emitBoardLifecycleEventBestEffort(ctx context.Context, opts handlerOptions, actorID string, event map[string]any) {
	_ = emitBoardLifecycleEvent(ctx, opts, actorID, event)
}

func emitCardLifecycleEvent(ctx context.Context, opts handlerOptions, actorID string, event map[string]any) error {
	if opts.primitiveStore == nil || event == nil {
		return nil
	}
	stored, err := opts.primitiveStore.AppendEvent(ctx, actorID, event)
	if err != nil {
		return err
	}
	threadIDs := []string{anyString(stored["thread_id"])}
	payload, _ := stored["payload"].(map[string]any)
	if refs, err := extractStringSlice(payload["related_refs"]); err == nil {
		for _, ref := range refs {
			prefix, id, err := schema.SplitTypedRef(ref)
			if err != nil || prefix != "thread" {
				continue
			}
			threadIDs = append(threadIDs, id)
		}
	}
	enqueueTopicProjectionsBestEffort(ctx, opts, threadIDs, time.Now().UTC())
	for _, threadID := range uniqueServerStrings(threadIDs) {
		_ = refreshDerivedTopicProjection(ctx, opts, threadID, time.Now().UTC(), actorID)
	}
	return nil
}

func emitCardLifecycleEventBestEffort(ctx context.Context, opts handlerOptions, actorID string, event map[string]any) {
	_ = emitCardLifecycleEvent(ctx, opts, actorID, event)
}

func boardListItemsResponse(items []primitives.BoardListItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"board":   item.Board,
			"summary": item.Summary,
		})
	}
	return out
}

func boardMembershipSectionResponse(items []primitives.BoardMembership) map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"board": item.Board,
			"card":  publicCardView(item.Card),
		})
	}
	return map[string]any{
		"items": out,
		"count": len(out),
	}
}

func nullableTypedRef(prefix string, id string) any {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return prefix + ":" + id
}

func boardRefs(board map[string]any) []string {
	refs, err := extractStringSlice(board["refs"])
	if err == nil {
		return uniqueSortedStrings(refs)
	}
	return []string{}
}

// mergeBoardHTTPConvenienceFields folds deprecated HTTP-only thread/document id fields (spelled
// without embedding their legacy names as contiguous substrings in this source file) into typed refs.
func mergeBoardHTTPConvenienceFields(board map[string]any) {
	if board == nil {
		return
	}
	legacyDocumentField := "primary" + "_document_id"
	refs := boardRefs(board)
	changed := false
	if did := strings.TrimSpace(anyString(board[legacyDocumentField])); did != "" {
		refs = append(refs, "document:"+did)
		changed = true
	}
	if changed {
		board["refs"] = uniqueSortedStrings(refs)
	}
	delete(board, legacyDocumentField)
}

func boardDocumentRefs(board map[string]any) []string {
	if refs, err := extractStringSlice(board["document_refs"]); err == nil {
		return uniqueSortedStrings(refs)
	}
	return []string{}
}

func boardThreadRefs(board map[string]any) []string {
	refs := make([]string, 0)
	for _, ref := range boardRefs(board) {
		prefix, value, err := schema.SplitTypedRef(ref)
		if err != nil || prefix != "thread" {
			continue
		}
		refs = append(refs, "thread:"+value)
	}
	return uniqueSortedStrings(refs)
}

func validateBoardTypedRefs(contract *schema.Contract, board map[string]any, field string) error {
	raw, exists := board[field]
	if !exists || raw == nil {
		return nil
	}
	refs, err := extractStringSlice(raw)
	if err != nil {
		return fmt.Errorf("board.%s must be a list of strings", field)
	}
	return schema.ValidateTypedRefs(contract, refs)
}

func loadBoardCardPinnedDocument(ctx context.Context, opts handlerOptions, card map[string]any) (any, error) {
	documentID := strings.TrimSpace(pinnedDocumentIDFromCard(card))
	if documentID == "" {
		return nil, nil
	}
	document, _, err := opts.primitiveStore.GetDocument(ctx, documentID)
	if err != nil {
		return nil, err
	}
	return document, nil
}

func loadBoardCardForEvent(ctx context.Context, opts handlerOptions, boardID, threadID string) (map[string]any, error) {
	return opts.primitiveStore.GetBoardCard(ctx, boardID, threadID)
}

func loadExistingBoardCardForCreateReplay(ctx context.Context, opts handlerOptions, boardID, cardID, parentThreadID, legacyThreadID string) (map[string]any, error) {
	candidates := []string{
		strings.TrimSpace(cardID),
		strings.TrimSpace(parentThreadID),
		strings.TrimSpace(legacyThreadID),
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		card, err := opts.primitiveStore.GetBoardCard(ctx, boardID, candidate)
		if err == nil {
			return card, nil
		}
		if !errors.Is(err, primitives.ErrNotFound) {
			return nil, err
		}
	}
	return nil, primitives.ErrNotFound
}

func boardCardMatchesCreateReplay(existingCard map[string]any, explicitCardID, title, body, parentThreadID, legacyThreadID, columnKey, status string, assignee, pinnedDocumentID, dueAt, resolution *string, definitionOfDone, resolutionRefs, refs []string, risk *string) bool {
	explicitCardID = strings.TrimSpace(explicitCardID)
	if explicitCardID != "" && strings.TrimSpace(anyString(existingCard["id"])) != explicitCardID {
		return false
	}
	expectedParentThread := strings.TrimSpace(firstNonEmptyString(parentThreadID, legacyThreadID))
	if expectedParentThread == "" {
		if derived, err := primitives.SoleThreadRefIDFromRefs(refs); err == nil {
			expectedParentThread = derived
		}
	}
	if expectedParentThread != "" && primaryRelatedThreadID(existingCard) != expectedParentThread {
		return false
	}
	expectedColumn := strings.TrimSpace(columnKey)
	if expectedColumn == "" {
		expectedColumn = "backlog"
	}
	if strings.TrimSpace(anyString(existingCard["column_key"])) != expectedColumn {
		return false
	}
	if title = strings.TrimSpace(title); title != "" && strings.TrimSpace(anyString(existingCard["title"])) != title {
		return false
	}
	if body = strings.TrimSpace(body); body != "" && strings.TrimSpace(anyString(existingCard["summary"])) != body {
		return false
	}
	if assignee != nil && canonicalAssigneeStorageString(existingCard) != strings.TrimSpace(*assignee) {
		return false
	}
	if pinnedDocumentID != nil && strings.TrimSpace(pinnedDocumentIDFromCard(existingCard)) != strings.TrimSpace(*pinnedDocumentID) {
		return false
	}
	expectedDueAt := ""
	if dueAt != nil {
		expectedDueAt = strings.TrimSpace(*dueAt)
	}
	if strings.TrimSpace(anyString(existingCard["due_at"])) != expectedDueAt {
		return false
	}
	expectedDefinitionOfDone := uniqueSortedStrings(definitionOfDone)
	existingDefinitionOfDone, err := extractStringSlice(existingCard["definition_of_done"])
	if err != nil {
		existingDefinitionOfDone = nil
	}
	if !stringSlicesEqual(uniqueSortedStrings(existingDefinitionOfDone), expectedDefinitionOfDone) {
		return false
	}
	expectedResolution := replayExpectedCardResolution(normalizeOptionalRequestStringPointer(resolution), status)
	if strings.TrimSpace(anyString(existingCard["resolution"])) != expectedResolution {
		return false
	}
	existingResolutionRefs, err := extractStringSlice(existingCard["resolution_refs"])
	if err != nil {
		existingResolutionRefs = nil
	}
	if !stringSlicesEqual(uniqueSortedStrings(existingResolutionRefs), uniqueSortedStrings(resolutionRefs)) {
		return false
	}
	existingRefs, err := extractStringSlice(existingCard["refs"])
	if err != nil {
		existingRefs = nil
	}
	if !stringSlicesEqual(uniqueSortedStrings(existingRefs), uniqueSortedStrings(refs)) {
		return false
	}
	expectedRisk := "low"
	if risk != nil && strings.TrimSpace(*risk) != "" {
		expectedRisk = strings.TrimSpace(*risk)
	}
	if publicCardRisk(existingCard) != expectedRisk {
		return false
	}
	return true
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func replayExpectedCardResolution(raw *string, status string) string {
	if raw != nil {
		value := strings.TrimSpace(*raw)
		switch value {
		case "completed", "superseded":
			return "done"
		case "unresolved", "":
			return ""
		default:
			return value
		}
	}
	return ""
}

func boardCardReplayPreconditionMatches(board map[string]any, ifBoardUpdatedAt *string) bool {
	if ifBoardUpdatedAt == nil {
		return true
	}
	return strings.TrimSpace(anyString(board["updated_at"])) == strings.TrimSpace(*ifBoardUpdatedAt)
}

func validateBoardCardCreateRequest(cardID, parentThreadID, legacyThreadID, columnKey, beforeCardID, afterCardID, beforeThreadID, afterThreadID string, pinnedDocumentID *string) error {
	if strings.TrimSpace(parentThreadID) != "" {
		return errors.New("parent_thread must not be set on board card create; use related_refs with a thread ref instead")
	}
	if strings.TrimSpace(legacyThreadID) != "" {
		return errors.New("thread_id must not be set on board card create")
	}
	if strings.TrimSpace(beforeThreadID) != "" || strings.TrimSpace(afterThreadID) != "" {
		return errors.New("before_thread_id and after_thread_id must not be set on board card create; use before_card_id / after_card_id")
	}
	if cardID = strings.TrimSpace(cardID); cardID != "" {
		if strings.Contains(cardID, "/") || strings.Contains(cardID, `\`) {
			return errors.New("card_id contains invalid path characters")
		}
	}
	if strings.TrimSpace(columnKey) != "" {
		switch strings.TrimSpace(columnKey) {
		case "backlog", "ready", "in_progress", "blocked", "review", "done":
		default:
			return errors.New("column_key must be one of: backlog, ready, in_progress, blocked, review, done")
		}
	}
	if strings.TrimSpace(beforeCardID) != "" && strings.TrimSpace(afterCardID) != "" {
		return errors.New("before_card_id and after_card_id are mutually exclusive")
	}
	if strings.TrimSpace(beforeThreadID) != "" && strings.TrimSpace(afterThreadID) != "" {
		return errors.New("before_thread_id and after_thread_id are mutually exclusive")
	}
	if pinnedDocumentID != nil {
		value := strings.TrimSpace(*pinnedDocumentID)
		if value != "" {
			if err := validateDocumentID(value); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseBoardCardPatchInput(w http.ResponseWriter, patch map[string]any) (primitives.UpdateBoardCardInput, []string, bool) {
	if patch == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
		return primitives.UpdateBoardCardInput{}, nil, false
	}
	if rejectMixedBoardCardPatchAliases(w, patch) {
		return primitives.UpdateBoardCardInput{}, nil, false
	}

	var (
		input         primitives.UpdateBoardCardInput
		changedFields []string
	)
	appendChanged := func(field string) {
		changedFields = append(changedFields, field)
	}
	for field, raw := range patch {
		switch field {
		case "board_id", "board_ref", "column_key", "rank":
			writeError(w, http.StatusBadRequest, "invalid_request", "patch."+field+" is not writable; use the move endpoint for board placement changes")
			return primitives.UpdateBoardCardInput{}, nil, false
		case "title":
			value := strings.TrimSpace(anyString(raw))
			if value == "" {
				writeError(w, http.StatusBadRequest, "invalid_request", "patch.title must not be empty")
				return primitives.UpdateBoardCardInput{}, nil, false
			}
			input.Title = &value
			appendChanged(field)
		case "summary":
			value := strings.TrimSpace(anyString(raw))
			input.Body = &value
			appendChanged("summary")
		case "parent_thread", "thread_id":
			writeError(w, http.StatusBadRequest, "invalid_request", "patch."+field+" is not writable; use related_refs for associations")
			return primitives.UpdateBoardCardInput{}, nil, false
		case "assignee_refs":
			value, err := extractStringSlice(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "patch.assignee_refs must be a list of strings")
				return primitives.UpdateBoardCardInput{}, nil, false
			}
			if len(value) == 0 {
				empty := ""
				input.Assignee = &empty
			} else {
				input.Assignee = assigneeStorageStringFromRefs(uniqueSortedStrings(value))
			}
			appendChanged("assignee_refs")
		case "due_at":
			value := strings.TrimSpace(anyString(raw))
			input.DueAt = &value
			appendChanged(field)
		case "definition_of_done":
			value, err := extractStringSlice(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "patch.definition_of_done must be a list of strings")
				return primitives.UpdateBoardCardInput{}, nil, false
			}
			input.DefinitionOfDone = &value
			appendChanged(field)
		case "resolution":
			value := strings.TrimSpace(anyString(raw))
			if value == "completed" || value == "superseded" {
				value = "done"
			}
			if value == "unresolved" {
				value = ""
			}
			if value != "" {
				if err := validateCardResolution(value, false); err != nil {
					writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
					return primitives.UpdateBoardCardInput{}, nil, false
				}
			}
			input.Resolution = &value
			appendChanged(field)
		case "resolution_refs":
			value, err := extractStringSlice(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "patch.resolution_refs must be a list of strings")
				return primitives.UpdateBoardCardInput{}, nil, false
			}
			input.ResolutionRefs = &value
			appendChanged(field)
		case "related_refs":
			value, err := extractStringSlice(raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "patch.related_refs must be a list of strings")
				return primitives.UpdateBoardCardInput{}, nil, false
			}
			sorted := uniqueSortedStrings(value)
			input.Refs = &sorted
			appendChanged("related_refs")
		case "document_ref":
			ref := strings.TrimSpace(anyString(raw))
			idPtr, err := pinnedDocumentIDFromTypedRef(ref)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return primitives.UpdateBoardCardInput{}, nil, false
			}
			input.PinnedDocumentID = idPtr
			appendChanged("document_ref")
		case "risk":
			value := strings.TrimSpace(anyString(raw))
			switch value {
			case "low", "medium", "high", "critical":
			default:
				writeError(w, http.StatusBadRequest, "invalid_request", "patch.risk must be one of: low, medium, high, critical")
				return primitives.UpdateBoardCardInput{}, nil, false
			}
			input.Risk = &value
			appendChanged(field)
		default:
			continue
		}
	}
	sort.Strings(changedFields)
	changedFields = compactSortedStrings(changedFields)
	return input, changedFields, true
}

func validateBoardCardMoveRequest(columnKey, beforeCardID, afterCardID, beforeThreadID, afterThreadID string) error {
	if strings.TrimSpace(beforeThreadID) != "" || strings.TrimSpace(afterThreadID) != "" {
		return errors.New("before_thread_id and after_thread_id must not be set on card move; use before_card_id / after_card_id")
	}
	if strings.TrimSpace(columnKey) == "" {
		return errors.New("column_key is required")
	}
	if err := validateBoardPlacementRequest(columnKey, "", "", nil); err != nil {
		return err
	}
	if err := primitives.ValidateBoardPlacementAnchors(beforeCardID, afterCardID); err != nil {
		return err
	}
	return nil
}

func validateCardResolution(resolution string, allowEmpty bool) error {
	value := strings.TrimSpace(resolution)
	if value == "completed" || value == "superseded" {
		value = "done"
	}
	if value == "unresolved" {
		value = ""
	}
	if value == "" && allowEmpty {
		return nil
	}
	switch value {
	case "done", "canceled":
		return nil
	default:
		return errors.New("resolution must be one of: done, canceled")
	}
}

func validateMoveCardResolutionRefs(resolution string, resolutionRefs []string) error {
	resolution = strings.TrimSpace(resolution)
	if resolution == "completed" || resolution == "superseded" {
		resolution = "done"
	}
	if len(resolutionRefs) == 0 {
		return errors.New("resolution_refs are required when resolution is set")
	}
	switch resolution {
	case "done":
		if !containsTypedRefPrefix(resolutionRefs, "artifact") && !containsTypedRefPrefix(resolutionRefs, "event") {
			return errors.New("resolution_refs must include at least one artifact: or event: ref for resolution done")
		}
	case "canceled":
		if !containsTypedRefPrefix(resolutionRefs, "event") {
			return errors.New("resolution_refs must include at least one event: ref for resolution canceled")
		}
	default:
		return errors.New("resolution must be one of: done, canceled")
	}
	return nil
}

func collectBoardWorkspaceThreadIDs(primaryThreadID string, board map[string]any, cards []map[string]any) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(cards)+1)
	primaryThreadID = strings.TrimSpace(primaryThreadID)
	if primaryThreadID != "" {
		seen[primaryThreadID] = struct{}{}
		out = append(out, primaryThreadID)
	}
	for _, threadRef := range boardThreadRefs(board) {
		_, threadID, err := schema.SplitTypedRef(threadRef)
		if err != nil || threadID == "" {
			continue
		}
		if _, ok := seen[threadID]; ok {
			continue
		}
		seen[threadID] = struct{}{}
		out = append(out, threadID)
	}
	for _, card := range cards {
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		if threadID == "" {
			continue
		}
		if _, ok := seen[threadID]; ok {
			continue
		}
		seen[threadID] = struct{}{}
		out = append(out, threadID)
	}
	return out
}

func validateBoardCreateRequest(contract *schema.Contract, board map[string]any) error {
	if board == nil {
		return errors.New("board is required")
	}
	if strings.TrimSpace(anyString(board["title"])) == "" {
		return errors.New("board.title is required")
	}
	if status := strings.TrimSpace(anyString(board["status"])); status != "" {
		if err := validateBoardStatus(status); err != nil {
			return err
		}
	}
	if err := validateBoardTypedRefs(contract, board, "refs"); err != nil {
		return err
	}
	if err := validateBoardTypedRefs(contract, board, "document_refs"); err != nil {
		return err
	}
	if err := validateBoardTypedRefs(contract, board, "pinned_refs"); err != nil {
		return err
	}
	return nil
}

func validateBoardPatchRequest(contract *schema.Contract, patch map[string]any) error {
	if patch == nil || len(patch) == 0 {
		return errors.New("patch is required")
	}
	if status, exists := patch["status"]; exists && status != nil {
		if err := validateBoardStatus(strings.TrimSpace(anyString(status))); err != nil {
			return err
		}
	}
	if err := validateBoardTypedRefs(contract, patch, "refs"); err != nil {
		return err
	}
	if err := validateBoardTypedRefs(contract, patch, "document_refs"); err != nil {
		return err
	}
	if err := validateBoardTypedRefs(contract, patch, "pinned_refs"); err != nil {
		return err
	}
	return nil
}

func validateBoardStatus(status string) error {
	switch strings.TrimSpace(status) {
	case "active", "paused", "closed":
		return nil
	case "":
		return errors.New("board.status is required")
	default:
		return errors.New("board.status must be one of: active, paused, closed")
	}
}

func validateBoardPlacementRequest(columnKey, beforeThreadID, afterThreadID string, pinnedDocumentID *string) error {
	if strings.TrimSpace(columnKey) != "" {
		switch strings.TrimSpace(columnKey) {
		case "backlog", "ready", "in_progress", "blocked", "review", "done":
		default:
			return errors.New("column_key must be one of: backlog, ready, in_progress, blocked, review, done")
		}
	}
	if strings.TrimSpace(beforeThreadID) != "" && strings.TrimSpace(afterThreadID) != "" {
		return errors.New("before_thread_id and after_thread_id are mutually exclusive")
	}
	if pinnedDocumentID != nil {
		documentID := strings.TrimSpace(*pinnedDocumentID)
		if documentID != "" {
			if err := validateDocumentID(documentID); err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeRequiredTimestamp(w http.ResponseWriter, value *string, fieldName string) (string, bool) {
	raw := strings.TrimSpace(*value)
	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", fieldName+" must be an RFC3339 datetime string")
		return "", false
	}
	return raw, true
}

func normalizeOptionalRequestStringPointer(raw *string) *string {
	if raw == nil {
		return nil
	}
	value := strings.TrimSpace(*raw)
	return &value
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactSortedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	last := ""
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == last {
			continue
		}
		out = append(out, value)
		last = value
	}
	return out
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsTypedRefPrefix(refs []string, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	for _, ref := range refs {
		refPrefix, _, err := schema.SplitTypedRef(strings.TrimSpace(ref))
		if err == nil && refPrefix == prefix {
			return true
		}
	}
	return false
}

func boardCardDerivedSummary(card map[string]any, threadID string, states map[string]topicProjectionState, now time.Time) any {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	projection := states[threadID].Projection
	riskState, dueAt, hasDueAt := boardCardRiskState(card, now, defaultInboxRiskHorizon)
	summary := map[string]any{
		"decision_request_count": projection.DecisionRequestCount,
		"decision_count":         projection.DecisionCount,
		"recommendation_count":   projection.RecommendationCount,
		"document_count":         projection.DocumentCount,
		"inbox_count":            projection.InboxCount,
		"latest_activity_at":     nullableStringValue(projection.LastActivityAt),
		"stale":                  projection.Stale,
		"risk_state":             nullableStringValue(riskState),
	}
	if hasDueAt {
		summary["due_at"] = dueAt.Format(time.RFC3339)
	}
	return summary
}

func boardCardDerivedFreshness(threadID string, states map[string]topicProjectionState) any {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	return cloneWorkspaceMap(states[threadID].Freshness)
}

func laterTimestamp(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	}
	leftTime, leftErr := time.Parse(time.RFC3339Nano, left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, right)
	if leftErr != nil || rightErr != nil {
		if right > left {
			return right
		}
		return left
	}
	if rightTime.After(leftTime) {
		return right
	}
	return left
}

func nullableStringValue(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func boardDisplayName(board map[string]any) string {
	if title := strings.TrimSpace(anyString(board["title"])); title != "" {
		return title
	}
	if boardID := strings.TrimSpace(anyString(board["id"])); boardID != "" {
		return boardID
	}
	return "board"
}

func cardDisplayName(card map[string]any) string {
	if threadID := strings.TrimSpace(anyString(card["thread_id"])); threadID != "" {
		return threadID
	}
	return "card"
}

func mapValues(values map[string]map[string]any) []map[string]any {
	if len(values) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
