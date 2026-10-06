package resourceaccess

import (
	"context"
	"database/sql"
	"fmt"
)

// InstallPMAccess runs on the schema/write connection. PM can also be used with
// a standalone database; only canonical workspaces have the authorization ledger.
// A record's complete body is parsed once at a write, never during point reads.
func InstallPMAccess(ctx context.Context, tx *sql.Tx, rebuild bool) error {
	var ready bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='resource_access_edges') AND EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='pm_records')`).Scan(&ready); err != nil || !ready {
		return err
	}
	var installed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='trigger' AND name='access_pm_records_insert')`).Scan(&installed); err != nil {
		return err
	}
	if installed && !rebuild {
		return nil
	}
	// PM uses (kind,id) keys, while the historical privacy policy follows IDs
	// across kinds. Keep each contributor separately so replacing one row never
	// removes another row's private evidence for the shared ID.
	statements := []string{
		`CREATE TABLE IF NOT EXISTS resource_access_pm_refs(kind TEXT NOT NULL,id TEXT NOT NULL,target_ref TEXT NOT NULL COLLATE NOCASE,PRIMARY KEY(kind,id,target_ref))`,
		`CREATE INDEX IF NOT EXISTS idx_access_pm_refs_source ON resource_access_pm_refs(id,target_ref)`,
		`CREATE TRIGGER IF NOT EXISTS access_pm_ref_insert AFTER INSERT ON resource_access_pm_refs BEGIN INSERT INTO resource_access_edges VALUES('pm',NEW.id,NEW.target_ref) ON CONFLICT DO NOTHING; END`,
		`CREATE TRIGGER IF NOT EXISTS access_pm_ref_delete AFTER DELETE ON resource_access_pm_refs BEGIN DELETE FROM resource_access_edges WHERE source_kind='pm' AND source_id=OLD.id AND target_ref=OLD.target_ref AND NOT EXISTS(SELECT 1 FROM resource_access_pm_refs WHERE id=OLD.id AND target_ref=OLD.target_ref); END`,
		`CREATE TRIGGER IF NOT EXISTS access_pm_ref_update AFTER UPDATE ON resource_access_pm_refs BEGIN DELETE FROM resource_access_edges WHERE source_kind='pm' AND source_id=OLD.id AND target_ref=OLD.target_ref AND NOT EXISTS(SELECT 1 FROM resource_access_pm_refs WHERE id=OLD.id AND target_ref=OLD.target_ref); INSERT INTO resource_access_edges VALUES('pm',NEW.id,NEW.target_ref) ON CONFLICT DO NOTHING; END`,
		`DROP TRIGGER IF EXISTS access_pm_records_insert`,
		`DROP TRIGGER IF EXISTS access_pm_records_update`,
		`DROP TRIGGER IF EXISTS access_pm_records_delete`,
		`DELETE FROM resource_access_pm_refs`,
		`DELETE FROM resource_access_edges WHERE source_kind='pm'`,
	}
	insert := func(prefix, from string) string {
		body := prefix + "body"
		return `INSERT INTO resource_access_pm_refs SELECT ` + prefix + `kind,` + prefix + `id,j.value FROM ` + from + `json_each(` + ReferenceSQLAtoms(body, true) + `) j WHERE j.value<>''
 UNION SELECT ` + prefix + `kind,` + prefix + `id,CAST(j.atom AS TEXT) FROM ` + from + `json_tree(CASE WHEN json_valid(CAST(` + body + ` AS TEXT)) THEN CAST(` + body + ` AS TEXT) ELSE 'null' END) j WHERE j.atom IS NOT NULL ON CONFLICT DO NOTHING;`
	}
	clear := `DELETE FROM resource_access_pm_refs WHERE kind=OLD.kind AND id=OLD.id;`
	statements = append(statements,
		insert("r.", "pm_records r, "),
		`CREATE TRIGGER access_pm_records_insert AFTER INSERT ON pm_records BEGIN `+insert("NEW.", "")+` END`,
		`CREATE TRIGGER access_pm_records_update AFTER UPDATE OF kind,id,body ON pm_records BEGIN `+clear+insert("NEW.", "")+` END`,
		`CREATE TRIGGER access_pm_records_delete AFTER DELETE ON pm_records BEGIN `+clear+` END`,
	)
	// Even a write without reference atoms invalidates the request boundary.
	for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
		statements = append(statements, `CREATE TRIGGER IF NOT EXISTS access_epoch_pm_records_`+op+` AFTER `+op+` ON pm_records BEGIN UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1; END`)
	}
	for _, q := range statements {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("index PM authorization: %w", err)
		}
	}
	return nil
}
