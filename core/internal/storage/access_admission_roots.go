package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// A statement can skip all ancestry probes when it has no denied root. Count
// distinct owners, not roots, so this check never enumerates private threads.
// owner deliberately has no affinity: legacy numeric JSON owners must retain
// json_extract's SQLite comparison semantics rather than becoming TEXT grants.
func installAccessAdmissionRootCounts(ctx context.Context, tx *sql.Tx) error {
	exec := func(q string) error {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("admission root counts: %w", err)
		}
		return nil
	}
	for _, table := range []string{"threads", "resource_access_tombstones", "artifacts"} {
		for _, op := range []string{"insert", "update", "delete"} {
			if err := exec(`DROP TRIGGER IF EXISTS admission_roots_` + table + `_` + op); err != nil {
				return err
			}
		}
	}
	if err := exec(`DROP TABLE IF EXISTS resource_access_admission_root_owners`); err != nil {
		return err
	}
	if err := exec(`CREATE TABLE resource_access_admission_root_owners(unconditional INTEGER NOT NULL,owner NOT NULL,roots INTEGER NOT NULL CHECK(roots>0),PRIMARY KEY(unconditional,owner)) WITHOUT ROWID`); err != nil {
		return err
	}
	for _, source := range []struct {
		table, owner, condition string
		unconditional           int
	}{
		{"threads", `json_extract(@body_json,'$.pm_actor_id')`, `COALESCE(json_extract(@body_json,'$.pm_actor_id'),'')<>''`, 0},
		{"resource_access_tombstones", `@owner`, `1`, 0},
		{"artifacts", `''`, `@content_refs_json IS NULL`, 1},
	} {
		replace := func(s, p string) string { return strings.ReplaceAll(s, "@", p) }
		add := func(p, from string) string {
			return `INSERT INTO resource_access_admission_root_owners SELECT ` + fmt.Sprint(source.unconditional) + `,` + replace(source.owner, p) + `,1 FROM ` + from + `(SELECT 1) WHERE ` + replace(source.condition, p) + ` ON CONFLICT(unconditional,owner) DO UPDATE SET roots=roots+1;`
		}
		match := `unconditional=` + fmt.Sprint(source.unconditional) + ` AND owner IS ` + replace(source.owner, "OLD.") + ` AND (` + replace(source.condition, "OLD.") + `)`
		remove := `DELETE FROM resource_access_admission_root_owners WHERE ` + match + ` AND roots=1; UPDATE resource_access_admission_root_owners SET roots=roots-1 WHERE ` + match + ` AND roots>1;`
		for _, q := range []string{
			`CREATE TRIGGER admission_roots_` + source.table + `_insert AFTER INSERT ON ` + source.table + ` BEGIN ` + add("NEW.", "") + ` END`,
			`CREATE TRIGGER admission_roots_` + source.table + `_update AFTER UPDATE ON ` + source.table + ` BEGIN ` + remove + add("NEW.", "") + ` END`,
			`CREATE TRIGGER admission_roots_` + source.table + `_delete AFTER DELETE ON ` + source.table + ` BEGIN ` + remove + ` END`,
			add("r.", source.table+" r, "),
		} {
			if err := exec(q); err != nil {
				return err
			}
		}
	}
	return nil
}
