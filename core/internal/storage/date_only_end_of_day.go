package storage

import (
	"context"
	"database/sql"
)

// installDateOnlyEndOfDay rewrites facts that stored a calendar date as
// midnight UTC. "by Oct 15" is the end of that UTC day, which is what new
// writes already store through plans.ParseWhen. A zoned timestamp that
// happens to fall at midnight is left alone: only a bare YYYY-MM-DD source
// moves. Touched note cards are queued so the promise fold is recomputed.
func installDateOnlyEndOfDay(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"events", "brief_asks", "brief_notes", "ask_deadlines", "brief_dirty"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !exists {
			return err
		}
	}
	for _, q := range []string{
		`UPDATE brief_asks SET decide_by = substr(decide_by,1,10)||'T23:59:59Z'
 WHERE decide_by GLOB '????-??-??T00:00:00Z'
 AND ask_id IN (
  SELECT id FROM events WHERE type='human_attention_requested'
  AND trim(COALESCE(json_extract(payload_json,'$.payload.by'),'')) GLOB '????-??-??')`,
		`UPDATE ask_deadlines SET decide_by = trim(by_raw)||'T23:59:59.000000000Z'
 WHERE trim(by_raw) GLOB '????-??-??'
 AND (decide_by IS NULL OR decide_by GLOB '????-??-??T00:00:00Z' OR decide_by GLOB '????-??-??T00:00:00.000000000Z')`,
		`INSERT INTO brief_dirty(card_id) SELECT DISTINCT n.card_id FROM brief_notes n
 JOIN events e ON e.id=n.event_id
 WHERE n.next_at GLOB '????-??-??T00:00:00Z' AND e.type='message_posted'
 AND trim(COALESCE(json_extract(e.payload_json,'$.payload.next.at'),'')) GLOB '????-??-??'
 ON CONFLICT DO NOTHING`,
		`UPDATE brief_notes SET next_at = substr(next_at,1,10)||'T23:59:59Z'
 WHERE next_at GLOB '????-??-??T00:00:00Z'
 AND event_id IN (
  SELECT id FROM events WHERE type='message_posted'
  AND trim(COALESCE(json_extract(payload_json,'$.payload.next.at'),'')) GLOB '????-??-??')`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
