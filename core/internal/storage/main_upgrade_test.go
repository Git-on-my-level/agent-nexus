package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// Build real main history from the unchanged migration definitions, rather than
// deleting ledger entries from an already upgraded database. No PR-preview
// schemas or numbers participate in the supported upgrade matrix.
func mainHistoryDatabase(t *testing.T, root string, version int) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", sqliteDSN(NewLayout(root).DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, createMigrationsTableSQL); err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.Version > version {
			break
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range m.Statements {
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				t.Fatalf("history %d: %v", m.Version, err)
			}
		}
		if m.AfterApply != nil {
			if err = m.AfterApply(ctx, tx); err != nil {
				t.Fatalf("history hook %d: %v", m.Version, err)
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations VALUES(?,'historical-main')`, m.Version); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestFeatureUpgradeFromReleased54AndMain60And63(t *testing.T) {
	t.Parallel()
	for _, version := range []int{54, 60, 63} {
		t.Run(fmt.Sprintf("main-%d", version), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			db := mainHistoryDatabase(t, root, version)
			metadata := `{"source":{"authority":"generic","connection_id":"connection","native_id":"source"},"source_refs":[{"authority":"generic","connection_id":"connection","native_id":"evidence","aliases":["main-alias"],"status":"done","extension":{"ref":"card:PRIVATE._/CARD"}}]}`
			observation := `{"observed_at":"2026-10-01T00:00:00Z","facts":{"phase":"done","identifier_aliases":["observation-alias"]}}`
			for _, statement := range []string{
				`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('private-thread','now','owner','{"pm_actor_id":"owner"}'),('public-thread','now','owner','{}')`,
				`INSERT INTO boards(id,title,summary,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by) VALUES('private-board','Private','','private-thread','{}','now','owner','now','owner'),('public-board','Public','','public-thread','{}','now','owner','now','owner')`,
				`INSERT INTO cards(id,board_id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES('private-card','private-board','Private','','todo','private-thread','now','owner','now','owner'),('source-card','public-board','Source','','todo','public-thread','now','owner','now','owner')`,
				`INSERT INTO agents(id,username,actor_id,created_at,updated_at) VALUES('agent','agent','owner','now','now')`,
				`INSERT INTO events(id,type,ts,actor_id,thread_id) VALUES('request-event','human_attention_requested','now','owner','public-thread')`,
				`INSERT INTO access_requests(id,principal_id,actor_id,username,grant_name,reason,created_at,request_event_id,inbox_item_id) VALUES('request','agent','owner','agent','write','preserve decision','now','request-event','request-item')`,
			} {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO work_metadata(card_id,authority,connection_id,native_id,metadata_json,latest_observation_id,updated_at,updated_by) VALUES('source-card','generic','connection','source',?,'observation','now','owner')`, metadata); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO work_observations(id,card_id,idempotency_key,digest,observed_at,received_at,status,body_json) VALUES('observation','source-card','fixture','digest','now','now','reported',?)`, observation); err != nil {
				t.Fatal(err)
			}
			if version == 60 {
				// Old main scanners did not index punctuation-normalized handles.
				if _, err := db.Exec(`DELETE FROM resource_access_edges WHERE source_kind='work_metadata' AND target_ref='card:private-card'`); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			for open := 0; open < 2; open++ {
				ws, err := InitializeWorkspace(ctx, root)
				if err != nil {
					t.Fatal(err)
				}
				db = ws.DB()
				var gotMetadata, gotObservation, role, status string
				if err = db.QueryRow(`SELECT metadata_json FROM work_metadata WHERE card_id='source-card'`).Scan(&gotMetadata); err != nil {
					t.Fatal(err)
				}
				if err = db.QueryRow(`SELECT body_json FROM work_observations WHERE id='observation'`).Scan(&gotObservation); err != nil {
					t.Fatal(err)
				}
				if gotMetadata != metadata || gotObservation != observation {
					t.Fatal("canonical evidence changed during upgrade")
				}
				if err = db.QueryRow(`SELECT role FROM boards WHERE id='public-board'`).Scan(&role); err != nil {
					t.Fatal(err)
				}
				if role != []string{"", "initiatives"}[open] {
					t.Fatalf("role=%q", role)
				}
				if err = db.QueryRow(`SELECT status FROM access_requests WHERE id='request'`).Scan(&status); err != nil || status != "pending" {
					t.Fatalf("request=%q err=%v", status, err)
				}
				var count int
				if err = db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version<=? AND applied_at<>'historical-main'`, version).Scan(&count); err != nil || count != 0 {
					t.Fatalf("main ledger changed: %d %v", count, err)
				}
				if err = db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version IN (64,65)`).Scan(&count); err != nil || count != 2 {
					t.Fatalf("feature ledger: %d %v", count, err)
				}
				if err = db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='trigger' AND name IN ('access_epoch_work_evidence_records_update','access_epoch_work_evidence_index_update','access_epoch_resource_access_external_edges_update')`).Scan(&count); err != nil || count != 3 {
					t.Fatalf("evidence snapshot invalidation triggers: %d %v", count, err)
				}
				if err = db.QueryRow(`SELECT count(*) FROM resource_access_identities WHERE kind IN ('work_evidence_record','work_evidence_alias')`).Scan(&count); err != nil || count != 0 {
					t.Fatalf("internal evidence IDs became public identities: %d %v", count, err)
				}
				if err = db.QueryRow(`SELECT count(*) FROM work_evidence_index WHERE lookup_key IN ('main-alias','observation-alias')`).Scan(&count); err != nil || count != 2 {
					t.Fatalf("backfill=%d err=%v", count, err)
				}
				if err = db.QueryRow(`SELECT count(*) FROM resource_access_edges WHERE source_kind='work_evidence_record' AND target_ref='card:private-card'`).Scan(&count); err != nil || count != 1 {
					t.Fatalf("ownership backfill=%d err=%v", count, err)
				}
				if err = db.QueryRow(`SELECT count(*) FROM resource_access_edges WHERE source_kind='work_metadata' AND target_ref='card:private-card'`).Scan(&count); err != nil || count != 1 {
					t.Fatalf("normalized canonical ownership backfill=%d err=%v", count, err)
				}
				if err = db.QueryRow(`SELECT count(*) FROM resource_access_external_edges WHERE source_kind='work_metadata' AND target_key=anx_resource_atom_key(CAST('card:private-card' AS BLOB))`).Scan(&count); err != nil || count != 1 {
					t.Fatalf("external-reference backfill=%d err=%v", count, err)
				}
				if err = db.QueryRow(`SELECT count(*) FROM resource_access_external_edges WHERE target_key IN (anx_resource_atom_key(CAST('main-alias' AS BLOB)),anx_resource_atom_key(CAST('observation-alias' AS BLOB)))`).Scan(&count); err != nil || count != 0 {
					t.Fatalf("publication fields became references=%d err=%v", count, err)
				}
				if open == 0 {
					if _, err = db.Exec(`UPDATE boards SET role='initiatives' WHERE id='public-board'`); err != nil {
						t.Fatal(err)
					}
					if _, err = db.Exec(`UPDATE work_evidence_records SET evidence_json=json_set(evidence_json,'$.reopen_marker',true)`); err != nil {
						t.Fatal(err)
					}
					if _, err = db.Exec(`INSERT INTO resource_access_external_edges VALUES('work_metadata','source-card',anx_resource_atom_key(CAST('external-reopen-marker' AS BLOB)))`); err != nil {
						t.Fatal(err)
					}
				} else {
					if err = db.QueryRow(`SELECT count(*) FROM work_evidence_records WHERE json_extract(evidence_json,'$.reopen_marker')=1`).Scan(&count); err != nil || count != 2 {
						t.Fatalf("reopen rebuilt evidence: %d %v", count, err)
					}
					if err = db.QueryRow(`SELECT count(*) FROM resource_access_external_edges WHERE target_key=anx_resource_atom_key(CAST('external-reopen-marker' AS BLOB))`).Scan(&count); err != nil || count != 1 {
						t.Fatalf("reopen rebuilt external ownership: %d %v", count, err)
					}
				}
				if err = ws.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
