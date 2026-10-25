package primitives

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestAdmissionWritesTouchOnlyChangedIndexes(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	c, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Write control"})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := ws.DB().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','owner') WHERE id=?`, c["thread_id"])
	exec(`UPDATE work_metadata SET authority='remote',metadata_json='{"source":{"authority":"remote","url":"https://example.test/work"}}' WHERE card_id=?`, c["id"])
	other, err := s.CreateWork(ctx, "unrelated-owner", "", map[string]any{"title": "Untouched component"})
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','unrelated-owner') WHERE id=?`, other["thread_id"])
	for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
		p := "NEW."
		if op == "DELETE" {
			p = "OLD."
		}
		ids := `'` + strings.ReplaceAll(workString(other["id"]), `'`, `''`) + `','` + strings.ReplaceAll(workString(other["thread_id"]), `'`, `''`) + `'`
		exec(`CREATE TRIGGER test_unrelated_owner_` + op + ` BEFORE ` + op + ` ON resource_access_admission_owners WHEN ` + p + `id IN (` + ids + `) BEGIN SELECT RAISE(ABORT,'unrelated component owner index changed'); END`)
		exec(`CREATE TRIGGER test_unrelated_root_owner_` + op + ` BEFORE ` + op + ` ON resource_access_admission_root_owners WHEN ` + p + `owner='unrelated-owner' BEGIN SELECT RAISE(ABORT,'unrelated root owner changed'); END`)
	}
	exec(`CREATE TABLE test_admission_touches(name TEXT NOT NULL)`)
	rows, err := ws.DB().Query(`SELECT name FROM sqlite_schema WHERE type='table' AND (name GLOB 'resource_access_admission_*' OR name='work_lifecycle_positions')`)
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	for i, table := range tables {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			exec(fmt.Sprintf(`CREATE TRIGGER test_touch_%d_%s AFTER %s ON %s BEGIN INSERT INTO test_admission_touches VALUES('%s'); END`, i, op, op, table, table))
		}
	}
	check := func(name, q string, args []any, allowed ...string) {
		t.Helper()
		exec(`DELETE FROM test_admission_touches`)
		exec(q, args...)
		rs, err := ws.DB().Query(`SELECT name,count(*) FROM test_admission_touches GROUP BY name`)
		if err != nil {
			t.Fatal(err)
		}
		defer rs.Close()
		for rs.Next() {
			var table string
			var n int
			if err := rs.Scan(&table, &n); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, allow := range allowed {
				found = found || table == allow
			}
			if !found {
				t.Errorf("%s rewrote unchanged %s (%d rows)", name, table, n)
			}
		}
	}
	check("unrelated card field", `UPDATE cards SET title=title||' edited' WHERE id=?`, []any{c["id"]})
	check("same owner", `UPDATE threads SET body_json=body_json WHERE id=?`, []any{c["thread_id"]})
	check("owner transfer", `UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','second-owner') WHERE id=?`, []any{c["thread_id"]}, "resource_access_admission_roots", "resource_access_admission_root_owners", "resource_access_admission_owners")
	check("metadata unrelated to references", `UPDATE work_metadata SET refresh_json='{"state":"queued"}' WHERE card_id=?`, []any{c["id"]})
	check("metadata with unchanged source URL", `UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.non_ref','ordinary text') WHERE card_id=?`, []any{c["id"]})
	// Owner transfer updates root declarations, while descendants keep the same
	// root identities. Revocation must nevertheless be visible on the next read.
	if s.CanAccessResource(WithRequestAccessScope(ctx, AccessScope{ActorID: "owner"}), "card", workString(c["id"])) {
		t.Fatal("old owner retained authority")
	}
	if !s.CanAccessResource(WithRequestAccessScope(ctx, AccessScope{ActorID: "second-owner"}), "card", workString(c["id"])) {
		t.Fatal("new owner missing authority")
	}
	for _, table := range tables {
		if strings.HasSuffix(table, "rebuild") {
			var n int
			if err := ws.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
				t.Fatalf("maintenance state retained: %d %v", n, err)
			}
		}
	}
}
