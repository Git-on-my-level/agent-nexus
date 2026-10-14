package storage

import (
	"context"
	"errors"
	"fmt"
)

// CompactAccessMigrationError is safe to log in serving maintenance: it contains
// no SQL, identifiers, profile values or driver-provided error text. Unwrap
// preserves the cause for operator tooling and retry classification.
type CompactAccessMigrationError struct {
	Stage, Reason string
	SQLiteCode    int
	cause         error
}

func (e *CompactAccessMigrationError) Error() string {
	return fmt.Sprintf("stage=%s reason=%s sqlite_code=%d; current reader retained", e.Stage, e.Reason, e.SQLiteCode)
}
func (e *CompactAccessMigrationError) Unwrap() error { return e.cause }

type compactMismatchError struct{ check string }

func (e *compactMismatchError) Error() string {
	return "compact access verification failed: " + e.check
}
func (e *compactMismatchError) Unwrap() error { return ErrCompactAccessMismatch }

func compactMigrationError(stage string, err error) error {
	if err == nil {
		return nil
	}
	var existing *CompactAccessMigrationError
	if errors.As(err, &existing) {
		return existing
	}
	e := &CompactAccessMigrationError{Stage: stage, Reason: "unexpected_error", cause: err}
	var code interface{ Code() int }
	var mismatch *compactMismatchError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		e.Reason = "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		e.Reason = "cancelled"
	case errors.As(err, &mismatch):
		e.Reason = mismatch.check + "_mismatch_requires_offline_verification"
	case errors.Is(err, ErrCompactAccessMismatch):
		e.Reason = "ledger_mismatch_requires_offline_verification"
	case errors.Is(err, ErrCompactAccessChanged):
		e.Reason = "ledger_changed_retry"
	case errors.As(err, &code):
		e.SQLiteCode = code.Code()
		e.Reason = "sqlite_error"
		if e.SQLiteCode&255 == 5 || e.SQLiteCode&255 == 6 {
			e.Reason = "write_contention_retry"
		}
	}
	return e
}
