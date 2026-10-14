package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// handleCreateAskBatch files 2..10 related human_attention_requested events as
// one atomic batch. Every member passes the same validation as POST /events,
// all members must share one backing thread, and the server assigns the shared
// batch id so the human inbox derives a single decision item.
func handleCreateAskBatch(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}
	batchStore, ok := opts.primitiveStore.(interface {
		AppendTaskAttentionEventBatch(context.Context, string, string, string, []map[string]any) (map[string]any, bool, error)
	})
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "task attention batch store is not configured")
		return
	}

	var body struct {
		ActorID    string           `json:"actor_id"`
		RequestKey string           `json:"request_key"`
		Asks       []map[string]any `json:"asks"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.RequestKey) != "" {
		if len(body.RequestKey) > 128 || strings.TrimSpace(body.RequestKey) != body.RequestKey {
			writeError(w, http.StatusBadRequest, "invalid_request", "request_key must be at most 128 characters without surrounding whitespace")
			return
		}
	}
	if len(body.Asks) < primitives.MinAskBatchMembers || len(body.Asks) > primitives.MaxAskBatchMembers {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"asks must contain between "+strconv.Itoa(primitives.MinAskBatchMembers)+" and "+strconv.Itoa(primitives.MaxAskBatchMembers)+" human_attention_requested events")
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, body.ActorID)
	if !ok {
		return
	}
	requestPayload := map[string]any{"actor_id": body.ActorID, "request_key": body.RequestKey, "asks": body.Asks}
	requestHash := ""
	if strings.TrimSpace(body.RequestKey) != "" {
		var err error
		if requestHash, err = hashJSONPayload(requestPayload); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to hash ask batch")
			return
		}
	}
	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "asks.batch.create", actorID, body.RequestKey, requestPayload)
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

	sharedThread := ""
	sharedKind := ""
	members := make([]map[string]any, 0, len(body.Asks))
	for index, rawEvent := range body.Asks {
		event, ok := validatedAskBatchMember(w, r, opts, actorID, rawEvent, index)
		if !ok {
			return
		}
		threadID := strings.TrimSpace(anyString(event["thread_id"]))
		if sharedThread == "" {
			sharedThread = threadID
		} else if threadID != sharedThread {
			writeError(w, http.StatusBadRequest, "invalid_request", "every ask in a batch must share one thread_id")
			return
		}
		kind := canonicalHumanAttentionKind(anyString(event["payload"].(map[string]any)["kind"]))
		if sharedKind == "" {
			sharedKind = kind
		} else if kind != sharedKind {
			writeError(w, http.StatusBadRequest, "invalid_request", "every ask in a batch must share one kind")
			return
		}
		members = append(members, event)
	}

	// The filing replay row commits inside the store transaction, so an exact
	// retry after a crash or a concurrent same-key create replays the original
	// batch instead of filing a second one.
	response, replayed, err := batchStore.AppendTaskAttentionEventBatch(r.Context(), actorID, body.RequestKey, requestHash, members)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded) {
			writeError(w, http.StatusConflict, "conflict", "human attention request is already resolved")
			return
		}
		if errors.Is(err, primitives.ErrInvalidWorkRequest) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "event already exists")
			return
		}
		// A subject (or thread ref) the caller cannot resolve is absent, the
		// same answer a single ask gives; the body never names the private id.
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "human attention request not found")
			return
		}
		log.Printf("append ask batch failed: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to append ask batch")
		return
	}
	if replayed {
		writeJSON(w, http.StatusCreated, response)
		return
	}
	notifyProjectionMaintainer(opts)
	if opts.projectionMaintainer != nil {
		if err := opts.projectionMaintainer.RefreshThread(r.Context(), sharedThread, time.Now().UTC()); err != nil {
			log.Printf("best-effort ask batch projection refresh failed (thread=%s): %v", sharedThread, err)
		}
	}
	writeJSON(w, http.StatusCreated, response)
}

// validatedAskBatchMember applies the per-ask validation POST /events applies
// to human_attention_requested events: hygiene, title rules, reference and
// provenance conventions, requester identity, and no client-supplied batch id.
// It writes the error response itself and returns false when validation failed.
func validatedAskBatchMember(w http.ResponseWriter, r *http.Request, opts handlerOptions, actorID string, rawEvent map[string]any, index int) (map[string]any, bool) {
	invalid := func(message string) (map[string]any, bool) {
		writeError(w, http.StatusBadRequest, "invalid_request", message)
		return nil, false
	}
	label := "asks[" + strconv.Itoa(index) + "]"
	if rawEvent == nil {
		return invalid(label + " event is required")
	}
	eventType, _ := rawEvent["type"].(string)
	if strings.TrimSpace(eventType) != humanAttentionRequestedEventType {
		return invalid(label + ".type must be " + humanAttentionRequestedEventType)
	}
	if _, ok := rawEvent["summary"].(string); !ok {
		return invalid(label + ".summary is required")
	}
	refs, err := extractStringSlice(rawEvent["refs"])
	if err != nil {
		return invalid(label + ".refs must be a list of strings")
	}
	if err := schema.ValidateTypedRefs(opts.contract, refs); err != nil {
		return invalid(label + ".refs: " + err.Error())
	}
	provenance, ok := rawEvent["provenance"].(map[string]any)
	if !ok {
		return invalid(label + ".provenance is required")
	}
	if err := schema.ValidateProvenance(opts.contract, provenance); err != nil {
		return invalid(label + ".provenance: " + err.Error())
	}
	payload, _ := rawEvent["payload"].(map[string]any)
	if payload == nil {
		return invalid(label + ".payload must be an object")
	}
	if strings.TrimSpace(anyString(payload["batch_id"])) != "" {
		return invalid(label + ".payload.batch_id is assigned by the server")
	}
	if code, message := validateAskText(payload); code != "" {
		writeError(w, http.StatusBadRequest, code, label+": "+message)
		return nil, false
	}
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeMapString(w, label+".payload.body", payload, "body") {
		return nil, false
	}
	if err := validateEventReferenceConventions(opts.contract, rawEvent, refs); err != nil {
		return invalid(label + ": " + err.Error())
	}
	if principal, ok := cachedAuthenticatedPrincipal(r); ok && isAgentPrincipal(principal) && anyString(payload["requester_actor_id"]) != principal.ActorID {
		writeError(w, http.StatusForbidden, "forbidden", "requester must be the authenticated agent")
		return nil, false
	}
	if canonicalHumanAttentionKind(anyString(payload["kind"])) == "" {
		return invalid(label + ".payload.kind must be ask, review, or escalate")
	}
	return rawEvent, true
}
