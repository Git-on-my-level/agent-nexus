package storage

import (
	"context"
	"testing"
)

func TestWatcherPositionsUpgradeAndCanonicalLifecycle(t *testing.T) {
	ctx := context.Background()
	db := mainHistoryDatabase(t, t.TempDir(), 135)
	defer db.Close()
	for _, q := range []string{
		`INSERT INTO cards(id,title,column_key,created_at,created_by,updated_at,updated_by) VALUES('loop','Loop','todo','now','system','now','system')`,
		`INSERT INTO work_metadata(card_id,authority,metadata_json,updated_at,updated_by) VALUES('loop','nexus','{"cadence":{"every_seconds":60}}','now','system')`,
		`INSERT INTO events(id,handle,type,ts,actor_id,refs_json,payload_json) VALUES('run','run','run_recorded','2026-01-01T00:00:00Z','system','[]','{"subject_ref":"card:loop","payload":{"run_id":"one","outcome":"measured"}}')`,
		`INSERT INTO events(id,handle,type,ts,actor_id,refs_json,payload_json,archived_at) VALUES('effect','effect','run_recorded','2026-01-02T00:00:00Z','system','[]','{"subject_ref":"card:loop","payload":{"run_id":"two","outcome":"acted"}}','2026-01-03T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	// Migration 136 installs the watcher positions; later migrations follow it.
	for _, m := range migrations {
		if m.Version == 136 {
			if err := applyMigration(ctx, db, m); err != nil {
				t.Fatal(err)
			}
		}
	}
	check := func(candidates, runs, effects int) {
		t.Helper()
		var c, r, e int
		if err := db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM watcher_loop_candidates),(SELECT COUNT(*) FROM watcher_run_positions),(SELECT COUNT(*) FROM watcher_effect_positions)`).Scan(&c, &r, &e); err != nil || c != candidates || r != runs || e != effects {
			t.Fatalf("candidates/runs/effects=%d/%d/%d want=%d/%d/%d error=%v", c, r, e, candidates, runs, effects, err)
		}
	}
	check(1, 1, 0)
	for _, step := range []struct {
		q       string
		c, r, e int
	}{
		{`UPDATE events SET archived_at=NULL WHERE id='effect'`, 1, 2, 1},
		{`UPDATE events SET payload_json=json_set(payload_json,'$.payload.outcome','measured') WHERE id='effect'`, 1, 2, 0},
		{`UPDATE events SET trashed_at='now' WHERE id='run'`, 1, 1, 0},
		{`UPDATE events SET type='message_posted' WHERE id='effect'`, 1, 0, 0},
		{`UPDATE events SET type='run_recorded',payload_json=json_set(payload_json,'$.payload.experiment.decision','kept') WHERE id='effect'`, 1, 1, 1},
		{`DELETE FROM events WHERE id='effect'`, 1, 0, 0},
		{`UPDATE work_metadata SET metadata_json='{}' WHERE card_id='loop'`, 0, 0, 0},
		{`UPDATE work_metadata SET metadata_json='{"goal":{"metric":"score"}}' WHERE card_id='loop'`, 1, 0, 0},
		{`DELETE FROM work_metadata WHERE card_id='loop'`, 0, 0, 0},
	} {
		if _, err := db.ExecContext(ctx, step.q); err != nil {
			t.Fatal(step.q, err)
		}
		check(step.c, step.r, step.e)
	}
}
