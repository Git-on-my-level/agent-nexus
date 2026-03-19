package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

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
		Status: status,
		Labels: normalizedQueryValues(query["label"]),
		Owners: normalizedQueryValues(query["owner"]),
		Query:  strings.TrimSpace(query.Get("q")),
		Limit:  limitFilter,
		Cursor: strings.TrimSpace(query.Get("cursor")),
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
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
		req.Board["id"] = deriveRequestScopedID("boards.create", actorID, req.RequestKey, "board")
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

	board, err := opts.primitiveStore.CreateBoard(r.Context(), actorID, req.Board)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" && derivedBoardID {
			boardID := firstNonEmptyString(req.Board["id"])
			existing, loadErr := opts.primitiveStore.GetBoard(r.Context(), boardID)
			if loadErr == nil {
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
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "referenced primary thread or document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board already exists")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to create board")
		}
		return
	}

	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCreatedEvent(board))

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.create", actorID, req.RequestKey, req, http.StatusCreated, map[string]any{"board": board})
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

	writeJSON(w, http.StatusOK, map[string]any{"board": board})
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
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

	writeJSON(w, http.StatusOK, map[string]any{"board": updatedBoard})
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
		"cards":    cards,
	})
}

func handleAddBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID          string  `json:"actor_id"`
		RequestKey       string  `json:"request_key"`
		IfBoardUpdatedAt *string `json:"if_board_updated_at"`
		ThreadID         string  `json:"thread_id"`
		ColumnKey        string  `json:"column_key"`
		BeforeThreadID   string  `json:"before_thread_id"`
		AfterThreadID    string  `json:"after_thread_id"`
		PinnedDocumentID *string `json:"pinned_document_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	threadID := strings.TrimSpace(req.ThreadID)
	if threadID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "thread_id is required")
		return
	}
	if req.IfBoardUpdatedAt != nil {
		normalized, ok := normalizeRequiredTimestamp(w, req.IfBoardUpdatedAt, "if_board_updated_at")
		if !ok {
			return
		}
		req.IfBoardUpdatedAt = &normalized
	}
	if err := validateBoardPlacementRequest(req.ColumnKey, req.BeforeThreadID, req.AfterThreadID, req.PinnedDocumentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.cards.add", actorID, req.RequestKey, req)
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

	result, err := opts.primitiveStore.AddBoardCard(r.Context(), actorID, boardID, primitives.AddBoardCardInput{
		ThreadID:         threadID,
		ColumnKey:        strings.TrimSpace(req.ColumnKey),
		BeforeThreadID:   strings.TrimSpace(req.BeforeThreadID),
		AfterThreadID:    strings.TrimSpace(req.AfterThreadID),
		PinnedDocumentID: normalizeOptionalRequestStringPointer(req.PinnedDocumentID),
		IfBoardUpdatedAt: req.IfBoardUpdatedAt,
	})
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" && req.IfBoardUpdatedAt == nil {
			existingBoard, loadBoardErr := opts.primitiveStore.GetBoard(r.Context(), boardID)
			existingCards, loadCardsErr := opts.primitiveStore.ListBoardCards(r.Context(), boardID)
			if loadBoardErr == nil && loadCardsErr == nil {
				if existingCard, ok := findBoardCard(existingCards, threadID); ok && boardCardMatchesAddRequest(existingCard, strings.TrimSpace(req.ColumnKey), normalizeOptionalRequestStringPointer(req.PinnedDocumentID)) {
					response := map[string]any{"board": existingBoard, "card": existingCard}
					status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.cards.add", actorID, req.RequestKey, req, http.StatusCreated, response)
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
		}
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board, thread, or document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board membership already exists or board has changed; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to add board card")
		}
		return
	}

	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardAddedEvent(result.Board, result.Card))

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.cards.add", actorID, req.RequestKey, req, http.StatusCreated, map[string]any{
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
	writeJSON(w, status, payload)
}

func handleUpdateBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID          string         `json:"actor_id"`
		IfBoardUpdatedAt *string        `json:"if_board_updated_at"`
		Patch            map[string]any `json:"patch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
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
	if req.Patch == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
		return
	}
	pinnedDocumentID, hasPinnedDocumentID, ok := boardCardPatchPinnedDocumentID(w, req.Patch)
	if !ok {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	beforeCard, err := loadBoardCardForEvent(r.Context(), opts, boardID, threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board card")
		return
	}
	if !hasPinnedDocumentID {
		currentBoard, err := opts.primitiveStore.GetBoard(r.Context(), boardID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "board or card not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board")
			return
		}
		if strings.TrimSpace(anyString(currentBoard["updated_at"])) != ifBoardUpdatedAt {
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"board": currentBoard, "card": beforeCard})
		return
	}

	result, err := opts.primitiveStore.UpdateBoardCard(r.Context(), actorID, boardID, threadID, primitives.UpdateBoardCardInput{
		PinnedDocumentID: pinnedDocumentID,
		IfBoardUpdatedAt: &ifBoardUpdatedAt,
	})
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

	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardUpdatedEvent(result.Board, beforeCard, result.Card))

	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "card": result.Card})
}

func handleMoveBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID          string  `json:"actor_id"`
		IfBoardUpdatedAt *string `json:"if_board_updated_at"`
		ColumnKey        string  `json:"column_key"`
		BeforeThreadID   string  `json:"before_thread_id"`
		AfterThreadID    string  `json:"after_thread_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
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
	if err := validateBoardPlacementRequest(req.ColumnKey, req.BeforeThreadID, req.AfterThreadID, nil); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	beforeCard, err := loadBoardCardForEvent(r.Context(), opts, boardID, threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board card")
		return
	}

	result, err := opts.primitiveStore.MoveBoardCard(r.Context(), actorID, boardID, threadID, primitives.MoveBoardCardInput{
		ColumnKey:        strings.TrimSpace(req.ColumnKey),
		BeforeThreadID:   strings.TrimSpace(req.BeforeThreadID),
		AfterThreadID:    strings.TrimSpace(req.AfterThreadID),
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

	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardMovedEvent(result.Board, beforeCard, result.Card, req.BeforeThreadID, req.AfterThreadID))

	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "card": result.Card})
}

func handleRemoveBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID          string  `json:"actor_id"`
		IfBoardUpdatedAt *string `json:"if_board_updated_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
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

	beforeCard, err := loadBoardCardForEvent(r.Context(), opts, boardID, threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load board card")
		return
	}

	result, err := opts.primitiveStore.RemoveBoardCard(r.Context(), actorID, boardID, threadID, primitives.RemoveBoardCardInput{
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

	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardRemovedEvent(result.Board, beforeCard))

	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "removed_thread_id": result.RemovedThreadID})
}

func buildBoardWorkspacePayload(ctx context.Context, opts handlerOptions, boardID string) (map[string]any, error) {
	board, err := opts.primitiveStore.GetBoard(ctx, boardID)
	if err != nil {
		return nil, err
	}

	primaryThreadID := strings.TrimSpace(anyString(board["primary_thread_id"]))
	primaryThread, err := opts.primitiveStore.GetThread(ctx, primaryThreadID)
	if err != nil {
		return nil, err
	}

	primaryDocument, warnings, err := loadBoardWorkspacePrimaryDocument(ctx, opts, board)
	if err != nil {
		return nil, err
	}

	cards, err := opts.primitiveStore.ListBoardCards(ctx, boardID)
	if err != nil {
		return nil, err
	}

	threadIDs := collectBoardWorkspaceThreadIDs(primaryThreadID, cards)
	now := time.Now().UTC()
	states, err := loadThreadProjectionStates(ctx, opts, threadIDs)
	if err != nil {
		return nil, err
	}

	cardSection, cardWarnings, err := buildBoardWorkspaceCardsSection(ctx, opts, cards, states)
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, cardWarnings...)

	documentsSection, err := buildBoardWorkspaceDocumentsSection(ctx, opts, threadIDs)
	if err != nil {
		return nil, err
	}
	commitmentsSection, err := buildBoardWorkspaceCommitmentsSection(ctx, opts, threadIDs)
	if err != nil {
		return nil, err
	}
	inboxSection, err := buildBoardWorkspaceInboxSection(ctx, opts, threadIDs, now, states)
	if err != nil {
		return nil, err
	}

	boardSummary := buildBoardWorkspaceSummary(board, cards, states)
	freshness := aggregateThreadProjectionFreshness(states, threadIDs)
	return map[string]any{
		"board_id":                boardID,
		"board":                   board,
		"primary_thread":          primaryThread,
		"primary_document":        primaryDocument,
		"cards":                   cardSection,
		"documents":               documentsSection,
		"commitments":             commitmentsSection,
		"inbox":                   inboxSection,
		"board_summary":           boardSummary,
		"projection_freshness":    freshness,
		"board_summary_freshness": cloneWorkspaceMap(freshness),
		"warnings":                map[string]any{"items": warnings, "count": len(warnings)},
		"section_kinds":           map[string]any{"board": "canonical", "primary_thread": "canonical", "primary_document": "canonical", "cards": "convenience", "documents": "derived", "commitments": "derived", "inbox": "derived", "board_summary": "derived"},
		"generated_at":            now.Format(time.RFC3339Nano),
	}, nil
}

func buildBoardWorkspaceCardsSection(ctx context.Context, opts handlerOptions, cards []map[string]any, states map[string]threadProjectionState) (map[string]any, []map[string]any, error) {
	items := make([]map[string]any, 0, len(cards))
	warnings := make([]map[string]any, 0)
	for _, card := range cards {
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		thread, err := opts.primitiveStore.GetThread(ctx, threadID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				warnings = append(warnings, map[string]any{"thread_id": threadID, "message": "skipped board card because backing thread was not found"})
				continue
			}
			return nil, nil, err
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

		items = append(items, map[string]any{
			"membership": card,
			"backing": map[string]any{
				"thread_ref":          "thread:" + threadID,
				"thread":              thread,
				"pinned_document_ref": nullableTypedRef("document", anyString(card["pinned_document_id"])),
				"pinned_document":     pinnedDocument,
			},
			"derived": map[string]any{
				"summary":   boardCardSummaryFromProjection(states[threadID].Projection),
				"freshness": cloneWorkspaceMap(states[threadID].Freshness),
			},
		})
	}

	return map[string]any{"items": items, "count": len(items)}, warnings, nil
}

func buildBoardWorkspaceDocumentsSection(ctx context.Context, opts handlerOptions, threadIDs []string) (map[string]any, error) {
	seen := map[string]map[string]any{}
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

func buildBoardWorkspaceCommitmentsSection(ctx context.Context, opts handlerOptions, threadIDs []string) (map[string]any, error) {
	seen := map[string]map[string]any{}
	for _, threadID := range threadIDs {
		commitments, err := opts.primitiveStore.ListCommitments(ctx, primitives.CommitmentListFilter{ThreadID: threadID})
		if err != nil {
			return nil, err
		}
		for _, commitment := range commitments {
			commitmentID := strings.TrimSpace(anyString(commitment["id"]))
			if commitmentID == "" {
				continue
			}
			seen[commitmentID] = commitment
		}
	}

	items := mapValues(seen)
	sort.SliceStable(items, func(i int, j int) bool {
		leftDue := strings.TrimSpace(anyString(items[i]["due_at"]))
		rightDue := strings.TrimSpace(anyString(items[j]["due_at"]))
		if leftDue != rightDue {
			if leftDue == "" {
				return false
			}
			if rightDue == "" {
				return true
			}
			return leftDue < rightDue
		}
		return strings.TrimSpace(anyString(items[i]["id"])) < strings.TrimSpace(anyString(items[j]["id"]))
	})
	return map[string]any{"items": items, "count": len(items)}, nil
}

func buildBoardWorkspaceInboxSection(ctx context.Context, opts handlerOptions, threadIDs []string, now time.Time, states map[string]threadProjectionState) (map[string]any, error) {
	items := make([]map[string]any, 0)
	for _, threadID := range threadIDs {
		threadItems, err := opts.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
		if err != nil {
			return nil, err
		}
		for _, item := range threadItems {
			copy := cloneWorkspaceMap(item.Data)
			if copy == nil {
				copy = map[string]any{}
			}
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
		"projection_freshness": aggregateThreadProjectionFreshness(states, threadIDs),
	}, nil
}

func buildBoardWorkspaceSummary(board map[string]any, cards []map[string]any, states map[string]threadProjectionState) map[string]any {
	cardsByColumn := map[string]any{
		"backlog":     0,
		"ready":       0,
		"in_progress": 0,
		"blocked":     0,
		"review":      0,
		"done":        0,
	}

	threadIDs := map[string]struct{}{}
	primaryThreadID := strings.TrimSpace(anyString(board["primary_thread_id"]))
	if primaryThreadID != "" {
		threadIDs[primaryThreadID] = struct{}{}
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

	openCommitmentCount := 0
	documentCount := 0
	latestActivityAt := strings.TrimSpace(anyString(board["updated_at"]))
	for threadID := range threadIDs {
		projection := states[threadID].Projection
		openCommitmentCount += projection.OpenCommitmentCount
		documentCount += projection.DocumentCount
		latestActivityAt = laterTimestamp(latestActivityAt, projection.LastActivityAt)
	}

	return map[string]any{
		"card_count":            len(cards),
		"cards_by_column":       cardsByColumn,
		"open_commitment_count": openCommitmentCount,
		"document_count":        documentCount,
		"latest_activity_at":    nullableStringValue(latestActivityAt),
		"has_primary_document":  strings.TrimSpace(anyString(board["primary_document_id"])) != "",
	}
}

func buildBoardCreatedEvent(board map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":            anyString(board["id"]),
		"primary_thread_id":   anyString(board["primary_thread_id"]),
		"primary_document_id": nullableStringValue(anyString(board["primary_document_id"])),
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
		"board_id":           anyString(board["id"]),
		"card_thread_id":     anyString(card["thread_id"]),
		"column_key":         anyString(card["column_key"]),
		"pinned_document_id": nullableStringValue(anyString(card["pinned_document_id"])),
	}
	return buildBoardLifecycleEvent("board_card_added", board, card, payload, "Board card added: "+cardDisplayName(card))
}

func buildBoardCardUpdatedEvent(board, previousCard, updatedCard map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":                    anyString(board["id"]),
		"card_thread_id":              anyString(updatedCard["thread_id"]),
		"changed_fields":              []string{"pinned_document_id"},
		"previous_pinned_document_id": nullableStringValue(anyString(previousCard["pinned_document_id"])),
		"pinned_document_id":          nullableStringValue(anyString(updatedCard["pinned_document_id"])),
	}
	return buildBoardLifecycleEvent("board_card_updated", board, updatedCard, payload, "Board card updated: "+cardDisplayName(updatedCard))
}

func buildBoardCardMovedEvent(board, previousCard, updatedCard map[string]any, beforeThreadID, afterThreadID string) map[string]any {
	payload := map[string]any{
		"board_id":         anyString(board["id"]),
		"card_thread_id":   anyString(updatedCard["thread_id"]),
		"from_column_key":  nullableStringValue(anyString(previousCard["column_key"])),
		"column_key":       nullableStringValue(anyString(updatedCard["column_key"])),
		"before_thread_id": nullableStringValue(strings.TrimSpace(beforeThreadID)),
		"after_thread_id":  nullableStringValue(strings.TrimSpace(afterThreadID)),
	}
	return buildBoardLifecycleEvent("board_card_moved", board, updatedCard, payload, "Board card moved: "+cardDisplayName(updatedCard))
}

func buildBoardCardRemovedEvent(board, previousCard map[string]any) map[string]any {
	payload := map[string]any{
		"board_id":           anyString(board["id"]),
		"card_thread_id":     anyString(previousCard["thread_id"]),
		"column_key":         nullableStringValue(anyString(previousCard["column_key"])),
		"pinned_document_id": nullableStringValue(anyString(previousCard["pinned_document_id"])),
	}
	return buildBoardLifecycleEvent("board_card_removed", board, previousCard, payload, "Board card removed: "+cardDisplayName(previousCard))
}

func buildBoardLifecycleEvent(eventType string, board, card map[string]any, payload map[string]any, summary string) map[string]any {
	refs := []string{"board:" + anyString(board["id"])}
	if primaryThreadID := strings.TrimSpace(anyString(board["primary_thread_id"])); primaryThreadID != "" {
		refs = append(refs, "thread:"+primaryThreadID)
	}
	if primaryDocumentID := strings.TrimSpace(anyString(board["primary_document_id"])); primaryDocumentID != "" {
		refs = append(refs, "document:"+primaryDocumentID)
	}
	if card != nil {
		if threadID := strings.TrimSpace(anyString(card["thread_id"])); threadID != "" {
			refs = append(refs, "thread:"+threadID)
		}
		if pinnedDocumentID := strings.TrimSpace(anyString(card["pinned_document_id"])); pinnedDocumentID != "" {
			refs = append(refs, "document:"+pinnedDocumentID)
		}
	}
	refs = uniqueSortedStrings(refs)

	event := map[string]any{
		"type":       eventType,
		"thread_id":  anyString(board["primary_thread_id"]),
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
	enqueueThreadProjectionsBestEffort(ctx, opts, []string{anyString(stored["thread_id"])}, time.Now().UTC())
	return nil
}

func emitBoardLifecycleEventBestEffort(ctx context.Context, opts handlerOptions, actorID string, event map[string]any) {
	_ = emitBoardLifecycleEvent(ctx, opts, actorID, event)
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
			"card":  item.Card,
		})
	}
	return map[string]any{
		"items": out,
		"count": len(out),
	}
}

func boardCardSummaryFromProjection(projection primitives.DerivedThreadProjection) map[string]any {
	return map[string]any{
		"open_commitment_count":  projection.OpenCommitmentCount,
		"decision_request_count": projection.DecisionRequestCount,
		"decision_count":         projection.DecisionCount,
		"recommendation_count":   projection.RecommendationCount,
		"document_count":         projection.DocumentCount,
		"inbox_count":            projection.InboxCount,
		"latest_activity_at":     nullableStringValue(projection.LastActivityAt),
		"stale":                  projection.Stale,
	}
}

func nullableTypedRef(prefix string, id string) any {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return prefix + ":" + id
}

func loadBoardWorkspacePrimaryDocument(ctx context.Context, opts handlerOptions, board map[string]any) (any, []map[string]any, error) {
	primaryDocumentID := strings.TrimSpace(anyString(board["primary_document_id"]))
	if primaryDocumentID == "" {
		return nil, nil, nil
	}
	document, _, err := opts.primitiveStore.GetDocument(ctx, primaryDocumentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return nil, []map[string]any{{"document_id": primaryDocumentID, "message": "board primary document is no longer available"}}, nil
		}
		return nil, nil, err
	}
	return document, nil, nil
}

func loadBoardCardPinnedDocument(ctx context.Context, opts handlerOptions, card map[string]any) (any, error) {
	documentID := strings.TrimSpace(anyString(card["pinned_document_id"]))
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
	cards, err := opts.primitiveStore.ListBoardCards(ctx, boardID)
	if err != nil {
		return nil, err
	}
	card, ok := findBoardCard(cards, threadID)
	if !ok {
		return nil, primitives.ErrNotFound
	}
	return card, nil
}

func collectBoardWorkspaceThreadIDs(primaryThreadID string, cards []map[string]any) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(cards)+1)
	primaryThreadID = strings.TrimSpace(primaryThreadID)
	if primaryThreadID != "" {
		seen[primaryThreadID] = struct{}{}
		out = append(out, primaryThreadID)
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

func findBoardCard(cards []map[string]any, threadID string) (map[string]any, bool) {
	threadID = strings.TrimSpace(threadID)
	for _, card := range cards {
		if strings.TrimSpace(anyString(card["thread_id"])) == threadID {
			return card, true
		}
	}
	return nil, false
}

func boardCardMatchesAddRequest(card map[string]any, requestedColumnKey string, requestedPinnedDocumentID *string) bool {
	columnKey := strings.TrimSpace(requestedColumnKey)
	if columnKey == "" {
		columnKey = "backlog"
	}
	if strings.TrimSpace(anyString(card["column_key"])) != columnKey {
		return false
	}

	requestedPinned := ""
	if requestedPinnedDocumentID != nil {
		requestedPinned = strings.TrimSpace(*requestedPinnedDocumentID)
	}
	return strings.TrimSpace(anyString(card["pinned_document_id"])) == requestedPinned
}

func validateBoardCreateRequest(contract *schema.Contract, board map[string]any) error {
	if board == nil {
		return errors.New("board is required")
	}
	if strings.TrimSpace(anyString(board["title"])) == "" {
		return errors.New("board.title is required")
	}
	if strings.TrimSpace(anyString(board["primary_thread_id"])) == "" {
		return errors.New("board.primary_thread_id is required")
	}
	if status := strings.TrimSpace(anyString(board["status"])); status != "" {
		if err := validateBoardStatus(status); err != nil {
			return err
		}
	}
	if primaryDocumentID := strings.TrimSpace(anyString(board["primary_document_id"])); primaryDocumentID != "" {
		if err := validateDocumentID(primaryDocumentID); err != nil {
			return err
		}
	}
	if pinnedRefs, exists := board["pinned_refs"]; exists && pinnedRefs != nil {
		refs, err := extractStringSlice(pinnedRefs)
		if err != nil {
			return errors.New("board.pinned_refs must be a list of strings")
		}
		if err := schema.ValidateTypedRefs(contract, refs); err != nil {
			return err
		}
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
	if primaryDocumentID, exists := patch["primary_document_id"]; exists && primaryDocumentID != nil {
		if err := validateDocumentID(strings.TrimSpace(anyString(primaryDocumentID))); err != nil {
			return err
		}
	}
	if pinnedRefs, exists := patch["pinned_refs"]; exists {
		refs, err := extractStringSlice(pinnedRefs)
		if err != nil {
			return errors.New("board.pinned_refs must be a list of strings")
		}
		if err := schema.ValidateTypedRefs(contract, refs); err != nil {
			return err
		}
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

func boardCardPatchPinnedDocumentID(w http.ResponseWriter, patch map[string]any) (*string, bool, bool) {
	rawValue, exists := patch["pinned_document_id"]
	if !exists {
		return nil, false, true
	}
	if rawValue == nil {
		empty := ""
		return &empty, true, true
	}
	documentID := strings.TrimSpace(anyString(rawValue))
	if documentID == "" {
		empty := ""
		return &empty, true, true
	}
	if err := validateDocumentID(documentID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return nil, false, false
	}
	return &documentID, true, true
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
