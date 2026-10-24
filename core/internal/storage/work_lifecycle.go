package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Only inactive card identities are routed here. Payloads and authorization
// remain canonical and are hydrated after indexed page selection.
func installWorkLifecycle(ctx context.Context, tx *sql.Tx) error {
	hidden := `(COALESCE(c.archived_at,'')<>'' OR COALESCE(c.trashed_at,'')<>'' OR EXISTS(SELECT 1 FROM boards b WHERE b.id=c.board_id AND (COALESCE(b.archived_at,'')<>'' OR COALESCE(b.trashed_at,'')<>'')) OR EXISTS(SELECT 1 FROM work_metadata m JOIN topics t ON t.id=substr(json_extract(m.metadata_json,'$.project_ref'),7) OR (t.handle=substr(json_extract(m.metadata_json,'$.project_ref'),7) AND t.handle IS NOT NULL AND trim(t.handle)<>'') WHERE m.card_id=c.id AND substr(json_extract(m.metadata_json,'$.project_ref'),1,6)='topic:' AND (COALESCE(t.archived_at,'')<>'' OR COALESCE(t.trashed_at,'')<>'')))`
	class, owner := admissionOwnerClass("l.value", "'card'", "c.id")
	refresh := func(ids string) string {
		return `DELETE FROM work_lifecycle_positions WHERE id IN (` + ids + `) AND NOT EXISTS(SELECT 1 FROM cards c WHERE c.id=work_lifecycle_positions.id AND ` + hidden + `); INSERT INTO work_lifecycle_positions SELECT l.value,c.id,` + class + `,` + owner + ` FROM json_each('[-1,1,2]') l CROSS JOIN cards c WHERE c.id IN (` + ids + `) AND ` + hidden + ` ON CONFLICT(ledger,id) DO UPDATE SET availability=excluded.availability,owner=excluded.owner WHERE work_lifecycle_positions.availability IS NOT excluded.availability OR work_lifecycle_positions.owner IS NOT excluded.owner;`
	}
	queries := []string{
		`DROP TABLE IF EXISTS work_lifecycle_positions`,
		`CREATE TABLE work_lifecycle_positions(ledger INTEGER NOT NULL,id TEXT NOT NULL,availability INTEGER NOT NULL,owner NOT NULL,PRIMARY KEY(ledger,id)) WITHOUT ROWID`,
		`CREATE INDEX idx_work_lifecycle_public ON work_lifecycle_positions(ledger,id) WHERE availability=0`,
		`CREATE INDEX idx_work_lifecycle_owner ON work_lifecycle_positions(ledger,owner,id) WHERE availability=1`,
		`CREATE INDEX idx_work_lifecycle_pm ON work_lifecycle_positions(ledger,id) WHERE availability<>3`,
		refresh(`SELECT id FROM cards`),
	}
	for _, op := range []string{"insert", "delete"} {
		p := "NEW."
		if op == "delete" {
			p = "OLD."
		}
		class, owner := admissionOwnerClass(p+"ledger", p+"kind", p+"id")
		queries = append(queries, `CREATE TRIGGER admission_work_lifecycle_owner_`+op+` AFTER `+strings.ToUpper(op)+` ON resource_access_admission_owners WHEN `+p+`kind='card' BEGIN UPDATE work_lifecycle_positions SET availability=`+class+`,owner=`+owner+` WHERE ledger=`+p+`ledger AND id=`+p+`id AND (availability IS NOT (`+class+`) OR owner IS NOT (`+owner+`)); END`)
	}
	for _, source := range []struct{ table, cols, oldIDs, newIDs string }{
		{"cards", "id,board_id,archived_at,trashed_at", `SELECT OLD.id`, `SELECT NEW.id`},
		{"work_metadata", "card_id,metadata_json", `SELECT OLD.card_id`, `SELECT NEW.card_id`},
		{"boards", "id,archived_at,trashed_at", `SELECT id FROM cards WHERE board_id=OLD.id`, `SELECT id FROM cards WHERE board_id=NEW.id`},
		{"topics", "id,handle,archived_at,trashed_at", `SELECT card_id FROM work_metadata WHERE json_extract(metadata_json,'$.project_ref') IN ('topic:'||OLD.id,'topic:'||OLD.handle)`, `SELECT card_id FROM work_metadata WHERE json_extract(metadata_json,'$.project_ref') IN ('topic:'||NEW.id,'topic:'||NEW.handle)`},
	} {
		for _, op := range []string{"insert", "update", "delete"} {
			queries = append(queries, `DROP TRIGGER IF EXISTS admission_work_lifecycle_`+source.table+`_`+op)
		}
		queries = append(queries,
			`CREATE TRIGGER admission_work_lifecycle_`+source.table+`_insert AFTER INSERT ON `+source.table+` BEGIN `+refresh(source.newIDs)+` END`,
			`CREATE TRIGGER admission_work_lifecycle_`+source.table+`_update AFTER UPDATE OF `+source.cols+` ON `+source.table+` BEGIN `+refresh(source.oldIDs+` UNION `+source.newIDs)+` END`,
			`CREATE TRIGGER admission_work_lifecycle_`+source.table+`_delete AFTER DELETE ON `+source.table+` BEGIN `+refresh(source.oldIDs)+` END`)
	}
	for _, q := range queries {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("work lifecycle routing: %w", err)
		}
	}
	return nil
}
