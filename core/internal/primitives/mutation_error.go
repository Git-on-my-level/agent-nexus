package primitives

// MutationOutcomeUnknown marks a commit error or a failure after commit. Other
// MoveBoardCard/PatchWork errors establish that the requested write did not commit.
// Callers must read canonical state before deciding whether to retry.
type MutationOutcomeUnknown struct{ Cause error }

func (e *MutationOutcomeUnknown) Error() string { return e.Cause.Error() }
func (e *MutationOutcomeUnknown) Unwrap() error { return e.Cause }
