package storage

import (
	"context"
	"database/sql"
	"strings"
)

// Positions route candidates to capped indexed seeks; they never grant access
// and carry no privacy decision of their own. Each row is one record family's
// identity/time cursor (kind, id, at_key). The unscoped Watch calculation
// bounds physical reads through (kind,at_key,id) and, for completions,
// (kind,card_id,at_key,id), then hydrates canonical rows that keep their own
// authorization.
const trendUpsert = ` ON CONFLICT(kind,id) DO UPDATE SET at_key=excluded.at_key,card_id=excluded.card_id;`
const trendEventTypes = `('human_attention_requested','human_attention_responded','run_recorded','card_moved')`
const trendEventKinds = `('ask','answer','run','open_ask','done_event')`
const trendCardKinds = `('card','card_done','promise_open','promise_closed','loop')`

func trendEventPositions(from string) string {
	return `INSERT INTO watch_trend_positions(kind,id,at_key,card_id) SELECT
 CASE e.type WHEN 'human_attention_requested' THEN 'ask' WHEN 'human_attention_responded' THEN 'answer' WHEN 'card_moved' THEN 'done_event' ELSE 'run' END,
 e.id,anx_timestamp_key(e.ts),CASE WHEN e.type='card_moved' THEN COALESCE(json_extract(e.payload_json,'$.payload.card_id'),'') ELSE '' END ` + from + `
 AND e.type IN ` + trendEventTypes + ` AND COALESCE(e.trashed_at,'')=''
 AND (e.type<>'card_moved' OR (json_extract(e.payload_json,'$.payload.column_key')='done' AND COALESCE(json_extract(e.payload_json,'$.payload.from_column_key'),'')<>'done'))` + trendUpsert +
		// Backlog candidates include resolved asks; open state is derived at
		// read time from the resolutions visible in the calculation snapshot.
		`INSERT INTO watch_trend_positions(kind,id,at_key) SELECT 'open_ask',e.id,anx_timestamp_key(e.ts) ` + from + ` AND e.type='human_attention_requested' AND COALESCE(e.trashed_at,'')=''` + trendUpsert
}

func trendCardPositions(from string) string {
	return `INSERT INTO watch_trend_positions(kind,id,at_key) SELECT 'card',c.id,anx_timestamp_key(c.created_at) ` + from + ` AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''` + trendUpsert +
		`INSERT INTO watch_trend_positions(kind,id,at_key) SELECT 'card_done',c.id,anx_timestamp_key(c.updated_at) ` + from + ` AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')='' AND c.column_key='done'` + trendUpsert
}

func trendInitiativePositions(from string) string {
	return `INSERT INTO watch_trend_positions(kind,id,at_key) SELECT CASE WHEN bi.open=1 THEN 'promise_open' ELSE 'promise_closed' END,bi.card_id,anx_timestamp_key(bi.promise_at) ` + from + ` AND bi.promise_at IS NOT NULL` + trendUpsert +
		`INSERT INTO watch_trend_positions(kind,id,at_key) SELECT 'loop',bi.card_id,COALESCE(NULLIF(bi.loop_annotated_at,''),'') ` + from + ` AND bi.loop_annotated=1` + trendUpsert
}

func installWatchTrendPositions(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"events", "cards", "brief_initiatives"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !exists {
			return err
		}
	}
	// This is a rebuildable index, not canonical data. Migration-repair tests
	// replay older migrations over an existing latest schema, and admission
	// rebuilds can replace its trigger hosts. Reinstall every owned trigger and
	// backfill from canonical rows rather than retaining stale programs/keys.
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='trigger' AND name GLOB 'watch_trend_*'`)
	if err != nil {
		return err
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER "`+strings.ReplaceAll(name, `"`, `""`)+`"`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS watch_trend_positions`); err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE watch_trend_positions(kind TEXT NOT NULL,id TEXT NOT NULL,at_key TEXT NOT NULL,card_id TEXT NOT NULL DEFAULT '',PRIMARY KEY(kind,id)) WITHOUT ROWID`,
		`CREATE INDEX idx_watch_trend_all ON watch_trend_positions(kind,at_key,id)`,
		`CREATE INDEX idx_watch_trend_done_all ON watch_trend_positions(kind,card_id,at_key,id) WHERE kind='done_event'`,
		`CREATE TRIGGER watch_trend_event_insert AFTER INSERT ON events WHEN NEW.type IN ` + trendEventTypes + ` BEGIN ` + trendEventPositions(`FROM events e WHERE e.id=NEW.id`) + ` END`,
		`CREATE TRIGGER watch_trend_event_update AFTER UPDATE OF ts,trashed_at ON events WHEN NEW.type IN ` + trendEventTypes + ` BEGIN DELETE FROM watch_trend_positions WHERE kind IN ` + trendEventKinds + ` AND id=NEW.id; ` + trendEventPositions(`FROM events e WHERE e.id=NEW.id`) + ` END`,
		`CREATE TRIGGER watch_trend_event_delete AFTER DELETE ON events BEGIN DELETE FROM watch_trend_positions WHERE kind IN ` + trendEventKinds + ` AND id=OLD.id; END`,
		`CREATE TRIGGER watch_trend_card_insert AFTER INSERT ON cards BEGIN ` + trendCardPositions(`FROM cards c WHERE c.id=NEW.id`) + ` END`,
		`CREATE TRIGGER watch_trend_card_update AFTER UPDATE OF column_key,archived_at,trashed_at,created_at,updated_at ON cards BEGIN DELETE FROM watch_trend_positions WHERE kind IN ('card','card_done') AND id=NEW.id; ` + trendCardPositions(`FROM cards c WHERE c.id=NEW.id`) + ` END`,
		`CREATE TRIGGER watch_trend_card_delete AFTER DELETE ON cards BEGIN DELETE FROM watch_trend_positions WHERE kind IN ` + trendCardKinds + ` AND id=OLD.id; END`,
		`CREATE TRIGGER watch_trend_initiative_insert AFTER INSERT ON brief_initiatives BEGIN ` + trendInitiativePositions(`FROM brief_initiatives bi JOIN cards c ON c.id=bi.card_id WHERE bi.card_id=NEW.card_id`) + ` END`,
		`CREATE TRIGGER watch_trend_initiative_update AFTER UPDATE ON brief_initiatives BEGIN DELETE FROM watch_trend_positions WHERE kind IN ('promise_open','promise_closed','loop') AND id=NEW.card_id; ` + trendInitiativePositions(`FROM brief_initiatives bi JOIN cards c ON c.id=bi.card_id WHERE bi.card_id=NEW.card_id`) + ` END`,
		`CREATE TRIGGER watch_trend_initiative_delete AFTER DELETE ON brief_initiatives BEGIN DELETE FROM watch_trend_positions WHERE kind IN ('promise_open','promise_closed','loop') AND id=OLD.card_id; END`,
	}
	statements = append(statements, strings.Split(trendEventPositions(`FROM events e WHERE 1`), ";")...)
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
