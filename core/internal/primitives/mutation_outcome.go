package primitives

import (
	"errors"
)

// mutationOutcomeAfterBegin classifies a failure raised after a mutation's
// transaction already holds the workspace writer. From that moment the
// process can no longer prove whether the change applied, so every
// non-typed failure must surface as MutationOutcomeUnknown and the caller
// must read canonical state before retrying. Deterministic request
// rejections stay typed: they answer the request unambiguously, and a retry
// converges on the same answer through the concurrency-token check.
func mutationOutcomeAfterBegin(err error) error {
	if err == nil {
		return nil
	}
	var outcome *MutationOutcomeUnknown
	var violation *QuotaViolation
	switch {
	case errors.As(err, &outcome),
		errors.As(err, &violation),
		errors.Is(err, ErrNotFound),
		errors.Is(err, ErrConflict),
		errors.Is(err, ErrAlreadyTrashed),
		errors.Is(err, ErrInvalidBoardRequest),
		errors.Is(err, ErrInvalidWorkRequest),
		errors.Is(err, ErrInvalidCursor),
		errors.Is(err, ErrInvalidResourceRef),
		errors.Is(err, ErrHandleAllocation):
		return err
	default:
		return &MutationOutcomeUnknown{Cause: err}
	}
}
