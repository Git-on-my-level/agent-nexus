package storage

import (
	"context"
	"testing"
)

func TestWorkspaceMigrationV6BackfillsLegacyDocumentLifecycleBeforeDroppingStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspaceRoot := t.TempDir()
	// Build real v5 history rather than an incomplete migration ledger.
	db := mainHistoryDatabase(t, workspaceRoot, 5)
	statements := []string{
		`INSERT INTO documents(id, head_revision_id, head_revision_number, status, created_at, created_by, updated_at, updated_by) VALUES
			('doc-archived', 'head-archived', 1, 'archived', '2026-03-01T00:00:00Z', 'actor-create', '2026-03-02T00:00:00Z', 'actor-update'),
			('doc-trashed', 'head-trashed', 1, 'trashed', '2026-03-03T00:00:00Z', 'actor-create-2', '2026-03-04T00:00:00Z', 'actor-update-2');`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}

	workspace, err := InitializeWorkspace(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("initialize migrated workspace: %v", err)
	}
	defer workspace.Close()

	var remaining int
	if err := workspace.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('documents') WHERE name='status'`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("obsolete document status", remaining, err)
	}

	var (
		archivedAt  string
		archivedBy  string
		trashedAt   string
		trashedBy   string
		trashReason string
	)
	if err := workspace.DB().QueryRowContext(ctx, `SELECT archived_at, archived_by FROM documents WHERE id = ?`, "doc-archived").Scan(&archivedAt, &archivedBy); err != nil {
		t.Fatalf("query archived document lifecycle: %v", err)
	}
	if archivedAt != "2026-03-02T00:00:00Z" || archivedBy != "actor-update" {
		t.Fatalf("unexpected archived lifecycle backfill: archived_at=%q archived_by=%q", archivedAt, archivedBy)
	}

	if err := workspace.DB().QueryRowContext(ctx, `SELECT trashed_at, trashed_by, trash_reason FROM documents WHERE id = ?`, "doc-trashed").Scan(&trashedAt, &trashedBy, &trashReason); err != nil {
		t.Fatalf("query trashed document lifecycle: %v", err)
	}
	if trashedAt != "2026-03-04T00:00:00Z" || trashedBy != "actor-update-2" || trashReason != "legacy status migration" {
		t.Fatalf("unexpected trashed lifecycle backfill: trashed_at=%q trashed_by=%q trash_reason=%q", trashedAt, trashedBy, trashReason)
	}
}
