package server

import (
	"errors"
	"net/http"
	"strings"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

func handleListCards(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	query := r.URL.Query()
	trashedOnly := strings.TrimSpace(query.Get("trashed_only")) == "true"
	archivedOnly := strings.TrimSpace(query.Get("archived_only")) == "true"
	includeArchived := strings.TrimSpace(query.Get("include_archived")) == "true"
	includeTrashed := strings.TrimSpace(query.Get("include_trashed")) == "true"
	listFilter := primitives.CardListFilter{
		IncludeArchived: includeArchived,
		IncludeTrashed:  includeTrashed,
	}
	if trashedOnly {
		listFilter.TrashedOnly = true
	} else if archivedOnly {
		listFilter.ArchivedOnly = true
	}
	cards, err := opts.primitiveStore.ListCards(r.Context(), listFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list cards")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"cards": publicCardsView(cards)})
}

func handleCreateCardGlobal(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var raw map[string]any
	if !decodeJSONBody(w, r, &raw) {
		return
	}
	boardID, ok := resolveBoardIDForGlobalCardCreate(w, raw, opts.contract)
	if !ok {
		return
	}
	addBoardCardFromRaw(w, r, opts, boardID, raw, "cards.create")
}

func resolveBoardIDForGlobalCardCreate(w http.ResponseWriter, raw map[string]any, contract *schema.Contract) (string, bool) {
	if raw == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "body is required")
		return "", false
	}
	boardID := strings.TrimSpace(anyString(raw["board_id"]))
	refRaw := raw["board_ref"]
	refStr := strings.TrimSpace(anyString(refRaw))
	if refStr == "" && refRaw != nil {
		if m, ok := refRaw.(map[string]any); ok {
			refStr = strings.TrimSpace(anyString(m["ref"]))
			if refStr == "" {
				suffix := strings.TrimSpace(anyString(m["value"]))
				prefix := strings.TrimSpace(anyString(m["prefix"]))
				if prefix != "" && suffix != "" {
					refStr = prefix + ":" + suffix
				}
			}
		}
	}
	if boardID != "" && refStr != "" {
		prefix, suffix, err := schema.SplitTypedRef(refStr)
		if err == nil && prefix == "board" && strings.TrimSpace(suffix) != "" && strings.TrimSpace(suffix) != boardID {
			writeError(w, http.StatusBadRequest, "invalid_request", "board_id and board_ref disagree")
			return "", false
		}
	}
	if boardID != "" {
		return boardID, true
	}
	if refStr == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "board_id or board_ref is required for POST /cards")
		return "", false
	}
	prefix, id, err := schema.SplitTypedRef(refStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "board_ref must be a typed ref (board:<id>)")
		return "", false
	}
	if strings.TrimSpace(prefix) != "board" {
		writeError(w, http.StatusBadRequest, "invalid_request", "board_ref must use board: prefix")
		return "", false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "board_ref must be board:<id>")
		return "", false
	}
	if contract != nil {
		if err := schema.ValidateTypedRefs(contract, []string{refStr}); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return "", false
		}
	}
	return id, true
}

func handleGetCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	handleGetBoardCard(w, r, opts, "", cardID)
}

func handleGetCardTimeline(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	card, err := opts.primitiveStore.GetBoardCard(r.Context(), "", cardID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "card not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load card")
		return
	}

	threadID := strings.TrimSpace(anyString(card["thread_id"]))
	if threadID == "" {
		writeError(w, http.StatusInternalServerError, "internal_error", "card missing thread id")
		return
	}

	exp, err := expandThreadTimeline(r.Context(), opts, threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load card timeline")
		return
	}

	cardIDs := map[string]struct{}{strings.TrimSpace(cardID): {}}
	threadIDs := map[string]struct{}{threadID: {}}
	for _, event := range exp.Events {
		refs, err := extractStringSlice(event["refs"])
		if err != nil {
			continue
		}
		for _, ref := range refs {
			prefix, id, err := schema.SplitTypedRef(ref)
			if err != nil {
				continue
			}
			switch prefix {
			case "card":
				if strings.TrimSpace(id) != "" {
					cardIDs[strings.TrimSpace(id)] = struct{}{}
				}
			case "thread":
				if strings.TrimSpace(id) != "" {
					threadIDs[strings.TrimSpace(id)] = struct{}{}
				}
			}
		}
	}

	cards := make([]map[string]any, 0, len(cardIDs))
	for id := range cardIDs {
		loaded, err := opts.primitiveStore.GetBoardCard(r.Context(), "", id)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load related cards")
			return
		}
		cards = append(cards, publicCardView(loaded))
	}
	cards = dedupeAndSortResourceMaps(cards)

	threads := make([]map[string]any, 0, len(threadIDs))
	for id := range threadIDs {
		thread, err := opts.primitiveStore.GetThread(r.Context(), id)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load related threads")
			return
		}
		threads = append(threads, thread)
	}
	threads = dedupeAndSortResourceMaps(threads)

	writeJSON(w, http.StatusOK, map[string]any{
		"card":      publicCardView(card),
		"events":    exp.Events,
		"artifacts": mapsByIDToSortedSlice(exp.Artifacts),
		"cards":     cards,
		"documents": mapsByIDToSortedSlice(exp.Documents),
		"threads":   threads,
	})
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
	}

	writeJSON(w, http.StatusOK, map[string]any{"card": publicCardView(result.Card)})
}

func handleMoveCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	handleMoveCardMutation(w, r, opts, "", cardID, "card not found")
}

func handleArchiveCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	handleArchiveBoardCard(w, r, opts, "", cardID)
}

func handleTrashCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID string) {
	handleTrashBoardCard(w, r, opts, "", cardID)
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

	writeJSON(w, http.StatusOK, map[string]any{"board": result.Board, "card": publicCardView(result.Card)})
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
