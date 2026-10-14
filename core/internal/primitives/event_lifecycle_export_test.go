package primitives

import "context"

// Export only to external-package tests so production retains a private reader.
func EventDescendantsForTest(s *Store, ctx context.Context, thread, parent string) ([]string, error) {
	return s.collectMessageDescendantIDs(ctx, thread, parent)
}
