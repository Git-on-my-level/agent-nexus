package storage

import (
	"context"
	"database/sql"
)

// Retain historical discovery rows while indexing only the capacity-bearing
// population. Backfills run once in migration 105, never on serving paths.
func upgradeSeriesCapacityIndexes(ctx context.Context, tx *sql.Tx) error {
	for _, field := range []struct{ table, column, definition string }{
		{"series_adapters", "definition_count", "INTEGER NOT NULL DEFAULT 0"},
		{"series_labels", "last_point_ts", "INTEGER"},
	} {
		exists, err := sqliteTableExists(ctx, tx, field.table)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		has, err := sqliteTableHasColumn(ctx, tx, field.table, field.column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err = tx.ExecContext(ctx, `ALTER TABLE `+field.table+` ADD COLUMN `+field.column+` `+field.definition); err != nil {
			return err
		}
		if field.table == "series_adapters" {
			_, err = tx.ExecContext(ctx, `UPDATE series_adapters SET definition_count=(SELECT COUNT(*) FROM series_definitions WHERE adapter=series_adapters.name)`)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE series_labels SET last_point_ts=(SELECT MAX(ts) FROM (SELECT MAX(ts) AS ts FROM series_points p WHERE p.series=series_labels.series AND p.labels=series_labels.labels UNION ALL SELECT MAX(last_ts) FROM series_daily d WHERE d.series=series_labels.series AND d.labels=series_labels.labels))`)
		}
		if err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `
 CREATE INDEX IF NOT EXISTS idx_series_adapters_capacity ON series_adapters(definition_count) WHERE revoked_at IS NULL AND deleted_at IS NULL AND definition_count>0;
 CREATE INDEX IF NOT EXISTS idx_series_labels_capacity ON series_labels(series,last_point_ts);
 CREATE TRIGGER IF NOT EXISTS series_capacity_definition_insert AFTER INSERT ON series_definitions BEGIN
 UPDATE series_adapters SET definition_count=definition_count+1 WHERE name=NEW.adapter; END;
 CREATE TRIGGER IF NOT EXISTS series_capacity_definition_delete AFTER DELETE ON series_definitions BEGIN
 UPDATE series_adapters SET definition_count=definition_count-1 WHERE name=OLD.adapter; END;
 CREATE TRIGGER IF NOT EXISTS series_capacity_definition_update AFTER UPDATE OF adapter ON series_definitions WHEN NEW.adapter<>OLD.adapter BEGIN
 UPDATE series_adapters SET definition_count=definition_count-1 WHERE name=OLD.adapter;
 UPDATE series_adapters SET definition_count=definition_count+1 WHERE name=NEW.adapter; END;
 CREATE TRIGGER IF NOT EXISTS series_capacity_point_insert AFTER INSERT ON series_points BEGIN
 UPDATE series_labels SET last_point_ts=MAX(COALESCE(last_point_ts,NEW.ts),NEW.ts) WHERE series=NEW.series AND labels=NEW.labels; END;
 CREATE TRIGGER IF NOT EXISTS series_capacity_point_update AFTER UPDATE OF ts,series,labels ON series_points BEGIN
 UPDATE series_labels SET last_point_ts=MAX(COALESCE(last_point_ts,NEW.ts),NEW.ts) WHERE series=NEW.series AND labels=NEW.labels; END;
 `)
	return err
}
