package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/seriesref"
)

const loopWindow = 30 * 24 * time.Hour
const loopIncumbentPredicate = `COALESCE(json_extract(payload_json,'$.payload.experiment.decision'),'kept')='kept'`
const loopEffectPredicate = `(json_extract(payload_json,'$.payload.outcome')='acted' OR json_extract(payload_json,'$.payload.experiment.decision')='kept')`

type loopCard struct {
	id, phase, created string
	metadata           map[string]any
}
type loopFacts struct {
	Runs            []map[string]any `json:"runs"`
	Points          []map[string]any `json:"points"`
	Counts          []map[string]any `json:"counts"`
	LastRun         *string          `json:"last_run"`
	LastEffect      *string          `json:"last_effect"`
	LatestIncumbent *float64         `json:"latest_incumbent"`
	BestBefore      *float64         `json:"best_before"`
	BestAfter       *float64         `json:"best_after"`
	NextRun         *string          `json:"next_run"`
	Expirations     []*string        `json:"expirations"`
}

// loadLoopStates admits only caller-selected cards (<=50). Reads canonical scoped
// events; no shared projection can leak receipts with private effect provenance.
// Histories are bounded by indexed subject/time ranges; only 60 receipts/points
// per card cross the SQL boundary. Counts aggregate the 14-day indexed window.
func (s *Store) loadLoopStates(ctx context.Context, ids []string, now time.Time) (map[string]map[string]any, map[string]loopFacts, error) {
	return s.loadLoopStatesForRead(ctx, ids, now, false)
}

func (s *Store) loadBriefLoopStates(ctx context.Context, ids []string, now time.Time) (map[string]map[string]any, map[string]loopFacts, error) {
	return s.loadLoopStatesForRead(ctx, ids, now, true)
}

func (s *Store) loadLoopStatesForRead(ctx context.Context, ids []string, now time.Time, briefOnly bool) (map[string]map[string]any, map[string]loopFacts, error) {
	out := map[string]map[string]any{}
	facts := map[string]loopFacts{}
	if len(ids) == 0 {
		return out, facts, nil
	}
	if len(ids) > 50 {
		return nil, nil, workInvalid("at most 50 loop cards")
	}
	encoded, _ := json.Marshal(ids)
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.column_key,c.created_at,COALESCE(c.archived_at,''),COALESCE(m.metadata_json,'{}'),EXISTS(SELECT 1 FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')='card:'||c.id AND ts>=? AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='') FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id WHERE c.id IN (SELECT value FROM json_each(?))`, loopTS(now.Add(-loopWindow)), loopTS(now), string(encoded))
	if err != nil {
		return nil, nil, err
	}
	cards := []loopCard{}
	requests := []map[string]any{}
	for rows.Next() {
		var c loopCard
		var raw, archived string
		var hasRecent bool
		if err = rows.Scan(&c.id, &c.phase, &c.created, &archived, &raw, &hasRecent); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if archived != "" {
			c.phase = "done"
		}
		_ = json.Unmarshal([]byte(raw), &c.metadata)
		if !hasRecent && len(workMap(c.metadata["cadence"])) == 0 && len(workMap(c.metadata["goal"])) == 0 {
			continue
		}
		cards = append(cards, c)
		requests = append(requests, map[string]any{"id": c.id, "subject": "card:" + c.id, "metric": workString(workMap(c.metadata["goal"])["metric"]), "direction": workString(workMap(c.metadata["goal"])["direction"])})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	if len(cards) == 0 {
		return out, facts, nil
	}

	cached, err := s.cachedLoopFactsForRead(ctx, ids, now, briefOnly)
	if err != nil {
		return nil, nil, err
	}
	missing := []map[string]any{}
	for _, request := range requests {
		id := workString(request["id"])
		if f, ok := cached[id]; ok {
			facts[id] = f
		} else {
			missing = append(missing, request)
		}
	}
	fresh, err := readLoopFacts(ctx, s.db, missing, now)
	if err != nil {
		return nil, nil, err
	}
	for id, f := range fresh {
		facts[id] = f
	}

	for _, c := range cards {
		out[c.id] = computeLoop(c, facts[c.id], now)
	}
	if err = s.loopSeries(ctx, cards, out, now); err != nil {
		return nil, nil, err
	}
	for _, c := range cards {
		if workString(workMap(c.metadata["goal"])["series_ref"]) == "" {
			continue
		}
		f := facts[c.id]
		f.BestBefore = nil
		f.BestAfter = nil
		if state := out[c.id]; state != nil {
			if points, ok := workMap(state["goal"])["points"].([]map[string]any); ok {
				for _, p := range points {
					v, ok := loopNumber(p["value"])
					if !ok {
						continue
					}
					at, _ := time.Parse(time.RFC3339Nano, workString(p["at"]))
					target := &f.BestBefore
					if !at.Before(now.Add(-7 * 24 * time.Hour)) {
						target = &f.BestAfter
					}
					direction := workMap(state["goal"])["direction"]
					if *target == nil || (direction == "down" && v < **target) || (direction == "up" && v > **target) {
						value := v
						*target = &value
					}
				}
			}
		}
		facts[c.id] = f
	}

	return out, facts, nil
}
func loopTS(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func loopTime(s *string) time.Time {
	if s == nil {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, *s)
	return t
}
func loopCounts() map[string]any {
	return map[string]any{"acted": 0, "measured": 0, "no_op": 0, "infra_error": 0}
}
func loopDecision(run map[string]any) any { return workMap(run["experiment"])["decision"] }
func computeLoop(c loopCard, f loopFacts, now time.Time) map[string]any {
	cadence := workMap(c.metadata["cadence"])
	goal := workMap(c.metadata["goal"])
	if len(cadence) == 0 && len(goal) == 0 && (f.LastRun == nil || loopTime(f.LastRun).Before(now.Add(-loopWindow))) {
		return nil
	}
	recent := []map[string]any{}
	for _, r := range f.Runs[:min(12, len(f.Runs))] {
		recent = append(recent, map[string]any{"at": r["at"], "outcome": r["outcome"], "decision": loopDecision(r)})
	}
	counts := loopCounts()
	effects := 0
	for _, c := range f.Counts {
		if c["week"] == float64(1) {
			counts[workString(c["outcome"])] = c["n"]
			n, _ := loopNumber(c["effects"])
			effects += int(n)
		}
	}
	out := map[string]any{"health": "unknown", "health_reason": "no runs", "every_seconds": cadence["every_seconds"], "progress_budget_seconds": cadence["progress_budget_seconds"], "stop_rule": cadence["stop_rule"], "last_run_at": f.LastRun, "last_effect_at": f.LastEffect, "next_run_due_at": nil, "recent_runs": recent, "runs_7d": counts, "effects_7d": effects, "goal": nil}
	every, _ := workInt(cadence["every_seconds"])
	budget, ok := workInt(cadence["progress_budget_seconds"])
	if !ok {
		budget = 604800
	}
	anchor := loopTime(f.LastRun)
	if anchor.IsZero() {
		anchor, _ = time.Parse(time.RFC3339Nano, workString(c.metadata["cadence_set_at"]))
		if anchor.IsZero() {
			anchor, _ = time.Parse(time.RFC3339Nano, c.created)
		}
	}
	if every > 0 && !anchor.IsZero() {
		out["next_run_due_at"] = loopTS(anchor.Add(time.Duration(every) * time.Second))
	}
	switch {
	case c.phase == "done" || c.phase == "cancelled":
		out["health_reason"] = "done"
	case len(f.Runs) >= 2 && f.Runs[0]["outcome"] == "infra_error" && f.Runs[1]["outcome"] == "infra_error":
		out["health"] = "failing"
		out["health_reason"] = "two consecutive infrastructure errors"
	case every > 0 && !anchor.IsZero() && now.Sub(anchor) > time.Duration(every)*2*time.Second:
		out["health"] = "overdue"
		out["health_reason"] = "more than two cadences since last run"
	case f.LastRun != nil && (f.LastEffect == nil || now.Sub(loopTime(f.LastEffect)) > time.Duration(budget)*time.Second):
		out["health"] = "stalled"
		out["health_reason"] = "no effect within progress budget"
	case f.LastEffect != nil && now.Sub(loopTime(f.LastEffect)) <= time.Duration(budget)*time.Second:
		out["health"] = "healthy"
		out["health_reason"] = "effect within progress budget"
	}
	if len(goal) > 0 {
		points := f.Points
		if points == nil {
			points = []map[string]any{}
		}
		sort.SliceStable(points, func(i, j int) bool { return workString(points[i]["at"]) < workString(points[j]["at"]) })
		g := loopGoal(goal, points)
		if f.LatestIncumbent != nil {
			g["latest"] = *f.LatestIncumbent
		}
		for _, best := range []*float64{f.BestBefore, f.BestAfter} {
			if best != nil {
				prior, ok := loopNumber(g["best"])
				if !ok || (goal["direction"] == "down" && *best < prior) || (goal["direction"] == "up" && *best > prior) {
					g["best"] = *best
				}
			}
		}
		out["goal"] = g
	}
	return out
}
func loopGoal(goal map[string]any, points []map[string]any) map[string]any {
	g := map[string]any{"label": goal["label"], "metric": goal["metric"], "direction": goal["direction"], "target": goal["target"], "unit": goal["unit"], "baseline": goal["baseline"], "latest": nil, "best": nil, "points": points}
	for _, p := range points {
		v, ok := loopNumber(p["value"])
		if !ok || p["decision"] != nil && p["decision"] != "kept" {
			continue
		}
		g["latest"] = v
		if best, ok := loopNumber(g["best"]); !ok || (goal["direction"] == "down" && v < best) || (goal["direction"] == "up" && v > best) {
			g["best"] = v
		}
		if g["baseline"] == nil {
			g["baseline"] = v
		}
	}
	return g
}

// One batch resolves at most 50 selectors; one batch reads at most 31 daily
// buckets per selected stream in each rollup table. Raw density adds no work.
func (s *Store) loopSeries(ctx context.Context, cards []loopCard, out map[string]map[string]any, now time.Time) error {
	refs := []seriesref.Ref{}
	byCard := map[string]string{}
	seen := map[string]bool{}
	for _, c := range cards {
		if out[c.id] == nil {
			continue
		}
		ref := workString(workMap(c.metadata["goal"])["series_ref"])
		if ref != "" {
			r, err := seriesref.ParseRef(ref)
			if err == nil {
				r.Range = loopWindow
				if !seen[ref] {
					refs = append(refs, r)
					seen[ref] = true
				}
				byCard[c.id] = ref
				out[c.id]["goal"] = loopGoal(workMap(c.metadata["goal"]), []map[string]any{})
			}
		}
	}
	if len(refs) == 0 {
		return nil
	}
	metrics, err := seriesref.LatestRefs(ctx, s.db, refs, now)
	if err != nil {
		return err
	}
	requests := []map[string]string{}
	for ref, m := range metrics {
		if m.Labels != nil && m.Status != "ambiguous" {
			labels, _ := json.Marshal(m.Labels)
			requests = append(requests, map[string]string{"ref": ref, "name": m.Name, "labels": string(labels)})
		}
	}
	if len(requests) == 0 {
		return nil
	}
	raw, _ := json.Marshal(requests)
	start := now.Add(-loopWindow).Truncate(24 * time.Hour).UnixNano()
	end := now.Truncate(24 * time.Hour).UnixNano()
	rows, err := s.db.QueryContext(ctx, `WITH requested AS MATERIALIZED (SELECT json_extract(value,'$.ref') ref,json_extract(value,'$.name') name,json_extract(value,'$.labels') labels FROM json_each(?))
 SELECT r.ref,d.day,d.last_ts,d.last_value FROM requested r JOIN series_live_daily d ON d.series=r.name AND d.labels=r.labels AND d.day>=? AND d.day<=?
 UNION ALL SELECT r.ref,d.day,d.last_ts,d.last_value FROM requested r JOIN series_daily d ON d.series=r.name AND d.labels=r.labels AND d.day>=? AND d.day<=?`, string(raw), start, end, start, end)
	if err != nil {
		return err
	}
	defer rows.Close()
	days := map[string]map[int64]map[string]any{}
	for rows.Next() {
		var ref string
		var day, at int64
		var value sql.NullFloat64
		if err = rows.Scan(&ref, &day, &at, &value); err != nil {
			return err
		}
		if !value.Valid || at > now.UnixNano() {
			continue
		}
		if days[ref] == nil {
			days[ref] = map[int64]map[string]any{}
		}
		p := map[string]any{"at": loopTS(time.Unix(0, at)), "value": value.Float64, "decision": nil}
		old := days[ref][day]
		if old == nil || workString(old["at"]) < workString(p["at"]) {
			days[ref][day] = p
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, c := range cards {
		ref := byCard[c.id]
		if ref == "" {
			continue
		}
		points := []map[string]any{}
		for _, p := range days[ref] {
			points = append(points, p)
		}
		sort.Slice(points, func(i, j int) bool { return workString(points[i]["at"]) < workString(points[j]["at"]) })
		out[c.id]["goal"] = loopGoal(workMap(c.metadata["goal"]), points)
	}
	return nil
}

// LoopReport uses the same computation as brief. Candidate admission is capped
// at 50 visible loops before receipt enrichment. Truncated means there are
// more loops than the admission/display limit; statistics describe this cohort.
func (s *Store) LoopReport(ctx context.Context, board, card, health string, limit int, now time.Time) (map[string]any, bool, error) {
	if limit == 0 {
		limit = 24
	}
	if limit < 1 || limit > 48 {
		return nil, false, workInvalid("loop limit must be 1..48")
	}
	ids := []string{}
	items := map[string]map[string]any{}
	activity := map[string]string{}
	partial := false
	if card != "" {
		c, err := s.getWorkCard(ctx, card)
		if err != nil {
			return nil, false, err
		}
		id := workString(c["id"])
		ids = append(ids, id)
		items[id] = map[string]any{"ref": "card:" + firstNonEmptyString(workString(c["handle"]), id), "title": c["title"]}
	} else {
		boardID := ""
		if board != "" {
			b, err := resolveResourceRef(ctx, s.db, ResourceRefInput{Type: "board", Ref: board})
			if err != nil {
				return nil, false, err
			}
			boardID = b.ID
		}
		// Each indexed arm admits at most 51 visible loops. The annotation and
		// receipt predicates recheck scoped truth before LIMIT: hints can include
		// private receipts, future runs, or metadata the reader cannot see.
		for _, arm := range []struct {
			predicate, activity string
			args                []any
		}{
			{`bi.loop_annotated=1 AND EXISTS(SELECT 1 FROM work_metadata m WHERE m.card_id=c.id AND (json_type(m.metadata_json,'$.cadence')='object' OR json_type(m.metadata_json,'$.goal')='object'))`, `bi.loop_annotated_at`, nil},
			{`bi.loop_last_run_at>=? AND EXISTS(SELECT 1 FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')='card:'||c.id AND ts>=? AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='')`, `bi.loop_last_run_at`, []any{loopTS(now.Add(-loopWindow)), loopTS(now.Add(-loopWindow)), loopTS(now)}},
		} {
			args := append(arm.args, boardID, boardID, boardID)
			rows, err := s.db.QueryContext(ctx, `SELECT c.id,COALESCE(NULLIF(c.handle,''),c.id),c.title,`+arm.activity+` FROM brief_initiatives bi JOIN cards c ON c.id=bi.card_id WHERE `+arm.predicate+` AND COALESCE(c.trashed_at,'')='' AND COALESCE(c.archived_at,'')='' AND ((?<>'' AND c.board_id=?) OR (?='' AND `+workIsInitiativeSQL("c")+`)) ORDER BY `+arm.activity+` DESC,bi.card_id LIMIT 51`, args...)
			if err != nil {
				return nil, false, err
			}
			for rows.Next() {
				var id, ref, title, at string
				if err = rows.Scan(&id, &ref, &title, &at); err != nil {
					rows.Close()
					return nil, false, err
				}
				if _, exists := items[id]; !exists {
					ids = append(ids, id)
				}
				items[id] = map[string]any{"ref": "card:" + ref, "title": title}
				if at > activity[id] {
					activity[id] = at
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, false, err
			}
		}
		sort.SliceStable(ids, func(i, j int) bool {
			if activity[ids[i]] != activity[ids[j]] {
				return activity[ids[i]] > activity[ids[j]]
			}
			return ids[i] < ids[j]
		})
		if len(ids) > 50 {
			ids = ids[:50]
			partial = true
		}
	}
	states, facts, err := s.loadLoopStates(ctx, ids, now)
	if err != nil {
		return nil, false, err
	}
	if card != "" {
		item := items[ids[0]]
		item["loop"] = states[ids[0]]
		runs := facts[ids[0]].Runs
		if runs == nil {
			runs = []map[string]any{}
		}
		item["runs"] = runs
		return item, false, nil
	}
	loops := []map[string]any{}
	stats := map[string]any{"loops": 0, "effects_7d": 0, "effects_prev_7d": 0, "runs_7d": loopCounts(), "improved_7d": 0, "needs_attention": 0}
	rank := map[string]int{"failing": 0, "overdue": 1, "stalled": 2, "healthy": 3, "unknown": 4}
	for _, id := range ids {
		state := states[id]
		if state == nil {
			continue
		}
		h := workString(state["health"])
		if health != "" && !strings.Contains(","+health+",", ","+h+",") {
			continue
		}
		item := items[id]
		item["loop"] = state
		loops = append(loops, item)
		stats["loops"] = stats["loops"].(int) + 1
		stats["effects_7d"] = stats["effects_7d"].(int) + state["effects_7d"].(int)
		if rank[h] < 3 {
			stats["needs_attention"] = stats["needs_attention"].(int) + 1
		}
		for _, c := range facts[id].Counts {
			n, _ := loopNumber(c["n"])
			effects, _ := loopNumber(c["effects"])
			if c["week"] == float64(1) {
				counts := stats["runs_7d"].(map[string]any)
				key := workString(c["outcome"])
				counts[key] = counts[key].(int) + int(n)
			} else {
				stats["effects_prev_7d"] = stats["effects_prev_7d"].(int) + int(effects)
			}
		}
		f := facts[id]
		if f.BestBefore == nil {
			if baseline, ok := loopNumber(workMap(state["goal"])["baseline"]); ok {
				f.BestBefore = &baseline
			}
		}
		if f.BestAfter != nil && f.BestBefore != nil && ((workMap(state["goal"])["direction"] == "down" && *f.BestAfter < *f.BestBefore) || (workMap(state["goal"])["direction"] == "up" && *f.BestAfter > *f.BestBefore)) {
			stats["improved_7d"] = stats["improved_7d"].(int) + 1
		}
	}
	sort.SliceStable(loops, func(i, j int) bool {
		a, b := workMap(loops[i]["loop"]), workMap(loops[j]["loop"])
		ra, rb := rank[workString(a["health"])], rank[workString(b["health"])]
		if ra != rb {
			return ra < rb
		}
		return loopTimeValue(a["last_effect_at"]) > loopTimeValue(b["last_effect_at"])
	})
	if len(loops) > limit {
		loops = loops[:limit]
		partial = true
	}
	return map[string]any{"stats": stats, "loops": loops}, partial, nil
}
func loopTimeValue(v any) string {
	if s, ok := v.(*string); ok && s != nil {
		return *s
	}
	return workString(v)
}

// readLoopFacts is used by canonical maintenance and the reader-scoped fallback.
// Shared projections are never replaced with a principal's filtered results.
func readLoopFacts(ctx context.Context, q briefQueryer, requests []map[string]any, now time.Time) (map[string]loopFacts, error) {
	facts := map[string]loopFacts{}
	if len(requests) == 0 {
		return facts, nil
	}
	encoded, _ := json.Marshal(requests)
	// The outer requested set is already capped. Each correlated index seek fixes
	// the subject before date, order or limit; no workspace event scan or N+1 call.
	rows, err := q.QueryContext(ctx, `WITH requested AS MATERIALIZED (
 SELECT json_extract(value,'$.id') id,json_extract(value,'$.subject') subject,json_extract(value,'$.metric') metric,json_extract(value,'$.direction') direction FROM json_each(?)
 ) SELECT r.id,json_object(
 'next_run',(SELECT ts FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts>? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts,id LIMIT 1),
 'expirations',json_array((SELECT ts FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts>=? AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts,id LIMIT 1),(SELECT ts FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts>=? AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts,id LIMIT 1),(SELECT ts FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts>=? AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts,id LIMIT 1)),
 'last_run',(SELECT ts FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts DESC,id DESC LIMIT 1),
 'last_effect',(SELECT ts FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND `+loopEffectPredicate+` AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts DESC,id DESC LIMIT 1),
 'runs',(SELECT json_group_array(json_object('at',ts,'outcome',json_extract(payload_json,'$.payload.outcome'),'summary',json_extract(payload_json,'$.summary'),'run_id',json_extract(payload_json,'$.payload.run_id'),'metrics',json_extract(payload_json,'$.payload.metrics'),'experiment',json_extract(payload_json,'$.payload.experiment'),'effect_refs',json_extract(payload_json,'$.payload.effect_refs'),'event_id',id)) FROM (SELECT id,ts,payload_json FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' ORDER BY ts DESC,id DESC LIMIT 50)),
 'counts',(SELECT json_group_array(json_object('outcome',outcome,'week',week,'n',n,'effects',effects)) FROM (SELECT json_extract(payload_json,'$.payload.outcome') outcome,ts>=? week,COUNT(*) n,SUM(CASE WHEN `+loopEffectPredicate+` THEN 1 ELSE 0 END) effects FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts>=? AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' GROUP BY outcome,week)),
 'points',(SELECT json_group_array(json_object('at',ts,'value',value,'decision',decision)) FROM (SELECT ts,COALESCE(json_extract(payload_json,'$.payload.metrics."'||r.metric||'"'),json_extract(payload_json,'$.payload.experiment.score_after')) value,json_extract(payload_json,'$.payload.experiment.decision') decision FROM events WHERE r.metric<>'' AND type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts>=? AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' AND COALESCE(json_extract(payload_json,'$.payload.metrics."'||r.metric||'"'),json_extract(payload_json,'$.payload.experiment.score_after')) IS NOT NULL ORDER BY ts DESC,id DESC LIMIT 60)),
 'latest_incumbent',(SELECT COALESCE(json_extract(payload_json,'$.payload.metrics."'||r.metric||'"'),json_extract(payload_json,'$.payload.experiment.score_after')) FROM events WHERE r.metric<>'' AND `+loopIncumbentPredicate+` AND type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts>=? AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='' AND COALESCE(json_extract(payload_json,'$.payload.metrics."'||r.metric||'"'),json_extract(payload_json,'$.payload.experiment.score_after')) IS NOT NULL ORDER BY ts DESC,id DESC LIMIT 1),
 'best_before',(SELECT CASE WHEN r.direction='down' THEN MIN(COALESCE(json_extract(payload_json,'$.payload.metrics."'||r.metric||'"'),json_extract(payload_json,'$.payload.experiment.score_after'))) ELSE MAX(COALESCE(json_extract(payload_json,'$.payload.metrics."'||r.metric||'"'),json_extract(payload_json,'$.payload.experiment.score_after'))) END FROM events WHERE r.metric<>'' AND `+loopIncumbentPredicate+` AND type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts>=? AND ts<? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')=''),
 'best_after',(SELECT CASE WHEN r.direction='down' THEN MIN(COALESCE(json_extract(payload_json,'$.payload.metrics."'||r.metric||'"'),json_extract(payload_json,'$.payload.experiment.score_after'))) ELSE MAX(COALESCE(json_extract(payload_json,'$.payload.metrics."'||r.metric||'"'),json_extract(payload_json,'$.payload.experiment.score_after'))) END FROM events WHERE r.metric<>'' AND `+loopIncumbentPredicate+` AND type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=r.subject AND ts>=? AND ts<=? AND COALESCE(trashed_at,'')='' AND COALESCE(archived_at,'')='')
 ) FROM requested r`,
		string(encoded), loopTS(now), // requested, next_run
		loopTS(now.Add(-7*24*time.Hour)), loopTS(now), // 7d expiry
		loopTS(now.Add(-14*24*time.Hour)), loopTS(now), // 14d expiry
		loopTS(now.Add(-loopWindow)), loopTS(now), // 30d expiry
		loopTS(now), loopTS(now), loopTS(now), // last_run, last_effect, runs
		loopTS(now.Add(-7*24*time.Hour)), loopTS(now.Add(-14*24*time.Hour)), loopTS(now), // counts
		loopTS(now.Add(-loopWindow)), loopTS(now), // displayed points
		loopTS(now.Add(-loopWindow)), loopTS(now), // latest incumbent
		loopTS(now.Add(-loopWindow)), loopTS(now.Add(-7*24*time.Hour)), // best_before: [now-30d, now-7d)
		loopTS(now.Add(-7*24*time.Hour)), loopTS(now)) // best_after: [now-7d, now]
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var f loopFacts
		if err = json.Unmarshal([]byte(raw), &f); err != nil {
			rows.Close()
			return nil, err
		}
		facts[id] = f
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return facts, nil
}
