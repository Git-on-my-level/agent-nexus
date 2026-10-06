package primitives

import "time"

type cardHealthInput struct {
	Activity, Created time.Time
	Due               string
}

// Explicit work due annotations, including null/empty clearing, take precedence.
const effectiveCardDueSQL = `CASE WHEN json_type(m.metadata_json,'$.due_at') IS NOT NULL THEN COALESCE(json_extract(m.metadata_json,'$.due_at'),'') ELSE COALESCE(c.due_at,'') END`

// Source polling updates card.updated_at, so only meaningful source timestamps
// count. Native cards use canonical updates. Plan/discussion edits are folded in
// by loadPlans for both enrichment and previews.
const effectiveCardActivitySQL = `CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN c.updated_at ELSE COALESCE(CASE WHEN julianday(json_extract(o.body_json,'$.source_activity_at')) > julianday(json_extract(o.body_json,'$.meaningful_progress_at')) THEN json_extract(o.body_json,'$.source_activity_at') END,json_extract(o.body_json,'$.meaningful_progress_at'),json_extract(o.body_json,'$.source_activity_at'),c.created_at) END`

func latestCardActivity(values ...string) time.Time {
	var latest time.Time
	for _, raw := range values {
		at, _ := time.Parse(time.RFC3339Nano, raw)
		if at.After(latest) {
			latest = at
		}
	}
	return latest
}
