package storage

import (
	"context"
	"database/sql"
	"slices"
	"strings"
)

// Watch trends admit each record family through watch_trend_positions, keyed
// by the record's privacy scope: the private owner it inherits from its
// backing thread, its card's board thread, the card it is about (subject_ref)
// or, for an answer, the ask it resolves; '' when none is private. A reader
// seeks only the public scope and its own, newest first, with the scoped
// relation joined inside the same LIMIT, so records it cannot see neither take
// window places nor cost examination when they sit in another owner's scope.
// The key steers cost and completeness only: every admitted row is still
// verified by the canonical scoped relation, so a stale key can hide a record,
// never reveal one. Privacy inherited through other references (blocks,
// prose mentions, evidence) is still enforced by the scoped relation; such a
// record keeps a visible scope and costs its reader one check.
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

// trendSubject is an event's subject ref, spelled exactly as
// idx_events_report_subject_ts indexes it.
func trendSubject(alias string) string {
	return `COALESCE(json_extract(` + alias + `.payload_json,'$.subject_ref'),json_extract(` + alias + `.payload_json,'$.payload.subject_ref'))`
}

// trendSubjectScope is the scope of the card an event is about.
func trendSubjectScope(alias string) string {
	subject := trendSubject(alias)
	return `CASE WHEN substr(` + subject + `,1,5)='card:' THEN COALESCE(` +
		`(SELECT ` + trendCardScope("rc") + ` FROM cards rc WHERE rc.id=substr(` + subject + `,6)),` +
		`(SELECT ` + trendCardScope("rc") + ` FROM cards rc WHERE rc.handle=substr(` + subject + `,6)),'') ELSE '' END`
}

// trendOwnEventScope is an event's scope from its thread or its subject.
func trendOwnEventScope(alias string) string {
	return `COALESCE(NULLIF(` + trendThreadScope(alias+".thread_id") + `,''),NULLIF(` + trendSubjectScope(alias) + `,''),'')`
}

// trendEventScope adds, for an answer, the scope of the ask it resolves.
func trendEventScope(alias string) string {
	return `COALESCE(NULLIF(` + trendOwnEventScope(alias) + `,''),` +
		`(SELECT NULLIF(` + trendOwnEventScope("rq") + `,'') FROM human_attention_request_resolutions rr JOIN events rq ON rq.id=rr.request_event_id WHERE rr.resolution_event_id=` + alias + `.id),'')`
}

const trendUpsert = ` ON CONFLICT(kind,id) DO UPDATE SET scope=excluded.scope,at_key=excluded.at_key;`

const trendEventKind = `CASE %s.type WHEN 'human_attention_requested' THEN 'ask' WHEN 'human_attention_responded' THEN 'answer' ELSE 'run' END`

const trendEventTypes = `('human_attention_requested','human_attention_responded','run_recorded')`

func trendEventPositions(row string) string {
	return `INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT ` + strings.ReplaceAll(trendEventKind, "%s", row) + `,` + row + `.id,` + trendEventScope(row) + `,anx_timestamp_key(` + row + `.ts)`
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

// trendRekeyEvents recomputes the scope of the event positions in ids.
func trendRekeyEvents(ids string) string {
	return `UPDATE watch_trend_positions SET scope=(SELECT ` + trendEventScope("e") + ` FROM events e WHERE e.id=watch_trend_positions.id) WHERE kind IN ` + trendEventKinds + ` AND id IN (` + ids + `);`
}

// trendRekeyClauses re-key everything whose scope derives from the cards
// matching cond (card rows aliased k) and, when thread is set, from that
// thread directly. Each clause is one statement so its tests can name it:
//
//   - cards: the cards' own positions (created, done, promises, loops);
//   - thread_events: events on the cards' threads (and on thread);
//   - subject_events: events about the cards (subject_ref);
//   - answers: answers to the asks among both.
func trendRekeyClauses(cond, thread string) map[string]string {
	cards := `cards k WHERE (` + cond + `)`
	threads := `SELECT k.thread_id FROM ` + cards
	if thread != "" {
		threads += ` UNION SELECT ` + thread
	}
	onThreads := `SELECT e.id FROM events e WHERE e.thread_id IN (` + threads + `)`
	about := `SELECT e.id FROM events e WHERE ` + trendSubject("e") + ` IN (SELECT 'card:'||k.id FROM ` + cards + ` UNION SELECT 'card:'||k.handle FROM ` + cards + ` AND k.handle IS NOT NULL AND trim(k.handle)<>'')`
	return map[string]string{
		"cards":          `UPDATE watch_trend_positions SET scope=(SELECT ` + trendCardScope("c") + ` FROM cards c WHERE c.id=watch_trend_positions.id) WHERE kind IN ` + trendCardKinds + ` AND id IN (SELECT k.id FROM ` + cards + `);`,
		"thread_events":  trendRekeyEvents(onThreads),
		"subject_events": trendRekeyEvents(about),
		"answers":        trendRekeyEvents(`SELECT r.resolution_event_id FROM human_attention_request_resolutions r WHERE r.request_event_id IN (` + onThreads + ` UNION ` + about + `)`),
	}
}

// trendRekeyOrder is the order clauses run in; answers last, after their asks.
var trendRekeyOrder = []string{"cards", "thread_events", "subject_events", "answers"}

func trendRekey(cond, thread string, skip ...string) string {
	clauses := trendRekeyClauses(cond, thread)
	out := ""
	for _, name := range trendRekeyOrder {
		if !slices.Contains(skip, name) {
			out += "\n " + clauses[name]
		}
	}
	return out
}

func installWatchTrendPositions(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"events", "cards", "boards", "threads", "brief_asks", "brief_initiatives", "human_attention_request_resolutions"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !exists {
			return err
		}
	}
	openAsk := `INSERT INTO watch_trend_positions(kind,id,scope,at_key) SELECT 'open_ask',a.ask_id,` + trendEventScope("e") + `,anx_timestamp_key(a.created_at) FROM brief_asks a JOIN events e ON e.id=a.ask_id WHERE a.state='open' AND e.trashed_at IS NULL`
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
		// brief_asks rows are written by brief_asks_insert, a trigger on events,
		// so this fires as a trigger fired by a trigger. That is ordinary
		// nesting, not recursion: SQLite's default recursive_triggers=OFF only
		// stops a trigger from re-firing itself, and the pre-existing
		// brief_ask_blocks_insert chain relies on the same behaviour.
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
 END`,
		// A card moved between boards (or re-backed) takes its new privacy to
		// the events that derive from it. Its own positions are re-keyed by
		// watch_trend_card_update above.
		`CREATE TRIGGER IF NOT EXISTS watch_trend_card_move AFTER UPDATE OF board_id,thread_id ON cards
 WHEN OLD.board_id IS NOT NEW.board_id OR OLD.thread_id IS NOT NEW.thread_id BEGIN` +
			trendRekey(`k.id=NEW.id`, "", "cards") + `
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

		// A privacy change on a thread re-keys what derives from it: its own
		// events, the cards it backs or whose board it is, those cards' events,
		// events about those cards and the answers to those asks. Rare, and
		// bounded by the container's size.
		`CREATE TRIGGER IF NOT EXISTS watch_trend_thread_scope AFTER UPDATE OF body_json ON threads
 WHEN COALESCE(json_extract(OLD.body_json,'$.pm_actor_id'),'') IS NOT COALESCE(json_extract(NEW.body_json,'$.pm_actor_id'),'') BEGIN` +
			trendRekey(`k.thread_id=NEW.id OR k.board_id IN (SELECT b.id FROM boards b WHERE b.thread_id=NEW.id)`, "NEW.id") + `
 END`,
		// An answer can be recorded before its resolution links it to its ask.
		`CREATE TRIGGER IF NOT EXISTS watch_trend_resolution_insert AFTER INSERT ON human_attention_request_resolutions BEGIN ` +
			trendRekeyEvents(`SELECT NEW.resolution_event_id`) + ` END`,

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
