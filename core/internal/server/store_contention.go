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
	// internal fault: nothing can have committed. This stage boundary is a
	// store-layer invariant for exactly the mutations wired to this
	// classifier (board card create/batch-add/update/move/archive/trash/
	// remove and the work mutations behind workStoreError): each wraps every
	// post-begin failure as MutationOutcomeUnknown, answered above, so a raw
	// context error reaching this branch is pre-write by construction —
	// before BEGIN, or inside its busy wait where an interrupted wait
	// usually surfaces as the coded BUSY below instead. Keep it retryable so
	// callers refresh their concurrency token and retry instead of treating
	// core as broken. A mutation added here without the store-side wrap
	// would reintroduce the retry-safety hole; the seam test in primitives
	// and the queued-move integration test lock the boundary from both ends.
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
