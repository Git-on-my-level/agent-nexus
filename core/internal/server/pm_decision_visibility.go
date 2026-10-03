package server

import (
	"context"
	"errors"
	"strings"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

// PM decision records are workspace-readable, but the evidence attached to a
// decision is only as visible as the underlying resource is to the reader.
// Resolution summaries are projected live at the HTTP boundary, so every
// decision readback (direct proposals, PM turn proposals, get, list) funnels
// through deps.ResolveResolution. Applying the requesting actor's existing
// resource-visibility rules there closes the path where a decision shared
// across actors in one workspace could expose a private PM conversation
// summary (event payload text) to an actor who cannot read the conversation.
//
// Workspace membership alone is never sufficient: the same rules that gate
// GET /threads/{id} and GET /events/{id} gate resolution summaries.

// redactedDecisionSummary clears a resolution summary the requesting actor
// cannot read. exists stays truthful so consumers can still detect missing or
// trashed evidence.
func redactedDecisionSummary(ctx context.Context, store *primitives.Store, agentActorID string, resolved primitives.ResolutionRef) string {
	if resolved.TitleOrSummary == "" {
		return ""
	}
	if pmDecisionRefVisible(ctx, store, agentActorID, resolved.Ref) {
		return resolved.TitleOrSummary
	}
	return ""
}

// pmDecisionRefVisible answers whether the authenticated principal in ctx may
// read the summary of the resource addressed by ref. Fail-closed: a missing
// principal, an unreadable resource, or an unparseable ref denies. Refs may
// address resources by handle or alias, so resolution uses the same canonical
// lookup live resolution uses.
func pmDecisionRefVisible(ctx context.Context, store *primitives.Store, agentActorID, ref string) bool {
	principal, ok := ctx.Value(principalContextKey{}).(*auth.Principal)
	if !ok || principal == nil || strings.TrimSpace(principal.ActorID) == "" {
		return false
	}
	if store == nil {
		return false
	}
	kind, id, err := store.CanonicalResolutionRefID(ctx, ref)
	if err != nil {
		return false
	}
	switch kind {
	case "event":
		if id == "" {
			return true
		}
		event, err := store.GetEvent(ctx, id)
		if err != nil {
			return false
		}
		found, owner, ok := pmBackingThread(ctx, store, eventThreadID(event))
		if !ok {
			return false
		}
		if !found {
			// No live backing thread: mirror requireAccessibleEvent, which
			// hides only PM-shaped events from non-owners.
			if resourceLooksLikePM(event) {
				return pmThreadVisibleForPrincipal(principal, agentActorID, anyString(event["actor_id"]))
			}
			return true
		}
		return pmThreadVisibleForPrincipal(principal, agentActorID, owner)
	case "artifact":
		if id == "" {
			return true
		}
		artifact, err := store.GetArtifact(ctx, id)
		if err != nil {
			return false
		}
		found, owner, ok := pmBackingThread(ctx, store, artifactThreadID(artifact))
		if !ok {
			return false
		}
		// requireAccessibleArtifact treats a missing backing thread as shared.
		if !found {
			return true
		}
		return pmThreadVisibleForPrincipal(principal, agentActorID, owner)
	case "thread":
		if id == "" {
			return true
		}
		found, owner, ok := pmBackingThread(ctx, store, id)
		if !ok {
			return false
		}
		// threadAccessible treats a missing thread as shared.
		if !found {
			return true
		}
		return pmThreadVisibleForPrincipal(principal, agentActorID, owner)
	default:
		// Topics, boards, cards, documents, and their revisions are
		// workspace-readable; access keeps the existing live projection.
		return true
	}
}

// pmBackingThread resolves a backing thread reference that may carry a handle
// rather than an id. found=false covers absent threads (purged or unknown);
// ok=false is a storage error and fails closed.
func pmBackingThread(ctx context.Context, store *primitives.Store, threadID string) (found bool, pmOwner string, ok bool) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return false, "", true
	}
	_, id, err := store.CanonicalResolutionRefID(ctx, "thread:"+threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return false, "", true
		}
		return false, "", false
	}
	if id == "" {
		return false, "", true
	}
	thread, err := store.GetThread(ctx, id)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return false, "", true
		}
		return false, "", false
	}
	return true, anyString(thread["pm_actor_id"]), true
}

// pmThreadVisibleForPrincipal is canAccessPMThread keyed by an explicit
// principal so resolution redaction can answer for the requesting actor
// without an *http.Request. Threads without a PM owner stay workspace-readable.
func pmThreadVisibleForPrincipal(principal *auth.Principal, agentActorID, owner string) bool {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return true
	}
	if principal.ActorID == owner {
		return true
	}
	agentActorID = strings.TrimSpace(agentActorID)
	return agentActorID != "" && principal.ActorID == agentActorID
}
