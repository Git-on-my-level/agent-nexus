package storage_test

import (
	"context"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestWatcherHealthMigrationPreservesHistoryAndReopenState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	// Model the released schema immediately before 136, retaining canonical data.
	for _, q := range []string{
		`INSERT INTO events(id,handle,type,ts,actor_id,refs_json,payload_json) VALUES('watcher-legacy','watcher-legacy','message_posted','2026-01-01T00:00:00Z','system','[]','{"payload":{"text":"Original history"}}')`,
		`DROP VIEW watcher_run_positions`, `DROP VIEW watcher_effect_positions`,
		`DROP INDEX idx_watcher_loop_runs`, `DROP INDEX idx_watcher_loop_effects`,
		`DROP INDEX idx_work_metadata_health`, `DROP TABLE watcher_health`, `DROP TABLE watcher_health_cursor`,
		`DELETE FROM schema_migrations WHERE version=136`,
	} {
		if _, err = ws.DB().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var raw, cursor string
	if err = ws.DB().QueryRowContext(ctx, `SELECT payload_json FROM events WHERE id='watcher-legacy'`).Scan(&raw); err != nil || raw != `{"payload":{"text":"Original history"}}` {
		t.Fatal(raw, err)
	}
	if err = ws.DB().QueryRowContext(ctx, `SELECT after_card_id FROM watcher_health_cursor WHERE singleton=1`).Scan(&cursor); err != nil || cursor != "" {
		t.Fatal(cursor, err)
	}
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO watcher_health(alarm_key,episode,raise_count,next_raise_at) VALUES('pm',9,3,'2027-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var episode, raises int
	var next string
	if err = ws.DB().QueryRowContext(ctx, `SELECT episode,raise_count,next_raise_at FROM watcher_health WHERE alarm_key='pm'`).Scan(&episode, &raises, &next); err != nil || episode != 9 || raises != 3 || next != "2027-01-01T00:00:00Z" {
		t.Fatal(episode, raises, next, err)
	}
}
