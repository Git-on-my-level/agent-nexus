package storage

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAdaptiveMaintenanceBatchCancellationAndMinimum(t *testing.T) {
	var batch adaptiveMaintenanceBatch
	_, err := batch.run(context.Background(), 3, func(ctx context.Context, limit int) (bool, error) {
		if limit != 3 {
			t.Fatalf("initial limit=%d", limit)
		}
		<-ctx.Done()
		return false, sql.ErrTxDone // SQL drivers need not return DeadlineExceeded.
	})
	if !errors.Is(err, sql.ErrTxDone) || batch.limit.Load() != 1 {
		t.Fatalf("err=%v cap=%d", err, batch.limit.Load())
	}
	done, err := batch.run(context.Background(), 200, func(ctx context.Context, limit int) (bool, error) {
		if limit != 1 {
			t.Fatalf("minimum cap was lost: %d", limit)
		}
		return true, nil
	})
	if err != nil || !done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	for _, cancelled := range []bool{false, true} {
		var unchanged adaptiveMaintenanceBatch
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		}
		_, _ = unchanged.run(ctx, 200, func(ctx context.Context, limit int) (bool, error) { return false, sql.ErrTxDone })
		cancel()
		if unchanged.limit.Load() != 200 {
			t.Fatalf("non-deadline failure reduced cap=%d", unchanged.limit.Load())
		}
	}
}

func TestAdaptiveMaintenanceSmallCallerDoesNotLowerCap(t *testing.T) {
	var batch adaptiveMaintenanceBatch
	for _, requested := range []int{1, 200} {
		_, err := batch.run(context.Background(), requested, func(ctx context.Context, limit int) (bool, error) {
			if limit != requested {
				t.Fatalf("requested=%d limit=%d", requested, limit)
			}
			return false, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if batch.limit.Load() != 200 {
		t.Fatalf("successful small call lowered cap=%d", batch.limit.Load())
	}
}
