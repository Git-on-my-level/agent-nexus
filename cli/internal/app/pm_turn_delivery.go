// Local reply files for turns whose complete call failed: writing, reading
// and cleaning up undelivered replies, and giving up on undeliverable turns.

package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func turnReplyPath(workDir, turnID string) string {
	return filepath.Join(workDir, "turn-"+sanitizeFilePart(turnID)+".reply.md")
}

func writeTurnReply(workDir, turnID, text string) (string, error) {
	path := turnReplyPath(workDir, turnID)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return path, err
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs, nil
	}
	return path, nil
}

func readTurnReply(workDir, turnID string) (string, error) {
	raw, err := os.ReadFile(turnReplyPath(workDir, turnID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "", fmt.Errorf("empty or corrupt")
	}
	return text, nil
}

func removeTurnReply(workDir, turnID string) {
	if strings.TrimSpace(workDir) == "" || strings.TrimSpace(turnID) == "" {
		return
	}
	_ = os.Remove(turnReplyPath(workDir, turnID))
}

func (a *App) saveTurnReplyFile(workDir, turnID, text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	path, err := writeTurnReply(workDir, turnID, text)
	if err != nil {
		a.pmLog("pm serve: turn %s could not save reply: %v\n", turnID, err)
		return ""
	}
	return path
}

func (a *App) handleFailedComplete(ctx, shutdownCtx context.Context, cfg config.Resolved, workDir string, turn map[string]any, err error) (settled bool, retryHarness bool) {
	turnID := anyString(turn["id"])
	a.pmLog("pm serve: turn %s complete could not be delivered: %v\n", turnID, err)
	if a.turnMem().noteDeliveryAttempt(turnID) >= maxDeliveryAttempts {
		return a.failUndeliverableReply(ctx, shutdownCtx, cfg, workDir, turn, err), false
	}
	if terminalLeaseLost(err) {
		got, getErr := a.invokeRawJSON(ctx, cfg, "pm turns get", "GET", "/pm/turns/"+url.PathEscape(turnID), nil, map[string]string{"X-ANX-PM-Lease-Token": anyString(turn["lease_token"])})
		if getErr == nil {
			current := commandResultBody(got)
			if current != nil && turnIsTerminal(current) {
				removeTurnReply(workDir, turnID)
				a.turnMem().observeTerminal(turnID)
				a.pmLog("pm serve: turn %s already %s; nothing to do\n", turnID, firstNonEmpty(anyString(current["status"]), "unknown"))
				return true, false
			}
		}
		return true, false
	}
	a.finishUndeliveredComplete(ctx, cfg, workDir, turn)
	return true, false
}

func (a *App) failUndeliverableReply(ctx, shutdownCtx context.Context, cfg config.Resolved, workDir string, turn map[string]any, completeErr error) bool {
	turnID := anyString(turn["id"])
	leaseToken := anyString(turn["lease_token"])
	if cause := shortCompleteCause(completeErr); cause != "" {
		a.pmLog("pm serve: turn %s undeliverable: %s\n", turnID, cause)
	}
	a.pmLog("pm serve: turn %s giving up after %d undeliverable complete attempts; failing the turn\n", turnID, maxDeliveryAttempts)
	_, failErr := a.failTurnUntil(ctx, shutdownCtx, cfg, turnID, leaseToken, undeliverableReplyReason, parseTurnDeadline(turn))
	removeTurnReply(workDir, turnID)
	if failErr == nil {
		a.turnMem().observeTerminal(turnID)
		a.pmLog("pm serve: turn %s failed: %s\n", turnID, undeliverableReplyReason)
		return true
	}
	if shuttingDown(shutdownCtx) || errors.Is(failErr, context.Canceled) {
		return false
	}
	if terminalLeaseLost(failErr) {
		a.pmLog("pm serve: turn %s fail refused (%s); no longer ours\n", turnID, claimErrorLabel(failErr))
		a.turnMem().observeTerminal(turnID)
		return true
	}
	a.pmLog("pm serve: turn %s fail after undeliverable complete failed: %v; releasing lease\n", turnID, failErr)
	a.releaseTurnBestEffort(ctx, cfg, turn, workDir, "undeliverable terminal call")
	a.turnMem().observeTerminal(turnID)
	return true
}

func shortCompleteCause(err error) string {
	if err == nil {
		return ""
	}
	var typed *errnorm.Error
	cause := strings.TrimSpace(err.Error())
	if errors.As(err, &typed) && typed != nil {
		cause = strings.TrimSpace(typed.Code)
		if msg := strings.TrimSpace(typed.Message); msg != "" {
			if cause != "" {
				cause += ": " + msg
			} else {
				cause = msg
			}
		}
	}
	cause = strings.Join(strings.Fields(cause), " ")
	return truncateToMaxBytes(cause, maxUndeliverableCauseBytes)
}

func (a *App) finishUndeliveredComplete(ctx context.Context, cfg config.Resolved, workDir string, turn map[string]any) {
	turnID := anyString(turn["id"])
	a.releaseTurnBestEffort(ctx, cfg, turn, workDir, "undeliverable terminal call")
	path := turnReplyPath(workDir, turnID)
	if abs, absErr := filepath.Abs(path); absErr == nil {
		path = abs
	}
	if _, statErr := os.Stat(path); statErr == nil {
		deadline := firstNonEmpty(anyString(turn["deadline"]), parseTurnDeadline(turn).UTC().Format(time.RFC3339))
		a.pmLog("pm serve: reply for turn %s saved to %s; lease released; the turn stays claimable until %s\n", turnID, path, deadline)
	}
}

func (a *App) giveUpOnTurn(ctx, shutdownCtx context.Context, cfg config.Resolved, workDir string, turn map[string]any, started time.Time) bool {
	if turn == nil {
		return true
	}
	turnID := anyString(turn["id"])
	a.turnMem().markGivenUp(turnID)
	reply, replyErr := readTurnReply(workDir, turnID)
	if replyErr == nil && reply != "" {
		return a.failUndeliverableReply(ctx, shutdownCtx, cfg, workDir, turn, nil)
	}
	n := a.turnMem().harnessRuns(turnID)
	if n < 1 {
		n = maxHarnessRunsPerTurn
	}
	if a.turnMem().consumeGiveUpLog(turnID) {
		a.pmLog("pm serve: giving up on turn %s after %d harness runs with no saved reply; failing the turn\n", turnID, n)
	}
	if started.IsZero() {
		started = nowFn()
	}
	return a.settleFailedTurn(ctx, shutdownCtx, cfg, workDir, turn, started, harnessGiveUpReason, nil, false)
}

func sanitizeFilePart(id string) string {
	out := make([]rune, 0, len(id))
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			out = append(out, r)
		} else {
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "turn"
	}
	return string(out)
}
