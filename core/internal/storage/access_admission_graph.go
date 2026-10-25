package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Independent origins can contribute the same edge (ID, handle, raw atom,
// revision provenance). Traverse each edge once; retain its contribution count
// so replacing one origin cannot revoke another origin's dependency.
func installAccessAdmissionGraph(ctx context.Context, tx *sql.Tx) error {
	exec := func(q string) error {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("admission graph: %w", err)
		}
		return nil
	}
	for _, q := range []string{
		`DROP TRIGGER IF EXISTS admission_graph_insert`, `DROP TRIGGER IF EXISTS admission_graph_update`, `DROP TRIGGER IF EXISTS admission_graph_delete`,
		`DROP TABLE IF EXISTS resource_access_admission_graph`,
		`CREATE TABLE resource_access_admission_graph(child_kind TEXT NOT NULL,child_id TEXT NOT NULL,ledger INTEGER NOT NULL,parent_kind TEXT NOT NULL,parent_id TEXT NOT NULL,contributions INTEGER NOT NULL CHECK(contributions>0),PRIMARY KEY(child_kind,child_id,ledger,parent_kind,parent_id)) WITHOUT ROWID`,
		`CREATE INDEX idx_access_admission_graph_parent ON resource_access_admission_graph(parent_kind,parent_id,ledger,child_kind,child_id)`,
		`INSERT INTO resource_access_admission_graph SELECT child_kind,child_id,ledger,parent_kind,parent_id,count(*) FROM resource_access_admission_links GROUP BY child_kind,child_id,ledger,parent_kind,parent_id`,
	} {
		if err := exec(q); err != nil {
			return err
		}
	}
	add := `INSERT INTO resource_access_admission_graph VALUES(NEW.child_kind,NEW.child_id,NEW.ledger,NEW.parent_kind,NEW.parent_id,1) ON CONFLICT(child_kind,child_id,ledger,parent_kind,parent_id) DO UPDATE SET contributions=contributions+1;`
	match := `child_kind=OLD.child_kind AND child_id=OLD.child_id AND ledger=OLD.ledger AND parent_kind=OLD.parent_kind AND parent_id=OLD.parent_id`
	remove := `DELETE FROM resource_access_admission_graph WHERE ` + match + ` AND contributions=1; UPDATE resource_access_admission_graph SET contributions=contributions-1 WHERE ` + match + ` AND contributions>1;`
	for _, q := range []string{
		`CREATE TRIGGER admission_graph_insert AFTER INSERT ON resource_access_admission_links BEGIN ` + add + ` END`,
		`CREATE TRIGGER admission_graph_update AFTER UPDATE ON resource_access_admission_links BEGIN ` + remove + add + ` END`,
		`CREATE TRIGGER admission_graph_delete AFTER DELETE ON resource_access_admission_links BEGIN ` + remove + ` END`,
	} {
		if err := exec(q); err != nil {
			return err
		}
	}
	return nil
}
