package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// Explicit old identity/options select the rollback executor. Installed host
// units clear ANX_AS and use bare `bridge run`; they always serve the registry.
func legacyAnswerBridgeInvocation(args []string, cfg config.Resolved) bool {
	return cfg.As != "" || len(args) > 1
}

// Keep original service argv runnable across an in-place upgrade. This is only
// the answers lane, using the same fenced consumer and durable outcomes as the
// host supervisor. It neither edits the host registry nor publishes presence.
func (a *App) runLegacyAnswerBridge(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("bridge run")
	timeout := fs.Duration("command-timeout", 30*time.Minute, "Legacy answer command timeout")
	attempts := fs.Int("max-attempts", 5, "Legacy answer attempts (1..20)")
	if e := fs.Parse(args); e != nil {
		return nil, errnorm.Usage("invalid_flags", e.Error())
	}
	if len(fs.Args()) != 0 || !agentNamePattern.MatchString(cfg.As) || *timeout <= 0 || *attempts < 1 || *attempts > 20 {
		return nil, errnorm.Usage("invalid_args", "legacy bridge run needs --as <agent>, a positive --command-timeout and --max-attempts 1..20; use bare bridge run for the host service")
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	w, agent, e := a.bridgeRegistration(ctx, cfg)
	if e != nil {
		return nil, e
	}
	cfg, e = a.validateBridgeAgent(ctx, w, agent)
	if e != nil {
		return nil, e
	}
	dir, e := a.askRegistryDir(cfg)
	if e != nil {
		return nil, e
	}
	agent.RegistryDirs = []string{dir}
	agent.Policy.Timeout, agent.Policy.MaxAttempts = *timeout, *attempts
	fmt.Fprintln(a.Stderr, "anx bridge: legacy answers-only rollback mode; retry anx bridge install to consolidate this host")
	for ctx.Err() == nil {
		if cfg, e = a.validateBridgeAgent(ctx, w, agent); e == nil {
			e = checkResumeNamespace(dir, w, agent)
		}
		if e == nil {
			e = a.consumeHostWakes(ctx, cfg, agent, make(chan struct{}, 1))
		}
		if e != nil && ctx.Err() == nil {
			fmt.Fprintf(a.Stderr, "anx bridge: legacy answers paused/reconnecting code=%s\n", errnorm.Normalize(e).Code)
		}
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	return &commandResult{Data: map[string]any{"stopped": true}}, nil
}
