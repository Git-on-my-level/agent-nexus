package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Persist only root reachability, not principal decisions or every vertex pair.
// Owners remain in roots, so a transfer/revoke never rebuilds this index. The
// two ledgers retain independent exact/compact semantics, including imports.
func installAccessAdmissionReach(ctx context.Context, tx *sql.Tx) error {
	exec := func(q string) error {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("admission reachability: %w", err)
		}
		return nil
	}
	for _, q := range []string{
		`DROP TABLE IF EXISTS resource_access_admission_reach`,
		`DROP TABLE IF EXISTS resource_access_admission_rebuild`,
		`CREATE TABLE resource_access_admission_reach(ledger INTEGER NOT NULL,kind TEXT NOT NULL,id TEXT NOT NULL,root_kind TEXT NOT NULL,root_id TEXT NOT NULL,PRIMARY KEY(ledger,kind,id,root_kind,root_id)) WITHOUT ROWID`,
		`CREATE INDEX idx_admission_reach_root ON resource_access_admission_reach(root_kind,root_id,ledger,kind,id)`,
		`CREATE TABLE resource_access_admission_rebuild(ledger INTEGER NOT NULL,kind TEXT NOT NULL,id TEXT NOT NULL,PRIMARY KEY(ledger,kind,id)) WITHOUT ROWID`,
	} {
		if err := exec(q); err != nil {
			return err
		}
	}
	// WITH is embedded in INSERT/DELETE SELECTs because SQLite trigger grammar
	// does not accept a top-level WITH statement. UNION terminates arbitrary cycles.
	walk := func(seeds string) string {
		return `WITH RECURSIVE reach(ledger,kind,id,root_kind,root_id) AS (` + seeds + `
 UNION SELECT r.ledger,e.child_kind,e.child_id,r.root_kind,r.root_id FROM reach r JOIN resource_access_admission_graph e ON e.parent_kind=r.kind AND e.parent_id=r.id AND e.ledger IN (0,r.ledger)
 UNION SELECT ledger,'document',id,root_kind,root_id FROM reach WHERE kind='document_history') `
	}
	initial := `SELECT l.value,r.kind,r.id,r.kind,r.id FROM resource_access_admission_roots r CROSS JOIN json_each('[-1,1,2]') l`
	if err := exec(`INSERT INTO resource_access_admission_reach ` + walk(initial) + `SELECT * FROM reach WHERE true ON CONFLICT DO NOTHING`); err != nil {
		return err
	}
	newRoot := `SELECT l.value,NEW.kind,NEW.id,NEW.kind,NEW.id FROM json_each('[-1,1,2]') l WHERE NOT EXISTS(SELECT 1 FROM resource_access_admission_reach WHERE ledger=1 AND kind=NEW.kind AND id=NEW.id AND root_kind=NEW.kind AND root_id=NEW.id)`
	addRoot := `INSERT INTO resource_access_admission_reach ` + walk(newRoot) + `SELECT * FROM reach WHERE true ON CONFLICT DO NOTHING;`
	dropRoot := `DELETE FROM resource_access_admission_reach WHERE root_kind=OLD.kind AND root_id=OLD.id AND NOT EXISTS(SELECT 1 FROM resource_access_admission_roots WHERE kind=OLD.kind AND id=OLD.id);`
	// A new edge only changes roots already reaching its parent, and only if
	// those roots do not already reach the child. Unrelated components are idle.
	newEdge := `SELECT r.ledger,NEW.child_kind,NEW.child_id,r.root_kind,r.root_id FROM json_each('[-1,1,2]') l CROSS JOIN resource_access_admission_reach r WHERE r.ledger=l.value AND r.kind=NEW.parent_kind AND r.id=NEW.parent_id AND NEW.ledger IN (0,r.ledger) AND NOT EXISTS(SELECT 1 FROM resource_access_admission_reach old WHERE old.ledger=r.ledger AND old.kind=NEW.child_kind AND old.id=NEW.child_id AND old.root_kind=r.root_kind AND old.root_id=r.root_id)`
	addEdge := `INSERT INTO resource_access_admission_reach ` + walk(newEdge) + `SELECT * FROM reach WHERE true ON CONFLICT DO NOTHING;`
	// Deletion can remove one of several paths or split a cycle. Recompute only
	// roots that reached this edge; change only membership rows that differ.
	affected := `INSERT INTO resource_access_admission_rebuild SELECT r.ledger,r.root_kind,r.root_id FROM json_each('[-1,1,2]') l CROSS JOIN resource_access_admission_reach r WHERE r.ledger=l.value AND r.kind=OLD.parent_kind AND r.id=OLD.parent_id AND OLD.ledger IN (0,r.ledger) ON CONFLICT DO NOTHING;`
	fresh := walk(`SELECT ledger,kind,id,kind,id FROM resource_access_admission_rebuild`)
	removeEdge := affected + `DELETE FROM resource_access_admission_reach WHERE (root_kind,root_id,ledger) IN (SELECT kind,id,ledger FROM resource_access_admission_rebuild) AND (ledger,kind,id,root_kind,root_id) NOT IN (` + fresh + `SELECT * FROM reach); DELETE FROM resource_access_admission_rebuild;`
	for _, q := range []string{
		`CREATE TRIGGER admission_reach_root_insert AFTER INSERT ON resource_access_admission_roots BEGIN ` + addRoot + ` END`,
		`CREATE TRIGGER admission_reach_root_delete AFTER DELETE ON resource_access_admission_roots BEGIN ` + dropRoot + ` END`,
		`CREATE TRIGGER admission_reach_root_update AFTER UPDATE ON resource_access_admission_roots WHEN OLD.kind IS NOT NEW.kind OR OLD.id IS NOT NEW.id OR OLD.owner IS NOT NEW.owner OR OLD.unconditional IS NOT NEW.unconditional BEGIN ` + dropRoot + addRoot + ` END`,
		`CREATE TRIGGER admission_reach_graph_insert AFTER INSERT ON resource_access_admission_graph BEGIN ` + addEdge + ` END`,
		`CREATE TRIGGER admission_reach_graph_delete AFTER DELETE ON resource_access_admission_graph BEGIN ` + removeEdge + ` END`,
	} {
		if err := exec(q); err != nil {
			return err
		}
	}
	return nil
}
