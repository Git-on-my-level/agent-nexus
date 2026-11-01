// Completing, failing and releasing claimed PM turns: reply clipping, terminal
// call retries with backoff, lease recovery, and turn failure text.

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

var terminalRetryDelays = []time.Duration{
	time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
}

func (a *App) completeTurn(ctx context.Context, cfg config.Resolved, turnID, leaseToken, text string, raw []byte, maxBytes int) error {
	_, err := a.completeTurnUntil(ctx, nil, cfg, turnID, leaseToken, text, raw, maxBytes, time.Time{})
	return err
}

func (a *App) completeTurnUntil(ctx, shutdownCtx context.Context, cfg config.Resolved, turnID, leaseToken, text string, raw []byte, maxBytes int, deadline time.Time) (int, error) {
	return a.completeTurnAttempts(ctx, shutdownCtx, cfg, turnID, leaseToken, text, raw, maxBytes, deadline, 0)
}

func (a *App) completeTurnAttempts(ctx, shutdownCtx context.Context, cfg config.Resolved, turnID, leaseToken, text string, raw []byte, maxBytes int, deadline time.Time, maxAttempts int) (int, error) {
	text, refs := collectDeliberateEvidenceRefs(text, string(raw))
	clipped, dropped := clipReplyForTurn(text, maxBytes)
	if dropped > 0 {
		a.pmLog("pm serve: turn %s warning: reply truncated by anx pm serve at %d bytes; %d bytes were dropped\n", turnID, maxBytes, dropped)
		text = clipped
	}
	refs = a.filterResolvableEvidenceRefs(ctx, cfg, refs)
	body := map[string]any{
		"text":          text,
		"evidence_refs": refs,
		"lease_token":   leaseToken,
	}
	return a.retryTerminalCallLimited(ctx, shutdownCtx, cfg, turnID, "complete", "pm turns complete", "/pm/turns/"+url.PathEscape(turnID)+"/complete", body, deadline, maxAttempts)
}

func clipReplyForTurn(text string, maxBytes int) (string, int) {
	if maxBytes <= 0 {
		return "", len(text)
	}
	if len(text) <= maxBytes {
		return text, 0
	}
	dropped := len(text) - maxBytes
	for i := 0; i < 4; i++ {
		marker := fmt.Sprintf(replyTruncationMarkerTmpl, maxBytes, dropped)
		budget := maxBytes - len(marker)
		if budget < 0 {
			return truncateToMaxBytes(text, maxBytes), len(text) - minInt(maxBytes, len(text))
		}
		prefix := truncateToMaxBytes(text, budget)
		dropped = len(text) - len(prefix)
		marker = fmt.Sprintf(replyTruncationMarkerTmpl, maxBytes, dropped)
		if len(prefix)+len(marker) <= maxBytes {
			return prefix + marker, dropped
		}
	}
	return truncateToMaxBytes(text, maxBytes), len(text) - minInt(maxBytes, len(text))
}

func truncateToMaxBytes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	n := maxBytes
	for i := 0; i < utf8.UTFMax-1 && n > 0 && !utf8.RuneStart(text[n]); i++ {
		n--
	}
	if n == 0 {
		return ""
	}
	_, _ = utf8.DecodeLastRuneInString(text[:n])
	return text[:n]
}

func (a *App) releaseTurn(ctx context.Context, cfg config.Resolved, runnerID, turnID, leaseToken string) error {
	_, err := a.invokeRawJSON(ctx, cfg, "pm turns release", "POST", "/pm/turns/"+url.PathEscape(turnID)+"/release", map[string]any{
		"runner_id":   runnerID,
		"lease_token": leaseToken,
	})
	return err
}

func (a *App) failTurn(ctx context.Context, cfg config.Resolved, turnID, leaseToken, reason string) error {
	_, err := a.failTurnUntil(ctx, nil, cfg, turnID, leaseToken, reason, time.Time{})
	return err
}

func (a *App) failTurnUntil(ctx, shutdownCtx context.Context, cfg config.Resolved, turnID, leaseToken, reason string, deadline time.Time) (int, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "pm serve failed"
	}
	body := map[string]any{
		"reason":      reason,
		"lease_token": leaseToken,
	}
	return a.retryTerminalCall(ctx, shutdownCtx, cfg, turnID, "fail", "pm turns fail", "/pm/turns/"+url.PathEscape(turnID)+"/fail", body, deadline)
}

func (a *App) settleFailedTurn(ctx, shutdownCtx context.Context, cfg config.Resolved, workDir string, turn map[string]any, started time.Time, reason string, raw []byte, direct bool) bool {
	turnID := anyString(turn["id"])
	leaseToken := anyString(turn["lease_token"])
	if shuttingDown(shutdownCtx) {
		return false
	}
	if len(raw) > 0 {
		source := "agentctl"
		if direct {
			source = "runner"
		}
		a.pmLog("pm serve: turn %s %s: %s\n", turnID, source, strings.TrimSpace(string(raw)))
	}
	a.pmLog("pm serve: turn %s failed in %ds: %s\n", turnID, elapsedSeconds(started), reason)
	attempts, err := a.failTurnUntil(ctx, shutdownCtx, cfg, turnID, leaseToken, reason, parseTurnDeadline(turn))
	if err == nil {
		removeTurnReply(workDir, turnID)
		a.turnMem().observeTerminal(turnID)
		return true
	}
	if shuttingDown(shutdownCtx) || errors.Is(err, context.Canceled) {
		return false
	}
	if terminalLeaseLost(err) {
		settled, retry := a.recoverLostLease(ctx, shutdownCtx, cfg, workDir, turn, err)
		if retry {
			// Harness already failed; do not re-run. Fail again under the new lease, or release.
			leaseToken = anyString(turn["lease_token"])
			if _, failErr := a.failTurnUntil(ctx, shutdownCtx, cfg, turnID, leaseToken, reason, parseTurnDeadline(turn)); failErr != nil {
				a.pmLog("pm serve: turn %s fail after re-claim failed: %v; releasing lease\n", turnID, failErr)
				a.releaseTurnBestEffort(ctx, cfg, turn, workDir, "undeliverable terminal call")
			}
			return true
		}
		return settled
	}
	a.pmLog("pm serve: turn %s fail request failed after %d attempts: %v; releasing lease\n", turnID, attempts, err)
	a.releaseTurnBestEffort(ctx, cfg, turn, workDir, "undeliverable terminal call")
	return true
}

func (a *App) retryTerminalCall(ctx, shutdownCtx context.Context, cfg config.Resolved, turnID, verb, command, path string, body map[string]any, deadline time.Time) (int, error) {
	return a.retryTerminalCallLimited(ctx, shutdownCtx, cfg, turnID, verb, command, path, body, deadline, 0)
}

func (a *App) retryTerminalCallLimited(ctx, shutdownCtx context.Context, cfg config.Resolved, turnID, verb, command, path string, body map[string]any, deadline time.Time, maxAttempts int) (int, error) {
	started := nowFn()
	budgetEnd := started.Add(terminalRetryBudget)
	if !deadline.IsZero() && deadline.Before(budgetEnd) {
		budgetEnd = deadline
	}
	if maxAttempts <= 0 {
		maxAttempts = 1 + len(terminalRetryDelays)
	}
	var last error
	for attempt := 1; ; attempt++ {
		if shuttingDown(shutdownCtx) {
			if last != nil {
				return attempt - 1, last
			}
			return attempt - 1, context.Canceled
		}
		_, err := a.invokeRawJSON(ctx, cfg, command, "POST", path, body)
		if err == nil {
			return attempt, nil
		}
		last = err
		if terminalLeaseLost(err) || !terminalCallRetryable(err) || attempt >= maxAttempts {
			return attempt, err
		}
		if attempt > len(terminalRetryDelays) {
			return attempt, err
		}
		delay := terminalRetryDelays[attempt-1]
		if nowFn().Add(delay).After(budgetEnd) {
			return attempt, err
		}
		a.pmLog("pm serve: turn %s %s retry %d in %s: %v\n", turnID, verb, attempt, delay, err)
		sleepCtx := shutdownCtx
		if sleepCtx == nil {
			sleepCtx = ctx
		}
		if sleepErr := sleepFn(sleepCtx, delay); sleepErr != nil {
			return attempt, last
		}
	}
}

func terminalCallRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var typed *errnorm.Error
	if errors.As(err, &typed) {
		if strings.EqualFold(typed.Code, "lease_required") {
			return false
		}
		if typed.Kind == errnorm.KindNetwork {
			return true
		}
		status := httpStatusFromErr(typed)
		if status == http.StatusTooManyRequests || status >= 500 {
			return true
		}
		return false
	}
	return true
}

func terminalLeaseLost(err error) bool {
	var typed *errnorm.Error
	if !errors.As(err, &typed) {
		return false
	}
	switch strings.TrimSpace(typed.Code) {
	case "lease_mismatch", "turn_closed", "turn_not_claimed":
		return true
	}
	return false
}

func turnIsTerminal(turn map[string]any) bool {
	status := strings.ToLower(anyString(turn["status"]))
	return status == "delivered" || status == "failed"
}

func copyTurnFields(dst, src map[string]any) {
	if dst == nil || src == nil {
		return
	}
	for k := range dst {
		delete(dst, k)
	}
	for k, v := range src {
		dst[k] = v
	}
}

func runnerIDFromTurn(turn map[string]any, workDir string) string {
	id := firstNonEmpty(anyString(turn["lease_owner"]), anyString(turn["runner_id"]))
	if id != "" {
		return id
	}
	if strings.TrimSpace(workDir) == "" {
		return ""
	}
	id, err := loadOrCreateRunnerID(workDir)
	if err != nil {
		return ""
	}
	return id
}

func (a *App) releaseTurnBestEffort(ctx context.Context, cfg config.Resolved, turn map[string]any, workDir, reason string) {
	turnID := anyString(turn["id"])
	lease := anyString(turn["lease_token"])
	runnerID := runnerIDFromTurn(turn, workDir)
	if turnID == "" || lease == "" || runnerID == "" {
		a.pmLog("pm serve: turn %s cannot release lease (missing runner_id or lease_token)\n", turnID)
		return
	}
	if err := a.releaseTurn(ctx, cfg, runnerID, turnID, lease); err != nil {
		a.pmLog("pm serve: turn %s release failed: %v\n", turnID, err)
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "undeliverable terminal call"
	}
	a.pmLog("pm serve: released turn %s : %s\n", turnID, reason)
}

func (a *App) recoverLostLease(ctx, shutdownCtx context.Context, cfg config.Resolved, workDir string, turn map[string]any, lost error) (settled bool, retryHarness bool) {
	turnID := anyString(turn["id"])
	a.pmLog("pm serve: turn %s lease lost (%v); reading turn\n", turnID, lost)
	got, err := a.invokeRawJSON(ctx, cfg, "pm turns get", "GET", "/pm/turns/"+url.PathEscape(turnID), nil, map[string]string{"X-ANX-PM-Lease-Token": anyString(turn["lease_token"])})
	if err != nil {
		a.pmLog("pm serve: turn %s get after lease loss failed: %v; releasing lease\n", turnID, err)
		a.releaseTurnBestEffort(ctx, cfg, turn, workDir, "lease loss recovery failed")
		return true, false
	}
	current := commandResultBody(got)
	if current == nil {
		a.pmLog("pm serve: turn %s get after lease loss returned no object; releasing lease\n", turnID)
		a.releaseTurnBestEffort(ctx, cfg, turn, workDir, "lease loss recovery failed")
		return true, false
	}
	status := firstNonEmpty(anyString(current["status"]), "unknown")
	if turnIsTerminal(current) {
		removeTurnReply(workDir, turnID)
		a.turnMem().observeTerminal(turnID)
		a.pmLog("pm serve: turn %s already %s; nothing to do\n", turnID, status)
		return true, false
	}
	if shuttingDown(shutdownCtx) {
		return false, false
	}
	if a.turnMem().isGivenUp(turnID) || !a.turnMem().canRunHarness(turnID) {
		return a.giveUpOnTurn(ctx, shutdownCtx, cfg, workDir, turn, nowFn()), false
	}
	runnerID := runnerIDFromTurn(turn, workDir)
	claimed, claimErr := a.invokeRawJSON(ctx, cfg, "pm turns claim", "POST", "/pm/turns/claim", map[string]any{"runner_id": runnerID})
	if _, ok := parsePMClaimCapacity(claimed, claimErr); ok {
		a.pmLog("pm serve: turn %s still pending but not claimable; nothing to do\n", turnID)
		return true, false
	}
	if claimErr != nil {
		a.pmLog("pm serve: turn %s re-claim failed: %v; moving on\n", turnID, claimErr)
		return true, false
	}
	statusCode, _ := asMap(claimed.Data)["status_code"].(int)
	claimedTurn := commandResultBody(claimed)
	if statusCode == 204 || claimedTurn == nil {
		a.pmLog("pm serve: turn %s still pending but not claimable; nothing to do\n", turnID)
		return true, false
	}
	if anyString(claimedTurn["id"]) != turnID {
		a.pmLog("pm serve: re-claim returned %s instead of %s; releasing the new lease\n", anyString(claimedTurn["id"]), turnID)
		a.releaseTurnBestEffort(ctx, cfg, claimedTurn, workDir, "re-claim returned a different turn")
		return true, false
	}
	copyTurnFields(turn, claimedTurn)
	a.pmLog("pm serve: turn %s re-claimed after lease loss; retrying\n", turnID)
	return false, true
}

func elapsedSeconds(started time.Time) int {
	n := int(nowFn().Sub(started).Seconds())
	if n < 0 {
		return 0
	}
	return n
}

func humanTurnFailure(kind, extra string) string {
	switch kind {
	case "startup_timeout":
		return "The PM did not start in time (" + formatTimeoutPhrase(extra) + ")."
	case "await_failed":
		return "The PM did not reply before the deadline."
	case "no_assistant":
		return "The PM did not produce a reply."
	case "deadline":
		return "The PM did not reply before the deadline."
	default:
		return "The PM did not produce a reply."
	}
}

func harnessCmdFailure(err error) string {
	if err == nil {
		return humanTurnFailure("launch_failed", "")
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return humanTurnFailure("await_failed", "")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Sprintf("The PM did not produce a reply (the runner exited with status %d).", exitErr.ExitCode())
	}
	return "The PM did not produce a reply."
}

func formatTimeoutPhrase(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "the startup deadline"
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return raw
	}
	if d%time.Second == 0 && d < time.Minute {
		n := int(d / time.Second)
		if n == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", n)
	}
	return d.Round(time.Second).String()
}

func parseTurnDeadline(turn map[string]any) time.Time {
	raw := anyString(turn["deadline"])
	if raw == "" {
		return time.Now().Add(2 * time.Minute)
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts
	}
	return time.Now().Add(2 * time.Minute)
}

func turnByID(detail map[string]any, id string) map[string]any {
	if id == "" {
		return nil
	}
	rows, _ := detail["turns"].([]any)
	for _, row := range rows {
		item := asMap(row)
		if anyString(item["id"]) == id {
			return item
		}
	}
	return nil
}
