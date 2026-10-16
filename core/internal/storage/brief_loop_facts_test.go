package storage

import (
	"context"
	"testing"
)

func TestBriefLoopFactsUpgradeAndReceiptMutations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 99)
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('t','now','owner','{}')`,
		`INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES('c','Loop','','todo','t','now','owner','now','owner'),('d','Other','','todo','t','now','owner','now','owner')`,
		`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('r','run_recorded','2026-10-16T00:00:00Z','owner','t','["card:c"]','{"subject_ref":"card:c","payload":{"run_id":"one","outcome":"measured"}}')`,
		`DELETE FROM brief_dirty`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	check := func(q string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRow(q).Scan(&got); err != nil || got != want {
			t.Fatalf("%s got=%d want=%d err=%v", q, got, want, err)
		}
	}
	check(`SELECT count(*) FROM resource_access_edges WHERE source_kind='brief_loop' AND source_id='loop-facts/c' AND target_ref='event:r'`, 1)
	check(`SELECT count(*) FROM brief_dirty WHERE card_id IN ('c','d')`, 2)
	// Re-running the installer is safe on a populated workspace.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installBriefLoopFacts(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		sql      string
		old, new int
	}{
		{`UPDATE events SET payload_json=json_set(payload_json,'$.subject_ref','card:d') WHERE id='r'`, 0, 1},
		{`UPDATE events SET type='card_updated' WHERE id='r'`, 0, 0},
		{`UPDATE events SET type='run_recorded' WHERE id='r'`, 0, 1},
		{`DELETE FROM events WHERE id='r'`, 0, 0},
	} {
		if _, err = db.Exec(`DELETE FROM brief_dirty`); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(mutation.sql); err != nil {
			t.Fatal(err)
		}
		check(`SELECT count(*) FROM resource_access_edges WHERE source_kind='brief_loop' AND source_id='loop-facts/c'`, mutation.old)
		check(`SELECT count(*) FROM resource_access_edges WHERE source_kind='brief_loop' AND source_id='loop-facts/d'`, mutation.new)
		check(`SELECT count(*) FROM brief_dirty WHERE card_id='d'`, 1)
	}
}
