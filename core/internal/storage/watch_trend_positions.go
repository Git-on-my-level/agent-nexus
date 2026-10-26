package storage

import (
	"context"
	"database/sql"
	"strings"
)

// Watch trends admit each record family through watch_trend_positions, keyed
// by the record's privacy scope: the PM owner of its backing thread, or of its
// card's board thread, and '' when neither is private. A reader seeks only the
// public scope and its own, newest first, with the scoped relation joined
// inside the same LIMIT, so records it cannot see neither take window places
// nor cost examination when they sit in another owner's scope. The key steers
// cost and completeness only: every admitted row is still verified by the
// canonical scoped relation, so a stale key can hide a record, never reveal one.
//
// Only identity, kind, scope and a time key are stored; nothing is served.

// trendThreadScope is the scope of a record on thread expr.
func trendThreadScope(expr string) string {
	return `COALESCE((SELECT NULLIF(json_extract(st.body_json,'$.pm_actor_id'),'') FROM threads st WHERE st.id=` + expr + `),` +
		`(SELECT NULLIF(json_extract(sbt.body_json,'$.pm_actor_id'),'') FROM cards sc JOIN boards sb ON sb.id=sc.board_id JOIN threads sbt ON sbt.id=sb.thread_id WHERE sc.thread_id=` + expr + ` LIMIT 1),'')`
}

// trendCardScope is the scope of the card row alias.
func trendCardScope(alias string) string {
	return `COALESCE((SELECT NULLIF(json_extract(st.body_json,'$.pm_actor_id'),'') FROM threads st WHERE st.id=` + alias + `.thread_id),` +
		`(SELECT NULLIF(json_extract(sbt.body_json,'$.pm_actor_id'),'') FROM boards sb JOIN threads sbt ON sbt.id=sb.thread_id WHERE sb.id=` + alias + `.board_id),'')`
}

const trendUpsert = ` ON CONFLICT(kind,id) DO UPDATE SET scope=excluded.scope,at_key=excluded.at_key;`

const trendEventKind = `CASE %s.type WHEN 'human_attention_requested' THEN 'ask' WHEN 'human_attention_responded' THEN 'answer' ELSE 'run' END`

const trendEventTypes = `('human_attention_requested','human_attention_responded','run_recorded')`

func trendEventPositions(row string) string {
	return `INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT ` + strings.ReplaceAll(trendEventKind, "%s", row) + `,` + row + `.id,` + trendThreadScope(row+".thread_id") + `,anx_timestamp_key(` + row + `.ts)`
}

// trendCardPositions inserts the card kinds for the cards selected by from.
func trendCardPositions(from string) string {
	return `INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT 'card',c.id,` + trendCardScope("c") + `,anx_timestamp_key(c.created_at) ` + from + ` AND c.archived_at IS NULL AND c.trashed_at IS NULL` + trendUpsert +
		`INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT 'card_done',c.id,` + trendCardScope("c") + `,anx_timestamp_key(c.updated_at) ` + from + ` AND c.archived_at IS NULL AND c.trashed_at IS NULL AND c.column_key='done'` + trendUpsert
}

// trendInitiativePositions inserts promise and loop kinds for the
// brief_initiatives rows selected by from (alias bi, joined to cards c).
func trendInitiativePositions(from string) string {
	return `INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT CASE WHEN bi.open=1 THEN 'promise_open' ELSE 'promise_closed' END,bi.card_id,` + trendCardScope("c") + `,anx_timestamp_key(bi.promise_at) ` + from + ` AND bi.promise_at IS NOT NULL` + trendUpsert +
		`INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT 'loop',bi.card_id,` + trendCardScope("c") + `,COALESCE(NULLIF(bi.loop_annotated_at,''),'') ` + from + ` AND bi.loop_annotated=1` + trendUpsert
}

const trendCardKinds = `('card','card_done','promise_open','promise_closed','loop')`
const trendEventKinds = `('ask','answer','run','open_ask')`

func installWatchTrendPositions(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"events", "cards", "boards", "threads", "brief_asks", "brief_initiatives"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !exists {
			return err
		}
	}
	openAsk := `INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT 'open_ask',a.ask_id,` + trendThreadScope("e.thread_id") + `,anx_timestamp_key(a.created_at) FROM brief_asks a JOIN events e ON e.id=a.ask_id WHERE a.state='open' AND e.trashed_at IS NULL`
	statements := []string{
		`DROP VIEW IF EXISTS watch_trend_event_positions`,
		`DROP VIEW IF EXISTS watch_trend_card_positions`,
		`CREATE TABLE IF NOT EXISTS watch_trend_positions(kind TEXT NOT NULL,id TEXT NOT NULL,scope TEXT NOT NULL,at_key TEXT NOT NULL,PRIMARY KEY(kind,id)) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_watch_trend_positions ON watch_trend_positions(kind,scope,at_key,id)`,

		`CREATE TRIGGER IF NOT EXISTS watch_trend_event_insert AFTER INSERT ON events WHEN NEW.type IN ` + trendEventTypes + ` AND NEW.trashed_at IS NULL BEGIN ` +
			trendEventPositions("NEW") + ` WHERE 1` + trendUpsert + ` END`,
		`CREATE TRIGGER IF NOT EXISTS watch_trend_event_trash AFTER UPDATE OF trashed_at ON events WHEN NEW.type IN ` + trendEventTypes + ` BEGIN
 DELETE FROM watch_trend_positions WHERE kind IN ` + trendEventKinds + ` AND id=NEW.id AND NEW.trashed_at IS NOT NULL;
 ` + trendEventPositions("NEW") + ` WHERE NEW.trashed_at IS NULL` + trendUpsert + `
 ` + strings.Replace(openAsk, "a.state='open'", "a.ask_id=NEW.id AND a.state='open'", 1) + trendUpsert + `
 END`,
		`CREATE TRIGGER IF NOT EXISTS watch_trend_ask_insert AFTER INSERT ON brief_asks WHEN NEW.state='open' BEGIN ` +
			strings.Replace(openAsk, "a.state='open'", "a.ask_id=NEW.ask_id AND a.state='open'", 1) + trendUpsert + ` END`,
		`CREATE TRIGGER IF NOT EXISTS watch_trend_ask_state AFTER UPDATE OF state ON brief_asks BEGIN
 DELETE FROM watch_trend_positions WHERE kind='open_ask' AND id=NEW.ask_id AND NEW.state<>'open';
 ` + strings.Replace(openAsk, "a.state='open'", "a.ask_id=NEW.ask_id AND a.state='open'", 1) + trendUpsert + `
 END`,

		`CREATE TRIGGER IF NOT EXISTS watch_trend_card_insert AFTER INSERT ON cards BEGIN ` +
			trendCardPositions(`FROM cards c WHERE c.id=NEW.id`) + ` END`,
		`CREATE TRIGGER IF NOT EXISTS watch_trend_card_update AFTER UPDATE OF column_key,archived_at,trashed_at,created_at,updated_at,board_id,thread_id ON cards BEGIN
 DELETE FROM watch_trend_positions WHERE kind IN ('card','card_done') AND id=NEW.id;
 ` + trendCardPositions(`FROM cards c WHERE c.id=NEW.id`) + `
 UPDATE watch_trend_positions SET scope=(SELECT ` + trendCardScope("c") + ` FROM cards c WHERE c.id=NEW.id) WHERE kind IN ('promise_open','promise_closed','loop') AND id=NEW.id;
 UPDATE watch_trend_positions SET scope=` + trendThreadScope("NEW.thread_id") + ` WHERE kind IN ` + trendEventKinds + ` AND (OLD.board_id IS NOT NEW.board_id OR OLD.thread_id IS NOT NEW.thread_id) AND id IN (SELECT id FROM events WHERE thread_id=NEW.thread_id);
 END`,
		`CREATE TRIGGER IF NOT EXISTS watch_trend_card_delete AFTER DELETE ON cards BEGIN
 DELETE FROM watch_trend_positions WHERE kind IN ` + trendCardKinds + ` AND id=OLD.id;
 END`,

		`CREATE TRIGGER IF NOT EXISTS watch_trend_initiative_insert AFTER INSERT ON brief_initiatives BEGIN ` +
			trendInitiativePositions(`FROM brief_initiatives bi JOIN cards c ON c.id=bi.card_id WHERE bi.card_id=NEW.card_id`) + ` END`,
		`CREATE TRIGGER IF NOT EXISTS watch_trend_initiative_update AFTER UPDATE ON brief_initiatives BEGIN
 DELETE FROM watch_trend_positions WHERE kind IN ('promise_open','promise_closed','loop') AND id=NEW.card_id;
 ` + trendInitiativePositions(`FROM brief_initiatives bi JOIN cards c ON c.id=bi.card_id WHERE bi.card_id=NEW.card_id`) + `
 END`,
		`CREATE TRIGGER IF NOT EXISTS watch_trend_initiative_delete AFTER DELETE ON brief_initiatives BEGIN
 DELETE FROM watch_trend_positions WHERE kind IN ('promise_open','promise_closed','loop') AND id=OLD.card_id;
 END`,

		// A privacy change on a thread re-keys what it contains: its own events,
		// cards backed by it, and for a board thread every card on the board and
		// those cards' events. Rare, and bounded by the container's size.
		`CREATE TRIGGER IF NOT EXISTS watch_trend_thread_scope AFTER UPDATE OF body_json ON threads
 WHEN COALESCE(json_extract(OLD.body_json,'$.pm_actor_id'),'') IS NOT COALESCE(json_extract(NEW.body_json,'$.pm_actor_id'),'') BEGIN
 UPDATE watch_trend_positions SET scope=` + trendThreadScope("NEW.id") + ` WHERE kind IN ` + trendEventKinds + ` AND id IN (SELECT id FROM events WHERE thread_id=NEW.id);
 UPDATE watch_trend_positions SET scope=(SELECT ` + trendCardScope("c") + ` FROM cards c WHERE c.id=watch_trend_positions.id) WHERE kind IN ` + trendCardKinds + `
  AND id IN (SELECT id FROM cards WHERE thread_id=NEW.id UNION SELECT c.id FROM boards b JOIN cards c ON c.board_id=b.id WHERE b.thread_id=NEW.id);
 UPDATE watch_trend_positions SET scope=(SELECT ` + trendThreadScope("e.thread_id") + ` FROM events e WHERE e.id=watch_trend_positions.id) WHERE kind IN ` + trendEventKinds + `
  AND id IN (SELECT e.id FROM boards b JOIN cards c ON c.board_id=b.id JOIN events e ON e.thread_id=c.thread_id WHERE b.thread_id=NEW.id);
 END`,

		// Backfill through the same expressions as the triggers.
		trendEventPositions("e") + ` FROM events e WHERE e.type IN ` + trendEventTypes + ` AND e.trashed_at IS NULL` + trendUpsert,
		openAsk + trendUpsert,
	}
	// The card and initiative helpers emit two upserts each; their SQL holds
	// no other semicolon.
	statements = append(statements, strings.Split(trendCardPositions(`FROM cards c WHERE 1`), ";")...)
	statements = append(statements, strings.Split(trendInitiativePositions(`FROM brief_initiatives bi JOIN cards c ON c.id=bi.card_id WHERE 1`), ";")...)
	for _, q := range statements {
		if strings.TrimSpace(q) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
