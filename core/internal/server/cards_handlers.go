package server

import (
	"errors"
	"net/http"
	"strings"

	"organization-autorunner-core/internal/primitives"
)

func handleListCards(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	query := r.URL.Query()
	archivedOnly := strings.TrimSpace(query.Get("archived_only")) == "true" ||
		strings.TrimSpace(query.Get("tombstoned_only")) == "true"
	cards, err := opts.primitiveStore.ListCards(r.Context(), primitives.CardListFilter{ArchivedOnly: archivedOnly})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list cards")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

func handleGetCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	handleGetBoardCard(w, r, opts, "", cardID)
}

func handlePatchCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
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
	if req.Patch == nil || len(req.Patch) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
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

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	patchInput, changedFields, ok := parseBoardCardPatchInput(w, req.Patch)
	if !ok {
		return
	}
	patchInput.IfBoardUpdatedAt = &ifUpdatedAt

	beforeCard, err := loadBoardCardForEvent(r.Context(), opts, "", cardID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "card not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load card")
		return
	}

	result, err := opts.primitiveStore.UpdateBoardCard(r.Context(), actorID, "", cardID, patchInput)
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "card not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "card has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to patch card")
		}
		return
	}

	if anyString(result.Card["updated_at"]) != anyString(beforeCard["updated_at"]) || anyString(result.Card["version"]) != anyString(beforeCard["version"]) {
		emitCardLifecycleEventBestEffort(r.Context(), opts, actorID, buildCardUpdatedEvent(result.Board, beforeCard, result.Card, changedFields))
		emitBoardLifecycleEventBestEffort(r.Context(), opts, actorID, buildBoardCardUpdatedEvent(result.Board, beforeCard, result.Card, changedFields))
	}

	writeJSON(w, http.StatusOK, map[string]any{"card": result.Card})
}

func handleMoveCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	handleMoveCardMutation(w, r, opts, "", cardID, "card not found")
}

func handleArchiveCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	handleArchiveBoardCard(w, r, opts, "", cardID)
}

func writeBoardCardPurgeStoreError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "card not found")
		return true
	case errors.Is(err, primitives.ErrNotArchived):
		writeError(w, http.StatusConflict, "not_archived", "card is not archived")
		return true
	case errors.Is(err, primitives.ErrInvalidBoardRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return true
	default:
		return false
	}
}

func handleRestoreArchivedCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
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
	result, err := opts.primitiveStore.RestoreArchivedBoardCard(r.Context(), actorID, "", cardID, primitives.RemoveBoardCardInput{
		IfBoardUpdatedAt: req.IfBoardUpdatedAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "card not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board has been updated; refresh and retry")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to restore card")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "card": result.Card})
}

func handlePurgeArchivedCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	principal, ok := resolveOptionalPrincipal(w, r, opts)
	if !ok {
		return
	}

	if principal != nil {
		if !isHumanPrincipal(principal) {
			writeError(w, http.StatusForbidden, "human_only", "only human principals may permanently delete cards")
			return
		}
	} else {
		if !opts.allowUnauthenticatedWrites || !opts.enableDevActorMode {
			writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
			return
		}
		if opts.actorRegistry == nil {
			writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
			return
		}
		var req struct {
			ActorID string `json:"actor_id"`
		}
		if !decodeJSONBodyAllowEmpty(w, r, &req) {
			return
		}
		actorID := strings.TrimSpace(req.ActorID)
		if actorID == "" {
			writeError(w, http.StatusForbidden, "human_only", "only human principals may permanently delete cards; in development, include actor_id for an actor tagged `human` in the JSON body")
			return
		}
		registeredID, ok := requireRegisteredActorID(w, r, opts.actorRegistry, actorID)
		if !ok {
			return
		}
		if !actorRegistryActorHasHumanTag(r.Context(), opts.actorRegistry, registeredID) {
			writeError(w, http.StatusForbidden, "human_only", "only human-tagged actors may purge without authenticated passkey credentials")
			return
		}
	}

	if err := opts.primitiveStore.PurgeArchivedBoardCard(r.Context(), "", cardID); err != nil {
		if writeBoardCardPurgeStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to purge card")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"purged": true, "card_id": cardID})
}
