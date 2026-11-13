package primitives

import (
	"encoding/json"
	"time"

	"agent-nexus-core/internal/seriesref"
)

// A measure is the number an initiative is judged by: a label, a direction, a
// target and, when a producer publishes it, a series. It is metadata.measure,
// separate from a loop's goal: a goal makes a card recurring work with run
// receipts and loop health, while a measure only says how to read an outcome.
//
// A reading carries its age and nothing judges it: there is no threshold after
// which a number counts as old (the one-clock rule allows no such knob). A
// reader sees "read 4d ago" and decides.

func validateMeasure(raw any) error {
	if raw == nil {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return workInvalid("measure must be an object or null")
	}
	if err := loopKeys(obj, "label", "direction", "target", "baseline", "unit", "series_ref"); err != nil {
		return err
	}
	if !loopText(obj["label"], 120, true) {
		return workInvalid("measure.label must be 1 to 120 characters")
	}
	if obj["direction"] != "up" && obj["direction"] != "down" {
		return workInvalid("measure.direction must be up or down")
	}
	if _, ok := loopNumber(obj["target"]); !ok {
		return workInvalid("measure.target must be a finite number")
	}
	if v, has := obj["baseline"]; has {
		if _, ok := loopNumber(v); !ok {
			return workInvalid("measure.baseline must be finite")
		}
	}
	if v, has := obj["unit"]; has && !loopText(v, 80, false) {
		return workInvalid("invalid measure.unit")
	}
	if v, has := obj["series_ref"]; has {
		ref, ok := v.(string)
		if !ok {
			return workInvalid("invalid measure.series_ref")
		}
		if _, err := seriesref.ParseRef(ref); err != nil {
			return workInvalid("invalid measure.series_ref")
		}
	}
	return nil
}

// briefMeasure folds a measure and its daily points into what Watch shows:
// the latest reading, where it stood about a week earlier, whether it has
// reached the target, and how old the reading is. Every field is computed; none is an
// agent's description of progress.
func briefMeasure(measure map[string]any, points []map[string]any, now time.Time) map[string]any {
	out := map[string]any{"label": measure["label"], "direction": measure["direction"], "target": measure["target"], "unit": measure["unit"], "baseline": measure["baseline"],
		"series_ref": measure["series_ref"], "latest": nil, "latest_at": nil, "previous": nil, "previous_at": nil, "on_target": nil, "age_seconds": nil, "points": []map[string]any{}}
	if len(points) == 0 {
		return out
	}
	compact := make([]map[string]any, 0, len(points))
	for _, p := range points {
		compact = append(compact, map[string]any{"at": p["at"], "value": p["value"]})
	}
	last := compact[len(compact)-1]
	latest, _ := loopNumber(last["value"])
	out["points"], out["latest"], out["latest_at"] = compact, latest, last["at"]
	latestAt, err := time.Parse(time.RFC3339Nano, workString(last["at"]))
	if err == nil {
		out["age_seconds"] = max(int64(now.Sub(latestAt)/time.Second), 0)
		// The comparison point is the newest reading at least a week older than
		// the latest, else the oldest in the window.
		previous := compact[0]
		for _, p := range compact[:len(compact)-1] {
			if at, e := time.Parse(time.RFC3339Nano, workString(p["at"])); e == nil && latestAt.Sub(at) >= 7*24*time.Hour {
				previous = p
			}
		}
		if len(compact) > 1 {
			out["previous"], out["previous_at"] = previous["value"], previous["at"]
		}
	}
	if target, ok := loopNumber(measure["target"]); ok {
		out["on_target"] = (measure["direction"] == "down" && latest <= target) || (measure["direction"] == "up" && latest >= target)
	}
	return out
}

// initiativeMeasures attaches `measure` to the admitted cards that declare
// one. One statement reads the declared measures of the page; one batch reads
// their series through dailySeriesPoints (at most 31 daily buckets per
// stream), so the cost follows the page size, never the workspace.
func (b *briefBuild) initiativeMeasures(ids []string, items []map[string]any) error {
	encoded, _ := json.Marshal(ids)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT m.card_id,json_extract(m.metadata_json,'$.measure') FROM json_each(?) j JOIN work_metadata m ON m.card_id=j.value WHERE json_type(m.metadata_json,'$.measure')='object'`, string(encoded))
	if err != nil {
		return err
	}
	measures := map[string]map[string]any{}
	refs := []seriesref.Ref{}
	seen := map[string]bool{}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var measure map[string]any
		if json.Unmarshal([]byte(raw), &measure) != nil || measure == nil {
			continue
		}
		measures[id] = measure
		if ref := workString(measure["series_ref"]); ref != "" && !seen[ref] {
			if parsed, e := seriesref.ParseRef(ref); e == nil {
				parsed.Range = loopWindow
				refs = append(refs, parsed)
				seen[ref] = true
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(measures) == 0 {
		return err
	}
	points, err := b.s.dailySeriesPoints(b.ctx, refs, b.now)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if measure := measures[id]; measure != nil {
			items[i]["measure"] = briefMeasure(measure, points[workString(measure["series_ref"])], b.now)
		}
	}
	return nil
}
