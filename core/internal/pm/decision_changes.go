package pm

import (
	"context"
	"encoding/json"
	"time"
)

// NewDecisions is a single bounded candidate read for compact digests. It uses
// the ordinary PM authorization policy without loading conversation bodies or
// computing actionable decision state. Callers must also check native lifecycle.
func (s *Service) NewDecisions(ctx context.Context, p Principal, since, now time.Time) ([]Decision, bool, error) {
	if err := s.authorize(ctx, p, "pm.read", ""); err != nil {
		return nil, false, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT body FROM pm_records WHERE kind='decision' AND workspace_id=?
 AND julianday(json_extract(body,'$.created_at'))>=julianday(?) AND julianday(json_extract(body,'$.created_at'))<=julianday(?)
 ORDER BY rowid DESC LIMIT 201`, p.WorkspaceID, since.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, false, err
	}
	ds := []Decision{}
	candidates := 0
	for rows.Next() {
		var raw []byte
		var d Decision
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, false, err
		}
		candidates++
		if candidates > 200 {
			continue
		}
		if err = json.Unmarshal(raw, &d); err != nil {
			rows.Close()
			return nil, false, err
		}
		if d.CreatedAt.After(since) && !d.CreatedAt.After(now) {
			ds = append(ds, d)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	truncated := candidates > 200
	out := []Decision{}
	allowed, checked := map[string]bool{}, map[string]bool{}
	for _, d := range ds {
		if !checked[d.WorkRef] {
			allowed[d.WorkRef] = s.authorize(ctx, p, "pm.read", d.WorkRef) == nil
			checked[d.WorkRef] = true
		}
		if allowed[d.WorkRef] {
			out = append(out, Decision{ID: d.ID, WorkRef: d.WorkRef, CreatedAt: d.CreatedAt})
		}
	}
	return out, truncated, nil
}
