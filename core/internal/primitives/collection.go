package primitives

import (
	"context"
	"encoding/json"
	"time"
)

// CollectionWork applies collection and project admission before the existing
// bounded projection. Loops admit at most 50 cards before history enrichment.
func (s *Store) CollectionWork(ctx context.Context, collection, project string, now time.Time) (ReportWorkPage, error) {
	limit := 200
	if collection == "loops" {
		limit = 50
	}
	page, err := s.ListReportWork(ctx, ReportWorkFilter{Collection: collection, ProjectRef: project, Limit: limit})
	if err != nil {
		return page, err
	}
	if collection == "loops" {
		ids := []string{}
		for _, w := range page.Work {
			ids = append(ids, workString(w["id"]))
		}
		states, _, err := s.loadLoopStates(ctx, ids, now)
		if err != nil {
			return page, err
		}
		for _, w := range page.Work {
			w["loop"] = states[workString(w["id"])]
		}
	}
	return page, nil
}

// CollectionActivity counts only scoped events within fourteen UTC days, using
// (thread_id,ts) or (actor_id,anx_timestamp_key(ts)) index ranges per admitted row.
func (s *Store) CollectionActivity(ctx context.Context, keys []string, actors bool, now time.Time) (map[string][]int, error) {
	if len(keys) > 200 {
		return nil, workInvalid("at most 200 activity keys")
	}
	out := map[string][]int{}
	if len(keys) == 0 {
		return out, nil
	}
	start := now.UTC().Truncate(24 * time.Hour).Add(-13 * 24 * time.Hour)
	encoded, _ := json.Marshal(keys)
	join := `e.thread_id=j.value AND e.ts>=? AND e.ts<=?`
	if actors {
		join = `e.actor_id=j.value AND anx_timestamp_key(e.ts)>=anx_timestamp_key(?) AND anx_timestamp_key(e.ts)<=anx_timestamp_key(?)`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT j.value,strftime('%Y-%m-%d',e.ts),COUNT(*) FROM json_each(?) j CROSS JOIN events e ON `+join+` WHERE COALESCE(e.trashed_at,'')='' AND COALESCE(e.archived_at,'')='' GROUP BY j.value,strftime('%Y-%m-%d',e.ts)`, string(encoded), loopTS(start), loopTS(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, day string
		var n int
		if err = rows.Scan(&key, &day, &n); err != nil {
			return nil, err
		}
		at, err := time.Parse("2006-01-02", day)
		if err != nil {
			continue
		}
		i := int(at.Sub(start) / (24 * time.Hour))
		if i < 0 || i >= 14 {
			continue
		}
		if out[key] == nil {
			out[key] = make([]int, 14)
		}
		out[key][i] = n
	}
	return out, rows.Err()
}

// WaitingCollectionRefs uses two indexed EXISTS probes per admitted ref, never
// materializes the viewer's inbox or the workspace ask history.
func (s *Store) WaitingCollectionRefs(ctx context.Context, refs []string, viewer string, human bool) (map[string]bool, error) {
	if len(refs) > 400 {
		return nil, workInvalid("too many collection refs")
	}
	out := map[string]bool{}
	if len(refs) == 0 {
		return out, nil
	}
	raw, _ := json.Marshal(refs)
	eligible := `a.state='open' AND (a.recipient IN (?,?) OR (? AND a.recipient='')) AND a.requester NOT IN (?,?) AND COALESCE(e.trashed_at,'')=''`
	args := []any{viewer, "actor:" + viewer, human, viewer, "actor:" + viewer, viewer, "actor:" + viewer, human, viewer, "actor:" + viewer, string(raw)}
	rows, err := s.db.QueryContext(ctx, `SELECT j.value,EXISTS(SELECT 1 FROM brief_asks a JOIN events e ON e.id=a.ask_id `+briefAnswerableAskJoin+` WHERE a.subject_ref=j.value AND `+eligible+` LIMIT 1) OR EXISTS(SELECT 1 FROM brief_ask_blocks k CROSS JOIN brief_asks a ON a.ask_id=k.ask_id JOIN events e ON e.id=a.ask_id `+briefAnswerableAskJoin+` WHERE k.ref=j.value AND `+eligible+` LIMIT 1) FROM json_each(?) j`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		var yes bool
		if err = rows.Scan(&ref, &yes); err != nil {
			return nil, err
		}
		out[ref] = yes
	}
	return out, rows.Err()
}
