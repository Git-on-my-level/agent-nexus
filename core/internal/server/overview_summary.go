package server

// compactOverviewWork runs after publicWork and all derived-section/visit work.
// It preserves the fields used by tile deduplication, freshness and navigation.
func compactOverviewWork(item map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"id", "ref", "handle", "title", "phase", "owner", "priority", "next_actor", "next_action", "next_step", "board_ref", "project_ref", "created_at", "updated_at", "freshness"} {
		if value, ok := item[key]; ok {
			out[key] = value
		}
	}
	for _, field := range []struct {
		name string
		keys []string
	}{
		{"source", []string{"authority", "native_id", "url", "revision", "native_status"}},
		{"refresh", []string{"state", "last_error"}},
	} {
		if source, ok := item[field.name].(map[string]any); ok {
			value := map[string]any{}
			for _, key := range field.keys {
				if v, ok := source[key]; ok {
					value[key] = v
				}
			}
			out[field.name] = value
		}
	}
	count := 0
	switch blockers := item["blockers"].(type) {
	case []any:
		count = len(blockers)
	case []string:
		count = len(blockers)
	}
	out["blocker_count"] = count
	return out
}
