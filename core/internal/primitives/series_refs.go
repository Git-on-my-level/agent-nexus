package primitives

import (
	"agent-nexus-core/internal/plans"
	series "agent-nexus-core/internal/seriesref"
	"context"
	"encoding/json"
	"strings"
	"time"
)

func (s *Store) resolveSeriesPreviews(ctx context.Context, previews []RefPreview, now time.Time) error {
	refs := []series.Ref{}
	seen := map[string]bool{}
	for _, p := range previews {
		if strings.HasPrefix(p.Ref, "series:") && !seen[p.Ref] {
			r, err := series.ParseRef(p.Ref)
			if err != nil {
				continue
			}
			seen[p.Ref] = true
			refs = append(refs, r)
		}
	}
	metrics, err := series.CompareRefs(ctx, s.db, refs, now)
	if err != nil {
		return err
	}
	for i, p := range previews {
		if m, ok := metrics[p.Ref]; ok {
			if m.Status == "ambiguous" || m.Labels == nil {
				previews[i] = RefPreview{Ref: p.Ref}
				continue
			}
			previews[i].Kind = "series"
			previews[i].Title = m.Name
			previews[i].Resolvable = true
			previews[i].Metric = &m
			previews[i].ObservedAt = m.ObservedAt
		}
	}
	return nil
}
func (s *Store) enrichSummaryMetrics(ctx context.Context, inputs map[string]cardHealthInput, ps map[string]plans.Plan, summaries map[string]*WorkSummary, now time.Time) error {
	refs := []series.Ref{}
	seen := map[string]bool{}
	byCard := map[string][]string{}
	// Stable admission when a page exceeds the shared metric budget.
	ids := []string{}
	for id := range summaries {
		ids = append(ids, id)
	}
	ids = uniqueSortedStrings(ids)
	for _, id := range ids {
		candidates := append(append([]string{}, inputs[id].Refs...), inputs[id].Related...)
		candidates = append(candidates, planStepRefs(ps[id])...)
		admitted := map[string]bool{}
		for _, ref := range candidates {
			if !strings.HasPrefix(ref, "series:") || admitted[ref] {
				continue
			}
			admitted[ref] = true
			if len(byCard[id]) >= 8 || (!seen[ref] && len(refs) >= 200) {
				summaries[id].MetricsTruncated = true
				continue
			}
			parsed, err := series.ParseRef(ref)
			if err != nil {
				continue
			}
			byCard[id] = append(byCard[id], ref)
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, parsed)
			}
		}
	}
	metrics, err := series.CompareRefs(ctx, s.db, refs, now)
	if err != nil {
		return err
	}
	for id, refs := range byCard {
		for _, ref := range refs {
			if m, ok := metrics[ref]; ok && (m.Labels != nil || m.Status == "ambiguous") {
				summaries[id].Metrics = append(summaries[id].Metrics, m)
			}
		}
	}
	return nil
}

// Series are context refs, with authored fallback step status. Presence only
// needs a scoped definition lookup; metric observations are resolved separately.
func readSeriesRefFacts(ctx context.Context, q briefQueryer, refs []string, previews []RefPreview) error {
	names := []string{}
	parsed := map[string]series.Ref{}
	for _, ref := range refs {
		if strings.HasPrefix(ref, "series:") {
			r, err := series.ParseRef(ref)
			if err != nil {
				continue
			}
			parsed[ref] = r
			names = append(names, r.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	raw, _ := json.Marshal(uniqueSortedStrings(names))
	rows, err := q.QueryContext(ctx, `SELECT name FROM series_definitions WHERE name IN (SELECT value FROM json_each(?))`, string(raw))
	if err != nil {
		return err
	}
	defer rows.Close()
	visible := map[string]bool{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return err
		}
		visible[name] = true
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i, p := range previews {
		if r, ok := parsed[p.Ref]; ok && visible[r.Name] {
			previews[i].Kind = "series"
			previews[i].Title = r.Name
			previews[i].Resolvable = true
		}
	}
	return nil
}
