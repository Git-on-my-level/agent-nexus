package storage

type compactMismatchError struct{ check string }

func (e *compactMismatchError) Error() string {
	return "compact access verification failed: " + e.check
}
func (e *compactMismatchError) Unwrap() error { return ErrCompactAccessMismatch }
