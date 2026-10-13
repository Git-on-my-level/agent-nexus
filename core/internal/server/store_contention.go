package server

import (
	"context"
	"errors"
	"net/http"

	"agent-nexus-core/internal/primitives"
)

// Classify wrapped SQLite result codes, including extended BUSY/LOCKED codes.
// Never match error text: submitted data can occur in arbitrary store errors.
func writeStoreContentionError(w http.ResponseWriter, r *http.Request, err error) bool {
	if writeMutationOutcomeUnknownError(w, r, err) {
		return true
	}
	// A mutation that ends because its request ended (caller gave up or its
	// deadline expired) while the store call waited on the workspace writer is
	// contention, not an internal fault. An interrupted busy wait usually
	// surfaces as a coded BUSY below; the raw context error reaches this branch
	// when the cancellation lands outside it. Keep it retryable so callers
	// refresh their concurrency token and retry instead of treating core as
	// broken. A request that ends after a commit attempt cannot reach this
	// branch: writeMutationOutcomeUnknownError above classifies it
	// outcome_unknown, so "before the store operation finished" stays true.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		logUnmappedWorkError(w, r, err)
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "busy", "request ended before the store operation finished; refresh the concurrency token and retry")
		return true
	}
	var coded interface{ Code() int }
	if !errors.As(err, &coded) || (coded.Code()&255 != 5 && coded.Code()&255 != 6) {
		return false
	}
	logUnmappedWorkError(w, r, err)
	w.Header().Set("Retry-After", "1")
	writeError(w, http.StatusServiceUnavailable, "busy", "workspace writer is busy; wait, refresh the board token, and retry")
	return true
}

func writeMutationOutcomeUnknownError(w http.ResponseWriter, r *http.Request, err error) bool {
	var uncertain *primitives.MutationOutcomeUnknown
	if !errors.As(err, &uncertain) {
		return false
	}
	logUnmappedWorkError(w, r, err)
	writeError(w, http.StatusConflict, "outcome_unknown", "mutation outcome is unknown; the change may already be committed")
	return true
}
