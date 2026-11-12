package storage

import (
	"context"
	"testing"
)

// Migration 147 removes indexes that are either covered by a wider index or
// have no reader. Fresh databases must not recreate them, and upgraded
// databases must drop them.
func TestDropRedundantIndexes(t *testing.T) {
	ctx := context.Background()
	dropped := []string{
		"idx_events_thread_ts",
		"idx_events_overview_answers",
		"idx_auth_audit_events_occurred_at",
		"idx_auth_audit_events_invite_id",
	}
	kept := []string{
		"idx_events_thread_ts_id",
		"idx_events_type_ts_id",
		"idx_auth_audit_events_event_type",
		"idx_auth_audit_events_sort_key",
	}
	check := func(t *testing.T, root string) {
		t.Helper()
		ws, err := InitializeWorkspace(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()
		for _, name := range dropped {
			var count int
			if err := ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("dropped index %s still exists", name)
			}
		}
		for _, name := range kept {
			var count int
			if err := ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("kept index %s missing", name)
			}
		}
	}

	t.Run("fresh", func(t *testing.T) {
		check(t, t.TempDir())
	})

	t.Run("upgrade", func(t *testing.T) {
		root := t.TempDir()
		db := mainHistoryDatabase(t, root, 138)
		for _, statement := range []string{
			`CREATE INDEX idx_events_thread_ts ON events(thread_id, ts)`,
			`CREATE INDEX idx_events_overview_answers ON events(type, ts, id)`,
			`CREATE INDEX idx_auth_audit_events_occurred_at ON auth_audit_events(occurred_at DESC, id DESC)`,
			`CREATE INDEX idx_auth_audit_events_invite_id ON auth_audit_events(invite_id, occurred_at DESC, id DESC)`,
		} {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		check(t, root)
	})
}
