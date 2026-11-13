package server

import (
	"net/http"
	"strings"
)

// Top-level goals are a human's to set. A human creates or promotes one
// directly. An agent may do it when a human asked, and says who: the name is
// recorded with the change, where the human reads it under recent changes.
// An agent acting on its own is redirected rather than refused: a new card
// lands in Unsorted, where it can be grouped under an existing goal or
// proposed to a human with an ask.
const initiativeRequestedByMax = 120

// agentActsWithoutHumanRequest reports whether the caller is an agent that
// named no requesting human, and returns the trimmed name otherwise.
func agentActsWithoutHumanRequest(r *http.Request, raw map[string]any) (string, bool) {
	requestedBy := strings.TrimSpace(anyString(raw["requested_by"]))
	if len(requestedBy) > initiativeRequestedByMax {
		requestedBy = requestedBy[:initiativeRequestedByMax]
	}
	principal, ok := cachedAuthenticatedPrincipal(r)
	return requestedBy, ok && isAgentPrincipal(principal) && requestedBy == ""
}

// redirectAgentInitiativeCreate decides where an agent's top-level create
// lands. Unrequested, it becomes an Unsorted create (redirected). Requested,
// the card is created in Unsorted too and then promoted with a reason naming
// who asked, so the new goal appears in recent changes with its provenance.
// The create itself never carries initiative authority: an agent's card on an
// initiatives-role board waits in Unsorted like any other. Recurring work
// (a cadence or a goal) is not a goal initiative and is left alone, as is
// every human create.
func redirectAgentInitiativeCreate(r *http.Request, raw map[string]any) (redirected bool, promoteReason string) {
	requestedBy, unrequested := agentActsWithoutHumanRequest(r, raw)
	delete(raw, "requested_by")
	principal, ok := cachedAuthenticatedPrincipal(r)
	if raw["initiative"] != true || !ok || !isAgentPrincipal(principal) || raw["cadence"] != nil || raw["goal"] != nil {
		return false, ""
	}
	delete(raw, "initiative")
	if unrequested {
		return true, ""
	}
	return false, "Created as a top-level goal (requested by " + requestedBy + ")"
}

// initiativePromotionReason gates an agent's promotion of existing work to
// the top level. ok is false when no human was named; otherwise the returned
// reason carries who asked.
func initiativePromotionReason(r *http.Request, raw map[string]any) (string, bool) {
	requestedBy, unrequested := agentActsWithoutHumanRequest(r, raw)
	if unrequested {
		return "", false
	}
	reason := strings.TrimSpace(anyString(raw["reason"]))
	if requestedBy != "" {
		reason = strings.TrimSpace(reason + " (requested by " + requestedBy + ")")
	}
	return reason, true
}
