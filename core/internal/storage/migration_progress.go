package storage

import (
	"context"
	"log"
	"time"
)

// Migrations run before the listener opens. Emit a start and periodic elapsed
// signal so operators can distinguish work in progress from a hung process.
// This does not make an unmigrated database ready or expose record contents.
func migrationProgress(ctx context.Context, version int, interval time.Duration, emit func(string, ...any)) func() {
	start := time.Now()
	emit("migration_started version=%d", version)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				emit("migration_progress version=%d elapsed=%s", version, time.Since(start).Round(time.Millisecond))
			case <-ctx.Done():
				return
			case <-stop:
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-done
		emit("migration_finished version=%d elapsed=%s", version, time.Since(start).Round(time.Millisecond))
	}
}

func reportMigrationProgress(ctx context.Context, version int) func() {
	return migrationProgress(ctx, version, 5*time.Second, log.Printf)
}
