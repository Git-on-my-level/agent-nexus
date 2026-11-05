package server

import (
	"net/http"

	"agent-nexus-core/internal/auth"
)

// agentArchivedMessage is what an agent sees when it uses an archived persona
// name. Archived names stay reserved; only a human can bring one back.
const agentArchivedMessage = "this agent name is archived on this host; reuse an active name on this host, or ask a human to restore it on the Access page"

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
