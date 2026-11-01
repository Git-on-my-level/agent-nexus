// Work-lease keepalive for a claimed PM turn: the lease loss watcher and the
// heartbeat loop with its retry interval clamping.

package app

import (
	"context"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) watchTurnLease(runCtx context.Context, cancel context.CancelFunc, cfg config.Resolved, turn map[string]any) *atomic.Bool {
	lost := &atomic.Bool{}
	if a == nil || a.turnMem().heartbeatOff() || turn == nil {
		return lost
	}
	turnID := anyString(turn["id"])
	token := anyString(turn["lease_token"])
	if turnID == "" || token == "" || cancel == nil {
		return lost
	}
	interval := leaseHeartbeatInterval(turn)
	if progressFromContext(runCtx) != nil && interval > 5*time.Second {
		interval = 5 * time.Second
	}
	retry := heartbeatRetryStart
	go func() {
		first := interval
		if progressFromContext(runCtx) != nil {
			first = 0
		}
		timer := time.NewTimer(first)
		defer timer.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-timer.C:
				disable, leaseLost, next := a.heartbeatTurn(runCtx, cfg, turnID, token)
				if disable {
					if a.turnMem().disableHeartbeat() {
						a.pmLog("pm serve: lease heartbeat not supported by this core; disabling heartbeats\n")
					}
					return
				}
				if leaseLost {
					a.pmLog("pm serve: turn %s lease lost\n", turnID)
					lost.Store(true)
					cancel()
					return
				}
				if next > 0 {
					interval = next
					if progressFromContext(runCtx) != nil && interval > 5*time.Second {
						interval = 5 * time.Second
					}
					retry = heartbeatRetryStart
				} else {
					interval = retry
					nextRetry := retry * 2
					if nextRetry > heartbeatRetryCap {
						nextRetry = heartbeatRetryCap
					}
					retry = nextRetry
				}
				timer.Reset(interval)
			}
		}
	}()
	return lost
}

func (a *App) heartbeatTurn(ctx context.Context, cfg config.Resolved, turnID, leaseToken string) (disable, lost bool, next time.Duration) {
	if a.turnMem().heartbeatOff() {
		return false, false, 0
	}
	body := map[string]any{"lease_token": leaseToken}
	progress := progressFromContext(ctx)
	if progress != nil {
		body = progress.body(leaseToken)
	}
	res, err := a.invokeRawJSON(ctx, cfg, "pm turns heartbeat", "POST", "/pm/turns/"+url.PathEscape(turnID)+"/heartbeat", body)
	if err != nil && httpStatusFromErr(err) == http.StatusBadRequest && progress != nil {
		progress.disable()
		res, err = a.invokeRawJSON(ctx, cfg, "pm turns heartbeat", "POST", "/pm/turns/"+url.PathEscape(turnID)+"/heartbeat", map[string]any{"lease_token": leaseToken})
	}
	if err != nil {
		if httpStatusFromErr(err) == http.StatusNotFound {
			return true, false, 0
		}
		if terminalLeaseLost(err) {
			return false, true, 0
		}
		if ctx.Err() != nil {
			return false, false, 0
		}
		a.pmLog("pm serve: turn %s heartbeat failed: %v\n", turnID, err)
		return false, false, 0
	}
	responseBody := commandResultBody(res)
	if expires, ok := parseRFC3339Timestamp(anyString(responseBody["lease_expires_at"])); ok {
		if remain := expires.Sub(nowFn()); remain > 0 {
			return false, false, clampLeaseHeartbeatInterval(remain / 2)
		}
	}
	return false, false, defaultLeaseHeartbeatInterval
}

func (a *App) handleHeartbeatLeaseLoss(ctx, shutdownCtx context.Context, cfg config.Resolved, workDir string, turn map[string]any) (bool, bool) {
	lost := errnorm.New(errnorm.KindRemote, "lease_mismatch", "lease lost")
	return a.recoverLostLease(ctx, shutdownCtx, cfg, workDir, turn, lost)
}

func leaseHeartbeatInterval(turn map[string]any) time.Duration {
	if expires, ok := parseRFC3339Timestamp(anyString(turn["lease_expires_at"])); ok {
		if remain := expires.Sub(nowFn()); remain > 0 {
			return clampLeaseHeartbeatInterval(remain / 2)
		}
	}
	return defaultLeaseHeartbeatInterval
}

func clampLeaseHeartbeatInterval(d time.Duration) time.Duration {
	if d < minLeaseHeartbeatInterval {
		return minLeaseHeartbeatInterval
	}
	return d
}
