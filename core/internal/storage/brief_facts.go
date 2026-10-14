package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"time"

	"agent-nexus-core/internal/plans"
	"modernc.org/sqlite"
)

func init() {
	// anx_resolve_when(raw, from) resolves the review_by grammar (date,
	// RFC3339 or s/m/h/d duration) against an RFC3339 base and returns the
	// RFC3339 UTC instant, or NULL. Brief fact triggers use it so decide_by and
	// note promises are resolved once, at write time, on every write path.
	sqlite.MustRegisterDeterministicScalarFunction("anx_resolve_when", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		raw, _ := args[0].(string)
		base, _ := args[1].(string)
		from, err := time.Parse(time.RFC3339Nano, base)
		if err != nil {
			return nil, nil
		}
		at, ok := plans.ParseWhen(raw, from)
		if !ok {
			return nil, nil
		}
		return at.UTC().Format(time.RFC3339), nil
	})
}

// briefEvidenceSQL is 1 when at least one of the first 8 evidence refs on the
// NEW message resolves: an http(s) URL, or an existing event, card, artifact
// or document. Bounded by the LIMIT and primary-key or handle lookups.
const briefEvidenceSQL = `(SELECT COUNT(*)>0 FROM (SELECT value FROM json_each(CASE WHEN json_type(NEW.payload_json,'$.payload.evidence_refs')='array' THEN json_extract(NEW.payload_json,'$.payload.evidence_refs') ELSE '[]' END) LIMIT 8) v WHERE typeof(v.value)='text' AND (
 v.value LIKE 'http://_%' OR v.value LIKE 'https://_%'
 OR (substr(v.value,1,6)='event:' AND EXISTS(SELECT 1 FROM events x WHERE x.id=substr(v.value,7) OR x.handle=substr(v.value,7)))
 OR (substr(v.value,1,5)='card:' AND EXISTS(SELECT 1 FROM cards x WHERE x.id=substr(v.value,6) OR x.handle=substr(v.value,6)))
 OR (substr(v.value,1,9)='artifact:' AND EXISTS(SELECT 1 FROM artifacts x WHERE x.id=substr(v.value,10)))
 OR (substr(v.value,1,9)='document:' AND EXISTS(SELECT 1 FROM documents x WHERE x.id=substr(v.value,10) OR x.handle=substr(v.value,10)))))`

// installBriefFacts materializes brief.v1 facts on write events (Move 2):
// asks with resolved decide_by and blocks, card notes with resolved next and
// evidence, and a dirty queue of cards whose promise fold must be refreshed.
// Durations are computed at read time. Existing rows are backfilled once here.
func installBriefFacts(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"events", "cards", "card_plans", "work_metadata", "human_attention_request_resolutions", "artifacts", "documents"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !exists {
			return err
		}
	}
	kind := `COALESCE(NULLIF(trim(json_extract(NEW.payload_json,'$.payload.kind')),''),'ask')`
	statements := []string{
		`CREATE TABLE IF NOT EXISTS brief_asks(
			ask_id TEXT PRIMARY KEY,
			inbox_item_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			requester TEXT NOT NULL,
			recipient TEXT NOT NULL DEFAULT '',
			subject_ref TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			decide_by TEXT,
			blocks_json TEXT NOT NULL DEFAULT '[]',
			blocks_count INTEGER NOT NULL DEFAULT 0,
			priority TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT 'open',
			answered_at TEXT,
			responder TEXT NOT NULL DEFAULT '',
			response_event_id TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_brief_asks_open_decide_by ON brief_asks(state, decide_by)`,
		`CREATE INDEX IF NOT EXISTS idx_brief_asks_requester ON brief_asks(requester, state, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_brief_asks_answered ON brief_asks(requester, state, answered_at)`,
		`CREATE INDEX IF NOT EXISTS idx_brief_asks_subject ON brief_asks(subject_ref, state)`,
		`CREATE TABLE IF NOT EXISTS brief_notes(
			event_id TEXT PRIMARY KEY,
			card_id TEXT NOT NULL,
			author TEXT NOT NULL,
			ts TEXT NOT NULL,
			next_at TEXT,
			next_what TEXT NOT NULL DEFAULT '',
			evidence INTEGER NOT NULL DEFAULT 0,
			excerpt TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_brief_notes_card_ts ON brief_notes(card_id, ts DESC)`,
		`CREATE TABLE IF NOT EXISTS brief_initiatives(
			card_id TEXT PRIMARY KEY,
			open INTEGER NOT NULL DEFAULT 1,
			promise_at TEXT,
			promise_what TEXT NOT NULL DEFAULT '',
			promise_source TEXT NOT NULL DEFAULT '',
			promise_source_ref TEXT NOT NULL DEFAULT '',
			promise_author TEXT NOT NULL DEFAULT '',
			promise_moves INTEGER NOT NULL DEFAULT 0,
			last_evidence_at TEXT,
			step_status_sig TEXT NOT NULL DEFAULT '',
			step_change_at TEXT,
			computed_at TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS idx_brief_initiatives_promise ON brief_initiatives(open, promise_at)`,
		`CREATE INDEX IF NOT EXISTS idx_brief_initiatives_evidence ON brief_initiatives(open, last_evidence_at)`,
		`CREATE TABLE IF NOT EXISTS brief_dirty(card_id TEXT PRIMARY KEY)`,
		`CREATE TRIGGER IF NOT EXISTS brief_asks_insert AFTER INSERT ON events WHEN NEW.type='human_attention_requested' BEGIN
 INSERT INTO brief_asks(ask_id,inbox_item_id,kind,requester,recipient,subject_ref,title,created_at,decide_by,blocks_json,blocks_count,priority)
 VALUES(NEW.id,
  'inbox:'||` + kind + `||':'||COALESCE(NEW.thread_id,'')||':'||COALESCE(NULLIF(trim(json_extract(NEW.payload_json,'$.payload.request_id')),''),NEW.id)||':'||NEW.id,
  ` + kind + `,
  COALESCE(NULLIF(trim(json_extract(NEW.payload_json,'$.payload.requester_actor_id')),''),NEW.actor_id,''),
  COALESCE(trim(json_extract(NEW.payload_json,'$.payload.recipient_actor_id')),''),
  COALESCE(trim(json_extract(NEW.payload_json,'$.payload.subject_ref')),''),
  substr(COALESCE(json_extract(NEW.payload_json,'$.payload.title'),''),1,500),
  NEW.ts,
  anx_resolve_when(json_extract(NEW.payload_json,'$.payload.by'),NEW.ts),
  CASE WHEN json_type(NEW.payload_json,'$.payload.blocks')='array' THEN json_extract(NEW.payload_json,'$.payload.blocks') ELSE '[]' END,
  CASE WHEN json_type(NEW.payload_json,'$.payload.blocks')='array' THEN min(json_array_length(NEW.payload_json,'$.payload.blocks'),20) ELSE 0 END,
  lower(COALESCE(trim(json_extract(NEW.payload_json,'$.payload.priority')),''))) ON CONFLICT DO NOTHING;
 INSERT INTO brief_dirty(card_id) SELECT c.id FROM cards c WHERE substr(json_extract(NEW.payload_json,'$.payload.subject_ref'),1,5)='card:' AND (c.id=substr(json_extract(NEW.payload_json,'$.payload.subject_ref'),6) OR c.handle=substr(json_extract(NEW.payload_json,'$.payload.subject_ref'),6)) ON CONFLICT DO NOTHING;
 END`,
		`CREATE TRIGGER IF NOT EXISTS brief_asks_resolve AFTER INSERT ON human_attention_request_resolutions BEGIN
 UPDATE brief_asks SET state=NEW.resolution_type, answered_at=NEW.created_at, responder=COALESCE(NEW.actor_id,''), response_event_id=COALESCE(NEW.resolution_event_id,'') WHERE ask_id=NEW.request_event_id;
 END`,
		`CREATE TRIGGER IF NOT EXISTS brief_notes_insert AFTER INSERT ON events WHEN NEW.type='message_posted' AND COALESCE(NEW.thread_id,'')<>'' BEGIN
 INSERT INTO brief_notes(event_id,card_id,author,ts,next_at,next_what,evidence,excerpt)
 SELECT NEW.id,c.id,COALESCE(NEW.actor_id,''),NEW.ts,
  anx_resolve_when(json_extract(NEW.payload_json,'$.payload.next.at'),NEW.ts),
  substr(COALESCE(json_extract(NEW.payload_json,'$.payload.next.what'),''),1,160),
  ` + briefEvidenceSQL + `,
  substr(COALESCE(json_extract(NEW.payload_json,'$.payload.body'),''),1,280)
 FROM cards c WHERE c.thread_id=NEW.thread_id ON CONFLICT DO NOTHING;
 INSERT INTO brief_dirty(card_id) SELECT c.id FROM cards c WHERE c.thread_id=NEW.thread_id ON CONFLICT DO NOTHING;
 END`,
		`CREATE TRIGGER IF NOT EXISTS brief_dirty_card_insert AFTER INSERT ON cards BEGIN INSERT INTO brief_dirty(card_id) VALUES(NEW.id) ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS brief_dirty_card_update AFTER UPDATE OF column_key,archived_at,trashed_at,due_at,board_id ON cards BEGIN INSERT INTO brief_dirty(card_id) VALUES(NEW.id) ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS brief_dirty_plan_insert AFTER INSERT ON card_plans BEGIN INSERT INTO brief_dirty(card_id) VALUES(NEW.card_id) ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS brief_dirty_plan_update AFTER UPDATE ON card_plans BEGIN INSERT INTO brief_dirty(card_id) VALUES(NEW.card_id) ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS brief_dirty_metadata_insert AFTER INSERT ON work_metadata BEGIN INSERT INTO brief_dirty(card_id) VALUES(NEW.card_id) ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS brief_dirty_metadata_update AFTER UPDATE OF metadata_json,latest_observation_id ON work_metadata BEGIN INSERT INTO brief_dirty(card_id) VALUES(NEW.card_id) ON CONFLICT DO NOTHING; END`,
		// One-time backfill. Asks and notes replay through the same
		// expressions as the triggers; every card is queued for a fold.
		`INSERT OR IGNORE INTO brief_dirty(card_id) SELECT id FROM cards`,
	}
	for _, q := range statements {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	backfill := []string{
		`INSERT OR IGNORE INTO brief_asks(ask_id,inbox_item_id,kind,requester,recipient,subject_ref,title,created_at,decide_by,blocks_json,blocks_count,priority)
 SELECT NEW.id,'inbox:'||` + kind + `||':'||COALESCE(NEW.thread_id,'')||':'||COALESCE(NULLIF(trim(json_extract(NEW.payload_json,'$.payload.request_id')),''),NEW.id)||':'||NEW.id,` + kind + `,
  COALESCE(NULLIF(trim(json_extract(NEW.payload_json,'$.payload.requester_actor_id')),''),NEW.actor_id,''),
  COALESCE(trim(json_extract(NEW.payload_json,'$.payload.recipient_actor_id')),''),
  COALESCE(trim(json_extract(NEW.payload_json,'$.payload.subject_ref')),''),
  substr(COALESCE(json_extract(NEW.payload_json,'$.payload.title'),''),1,500),NEW.ts,
  anx_resolve_when(json_extract(NEW.payload_json,'$.payload.by'),NEW.ts),
  CASE WHEN json_type(NEW.payload_json,'$.payload.blocks')='array' THEN json_extract(NEW.payload_json,'$.payload.blocks') ELSE '[]' END,
  CASE WHEN json_type(NEW.payload_json,'$.payload.blocks')='array' THEN min(json_array_length(NEW.payload_json,'$.payload.blocks'),20) ELSE 0 END,
  lower(COALESCE(trim(json_extract(NEW.payload_json,'$.payload.priority')),''))
 FROM events NEW WHERE NEW.type='human_attention_requested'`,
		`UPDATE brief_asks SET (state,answered_at,responder,response_event_id)=(SELECT r.resolution_type,r.created_at,COALESCE(r.actor_id,''),COALESCE(r.resolution_event_id,'') FROM human_attention_request_resolutions r WHERE r.request_event_id=brief_asks.ask_id ORDER BY r.created_at LIMIT 1) WHERE EXISTS(SELECT 1 FROM human_attention_request_resolutions r WHERE r.request_event_id=brief_asks.ask_id)`,
		`INSERT OR IGNORE INTO brief_notes(event_id,card_id,author,ts,next_at,next_what,evidence,excerpt)
 SELECT NEW.id,c.id,COALESCE(NEW.actor_id,''),NEW.ts,
  anx_resolve_when(json_extract(NEW.payload_json,'$.payload.next.at'),NEW.ts),
  substr(COALESCE(json_extract(NEW.payload_json,'$.payload.next.what'),''),1,160),
  ` + briefEvidenceSQL + `,
  substr(COALESCE(json_extract(NEW.payload_json,'$.payload.body'),''),1,280)
 FROM events NEW JOIN cards c ON c.thread_id=NEW.thread_id WHERE NEW.type='message_posted' AND COALESCE(NEW.thread_id,'')<>''`,
	}
	for _, q := range backfill {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// installBriefAskBlocks turns blocks lookups by ref and undated decision
// pages into index seeks: one row per (ask, blocked ref), and composite
// indexes that serve the brief's ORDER BY without a sort over open asks.
func installBriefAskBlocks(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "brief_asks")
	if err != nil || !exists {
		return err
	}
	// Triggers from 89 used INSERT OR IGNORE, which an outer statement's
	// conflict policy overrides; reinstall them with ON CONFLICT DO NOTHING.
	for _, name := range []string{"brief_asks_insert", "brief_notes_insert", "brief_dirty_card_insert", "brief_dirty_card_update", "brief_dirty_plan_insert", "brief_dirty_plan_update", "brief_dirty_metadata_insert", "brief_dirty_metadata_update"} {
		if _, err := tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+name); err != nil {
			return err
		}
	}
	if err := installBriefFacts(ctx, tx); err != nil {
		return err
	}
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS brief_ask_blocks(ask_id TEXT NOT NULL, ref TEXT NOT NULL, PRIMARY KEY(ask_id, ref))`,
		`CREATE INDEX IF NOT EXISTS idx_brief_ask_blocks_ref ON brief_ask_blocks(ref, ask_id)`,
		`CREATE TRIGGER IF NOT EXISTS brief_ask_blocks_insert AFTER INSERT ON brief_asks BEGIN
 INSERT INTO brief_ask_blocks(ask_id, ref) SELECT NEW.ask_id, trim(j.value) FROM (SELECT value FROM json_each(NEW.blocks_json) LIMIT 20) j WHERE typeof(j.value)='text' AND trim(j.value)<>'' ON CONFLICT DO NOTHING;
 END`,
		`INSERT OR IGNORE INTO brief_ask_blocks(ask_id, ref) SELECT a.ask_id, trim(j.value) FROM brief_asks a, json_each(a.blocks_json) j WHERE typeof(j.value)='text' AND trim(j.value)<>''`,
		`CREATE INDEX IF NOT EXISTS idx_brief_asks_open_page ON brief_asks(state, decide_by, created_at)`,
		`DROP INDEX IF EXISTS idx_brief_initiatives_promise`,
		`DROP INDEX IF EXISTS idx_brief_initiatives_evidence`,
		`CREATE INDEX IF NOT EXISTS idx_brief_initiatives_page ON brief_initiatives(open, promise_at, last_evidence_at)`,
		`CREATE INDEX IF NOT EXISTS idx_brief_initiatives_done ON brief_initiatives(open, last_evidence_at)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// installBriefNotesNextAtIndex serves S20's seek over notes by promised time.
func installBriefNotesNextAtIndex(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "brief_notes")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_brief_notes_next_at ON brief_notes(next_at, ts) WHERE next_at IS NOT NULL`)
	return err
}
