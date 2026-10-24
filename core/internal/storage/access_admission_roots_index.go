package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Point root constraints avoid JSON decoding and three canonical probes for
// every ancestor. Contributions preserve independent retained tombstones.
func installAccessAdmissionRoots(ctx context.Context, tx *sql.Tx) error {
	exec := func(q string) error {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("admission root index: %w", err)
		}
		return nil
	}
	for _, table := range []string{"threads", "resource_access_tombstones", "artifacts"} {
		for _, op := range []string{"insert", "update", "delete"} {
			if err := exec(`DROP TRIGGER IF EXISTS admission_root_index_` + table + `_` + op); err != nil {
				return err
			}
		}
	}
	if err := exec(`DROP TABLE IF EXISTS resource_access_admission_roots`); err != nil {
		return err
	}
	if err := exec(`CREATE TABLE resource_access_admission_roots(kind TEXT NOT NULL,id TEXT NOT NULL,unconditional INTEGER NOT NULL,owner NOT NULL,contributions INTEGER NOT NULL CHECK(contributions>0),PRIMARY KEY(kind,id,unconditional,owner)) WITHOUT ROWID`); err != nil {
		return err
	}
	for _, source := range []struct {
		table, kind, id, owner, condition string
		unconditional                     int
	}{
		{"threads", `'thread'`, `@id`, `json_extract(@body_json,'$.pm_actor_id')`, `COALESCE(json_extract(@body_json,'$.pm_actor_id'),'')<>''`, 0},
		{"resource_access_tombstones", `@kind`, `@id`, `@owner`, `1`, 0},
		{"artifacts", `'artifact'`, `@id`, `''`, `@content_refs_json IS NULL`, 1},
	} {
		replace := func(s, p string) string { return strings.ReplaceAll(s, "@", p) }
		add := func(p, f string) string {
			return `INSERT INTO resource_access_admission_roots SELECT ` + replace(source.kind, p) + `,` + replace(source.id, p) + `,` + fmt.Sprint(source.unconditional) + `,` + replace(source.owner, p) + `,1 FROM ` + f + `(SELECT 1) WHERE (` + replace(source.condition, p) + `) AND ` + replace(source.id, p) + ` IS NOT NULL ON CONFLICT(kind,id,unconditional,owner) DO UPDATE SET contributions=contributions+1;`
		}
		match := `kind=` + replace(source.kind, "OLD.") + ` AND id=` + replace(source.id, "OLD.") + ` AND unconditional=` + fmt.Sprint(source.unconditional) + ` AND owner IS ` + replace(source.owner, "OLD.") + ` AND (` + replace(source.condition, "OLD.") + `)`
		remove := `DELETE FROM resource_access_admission_roots WHERE ` + match + ` AND contributions=1; UPDATE resource_access_admission_roots SET contributions=contributions-1 WHERE ` + match + ` AND contributions>1;`
		for _, q := range []string{
			`CREATE TRIGGER admission_root_index_` + source.table + `_insert AFTER INSERT ON ` + source.table + ` BEGIN ` + add("NEW.", "") + ` END`,
			`CREATE TRIGGER admission_root_index_` + source.table + `_update AFTER UPDATE ON ` + source.table + ` BEGIN ` + remove + add("NEW.", "") + ` END`,
			`CREATE TRIGGER admission_root_index_` + source.table + `_delete AFTER DELETE ON ` + source.table + ` BEGIN ` + remove + ` END`,
			add("r.", source.table+" r, "),
		} {
			if err := exec(q); err != nil {
				return err
			}
		}
	}
	return nil
}
