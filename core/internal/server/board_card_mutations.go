// HTTP handlers that mutate an existing board card: update, move, remove,
// archive, and trash.

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/schema"
)

func handleUpdateBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, cardKey string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
		return
	}
	cardKey, ok = resolveHTTPResourceID(w, r, opts, "card", cardKey, "card")
	if !ok {
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
	if opts.contract != nil && patchInput.ResolutionRefs != nil {
		if err := schema.ValidateTypedRefs(opts.contract, *patchInput.ResolutionRefs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	if opts.contract != nil && patchInput.Refs != nil {
		if err := schema.ValidateTypedRefs(opts.contract, *patchInput.Refs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	var hygiene markdownHygieneCollector
	if patchInput.Body != nil {
		body := *patchInput.Body
		if !hygiene.normalizeField(w, "patch.summary", &body) {
			return
		}
		body = strings.TrimSpace(body)
		patchInput.Body = &body
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
		writeInternalError(w, r, err, "failed to load board card")
		return
	}
	if len(changedFields) == 0 {
		currentBoard, err := opts.primitiveStore.GetBoard(r.Context(), anyString(beforeCard["board_id"]))
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "board or card not found")
				return
			}
			writeInternalError(w, r, err, "failed to load board")
			return
		}
		if !boardCardReplayPreconditionMatches(currentBoard, patchInput.IfBoardUpdatedAt) {
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
			return
		}
		writeJSON(w, http.StatusOK, hygiene.attach(map[string]any{"board": currentBoard, "card": cardMutationEchoView(r.Context(), r, opts, beforeCard)}))
		return
	}

	result, err := opts.primitiveStore.UpdateBoardCard(r.Context(), actorID, boardID, cardKey, patchInput)
	if err != nil {
		if writeStoreContentionError(w, r, err) {
			return
		}
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board, card, or document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeInternalError(w, r, err, "failed to update board card")
		}
		return
	}

	if anyString(result.Card["updated_at"]) != anyString(beforeCard["updated_at"]) || anyString(result.Card["version"]) != anyString(beforeCard["version"]) {
		storedLifecycle, emitErr := emitCardLifecycleEvent(r.Context(), opts, actorID, buildCardUpdatedEvent(result.Board, beforeCard, result.Card, changedFields))
		if emitErr == nil && storedLifecycle != nil {
			enqueueCardAssigneeWakeBestEffort(r.Context(), opts, actorID, beforeCard, result.Card, result.Board, storedLifecycle)
		}
	}

	writeJSON(w, http.StatusOK, hygiene.attach(map[string]any{"board": result.Board, "card": cardMutationEchoView(r.Context(), r, opts, result.Card)}))
}

func handleMoveBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, cardKey string) {
	handleMoveCardMutation(w, r, opts, boardID, cardKey, "board or card not found")
}

func handleMoveCardMutation(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, cardKey, notFoundMessage string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	if strings.TrimSpace(boardID) != "" {
		boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
		if !ok {
			return
		}
	}
	cardKey, ok = resolveHTTPResourceID(w, r, opts, "card", cardKey, "card")
	if !ok {
		return
	}

	var req struct {
		BoardRef         string   `json:"board_ref"`
		Reason           string   `json:"reason"`
		ActorID          string   `json:"actor_id"`
		RequestKey       string   `json:"request_key"`
		IfWorkVersion    *int64   `json:"if_version"`
		IfBoardUpdatedAt *string  `json:"if_board_updated_at"`
		ColumnKey        string   `json:"column_key"`
		BeforeCardID     string   `json:"before_card_id"`
		AfterCardID      string   `json:"after_card_id"`
		BeforeThreadID   string   `json:"before_thread_id"`
		AfterThreadID    string   `json:"after_thread_id"`
		Resolution       *string  `json:"resolution"`
		ResolutionRefs   []string `json:"resolution_refs"`
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
	if strings.TrimSpace(req.ColumnKey) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "column_key is required")
		return
	}
	if err := validateBoardCardMoveRequest(req.ColumnKey, req.BeforeCardID, req.AfterCardID, req.BeforeThreadID, req.AfterThreadID); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	req.ResolutionRefs = uniqueSortedStrings(req.ResolutionRefs)
	if req.Resolution != nil {
		normalizedResolution := strings.TrimSpace(*req.Resolution)
		if err := validateCardResolution(normalizedResolution, false); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		req.Resolution = &normalizedResolution
	}
	if req.Resolution == nil && strings.TrimSpace(req.ColumnKey) == "done" && len(req.ResolutionRefs) > 0 {
		normalized := "done"
		req.Resolution = &normalized
	}
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

	mutationBoardID := boardID // Canonical-route replay is independent of the card's new board.
	beforeCard, err := loadBoardCardForEvent(r.Context(), opts, boardID, cardKey)
	if err != nil {
		if writeStoreContentionError(w, r, err) {
			return
		}
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", notFoundMessage)
			return
		}
		writeInternalError(w, r, err, "failed to load board card")
		return
	}

	// Bind both route identities and the original optimistic tokens. Every
	// nested write borrows this transaction; keyed lifecycle failures are fatal.
	if boardID == "" {
		boardID = anyString(beforeCard["board_id"])
	}
	status, response, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "cards.move", actorID, req.RequestKey, struct {
		BoardID string
		CardID  string
		Request any
	}{mutationBoardID, cardKey, req}, http.StatusOK, func(ctx context.Context) (map[string]any, error) {
		// Capture lifecycle before-state under the same write lock as the move.
		beforeCard, err := loadBoardCardForEvent(ctx, opts, boardID, cardKey)
		if err != nil {
			return nil, err
		}
		result, err := opts.primitiveStore.MoveBoardCard(ctx, actorID, boardID, cardKey, primitives.MoveBoardCardInput{
			BoardRef: req.BoardRef, Reason: req.Reason,
			ColumnKey:        strings.TrimSpace(req.ColumnKey),
			BeforeCardID:     strings.TrimSpace(req.BeforeCardID),
			AfterCardID:      strings.TrimSpace(req.AfterCardID),
			Resolution:       req.Resolution,
			ResolutionRefs:   &req.ResolutionRefs,
			IfBoardUpdatedAt: &ifBoardUpdatedAt,
			IfWorkVersion:    req.IfWorkVersion,
		})
		if err != nil {
			return nil, err
		}
		emit := func(event map[string]any) error {
			if event == nil {
				return nil
			}
			_, err := opts.primitiveStore.AppendEvent(ctx, actorID, event)
			if strings.TrimSpace(req.RequestKey) != "" || strings.TrimSpace(req.BoardRef) != "" {
				return err
			}
			return nil // Legacy unkeyed column notifications remain best effort.
		}
		movedEvent := buildCardMovedEvent(result.Board, beforeCard, result.Card, req.BeforeCardID, req.AfterCardID, req.BeforeThreadID, req.AfterThreadID)
		if reason := strings.TrimSpace(req.Reason); reason != "" {
			movedEvent["payload"].(map[string]any)["reason"] = reason
		}
		if err := emit(movedEvent); err != nil {
			return nil, err
		}
		if anyString(result.Card["updated_at"]) != anyString(beforeCard["updated_at"]) || anyString(result.Card["version"]) != anyString(beforeCard["version"]) {
			if err := emit(buildCardUpdatedEvent(result.Board, beforeCard, result.Card, []string{"resolution", "resolution_refs"})); err != nil {
				return nil, err
			}
		}
		return map[string]any{"board": result.Board, "card": cardMutationEchoView(ctx, r, opts, result.Card)}, nil
	})
	if err != nil {
		switch {
		case writeStoreContentionError(w, r, err):
		case writePrimitiveQuotaViolationError(w, err):
		case errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidWorkRequest), errors.Is(err, primitives.ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeInternalError(w, r, err, "failed to move board card")
		}
		return
	}

	notifyProjectionMaintainer(opts)
	writeJSON(w, status, response)
}

func handleRemoveBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, identifier string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
		return
	}
	identifier, ok = resolveHTTPResourceID(w, r, opts, "card", identifier, "card")
	if !ok {
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
		case writeStoreContentionError(w, r, err):
		case writePrimitiveQuotaViolationError(w, err):
		case errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeInternalError(w, r, err, "failed to remove board card")
		}
		return
	}

	emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardArchivedEvent(result.Board, result.Card))

	writeJSON(w, http.StatusOK, map[string]any{
		"board":             result.Board,
		"card":              cardMutationEchoView(r.Context(), r, opts, result.Card),
		"removed_thread_id": result.RemovedThreadID,
	})
}

func handleArchiveBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, identifier string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	if strings.TrimSpace(boardID) != "" {
		boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
		if !ok {
			return
		}
	}
	identifier, ok = resolveHTTPResourceID(w, r, opts, "card", identifier, "card")
	if !ok {
		return
	}
	var req struct {
		Reason                string             `json:"reason"`
		ActorID               string             `json:"actor_id"`
		RequestKey            string             `json:"request_key,omitempty"`
		IfLifecycle           map[string]*string `json:"if_lifecycle,omitempty"`
		IfBoardUpdatedAt      *string            `json:"if_board_updated_at"`
		IfWorkVersion         *int64             `json:"if_version"`
		IfLatestObservationID *string            `json:"if_latest_observation_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	var lifecycle *primitives.EventLifecycleBase
	if req.IfLifecycle != nil {
		if _, ok := req.IfLifecycle["archived_at"]; !ok {
			writeError(w, 400, "invalid_request", "if_lifecycle requires archived_at and trashed_at")
			return
		}
		if _, ok := req.IfLifecycle["trashed_at"]; !ok {
			writeError(w, 400, "invalid_request", "if_lifecycle requires archived_at and trashed_at")
			return
		}
		lifecycle = &primitives.EventLifecycleBase{ArchivedAt: req.IfLifecycle["archived_at"], TrashedAt: req.IfLifecycle["trashed_at"]}
	}
	if req.RequestKey != "" && (strings.TrimSpace(req.RequestKey) == "" || len(req.RequestKey) > 256 || strings.ContainsRune(req.RequestKey, 0) || req.IfWorkVersion == nil || req.IfLifecycle == nil || req.IfLatestObservationID == nil) {
		writeError(w, 400, "invalid_request", "keyed archive requires an original key, work version and lifecycle base")
		return
	}
	if req.IfBoardUpdatedAt != nil {
		normalized, ok := normalizeRequiredTimestamp(w, req.IfBoardUpdatedAt, "if_board_updated_at")
		if !ok {
			return
		}
		req.IfBoardUpdatedAt = &normalized
	}
	if req.IfWorkVersion != nil && *req.IfWorkVersion < 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "if_version must be non-negative")
		return
	}
	if req.IfLatestObservationID != nil && strings.TrimSpace(*req.IfLatestObservationID) == "" && req.RequestKey == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "if_latest_observation_id must be nonempty")
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	status, response, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "cards.archive:"+identifier, actorID, req.RequestKey, req, http.StatusOK, func(ctx context.Context) (map[string]any, error) {
		result, err := opts.primitiveStore.ArchiveBoardCard(ctx, actorID, boardID, identifier, primitives.RemoveBoardCardInput{
			Reason:                req.Reason,
			IfLifecycle:           lifecycle,
			IfBoardUpdatedAt:      req.IfBoardUpdatedAt,
			IfWorkVersion:         req.IfWorkVersion,
			IfLatestObservationID: req.IfLatestObservationID,
		})
		if err != nil {
			return nil, err
		}
		event := buildCardArchivedEvent(result.Board, result.Card)
		if payload, ok := event["payload"].(map[string]any); ok {
			payload["reason"] = strings.TrimSpace(req.Reason)
		}
		if strings.TrimSpace(req.Reason) == "" {
			emitCardLifecycleEventBestEffort(ctx, opts, actorID, event)
		}
		return map[string]any{"board": result.Board, "card": cardMutationEchoView(ctx, r, opts, result.Card)}, nil
	})

	if err != nil {
		switch {
		case writeStoreContentionError(w, r, err):
		case writePrimitiveQuotaViolationError(w, err):
		case errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
		case errors.Is(err, primitives.ErrAlreadyTrashed):
			writeError(w, http.StatusConflict, "already_trashed", "card is trashed")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board, work version or latest observation has changed; refresh and retry")
		default:
			writeInternalError(w, r, err, "failed to archive board card")
		}
		return
	}
	writeJSON(w, status, response)
}

func handleTrashBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, identifier string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	if strings.TrimSpace(boardID) != "" {
		boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
		if !ok {
			return
		}
	}
	identifier, ok = resolveHTTPResourceID(w, r, opts, "card", identifier, "card")
	if !ok {
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
		case writeStoreContentionError(w, r, err):
		case writePrimitiveQuotaViolationError(w, err):
		case errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board or card not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeInternalError(w, r, err, "failed to trash board card")
		}
		return
	}
	if result.Card == nil || result.Card["_mutation_applied"] != false {
		emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardTrashedEvent(result.Board, result.Card))
	}
	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "card": cardMutationEchoView(r.Context(), r, opts, result.Card)})
}
