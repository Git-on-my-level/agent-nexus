package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
	"agent-nexus-core/internal/schema"
)

type humanAttentionResponseStore interface {
	HumanAttentionResponseRequest(context.Context, string) (map[string]any, error)
	HumanAttentionResponseReplay(context.Context, string, string, string) (map[string]any, error)
	HumanAttentionResponseClaimed(context.Context, string) (bool, error)
	AppendHumanAttentionResponse(context.Context, string, string, string, string, string, map[string]any, map[string]any, ...map[string]any) (map[string]any, bool, error)
}

type inboxRespondAnswer struct {
	AskRef       string   `json:"ask_ref"`
	ResponseText string   `json:"response_text"`
	Outcome      string   `json:"outcome"`
	RelatedRefs  []string `json:"related_refs"`
}

type inboxRespondRequest struct {
	IdempotencyKey      string               `json:"idempotency_key"`
	ActorID             string               `json:"actor_id"`
	InboxItemID         string               `json:"inbox_item_id"`
	ResponseText        string               `json:"response_text"`
	Outcome             string               `json:"outcome"`
	RelatedRefs         []string             `json:"related_refs"`
	NotifyMode          string               `json:"notify_mode"`
	NotifyTargetActorID string               `json:"notify_target_actor_id"`
	NotifyTargetAgentID string               `json:"notify_target_agent_id"`
	Answers             []inboxRespondAnswer `json:"answers"`
}

// inboxRespondRequestCore preserves the exact field order single-ask response
// requests have always hashed, so replays of requests sent before batch
// answers existed still match.
type inboxRespondRequestCore struct {
	IdempotencyKey      string   `json:"idempotency_key"`
	ActorID             string   `json:"actor_id"`
	InboxItemID         string   `json:"inbox_item_id"`
	ResponseText        string   `json:"response_text"`
	Outcome             string   `json:"outcome"`
	RelatedRefs         []string `json:"related_refs"`
	NotifyMode          string   `json:"notify_mode"`
	NotifyTargetActorID string   `json:"notify_target_actor_id"`
	NotifyTargetAgentID string   `json:"notify_target_agent_id"`
}

func (r inboxRespondRequest) core() inboxRespondRequestCore {
	return inboxRespondRequestCore{
		IdempotencyKey:      r.IdempotencyKey,
		ActorID:             r.ActorID,
		InboxItemID:         r.InboxItemID,
		ResponseText:        r.ResponseText,
		Outcome:             r.Outcome,
		RelatedRefs:         r.RelatedRefs,
		NotifyMode:          r.NotifyMode,
		NotifyTargetActorID: r.NotifyTargetActorID,
		NotifyTargetAgentID: r.NotifyTargetAgentID,
	}
}

func handleRespondInboxItem(w http.ResponseWriter, r *http.Request, opts handlerOptions, pathInboxItemID string) {
	if _, ok := requireHumanPrincipal(w, r, opts); !ok {
		return
	}
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}
	responseStore, ok := opts.primitiveStore.(humanAttentionResponseStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "human attention response store is not configured")
		return
	}

	var req inboxRespondRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	pathInboxItemID = strings.TrimSpace(pathInboxItemID)
	bodyItemID := strings.TrimSpace(req.InboxItemID)
	effectiveItemID := bodyItemID
	if pathInboxItemID != "" {
		if bodyItemID != "" && bodyItemID != pathInboxItemID {
			writeError(w, http.StatusBadRequest, "invalid_request", "inbox_item_id must match path inbox_id")
			return
		}
		effectiveItemID = pathInboxItemID
	}
	if effectiveItemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox_item_id is required (body or path)")
		return
	}

	// A batch item answers every member in one atomic pass. Intercept before
	// the single-ask validations, which require flat response fields.
	if respondInboxBatchIfNeeded(w, r, opts, effectiveItemID, actorID, req) {
		return
	}

	responseText := strings.TrimSpace(req.ResponseText)
	if err := schema.ValidateEnum(opts.contract, "human_attention_response_outcome", req.Outcome); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if responseText == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "response_text is required")
		return
	}
	var hygiene markdownHygieneCollector
	if !hygiene.normalizeField(w, "response_text", &responseText) {
		return
	}
	responseText = strings.TrimSpace(responseText)
	req.ResponseText = responseText
	if len(req.IdempotencyKey) > 128 || strings.TrimSpace(req.IdempotencyKey) != req.IdempotencyKey {
		writeError(w, http.StatusBadRequest, "invalid_request", "idempotency_key must be at most 128 characters without surrounding whitespace")
		return
	}
	hashInput, err := json.Marshal(struct {
		ActorID     string `json:"actor_id"`
		InboxItemID string `json:"inbox_item_id"`
		Request     any    `json:"request"`
	}{actorID, effectiveItemID, req.core()})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to hash human attention response")
		return
	}
	digest := sha256.Sum256(hashInput)
	requestHash := hex.EncodeToString(digest[:])
	// Decode and validate the body before reserving answer priority. A stalled
	// upload must never hold the finite-read lane away from other readers.
	if opts.readAdmission != nil {
		release := opts.readAdmission.answer()
		defer release()
	}
	if !authorizeInboxResponseAccess(w, r, opts, responseStore, effectiveItemID) {
		return
	}
	if req.IdempotencyKey != "" {
		replay, replayErr := responseStore.HumanAttentionResponseReplay(r.Context(), actorID, req.IdempotencyKey, requestHash)
		if replayErr == nil {
			writeAuthorizedInboxResponseReplay(w, r, opts, replay)
			return
		}
		if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return
		}
		if !errors.Is(replayErr, primitives.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load response replay")
			return
		}
	}
	for _, variant := range inboxItemIDVariants(effectiveItemID) {
		claimed, claimErr := responseStore.HumanAttentionResponseClaimed(r.Context(), variant)
		if claimErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to check response state")
			return
		}
		if claimed {
			if req.IdempotencyKey != "" {
				replay, replayErr := responseStore.HumanAttentionResponseReplay(r.Context(), actorID, req.IdempotencyKey, requestHash)
				if replayErr == nil {
					writeAuthorizedInboxResponseReplay(w, r, opts, replay)
					return
				}
				if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
					writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
					return
				}
			}
			writeError(w, http.StatusConflict, "conflict", "human attention request already has a response")
			return
		}
	}

	item, err := resolveInboxItemByVariants(r.Context(), opts.primitiveStore, effectiveItemID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			if req.IdempotencyKey != "" {
				replay, replayErr := responseStore.HumanAttentionResponseReplay(r.Context(), actorID, req.IdempotencyKey, requestHash)
				if replayErr == nil {
					writeAuthorizedInboxResponseReplay(w, r, opts, replay)
					return
				}
				if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
					writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
					return
				}
			}
			for _, variant := range inboxItemIDVariants(effectiveItemID) {
				claimed, claimErr := responseStore.HumanAttentionResponseClaimed(r.Context(), variant)
				if claimErr == nil && claimed {
					writeError(w, http.StatusConflict, "conflict", "human attention request already has a response")
					return
				}
			}
			writeError(w, http.StatusNotFound, "not_found", "inbox item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox item")
		return
	}

	itemPayload := cloneWorkspaceMap(item.Data)
	applyInboxContractShape(itemPayload, inboxContractHintFromDerived(item))
	if !inboxItemAccessible(r, opts, item.ThreadID, itemPayload) {
		denyPMNotFound(w, "inbox item")
		return
	}
	kind := canonicalHumanAttentionKind(anyString(itemPayload["kind"]))
	if kind == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox item is not a human attention item")
		return
	}

	threadID := strings.TrimSpace(item.ThreadID)
	if threadID == "" {
		threadID = strings.TrimSpace(anyString(itemPayload["thread_id"]))
	}
	if threadID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ask inbox item is missing backing thread_id")
		return
	}

	inboxItemID := strings.TrimSpace(anyString(itemPayload["id"]))
	if inboxItemID == "" {
		inboxItemID = strings.TrimSpace(item.ID)
	}
	if inboxItemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "ask inbox item is missing id")
		return
	}

	subjectRef := strings.TrimSpace(anyString(itemPayload["subject_ref"]))
	relatedRefs, _ := extractStringSlice(itemPayload["related_refs"])
	relatedRefs = append(relatedRefs, normalizeStringSlice(req.RelatedRefs)...)
	// Added response evidence will also be disclosed by an idempotent replay.
	// Check the same merged refs before the initial response can commit.
	responseAccess := cloneWorkspaceMap(itemPayload)
	responseAccess["related_refs"] = relatedRefs
	if !inboxItemAccessible(r, opts, threadID, responseAccess) {
		denyPMNotFound(w, "inbox item")
		return
	}
	requesterActorID := strings.TrimSpace(anyString(itemPayload["requester_actor_id"]))
	requesterAgentID := strings.TrimSpace(anyString(itemPayload["requester_agent_id"]))
	requesterLabel := strings.TrimSpace(anyString(itemPayload["requester_label"]))
	sourceEventID := strings.TrimSpace(anyString(itemPayload["source_event_id"]))
	requestEventRef := strings.TrimSpace(anyString(itemPayload["request_event_ref"]))
	if requestEventRef == "" && sourceEventID != "" {
		requestEventRef = "event:" + sourceEventID
	}
	if sourceEventID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox item is missing source event id")
		return
	}

	// delivery=none on the ask itself opts the requesting agent out of wake
	// delivery server-side. The answer stays recorded and visible; no wake is
	// resolved, queued, or flushed for it, even when the human passes an
	// explicit notify target.
	askDelivery := strings.TrimSpace(anyString(itemPayload["delivery"]))
	if askDelivery == "" {
		askDelivery = askDeliveryModeFromSourceEvent(r.Context(), opts, strings.TrimSpace(anyString(itemPayload["source_event_id"])))
	}
	var target humanAttentionResponseTarget
	if askDelivery == "none" {
		target = humanAttentionResponseTarget{Mode: "none"}
	} else {
		var ok bool
		target, ok = resolveHumanAttentionResponseTarget(w, r, opts, humanAttentionTargetRequest{
			NotifyMode:          req.NotifyMode,
			NotifyTargetActorID: req.NotifyTargetActorID,
			NotifyTargetAgentID: req.NotifyTargetAgentID,
			RequesterActorID:    requesterActorID,
			RequesterAgentID:    requesterAgentID,
		})
		if !ok {
			return
		}
	}

	responseRefs := make([]string, 0, len(relatedRefs)+6)
	responseRefs = append(responseRefs, "thread:"+threadID, "inbox:"+inboxItemID)
	responseRefs = append(responseRefs, relatedRefs...)
	if subjectRef != "" {
		responseRefs = append(responseRefs, subjectRef)
	}
	if sourceEventID != "" {
		responseRefs = append(responseRefs, "event:"+sourceEventID)
	}
	if requestEventRef != "" {
		responseRefs = append(responseRefs, requestEventRef)
	}
	responseRefs = mergeUniqueSortedRefs(responseRefs...)

	summary := buildHumanAttentionResponseSummary(kind, itemPayload, responseText)
	responseEvent := map[string]any{
		"type":      humanAttentionRespondedEventType,
		"thread_id": threadID,
		"refs":      responseRefs,
		"summary":   summary,
		"payload": map[string]any{
			"inbox_item_id":       inboxItemID,
			"request_event_ref":   requestEventRef,
			"kind":                kind,
			"response_text":       responseText,
			"outcome":             req.Outcome,
			"subject_ref":         subjectRef,
			"related_refs":        responseRefs,
			"requester_actor_id":  requesterActorID,
			"requester_agent_id":  requesterAgentID,
			"requester_label":     requesterLabel,
			"responding_actor_id": actorID,
			"notified_actor_id":   target.ActorID,
			"notified_agent_id":   target.AgentID,
		},
		"provenance": eventProvenance(),
	}
	if err := validateEventReferenceConventions(opts.contract, responseEvent, responseRefs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	notifyRequested := target.Mode != "none"
	notifyMessageText := map[bool]string{true: "Answer wake queued for the requester.", false: "Response recorded without notification target."}[notifyRequested]
	if askDelivery == "none" {
		notifyMessageText = "Ask requested no delivery (mode none); response recorded without waking the requester."
	}
	initialNotify := map[string]any{
		"requested":       notifyRequested,
		"queued":          notifyRequested,
		"message":         notifyMessageText,
		"target_actor_id": target.ActorID,
		"target_agent_id": target.AgentID,
		"target_handle":   target.Handle,
		"workspace_id":    opts.workspaceID,
		"thread_id":       threadID,
		"subject_ref":     subjectRef,
		"mode":            target.Mode,
		"quiet_window_ns": int64(opts.answerWakeQuietWindow),
	}
	if accessStore, ok := opts.primitiveStore.(*primitives.Store); ok && notifyRequested {
		if request, err := accessStore.AccessRequestForEvent(r.Context(), sourceEventID); err == nil {
			initialNotify["related_refs"] = []string{"access-request:" + request.ID}
		}
	}
	// Persist the exact HTTP replay together with the answer. Projection refresh
	// and wake delivery are driven by durable queues, never the request lifetime.
	responseFields := hygiene.attach(map[string]any{"notify": map[string]any{
		"requested": notifyRequested, "queued": notifyRequested,
		"message": initialNotify["message"], "target_actor_id": target.ActorID,
		"target_agent_id": target.AgentID, "target_handle": target.Handle, "mode": target.Mode,
	}})
	storedResponse, replayed, err := responseStore.AppendHumanAttentionResponse(r.Context(), actorID, sourceEventID, inboxItemID, req.IdempotencyKey, requestHash, responseEvent, initialNotify, responseFields)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrInvalidAccessDecision) || errors.Is(err, auth.ErrInvalidRequest) || errors.Is(err, auth.ErrAgentNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, auth.ErrHumanRequired) {
			writeError(w, http.StatusForbidden, "human_required", "only active humans can decide access requests")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded) {
			writeError(w, http.StatusConflict, "conflict", "human attention request already has a response")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to store human attention response")
		return
	}
	if replayed {
		writeAuthorizedInboxResponseReplay(w, r, opts, storedResponse)
		return
	}
	notifyProjectionMaintainer(opts)
	writeJSON(w, http.StatusCreated, storedResponse)
}

// Authorization precedes replay and claim lookups so an inaccessible request
// also looks absent when it has already been answered or the key conflicts.
func authorizeInboxResponseAccess(w http.ResponseWriter, r *http.Request, opts handlerOptions, store humanAttentionResponseStore, id string) bool {
	item, err := resolveInboxItemByVariants(r.Context(), opts.primitiveStore, id)
	if err == nil {
		if inboxItemAccessible(r, opts, item.ThreadID, payloadFromDerivedInboxItem(item)) {
			return true
		}
	} else if errors.Is(err, primitives.ErrNotFound) {
		request, requestErr := store.HumanAttentionResponseRequest(r.Context(), id)
		if requestErr == nil {
			requestPayload, ok := request["payload"].(map[string]any)
			if !ok {
				denyPMNotFound(w, "inbox item")
				return false
			}
			payload := cloneWorkspaceMap(requestPayload)
			payload["related_refs"] = append(stringSliceAny(payload["related_refs"]), stringSliceAny(request["refs"])...)
			if inboxItemAccessible(r, opts, eventThreadID(request), payload) {
				return true
			}
		} else if !errors.Is(requestErr, primitives.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to authorize inbox item")
			return false
		}
	} else {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to authorize inbox item")
		return false
	}
	denyPMNotFound(w, "inbox item")
	return false
}

// A successful response removes the open projection. Replays therefore use
// the persisted response's original subject/refs, with current authorization,
// rather than trusting either the replay key or a now-missing open Inbox row.
func writeAuthorizedInboxResponseReplay(w http.ResponseWriter, r *http.Request, opts handlerOptions, replay map[string]any) {
	event, _ := replay["event"].(map[string]any)
	payload, _ := event["payload"].(map[string]any)
	if event == nil || payload == nil || !inboxItemAccessible(r, opts, eventThreadID(event), payload) {
		denyPMNotFound(w, "inbox item")
		return
	}
	writeJSON(w, http.StatusCreated, replay)
}

func resolveInboxItemByVariants(ctx context.Context, store PrimitiveStore, inboxItemID string) (primitives.DerivedInboxItem, error) {
	for _, candidate := range inboxItemIDVariants(strings.TrimSpace(inboxItemID)) {
		item, err := store.GetDerivedInboxItem(ctx, candidate)
		if err == nil {
			return item, nil
		}
		if !errors.Is(err, primitives.ErrNotFound) {
			return primitives.DerivedInboxItem{}, err
		}
	}
	return primitives.DerivedInboxItem{}, primitives.ErrNotFound
}

type humanAttentionTargetRequest struct {
	NotifyMode          string
	NotifyTargetActorID string
	NotifyTargetAgentID string
	RequesterActorID    string
	RequesterAgentID    string
}

type humanAttentionResponseTarget struct {
	Mode    string
	ActorID string
	AgentID string
	Handle  string
}

func resolveHumanAttentionResponseTarget(
	w http.ResponseWriter,
	r *http.Request,
	opts handlerOptions,
	req humanAttentionTargetRequest,
) (humanAttentionResponseTarget, bool) {
	mode := strings.ToLower(strings.TrimSpace(req.NotifyMode))
	if mode == "" {
		mode = "original"
	}
	if strings.TrimSpace(req.NotifyTargetActorID) != "" || strings.TrimSpace(req.NotifyTargetAgentID) != "" {
		mode = "target"
	}

	switch mode {
	case "none":
		return humanAttentionResponseTarget{Mode: "none"}, true
	case "original":
		target, found, err := resolveAgentNotificationTarget(r.Context(), opts, req.RequesterActorID, req.RequesterAgentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve requester notification target")
			return humanAttentionResponseTarget{}, false
		}
		if !found {
			writeError(w, http.StatusConflict, "notification_target_required", "original requester is not resolvable; choose a replacement notification target or submit notify_mode=none")
			return humanAttentionResponseTarget{}, false
		}
		target.Mode = "original"
		return target, true
	case "target":
		target, found, err := resolveAgentNotificationTarget(r.Context(), opts, req.NotifyTargetActorID, req.NotifyTargetAgentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve notification target")
			return humanAttentionResponseTarget{}, false
		}
		if !found {
			writeError(w, http.StatusConflict, "notification_target_required", "notification target is not resolvable; choose another target or submit notify_mode=none")
			return humanAttentionResponseTarget{}, false
		}
		target.Mode = "target"
		return target, true
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "notify_mode must be original, target, or none")
		return humanAttentionResponseTarget{}, false
	}
}

func resolveAgentNotificationTarget(ctx context.Context, opts handlerOptions, actorID string, agentID string) (humanAttentionResponseTarget, bool, error) {
	if opts.authStore == nil {
		return humanAttentionResponseTarget{}, false, nil
	}
	actorID = strings.TrimSpace(actorID)
	agentID = strings.TrimSpace(agentID)
	var principal auth.AuthPrincipalSummary
	var found bool
	var err error
	if agentID != "" {
		principal, found, err = findAgentPrincipalByAgentID(ctx, opts.authStore, agentID)
	} else if actorID != "" {
		principal, found, err = findAgentPrincipalByActorID(ctx, opts.authStore, actorID)
	}
	if err != nil || !found {
		return humanAttentionResponseTarget{}, found, err
	}
	return humanAttentionResponseTarget{
		ActorID: principal.ActorID,
		AgentID: principal.AgentID,
		Handle:  principal.Username,
	}, true, nil
}

func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func resolveOptionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func buildAskResponseSummary(queryText, answer string) string {
	queryText = strings.TrimSpace(queryText)
	answer = strings.TrimSpace(answer)
	if queryText == "" {
		return "Answered agent ask"
	}
	if len(queryText) > 72 {
		queryText = strings.TrimSpace(queryText[:72]) + "…"
	}
	if answer == "" {
		return "Answered: " + queryText
	}
	return "Answered: " + queryText
}

func buildHumanAttentionResponseSummary(kind string, itemPayload map[string]any, responseText string) string {
	title := strings.TrimSpace(anyString(itemPayload["title"]))
	if title == "" {
		title = strings.TrimSpace(anyString(itemPayload["body"]))
	}
	if title == "" {
		title = strings.TrimSpace(kind)
	}
	if len(title) > 72 {
		title = strings.TrimSpace(title[:72]) + "..."
	}
	return "Human response recorded: " + title
}

func sendHumanAttentionResponseWakeBestEffort(
	ctx context.Context,
	opts handlerOptions,
	actorID string,
	threadID string,
	subjectRef string,
	targetActorID string,
	targetHandle string,
	triggerText string,
	triggerEventID string,
	triggerCreatedAt string,
) (bool, string) {
	targetActorID = strings.TrimSpace(targetActorID)
	if targetActorID == "" {
		return false, "Response recorded without notification target."
	}
	workspaceID := strings.TrimSpace(opts.workspaceID)
	if workspaceID == "" {
		workspaceID = "ws_main"
	}
	targetHandle = strings.TrimSpace(targetHandle)
	if targetHandle == "" {
		targetHandle = "agent"
	}
	online := false
	if opts.authStore != nil {
		principal, found, err := findAgentPrincipalByActorID(ctx, opts.authStore, targetActorID)
		if err == nil && found {
			if strings.TrimSpace(principal.Username) != "" {
				targetHandle = strings.TrimSpace(principal.Username)
			}
			status := auth.DescribeWakeRouting(principal, workspaceID, time.Now().UTC())
			online = status.Online
		}
	}

	if triggerEventID == "" {
		triggerEventID = fmt.Sprintf("ask-response:%d", time.Now().UTC().UnixNano())
	}
	if triggerCreatedAt == "" {
		triggerCreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	wakeupID := router.WakeupArtifactID(workspaceID, threadID, triggerEventID, targetActorID)
	wakeRefs := append(router.WakeArtifactRefs(threadID, triggerEventID, subjectRef), "artifact:"+wakeupID)

	_, artifactErr := opts.primitiveStore.CreateArtifact(ctx, actorID, map[string]any{
		"id":              wakeupID,
		"kind":            router.WakeArtifactKind,
		"summary":         "Wake packet for @" + targetHandle,
		"refs":            router.WakeArtifactRefs(threadID, triggerEventID, subjectRef),
		"target_handle":   targetHandle,
		"target_actor_id": targetActorID,
		"workspace_id":    workspaceID,
		"thread_id":       threadID,
	}, map[string]any{
		"version":            router.WakePacketVersion,
		"wakeup_id":          wakeupID,
		"target_handle":      targetHandle,
		"target_actor_id":    targetActorID,
		"workspace_id":       workspaceID,
		"thread_id":          threadID,
		"trigger_event_id":   triggerEventID,
		"trigger_created_at": triggerCreatedAt,
		"trigger_text":       triggerText,
		"subject_ref":        subjectRef,
	}, "structured")
	if artifactErr != nil && !errors.Is(artifactErr, primitives.ErrConflict) {
		// Continue: queue metadata still carries enough wake metadata for delivery.
	}

	_, wakeErr := opts.primitiveStore.UpsertAgentWakeup(ctx, primitives.AgentWakeup{
		WakeupID:         wakeupID,
		Status:           primitives.AgentWakeupStatusRequested,
		TargetHandle:     targetHandle,
		TargetActorID:    targetActorID,
		WorkspaceID:      workspaceID,
		ThreadID:         threadID,
		TriggerEventID:   triggerEventID,
		TriggerCreatedAt: triggerCreatedAt,
		TriggerText:      triggerText,
		Refs:             wakeRefs,
	})
	if wakeErr != nil && !errors.Is(wakeErr, primitives.ErrConflict) {
		return true, "Queued — will deliver when agent reconnects."
	}

	if online {
		return false, "Delivered to asking agent."
	}
	return true, "Queued — will deliver when agent reconnects."
}

func findAgentPrincipalByActorID(ctx context.Context, authStore *auth.Store, actorID string) (auth.AuthPrincipalSummary, bool, error) {
	if authStore == nil {
		return auth.AuthPrincipalSummary{}, false, nil
	}
	targets, err := authStore.NotificationTargets(ctx, []string{strings.TrimSpace(actorID)}, nil)
	p, found := targets["actor:"+strings.TrimSpace(actorID)]
	return p, found, err
}

func findAgentPrincipalByAgentID(ctx context.Context, authStore *auth.Store, agentID string) (auth.AuthPrincipalSummary, bool, error) {
	if authStore == nil {
		return auth.AuthPrincipalSummary{}, false, nil
	}
	targets, err := authStore.NotificationTargets(ctx, nil, []string{strings.TrimSpace(agentID)})
	p, found := targets["agent:"+strings.TrimSpace(agentID)]
	return p, found, err
}

// askBatchRespondStore is the store surface the atomic batch respond path needs.
type askBatchRespondStore interface {
	AppendHumanAttentionBatchResponse(context.Context, string, string, string, string, string, []primitives.HumanAttentionBatchResponseMember) (map[string]any, bool, error)
	HumanAttentionBatchResponseReplay(context.Context, string, string, string) (map[string]any, error)
	HumanAttentionBatchResponseClaimed(context.Context, string) (bool, error)
	HumanAttentionBatchResponseRequest(context.Context, string) (map[string]any, error)
}

// respondInboxBatchIfNeeded intercepts responses to a batch inbox item, which
// answer every member in one atomic pass. It also replays a retried batch
// submit whose item has already left the open projection. It returns true when
// the request was handled (or failed) as a batch response.
func respondInboxBatchIfNeeded(w http.ResponseWriter, r *http.Request, opts handlerOptions, effectiveItemID string, actorID string, req inboxRespondRequest) bool {
	item, err := resolveInboxItemByVariants(r.Context(), opts.primitiveStore, effectiveItemID)
	batchStore, hasBatchStore := opts.primitiveStore.(askBatchRespondStore)
	if err == nil {
		payload := payloadFromDerivedInboxItem(item)
		if _, isBatch := payload["batch"].(map[string]any); !isBatch || !hasBatchStore {
			return false
		}
		handleRespondInboxBatch(w, r, opts, batchStore, item, payload, effectiveItemID, actorID, req)
		return true
	}
	if !errors.Is(err, primitives.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox item")
		return true
	}
	// An answered batch item is gone from the open projection. A retried
	// submit must replay through the batch claim before the single-ask path
	// reports the item missing.
	if !hasBatchStore {
		return false
	}
	hash := ""
	if req.IdempotencyKey != "" {
		var hashErr error
		hash, hashErr = hashBatchRespondRequest(actorID, effectiveItemID, req)
		if hashErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to hash human attention response")
			return true
		}
		replay, replayErr := batchStore.HumanAttentionBatchResponseReplay(r.Context(), actorID, req.IdempotencyKey, hash)
		if replayErr == nil {
			writeAuthorizedInboxBatchReplay(w, r, opts, replay)
			return true
		}
		if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return true
		}
		if !errors.Is(replayErr, primitives.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load response replay")
			return true
		}
	}
	// A no-key retry of an answered batch has nothing to replay through; the
	// stored batch claim (by item id) must still report the conflict instead
	// of falling into the single-ask path, which would 400 on the empty flat
	// fields before it ever saw the claim. Authorization still runs first so
	// an inaccessible answered batch looks absent, matching the single-ask path.
	if claimed, claimErr := batchStore.HumanAttentionBatchResponseRequest(r.Context(), effectiveItemID); claimErr == nil && claimed != nil {
		if !inboxBatchResponseAccessible(r, opts, claimed) {
			denyPMNotFound(w, "inbox item")
			return true
		}
		writeError(w, http.StatusConflict, "conflict", "this ask batch has already been answered")
		return true
	} else if !errors.Is(claimErr, primitives.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to check batch response state")
		return true
	}
	return false
}

func hashBatchRespondRequest(actorID, inboxItemID string, req inboxRespondRequest) (string, error) {
	hashInput, err := json.Marshal(struct {
		ActorID     string              `json:"actor_id"`
		InboxItemID string              `json:"inbox_item_id"`
		Request     inboxRespondRequest `json:"request"`
	}{actorID, inboxItemID, req})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(hashInput)
	return hex.EncodeToString(digest[:]), nil
}

// handleRespondInboxBatch records one answer per member of a filed ask batch
// in a single atomic pass. Each answer releases to its own ask's delivery
// mode: members with delivery=none stay recorded without waking the requester,
// the rest queue into the requester's debounced answer wake.
func handleRespondInboxBatch(
	w http.ResponseWriter,
	r *http.Request,
	opts handlerOptions,
	batchStore askBatchRespondStore,
	item primitives.DerivedInboxItem,
	itemPayload map[string]any,
	effectiveItemID string,
	actorID string,
	req inboxRespondRequest,
) {
	// Authorization precedes every batch-shape validation, mirroring the
	// single-ask invariant: an inaccessible batch must also look absent.
	batchThreadID := strings.TrimSpace(item.ThreadID)
	if batchThreadID == "" {
		batchThreadID = strings.TrimSpace(anyString(itemPayload["thread_id"]))
	}
	if !inboxItemAccessible(r, opts, batchThreadID, itemPayload) {
		denyPMNotFound(w, "inbox item")
		return
	}
	if strings.TrimSpace(req.ResponseText) != "" || strings.TrimSpace(req.Outcome) != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "response_text and outcome must be empty when answers are provided for a batch inbox item")
		return
	}
	if strings.TrimSpace(req.NotifyMode) != "" || strings.TrimSpace(req.NotifyTargetActorID) != "" || strings.TrimSpace(req.NotifyTargetAgentID) != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "batch answers release to each ask's delivery mode; notify overrides are not supported")
		return
	}
	if len(req.IdempotencyKey) > 128 || strings.TrimSpace(req.IdempotencyKey) != req.IdempotencyKey {
		writeError(w, http.StatusBadRequest, "invalid_request", "idempotency_key must be at most 128 characters without surrounding whitespace")
		return
	}
	if len(req.Answers) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "answers is required for a batch inbox item")
		return
	}
	batch, _ := itemPayload["batch"].(map[string]any)
	batchID := strings.TrimSpace(anyString(batch["id"]))
	if batchID == "" {
		writeError(w, http.StatusInternalServerError, "internal_error", "batch inbox item is missing batch id")
		return
	}
	sections := batchSectionsFromPayload(batch["asks"])
	if len(sections) == 0 {
		writeError(w, http.StatusInternalServerError, "internal_error", "batch inbox item has no decision sections")
		return
	}
	threadID := batchThreadID
	if len(req.Answers) != len(sections) {
		writeError(w, http.StatusBadRequest, "invalid_request", "one answer per batch ask is required")
		return
	}

	// Answers are matched to members by ask ref so a stale section order
	// cannot shuffle a decision onto the wrong ask.
	answerByRef := make(map[string]inboxRespondAnswer, len(req.Answers))
	for _, answer := range req.Answers {
		ref := strings.TrimSpace(answer.AskRef)
		if ref == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "every answer needs ask_ref")
			return
		}
		if _, dup := answerByRef[ref]; dup {
			writeError(w, http.StatusBadRequest, "invalid_request", "duplicate answers for "+ref)
			return
		}
		answerByRef[ref] = answer
	}
	kind := canonicalHumanAttentionKind(anyString(itemPayload["kind"]))
	requesterActorID := strings.TrimSpace(anyString(itemPayload["requester_actor_id"]))
	requesterAgentID := strings.TrimSpace(anyString(itemPayload["requester_agent_id"]))
	requesterLabel := strings.TrimSpace(anyString(itemPayload["requester_label"]))

	var hygiene markdownHygieneCollector
	anyNotify := false
	members := make([]primitives.HumanAttentionBatchResponseMember, 0, len(sections))
	for _, section := range sections {
		ref := strings.TrimSpace(anyString(section["request_event_ref"]))
		answer, ok := answerByRef[ref]
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "missing answer for batch ask "+ref)
			return
		}
		delete(answerByRef, ref)
		sourceEventID := strings.TrimPrefix(ref, "event:")
		if sourceEventID == "" || !strings.HasPrefix(ref, "event:") {
			writeError(w, http.StatusInternalServerError, "internal_error", "batch section is missing its request event ref")
			return
		}
		answerText := strings.TrimSpace(answer.ResponseText)
		if answerText == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "response_text is required for batch ask "+ref)
			return
		}
		if err := schema.ValidateEnum(opts.contract, "human_attention_response_outcome", answer.Outcome); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "batch ask "+ref+": "+err.Error())
			return
		}
		if !hygiene.normalizeField(w, "response_text", &answerText) {
			return
		}
		answerText = strings.TrimSpace(answerText)
		answerRefs := normalizeStringSlice(answer.RelatedRefs)
		// The section's own subject is part of this answer's access surface:
		// a member whose card is restricted must not be answerable through
		// the batch item by a principal who cannot see that card.
		mergedRefs := append(stringSliceAny(itemPayload["related_refs"]), answerRefs...)
		mergedRefs = append(mergedRefs, strings.TrimSpace(anyString(section["subject_ref"])))
		responseAccess := cloneWorkspaceMap(itemPayload)
		responseAccess["related_refs"] = mergedRefs
		if !inboxItemAccessible(r, opts, threadID, responseAccess) {
			denyPMNotFound(w, "inbox item")
			return
		}
		memberDelivery := strings.TrimSpace(anyString(section["delivery"]))
		if memberDelivery == "" {
			memberDelivery = askDeliveryModeFromSourceEvent(r.Context(), opts, sourceEventID)
		}
		memberNotify := memberDelivery != "none"
		anyNotify = anyNotify || memberNotify
		summary := buildHumanAttentionResponseSummary(kind, map[string]any{"title": anyString(section["title"])}, answerText)
		responseRefs := mergeUniqueSortedRefs(append([]string{"thread:" + threadID, "inbox:" + effectiveItemID, "event:" + sourceEventID, ref, strings.TrimSpace(anyString(section["subject_ref"]))}, answerRefs...)...)
		event := map[string]any{
			"type":      humanAttentionRespondedEventType,
			"thread_id": threadID,
			"refs":      responseRefs,
			"summary":   summary,
			"payload": map[string]any{
				"inbox_item_id":       effectiveItemID,
				"request_event_ref":   ref,
				"kind":                kind,
				"response_text":       answerText,
				"outcome":             answer.Outcome,
				"subject_ref":         strings.TrimSpace(anyString(section["subject_ref"])),
				"related_refs":        responseRefs,
				"requester_actor_id":  requesterActorID,
				"requester_agent_id":  requesterAgentID,
				"requester_label":     requesterLabel,
				"responding_actor_id": actorID,
				"batch_id":            batchID,
			},
			"provenance": eventProvenance(),
		}
		if err := validateEventReferenceConventions(opts.contract, event, responseRefs); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "batch ask "+ref+": "+err.Error())
			return
		}
		notifyMessageText := map[bool]string{true: "Answer wake queued for the requester.", false: "Ask requested no delivery (mode none); answer recorded without waking the requester."}[memberNotify]
		members = append(members, primitives.HumanAttentionBatchResponseMember{
			SourceEventID: sourceEventID,
			InboxItemID:   effectiveItemID,
			Event:         event,
			Notify: map[string]any{
				"requested":       memberNotify,
				"queued":          memberNotify,
				"message":         notifyMessageText,
				"target_actor_id": requesterActorID,
				"target_agent_id": requesterAgentID,
				"workspace_id":    opts.workspaceID,
				"thread_id":       threadID,
				"subject_ref":     strings.TrimSpace(anyString(section["subject_ref"])),
				"mode":            "original",
				"quiet_window_ns": int64(opts.answerWakeQuietWindow),
			},
		})
	}
	if len(answerByRef) != 0 {
		unknownRef := ""
		for ref := range answerByRef {
			unknownRef = ref
			break
		}
		writeError(w, http.StatusBadRequest, "invalid_request", "answer ask_ref "+unknownRef+" is not part of this batch")
		return
	}

	var target humanAttentionResponseTarget
	if anyNotify {
		resolved, found, targetErr := resolveAgentNotificationTarget(r.Context(), opts, requesterActorID, requesterAgentID)
		if targetErr != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve requester notification target")
			return
		}
		if !found {
			writeError(w, http.StatusConflict, "notification_target_required", "original requester is not resolvable; the batch cannot release its wakes")
			return
		}
		target = resolved
		target.Mode = "original"
	} else {
		target = humanAttentionResponseTarget{Mode: "none"}
	}

	hash, err := hashBatchRespondRequest(actorID, effectiveItemID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to hash human attention response")
		return
	}
	if req.IdempotencyKey != "" {
		replay, replayErr := batchStore.HumanAttentionBatchResponseReplay(r.Context(), actorID, req.IdempotencyKey, hash)
		if replayErr == nil {
			writeAuthorizedInboxBatchReplay(w, r, opts, replay)
			return
		}
		if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return
		}
		if !errors.Is(replayErr, primitives.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load response replay")
			return
		}
	}
	claimed, claimErr := batchStore.HumanAttentionBatchResponseClaimed(r.Context(), batchID)
	if claimErr != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to check batch response state")
		return
	}
	if claimed {
		if req.IdempotencyKey != "" {
			replay, replayErr := batchStore.HumanAttentionBatchResponseReplay(r.Context(), actorID, req.IdempotencyKey, hash)
			if replayErr == nil {
				writeAuthorizedInboxBatchReplay(w, r, opts, replay)
				return
			}
			if errors.Is(replayErr, primitives.ErrHumanAttentionIdempotencyConflict) {
				writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
				return
			}
		}
		writeError(w, http.StatusConflict, "conflict", "this ask batch has already been answered")
		return
	}

	stored, replayed, err := batchStore.AppendHumanAttentionBatchResponse(r.Context(), actorID, batchID, effectiveItemID, req.IdempotencyKey, hash, members)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrInvalidAccessDecision) || errors.Is(err, auth.ErrInvalidRequest) || errors.Is(err, auth.ErrAgentNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, auth.ErrHumanRequired) {
			writeError(w, http.StatusForbidden, "human_required", "only active humans can decide access requests")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded) {
			writeError(w, http.StatusConflict, "conflict", "this ask batch has already been answered")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionMemberWithdrawn) {
			// Nothing committed: the reader refreshes the item and re-submits
			// against the shrunk batch.
			writeError(w, http.StatusConflict, "conflict", "batch member "+err.Error()+" was withdrawn; refresh the item and answer the remaining asks")
			return
		}
		if errors.Is(err, primitives.ErrHumanAttentionIdempotencyConflict) {
			writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency key was used for another response")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to store batch response")
		return
	}
	if replayed {
		writeAuthorizedInboxBatchReplay(w, r, opts, stored)
		return
	}
	_ = refreshDerivedTopicProjection(r.Context(), opts, threadID, time.Now().UTC(), actorID)

	notifyMessageText := "All batch answers recorded; wakes queue per ask delivery."
	if !anyNotify {
		notifyMessageText = "Every ask in this batch requested no delivery (mode none); answers recorded without waking the requester."
	}
	notify := map[string]any{
		"requested":       anyNotify,
		"queued":          anyNotify,
		"message":         notifyMessageText,
		"target_actor_id": target.ActorID,
		"target_agent_id": target.AgentID,
		"target_handle":   target.Handle,
		"mode":            target.Mode,
		"batch_id":        batchID,
	}
	if anyNotify && opts.answerWakeFlushWhenNoOpenAsks {
		if flushStore, ok := opts.primitiveStore.(interface {
			CountOpenHumanAttentionAsks(context.Context, string) (int, error)
		}); ok {
			if openAsks, countErr := flushStore.CountOpenHumanAttentionAsks(r.Context(), requesterActorID); countErr == nil && openAsks == 0 {
				maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{
					PrimitiveStore:      opts.primitiveStore,
					WorkspaceID:         opts.workspaceID,
					QuietWindow:         opts.answerWakeQuietWindow,
					FlushWhenNoOpenAsks: true,
					RecipientIsActive:   answerWakeRecipientIsActive(opts.authStore),
				})
				if flushErr := maintainer.FlushTarget(r.Context(), target.ActorID); flushErr == nil {
					notify["message"] = "All of your open asks are answered; the answer batch is queued now."
				}
			}
		}
	}

	storedAnswers, _ := stored["answers"].([]any)
	response := map[string]any{
		"batch_id": batchID,
		"answers":  storedAnswers,
		"notify":   notify,
	}
	response = hygiene.attach(response)
	if saveErr := batchStoreSave(opts, batchID, response); saveErr != nil {
		log.Printf("save batch response replay: %v", saveErr)
	}
	writeJSON(w, http.StatusCreated, response)
}

func batchStoreSave(opts handlerOptions, batchID string, response map[string]any) error {
	if store, ok := opts.primitiveStore.(interface {
		SaveHumanAttentionBatchResponseResult(context.Context, string, map[string]any) error
	}); ok {
		return store.SaveHumanAttentionBatchResponseResult(context.Background(), batchID, response)
	}
	return nil
}

// writeAuthorizedInboxBatchReplay returns a stored batch response under
// current authorization: the answered batch item is gone from the open
// projection, so the stored answer events carry the original subject/refs.
func writeAuthorizedInboxBatchReplay(w http.ResponseWriter, r *http.Request, opts handlerOptions, replay map[string]any) {
	if !inboxBatchResponseAccessible(r, opts, replay) {
		denyPMNotFound(w, "inbox item")
		return
	}
	writeJSON(w, http.StatusCreated, replay)
}

func inboxBatchResponseAccessible(r *http.Request, opts handlerOptions, replay map[string]any) bool {
	answers, _ := replay["answers"].([]any)
	if len(answers) == 0 {
		return false
	}
	for _, raw := range answers {
		answer, _ := raw.(map[string]any)
		event, _ := answer["event"].(map[string]any)
		if event == nil {
			return false
		}
		if !inboxItemAccessible(r, opts, eventThreadID(event), event) {
			return false
		}
	}
	return true
}

func batchSectionsFromPayload(raw any) []map[string]any {
	out := []map[string]any{}
	switch typed := raw.(type) {
	case []map[string]any:
		for _, section := range typed {
			if section != nil {
				out = append(out, section)
			}
		}
	case []any:
		for _, entry := range typed {
			if section, ok := entry.(map[string]any); ok && section != nil {
				out = append(out, section)
			}
		}
	}
	return out
}

// askDeliveryModeFromSourceEvent reads the delivery mode recorded on the ask
// event itself. It covers items whose projection predates delivery fields.
func askDeliveryModeFromSourceEvent(ctx context.Context, opts handlerOptions, sourceEventID string) string {
	if opts.primitiveStore == nil || strings.TrimSpace(sourceEventID) == "" {
		return ""
	}
	event, err := opts.primitiveStore.GetEvent(ctx, strings.TrimSpace(sourceEventID))
	if err != nil {
		return ""
	}
	payload, _ := event["payload"].(map[string]any)
	return strings.TrimSpace(anyString(payload["delivery"]))
}
