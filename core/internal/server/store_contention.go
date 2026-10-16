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
	// A mutation whose request ends (caller gave up or its deadline expired)
	// before the store acquired the workspace writer is contention, not an
	// internal fault: nothing can have committed. Board card create and
	// batch-add leave a rolled-back statement error raw, so a busy or locked
	// failure after BEGIN is the coded branch below (503, recoverable) and a
	// deterministic statement failure stays a 500 with its chain logged.
	// Their only unknown outcomes are a failed Commit and a failed blob
	// promote. The other mutations wired here (card update/move/archive/
	// trash/remove and work) still wrap every post-begin failure as
	// MutationOutcomeUnknown, answered above, so a raw context error on
	// those paths is pre-write by construction. Keep that retryable. The
	// seam test in primitives and the queued-move integration test lock
	// that boundary.
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
	emitServerFault(w, r, http.StatusConflict, err, "mutation outcome is unknown; the change may already be committed")
	writeError(w, http.StatusConflict, "outcome_unknown", "mutation outcome is unknown; the change may already be committed")
	return true
}
