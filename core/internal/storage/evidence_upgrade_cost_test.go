package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestEvidenceUpgradePreservesUnrelatedAuthorizationIndexes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 63)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if _, err = tx.ExecContext(ctx, `INSERT INTO documents(id,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES(?, 'Unrelated document', '', 0, 'now', 'owner', 'now', 'owner')`, fmt.Sprintf("unrelated-%04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var identities int
	if err = db.QueryRow(`SELECT count(*) FROM resource_access_identities WHERE origin='documents'`).Scan(&identities); err != nil || identities != 1000 {
		t.Fatalf("fixture identities=%d err=%v", identities, err)
	}
	// Audit actual index mutations, not elapsed time: a delete/reinsert can
	// recreate identical final contents while doing workspace-sized startup work.
	if _, err = db.Exec(`CREATE TABLE upgrade_index_touches(name TEXT NOT NULL, operation TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"resource_access_edges", "resource_access_identities", "resource_access_exact_edges", "resource_access_mentions", "resource_access_mention_buckets"} {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			q := `CREATE TRIGGER upgrade_audit_` + table + `_` + op + ` AFTER ` + op + ` ON ` + table + ` BEGIN INSERT INTO upgrade_index_touches VALUES('` + table + `','` + op + `'); END`
			if _, err = db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	for open := 0; open < 2; open++ {
		ws, err := InitializeWorkspace(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		var touches, got int
		if err = ws.DB().QueryRow(`SELECT count(*) FROM upgrade_index_touches`).Scan(&touches); err != nil || touches != 0 {
			ws.Close()
			t.Fatalf("open %d rebuilt unrelated authorization indexes: touched rows=%d err=%v", open, touches, err)
		}
		if err = ws.DB().QueryRow(`SELECT count(*) FROM resource_access_identities WHERE origin='documents'`).Scan(&got); err != nil || got != identities {
			ws.Close()
			t.Fatalf("preserved identities=%d want=%d err=%v", got, identities, err)
		}
		for _, table := range []string{"work_metadata", "work_evidence_records", "work_evidence_index"} {
			if err = ws.DB().QueryRow(`SELECT count(*) FROM ` + table).Scan(&got); err != nil || got != 0 {
				ws.Close()
				t.Fatalf("fixture %s rows=%d err=%v", table, got, err)
			}
		}
		if err = ws.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEvidenceUpgradeBackfillsChangedSourcesAcrossBatches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 63)
	if _, err := db.Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('board-thread','now','owner','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO boards(id,title,summary,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by) VALUES('board','Board','','board-thread','{}','now','owner','now','owner')`); err != nil {
		t.Fatal(err)
	}
	const cards = 35
	for i := 0; i < cards; i++ {
		id := fmt.Sprintf("card-%04d", i)
		entry := map[string]any{"authority": "generic", "connection_id": "connection", "native_id": id, "aliases": []string{"ALIAS-" + id}, "extension": map[string]any{"ref": "document:PRIVATE._/DOC"}}
		metadata, _ := json.Marshal(map[string]any{"source": map[string]any{"authority": "generic"}, "source_refs": []any{entry}})
		observation, _ := json.Marshal(map[string]any{"evidence": []any{entry}})
		if _, err := db.Exec(`INSERT INTO cards(id,board_id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES(?,'board','Source','','todo','board-thread','now','owner','now','owner')`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO work_metadata(card_id,authority,connection_id,native_id,metadata_json,latest_observation_id,updated_at,updated_by) VALUES(?,'generic','connection',?,?,?,'now','owner')`, id, id, string(metadata), id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO work_observations(id,card_id,idempotency_key,digest,observed_at,received_at,status,body_json) VALUES(?,?,'fixture','digest','now','now','reported',?)`, id, id, string(observation)); err != nil {
			t.Fatal(err)
		}
	}
	// Model the main scanner's older spelling without the feature's normalized
	// atoms. Keep original edges; only the changed sources require supplementation.
	if _, err := db.Exec(`DELETE FROM resource_access_edges WHERE source_kind IN ('work_metadata','work_observation') AND target_ref='document:private-doc'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, source := range []struct {
		kind string
		want int
	}{{"work_metadata", cards}, {"work_observation", cards}, {"work_evidence_record", cards * 2}} {
		var got int
		if err = ws.DB().QueryRow(`SELECT count(*) FROM resource_access_edges WHERE source_kind=? AND target_ref='document:private-doc'`, source.kind).Scan(&got); err != nil || got != source.want {
			t.Fatalf("%s backfill rows=%d want=%d err=%v", source.kind, got, source.want, err)
		}
		if err = ws.DB().QueryRow(`SELECT count(*) FROM resource_access_exact_edges WHERE source_kind=? AND target_key=anx_resource_atom_key(CAST('document:private-doc' AS BLOB))`, source.kind).Scan(&got); err != nil || got != source.want {
			t.Fatalf("%s exact index rows=%d want=%d err=%v", source.kind, got, source.want, err)
		}
	}
	var got int
	if err = ws.DB().QueryRow(`SELECT count(*) FROM work_evidence_records`).Scan(&got); err != nil || got != cards*3 {
		t.Fatalf("evidence records=%d want=%d err=%v", got, cards*3, err)
	}
	if err = ws.DB().QueryRow(`SELECT count(DISTINCT card_id) FROM work_evidence_index WHERE lookup_key LIKE 'ALIAS-%'`).Scan(&got); err != nil || got != cards {
		t.Fatalf("alias cards=%d want=%d err=%v", got, cards, err)
	}
}
