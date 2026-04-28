package primitives

import (
	"strings"
)

// Canonical lifecycle values for OR combined list filtering.
var canonicalLifecycleStates = []string{"active", "archived", "trashed"}

// NormalizeListLifecycleStates returns a non-empty deduplicated slice in stable order (active, archived, trashed).
// Callers must pass only canonical lifecycle strings; empty input defaults to active-only.
func NormalizeListLifecycleStates(states []string) []string {
	if len(states) == 0 {
		return []string{"active"}
	}
	seen := make(map[string]struct{}, len(states))
	for _, raw := range states {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		seen[s] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for _, canon := range canonicalLifecycleStates {
		if _, ok := seen[canon]; ok {
			out = append(out, canon)
		}
	}
	if len(out) == 0 {
		return []string{"active"}
	}
	return out
}

// LifecycleStatePredicate returns SQL for one bucket (qualified column names).
func LifecycleStatePredicate(archivedCol, trashedCol, state string) string {
	switch strings.TrimSpace(state) {
	case "active":
		return "(" + archivedCol + " IS NULL AND " + trashedCol + " IS NULL)"
	case "archived":
		return "(" + archivedCol + " IS NOT NULL AND " + trashedCol + " IS NULL)"
	case "trashed":
		return "(" + trashedCol + " IS NOT NULL)"
	default:
		return "1=0"
	}
}

// LifecycleStatesOrGroup returns `(p1 OR p2 OR …)` for the given lifecycle states (after NormalizeListLifecycleStates).
func LifecycleStatesOrGroup(archivedCol, trashedCol string, states []string) string {
	states = NormalizeListLifecycleStates(states)
	parts := make([]string, 0, len(states))
	for _, s := range states {
		parts = append(parts, LifecycleStatePredicate(archivedCol, trashedCol, s))
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}
