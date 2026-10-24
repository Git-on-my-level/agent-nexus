package server

import (
	"errors"
	"net/http"
)

// Only SQLite's typed BUSY/LOCKED results prove contention here. Cancellation
// alone cannot prove rollback; uncertain outcomes must not invite blind retries.
// The event can already be durable when replay persistence contends: retaining
// the original request key/event identity lets the conflict recovery replay it.
func writeEventAppendContentionError(w http.ResponseWriter, r *http.Request, err error) bool {
	if writeMutationOutcomeUnknownError(w, r, err) {
		return true
	}
	var coded interface{ Code() int }
	if !errors.As(err, &coded) || (coded.Code()&255 != 5 && coded.Code()&255 != 6) {
		return false
	}
	logUnmappedWorkError(w, r, err)
	w.Header().Set("Retry-After", "1")
	payload := errorPayload("busy", "workspace writer is busy; wait and retry the original event request")
	payload["hint"] = "Wait for Retry-After, then retry the same body with the same request_key and event identity."
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": payload})
	return true
}
