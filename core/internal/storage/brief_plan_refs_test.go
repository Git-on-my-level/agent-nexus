package storage

import (
	"context"
	"strings"
	"testing"
)

func TestBriefPlanRefsUpgradeAndInvalidation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 107)
	for _, q := range []string{
		`INSERT INTO cards(id,handle,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES('child','linked','Child','','todo','now','owner','now','owner'),('parent',NULL,'Parent','','todo','now','owner','now','owner')`,
		`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES('parent','{"steps":[{"id":"step","title":"Build","ref":"card:linked"}]}','now')`,
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
	count := func(q string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRow(q).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: %d want=%d err=%v", q, got, want, err)
		}
	}
	exec := func(q string) {
		t.Helper()
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	count(`SELECT count(*) FROM brief_plan_refs WHERE ref='card:linked' AND card_id='parent'`, 1)
	count(`SELECT count(*) FROM brief_dirty WHERE card_id='parent'`, 1)
	exec(`DELETE FROM brief_dirty`)
	exec(`UPDATE cards SET column_key='done' WHERE id='child'`)
	count(`SELECT count(*) FROM brief_dirty WHERE card_id='parent'`, 1)
	// A canonical metadata upsert can override legacy OR IGNORE inside nested
	// triggers. Existing queue entries and duplicate evidence keys must replay.
	exec(`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('child','{}','now','owner') ON CONFLICT(card_id) DO UPDATE SET metadata_json=excluded.metadata_json`)
	exec(`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES('child','{}','now','owner') ON CONFLICT(card_id) DO UPDATE SET metadata_json=excluded.metadata_json`)
	count(`SELECT count(*) FROM brief_dirty WHERE card_id='parent'`, 1)
	rows, err := db.Query(`EXPLAIN QUERY PLAN SELECT card_id FROM brief_plan_refs WHERE ref IN ('card:child','card:linked')`)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for rows.Next() {
		var a, b, c int
		var detail string
		if err = rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "SEARCH brief_plan_refs USING COVERING INDEX idx_brief_plan_refs_target") {
			found = true
		}
	}
	rows.Close()
	if !found {
		t.Fatal("reverse lookup does not seek target index")
	}
	exec(`UPDATE card_plans SET body_json='{"steps":[]}' WHERE card_id='parent'`)
	count(`SELECT count(*) FROM brief_plan_refs WHERE card_id='parent'`, 0)
	exec(`DELETE FROM brief_dirty`)
	exec(`UPDATE cards SET column_key='todo' WHERE id='child'`)
	count(`SELECT count(*) FROM brief_dirty WHERE card_id='parent'`, 0)
	exec(`UPDATE card_plans SET body_json='{"steps":[{"id":"step","title":"Build","ref":"card:linked"}]}' WHERE card_id='parent'`)
	exec(`DELETE FROM card_plans WHERE card_id='parent'`)
	count(`SELECT count(*) FROM brief_plan_refs WHERE card_id='parent'`, 0)
	count(`SELECT count(*) FROM brief_dirty WHERE card_id='parent'`, 1)
}
