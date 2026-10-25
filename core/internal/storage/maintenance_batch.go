package storage

import (
	"context"
	"sync/atomic"
	"time"
)

// maintenanceChunkLease is the target duration of one maintenance chunk.
const maintenanceChunkLease = 100 * time.Millisecond

// Retain a learned cap across maintenance ticks. A chunk that overruns its
// lease still commits, and the next tick runs with fewer rows. Reopening may
// relearn the cap, but never loses committed cursor progress.
//
// The chunk's context is never cancelled. Cancelling a statement inside a
// write transaction can leave the driver connection open outside the pool
// with the SQLite write lock held, blocking every writer until the process
// exits. Chunks are bounded by their row limit instead, and their busy wait
// is capped by BeginMaintenanceChunkWithBusyWait.
type adaptiveMaintenanceBatch struct {
	limit atomic.Int64
	fast  atomic.Int64
}

func (b *adaptiveMaintenanceBatch) run(ctx context.Context, requested int, chunk func(context.Context, int) (bool, error)) (bool, error) {
	b.limit.CompareAndSwap(0, 200)
	limit := min(requested, int(b.limit.Load()))
	if err := ctx.Err(); err != nil {
		return false, err
	}
	start := time.Now()
	done, err := chunk(context.WithoutCancel(ctx), limit)
	// Parent cancellation and lock contention must not reduce the cap; only a
	// chunk that actually overran its lease does.
	elapsed := time.Since(start)
	if elapsed > maintenanceChunkLease && ctx.Err() == nil && maintenanceErrorClass(err) != "database busy" {
		b.fast.Store(0)
		reduced := int64(max(1, limit/2))
		for current := b.limit.Load(); current > reduced; current = b.limit.Load() {
			if b.limit.CompareAndSwap(current, reduced) {
				break
			}
		}
	} else if err == nil && ctx.Err() == nil && elapsed < maintenanceChunkLease/2 && int64(limit) == b.limit.Load() {
		// Recover from transient host contention. Keep the same 200-row ceiling
		// and require sixteen genuinely fast successful chunks before each rise.
		if b.fast.Add(1) >= 16 {
			b.fast.Store(0)
			current := b.limit.Load()
			b.limit.CompareAndSwap(current, min(200, current*2))
		}
	} else {
		b.fast.Store(0)
	}
	return done, err
}
