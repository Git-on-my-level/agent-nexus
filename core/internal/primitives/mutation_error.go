package primitives

// MutationOutcomeUnknown marks a failure raised after a mutation's transaction
// already held the workspace writer — a statement, commit, or post-commit read
// failed, so whether the change applied is no longer known to this process.
// Failures before BEGIN IMMEDIATE acquired the writer stay raw: nothing can
// have committed yet. Callers must read canonical state before deciding
// whether to retry.
type MutationOutcomeUnknown struct{ Cause error }

func (e *MutationOutcomeUnknown) Error() string { return e.Cause.Error() }
func (e *MutationOutcomeUnknown) Unwrap() error { return e.Cause }
