package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Root membership and current ownership have independent indexes. A read needs
// at most three owner-range seeks even when a candidate reaches many roots.
func installAccessAdmissionOwners(ctx context.Context, tx *sql.Tx) error {
	queries := []string{
		`DROP TABLE IF EXISTS resource_access_admission_owners`,
		`CREATE TABLE resource_access_admission_owners(ledger INTEGER NOT NULL,kind TEXT NOT NULL,id TEXT NOT NULL,unconditional INTEGER NOT NULL,owner NOT NULL,roots INTEGER NOT NULL CHECK(roots>0),PRIMARY KEY(ledger,kind,id,unconditional,owner)) WITHOUT ROWID`,
		`INSERT INTO resource_access_admission_owners SELECT a.ledger,a.kind,a.id,r.unconditional,r.owner,count(*) FROM resource_access_admission_reach a JOIN resource_access_admission_roots r ON r.kind=a.root_kind AND r.id=a.root_id GROUP BY a.ledger,a.kind,a.id,r.unconditional,r.owner`,
	}
	add := `INSERT INTO resource_access_admission_owners SELECT NEW.ledger,NEW.kind,NEW.id,r.unconditional,r.owner,1 FROM resource_access_admission_roots r WHERE r.kind=NEW.root_kind AND r.id=NEW.root_id ON CONFLICT(ledger,kind,id,unconditional,owner) DO UPDATE SET roots=roots+1;`
	match := `ledger=OLD.ledger AND kind=OLD.kind AND id=OLD.id AND (unconditional,owner) IN (SELECT unconditional,owner FROM resource_access_admission_roots WHERE kind=OLD.root_kind AND id=OLD.root_id)`
	remove := `DELETE FROM resource_access_admission_owners WHERE ` + match + ` AND roots=1; UPDATE resource_access_admission_owners SET roots=roots-1 WHERE ` + match + ` AND roots>1;`
	// Integrate declaration maintenance into the root trigger body. Do not
	// depend on SQLite's ordering of independent triggers for one event.
	rootAdd := `INSERT INTO resource_access_admission_owners SELECT ledger,kind,id,NEW.unconditional,NEW.owner,1 FROM resource_access_admission_reach WHERE root_kind=NEW.kind AND root_id=NEW.id ON CONFLICT(ledger,kind,id,unconditional,owner) DO UPDATE SET roots=roots+1;`
	rootMatch := `(ledger,kind,id) IN (SELECT ledger,kind,id FROM resource_access_admission_reach WHERE root_kind=OLD.kind AND root_id=OLD.id) AND unconditional=OLD.unconditional AND owner IS OLD.owner`
	rootRemove := `DELETE FROM resource_access_admission_owners WHERE ` + rootMatch + ` AND roots=1; UPDATE resource_access_admission_owners SET roots=roots-1 WHERE ` + rootMatch + ` AND roots>1;`
	queries = append(queries,
		`CREATE TRIGGER admission_owner_reach_insert AFTER INSERT ON resource_access_admission_reach BEGIN `+add+` END`,
		`CREATE TRIGGER admission_owner_reach_delete AFTER DELETE ON resource_access_admission_reach BEGIN `+remove+` END`)
	for _, q := range queries {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("admission owner index: %w", err)
		}
	}
	for _, op := range []string{"insert", "delete", "update"} {
		name := "admission_reach_root_" + op
		var original string
		if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE name=?`, name).Scan(&original); err != nil {
			return err
		}
		prefix := rootAdd
		if op == "delete" {
			prefix = rootRemove
		}
		if op == "update" {
			prefix = rootRemove + rootAdd
		}
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER `+name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, strings.Replace(original, " BEGIN ", " BEGIN "+prefix, 1)); err != nil {
			return err
		}
	}
	return nil
}

// Classification is routing only. The consuming read still enforces the
// canonical candidate predicate. 0=public, 1=one owner, 2=conflicting owners,
// 3=unconditional denial. Numeric legacy owners keep their SQLite value type.
func admissionOwnerClass(ledger, kind, id string) (string, string) {
	from := ` FROM resource_access_admission_owners o WHERE o.ledger=` + ledger + ` AND o.kind=` + kind + ` AND o.id=` + id
	count := `(SELECT count(*) FROM (SELECT 1` + from + ` AND unconditional=0 LIMIT 2))`
	class := `CASE WHEN EXISTS(SELECT 1` + from + ` AND unconditional=1) THEN 3 WHEN ` + count + `>1 THEN 2 WHEN ` + count + `=1 THEN 1 ELSE 0 END`
	owner := `CASE WHEN (` + class + `)=1 THEN (SELECT owner` + from + ` AND unconditional=0 LIMIT 1) ELSE '' END`
	return class, owner
}
