// HTTP handlers for the event primitive: append (with generic append
// protection), get, and the archive/unarchive/trash/restore lifecycle path.

package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func handleAppendEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
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
		Event      map[string]any `json:"event"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	if req.Event == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "event is required")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	if strings.TrimSpace(req.RequestKey) != "" && firstNonEmptyString(req.Event["id"]) == "" {
		req.Event["id"] = deriveRequestScopedID("events.create", actorID, req.RequestKey, "ev")
	}
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, "event.summary", req.Event, "summary") {
		return
	}
	if strings.TrimSpace(anyString(req.Event["type"])) == "message_posted" {
		if payload, ok := req.Event["payload"].(map[string]any); ok {
			if !hygiene.normalizeMapString(w, "event.payload.text", payload, "text") {
				return
			}
		}
	}
	if strings.TrimSpace(anyString(req.Event["type"])) == "human_attention_requested" {
		if payload, ok := req.Event["payload"].(map[string]any); ok {
			if !hygiene.normalizeMapString(w, "event.payload.body", payload, "body") {
				return
			}
		}
	}
	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, req.RequestKey, req)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		if writeEventAppendContentionError(w, r, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load idempotency replay")
		return
	}
	if replayed {
		writeJSON(w, replayStatus, replayPayload)
		return
	}
	typeValue, ok := req.Event["type"].(string)
	typeValue = strings.TrimSpace(typeValue)
	if !ok || typeValue == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "event.type is required")
		return
	}
	if isGenericAppendProtectedEventType(typeValue) && typeValue != humanAttentionWithdrawnEventType {
		writeError(w, http.StatusForbidden, "protected_event_type", typeValue+" must be created through its dedicated handler")
		return
	}
	req.Event["type"] = typeValue

	if err := schema.ValidateEnum(opts.contract, "event_type", typeValue); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if _, ok := req.Event["summary"].(string); !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "event.summary is required")
		return
	}

	refs, err := extractStringSlice(req.Event["refs"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "event.refs must be a list of strings")
		return
	}

	if err := schema.ValidateTypedRefs(opts.contract, refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	provenance, ok := req.Event["provenance"].(map[string]any)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request", "event.provenance is required")
		return
	}

	if err := schema.ValidateProvenance(opts.contract, provenance); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if typeValue == primitives.AskRevisedEventType || typeValue == primitives.AskRetiredEventType || typeValue == primitives.AskReopenedEventType || typeValue == primitives.AskRetirementAuditedEventType {
		writeError(w, 400, "invalid_request", "ask curation events must use POST /asks/{ask_id}/revise or /retire")
		return
	}
	if typeValue == "run_recorded" {
		if err := primitives.ValidateRunRecorded(req.Event); err != nil {
			writeError(w, 400, "invalid_request", err.Error())
			return
		}
	}
	if typeValue == humanAttentionRequestedEventType {
		payload, _ := req.Event["payload"].(map[string]any)
		if code, message := validateAskText(payload); code != "" {
			writeError(w, http.StatusBadRequest, code, message)
			return
		}
		// Batch membership is assigned by the dedicated ask-batch handler; a
		// client-supplied id would let a single ask join (or forge) someone
		// else's decision item.
		if payload != nil && strings.TrimSpace(anyString(payload["batch_id"])) != "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "event.payload.batch_id is assigned by the ask batch handler; file through POST /asks/batch")
			return
		}
	}
	if err := validateEventReferenceConventions(opts.contract, req.Event, refs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if typeValue == humanAttentionRequestedEventType {
		payload, _ := req.Event["payload"].(map[string]any)
		if principal, ok := cachedAuthenticatedPrincipal(r); ok && isAgentPrincipal(principal) && anyString(payload["requester_actor_id"]) != principal.ActorID {
			writeError(w, 403, "forbidden", "requester must be the authenticated agent")
			return
		}

	}

	var stored map[string]any
	var atomicReplay *primitives.IdempotencyReplay
	atomicEvent := typeValue == "message_posted" && strings.TrimSpace(req.RequestKey) != ""
	if typeValue == humanAttentionWithdrawnEventType {
		principal, ok := requireAuthenticatedPrincipal(w, r, opts)
		if !ok {
			return
		}
		if !isAgentPrincipal(principal) {
			writeError(w, http.StatusForbidden, "agent_required", "only the requesting agent may withdraw an ask")
			return
		}
		payload, _ := req.Event["payload"].(map[string]any)
		reason := strings.TrimSpace(anyString(payload["reason"]))
		if reason == "" || len(reason) > 500 {
			writeError(w, http.StatusBadRequest, "invalid_request", "event.payload.reason must contain 1 to 500 characters")
			return
		}
		requestEventID := strings.TrimSpace(anyString(payload["request_event_id"]))
		if requestEventID == "" {
			requestEventRef := strings.TrimSpace(anyString(payload["request_event_ref"]))
			if strings.HasPrefix(requestEventRef, "event:") {
				requestEventID = strings.TrimPrefix(requestEventRef, "event:")
			}
		}
		requestEventRef := strings.TrimSpace(anyString(payload["request_event_ref"]))
		if requestEventID == "" || requestEventRef != "event:"+requestEventID {
			writeError(w, http.StatusBadRequest, "invalid_request", "request_event_id and request_event_ref must identify the same ask event")
			return
		}
		withdrawalStore, ok := opts.primitiveStore.(interface {
			AppendHumanAttentionWithdrawal(context.Context, string, string, map[string]any) (map[string]any, error)
		})
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "human attention withdrawal store is not configured")
			return
		}
		stored, err = withdrawalStore.AppendHumanAttentionWithdrawal(r.Context(), actorID, requestEventID, req.Event)
	} else if typeValue == humanAttentionRequestedEventType {
		store, ok := opts.primitiveStore.(interface {
			AppendTaskAttentionEvent(context.Context, string, map[string]any) (map[string]any, error)
		})
		if !ok {
			writeError(w, 503, "primitives_unavailable", "task attention store is not configured")
			return
		}
		stored, err = store.AppendTaskAttentionEvent(r.Context(), actorID, req.Event)
	} else if atomicEvent {
		store, ok := opts.primitiveStore.(interface {
			AppendEventWithReplay(context.Context, string, string, string, map[string]any, map[string]any) (primitives.IdempotencyReplay, bool, error)
		})
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "atomic event replay store is not configured")
			return
		}
		var hash string
		hash, err = hashJSONPayload(req)
		if err == nil {
			var replay primitives.IdempotencyReplay
			var replayed bool
			replay, replayed, err = store.AppendEventWithReplay(r.Context(), actorID, strings.TrimSpace(req.RequestKey), hash, req.Event, hygiene.attach(map[string]any{}))
			if err == nil {
				if replayed {
					writeJSON(w, replay.Status, replay.Response)
					return
				}
				atomicReplay = &replay
				stored, _ = replay.Response["event"].(map[string]any)
			}
		}
	} else {
		stored, err = opts.primitiveStore.AppendEvent(r.Context(), actorID, req.Event)
	}
	if err != nil {
		if writeEventAppendContentionError(w, r, err) {
			return
		}
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded) {
			writeError(w, http.StatusConflict, "conflict", "human attention request is already resolved")
			return
		}
		if errors.Is(err, primitives.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden", "only the requesting agent may withdraw this ask")
			return
		}
		if errors.Is(err, primitives.ErrInvalidWorkRequest) {
			writeError(w, 400, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, primitives.ErrNotFound) && (typeValue == humanAttentionWithdrawnEventType || typeValue == humanAttentionRequestedEventType) {
			writeError(w, http.StatusNotFound, "not_found", "human attention request not found")
			return
		}
		if !atomicEvent && errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			eventID := firstNonEmptyString(req.Event["id"])
			existing, loadErr := opts.primitiveStore.GetEvent(r.Context(), eventID)
			if loadErr == nil {
				response := map[string]any{"event": existing}
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, req.RequestKey, req, http.StatusCreated, hygiene.attach(response))
				if writeIdempotencyError(w, replayErr) {
					return
				}
				if replayErr != nil {
					if writeEventAppendContentionError(w, r, replayErr) {
						return
					}
					writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
					return
				}
				writeJSON(w, status, payload)
				return
			}
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "event already exists")
			return
		}
		log.Printf("append event failed: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to append event")
		return
	}
	threadID := anyString(stored["thread_id"])
	notifyProjectionMaintainer(opts)
	if opts.pmRuntime != nil {
		if store, ok := opts.primitiveStore.(*primitives.Store); ok {
			if err := opts.pmRuntime.SyncBridgeReply(r.Context(), store, stored); err != nil {
				log.Printf("pm bridge reply sync failed (event=%s): %v", anyString(stored["id"]), err)
			}
		}
	}
	if (strings.TrimSpace(anyString(stored["type"])) == humanAttentionRequestedEventType || strings.TrimSpace(anyString(stored["type"])) == humanAttentionWithdrawnEventType) && opts.projectionMaintainer != nil {
		if err := opts.projectionMaintainer.RefreshThread(r.Context(), threadID, time.Now().UTC()); err != nil {
			log.Printf("best-effort human inbox projection refresh failed (thread=%s): %v", threadID, err)
		}
	}

	if atomicReplay != nil {
		writeJSON(w, atomicReplay.Status, atomicReplay.Response)
		return
	}

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, req.RequestKey, req, http.StatusCreated, hygiene.attach(map[string]any{"event": stored}))
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		if writeEventAppendContentionError(w, r, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, payload)
}

func isGenericAppendProtectedEventType(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case humanAttentionRespondedEventType, humanAttentionWithdrawnEventType, "attention_feedback",
		// Lease audit events are written only with the card_leases row.
		"card_lease_acquired", "card_lease_renewed", "card_lease_released", "card_lease_expired":
		return true
	default:
		return false
	}
}

func handleGetEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, eventID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	resolvedID, ok := resolveHTTPResourceID(w, r, opts, "event", eventID, "event")
	if !ok {
		return
	}

	event, err := opts.primitiveStore.GetEvent(r.Context(), resolvedID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "event not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load event")
		return
	}
	if !requireAccessibleEvent(w, r, opts, event) {
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"event": event})
}

func handleArchiveEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, id string) {
	handleEventLifecycle(w, r, opts, id, "archive")
}
func handleUnarchiveEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, id string) {
	handleEventLifecycle(w, r, opts, id, "unarchive")
}
func handleTrashEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, id string) {
	handleEventLifecycle(w, r, opts, id, "trash")
}
func handleRestoreEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, id string) {
	handleEventLifecycle(w, r, opts, id, "restore")
}

func handleEventLifecycle(w http.ResponseWriter, r *http.Request, opts handlerOptions, eventID, action string) {
	if opts.primitiveStore == nil {
		writeError(w, 503, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID       string                        `json:"actor_id"`
		RequestKey    string                        `json:"request_key"`
		Reason        string                        `json:"reason"`
		IfLifecycle   map[string]*string            `json:"if_lifecycle"`
		SelectedBases map[string]map[string]*string `json:"selected_bases,omitempty"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	var base *primitives.EventLifecycleBase
	if req.IfLifecycle != nil {
		archived, a := req.IfLifecycle["archived_at"]
		trashed, t := req.IfLifecycle["trashed_at"]
		if !a || !t {
			writeError(w, 400, "invalid_request", "if_lifecycle requires archived_at and trashed_at")
			return
		}
		base = &primitives.EventLifecycleBase{ArchivedAt: archived, TrashedAt: trashed}
	} else if strings.TrimSpace(req.RequestKey) != "" {
		writeError(w, 400, "invalid_request", "if_lifecycle is required for keyed event requests")
		return
	}
	if len(req.SelectedBases) > 2000 {
		writeError(w, 400, "invalid_request", "too many selected event bases")
		return
	}
	selectedBases := make(map[string]primitives.EventLifecycleBase, len(req.SelectedBases))
	for id, pair := range req.SelectedBases {
		archived, a := pair["archived_at"]
		trashed, t := pair["trashed_at"]
		if id == "" || !a || !t {
			writeError(w, 400, "invalid_request", "selected_bases requires event IDs and both lifecycle fields")
			return
		}
		selectedBases[id] = primitives.EventLifecycleBase{ArchivedAt: archived, TrashedAt: trashed}
	}
	actor, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	// Fresh scoped visibility before receipt access; the writer also rechecks.
	event, err := opts.primitiveStore.GetEvent(r.Context(), eventID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, 404, "not_found", "event not found")
		} else {
			writeError(w, 500, "internal_error", "failed to load event")
		}
		return
	}
	if !requireAccessibleEvent(w, r, opts, event) {
		return
	}
	status, response, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "events."+action, actor, req.RequestKey, struct {
		ID   string
		Body any
	}{eventID, req}, 200, func(ctx context.Context) (map[string]any, error) {
		var result map[string]any
		var affected []string
		var err error
		if store, ok := opts.primitiveStore.(interface {
			ApplyEventLifecycleSelection(context.Context, string, string, string, string, *primitives.EventLifecycleBase, map[string]primitives.EventLifecycleBase) (map[string]any, []string, error)
		}); ok {
			result, affected, err = store.ApplyEventLifecycleSelection(ctx, actor, eventID, action, req.Reason, base, selectedBases)
		} else {
			if base != nil || len(selectedBases) > 0 {
				return nil, errors.New("atomic lifecycle base checking is unavailable")
			}
			switch action {
			case "archive":
				result, err = opts.primitiveStore.ArchiveEvent(ctx, actor, eventID)
			case "unarchive":
				result, err = opts.primitiveStore.UnarchiveEvent(ctx, actor, eventID)
			case "trash":
				result, err = opts.primitiveStore.TrashEvent(ctx, actor, eventID, req.Reason)
			case "restore":
				result, err = opts.primitiveStore.RestoreEvent(ctx, actor, eventID)
			}
		}
		response := map[string]any{"event": result}
		if affected != nil && strings.TrimSpace(req.RequestKey) != "" {
			response["affected_event_ids"] = affected
		}
		return response, err
	})
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		switch {
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, 404, "not_found", "event not found")
		case errors.Is(err, primitives.ErrAlreadyTrashed):
			writeError(w, 409, "already_trashed", "event is trashed")
		case errors.Is(err, primitives.ErrNotArchived):
			writeError(w, 409, "not_archived", "event is not archived")
		case errors.Is(err, primitives.ErrNotTrashed):
			writeError(w, 409, "not_trashed", "event is not trashed")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, 409, "conflict", "event visibility changed or reply cascade exceeds its limit")
		case errors.Is(err, primitives.ErrInvalidDocumentRequest):
			writeError(w, 400, "invalid_request", "invalid event request")
		default:
			writeError(w, 500, "internal_error", "failed to change event visibility")
		}
		return
	}
	writeJSON(w, status, response)
}
