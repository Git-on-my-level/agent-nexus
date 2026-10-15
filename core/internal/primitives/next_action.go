package primitives

import (
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// displayedNextAction is the next_action a card read shows. The stored
// annotation, including an ask outcome's response text, stays until a live
// note promise is newer than it. A card that never had next_action does not
// grow one just because a note exists: plan_health.reason already names the
// promise. next_action_at is the server stamp of the last next_action write.
func displayedNextAction(stored, storedAt string, notes []plans.PromiseNote, promise *plans.HealthPromise) string {
	stored = strings.TrimSpace(stored)
	if promise == nil || promise.Source != "note" {
		return stored
	}
	what := strings.TrimSpace(promise.What)
	if what == "" || (stored == "" && strings.TrimSpace(storedAt) == "") {
		return stored
	}
	id := strings.TrimPrefix(strings.TrimSpace(promise.SourceRef), "event:")
	var noteAt time.Time
	for _, n := range notes {
		if n.EventID == id {
			noteAt = n.At
			break
		}
	}
	if noteAt.IsZero() {
		return stored
	}
	at, ok := parseMootTime(storedAt)
	if strings.TrimSpace(storedAt) == "" || !ok || noteAt.After(at) {
		return what
	}
	return stored
}
