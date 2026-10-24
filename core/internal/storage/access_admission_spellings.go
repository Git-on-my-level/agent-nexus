package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

// Projection references differ from graph-resolution keys: endpoints compare
// binary typed spellings, while JSON also compares bare IDs using NOCASE.
// Index the original spellings so neither policy needs a denied-workspace list.
func installAccessAdmissionSpellings(ctx context.Context, tx *sql.Tx) error {
	exec := func(q string) error {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("admission spellings: %w", err)
		}
		return nil
	}
	if err := exec(`DROP TABLE IF EXISTS resource_access_admission_spellings`); err != nil {
		return err
	}
	for _, q := range []string{
		`CREATE TABLE resource_access_admission_spellings(origin TEXT NOT NULL,origin_id TEXT NOT NULL,kind TEXT NOT NULL,id TEXT NOT NULL,ref TEXT NOT NULL,is_ref INTEGER NOT NULL,atom BLOB NOT NULL,bucket TEXT NOT NULL,PRIMARY KEY(origin,origin_id,kind,id,ref,is_ref)) WITHOUT ROWID`,
		`CREATE INDEX idx_access_admission_spelling_atom ON resource_access_admission_spellings(atom,kind,id)`,
		`CREATE INDEX idx_access_admission_spelling_ref ON resource_access_admission_spellings(ref,is_ref,kind,id)`,
		`CREATE INDEX idx_access_admission_spelling_bucket ON resource_access_admission_spellings(bucket,is_ref,ref,kind,id)`,
	} {
		if err := exec(q); err != nil {
			return err
		}
	}
	// inputs are schema-owned SQL expressions. Keep the old ownershipRefs and
	// _anx_denied_atoms formatting exactly, including document aliases and URLs.
	insert := func(origin, originID, kind, id, value, from string, raw bool) string {
		refs := `json_array(CASE WHEN k='external_key' OR k='card' AND (v LIKE 'http://%' OR v LIKE 'https://%') THEN v ELSE k||':'||v END,CASE WHEN k='document' THEN 'doc:'||v END)`
		valid := `k NOT LIKE 'filter/%' AND k NOT IN ('work_evidence_record','work_evidence_alias','document_history')`
		if raw {
			refs = `json_array(v)`
			valid = `k NOT LIKE 'filter/%' AND k NOT IN ('brief_loop','plan','work_evidence_record','work_evidence_alias','external_key')`
		}
		isRef := "1"
		if raw {
			isRef = "0"
		}
		return `INSERT INTO resource_access_admission_spellings SELECT '` + origin + `',o,k,i,j.value,` + isRef + `,unhex(` + resourceaccess.AtomKeySQL("j.value") + `),` + resourceaccess.MentionRefBucketSQL("j.value") + ` FROM (SELECT ` + originID + ` o,` + kind + ` k,` + id + ` i,` + value + ` v FROM ` + from + `) CROSS JOIN json_each(` + refs + `) j WHERE ` + valid + ` AND j.value IS NOT NULL AND j.value<>'' ON CONFLICT DO NOTHING;`
	}
	vertex := func(kind, id, from string) string {
		// document_history -> document is a virtual canonical edge even when
		// no live document row remains (retained purge provenance).
		originID := `json_array(` + kind + `,` + id + `)`
		// Carry the originating vertex separately from a virtual document.
		vertices := `(SELECT ` + originID + ` o,` + kind + ` k,` + id + ` i FROM ` + from + ` UNION SELECT ` + originID + `,'document',` + id + ` FROM ` + from + ` WHERE ` + kind + `='document_history') v`
		return insert("vertex", "v.o", "v.k", "v.i", "v.i", vertices, false) + insert("vertex", "v.o", "v.k", "v.i", "v.i", vertices, true)
	}
	removeVertex := func(kind, id string) string {
		return `DELETE FROM resource_access_admission_spellings WHERE origin='vertex' AND origin_id=json_array(` + kind + `,` + id + `) AND NOT EXISTS(SELECT 1 FROM resource_access_admission_graph WHERE parent_kind=` + kind + ` AND parent_id=` + id + `) AND NOT EXISTS(SELECT 1 FROM resource_access_admission_graph WHERE child_kind=` + kind + ` AND child_id=` + id + `) AND NOT EXISTS(SELECT 1 FROM resource_access_admission_roots WHERE kind=` + kind + ` AND id=` + id + `);`
	}
	for _, source := range []struct {
		table  string
		fields [][2]string
	}{
		{"resource_access_admission_graph", [][2]string{{"parent_kind", "parent_id"}, {"child_kind", "child_id"}}},
		{"resource_access_admission_roots", [][2]string{{"kind", "id"}}},
	} {
		var add, remove, backfill string
		for _, f := range source.fields {
			add += vertex("NEW."+f[0], "NEW."+f[1], "(SELECT 1)")
			remove += removeVertex("OLD."+f[0], "OLD."+f[1])
			backfill += vertex("r."+f[0], "r."+f[1], source.table+" r")
		}
		for _, q := range []string{
			`CREATE TRIGGER admission_spelling_` + source.table + `_insert AFTER INSERT ON ` + source.table + ` BEGIN ` + add + ` END`,
			`CREATE TRIGGER admission_spelling_` + source.table + `_update AFTER UPDATE ON ` + source.table + ` BEGIN ` + remove + add + ` END`,
			`CREATE TRIGGER admission_spelling_` + source.table + `_delete AFTER DELETE ON ` + source.table + ` BEGIN ` + remove + ` END`, backfill,
		} {
			if err := exec(q); err != nil {
				return err
			}
		}
	}
	for _, source := range []struct{ table, columnID, kind, id, ref, condition string }{
		{"resource_access_identities", "identity_id", "@kind", "@resource_id", "@ref", "1"},
		{"work_metadata", "card_id", "'card'", "@card_id", "json_extract(@metadata_json,'$.source.url')", "@authority<>'nexus'"},
	} {
		expr := func(s, p string) string { return strings.ReplaceAll(s, "@", p) }
		add := func(p, from string) string {
			return insert(source.table, p+source.columnID, expr(source.kind, p), expr(source.id, p), expr(source.ref, p), from+` WHERE `+expr(source.condition, p), false)
		}
		clear := `DELETE FROM resource_access_admission_spellings WHERE origin='` + source.table + `' AND origin_id=OLD.` + source.columnID + `;`
		for _, q := range []string{
			`CREATE TRIGGER admission_spelling_` + source.table + `_insert AFTER INSERT ON ` + source.table + ` BEGIN ` + add("NEW.", "(SELECT 1)") + ` END`,
			`CREATE TRIGGER admission_spelling_` + source.table + `_update AFTER UPDATE ON ` + source.table + ` BEGIN ` + clear + add("NEW.", "(SELECT 1)") + ` END`,
			`CREATE TRIGGER admission_spelling_` + source.table + `_delete AFTER DELETE ON ` + source.table + ` BEGIN ` + clear + ` END`, add("r.", source.table+" r"),
		} {
			if err := exec(q); err != nil {
				return err
			}
		}
	}
	return nil
}
