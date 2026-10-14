package primitives

import (
	"os"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

type cardHealthInput struct {
	Activity, Created    time.Time
	Due                  string
	Phase, Owner, Handle string
	Source               map[string]any
	Children             []string
	Refs                 []string
	// Wake is the raw wake_condition annotation; Related holds the card's
	// related_refs linked sources; DarkAfter is the per-card dark horizon;
	// Archived excludes the card from dark entirely.
	Wake      string
	Related   []string
	DarkAfter time.Duration
	Archived  bool
}

// darkInput resolves the per-card dark inputs for plans.HealthFor: the wake
// annotation plus the horizon, falling back to the workspace default.
func (input cardHealthInput) darkInput() plans.DarkInput {
	return plans.DarkInput{Wake: input.Wake, Threshold: input.darkHorizon(), Phase: input.Phase, Archived: input.Archived}
}

func (input cardHealthInput) darkHorizon() time.Duration {
	if input.DarkAfter > 0 {
		return input.DarkAfter
	}
	return DarkAfterDefault()
}

// DarkAfterDefault returns the workspace default dark horizon: the generic
// ANX_PLAN_DARK_AFTER duration when set, otherwise the contract default.
func DarkAfterDefault() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("ANX_PLAN_DARK_AFTER")); err == nil && d > 0 {
		return d
	}
	return plans.DefaultDarkAfter
}

// parseDarkAfter reads the dark_after annotation: a duration, with a plain
// day count ("7d") accepted because days is how the horizon is discussed.
// Zero means "not set"; negative and unparsable values read as unset.
func parseDarkAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d < 0 {
			return 0
		}
		return d
	}
	if n, err := strconv.ParseFloat(strings.TrimSuffix(raw, "d"), 64); err == nil && strings.HasSuffix(raw, "d") && n > 0 && n <= 3650 {
		return time.Duration(n * float64(24*time.Hour))
	}
	return 0
}

// Explicit work due annotations, including null/empty clearing, take precedence.
const effectiveCardDueSQL = `CASE WHEN json_type(m.metadata_json,'$.due_at') IS NOT NULL THEN COALESCE(json_extract(m.metadata_json,'$.due_at'),'') ELSE COALESCE(c.due_at,'') END`

// Source polling updates card.updated_at, so only meaningful source timestamps
// count. Native cards use canonical card/metadata updates. Plan/discussion edits are folded in
// by loadPlans for both enrichment and previews.
const effectiveCardActivitySQL = `CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN CASE WHEN julianday(m.updated_at)>julianday(c.updated_at) THEN m.updated_at ELSE c.updated_at END ELSE COALESCE(CASE WHEN julianday(json_extract(o.body_json,'$.source_activity_at')) > julianday(json_extract(o.body_json,'$.meaningful_progress_at')) THEN json_extract(o.body_json,'$.source_activity_at') END,json_extract(o.body_json,'$.meaningful_progress_at'),json_extract(o.body_json,'$.source_activity_at'),c.created_at) END`

// Wake, dark horizon and related refs live beside the other local annotations.
const cardSignalSQL = `COALESCE(json_extract(m.metadata_json,'$.wake_condition'),''),COALESCE(json_extract(m.metadata_json,'$.dark_after'),''),COALESCE(json_extract(m.metadata_json,'$.related_refs'),'[]')`

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
