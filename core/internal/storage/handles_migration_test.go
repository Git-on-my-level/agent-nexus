package storage

import (
	"context"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigration25BackfillsResourceHandles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	// Use the complete released schema: dropping obsolete host columns must
	// validate all views/triggers, which sparse hand-built ledgers cannot model.
	db := mainHistoryDatabase(t, root, 24)
	if _, err := db.ExecContext(ctx, `WITH seed(id,title,created_at) AS (VALUES
		('duplicate-name', 'Legacy ID Collision', '2026-01-01T00:00:00Z'),
		('t1', 'Duplicate Name', '2026-01-01T00:00:00Z'),
		('t2', 'Duplicate Name', '2026-01-01T00:00:01Z'),
		('t3', 'settings', '2026-01-01T00:00:02Z'),
		('550e8400-e29b-41d4-a716-446655440000', '550e8400-e29b-41d4-a716-446655440000', '2026-01-01T00:00:03Z'),
		('t5', 'Café Roadmap', '2026-01-01T00:00:04Z')) INSERT INTO topics(id,title,created_at,created_by,updated_at,updated_by) SELECT id,title,created_at,'owner',created_at,'owner' FROM seed`); err != nil {
		t.Fatalf("insert topics: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatalf("initialize migrated workspace: %v", err)
	}
	defer ws.Close()

	got := map[string]string{}
	rows, err := ws.DB().QueryContext(ctx, `SELECT id, handle FROM topics`)
	if err != nil {
		t.Fatalf("select handles: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, handle string
		if err := rows.Scan(&id, &handle); err != nil {
			t.Fatalf("scan handle: %v", err)
		}
		got[id] = handle
	}
	if got["t1"] != "duplicate-name-2" || got["t2"] != "duplicate-name-3" {
		t.Fatalf("duplicate handles not suffixed as expected: %#v", got)
	}
	if got["duplicate-name"] == "duplicate-name" {
		t.Fatalf("handle should not collide with an existing storage id: %#v", got)
	}
	if got["t3"] == "settings" || got["t3"] == "" {
		t.Fatalf("reserved title should receive fallback handle: %#v", got)
	}
	if got["550e8400-e29b-41d4-a716-446655440000"] == "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("uuid-looking title should not become handle: %#v", got)
	}
	if got["t5"] != "caf-roadmap" {
		t.Fatalf("non-ASCII normalization: %#v", got)
	}
}
