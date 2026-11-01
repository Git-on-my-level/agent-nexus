// HTTP handlers for board CRUD and lifecycle (list, create, get, patch,
// archive, trash, restore, purge), plus board create/patch request validation.
// Board-scoped card handlers live in board_cards_handlers.go and
// board_card_mutations.go; the board workspace view in
// board_workspace_handlers.go.

package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/schema"
)

func handleListBoards(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	query := r.URL.Query()
	states, parseErr := ParseListLifecycleStates(query)
	if parseErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", parseErr.Error())
		return
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
		States: states,
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
		writeInternalError(w, r, err, "failed to list boards")
		return
	}

	accessibleBoards := make([]primitives.BoardListItem, 0, len(items))
	boardIDs := []string{}
	for _, item := range items {
		if threadAccessible(r, opts, anyString(item.Board["thread_id"])) {
			accessibleBoards = append(accessibleBoards, item)
			boardIDs = append(boardIDs, anyString(item.Board["id"]))
		}
	}
	if store, ok := opts.primitiveStore.(interface {
		GetBoardSummariesVisible(context.Context, []string, func(string, string) bool) (map[string]map[string]any, error)
	}); ok {
		summaries, err := store.GetBoardSummariesVisible(r.Context(), boardIDs, planVisibility(r, opts))
		if err != nil {
			workStoreError(w, r, err)
			return
		}
		for i := range accessibleBoards {
			accessibleBoards[i].Summary = summaries[anyString(accessibleBoards[i].Board["id"])]
		}
	}
	response := map[string]any{
		"boards": boardListItemsResponse(accessibleBoards),
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
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, "board.summary", req.Board, "summary") {
		return
	}

	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.create", actorID, req.RequestKey, req)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeInternalError(w, r, err, "failed to load idempotency replay")
		return
	}
	if replayed {
		writeJSON(w, replayStatus, replayPayload)
		return
	}

	boardInput := prepareBoardWriteMap(req.Board)
	if err := validateBoardCreateRequest(opts.contract, boardInput); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	board, err := opts.primitiveStore.CreateBoard(r.Context(), actorID, boardInput)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" && derivedBoardID {
			boardID := firstNonEmptyString(req.Board["id"])
			existing, loadErr := opts.primitiveStore.GetBoard(r.Context(), boardID)
			if loadErr == nil {
				summary, summaryErr := opts.primitiveStore.GetBoardSummary(r.Context(), boardID)
				if summaryErr == nil {
					response := hygiene.attach(map[string]any{"board": existing, "summary": summary})
					status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.create", actorID, req.RequestKey, req, http.StatusCreated, response)
					if writeIdempotencyError(w, replayErr) {
						return
					}
					if replayErr != nil {
						writeInternalError(w, r, replayErr, "failed to persist idempotency replay")
						return
					}
					writeJSON(w, status, payload)
					return
				}
				response := hygiene.attach(map[string]any{"board": existing})
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.create", actorID, req.RequestKey, req, http.StatusCreated, response)
				if writeIdempotencyError(w, replayErr) {
					return
				}
				if replayErr != nil {
					writeInternalError(w, r, replayErr, "failed to persist idempotency replay")
					return
				}
				writeJSON(w, status, payload)
				return
			}
		}
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board already exists")
		default:
			writeInternalError(w, r, err, "failed to create board")
		}
		return
	}

	createdEvent := buildBoardCreatedEvent(board)
	if move, ok := req.Board["workspace_move"]; ok {
		if payload, ok := createdEvent["payload"].(map[string]any); ok {
			payload["workspace_move"] = move
		}
	}
	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, createdEvent)

	summary, summaryErr := opts.primitiveStore.GetBoardSummary(r.Context(), board["id"].(string))
	response := map[string]any{"board": board}
	if summaryErr == nil {
		response["summary"] = summary
	}
	response = hygiene.attach(response)
	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.create", actorID, req.RequestKey, req, http.StatusCreated, response)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeInternalError(w, r, err, "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, payload)
}

func handleGetBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
		return
	}

	board, err := opts.primitiveStore.GetBoard(r.Context(), boardID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board not found")
			return
		}
		writeInternalError(w, r, err, "failed to load board")
		return
	}

	summary, summaryErr := opts.primitiveStore.GetBoardSummary(r.Context(), boardID)
	if store, ok := opts.primitiveStore.(interface {
		GetBoardSummariesVisible(context.Context, []string, func(string, string) bool) (map[string]map[string]any, error)
	}); ok {
		summaries, err := store.GetBoardSummariesVisible(r.Context(), []string{boardID}, planVisibility(r, opts))
		summary, summaryErr = summaries[boardID], err
	}
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
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
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
		writeInternalError(w, r, err, "failed to load board")
		return
	}

	patchInput := prepareBoardWriteMap(req.Patch)
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, "patch.summary", patchInput, "summary") {
		return
	}
	if err := validateBoardPatchRequest(opts.contract, patchInput); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	updatedBoard, err := opts.primitiveStore.UpdateBoard(r.Context(), actorID, boardID, patchInput, &ifUpdatedAt)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or referenced document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeInternalError(w, r, err, "failed to update board")
		}
		return
	}

	emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardUpdatedEvent(currentBoard, updatedBoard, patchInput))

	summary, summaryErr := opts.primitiveStore.GetBoardSummary(r.Context(), boardID)
	response := map[string]any{"board": updatedBoard}
	if summaryErr == nil {
		response["summary"] = summary
	}
	writeJSON(w, http.StatusOK, hygiene.attach(response))
}

func writeBoardLifecycleStoreError(w http.ResponseWriter, err error) bool {
	if writePrimitiveQuotaViolationError(w, err) {
		return true
	}
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
	case errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
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
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
		return
	}
	var req struct {
		ActorID     string  `json:"actor_id"`
		IfUpdatedAt *string `json:"if_updated_at"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.IfUpdatedAt != nil {
		normalized, ok := normalizeRequiredTimestamp(w, req.IfUpdatedAt, "if_updated_at")
		if !ok {
			return
		}
		req.IfUpdatedAt = &normalized
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	board, err := opts.primitiveStore.ArchiveBoardIfUpdatedAt(r.Context(), actorID, boardID, req.IfUpdatedAt)
	if err != nil {
		if writeBoardLifecycleStoreError(w, err) {
			return
		}
		writeInternalError(w, r, err, "failed to archive board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": board})
}

func handleUnarchiveBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
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
		writeInternalError(w, r, err, "failed to unarchive board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": board})
}

func handleTrashBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
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
		writeInternalError(w, r, err, "failed to trash board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": board})
}

func handleRestoreBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
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
		writeInternalError(w, r, err, "failed to restore board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": board})
}

func handlePurgeBoard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
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
		writeInternalError(w, r, err, "failed to permanently delete board")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"purged": true, "board_id": boardID})
}

func validateBoardCreateRequest(contract *schema.Contract, board map[string]any) error {
	if board == nil {
		return errors.New("board is required")
	}
	if strings.TrimSpace(anyString(board["title"])) == "" {
		return errors.New("board.title is required")
	}
	if _, exists := board["status"]; exists {
		return errors.New("board.status is not supported; board.state is derived from lifecycle timestamps")
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
	if _, exists := board["labels"]; exists {
		return errors.New("board.labels is not supported")
	}
	return nil
}

func validateBoardPatchRequest(contract *schema.Contract, patch map[string]any) error {
	if patch == nil || len(patch) == 0 {
		return errors.New("patch is required")
	}
	if _, exists := patch["status"]; exists {
		return errors.New("patch.status is not supported; use archive/trash lifecycle routes")
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
	if _, exists := patch["labels"]; exists {
		return errors.New("patch.labels is not supported")
	}
	return nil
}
