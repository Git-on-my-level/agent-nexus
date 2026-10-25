package storage

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
)

// Compare the canonical closure and actual scoped pages, counts and continuation
// probes. PM designation and actor type must never grant additional authority.
func checkAdmissionSecurityParity(t *testing.T, db *sql.DB, stage string) {
	t.Helper()
	ctx := context.Background()
	for _, scope := range []primitives.AccessScope{{ActorID: "owner"}, {ActorID: "second-owner"}, {ActorID: "human"}, {ActorID: "agent"}, {ActorID: "human", PMActorID: "owner"}, {ActorID: "owner", PMActorID: "owner"}, {ActorID: "human", AuthAdmin: true}, {ActorID: "17"}, {}} {
		read := func(c context.Context, q string) []string {
			t.Helper()
			rows, err := resourceaccess.NewDB(db).QueryContext(c, q)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			out := []string{}
			for rows.Next() {
				var value string
				if err := rows.Scan(&value); err != nil {
					t.Fatal(err)
				}
				out = append(out, value)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		}
		old := primitives.WithAccessScope(ctx, scope)
		fresh := primitives.WithRequestAccessScope(ctx, scope)
		for _, query := range []string{
			`SELECT json_array(kind,CAST(id AS TEXT)) FROM _anx_denied ORDER BY kind,CAST(id AS TEXT)`,
			`SELECT DISTINCT json_array(ref,CAST(typed AS TEXT)) FROM _anx_denied_atoms ORDER BY 1`,
			`SELECT id FROM cards ORDER BY id`,
			`SELECT id FROM cards ORDER BY id LIMIT 2 OFFSET 1`,
			`SELECT json_array(count(*),EXISTS(SELECT 1 FROM cards ORDER BY id LIMIT 1 OFFSET 2)) FROM cards`,
		} {
			want := read(old, query)
			var got []string
			if query == `SELECT json_array(kind,CAST(id AS TEXT)) FROM _anx_denied ORDER BY kind,CAST(id AS TEXT)` || query == `SELECT DISTINCT json_array(ref,CAST(typed AS TEXT)) FROM _anx_denied_atoms ORDER BY 1` {
				constraint := "o.unconditional=1"
				var args []any
				if scope.ActorID == "" || scope.ActorID != scope.PMActorID {
					constraint += " OR o.owner<>?"
					args = append(args, scope.ActorID)
				}
				from := ` FROM resource_access_admission_owners o WHERE o.ledger=CASE WHEN ` + resourceaccess.CompactReaderSQL + ` THEN 2 WHEN NOT (` + resourceaccess.CompactReaderSQL + `) THEN 1 ELSE -1 END AND (` + constraint + `)`
				indexed := `SELECT DISTINCT json_array(kind,id)` + from + ` ORDER BY kind,id`
				if query == `SELECT DISTINCT json_array(ref,CAST(typed AS TEXT)) FROM _anx_denied_atoms ORDER BY 1` {
					indexed = `WITH denied AS (SELECT o.kind,o.id` + from + `) SELECT DISTINCT json_array(s.ref,CAST(s.is_ref AS TEXT)) FROM denied d JOIN resource_access_admission_spellings s ON s.kind=d.kind AND s.id=d.id ORDER BY 1`
				}
				rows, err := db.Query(indexed, args...)
				if err != nil {
					t.Fatal(err)
				}
				got = []string{}
				for rows.Next() {
					var value string
					if err := rows.Scan(&value); err != nil {
						t.Fatal(err)
					}
					got = append(got, value)
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				rows.Close()
			} else {
				got = read(fresh, query)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s scope=%+v query=%s canonical=%v candidate=%v", stage, scope, query, want, got)
			}
		}
	}
}

func TestHistoricalWakeupPrivateTransitionAdmission(t *testing.T) {
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 115)
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('parent','now','owner','{}')`,
		`INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,trigger_event_id,refs_json,created_at,updated_at) VALUES('historical','requested','unread','agent','agent','parent','','[]','now','now')`,
		`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) VALUES('dependent','dependent','Public','','ready','["wakeup:historical"]','now','owner','now','owner')`,
		`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('navigation','card','dependent','wakeup','historical','ref','now','{}')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	checkAdmissionSecurityParity(t, ws.DB(), "public upgrade")
	if _, err := ws.DB().Exec(`UPDATE threads SET body_json='{"pm_actor_id":"owner"}' WHERE id='parent'`); err != nil {
		t.Fatal(err)
	}
	checkAdmissionSecurityParity(t, ws.DB(), "private after upgrade")
	for _, q := range []string{
		`INSERT INTO resource_access_compact_edges SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges WHERE true ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key`,
		`UPDATE resource_access_compact_control SET reader=1,force_legacy=0`,
		`UPDATE threads SET body_json='{"pm_actor_id":"second-owner"}' WHERE id='parent'`,
		`UPDATE resource_access_compact_control SET force_legacy=1`,
		`DELETE FROM resource_access_compact_control`,
	} {
		if _, err := ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
		checkAdmissionSecurityParity(t, ws.DB(), q)
	}
}

func TestAliasOnlyTombstoneAdmission(t *testing.T) {
	for _, upgrade := range []bool{true, false} {
		t.Run(fmt.Sprint("upgrade=", upgrade), func(t *testing.T) {
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, 115)
			seed := func(db *sql.DB) {
				t.Helper()
				for _, q := range []string{
					`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document','retired-id','retired-alias','owner')`,
					`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) VALUES('typed','typed','Public','','ready','["document:retired-id"]','now','owner','now','owner'),('bare','bare','Public','','ready','["retired-id"]','now','owner','now','owner'),('alias','alias','Public','','ready','["document:retired-alias"]','now','owner','now','owner')`,
				} {
					if _, err := db.Exec(q); err != nil {
						t.Fatal(err)
					}
				}
			}
			if upgrade {
				seed(db)
			}
			db.Close()
			ws, err := InitializeWorkspace(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if !upgrade {
				seed(ws.DB())
			}
			checkAdmissionSecurityParity(t, ws.DB(), "alias-only tombstone")
		})
	}
}

func TestRetainedCanonicalSpellingAdmission(t *testing.T) {
	for _, upgrade := range []bool{true, false} {
		t.Run(fmt.Sprint("upgrade=", upgrade), func(t *testing.T) {
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, 115)
			seed := func(db *sql.DB) {
				t.Helper()
				kinds := map[string]bool{"external_key": true, "document_history": true, "series": true, "series_stream": true, "brief_loop": true, "pm": true}
				for _, source := range resourceaccess.OwnershipSources {
					kinds[source.Kind] = true
				}
				var ordered []string
				for kind := range kinds {
					ordered = append(ordered, kind)
				}
				sort.Strings(ordered)
				for _, kind := range ordered {
					id, alias := "retained-"+kind, "alias-"+kind
					if _, err := db.Exec(`INSERT INTO resource_access_tombstones VALUES(?,?,?,'owner')`, kind, id, alias); err != nil {
						t.Fatal(err)
					}
					for i, ref := range []string{kind + ":" + id, id, kind + ":" + alias} {
						card := fmt.Sprintf("candidate-%s-%d", kind, i)
						if _, err := db.Exec(`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Public','','ready',json_array(?),'now','owner','now','owner')`, card, card, ref); err != nil {
							t.Fatal(err)
						}
					}
				}
				// Empty ref has no identity row; ID inheritance still applies.
				if _, err := db.Exec(`INSERT INTO resource_access_tombstones VALUES('document','empty-retained','','owner'); INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) VALUES('empty-dependent','empty-dependent','Public','','ready','["document:empty-retained","empty-retained"]','now','owner','now','owner')`); err != nil {
					t.Fatal(err)
				}
			}
			if upgrade {
				seed(db)
			}
			db.Close()
			ws, err := InitializeWorkspace(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if !upgrade {
				seed(ws.DB())
			}
			checkAdmissionSecurityParity(t, ws.DB(), "all retained kinds")
			// Removing one spelling cannot erase a surviving contribution.
			for _, q := range []string{
				`INSERT INTO resource_access_tombstones VALUES('document','retained-document','second-alias','owner')`,
				`DELETE FROM resource_access_tombstones WHERE kind='document' AND ref='alias-document'`,
				`UPDATE resource_access_tombstones SET ref='',owner='second-owner' WHERE kind='document' AND id='retained-document'`,
				`DELETE FROM resource_access_tombstones WHERE kind='document'`,
			} {
				if _, err := ws.DB().Exec(q); err != nil {
					t.Fatal(err)
				}
				checkAdmissionSecurityParity(t, ws.DB(), q)
			}
		})
	}
}

// Seed the released schema, rather than constructing history through the new
// triggers. Every operation is followed by both policies for every principal,
// including actual scoped counts/pages. This catches late resolution during a
// recursive ledger insert and identities surviving only as retained aliases.
func TestAdmissionUpgradeRandomizedScaleParity(t *testing.T) {
	if testing.Short() {
		t.Skip("released-schema 4096-row differential fixture")
	}
	for _, seed := range []int64{46920261025, 46920261026} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) { admissionUpgradeRandomizedScaleParity(t, seed) })
	}
}

func admissionUpgradeRandomizedScaleParity(t *testing.T, seed int64) {
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 115)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('parent','now','owner','{}'),('other-parent','now','owner','{"pm_actor_id":"second-owner"}')`)
	exec(`INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<7) SELECT 'old-'||i,'requested','unread','agent','agent','parent','[]','now','now' FROM n`)
	exec(`INSERT INTO resource_access_tombstones VALUES('document','retired','alias','owner')`)
	exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at,reason) VALUES('document','second-alias','retired','former','now','retained')`)
	exec(`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095) SELECT printf('scale-%04d',i),printf('scale-%04d',i),'Public','','ready',CASE WHEN i<8 THEN json_array('wakeup:old-'||i) WHEN i=8 THEN '["document:retired"]' WHEN i=9 THEN '["retired"]' WHEN i=10 THEN '["document:second-alias"]' ELSE '[]' END,'now','owner','now','owner' FROM n`)
	db.Close()
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	if seed == 46920261026 {
		// SQLite does not promise the order of independent triggers. Recreate
		// admission triggers in reverse installation order for the second run.
		rows, err := db.Query(`SELECT name,sql FROM sqlite_schema WHERE type='trigger' AND name GLOB 'admission_*' ORDER BY rowid DESC`)
		if err != nil {
			t.Fatal(err)
		}
		var triggers [][2]string
		for rows.Next() {
			var trigger [2]string
			if err := rows.Scan(&trigger[0], &trigger[1]); err != nil {
				t.Fatal(err)
			}
			triggers = append(triggers, trigger)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for _, trigger := range triggers {
			if _, err := tx.Exec(`DROP TRIGGER "` + trigger[0] + `"`); err != nil {
				t.Fatal(err)
			}
		}
		for _, trigger := range triggers {
			if _, err := tx.Exec(trigger[1]); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	// In particular, check BOTH reported leaks before later random operations
	// happen to create another canonical identity or resolve a missing path.
	checkAdmissionSecurityParity(t, db, "upgrade")
	exec(`UPDATE threads SET body_json='{"pm_actor_id":"owner"}' WHERE id='parent'`)
	checkAdmissionSecurityParity(t, db, "first private transition")
	rng := rand.New(rand.NewSource(seed))
	for step := 0; step < 32; step++ {
		id := fmt.Sprintf("old-%d", rng.Intn(8))
		switch step % 8 {
		case 0:
			exec(`UPDATE threads SET body_json=json_object('pm_actor_id',?) WHERE id='parent'`, []any{"", "owner", "second-owner", 17}[rng.Intn(4)])
		case 1:
			exec(`UPDATE agent_wakeups SET thread_id=? WHERE wakeup_id=?`, []string{"parent", "other-parent"}[rng.Intn(2)], id)
		case 2:
			exec(`UPDATE resource_access_tombstones SET owner=?,ref=? WHERE kind='document' AND id='retired'`, []string{"owner", "second-owner"}[rng.Intn(2)], fmt.Sprintf("renamed-%d", step))
		case 3:
			exec(`UPDATE cards SET refs_json=json_array(?) WHERE id=?`, []string{"document:retired", "retired", "document:second-alias", "wakeup:" + id}[rng.Intn(4)], fmt.Sprintf("scale-%04d", rng.Intn(12)))
		case 4:
			exec(`UPDATE agent_wakeups SET refs_json=json_array(?) WHERE wakeup_id=?`, "wakeup:"+fmt.Sprintf("old-%d", rng.Intn(8)), id)
		case 5:
			exec(`UPDATE resource_handle_aliases SET alias_handle=? WHERE resource_type='document' AND resource_id='retired'`, fmt.Sprintf("kept-%d", step))
		case 6:
			exec(`INSERT INTO resource_access_compact_edges SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges WHERE true ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key`)
			exec(`UPDATE resource_access_compact_control SET reader=1,force_legacy=?`, rng.Intn(2))
		case 7:
			exec(`UPDATE threads SET body_json=json_object('pm_actor_id',?) WHERE id='other-parent'`, []string{"", "owner", "second-owner"}[rng.Intn(3)])
		}
		checkAdmissionSecurityParity(t, db, fmt.Sprintf("step-%d", step))
	}
}
