// HTTP handlers for board-scoped card reads and creation: listing cards on a
// board, getting one card, single and batch add. Card update/move/remove live
// in board_card_mutations.go.

package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/schema"
)

const boardCardsBatchMaxItems = 100

func handleListBoardCards(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
		return
	}

	cards, next, ok := readCardPage(w, r, opts, boardID, []string{"active"})
	if !ok {
		return
	}

	var accessible bool
	cards, accessible = filterPlanCards(w, r, opts, cards)
	if !accessible {
		return
	}
	if !enrichPlans(w, r, opts, cards) {
		return
	}
	response := map[string]any{"board_id": boardID, "cards": publicCardsView(cards)}
	if r.URL.Query().Get("summary") == "1" {
		response["next_cursor"] = next
	}
	writeJSON(w, http.StatusOK, response)
}

func handleGetBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID, identifier string) {
	closeRead := func() {}
	if canonical, ok := opts.primitiveStore.(*primitives.Store); ok {
		ctx, finishRead, err := canonical.BeginResourceRead(r.Context())
		if err != nil {
			writeInternalError(w, r, err, "failed to load board card")
			return
		}
		closeRead = finishRead
		defer closeRead()
		r = r.WithContext(ctx)
	}
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
	card, err := opts.primitiveStore.GetBoardCard(r.Context(), boardID, identifier)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "board card not found")
			return
		}
		writeInternalError(w, r, err, "failed to load board card")
		return
	}
	if !enrichPlans(w, r, opts, []map[string]any{card}) {
		return
	}
	closeRead() // Release the WAL snapshot before writing to a potentially slow client.
	writeJSON(w, http.StatusOK, map[string]any{"card": publicCardPayload(card)})
}

func handleAddBoardCard(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
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
	if opts.contract != nil && len(req.ResolutionRefs) > 0 {
		if err := schema.ValidateTypedRefs(opts.contract, req.ResolutionRefs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	if opts.contract != nil && len(req.Refs) > 0 {
		if err := schema.ValidateTypedRefs(opts.contract, req.Refs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	var hygiene markdownHygieneCollector
	if req.Body != "" {
		body := req.Body
		if !hygiene.normalizeField(w, "card.summary", &body) {
			return
		}
		req.Body = strings.TrimSpace(body)
		setAddBoardCardRawSummary(raw, req.Body)
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
		writeInternalError(w, r, err, "failed to load idempotency replay")
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

	input := addBoardCardStoreInput(req)
	if input.CreateSurface == "" {
		input.CreateSurface = idempotencyOp
	}
	if key := strings.TrimSpace(req.RequestKey); key != "" {
		hash, hashErr := hashJSONPayload(replayRequest)
		if hashErr != nil {
			writeInternalError(w, r, hashErr, "failed to hash card create")
			return
		}
		input.CreateReplay = &primitives.CardCreateReplay{Scope: idempotencyOp, Key: key, Hash: hash, Extras: hygiene.attach(map[string]any{})}
	}
	result, err := opts.primitiveStore.CreateBoardCard(r.Context(), actorID, boardID, input)
	if err != nil {
		if writeStoreContentionError(w, r, err) {
			return
		}
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			status, payload, replayed, replayErr := readIdempotencyReplay(r.Context(), opts.primitiveStore, idempotencyOp, actorID, req.RequestKey, replayRequest)
			if writeIdempotencyError(w, replayErr) {
				return
			}
			if replayErr != nil {
				writeInternalError(w, r, replayErr, "failed to load card replay")
				return
			}
			if replayed {
				writeJSON(w, status, normalizeBoardCardMutationReplayPayload(payload))
				return
			}
			existingCard, loadCardErr := loadExistingBoardCardForCreateReplay(r.Context(), opts, boardID, req.CardID, req.ParentThread, req.ThreadID)
			existingBoard, loadBoardErr := opts.primitiveStore.GetBoard(r.Context(), boardID)
			if loadCardErr == nil && loadBoardErr == nil && (req.Handle == "" || anyString(existingCard["handle"]) == req.Handle) && boardCardReplayPreconditionMatches(existingBoard, req.IfBoardUpdatedAt) && boardCardMatchesCreateReplay(
				existingCard,
				explicitReplayCardID,
				req.Title,
				req.Body,
				req.ParentThread,
				req.ThreadID,
				req.ColumnKey,
				req.Assignee,
				req.PinnedDocumentID,
				req.DueAt,
				req.Resolution,
				req.DefinitionOfDone,
				req.ResolutionRefs,
				req.Refs,
				req.Risk,
			) {
				response := hygiene.attach(map[string]any{"board": existingBoard, "card": existingCard})
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
		case errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidWorkRequest), errors.Is(err, primitives.ErrInvalidResourceRef), errors.Is(err, resourceaccess.ErrNULText):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "board, thread, or document not found")
		case errors.Is(err, primitives.ErrCardHandleConflict):
			writeError(w, http.StatusConflict, "conflict", "card handle is already reserved; choose another handle")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board membership already exists or board has changed; refresh and retry")
		default:
			writeInternalError(w, r, err, "failed to add board card")
		}
		return
	}

	storedLifecycle, emitErr := emitCardLifecycleEvent(r.Context(), opts, actorID, buildCardCreatedEvent(result.Board, result.Card))
	if emitErr == nil && storedLifecycle != nil {
		enqueueCardAssigneeWakeBestEffort(r.Context(), opts, actorID, nil, result.Card, result.Board, storedLifecycle)
	}

	// Atomic creates already persisted the replay. Do not take the writer lock again.
	if result.ReplayCommitted {
		writeJSON(w, http.StatusCreated, normalizeBoardCardMutationReplayPayload(hygiene.attach(cardCreationResponse(result.Board, result.Card))))
		return
	}
	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, idempotencyOp, actorID, req.RequestKey, replayRequest, http.StatusCreated, hygiene.attach(cardCreationResponse(result.Board, result.Card)))
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeInternalError(w, r, err, "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, normalizeBoardCardMutationReplayPayload(payload))
}

func handleBatchAddBoardCards(w http.ResponseWriter, r *http.Request, opts handlerOptions, boardID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	boardID, ok = resolveHTTPResourceID(w, r, opts, "board", boardID, "board")
	if !ok {
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

	inputs := make([]primitives.AddBoardCardInput, 0, len(list))
	var hygiene markdownHygieneCollector
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
		if opts.contract != nil && len(m.ResolutionRefs) > 0 {
			if err := schema.ValidateTypedRefs(opts.contract, m.ResolutionRefs); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("items[%d]: %s", i, err.Error()))
				return
			}
		}
		if opts.contract != nil && len(m.Refs) > 0 {
			if err := schema.ValidateTypedRefs(opts.contract, m.Refs); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("items[%d]: %s", i, err.Error()))
				return
			}
		}
		if m.Body != "" {
			body := m.Body
			if !hygiene.normalizeField(w, fmt.Sprintf("items[%d].card.summary", i), &body) {
				return
			}
			m.Body = strings.TrimSpace(body)
			setAddBoardCardRawSummary(itemMap, m.Body)
		}
		if strings.TrimSpace(batchRequestKey) != "" && strings.TrimSpace(m.CardID) == "" {
			m.CardID = deriveRequestScopedID("boards.cards.batch_create", actorID, fmt.Sprintf("%s#%d", batchRequestKey, i), "cd")
		}
		in := addBoardCardStoreInput(m)
		in.IfBoardUpdatedAt = nil
		inputs = append(inputs, in)
	}

	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.cards.batch_add", actorID, batchRequestKey, raw)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeInternalError(w, r, err, "failed to load idempotency replay")
		return
	}
	if replayed {
		writeJSON(w, replayStatus, normalizeBatchBoardCardCreatePayload(replayPayload))
		return
	}

	results, err := opts.primitiveStore.CreateBoardCardsBatch(r.Context(), actorID, boardID, ifBoardTok, inputs)
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
			writeError(w, http.StatusNotFound, "not_found", "board, thread, or document not found")
		case errors.Is(err, primitives.ErrCardHandleConflict):
			writeError(w, http.StatusConflict, "conflict", "card handle is already reserved; choose another handle")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "board membership already exists or board has changed; refresh and retry")
		default:
			writeInternalError(w, r, err, "failed to create board cards batch")
		}
		return
	}

	for _, res := range results {
		storedLifecycle, emitErr := emitCardLifecycleEvent(r.Context(), opts, actorID, buildCardCreatedEvent(res.Board, res.Card))
		if emitErr == nil && storedLifecycle != nil {
			enqueueCardAssigneeWakeBestEffort(r.Context(), opts, actorID, nil, res.Card, res.Board, storedLifecycle)
		}
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
	response = hygiene.attach(response)
	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "boards.cards.batch_add", actorID, batchRequestKey, raw, http.StatusCreated, response)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeInternalError(w, r, err, "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, normalizeBatchBoardCardCreatePayload(payload))
}
