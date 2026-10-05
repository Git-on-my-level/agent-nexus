package server

import (
	"context"
	"strings"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

// Scoped PM reads omit decisions containing inaccessible resources. Resolution
// summaries also recheck the shared predicate for direct proposal responses.

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
	ctx = primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: principal.ActorID, PMActorID: agentActorID})
	return store.CanAccessResource(ctx, "", ref)
}
