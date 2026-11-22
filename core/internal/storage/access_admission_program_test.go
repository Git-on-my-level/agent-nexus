package storage

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// Executed VM steps alone miss SQLite's cost of compiling inactive trigger
// bodies. Ordinary writes must not prepare ownership cascades for input columns
// they cannot change. Exercise both released-116 loaders as well as fresh setup.
func TestAdmissionUpdateProgramsFollowChangedColumns(t *testing.T) {
	for _, mode := range []string{"fresh", "upgrade", "fenced-upgrade"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			if mode != "fresh" {
				db := mainHistoryDatabase(t, root, 116)
				programs, _ := admissionUpdatePrograms(t, db, `UPDATE threads SET updated_by='other' WHERE id='program-thread'`)
				if !programs["admission_root_index_threads_update"] {
					t.Fatal("upgrade fixture already has narrowed programs before migration 117")
				}
				if mode == "fenced-upgrade" {
					fenceAdmissionHistory(t, root, db)
				} else {
					scopeMust(t, db.Close())
				}
			}
			w, err := InitializeWorkspace(ctx, root)
			scopeMust(t, err)
			defer w.Close()
			for _, q := range []string{
				`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('program-thread','now','owner','{}')`,
				`INSERT INTO cards(id,handle,thread_id,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES('program-card','program-card','program-thread','Public','','ready','now','owner','now','owner')`,
			} {
				scopeMust(t, execAdmissionTestSQL(w.DB(), q))
			}
			for _, probe := range []struct {
				query string
				idle  []string
			}{
				{`UPDATE threads SET updated_by='other' WHERE id='program-thread'`, []string{"admission_root_index_threads_update", "admission_roots_threads_update"}},
				{`UPDATE cards SET title='Changed' WHERE id='program-card'`, []string{"admission_struct_cards_update", "admission_empty_card_update"}},
				{`UPDATE resource_access_admission_roots SET contributions=contributions+1 WHERE kind='thread'`, []string{"admission_reach_root_update", "admission_vertex_resource_access_admission_roots_update"}},
			} {
				programs, operations := admissionUpdatePrograms(t, w.DB(), probe.query)
				t.Logf("compiled operations=%d query=%s", operations, probe.query)
				for _, name := range probe.idle {
					var exists bool
					scopeMust(t, w.DB().QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='trigger' AND name=?)`, name).Scan(&exists))
					if !exists {
						t.Fatalf("missing program under test: %s", name)
					}
					if programs[name] {
						t.Errorf("unchanged authorization input compiled %s: %s", name, probe.query)
					}
				}
			}
			q := `UPDATE threads SET body_json='{"pm_actor_id":"owner"}' WHERE id='program-thread'`
			programs, _ := admissionUpdatePrograms(t, w.DB(), q)
			if !programs["admission_root_index_threads_update"] {
				t.Fatal("ownership change lost its admission program")
			}
			scopeMust(t, execAdmissionTestSQL(w.DB(), q))
			scopeMust(t, execAdmissionTestSQL(w.DB(), `UPDATE threads SET body_json='{"pm_actor_id":"other"}' WHERE id='program-thread'`))
			var owners int
			scopeMust(t, w.DB().QueryRow(`SELECT count(*) FROM resource_access_admission_owners WHERE kind='card' AND id='program-card' AND owner='other'`).Scan(&owners))
			if owners != 3 {
				t.Fatalf("ownership transfer did not reach all three ledgers: %d", owners)
			}
		})
	}
}

func TestAdmissionProgramMigrationRejectsSkippedStep(t *testing.T) {
	original := migrations
	defer func() { migrations = original }()
	for _, fenced := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "fenced"}[fenced], func(t *testing.T) {
			migrations = original
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, 116)
			if fenced {
				fenceAdmissionHistory(t, root, db)
			} else {
				scopeMust(t, db.Close())
			}
			migrations = append([]migration(nil), original...)
			for i := range migrations {
				if migrations[i].Version == 117 {
					migrations[i].AfterApply = func(context.Context, *sql.Tx) error { return nil }
				}
			}
			if opened, err := InitializeWorkspace(context.Background(), root); err == nil {
				opened.Close()
				t.Fatal("skipped trigger-program migration accepted")
			}
			check, err := sql.Open("sqlite", sqliteDSN(NewLayout(root).DatabasePath))
			scopeMust(t, err)
			ledger := "schema_migrations"
			if fenced {
				ledger = "scope_schema_migrations"
			}
			var recorded int
			scopeMust(t, check.QueryRow(`SELECT count(*) FROM `+ledger+` WHERE version=117`).Scan(&recorded))
			scopeMust(t, check.Close())
			if recorded != 0 {
				t.Fatal("unexecuted migration 117 recorded")
			}
		})
	}
}

func TestAdmissionUpdateProgramsRetainRowIDAliases(t *testing.T) {
	w, err := InitializeWorkspace(context.Background(), t.TempDir())
	scopeMust(t, err)
	defer w.Close()
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('rowid-secret','now','owner','{"pm_actor_id":"owner"}'),('rowid-other','now','owner','{"pm_actor_id":"owner"}')`,
		`INSERT INTO cards(id,handle,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES('rowid-card','rowid-card','Public','','ready','now','owner','now','owner')`,
		`INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('card','rowid-card','thread:rowid-secret')`,
	} {
		scopeMust(t, execAdmissionTestSQL(w.DB(), q))
	}
	// The compact ledger is the only exact-edge ledger after migration 150. Its
	// update trigger must still move the admission contribution when the digest
	// changes, including through the declared INTEGER PRIMARY KEY alias.
	var edge int64
	scopeMust(t, w.DB().QueryRow(`SELECT c.edge_id FROM resource_access_compact_edges c JOIN resource_access_edges r ON r.rowid=c.edge_id WHERE r.source_kind='card' AND r.source_id='rowid-card' AND r.target_ref='thread:rowid-secret'`).Scan(&edge))
	countLinks := func(atom string) int {
		t.Helper()
		var n int
		scopeMust(t, w.DB().QueryRow(`SELECT count(*) FROM resource_access_admission_links l WHERE l.origin='exact' AND l.origin_id=CAST(? AS TEXT) AND l.via_id IN (SELECT key_id FROM resource_access_admission_keys WHERE atom=unhex(anx_resource_atom_key(CAST(? AS BLOB))))`, edge, atom).Scan(&n))
		return n
	}
	oldBefore, newBefore := countLinks("thread:rowid-secret"), countLinks("thread:rowid-other")
	if oldBefore == 0 {
		t.Fatal("missing compact-edge contribution before update")
	}
	if newBefore != 0 {
		t.Fatalf("unexpected contribution to the new atom before update: %d", newBefore)
	}
	scopeMust(t, execAdmissionProgramSQL(w.DB(), `UPDATE resource_access_compact_edges SET target_key=unhex(anx_resource_atom_key(CAST('thread:rowid-other' AS BLOB))) WHERE edge_id=?`, edge))
	oldAfter, newAfter := countLinks("thread:rowid-secret"), countLinks("thread:rowid-other")
	if oldAfter != 0 {
		t.Fatalf("stale exact links remain on the old atom: %d", oldAfter)
	}
	if newAfter == 0 {
		t.Fatal("exact links did not move to the new atom")
	}
	var total int
	scopeMust(t, w.DB().QueryRow(`SELECT count(*) FROM resource_access_admission_links WHERE origin='exact' AND origin_id=CAST(? AS TEXT)`, edge).Scan(&total))
	if total != newAfter {
		t.Fatalf("exact links did not move cleanly: total=%d new=%d", total, newAfter)
	}
	// Also cover a body spelled as NEW.rowid while the statement uses its
	// declared INTEGER PRIMARY KEY name. This needs schema alias discovery.
	tx, err := w.DB().BeginTx(context.Background(), nil)
	scopeMust(t, err)
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE program_alias_source(canonical_id INTEGER PRIMARY KEY,value TEXT)`,
		`CREATE TABLE program_alias_observed(id INTEGER)`,
		`INSERT INTO program_alias_source VALUES(1,'value')`,
		`CREATE TRIGGER admission_program_alias_update AFTER UPDATE ON program_alias_source WHEN OLD.rowid IS NOT NEW.rowid BEGIN INSERT INTO program_alias_observed VALUES(NEW.rowid); END`,
	} {
		_, err := tx.Exec(q)
		scopeMust(t, err)
	}
	scopeMust(t, restrictAdmissionUpdatePrograms(context.Background(), tx))
	_, err = tx.Exec(`UPDATE program_alias_source SET canonical_id=2 WHERE canonical_id=1`)
	scopeMust(t, err)
	var observed int
	scopeMust(t, tx.QueryRow(`SELECT id FROM program_alias_observed`).Scan(&observed))
	if observed != 2 {
		t.Fatalf("declared primary key lost its rowid input: %d", observed)
	}
}

func execAdmissionProgramSQL(db *sql.DB, query string, args ...any) error {
	_, err := db.Exec(query, args...)
	return err
}

func admissionUpdatePrograms(t *testing.T, db *sql.DB, query string) (map[string]bool, int) {
	t.Helper()
	rows, err := db.Query("EXPLAIN " + query)
	scopeMust(t, err)
	defer rows.Close()
	programs := map[string]bool{}
	operations := 0
	for rows.Next() {
		var address, p1, p2, p3, p5 int
		var opcode string
		var p4, comment any
		scopeMust(t, rows.Scan(&address, &opcode, &p1, &p2, &p3, &p4, &p5, &comment))
		operations++
		if value, ok := p4.(string); ok && strings.HasPrefix(value, "-- TRIGGER ") {
			programs[strings.TrimPrefix(value, "-- TRIGGER ")] = true
		}
	}
	scopeMust(t, rows.Err())
	return programs, operations
}
