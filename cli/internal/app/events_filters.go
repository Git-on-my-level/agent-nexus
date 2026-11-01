// Client-side event list filtering, sorting and lifecycle flag conversion
// used by `anx events list` before rendering.

package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/registry"
)

func normalizeEventTypeFilters(explicit []string, csv string) []string {
	out := make([]string, 0, len(explicit)+2)
	seen := make(map[string]struct{}, len(explicit)+2)
	appendValue := func(raw string) {
		value := strings.TrimSpace(raw)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range explicit {
		appendValue(value)
	}
	for _, value := range strings.Split(strings.TrimSpace(csv), ",") {
		appendValue(value)
	}
	return out
}

func normalizeCSVFilters(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		for _, value := range strings.Split(strings.TrimSpace(raw), ",") {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func normalizeStringFilters(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func filterEventsByType(events []any, types []string) []any {
	if len(events) == 0 {
		return []any{}
	}
	if len(types) == 0 {
		return append([]any(nil), events...)
	}
	allowed := make(map[string]struct{}, len(types))
	for _, eventType := range types {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			continue
		}
		allowed[eventType] = struct{}{}
	}
	if len(allowed) == 0 {
		return append([]any(nil), events...)
	}
	filtered := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		eventType := strings.TrimSpace(anyString(event["type"]))
		if _, ok := allowed[eventType]; !ok {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func filterEventsByEventGroups(events []any, groups []string, taxonomy registry.TaxonomyRegistry) []any {
	if len(events) == 0 {
		return []any{}
	}
	if len(groups) == 0 {
		return append([]any(nil), events...)
	}
	allowed := make(map[string]struct{})
	for _, group := range groups {
		for _, eventType := range taxonomy.EventGroups[strings.TrimSpace(group)] {
			eventType = strings.TrimSpace(eventType)
			if eventType != "" {
				allowed[eventType] = struct{}{}
			}
		}
	}
	if len(allowed) == 0 {
		return []any{}
	}
	filtered := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		if _, ok := allowed[strings.TrimSpace(anyString(event["type"]))]; ok {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func filterEventsByBackingScope(events []any, scope string, taxonomy registry.TaxonomyRegistry) []any {
	scope = strings.TrimSpace(scope)
	if len(events) == 0 {
		return []any{}
	}
	if scope == "" || scope == "all" {
		return append([]any(nil), events...)
	}
	backingTypes := make(map[string]struct{}, len(taxonomy.BackingEventTypes))
	for _, eventType := range taxonomy.BackingEventTypes {
		eventType = strings.TrimSpace(eventType)
		if eventType != "" {
			backingTypes[eventType] = struct{}{}
		}
	}
	filtered := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		_, isBacking := backingTypes[strings.TrimSpace(anyString(event["type"]))]
		if (scope == "backing_only" && isBacking) || (scope == "standalone" && !isBacking) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func validateEventGroupFilters(groups []string, taxonomy registry.TaxonomyRegistry) error {
	if len(groups) == 0 {
		return nil
	}
	known := make(map[string]struct{}, len(taxonomy.EventGroups))
	for group := range taxonomy.EventGroups {
		known[group] = struct{}{}
	}
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if _, ok := known[group]; !ok {
			return errnorm.Usage("invalid_flags", fmt.Sprintf("--event-group must be one of %s (got %q)", strings.Join(sortedMapKeys(known), ", "), group))
		}
	}
	return nil
}

func validateBackingScopeFilter(scope string, taxonomy registry.TaxonomyRegistry) error {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil
	}
	enum, ok := taxonomy.Enums["backing_scope"]
	if !ok {
		return errnorm.New(errnorm.KindInternal, "taxonomy_invalid", "embedded taxonomy is missing backing_scope")
	}
	known := make(map[string]struct{}, len(enum.Values))
	for _, value := range enum.Values {
		known[value] = struct{}{}
	}
	if _, ok := known[scope]; !ok {
		return errnorm.Usage("invalid_flags", fmt.Sprintf("--backing-scope must be one of %s (got %q)", strings.Join(sortedMapKeys(known), ", "), scope))
	}
	return nil
}

func eventThreadIDFromList(events []any) string {
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		threadID := strings.TrimSpace(anyString(event["thread_id"]))
		if threadID != "" {
			return threadID
		}
	}
	return ""
}

func normalizeIDFilters(rawIDs []string) []string {
	if len(rawIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(rawIDs))
	seen := make(map[string]struct{}, len(rawIDs))
	for _, raw := range rawIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func filterEventsByActorID(events []any, actorID string) []any {
	actorID = strings.TrimSpace(actorID)
	if len(events) == 0 {
		return []any{}
	}
	if actorID == "" {
		return append([]any(nil), events...)
	}
	filtered := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		if strings.TrimSpace(anyString(event["actor_id"])) != actorID {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

// validateLifecycleFilterFlags rejects mutually-exclusive combinations of the
// archived/trashed lifecycle filter flags commonly exposed on list-style
// commands. It returns a usage-kind error when the caller mixes
// --include-archived with --archived-only or --include-trashed with
// --trashed-only.
func validateLifecycleFilterFlags(includeArchived, archivedOnly, includeTrashed, trashedOnly bool) error {
	if includeArchived && archivedOnly {
		return errnorm.Usage("invalid_flags", "--include-archived and --archived-only are mutually exclusive")
	}
	if includeTrashed && trashedOnly {
		return errnorm.Usage("invalid_flags", "--include-trashed and --trashed-only are mutually exclusive")
	}
	return nil
}

func filterEventsByLifecycleState(events []any, includeArchived, archivedOnly, includeTrashed, trashedOnly bool) []any {
	if includeArchived && !archivedOnly && includeTrashed && !trashedOnly {
		return events
	}
	out := make([]any, 0, len(events))
	for _, raw := range events {
		event := asMap(raw)
		if event == nil {
			continue
		}
		isArchived := anyString(event["archived_at"]) != ""
		isTrashed := anyString(event["trashed_at"]) != ""

		if trashedOnly {
			if isTrashed {
				out = append(out, raw)
			}
			continue
		}
		if archivedOnly {
			if isArchived && !isTrashed {
				out = append(out, raw)
			}
			continue
		}
		if isTrashed && !includeTrashed {
			continue
		}
		if isArchived && !includeArchived {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func sortEventsByCreatedAt(events []any) {
	if len(events) <= 1 {
		return
	}
	sort.SliceStable(events, func(i int, j int) bool {
		left := asMap(events[i])
		right := asMap(events[j])
		if left == nil || right == nil {
			return false
		}
		leftTS, leftOK := eventCanonicalTimestamp(left)
		rightTS, rightOK := eventCanonicalTimestamp(right)
		if leftOK && rightOK {
			if leftTS.Equal(rightTS) {
				return strings.TrimSpace(anyString(left["id"])) < strings.TrimSpace(anyString(right["id"]))
			}
			return leftTS.Before(rightTS)
		}
		if leftOK != rightOK {
			return leftOK
		}
		return false
	})
}

func eventCanonicalTimestamp(event map[string]any) (time.Time, bool) {
	for _, field := range []string{"ts", "created_at"} {
		raw := strings.TrimSpace(anyString(event[field]))
		if raw == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return ts, true
		}
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

// appendLifecycleStatesForHTTPList converts legacy CLI lifecycle flags into repeated HTTP `state=…` pairs.
func appendLifecycleStatesForHTTPList(query *[]queryParam, explicitState string, includeArchived, archivedOnly, includeTrashed, trashedOnly bool) error {
	es := strings.TrimSpace(explicitState)
	if trashedOnly {
		if es != "" && es != "trashed" {
			return errnorm.Usage("invalid_flags", "--trashed-only cannot be combined with --state unless --state is trashed")
		}
		addMultiQuery(query, "state", []string{"trashed"})
		return nil
	}
	if archivedOnly {
		if es != "" && es != "archived" {
			return errnorm.Usage("invalid_flags", "--archived-only cannot be combined with --state unless --state is archived")
		}
		addMultiQuery(query, "state", []string{"archived"})
		return nil
	}
	if es != "" {
		switch es {
		case "active", "archived", "trashed":
			states := []string{es}
			if includeArchived {
				states = append(states, "archived")
			}
			if includeTrashed {
				states = append(states, "trashed")
			}
			states = dedupeCanonLifecycleStates(states)
			addMultiQuery(query, "state", states)
			return nil
		default:
			return errnorm.Usage("invalid_flags", fmt.Sprintf("--state must be one of active, archived, trashed (got %q)", es))
		}
	}
	states := []string{"active"}
	if includeArchived {
		states = append(states, "archived")
	}
	if includeTrashed {
		states = append(states, "trashed")
	}
	if len(states) == 1 && states[0] == "active" && !includeArchived && !includeTrashed {
		return nil
	}
	states = dedupeCanonLifecycleStates(states)
	addMultiQuery(query, "state", states)
	return nil
}

func dedupeCanonLifecycleStates(states []string) []string {
	seen := make(map[string]struct{})
	for _, s := range states {
		switch strings.TrimSpace(s) {
		case "active", "archived", "trashed":
			seen[s] = struct{}{}
		}
	}
	out := make([]string, 0, 3)
	for _, k := range []string{"active", "archived", "trashed"} {
		if _, ok := seen[k]; ok {
			out = append(out, k)
		}
	}
	return out
}
