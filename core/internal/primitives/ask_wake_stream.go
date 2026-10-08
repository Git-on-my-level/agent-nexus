package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
)

type AskWakePage struct {
	Wakeups []AgentWakeup
	Tokens  map[string]string
	Cursor  int64
	More    bool
}

func (s *Store) AskWakePage(ctx context.Context, actor string, cursor int64) (AskWakePage, error) {
	// Read bounded immutable positions first; never serialize denied positions.
	rows, err := s.db.QueryContext(ctx, `SELECT seq,wakeup_id,resume_token FROM agent_wakeup_actor_positions WHERE actor_id=? AND seq>? ORDER BY seq LIMIT 200`, actor, cursor)
	if err != nil {
		return AskWakePage{}, err
	}
	ids := []string{}
	positions := map[string]int64{}
	tokens := map[string]string{}
	for rows.Next() {
		var id, token string
		if err = rows.Scan(&cursor, &id, &token); err != nil {
			rows.Close()
			return AskWakePage{}, err
		}
		ids = append(ids, id)
		positions[id] = cursor
		tokens[id] = token
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return AskWakePage{}, err
	}
	out := AskWakePage{Tokens: tokens, Cursor: cursor, More: len(ids) == 200, Wakeups: []AgentWakeup{}}
	if len(ids) == 0 {
		return out, nil
	}
	raw, _ := json.Marshal(ids)
	rows, err = s.db.QueryContext(ctx, `SELECT wakeup_id,status,notification_status,target_handle,target_actor_id,workspace_id,workspace_name,thread_id,thread_title,trigger_event_id,trigger_created_at,trigger_text,refs_json,bridge_instance_id,failure_reason,created_at,claimed_at,completed_at,failed_at,read_at,dismissed_at,updated_at FROM agent_wakeups WHERE wakeup_id IN (SELECT value FROM json_each(?)) AND target_actor_id=?`, string(raw), actor)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		wake, e := scanAgentWakeup(rows)
		if e != nil {
			return out, e
		}
		out.Wakeups = append(out.Wakeups, wake)
	}
	sort.Slice(out.Wakeups, func(i, j int) bool { return positions[out.Wakeups[i].WakeupID] < positions[out.Wakeups[j].WakeupID] })
	return out, rows.Err()
}

// Fresh connections and accepted resumes page canonical receipts, including
// receipts predating the append log. Only visible receipt IDs cross the wire.
func (s *Store) AskWakeSnapshotStart(ctx context.Context, actor, resume string) (int64, string, string, error) {
	var head int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM agent_wakeup_actor_positions WHERE actor_id=?`, actor).Scan(&head); err != nil {
		return 0, "", "", err
	}
	if resume == "" {
		return head, "", "", nil
	}
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT p.seq FROM agent_wakeup_actor_positions p JOIN agent_wakeups w ON w.wakeup_id=p.wakeup_id WHERE p.resume_token=? AND p.actor_id=? AND w.target_actor_id=?`, resume, actor, actor).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return head, "~", "~", nil
	}
	return seq, "~", "~", err
}
func (s *Store) AskWakeSnapshotPage(ctx context.Context, actor, at, id string) ([]AgentWakeup, string, string, bool, error) {
	rows, err := s.db.QueryContext(ctx, `WITH unread AS MATERIALIZED(SELECT wakeup_id,created_at FROM ask_wake_snapshot_positions WHERE target_actor_id=? AND notification_status='unread' AND (created_at,wakeup_id)>(?,?) ORDER BY created_at,wakeup_id LIMIT 200), read AS MATERIALIZED(SELECT wakeup_id,created_at FROM ask_wake_snapshot_positions WHERE target_actor_id=? AND notification_status='read' AND (created_at,wakeup_id)>(?,?) ORDER BY created_at,wakeup_id LIMIT 200), dismissed AS MATERIALIZED(SELECT wakeup_id,created_at FROM ask_wake_snapshot_positions WHERE target_actor_id=? AND notification_status='dismissed' AND (created_at,wakeup_id)>(?,?) ORDER BY created_at,wakeup_id LIMIT 200) SELECT * FROM (SELECT * FROM unread UNION ALL SELECT * FROM read UNION ALL SELECT * FROM dismissed) ORDER BY created_at,wakeup_id LIMIT 200`, actor, at, id, actor, at, id, actor, at, id)
	if err != nil {
		return nil, at, id, false, err
	}
	ids := []string{}
	for rows.Next() {
		if err = rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, at, id, false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, at, id, false, err
	}
	raw, _ := json.Marshal(ids)
	rows, err = s.db.QueryContext(ctx, `SELECT `+agentWakeupSelectList+` FROM agent_wakeups WHERE wakeup_id IN (SELECT value FROM json_each(?)) AND target_actor_id=? ORDER BY created_at,wakeup_id`, string(raw), actor)
	if err != nil {
		return nil, at, id, false, err
	}
	defer rows.Close()
	wakes := []AgentWakeup{}
	for rows.Next() {
		w, e := scanAgentWakeup(rows)
		if e != nil {
			return nil, at, id, false, e
		}
		wakes = append(wakes, w)
	}
	return wakes, at, id, len(ids) == 200, rows.Err()
}
