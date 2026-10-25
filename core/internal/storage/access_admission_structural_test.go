package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
)

func fenceAdmissionHistory(t *testing.T, root string, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	scopeMust(t, err)
	scopeMust(t, InstallScopeMigration(ctx, tx))
	scopeMust(t, tx.Commit())
	w := &Workspace{layout: NewLayout(root), db: db, processLock: &scopeProcessLease{refs: 1}}
	scopeMust(t, w.InstallScopeFormatFence(ctx))
	scopeMust(t, w.Close())
}

func TestStructuralOnlyCanonicalAdmission(t *testing.T) {
	for _, upgrade := range []bool{true, false} {
		for _, late := range []bool{false, true} {
			t.Run(fmt.Sprintf("upgrade=%v/late=%v", upgrade, late), func(t *testing.T) {
				root := t.TempDir()
				db := mainHistoryDatabase(t, root, 115)
				seed := func(db *sql.DB) {
					t.Helper()
					queries := []string{
						`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('secret','now','owner','{"pm_actor_id":"owner"}')`,
						`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('missing-parent','{"ref":"thread:secret"}','now','owner')`,
						`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) VALUES('typed-dependent','typed-dependent','Public','','ready','["card:missing-parent"]','now','owner','now','owner'),('bare-dependent','bare-dependent','Public','','ready','["missing-parent"]','now','owner','now','owner'),('nav-dependent','nav-dependent','Public','','ready','[]','now','owner','now','owner')`,
						`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('missing-navigation','card','nav-dependent','card','missing-parent','ref','now','{}')`,
					}
					if late {
						metadata := queries[1]
						queries = append(append(append([]string{}, queries[:1]...), queries[2:]...), metadata)
					}
					for _, q := range queries {
						scopeMust(t, execAdmissionTestSQL(db, q))
					}
				}
				if upgrade {
					seed(db)
				}
				scopeMust(t, db.Close())
				w, err := InitializeWorkspace(context.Background(), root)
				scopeMust(t, err)
				defer w.Close()
				if !upgrade {
					seed(w.DB())
				}
				// Check actual pinned candidate rows first: these are the reported leaks.
				reader := primitives.WithRequestAccessScope(context.Background(), primitives.AccessScope{ActorID: "human"})
				read, closeRead, err := primitives.NewTestStore(w.DB(), w.Layout().ArtifactContentDir).BeginResourceRead(reader)
				scopeMust(t, err)
				rows, err := resourceaccess.NewDB(w.DB()).QueryContext(read, `SELECT id FROM cards ORDER BY id`)
				scopeMust(t, err)
				var leaked []string
				for rows.Next() {
					var id string
					scopeMust(t, rows.Scan(&id))
					leaked = append(leaked, id)
				}
				scopeMust(t, rows.Err())
				rows.Close()
				closeRead()
				if len(leaked) > 0 {
					t.Errorf("structural-only identity disclosed dependents: %v", leaked)
				}
				checkAdmissionSecurityParity(t, w.DB(), "structural-only canonical IDs")
			})
		}
	}
}
func execAdmissionTestSQL(db *sql.DB, q string) error { _, err := db.Exec(q); return err }

func TestAdmissionUpgradeEveryLoader(t *testing.T) {
	for _, fenced := range []bool{false, true} {
		t.Run(fmt.Sprint("fenced=", fenced), func(t *testing.T) {
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, 115)
			if fenced {
				fenceAdmissionHistory(t, root, db)
			} else {
				scopeMust(t, db.Close())
			}
			for reopen := 0; reopen < 2; reopen++ {
				w, err := InitializeWorkspace(context.Background(), root)
				scopeMust(t, err)
				ledger := "schema_migrations"
				if fenced {
					ledger = "scope_schema_migrations"
				}
				var recorded int
				scopeMust(t, w.DB().QueryRow(`SELECT count(*) FROM `+ledger+` WHERE version=116`).Scan(&recorded))
				if recorded != 1 {
					t.Fatalf("migration116 ledger=%d", recorded)
				}
				var admission int
				scopeMust(t, w.DB().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='resource_access_admission_root_owners'`).Scan(&admission))
				if admission != 1 {
					w.Close()
					t.Fatal("migration116 recorded without admission tables")
				}
				checkAdmissionSecurityParity(t, w.DB(), fmt.Sprintf("fenced=%v reopen=%d", fenced, reopen))
				scopeMust(t, w.Close())
			}
		})
	}
}

// Every ownership kind may exist only as an indexed source vertex. Typed IDs,
// raw IDs and navigation must preserve the forward policy's exclusions too.
func TestCanonicalIdentitySourceSweep(t *testing.T) {
	for _, upgrade := range []bool{true, false} {
		t.Run(fmt.Sprint("upgrade=", upgrade), func(t *testing.T) {
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, 115)
			seed := func(db *sql.DB) {
				t.Helper()
				scopeMust(t, execAdmissionTestSQL(db, `INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('sweep-secret','now','owner','{"pm_actor_id":"owner"}')`))
				kinds := map[string]bool{"external_key": true, "document_history": true, "series": true, "series_stream": true, "brief_loop": true, "pm": true, "filter/example": true}
				for _, source := range resourceaccess.OwnershipSources {
					kinds[source.Kind] = true
				}
				for _, edge := range resourceaccess.StructuralOwnershipEdges {
					kinds[edge[0]] = true
					kinds[edge[1]] = true
				}
				for kind := range kinds {
					id := "source-only-" + kind
					_, err := db.Exec(`INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,'thread:sweep-secret')`, kind, id)
					scopeMust(t, err)
					for i, ref := range []string{kind + ":" + id, id} {
						candidate := fmt.Sprintf("source-%s-%d", kind, i)
						_, err := db.Exec(`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Public','','ready',json_array(?),'now','owner','now','owner')`, candidate, candidate, ref)
						scopeMust(t, err)
					}
					candidate := "nav-" + kind
					_, err = db.Exec(`INSERT INTO cards(id,handle,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Public','','ready','now','owner','now','owner')`, candidate, candidate)
					scopeMust(t, err)
					_, err = db.Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES(?,'card',?,?,?,'ref','now','{}')`, candidate, candidate, kind, id)
					scopeMust(t, err)
				}
				// Empty plan steps and participants have no atom/identity rows. Their
				// backing absent card inherits privacy only through its metadata row.
				for _, q := range []string{
					`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('absent-backing-card','{"ref":"thread:sweep-secret"}','now','owner')`,
					`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES('absent-backing-card','{"steps":[]}','now'),('absent-plan-only','{"steps":[{"ref":"card:absent-backing-card"}]}','now')`,
					`INSERT INTO work_participants(id,card_id,session_id,activity,sequence,request_json,created_at,last_seen_at,expires_at) VALUES('orphan-participant','absent-backing-card','absent-session','active',1,'{}','now','now','later')`,
					`INSERT INTO work_observations(id,card_id,idempotency_key,digest,observed_at,received_at,status,body_json) VALUES('orphan-observation','absent-observed-card','k','d','now','now','reported','{"ref":"thread:sweep-secret"}')`,
					`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('orphan-edge','card','absent-edge-card','thread','sweep-secret','ref','now','{}')`,
					`INSERT INTO cards(id,handle,title,summary,column_key,refs_json,created_at,created_by,updated_at,updated_by) VALUES('plan-dependent','plan-dependent','Public','','ready','["plan:absent-backing-card","plan:absent-plan-only"]','now','owner','now','owner'),('participant-dependent','participant-dependent','Public','','ready','["participant:orphan-participant"]','now','owner','now','owner'),('observation-dependent','observation-dependent','Public','','ready','["card:absent-observed-card"]','now','owner','now','owner'),('edge-dependent','edge-dependent','Public','','ready','["card:absent-edge-card"]','now','owner','now','owner')`,
				} {
					scopeMust(t, execAdmissionTestSQL(db, q))
				}
			}
			if upgrade {
				seed(db)
			}
			scopeMust(t, db.Close())
			w, err := InitializeWorkspace(context.Background(), root)
			scopeMust(t, err)
			defer w.Close()
			if !upgrade {
				seed(w.DB())
			}
			checkAdmissionSecurityParity(t, w.DB(), "all identity-source kinds")
			// A live row arriving and departing cannot delete structural ID keys.
			for _, q := range []string{
				`INSERT INTO cards(id,handle,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES('absent-backing-card','now-live','Public','','ready','now','owner','now','owner')`,
				`UPDATE cards SET handle='renamed-live' WHERE id='absent-backing-card'`,
				`DELETE FROM cards WHERE id='absent-backing-card'`,
				`UPDATE work_metadata SET card_id='moved-absent-card' WHERE card_id='absent-backing-card'`,
				`UPDATE threads SET body_json='{"pm_actor_id":"second-owner"}' WHERE id='sweep-secret'`,
				`DELETE FROM work_metadata WHERE card_id='moved-absent-card'`,
				`DELETE FROM ref_edges WHERE id='orphan-edge'`,
				`DELETE FROM resource_access_edges WHERE source_id LIKE 'source-only-%'`,
				`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('card','source-only-card','new-retained-alias','second-owner')`,
				`DELETE FROM resource_access_tombstones WHERE kind='card' AND id='source-only-card'`,
			} {
				scopeMust(t, execAdmissionTestSQL(w.DB(), q))
				checkAdmissionSecurityParity(t, w.DB(), q)
			}
		})
	}
}

func TestAdmissionRecordedSchemaMustExist(t *testing.T) {
	for _, fenced := range []bool{false, true} {
		t.Run(fmt.Sprint("fenced=", fenced), func(t *testing.T) {
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, 115)
			if fenced {
				fenceAdmissionHistory(t, root, db)
			} else {
				scopeMust(t, db.Close())
			}
			w, err := InitializeWorkspace(context.Background(), root)
			scopeMust(t, err)
			scopeMust(t, execAdmissionTestSQL(w.DB(), `DROP TABLE resource_access_admission_root_owners`))
			scopeMust(t, w.Close())
			if opened, err := InitializeWorkspace(context.Background(), root); err == nil {
				opened.Close()
				t.Fatal("recorded116 without admission schema accepted")
			}
		})
	}
}

func TestMigrationRunnersRejectSkippedSteps(t *testing.T) {
	original := migrations
	defer func() { migrations = original }()
	var admission migration
	for _, m := range original {
		if m.Version == 116 {
			admission = m
		}
	}
	for _, fenced := range []bool{false, true} {
		for _, mode := range []string{"empty", "noop-chunk", "noop-hook", "mixed"} {
			t.Run(fmt.Sprintf("fenced=%v/%s", fenced, mode), func(t *testing.T) {
				migrations = original
				root := t.TempDir()
				db := mainHistoryDatabase(t, root, 115)
				if fenced {
					fenceAdmissionHistory(t, root, db)
				} else {
					scopeMust(t, db.Close())
				}
				m := admission
				m.ApplyChunk = nil
				switch mode {
				case "noop-chunk":
					m.ApplyChunk = func(context.Context, *sql.Tx) (bool, error) { return true, nil }
				case "noop-hook":
					m.AfterApply = func(context.Context, *sql.Tx) error { return nil }
				case "mixed":
					m.ApplyChunk = admission.ApplyChunk
					m.Statements = []string{`CREATE TABLE skipped_step(id)`}
				}
				migrations = append(append([]migration(nil), original[:len(original)-1]...), m)
				if opened, err := InitializeWorkspace(context.Background(), root); err == nil {
					opened.Close()
					t.Fatal("unexecuted116 recorded")
				}
				check, err := sql.Open("sqlite", sqliteDSN(NewLayout(root).DatabasePath))
				scopeMust(t, err)
				defer check.Close()
				ledger := "schema_migrations"
				if fenced {
					ledger = "scope_schema_migrations"
				}
				var count int
				scopeMust(t, check.QueryRow(`SELECT count(*) FROM `+ledger+` WHERE version=116`).Scan(&count))
				if count != 0 {
					t.Fatal("failed116 advanced migration ledger")
				}
			})
		}
	}
}

func TestHistoricalWakeupStructuralCycleAdmission(t *testing.T) {
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 115)
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('cycle-parent','now','owner','{}')`,
		`INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at) VALUES('cycle-wakeup-0','requested','unread','agent','agent','cycle-parent','[]','now','now'),('cycle-wakeup-1','requested','unread','agent','agent','cycle-parent','[]','now','now')`,
		`INSERT INTO cards(id,handle,title,summary,column_key,thread_id,refs_json,created_at,created_by,updated_at,updated_by) VALUES('cycle-bridge','cycle-bridge','Public','','ready','cycle-parent','["wakeup:cycle-wakeup-0"]','now','owner','now','owner'),('cycle-leaf','cycle-leaf','Public','','ready',NULL,'["wakeup:cycle-wakeup-1"]','now','owner','now','owner')`,
	} {
		scopeMust(t, execAdmissionTestSQL(db, q))
	}
	scopeMust(t, db.Close())
	w, err := InitializeWorkspace(context.Background(), root)
	scopeMust(t, err)
	defer w.Close()
	scopeMust(t, execAdmissionTestSQL(w.DB(), `UPDATE threads SET body_json='{"pm_actor_id":"owner"}' WHERE id='cycle-parent'`))
	checkAdmissionSecurityParity(t, w.DB(), "historical wakeup structural cycle")
}
