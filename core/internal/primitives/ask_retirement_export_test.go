package primitives

import "context"

func RequireAskVisibleEventForTest(s *Store, ctx context.Context, requester, eventID string) error {
	return s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		return requireAskVisibleEvent(ctx, tx, requester, eventID)
	})
}
