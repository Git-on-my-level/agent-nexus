package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

type agentHandoffStore interface {
	CreateAgentHandoff(context.Context, primitives.AgentHandoff, string) (primitives.AgentHandoff, error)
	GetAgentHandoff(context.Context, string, string, string) (primitives.AgentHandoff, error)
	AcknowledgeAgentHandoff(context.Context, primitives.AgentHandoff, string, int) (primitives.AgentHandoff, error)
}

const agentHandoffPath = "/agent-handoffs"

func registerAgentWakeRoutes(registerRoute, registerStreamRoute func(string, routeAccessClassifier, http.HandlerFunc), opts handlerOptions) {
	registerStreamRoute("agent-wakeups", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		handleAskWakeStream(w, r, opts)
	})
	handle := func(w http.ResponseWriter, r *http.Request) { handleAgentHandoff(w, r, opts) }
	registerRoute(agentHandoffPath, exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), handle)
	registerRoute(agentHandoffPath+"/", agentHandoffRouteAccess, handle)
}

func agentHandoffRouteAccess(r *http.Request) routeAccessRequirement {
	if strings.HasSuffix(r.URL.Path, "/ack") {
		return exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost)(r)
	}
	return exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet)(r)
}

func handleAgentHandoff(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isAgentPrincipal(principal) {
		writeError(w, http.StatusForbidden, "agent_required", "agent identity required")
		return
	}
	store, ok := opts.primitiveStore.(agentHandoffStore)
	if !ok || opts.authStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "handoffs unavailable")
		return
	}
	workspaceID := effectiveWorkspaceID(opts.workspaceID)
	if r.URL.Path == agentHandoffPath && r.Method == http.MethodPost {
		queueAgentHandoff(w, r, opts, store, workspaceID, principal.ActorID)
		return
	}
	id, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, agentHandoffPath+"/"), "/")
	pickup := action == "ack" && r.Method == http.MethodPost
	if !primitives.IsAgentHandoffID(id) || (!pickup && (action != "" || r.Method != http.MethodGet)) {
		writeHandoffError(w, primitives.ErrNotFound)
		return
	}
	handoff, err := store.GetAgentHandoff(r.Context(), workspaceID, principal.ActorID, id)
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	if pickup {
		acknowledgeAgentHandoff(w, r, opts, store, handoff, principal.ActorID)
		return
	}
	writeAgentHandoff(w, handoff)
}

var (
	handoffActorID    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	handoffEventRef   = regexp.MustCompile(`^event:[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	handoffMulticaRef = regexp.MustCompile(`^multica:issue:[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)
	handoffGitHubRef  = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9-]+/[A-Za-z0-9_.-]+/((issues|pull)/[1-9][0-9]*|commit/([a-f0-9]{40}|[a-f0-9]{64}))$`)
)

func canonicalHandoffRefs(refs []string) ([]string, bool) {
	if len(refs) < 1 || len(refs) > 8 {
		return nil, false
	}
	for i, ref := range refs {
		ref = strings.TrimSpace(ref)
		if len(ref) > 300 || !(handoffEventRef.MatchString(ref) || handoffMulticaRef.MatchString(ref) || handoffGitHubRef.MatchString(ref)) {
			return nil, false
		}
		if handoffGitHubRef.MatchString(ref) {
			ref = strings.ToLower(ref) // GitHub repository paths are case-insensitive.
			path := strings.Split(strings.TrimPrefix(ref, "https://github.com/"), "/")
			if path[1] == "." || path[1] == ".." {
				return nil, false
			}
		}
		refs[i] = ref
	}
	slices.Sort(refs)
	return slices.Compact(refs), true
}

func queueAgentHandoff(w http.ResponseWriter, r *http.Request, opts handlerOptions, store agentHandoffStore, workspaceID, senderID string) {
	var req struct {
		Recipient string    `json:"recipient_actor_id"`
		Refs      []string  `json:"refs"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	req.Recipient = strings.TrimSpace(req.Recipient)
	refs, valid := canonicalHandoffRefs(req.Refs)
	if !valid || !handoffActorID.MatchString(req.Recipient) || req.Recipient == senderID || req.ExpiresAt.IsZero() {
		writeError(w, http.StatusBadRequest, "invalid_request", "provide a different recipient actor, 1–8 canonical refs and expires_at")
		return
	}
	deadline := req.ExpiresAt.UTC().Truncate(time.Second)
	expiresAt := deadline.Format(time.RFC3339)
	id := primitives.AgentHandoffID(workspaceID, senderID, req.Recipient, expiresAt, refs)
	ctx := r.Context()
	prior, err := store.GetAgentHandoff(ctx, workspaceID, senderID, id)
	if err == nil {
		writeAgentHandoff(w, prior)
		return
	}
	if !errors.Is(err, primitives.ErrNotFound) {
		writeHandoffError(w, err)
		return
	}
	now := time.Now().UTC()
	if !deadline.After(now) || deadline.After(now.Add(7*24*time.Hour)) {
		writeError(w, http.StatusBadRequest, "invalid_request", "expires_at must be in the next seven days")
		return
	}
	target, err := opts.authStore.PrincipalForActor(ctx, req.Recipient)
	if errors.Is(err, auth.ErrAgentNotFound) || errors.Is(err, auth.ErrHostNotFound) {
		writeHandoffError(w, primitives.ErrNotFound)
		return
	}
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	if target.HostID == "" || !auth.DescribeWakeRouting(target, workspaceID, now).Taggable {
		writeHandoffError(w, primitives.ErrNotFound)
		return
	}
	active, err := opts.authStore.HostOwnsActor(ctx, target.HostID, target.ActorID)
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	if !active {
		writeHandoffError(w, primitives.ErrNotFound)
		return
	}
	recipientCtx := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: target.ActorID})
	if !checkHandoffEvents(w, ctx, opts, refs) || !checkHandoffEvents(w, recipientCtx, opts, refs) {
		return
	}
	handoff, err := store.CreateAgentHandoff(ctx, primitives.AgentHandoff{
		ID: id, SenderActorID: senderID, RecipientActorID: target.ActorID,
		WorkspaceID: workspaceID, Refs: refs, ExpiresAt: expiresAt,
	}, target.Username)
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	writeAgentHandoff(w, handoff)
}

func acknowledgeAgentHandoff(w http.ResponseWriter, r *http.Request, opts handlerOptions, store agentHandoffStore, handoff primitives.AgentHandoff, actorID string) {
	if handoff.RecipientActorID != actorID {
		writeHandoffError(w, primitives.ErrNotFound)
		return
	}
	var req struct {
		Outcome    string `json:"outcome"`
		BlockedRef string `json:"blocked_ref"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	blockedRefIndex := 0
	switch {
	case req.Outcome == "refs_verified" && req.BlockedRef == "":
	case req.Outcome == "ref_blocked" && slices.Contains(handoff.Refs, req.BlockedRef):
		blockedRefIndex = slices.Index(handoff.Refs, req.BlockedRef) + 1
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "report refs_verified or provide a handoff ref with ref_blocked")
		return
	}
	if handoff.AcknowledgedAt == "" && blockedRefIndex == 0 && !checkHandoffEvents(w, r.Context(), opts, handoff.Refs) {
		return
	}
	recorded, err := store.AcknowledgeAgentHandoff(r.Context(), handoff, actorID, blockedRefIndex)
	if err != nil {
		writeHandoffError(w, err)
		return
	}
	writeAgentHandoff(w, recorded)
}

// Native evidence is resolved through the scoped local store. External evidence
// is checked by the recipient using its own authorized source tools.
func checkHandoffEvents(w http.ResponseWriter, ctx context.Context, opts handlerOptions, refs []string) bool {
	for _, ref := range refs {
		id, native := strings.CutPrefix(ref, "event:")
		if !native {
			continue
		}
		event, err := opts.primitiveStore.GetEvent(ctx, id)
		if errors.Is(err, primitives.ErrNotFound) || (err == nil && anyString(event["trashed_at"]) != "") {
			writeError(w, http.StatusUnprocessableEntity, "ref_unavailable", "an ANX event ref is unavailable")
			return false
		}
		if err != nil {
			writeHandoffError(w, err)
			return false
		}
	}
	return true
}

func writeHandoffError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "handoff or recipient not found")
	case errors.Is(err, primitives.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "handoff pickup is unavailable or already recorded with a different outcome")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to access handoff")
	}
}

func writeAgentHandoff(w http.ResponseWriter, handoff primitives.AgentHandoff) {
	deadline, _ := time.Parse(time.RFC3339, handoff.ExpiresAt)
	expired := !deadline.After(time.Now())
	delivery := handoff.DeliveryStatus
	switch {
	case delivery == primitives.AgentWakeupStatusCompleted:
		delivery = "bridge_delivered"
	case delivery == primitives.AgentWakeupStatusFailed:
	case expired:
		delivery = "expired"
	case delivery == primitives.AgentWakeupStatusRequested:
		delivery = "queued"
	}
	out := map[string]any{
		"handoff_id": handoff.ID, "sender_actor_id": handoff.SenderActorID, "recipient_actor_id": handoff.RecipientActorID,
		"refs": handoff.Refs, "queued_at": handoff.QueuedAt, "expires_at": handoff.ExpiresAt,
		"delivery_status": delivery, "pickup_status": "pending",
	}
	if handoff.DeliveredAt != "" {
		out["bridge_delivered_at"] = handoff.DeliveredAt
	}
	switch {
	case handoff.AcknowledgedAt != "":
		out["pickup_status"] = "acknowledged"
		out["acknowledged_at"] = handoff.AcknowledgedAt
		out["outcome"] = "refs_verified"
		if handoff.BlockedRefIndex > 0 && handoff.BlockedRefIndex <= len(handoff.Refs) {
			out["outcome"] = "ref_blocked"
			out["blocked_ref"] = handoff.Refs[handoff.BlockedRefIndex-1]
		}
	case expired:
		out["pickup_status"] = "expired"
	}
	writeJSON(w, http.StatusOK, out)
}
