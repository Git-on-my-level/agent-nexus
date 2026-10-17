package storage

import (
	"context"
	"database/sql"
)

// Presence is operational state, never progress. Old host-wide wake coverage is
// intentionally not backfilled: only an explicit per-agent job can assert it.
func installHostBridge(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS host_bridge_presence (
 host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
 instance_id TEXT NOT NULL, sequence INTEGER NOT NULL, config_revision INTEGER NOT NULL,
 version TEXT NOT NULL, signed_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, expires_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS host_bridge_retired_instances (host_id TEXT NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,instance_id TEXT NOT NULL,PRIMARY KEY(host_id,instance_id));
 CREATE TABLE IF NOT EXISTS host_bridge_jobs (
 host_id TEXT NOT NULL REFERENCES host_bridge_presence(host_id) ON DELETE CASCADE,
 actor_id TEXT NOT NULL REFERENCES actors(id), kind TEXT NOT NULL,
 enabled INTEGER NOT NULL, ready INTEGER NOT NULL,
 PRIMARY KEY(host_id,actor_id,kind));
 CREATE INDEX IF NOT EXISTS idx_hosts_created_id ON hosts(created_at DESC,id);`)
	if err != nil {
		return err
	}
	// Historical repair fixtures can replay later migrations over an upgraded
	// database. Keep existing operational snapshots on such a replay.
	for _, col := range []string{"bridge_instance_id", "bridge_checked_in_at", "bridge_expires_at"} {
		has, e := sqliteTableHasColumn(ctx, tx, "hosts", col)
		if e != nil {
			return e
		}
		if has {
			if _, e = tx.ExecContext(ctx, `ALTER TABLE hosts DROP COLUMN `+col); e != nil {
				return e
			}
		}
	}
	return nil
}
