package primitives

import (
	"context"

	"agent-nexus-core/internal/resourceaccess"
)

// RunBatch runs fn inside one write transaction that also owns the
// workspace quota lock. Mutations fn makes through this store join that
// transaction as savepoints: a batch waits for the write lock once and
// commits once, and an item that fails rolls back only its own savepoint.
// The lock order matches every single mutation: quota, then transaction.
func (s *Store) RunBatch(ctx context.Context, fn func(context.Context) error) error {
	if s.quota.enabled() && !replayOwnsQuota(ctx, s) {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		ctx = context.WithValue(ctx, replayQuotaKey{}, s)
	}
	return s.db.WriteTransaction(ctx, func(ctx context.Context, _ *resourceaccess.Tx) error {
		return fn(ctx)
	})
}
