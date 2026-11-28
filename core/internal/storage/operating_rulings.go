package storage

import (
	"context"
	"database/sql"

	"agent-nexus-core/internal/contentusage"
	"agent-nexus-core/internal/resourceaccess"
)

func installOperatingRulingUsageTracking(ctx context.Context, tx *sql.Tx) error {
	return contentusage.InstallTable(ctx, tx, "standing_rulings")
}

func installOperatingRulings(ctx context.Context, tx *sql.Tx) error {
	for _, column := range []struct{ name, definition string }{
		{"ruling_id", "TEXT"},
		{"ruling_title", "TEXT NOT NULL DEFAULT ''"},
		{"aliases_json", "TEXT NOT NULL DEFAULT '[]'"},
		{"scope", "TEXT NOT NULL DEFAULT 'workspace'"},
		{"topic", "TEXT NOT NULL DEFAULT ''"},
		{"body", "TEXT NOT NULL DEFAULT ''"},
		{"source_quote", "TEXT NOT NULL DEFAULT ''"},
		{"source", "TEXT NOT NULL DEFAULT ''"},
		{"decided_by", "TEXT NOT NULL DEFAULT ''"},
		{"decided_at", "TEXT NOT NULL DEFAULT ''"},
		{"status", "TEXT NOT NULL DEFAULT ''"},
		{"status_hint", "TEXT NOT NULL DEFAULT ''"},
		{"superseded_by_hint", "TEXT NOT NULL DEFAULT ''"},
		{"superseded_by", "TEXT NOT NULL DEFAULT ''"},
		{"relations_json", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		exists, err := sqliteTableHasColumn(ctx, tx, "standing_rulings", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := tx.ExecContext(ctx, "ALTER TABLE standing_rulings ADD COLUMN "+column.name+" "+column.definition); err != nil {
				return err
			}
		}
	}
	for _, query := range []string{
		`DROP INDEX IF EXISTS idx_standing_rulings_recent`,
		`DROP INDEX IF EXISTS idx_standing_rulings_live`,
		`CREATE INDEX IF NOT EXISTS idx_standing_rulings_recent ON standing_rulings(created_at DESC,id) WHERE source_ask_id<>''`,
		`CREATE INDEX IF NOT EXISTS idx_standing_rulings_live ON standing_rulings(created_at DESC,id) WHERE source_ask_id<>'' AND revoked_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_standing_rulings_date_id ON standing_rulings(ruling_id) WHERE ruling_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_standing_rulings_topic ON standing_rulings(scope,topic,ruling_id) WHERE status='active' AND ruling_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_standing_rulings_proposed ON standing_rulings(created_at DESC,ruling_id) WHERE status='proposed' AND ruling_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS standing_ruling_names(name TEXT PRIMARY KEY, ruling_id TEXT NOT NULL) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS standing_ruling_revision(singleton INTEGER PRIMARY KEY CHECK(singleton=1), version INTEGER NOT NULL, cursor_key BLOB NOT NULL)`,
		`INSERT OR IGNORE INTO standing_ruling_revision VALUES(1,0,randomblob(32))`,
		// Internal traversal keys only; hydrate every row through scoped
		// standing_rulings before serializing it.
		`CREATE VIEW IF NOT EXISTS operating_ruling_positions AS SELECT ruling_id,created_at,status FROM standing_rulings WHERE ruling_id IS NOT NULL`,
		`CREATE TRIGGER IF NOT EXISTS standing_ruling_insert_revision AFTER INSERT ON standing_rulings WHEN NEW.ruling_id IS NOT NULL BEGIN UPDATE standing_ruling_revision SET version=version+1 WHERE singleton=1; END`,
		`CREATE TRIGGER IF NOT EXISTS standing_ruling_update_revision AFTER UPDATE ON standing_rulings WHEN NEW.ruling_id IS NOT NULL BEGIN UPDATE standing_ruling_revision SET version=version+1 WHERE singleton=1; END`,
		// Check the full topic even when some content is hidden from the caller.
		// The trigger reports no identities. The scoped store gives a more
		// specific redirect when the missing targets are visible.
		`CREATE TRIGGER IF NOT EXISTS standing_ruling_insert_coverage BEFORE INSERT ON standing_rulings
		 WHEN NEW.status='proposed' AND EXISTS (
		  SELECT 1 FROM standing_rulings incumbent WHERE incumbent.status='active' AND incumbent.ruling_id IS NOT NULL
		  AND incumbent.scope=NEW.scope AND incumbent.topic=NEW.topic
		  AND NOT EXISTS (SELECT 1 FROM json_each(NEW.relations_json) rel WHERE json_extract(rel.value,'$.ruling_id')=incumbent.ruling_id)
		 ) BEGIN SELECT RAISE(ABORT, 'standing ruling relation required'); END`,
		`CREATE TRIGGER IF NOT EXISTS standing_ruling_confirm_coverage BEFORE UPDATE OF status ON standing_rulings
		 WHEN OLD.status='proposed' AND NEW.status='active' AND EXISTS (
		  SELECT 1 FROM standing_rulings incumbent WHERE incumbent.status='active' AND incumbent.ruling_id IS NOT NULL
		  AND incumbent.scope=NEW.scope AND incumbent.topic=NEW.topic
		  AND NOT EXISTS (SELECT 1 FROM json_each(NEW.relations_json) rel WHERE json_extract(rel.value,'$.ruling_id')=incumbent.ruling_id)
		 ) BEGIN SELECT RAISE(ABORT, 'standing ruling relation required'); END`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return installResourceAccessSourceEdges(ctx, tx, []resourceaccess.OwnershipSource{{
		Table: "standing_rulings", Kind: "filter/standing_rulings", ID: "id", Columns: resourceaccess.FilterSources["standing_rulings"],
	}})
}
