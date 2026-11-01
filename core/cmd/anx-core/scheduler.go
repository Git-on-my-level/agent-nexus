// Background maintenance scheduling: runEvery (bounded interval runner),
// runDailyAtUTC, and the auth row purge timeout that bounds one purge run.

package main

import (
	"context"
	"time"
)

// authRowPurgeTimeout bounds one auth row purge run; batches commit
// individually, so a run cut short keeps what it deleted.
const authRowPurgeTimeout = 5 * time.Minute

// runEvery runs fn once per interval, each run bounded by timeout.
func runEvery(ctx context.Context, interval time.Duration, timeout time.Duration, fn func(context.Context)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCtx, cancel := context.WithTimeout(ctx, timeout)
			fn(runCtx)
			cancel()
		}
	}
}

func runDailyAtUTC(ctx context.Context, hour int, minute int, fn func(context.Context)) {
	if fn == nil {
		return
	}
	timer := time.NewTimer(durationUntilNextUTCRun(time.Now().UTC(), hour, minute))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			runCtx, cancel := context.WithTimeout(ctx, time.Minute)
			fn(runCtx)
			cancel()
			timer.Reset(durationUntilNextUTCRun(time.Now().UTC(), hour, minute))
		}
	}
}

func durationUntilNextUTCRun(now time.Time, hour int, minute int) time.Duration {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next.Sub(now)
}
