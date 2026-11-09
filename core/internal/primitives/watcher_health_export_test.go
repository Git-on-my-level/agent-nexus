package primitives

import (
	"context"
	"time"
)

func (s *Store) WatcherLoopFactsForTest(ctx context.Context, id string, now time.Time) (int, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	f, _, err := watcherLoopFacts(ctx, tx, id, now)
	return len(f.Runs), f.LastEffect != nil, err
}
