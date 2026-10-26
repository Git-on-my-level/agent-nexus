package storage

import (
	"context"
	"database/sql"
	"strings"
)

// Positions route candidates, never grant access. Derive their partitions from
// the canonical inherited-owner index, including every exact/prose/structural
// edge, rather than maintaining a second, incomplete privacy policy here.
// Union the exact and compact ledgers conservatively: disagreement may omit a
// candidate, but cannot leave a denied candidate in a readable partition.
// NULL is the unservable partition (conflicting owners/unconditional denial).
// scope has no affinity, preserving legacy numeric owner comparison semantics.
func trendOwnerScope(sources ...string) string {
	queries := make([]string, len(sources))
	for i, source := range sources {
		queries[i] = `SELECT unconditional,owner FROM resource_access_admission_owners WHERE ledger IN (-1,1,2) AND ` + source
	}
	return `(SELECT CASE WHEN MAX(unconditional)=1 OR COUNT(DISTINCT owner)>1 OR MAX(typeof(owner)<>'text')=1 THEN NULL ELSE COALESCE(MIN(owner),'') END FROM (` + strings.Join(queries, ` UNION ALL `) + `))`
}

func trendEventScope(id string) string {
	return trendOwnerScope(`kind='event' AND id=`+id,
		`kind='event' AND id=(SELECT request_event_id FROM human_attention_request_resolutions WHERE resolution_event_id=`+id+`)`)
}

func trendCardScope(id, kind string) string {
	// Only promises read the scoped brief projection. Created/done cards and
	// loop definitions must not disappear because of an unrelated hidden plan.
	card := `kind='card' AND id=` + id
	if kind == "promise" {
		return trendOwnerScope(card, `kind='plan' AND id=`+id, `kind='brief_progress' AND id='plan-facts/'||`+id)
	}
	return trendOwnerScope(card)
}

const trendUpsert = ` ON CONFLICT(kind,id) DO UPDATE SET scope=excluded.scope,at_key=excluded.at_key,card_id=excluded.card_id;`
const trendEventTypes = `('human_attention_requested','human_attention_responded','run_recorded','card_moved')`
const trendEventKinds = `('ask','answer','run','open_ask','done_event')`
const trendCardKinds = `('card','card_done','promise_open','promise_closed','loop')`

func trendEventPositions(from string) string {
	return `INSERT INTO watch_trend_positions(kind,id,scope,at_key,card_id) SELECT
 CASE e.type WHEN 'human_attention_requested' THEN 'ask' WHEN 'human_attention_responded' THEN 'answer' WHEN 'card_moved' THEN 'done_event' ELSE 'run' END,
 e.id,` + trendEventScope("e.id") + `,anx_timestamp_key(e.ts),CASE WHEN e.type='card_moved' THEN COALESCE(json_extract(e.payload_json,'$.payload.card_id'),'') ELSE '' END ` + from + `
 AND e.type IN ` + trendEventTypes + ` AND COALESCE(e.trashed_at,'')=''
 AND (e.type<>'card_moved' OR (json_extract(e.payload_json,'$.payload.column_key')='done' AND COALESCE(json_extract(e.payload_json,'$.payload.from_column_key'),'')<>'done'))` + trendUpsert +
		// Backlog candidates include resolved asks: only the reader's visible
		// response can resolve them. Never remove one on a hidden resolution.
		`INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT 'open_ask',e.id,` + trendEventScope("e.id") + `,anx_timestamp_key(e.ts) ` + from + ` AND e.type='human_attention_requested' AND COALESCE(e.trashed_at,'')=''` + trendUpsert
}

func trendCardPositions(from string) string {
	return `INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT 'card',c.id,` + trendCardScope("c.id", "card") + `,anx_timestamp_key(c.created_at) ` + from + ` AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''` + trendUpsert +
		`INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT 'card_done',c.id,` + trendCardScope("c.id", "card") + `,anx_timestamp_key(c.updated_at) ` + from + ` AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')='' AND c.column_key='done'` + trendUpsert
}

func trendInitiativePositions(from string) string {
	return `INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT CASE WHEN bi.open=1 THEN 'promise_open' ELSE 'promise_closed' END,bi.card_id,` + trendCardScope("c.id", "promise") + `,anx_timestamp_key(bi.promise_at) ` + from + ` AND bi.promise_at IS NOT NULL` + trendUpsert +
		`INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT 'loop',bi.card_id,` + trendCardScope("c.id", "card") + `,COALESCE(NULLIF(bi.loop_annotated_at,''),'') ` + from + ` AND bi.loop_annotated=1` + trendUpsert
}

func trendRekeyEvent(ids string) string {
	return `UPDATE watch_trend_positions SET scope=` + trendEventScope("watch_trend_positions.id") + ` WHERE kind IN ` + trendEventKinds + ` AND id IN (` + ids + `);`
}

func installWatchTrendPositions(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"events", "cards", "brief_initiatives", "resource_access_admission_owners", "human_attention_request_resolutions"} {
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
		`CREATE TABLE watch_trend_positions(kind TEXT NOT NULL,id TEXT NOT NULL,scope,at_key TEXT NOT NULL,card_id TEXT NOT NULL DEFAULT '',PRIMARY KEY(kind,id)) WITHOUT ROWID`,
		`CREATE INDEX idx_watch_trend_all ON watch_trend_positions(kind,at_key,id)`,
		`CREATE INDEX idx_watch_trend_done_all ON watch_trend_positions(kind,card_id,at_key,id) WHERE kind='done_event'`,
		`CREATE INDEX idx_watch_trend_positions ON watch_trend_positions(kind,scope,at_key,id)`,
		`CREATE INDEX idx_watch_trend_done ON watch_trend_positions(kind,scope,card_id,at_key,id) WHERE kind='done_event'`,
		`CREATE TRIGGER watch_trend_event_insert AFTER INSERT ON events WHEN NEW.type IN ` + trendEventTypes + ` BEGIN ` + trendEventPositions(`FROM events e WHERE e.id=NEW.id`) + ` END`,
		`CREATE TRIGGER watch_trend_event_update AFTER UPDATE OF ts,trashed_at ON events WHEN NEW.type IN ` + trendEventTypes + ` BEGIN DELETE FROM watch_trend_positions WHERE kind IN ` + trendEventKinds + ` AND id=NEW.id; ` + trendEventPositions(`FROM events e WHERE e.id=NEW.id`) + ` END`,
		`CREATE TRIGGER watch_trend_event_delete AFTER DELETE ON events BEGIN DELETE FROM watch_trend_positions WHERE kind IN ` + trendEventKinds + ` AND id=OLD.id; END`,
		`CREATE TRIGGER watch_trend_resolution_insert AFTER INSERT ON human_attention_request_resolutions BEGIN ` + trendRekeyEvent(`SELECT NEW.resolution_event_id`) + ` END`,
		`CREATE TRIGGER watch_trend_resolution_delete AFTER DELETE ON human_attention_request_resolutions BEGIN ` + trendRekeyEvent(`SELECT OLD.resolution_event_id`) + ` END`,
		`CREATE TRIGGER watch_trend_card_insert AFTER INSERT ON cards BEGIN ` + trendCardPositions(`FROM cards c WHERE c.id=NEW.id`) + ` END`,
		`CREATE TRIGGER watch_trend_card_update AFTER UPDATE OF column_key,archived_at,trashed_at,created_at,updated_at ON cards BEGIN DELETE FROM watch_trend_positions WHERE kind IN ('card','card_done') AND id=NEW.id; ` + trendCardPositions(`FROM cards c WHERE c.id=NEW.id`) + ` END`,
		`CREATE TRIGGER watch_trend_card_delete AFTER DELETE ON cards BEGIN DELETE FROM watch_trend_positions WHERE kind IN ` + trendCardKinds + ` AND id=OLD.id; END`,
		`CREATE TRIGGER watch_trend_initiative_insert AFTER INSERT ON brief_initiatives BEGIN ` + trendInitiativePositions(`FROM brief_initiatives bi JOIN cards c ON c.id=bi.card_id WHERE bi.card_id=NEW.card_id`) + ` END`,
		`CREATE TRIGGER watch_trend_initiative_update AFTER UPDATE ON brief_initiatives BEGIN DELETE FROM watch_trend_positions WHERE kind IN ('promise_open','promise_closed','loop') AND id=NEW.card_id; ` + trendInitiativePositions(`FROM brief_initiatives bi JOIN cards c ON c.id=bi.card_id WHERE bi.card_id=NEW.card_id`) + ` END`,
		`CREATE TRIGGER watch_trend_initiative_delete AFTER DELETE ON brief_initiatives BEGIN DELETE FROM watch_trend_positions WHERE kind IN ('promise_open','promise_closed','loop') AND id=OLD.card_id; END`,
	}
	// Ownership insert/delete changes partitions in the same canonical write
	// transaction, after any inherited-source, identity, graph or owner change.
	// Count-only owner updates do not change classification. No trigger ordering
	// assumption: positions read current owners on insertion, and owner changes
	// update already inserted positions.
	for _, op := range []string{"insert", "delete"} {
		row := "NEW."
		if op == "delete" {
			row = "OLD."
		}
		id := row + "id"
		statements = append(statements,
			`CREATE TRIGGER watch_trend_owner_`+op+` AFTER `+strings.ToUpper(op)+` ON resource_access_admission_owners BEGIN `+
				`UPDATE watch_trend_positions SET scope=`+trendCardScope("watch_trend_positions.id", "card")+` WHERE `+row+`kind='card' AND kind IN ('card','card_done','loop') AND id=`+id+`; `+
				`UPDATE watch_trend_positions SET scope=`+trendCardScope("watch_trend_positions.id", "promise")+` WHERE kind IN ('promise_open','promise_closed') AND id=CASE WHEN `+row+`kind IN ('card','plan') THEN `+id+` WHEN `+row+`kind='brief_progress' THEN substr(`+id+`,12) END; `+
				trendRekeyEvent(`SELECT `+id+` WHERE `+row+`kind='event' UNION SELECT resolution_event_id FROM human_attention_request_resolutions WHERE `+row+`kind='event' AND request_event_id=`+id)+` END`)
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
