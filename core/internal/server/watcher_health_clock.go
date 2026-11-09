package server

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"time"

	"agent-nexus-core/internal/loopstats"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

// RunWorkTrackingClocks shares cancellation and joins both maintenance workers.
func RunWorkTrackingClocks(ctx context.Context, tracking *WorkTrackingClock, runtime *PMRuntime) {
	done := make(chan struct{})
	go func() { defer close(done); RunWatcherHealthClock(ctx, tracking.Store, runtime) }()
	tracking.Run(ctx)
	<-done
}

// RunWatcherHealthClock is independent of the PM runner and its claim loop.
func RunWatcherHealthClock(ctx context.Context, store *primitives.Store, runtime *PMRuntime) {
	cfg := primitives.DefaultWatcherHealthConfig()
	cfg.PMOfflineAfter = watcherDuration("ANX_PM_OFFLINE_ALARM_AFTER", cfg.PMOfflineAfter)
	cfg.AgentActivityWithin = watcherDuration("ANX_WATCHER_AGENT_ACTIVITY_WITHIN", cfg.AgentActivityWithin)
	cfg.RepeatInitial = watcherDuration("ANX_WATCHER_REPEAT_INITIAL", cfg.RepeatInitial)
	cfg.RepeatMax = max(cfg.RepeatInitial, watcherDuration("ANX_WATCHER_REPEAT_MAX", cfg.RepeatMax))
	cfg.LoopDigestCadences = watcherInt("ANX_LOOP_HEALTH_DIGEST_CADENCES", cfg.LoopDigestCadences)
	cfg.LoopDecideCadences = max(cfg.LoopDigestCadences, watcherInt("ANX_LOOP_HEALTH_DECIDE_CADENCES", cfg.LoopDecideCadences))
	var service *pm.Service
	if runtime != nil {
		service = runtime.Service
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := WatcherHealthStep(ctx, store, service, time.Now().UTC(), cfg); err != nil && ctx.Err() == nil {
			log.Printf("watcher health: bounded maintenance retry required")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func WatcherHealthStep(ctx context.Context, store *primitives.Store, service *pm.Service, now time.Time, cfg primitives.WatcherHealthConfig) error {
	ctx, finish := loopstats.Start(ctx, "watcher_health")
	defer finish()
	var pmErr error
	if service != nil {
		// Expire before examining the signal, even with no connected runner.
		pmErr = service.ExpireTurns(ctx, now)
		if pmErr == nil {
			unhealthy, err := service.WatcherUnhealthy(ctx, now, cfg.PMOfflineAfter)
			pmErr = err
			if err == nil {
				pmErr = store.ReconcilePMHealth(ctx, unhealthy, now, cfg)
			}
		}
	}
	return errors.Join(pmErr, store.MaintainLoopHealth(ctx, now, cfg))
}
func watcherDuration(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v < time.Second || v > 365*24*time.Hour {
		log.Printf("invalid %s; using default", name)
		return fallback
	}
	return v
}
func watcherInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 1000 {
		log.Printf("invalid %s; using default", name)
		return fallback
	}
	return n
}
