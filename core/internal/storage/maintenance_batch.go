package storage

import (
	"context"
	"sync/atomic"
	"time"
)

// Retain a learned cap across maintenance ticks. A cancelled chunk rolls back,
// then the next tick retries from the durable cursor with fewer rows. Reopening
// may relearn the cap, but never loses committed cursor progress.
type adaptiveMaintenanceBatch struct{ limit atomic.Int64 }

func (b *adaptiveMaintenanceBatch) run(ctx context.Context, requested int, chunk func(context.Context, int) (bool, error)) (bool, error) {
	b.limit.CompareAndSwap(0, 200)
	limit := min(requested, int(b.limit.Load()))
	chunkCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	done, err := chunk(chunkCtx, limit)
	// Drivers may return a cancellation-related SQL error instead of the context
	// error. Inspect the chunk deadline; parent cancellation and lock contention
	// must not reduce the cap.
	if err != nil && chunkCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		reduced := int64(max(1, limit/2))
		for current := b.limit.Load(); current > reduced; current = b.limit.Load() {
			if b.limit.CompareAndSwap(current, reduced) {
				break
			}
		}
	}
	return done, err
}
