package server

import (
	"net/http"
	"time"
)

// handleGetBriefV1 serves brief.v1: the caller's decisions, initiatives
// against their promises, own asks and answers, for this workspace only.
func handleGetBriefV1(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, map[string]any{
		"schema":           "brief.v1",
		"generated_at":     now.Format(time.RFC3339),
		"principal":        map[string]any{"id": overviewPrincipal(r), "display": overviewPrincipal(r)},
		"decisions":        []any{},
		"initiatives":      []any{},
		"your_asks":        []any{},
		"answered_for_you": []any{},
		"truncated":        map[string]bool{"decisions": false, "initiatives": false, "your_asks": false, "answered_for_you": false},
	})
}
