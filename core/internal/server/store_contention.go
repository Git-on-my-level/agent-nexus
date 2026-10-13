package server

import (
	"errors"
	"net/http"

	"agent-nexus-core/internal/primitives"
)

// Classify wrapped SQLite result codes, including extended BUSY/LOCKED codes.
// Never match error text: submitted data can occur in arbitrary store errors.
func writeStoreContentionError(w http.ResponseWriter, r *http.Request, err error) bool {
	var coded interface{ Code() int }
	if !errors.As(err, &coded) || (coded.Code()&255 != 5 && coded.Code()&255 != 6) {
		return false
	}
	logUnmappedWorkError(w, r, err)
	var uncertain *primitives.MutationOutcomeUnknown
	if errors.As(err, &uncertain) {
		writeError(w, http.StatusConflict, "conflict", "move outcome is uncertain; read the card and board before deciding whether to retry")
		return true
	}
	w.Header().Set("Retry-After", "1")
	writeError(w, http.StatusServiceUnavailable, "busy", "workspace writer is busy; wait, refresh the board token, and retry")
	return true
}
