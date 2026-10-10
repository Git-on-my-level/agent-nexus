package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

func accessRequestRouteAccess(r *http.Request) routeAccessRequirement {
	if r.URL.Path == "/auth/access/summary" {
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet)(r)
	}
	if r.URL.Path == "/auth/access-requests" {
		if r.Method == http.MethodGet {
			return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet)(r)
		}
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/auth/access-requests/"), "/")
	if len(parts) == 2 && parts[0] != "" && (parts[1] == "approve" || parts[1] == "deny") {
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
	}
	return routeAccessRequirement{}
}

func handleAccessRequestRoutes(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok || opts.authStore == nil {
		writeError(w, 503, "primitives_unavailable", "access requests require workspace storage and authentication")
		return
	}
	if r.URL.Path == "/auth/access-requests" && r.Method == http.MethodPost {
		actor, ok := cachedAuthenticatedPrincipal(r)
		if !ok || actor == nil || actor.PrincipalKind != string(auth.PrincipalKindAgent) {
			writeError(w, 403, "agent_required", "only agents can request their own access")
			return
		}
		var body map[string]any
		if !decodeJSONBody(w, r, &body) {
			return
		}
		for field := range body {
			if field != "grant" && field != "reason" {
				writeError(w, 400, "invalid_request", "unknown access request field: "+field)
				return
			}
		}
		grant, grantOK := body["grant"].(string)
		reason, reasonOK := body["reason"].(string)
		if !grantOK || !reasonOK {
			writeError(w, 400, "invalid_request", "grant and reason must be strings")
			return
		}
		var hygiene markdownHygieneCollector
		if !hygiene.normalizeField(w, "reason", &reason) {
			return
		}
		item, err := store.CreateAccessRequest(r.Context(), *actor, grant, reason)
		if err != nil {
			writeAccessRequestError(w, err)
			return
		}
		// Refresh retries too: an earlier client disconnect must not leave an
		// otherwise committed request waiting for a projection timer.
		event, err := store.GetEvent(r.Context(), strings.TrimPrefix(item.RequestEventRef, "event:"))
		if err != nil {
			writeAccessRequestError(w, err)
			return
		}
		threadID := anyString(event["thread_id"])
		if err := refreshDerivedTopicProjection(r.Context(), opts, threadID, time.Now().UTC(), actor.ActorID); err != nil {
			writeAccessRequestError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, hygiene.attach(map[string]any{"request": item}))
		return
	}
	actor, ok := requireHumanPrincipal(w, r, opts)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/auth/access/summary" && r.Method == http.MethodGet {
		counts, err := store.AccessSummary(r.Context())
		if err != nil {
			writeAccessRequestError(w, err)
			return
		}
		writeJSON(w, 200, counts)
		return
	}
	if r.URL.Path == "/auth/access-requests" && r.Method == http.MethodGet {
		items, err := store.ListPendingAccessRequests(r.Context())
		if err != nil {
			writeAccessRequestError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"requests": items})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/auth/access-requests/"), "/")
	if len(parts) != 2 || parts[0] == "" || (parts[1] != "approve" && parts[1] != "deny") || r.Method != http.MethodPost {
		writeError(w, 404, "not_found", "endpoint not found")
		return
	}
	item, err := store.GetAccessRequest(r.Context(), parts[0])
	if err != nil {
		writeAccessRequestError(w, err)
		return
	}
	outcome, status := "rejected", "denied"
	if parts[1] == "approve" {
		outcome, status = "approved", "approved"
	}
	if item.Status != "pending" {
		if item.Status != status {
			writeError(w, 409, "conflict", "access request has the opposite decision")
			return
		}
		if err := refreshAccessRequestProjection(r.Context(), store, opts, item, actor.ActorID); err != nil {
			writeAccessRequestError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"request": item})
		return
	}
	sourceEventID := strings.TrimPrefix(item.RequestEventRef, "event:")
	source, err := store.GetEvent(r.Context(), sourceEventID)
	if err != nil {
		writeAccessRequestError(w, err)
		return
	}
	threadID := anyString(source["thread_id"])
	accessRef := "access-request:" + item.ID
	refs := []string{"thread:" + threadID, "event:" + sourceEventID, "inbox:" + item.InboxItemID}
	note := "Access request " + status + ": " + item.Grant
	event := map[string]any{"type": humanAttentionRespondedEventType, "thread_id": threadID, "refs": refs, "summary": note,
		"payload": map[string]any{"inbox_item_id": item.InboxItemID, "request_event_ref": item.RequestEventRef, "kind": "review", "response_text": note, "outcome": outcome, "subject_ref": "thread:" + threadID, "related_refs": refs, "requester_actor_id": item.ActorID, "requester_agent_id": item.PrincipalID, "requester_label": item.Username, "responding_actor_id": actor.ActorID, "notified_actor_id": item.ActorID, "notified_agent_id": item.PrincipalID}, "provenance": eventProvenance()}
	notify := map[string]any{"requested": true, "queued": true, "target_actor_id": item.ActorID, "target_agent_id": item.PrincipalID, "target_handle": item.Username, "workspace_id": opts.workspaceID, "thread_id": threadID, "subject_ref": "thread:" + threadID, "related_refs": []string{accessRef}, "mode": "original", "quiet_window_ns": int64(opts.answerWakeQuietWindow)}
	_, _, err = store.AppendHumanAttentionResponse(r.Context(), actor.ActorID, sourceEventID, item.InboxItemID, "", "", event, notify)
	if err != nil {
		// A matching concurrent decision is a successful retry; an opposing
		// decision stays a conflict. This never reapplies a revoked grant.
		if errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded) {
			item, loadErr := store.GetAccessRequest(r.Context(), parts[0])
			if loadErr == nil && item.Status == status {
				if err := refreshAccessRequestProjection(r.Context(), store, opts, item, actor.ActorID); err != nil {
					writeAccessRequestError(w, err)
					return
				}
				writeJSON(w, 200, map[string]any{"request": item})
				return
			}
		}
		writeAccessRequestError(w, err)
		return
	}
	if err := refreshDerivedTopicProjection(r.Context(), opts, threadID, time.Now().UTC(), actor.ActorID); err != nil {
		writeAccessRequestError(w, err)
		return
	}
	item, err = store.GetAccessRequest(r.Context(), parts[0])
	if err != nil {
		writeAccessRequestError(w, err)
		return
	}
	flushAccessRequestAnswerWake(r, opts, item.ActorID)
	writeJSON(w, 200, map[string]any{"request": item})
}

// flushAccessRequestAnswerWake delivers the same debounced answer wake as an
// ask response. When this decision clears the requester's open asks, the wake
// is queued in this request; otherwise the maintainer sends it after the quiet window.
func flushAccessRequestAnswerWake(r *http.Request, opts handlerOptions, requesterActorID string) {
	if !opts.answerWakeFlushWhenNoOpenAsks || strings.TrimSpace(requesterActorID) == "" {
		return
	}
	batchStore, ok := opts.primitiveStore.(interface {
		CountOpenHumanAttentionAsks(context.Context, string) (int, error)
	})
	if !ok {
		return
	}
	openAsks, err := batchStore.CountOpenHumanAttentionAsks(r.Context(), requesterActorID)
	if err != nil || openAsks != 0 {
		return
	}
	maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{
		PrimitiveStore:      opts.primitiveStore,
		WorkspaceID:         opts.workspaceID,
		QuietWindow:         opts.answerWakeQuietWindow,
		FlushWhenNoOpenAsks: true,
		RecipientIsActive:   answerWakeRecipientIsActive(opts.authStore),
	})
	_ = maintainer.FlushTarget(r.Context(), requesterActorID)
}

func refreshAccessRequestProjection(ctx context.Context, store *primitives.Store, opts handlerOptions, item primitives.AccessRequest, actorID string) error {
	event, err := store.GetEvent(ctx, strings.TrimPrefix(item.RequestEventRef, "event:"))
	if err != nil {
		return err
	}
	return refreshDerivedTopicProjection(ctx, opts, anyString(event["thread_id"]), time.Now().UTC(), actorID)
}

func writeAccessRequestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, primitives.ErrNotFound), errors.Is(err, auth.ErrAgentNotFound):
		writeError(w, 404, "not_found", "access request or active agent not found")
	case errors.Is(err, auth.ErrHumanRequired):
		writeError(w, 403, "human_required", "only active humans can decide access requests")
	case errors.Is(err, auth.ErrInvalidRequest), errors.Is(err, primitives.ErrInvalidAccessDecision):
		writeError(w, 400, "invalid_request", err.Error())
	case errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded):
		writeError(w, 409, "conflict", "access request already has a decision")
	default:
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		writeError(w, 500, "internal_error", "failed to process access request")
	}
}

func enrichAccessRequestInboxItem(ctx context.Context, opts handlerOptions, item map[string]any) {
	if anyString(item["source_event_id"]) == "" {
		return
	}
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok {
		return
	}
	request, err := store.AccessRequestForEvent(ctx, anyString(item["source_event_id"]))
	if err != nil {
		return
	}
	applyAccessRequestInboxMetadata(item, request)
}

func applyAccessRequestInboxMetadata(item map[string]any, request primitives.AccessRequest) {
	item["allowed_response_outcomes"] = []any{"approved", "rejected"}
	item["access_request_id"] = request.ID
	item["requested_grant"] = request.Grant
	item["requester_principal_id"] = request.PrincipalID
}
