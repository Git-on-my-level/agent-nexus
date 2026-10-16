package primitives

import (
	"context"
	"encoding/json"
	"time"
)

func (s *Store) cachedLoopFacts(ctx context.Context, ids []string, now time.Time) (map[string]loopFacts, error) {
	out := map[string]loopFacts{}
	if len(ids) == 0 {
		return out, nil
	}
	encoded, _ := json.Marshal(ids)
	rows, err := s.db.QueryContext(ctx, `SELECT f.card_id,f.facts_json FROM brief_loop_facts f WHERE f.card_id IN (SELECT value FROM json_each(?)) AND f.valid_from<=? AND f.valid_until>? AND NOT EXISTS(SELECT 1 FROM brief_dirty d WHERE d.card_id=f.card_id)`, string(encoded), loopTS(now), loopTS(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var f loopFacts
		if err = json.Unmarshal([]byte(raw), &f); err != nil {
			return nil, err
		}
		out[id] = f
	}
	return out, rows.Err()
}

func foldLoopFacts(ctx context.Context, tx *accessTx, ids []string, now time.Time) error {
	encoded, _ := json.Marshal(ids)
	rows, err := tx.QueryContext(ctx, `SELECT c.id,COALESCE(m.metadata_json,'{}'),EXISTS(SELECT 1 FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')='card:'||c.id LIMIT 1) FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id WHERE c.id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return err
	}
	requests := []map[string]any{}
	for rows.Next() {
		var id, raw string
		var hasRun bool
		if err = rows.Scan(&id, &raw, &hasRun); err != nil {
			rows.Close()
			return err
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(raw), &m)
		if !hasRun && len(workMap(m["cadence"])) == 0 && len(workMap(m["goal"])) == 0 {
			continue
		}
		g := workMap(m["goal"])
		requests = append(requests, map[string]any{"id": id, "subject": "card:" + id, "metric": g["metric"], "direction": g["direction"]})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	facts, err := readLoopFacts(ctx, tx, requests, now)
	if err != nil {
		return err
	}
	// Clearing/removing annotations cannot leave a stale metric projection behind.
	if _, err = tx.ExecContext(ctx, `UPDATE brief_initiatives SET loop_facts_json=NULL,loop_valid_from=NULL,loop_valid_until=NULL WHERE card_id IN (SELECT value FROM json_each(?))`, string(encoded)); err != nil {
		return err
	}
	for id, f := range facts {
		until := now.Add(loopWindow)
		if f.NextRun != nil {
			next, err := time.Parse(time.RFC3339Nano, *f.NextRun)
			if err != nil {
				return err
			}
			if next.Before(until) {
				until = next
			}
		}
		for i, stamp := range f.Expirations {
			if stamp == nil {
				continue
			}
			at, err := time.Parse(time.RFC3339Nano, *stamp)
			if err != nil {
				return err
			}
			days := []int{7, 14, 30}
			deadline := at.Add(time.Duration(days[i]) * 24 * time.Hour).Add(time.Nanosecond)
			if deadline.Before(until) {
				until = deadline
			}
		}
		raw, err := json.Marshal(f)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE brief_initiatives SET loop_facts_json=?,loop_valid_from=?,loop_valid_until=? WHERE card_id=?`, string(raw), loopTS(now), loopTS(until), id); err != nil {
			return err
		}
	}
	return nil
}
