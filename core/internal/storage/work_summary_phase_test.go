package storage

import (
	"context"
	"database/sql"
	"testing"
)

func TestPrimaryCardPhaseRepairPreservesActivityAndSecondaryPlacement(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE cards(id TEXT PRIMARY KEY,board_id TEXT,column_key TEXT,updated_at TEXT);
CREATE TABLE ref_edges(id TEXT PRIMARY KEY,source_type TEXT,source_id TEXT,target_type TEXT,target_id TEXT,edge_type TEXT,metadata_json TEXT,
UNIQUE(source_type,source_id,target_type,target_id,edge_type));
INSERT INTO cards VALUES('card','primary','backlog','unchanged');
INSERT INTO ref_edges VALUES('primary','board','primary','card','card','board_card','{"column_key":"blocked","rank":"1"}');
INSERT INTO ref_edges VALUES('secondary','board','secondary','card','card','board_card','{"column_key":"review","rank":"1"}');`)
	if err != nil {
		t.Fatal(err)
	}
	repair := func() {
		t.Helper()
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = repairPrimaryCardPhases(context.Background(), tx); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	repair()
	check := func(want string) {
		t.Helper()
		var phase, at string
		if err := db.QueryRow(`SELECT column_key,updated_at FROM cards WHERE id='card'`).Scan(&phase, &at); err != nil {
			t.Fatal(err)
		}
		if phase != want || at != "unchanged" {
			t.Fatalf("phase=%s activity=%s", phase, at)
		}
	}
	check("blocked")
	for _, edit := range []struct{ id, phase, want string }{{"secondary", "done", "blocked"}, {"primary", "review", "review"}, {"primary", "document:private", "review"}} {
		if _, err := db.Exec(`UPDATE ref_edges SET metadata_json=json_set(metadata_json,'$.column_key',?) WHERE id=?`, edit.phase, edit.id); err != nil {
			t.Fatal(err)
		}
		repair()
		check(edit.want)
	}
}
