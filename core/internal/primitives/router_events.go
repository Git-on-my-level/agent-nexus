package primitives

import "strings"

// Plain indexed tuple columns allow a seek through large timestamp ties.
// Hydrate only the canonical positions admitted by this bounded selector.
func routerEventsAfterSQL(cursor EventCursor, limit int) (string, []any) {
	positions := `SELECT event_id FROM router_message_positions ORDER BY sort_time,event_id LIMIT ?`
	args := []any{limit}
	if strings.TrimSpace(cursor.TS) != "" {
		positions = `SELECT event_id FROM router_message_positions WHERE (sort_time,event_id)>(julianday(?),?) ORDER BY sort_time,event_id LIMIT ?`
		args = []any{strings.TrimSpace(cursor.TS), strings.TrimSpace(cursor.ID), limit}
	}
	return `WITH router_positions AS MATERIALIZED (` + positions + `)
 SELECT e.id,COALESCE(e.handle,''),e.type,e.ts,e.actor_id,e.thread_id,e.refs_json,e.payload_json,
 e.archived_at,e.archived_by,e.trashed_at,e.trashed_by,e.trash_reason
 FROM router_positions p JOIN events e ON e.id=p.event_id ORDER BY julianday(e.ts),e.id`, args
}
