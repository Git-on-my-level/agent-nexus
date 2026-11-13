package server

import (
	"context"
	"log"
	"strings"

	"agent-nexus-core/internal/primitives"
)

// withdrawSupersededAsk closes the ask a new one names in payload.supersedes,
// so replacing a question leaves one row for the human rather than two. Only
// the requester's own open ask is closed; anything else (another requester's
// ask, one already answered, an unknown id) is left as it is. The new ask is
// already stored, so a failure here is logged and never fails the filing.
func withdrawSupersededAsk(ctx context.Context, opts handlerOptions, actorID string, stored map[string]any) {
	payload, _ := stored["payload"].(map[string]any)
	old := strings.TrimSpace(anyString(payload["supersedes"]))
	if !strings.HasPrefix(old, "event:") {
		return
	}
	oldID := strings.TrimPrefix(old, "event:")
	store, ok := opts.primitiveStore.(interface {
		AppendHumanAttentionWithdrawal(context.Context, string, string, map[string]any) (map[string]any, error)
	})
	if !ok || oldID == "" || oldID == anyString(stored["id"]) {
		return
	}
	requester := firstNonEmptyString(payload["requester_actor_id"], actorID)
	event := map[string]any{
		"type":       humanAttentionWithdrawnEventType,
		"summary":    "Ask withdrawn by requester",
		"provenance": stored["provenance"],
		"payload":    map[string]any{"reason": "Superseded by event:" + anyString(stored["id"])},
	}
	if _, err := store.AppendHumanAttentionWithdrawal(ctx, requester, oldID, event); err != nil && err != primitives.ErrNotFound && err != primitives.ErrForbidden && err != primitives.ErrHumanAttentionAlreadyResponded {
		log.Printf("withdraw superseded ask failed (ask=%s): %v", oldID, err)
	}
}
