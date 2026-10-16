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

func TestIncumbentLoopFactsUpgradeInvalidatesOldCaches(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 100)
	if _, err := db.Exec(`INSERT INTO brief_initiatives(card_id,computed_at,loop_facts_json,loop_valid_from,loop_valid_until) VALUES('old-loop','now','{"best_before":99,"best_after":100}','2026-01-01T00:00:00Z','2099-01-01T00:00:00Z'); DELETE FROM brief_dirty`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM brief_dirty WHERE card_id='old-loop'`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err = ws.DB().Exec(`DELETE FROM brief_dirty`); err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM brief_dirty WHERE card_id='old-loop'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("cache requeued again on restart", count, err)
	}
}

func TestLoopAdmissionUpgradeAndMutations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 101)
	for _, query := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('t','now','owner','{}')`,
		`INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES('c','Loop','','todo','t','now','owner','now','owner'),('d','Other','','todo','t','now','owner','now','owner')`,
		`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('c','{"goal":{"metric":"score","direction":"up"}}','now','owner')`,
		`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('r','run_recorded','2026-10-16T00:00:00Z','owner','t','["card:c"]','{"subject_ref":"card:c","payload":{"run_id":"one","outcome":"measured"}}')`,
	} {
		if _, err := db.Exec(query); err != nil {
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
	check := func(id string, annotated int, last string) {
		t.Helper()
		var got int
		var at string
		if err := db.QueryRow(`SELECT loop_annotated,COALESCE(loop_last_run_at,'') FROM brief_initiatives WHERE card_id=?`, id).Scan(&got, &at); err != nil || got != annotated || at != last {
			t.Fatal(id, got, at, err)
		}
	}
	at := "2026-10-16T00:00:00Z"
	check("c", 1, at)
	check("d", 0, "")
	for _, query := range []string{
		`UPDATE work_metadata SET metadata_json='{}' WHERE card_id='c'`,
		`UPDATE events SET payload_json=json_set(payload_json,'$.subject_ref','card:d') WHERE id='r'`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	check("c", 0, "")
	check("d", 0, at)
	for _, query := range []string{
		`UPDATE events SET archived_at='now' WHERE id='r'`,
		`UPDATE events SET archived_at=NULL WHERE id='r'`,
		`UPDATE events SET type='card_updated' WHERE id='r'`,
		`UPDATE events SET type='run_recorded' WHERE id='r'`,
		`DELETE FROM events WHERE id='r'`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
		want := ""
		if query == `UPDATE events SET archived_at=NULL WHERE id='r'` || query == `UPDATE events SET type='run_recorded' WHERE id='r'` {
			want = at
		}
		check("d", 0, want)
	}
	if _, err = db.Exec(`UPDATE work_metadata SET metadata_json='{"cadence":{"every_seconds":60}}' WHERE card_id='c'`); err != nil {
		t.Fatal(err)
	}
	check("c", 1, "")
	if _, err = db.Exec(`DELETE FROM work_metadata WHERE card_id='c'`); err != nil {
		t.Fatal(err)
	}
	check("c", 0, "")
}

func TestLoopAdmissionRecencyUpgradeAndMetadataWrites(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 102)
	for _, query := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('t','now','owner','{}')`,
		`INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES('c','Loop','','todo','t','now','owner','now','owner')`,
		`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('c','{"goal":{"metric":"score","direction":"up"}}','2026-10-15T10:00:00Z','owner')`,
	} {
		if _, err := db.Exec(query); err != nil {
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
	check := func(want string) {
		t.Helper()
		var at string
		if err := db.QueryRow(`SELECT loop_annotated_at FROM brief_initiatives WHERE card_id='c'`).Scan(&at); err != nil || at != want {
			t.Fatal(at, want, err)
		}
	}
	check("2026-10-15T10:00:00Z")
	if _, err = db.Exec(`UPDATE work_metadata SET updated_at='2026-10-16T10:00:00Z' WHERE card_id='c'`); err != nil {
		t.Fatal(err)
	}
	check("2026-10-16T10:00:00Z")
	if _, err = db.Exec(`DELETE FROM work_metadata WHERE card_id='c'`); err != nil {
		t.Fatal(err)
	}
	check("")
	if _, err = db.Exec(`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('c','{"cadence":{"every_seconds":60}}','2026-10-16T11:00:00Z','owner')`); err != nil {
		t.Fatal(err)
	}
	check("2026-10-16T11:00:00Z")
}
