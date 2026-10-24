package storage

import (
	"context"
	"testing"
)

func TestGroupingUpgradeRequeuesPromisesAndPreservesPlans(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 114)
	for _, query := range []string{
		`INSERT INTO cards(id,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES('parent','Parent','','ready','now','writer','now','writer'),('child','Child','','ready','now','writer','now','writer')`,
		`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES('parent','{"steps":[{"id":"child","title":"Child","ref":"card:child"}]}','now')`,
		`INSERT INTO brief_initiatives(card_id,open) VALUES('parent',1),('child',1)`,
		`DELETE FROM brief_dirty`,
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
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
	for _, check := range []struct {
		query string
		want  int
	}{
		{`SELECT count(*) FROM brief_dirty WHERE card_id IN ('parent','child')`, 2},
		{`SELECT count(*) FROM brief_plan_refs WHERE card_id='parent' AND ref='card:child'`, 1},
		{`SELECT count(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_brief_notes_promise_facts','idx_cards_grouping_recent','idx_cards_grouping_closed','idx_events_promise_facts')`, 4},
		{`SELECT count(*) FROM sqlite_master WHERE type='view' AND name IN ('work_grouping_card_positions','work_grouping_brief_positions','work_grouping_field_positions')`, 3},
	} {
		var got int
		if err = ws.DB().QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s: got=%d want=%d err=%v", check.query, got, check.want, err)
		}
	}
}
