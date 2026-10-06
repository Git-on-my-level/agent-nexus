package storage_test

import (
	"agent-nexus-core/internal/storage"
	"context"
	"testing"
)

func TestBoardRoleMigrationPreservesLegacyBoards(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	db := ws.DB()
	if _, err = db.ExecContext(ctx, `INSERT INTO boards(id,title,summary,owners_json,thread_id,refs_json,column_schema_json,created_at,created_by,updated_at,updated_by) VALUES('legacy','Legacy','','[]','legacy','[]','{}','2026-10-01','actor','2026-10-01','actor')`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP INDEX idx_boards_role`, `DROP TRIGGER access_boards_insert`, `DROP TRIGGER access_boards_update`} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.ExecContext(ctx, `ALTER TABLE boards DROP COLUMN role`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=64`); err != nil {
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
	var role, title string
	if err = ws.DB().QueryRowContext(ctx, `SELECT role,title FROM boards WHERE id='legacy'`).Scan(&role, &title); err != nil || role != "" || title != "Legacy" {
		t.Fatalf("role=%q title=%q err=%v", role, title, err)
	}
}
