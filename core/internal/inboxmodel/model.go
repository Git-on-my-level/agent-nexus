// Package inboxmodel defines the small, transitional scoped inbox index.
// It contains no payload or authority: serving must join the legacy scoped
// canonical relation before returning anything. Certified feeds remain staged.
package inboxmodel

const BatchSize = 64
const MaxCandidates = 512

const RankSQL = `CASE anx_unicode_trim(category) WHEN 'escalate' THEN 0 WHEN 'ask' THEN 1 WHEN 'review' THEN 2 ELSE 99 END`

const SchemaSQL = `CREATE TABLE IF NOT EXISTS scope_inbox_live (
 id TEXT PRIMARY KEY, scope_id TEXT NOT NULL, category_rank INTEGER NOT NULL,
 trigger_at TEXT NOT NULL
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS scope_inbox_live_order ON scope_inbox_live(category_rank,trigger_at DESC,id);
CREATE INDEX IF NOT EXISTS scope_inbox_live_scope_order ON scope_inbox_live(scope_id,category_rank,trigger_at DESC,id);
CREATE VIEW IF NOT EXISTS scope_inbox_live_positions AS SELECT id,scope_id,category_rank,trigger_at FROM scope_inbox_live;
CREATE TABLE IF NOT EXISTS scope_inbox_live_job(singleton INTEGER PRIMARY KEY CHECK(singleton=1),cursor TEXT NOT NULL DEFAULT '',done INTEGER NOT NULL DEFAULT 0 CHECK(done IN (0,1)));
INSERT OR IGNORE INTO scope_inbox_live_job(singleton) VALUES(1);
CREATE TRIGGER IF NOT EXISTS scope_inbox_live_insert AFTER INSERT ON derived_inbox_items BEGIN
 INSERT OR REPLACE INTO scope_inbox_live SELECT id,thread_id,` + RankSQL + `,trigger_at FROM derived_inbox_items WHERE id=NEW.id;
END;
CREATE TRIGGER IF NOT EXISTS scope_inbox_live_update AFTER UPDATE OF id,thread_id,category,trigger_at ON derived_inbox_items BEGIN
 DELETE FROM scope_inbox_live WHERE id=OLD.id;
 INSERT OR REPLACE INTO scope_inbox_live SELECT id,thread_id,` + RankSQL + `,trigger_at FROM derived_inbox_items WHERE id=NEW.id;
END;
CREATE TRIGGER IF NOT EXISTS scope_inbox_live_delete AFTER DELETE ON derived_inbox_items BEGIN
 DELETE FROM scope_inbox_live WHERE id=OLD.id;
END;`
