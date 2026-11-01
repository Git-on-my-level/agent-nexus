// Text projections for the PM-facing `anx work` reads: decisions, turns,
// conversations, dispatch actions and their attempt timestamps.

package app

import (
	"fmt"
	"strings"
	"time"
)

func formatPMDecisionGetText(root map[string]any) string {
	body := formatPrettyBody(root)
	if flag := decisionOperatorFlags(root); flag != "" {
		return flag + "\n" + body
	}
	return body
}

func decisionIsAwaiting(item map[string]any) bool {
	status := strings.ToLower(strings.TrimSpace(anyString(item["status"])))
	return status == "" || status == "awaiting_answer"
}

func decisionOperatorFlags(item map[string]any) string {
	if !decisionIsAwaiting(item) {
		return ""
	}
	var parts []string
	freshness := decisionFreshnessFlag(item)
	if v, ok := item["can_answer"].(bool); ok && !v && freshness == "" {
		parts = append(parts, "waiting on someone else")
	}
	if freshness != "" {
		parts = append(parts, freshness)
	}
	return strings.Join(parts, "\n")
}

func decisionFreshnessFlag(item map[string]any) string {
	if asBool(item["work_missing"]) {
		return "task missing"
	}
	if asBool(item["already_at_target"]) {
		return "already there"
	}
	if current, ok := item["target_current"].(bool); ok && !current {
		return "stale since proposal"
	}
	return ""
}

func formatPMTurnGetText(root map[string]any) string {
	status := renderPMTurnStatus(root)
	line := fmt.Sprintf("%s  status=%s", anyString(root["id"]), status)
	if claimedAt := anyString(root["claimed_at"]); claimedAt != "" {
		line += "  claimed_at=" + claimedAt
	}
	return line + fmt.Sprintf("  deadline=%s  failure=%s", anyString(root["deadline"]), anyString(root["failure"]))
}

func formatPMTurnClaimText(root map[string]any) string {
	if cap, ok := pmClaimCapacityFromMap(root); ok {
		return formatPMClaimCapacityText(cap)
	}
	line := fmt.Sprintf("%s  status=%s", anyString(root["id"]), renderPMTurnStatus(root))
	if runner := firstNonEmpty(anyString(root["lease_owner"]), anyString(root["runner_id"])); runner != "" {
		line += "  runner_id=" + runner
	}
	if token := anyString(root["lease_token"]); token != "" {
		line += "  lease_token=" + token
	}
	return line
}

func formatPMActionAcknowledgeText(root map[string]any) string {
	status := firstNonEmpty(anyString(root["status"]), "unknown")
	if asBool(root["closed_without_delivery"]) {
		status = "closed, nothing delivered"
	}
	return fmt.Sprintf("%s  status=%s  acknowledged_at=%s", anyString(root["id"]), status, anyString(root["acknowledged_at"]))
}

func renderPMTurnStatus(turn map[string]any) string {
	if strings.EqualFold(anyString(turn["failure_kind"]), "expired") {
		return "expired"
	}
	status := firstNonEmpty(anyString(turn["status"]), "unknown")
	if status != "sending" {
		return status
	}
	if asBool(turn["claimed"]) {
		return "in progress"
	}
	return "queued"
}

func conversationListStatus(item map[string]any) string {
	if turn := conversationLatestTurn(item); len(turn) > 0 {
		if status := renderPMTurnStatus(turn); status != "" && status != "unknown" {
			return status
		}
	}
	status := anyString(item["status"])
	if status == "" || status == "unknown" {
		return ""
	}
	return renderPMTurnStatus(item)
}

func conversationLatestTurn(item map[string]any) map[string]any {
	for _, key := range []string{"latest_turn", "last_turn", "turn"} {
		if turn := asMap(item[key]); len(turn) > 0 {
			return turn
		}
	}
	turns := asSlice(item["turns"])
	if len(turns) == 0 {
		return nil
	}
	return asMap(turns[len(turns)-1])
}

func formatPMActionReconcileText(root map[string]any) string {
	receipt := asMap(root["receipt"])
	line := fmt.Sprintf("%s  status=%s  receipt=%s", anyString(root["id"]), firstNonEmpty(anyString(root["status"]), "unknown"), firstNonEmpty(anyString(receipt["status"]), "unknown"))
	if detail := anyString(receipt["detail"]); detail != "" {
		line += "  " + detail
	}
	return line + fmt.Sprintf("  reconciliation_conflict=%t", root["reconciliation_conflict"] == true)
}

func formatPMDispatchText(root map[string]any, started time.Time, previousStatus string) string {
	receipt := asMap(root["receipt"])
	status := firstNonEmpty(anyString(root["status"]), "unknown")
	line := fmt.Sprintf("%s  status=%s  receipt=%s", anyString(root["id"]), status, firstNonEmpty(anyString(receipt["status"]), "unknown"))
	if detail := anyString(receipt["detail"]); detail != "" {
		line += "  " + detail
	}
	if note := pmDispatchSendNote(root, started, previousStatus); note != "" {
		line += "  " + note
	}
	return line
}

func actionHasSentAttempt(root map[string]any) bool {
	for _, row := range asSlice(root["attempts"]) {
		if anyString(asMap(row)["sent_at"]) != "" {
			return true
		}
	}
	return false
}

func parseRFC3339Timestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts, true
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts, true
	}
	return time.Time{}, false
}

func newestAttemptSentAt(root map[string]any) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, row := range asSlice(root["attempts"]) {
		parsed, ok := parseRFC3339Timestamp(anyString(asMap(row)["sent_at"]))
		if !ok {
			continue
		}
		if !found || parsed.After(latest) {
			latest = parsed
			found = true
		}
	}
	return latest, found
}

func pmStatusPending(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending_delivery", "pending":
		return true
	default:
		return false
	}
}

func pmDispatchCreatedAttempt(root map[string]any, started time.Time, previousStatus string) bool {
	if !started.IsZero() {
		if sentAt, ok := newestAttemptSentAt(root); ok && !sentAt.Before(started) {
			return true
		}
	}
	if !pmStatusPending(previousStatus) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(firstNonEmpty(anyString(root["status"]), ""))) {
	case "delivered", "acknowledged", "source_reported", "verified":
		return true
	default:
		return false
	}
}

func pmDispatchSendNote(root map[string]any, started time.Time, previousStatus string) string {
	status := strings.ToLower(strings.TrimSpace(firstNonEmpty(anyString(root["status"]), "")))
	if pmDispatchCreatedAttempt(root, started, previousStatus) {
		switch status {
		case "source_reported":
			id := firstNonEmpty(anyString(root["id"]), "<id>")
			return "reported by source, not yet verified — run `anx pm actions reconcile " + id + "` to read it back"
		case "delivered", "acknowledged":
			return "delivered"
		default:
			return ""
		}
	}
	if actionHasSentAttempt(root) {
		switch status {
		case "verified":
			return "already verified"
		case "delivered", "acknowledged", "source_reported":
			return "already delivered"
		default:
			return ""
		}
	}
	switch status {
	case "failed", "delivered", "acknowledged", "source_reported", "verified", "pending_delivery":
		return "nothing was sent"
	default:
		return ""
	}
}
