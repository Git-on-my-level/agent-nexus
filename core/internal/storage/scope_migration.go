package storage

import (
	"context"
	"database/sql"
)

// InstallScopeMigration is an expand-only metadata migration for A to register
// AFTER the actual merge head (64/65 belong to #275). All indexes are on empty
// new tables. It neither backfills historical data nor enables a new reader.
func InstallScopeMigration(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{
		`CREATE TABLE scope_migration_jobs(
job TEXT PRIMARY KEY,owner TEXT NOT NULL DEFAULT '',token INTEGER NOT NULL DEFAULT 0,
lease_until INTEGER NOT NULL DEFAULT 0,generation INTEGER NOT NULL DEFAULT 0,
source_epoch INTEGER NOT NULL DEFAULT -1,cursor TEXT NOT NULL DEFAULT '',
processed INTEGER NOT NULL DEFAULT 0,exceptions INTEGER NOT NULL DEFAULT 0,
done INTEGER NOT NULL DEFAULT 0 CHECK(done IN (0,1)))`,
		`CREATE TABLE scope_migration_placements(
job TEXT NOT NULL,generation INTEGER NOT NULL,source_key TEXT NOT NULL,
kind TEXT NOT NULL,resource_id TEXT NOT NULL,source_version INTEGER NOT NULL,
scope_id TEXT NOT NULL,exception INTEGER NOT NULL CHECK(exception IN (0,1)),
content_unavailable INTEGER NOT NULL CHECK(content_unavailable IN (0,1)),
PRIMARY KEY(job,generation,source_key),UNIQUE(job,generation,kind,resource_id)) WITHOUT ROWID`,
		`CREATE TABLE scope_format_state(singleton INTEGER PRIMARY KEY CHECK(singleton=1),
format INTEGER NOT NULL CHECK(format=1),ledger_digest TEXT NOT NULL,reader TEXT NOT NULL CHECK(reader='legacy'))`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
