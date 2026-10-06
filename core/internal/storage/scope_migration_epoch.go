package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// InstallScopeMigrationEpoch is a proposal for A's schema initialization, after
// all optional canonical schemas and the scope registry have been installed.
// It visits schema metadata only. It is not called by workspace startup.
// Broad invalidation deliberately includes ancillary authority, roles, PM,
// aliases, lifecycle, purge, imports and scope grants, not just graph edges.
// Any subsequent DDL invalidates coverage until this installer runs again.
func InstallScopeMigrationEpoch(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS scope_migration_epoch(singleton INTEGER PRIMARY KEY CHECK(singleton=1),version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>=0),schema_cookie INTEGER NOT NULL,authority_token TEXT NOT NULL)`,
		`INSERT INTO scope_migration_epoch VALUES(1,0,-1,'') ON CONFLICT DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS scope_migration_captures(rid INTEGER PRIMARY KEY CHECK(rid>0),scope_id TEXT NOT NULL,kind TEXT NOT NULL,resource_id TEXT NOT NULL,canonical_id TEXT NOT NULL,version INTEGER NOT NULL CHECK(version>0))`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	// SQLite virtual and shadow tables cannot be trigger targets. Their canonical
	// inputs are ordinary tables and are covered. Bound schema work independently
	// of the number of canonical records; refuse an unexpected schema explosion.
	rows, err := tx.QueryContext(ctx, `SELECT name FROM pragma_table_list WHERE schema='main' AND type='table' AND name NOT GLOB 'sqlite_*' ORDER BY name LIMIT 513`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(tables) > 512 {
		return fmt.Errorf("scope migration schema coverage exceeds budget")
	}
	for _, table := range tables {
		excluded := false
		switch table {
		case "scope_migration_epoch", "scope_migration_jobs", "scope_migration_placements",
			"home_read_cursors", "overview_visits", "series_request_budgets", "series_ingestion_days",
			"scope_feed", "scope_counters":
			// Reviewed worker staging, reader telemetry and admission budgets do
			// not change canonical source or authority. Captures and all scope
			// grants/aliases/resource registries remain covered. B's feed/counter
			// outputs are rebuild targets, not canonical projection inputs.
			excluded = true
		}
		quote := func(v string) string { return `"` + strings.ReplaceAll(v, `"`, `""`) + `"` }
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			name := quote("scope_migration_epoch_" + table + "_" + op)
			// Replace stale experimental definitions; IF NOT EXISTS is not proof
			// that the installed trigger implements this epoch contract.
			if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS `+name); err != nil {
				return err
			}
			if excluded {
				continue
			}
			q := `CREATE TRIGGER ` + name + ` AFTER ` + op + ` ON ` + quote(table) + ` BEGIN UPDATE scope_migration_epoch SET version=version+1 WHERE singleton=1; END`
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	// Reinstallation itself invalidates snapshots, including after optional PM
	// or role schemas appear. Reading a cookie never silently repairs coverage.
	_, err = tx.ExecContext(ctx, `UPDATE scope_migration_epoch SET version=version+1,schema_cookie=(SELECT schema_version FROM pragma_schema_version) WHERE singleton=1`)
	return err
}

// BindScopeMigrationAuthority durably captures authority selected outside SQL,
// such as the configured PM principal. A must bind the complete runtime
// authority token before starting a worker and supply it to MetadataSource.
// Another process with a different binding fails closed until reconfigured.
// The token is an internal identity/version, never an audience subset proof.
func BindScopeMigrationAuthority(ctx context.Context, tx *sql.Tx, token string) error {
	if token == "" || len(token) > 512 {
		return fmt.Errorf("invalid scope runtime authority binding")
	}
	_, err := tx.ExecContext(ctx, `UPDATE scope_migration_epoch SET authority_token=?,version=version+1 WHERE singleton=1 AND authority_token!=?`, token, token)
	return err
}
