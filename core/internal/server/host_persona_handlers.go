package server

import (
	"context"
	"net/http"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/commandcenter"
)

// rosterForReader omits last_used_at (a principal's last authentication
// time) unless the reader is a human or auth-admin principal. Other readers
// may see the roster but not when each agent last authenticated. A request
// with no cached principal sees none (fail closed).
func rosterForReader(ctx context.Context, items []commandcenter.Summary) []commandcenter.Summary {
	principal, _ := ctx.Value(principalContextKey{}).(*auth.Principal)
	if isAuthAdminPrincipal(principal) {
		return items
	}
	for i := range items {
		items[i].LastUsedAt = nil
	}
	return items
}

// agentArchivedMessage is what an agent sees when it uses an archived persona
// name. Archived names stay reserved; only a human can bring one back.
const agentArchivedMessage = "a human archived this agent name on this host; reuse an active name on this host, or ask a human to restore it on the Access page"

// hostAssertionResponse is the host_assertion token envelope. registered and
// similar_names appear only when this call created the persona.
func hostAssertionResponse(agent auth.HostAgent, tokens auth.TokenBundle) map[string]any {
	out := map[string]any{"agent": agent, "tokens": tokens}
	if agent.Registered {
		out["registered"] = true
		out["similar_names"] = agent.SimilarNames
	}
	return out
}

// handleHostAgentArchive archives or restores one host persona. Human
// auth-admin bearer only: agents, including granted auth-admin agents, are
// refused before the store runs, and the store re-checks inside its
// transaction.
func handleHostAgentArchive(w http.ResponseWriter, r *http.Request, opts handlerOptions, hostID, name string, archive bool) {
	admin, ok := humanHostAdmin(w, r, opts)
	if !ok {
		return
	}
	var host auth.Host
	var err error
	if archive {
		host, err = opts.authStore.ArchiveHostAgent(r.Context(), hostID, name, *admin)
	} else {
		host, err = opts.authStore.RestoreHostAgent(r.Context(), hostID, name, *admin)
	}
	if err != nil {
		hostError(w, r, err)
		return
	}
	byID, err := hostRoster(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "runs_unavailable", "agent roster is unavailable")
		return
	}
	opts.agentChanges.publish()
	writeJSON(w, http.StatusOK, map[string]any{"host": hostWithRoster(host, byID)})
}
