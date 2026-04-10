package storage_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"organization-autorunner-core/internal/blob"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/server"
	"organization-autorunner-core/internal/storage"

	_ "modernc.org/sqlite"
)

func TestWorkspaceInitializationAndRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspaceRoot := t.TempDir()

	first, err := storage.InitializeWorkspace(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("initialize first workspace: %v", err)
	}

	layout := first.Layout()
	requiredDirs := []string{
		layout.RootDir,
		layout.ArtifactsDir,
		layout.ArtifactContentDir,
		layout.LogsDir,
		layout.TmpDir,
	}
	for _, dir := range requiredDirs {
		assertDirExists(t, dir)
	}

	requiredTables := []string{
		"schema_migrations",
		"events",
		"threads",
		"topics",
		"ref_edges",
		"artifacts",
		"actors",
		"documents",
		"document_revisions",
		"boards",
		"cards",
		"card_versions",
		"derived_inbox_items",
		"derived_topic_views",
		"derived_topic_dirty_queue",
		"topic_projection_refresh_status",
		"agents",
		"agent_keys",
		"auth_refresh_sessions",
		"auth_access_tokens",
		"auth_used_assertions",
		"auth_bootstrap_state",
		"auth_invites",
		"auth_audit_events",
		"blob_usage_ledger",
		"blob_usage_totals",
	}
	assertTablesExist(t, first.DB(), requiredTables)
	assertHealthOK(t, first)

	if _, err := first.DB().ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json) VALUES (?, ?, ?, ?, ?)`,
		"actor-1",
		"Actor One",
		"[]",
		"2026-03-04T00:00:00Z",
		"{}",
	); err != nil {
		t.Fatalf("insert actor row: %v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("close first workspace: %v", err)
	}

	second, err := storage.InitializeWorkspace(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("initialize second workspace: %v", err)
	}
	defer second.Close()

	assertTablesExist(t, second.DB(), requiredTables)
	assertHealthOK(t, second)

	var actorCount int
	if err := second.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM actors WHERE id = ?`, "actor-1").Scan(&actorCount); err != nil {
		t.Fatalf("count persisted actor row: %v", err)
	}
	if actorCount != 1 {
		t.Fatalf("expected 1 persisted actor row, got %d", actorCount)
	}

	var migrationCount int
	if err := second.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count schema migration rows: %v", err)
	}
	if migrationCount < 1 {
		t.Fatalf("expected at least one schema_migrations row (v1 baseline), got %d", migrationCount)
	}

	if got := filepath.Dir(layout.DatabasePath); got != layout.RootDir {
		t.Fatalf("database path should be rooted under workspace: got %q root %q", got, layout.RootDir)
	}
}

func TestWorkspaceInitializationWithRelativeRoot(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	workspaceRoot := t.TempDir()
	relativeRoot, err := filepath.Rel(cwd, workspaceRoot)
	if err != nil {
		t.Fatalf("derive relative workspace path: %v", err)
	}
	if filepath.IsAbs(relativeRoot) {
		t.Fatalf("expected relative path, got %q", relativeRoot)
	}

	workspace, err := storage.InitializeWorkspace(context.Background(), relativeRoot)
	if err != nil {
		t.Fatalf("initialize workspace from relative root %q: %v", relativeRoot, err)
	}
	defer workspace.Close()

	assertHealthOK(t, workspace)

	if _, err := os.Stat(filepath.Join(workspaceRoot, "state.sqlite")); err != nil {
		t.Fatalf("expected sqlite database under workspace root: %v", err)
	}
}

func TestProjectionQueueStatsAndListingRecoverStrandedGenerationRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(
		workspace.DB(),
		blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir),
		workspace.Layout().ArtifactContentDir,
	)

	if _, err := workspace.DB().ExecContext(
		ctx,
		`INSERT INTO topic_projection_refresh_status(
			thread_id,
			desired_generation,
			materialized_generation,
			in_progress_generation,
			queued_at,
			started_at,
			updated_at
		) VALUES (?, 3, 2, 3, NULL, ?, ?)`,
		"stranded-thread",
		"2026-03-21T10:00:00Z",
		"2026-03-21T10:00:00Z",
	); err != nil {
		t.Fatalf("seed stranded projection status: %v", err)
	}

	entries, err := store.ListDerivedTopicProjectionDirtyEntries(ctx, 10)
	if err != nil {
		t.Fatalf("list dirty projection entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one recoverable dirty entry, got %#v", entries)
	}
	if entries[0].ThreadID != "stranded-thread" {
		t.Fatalf("expected stranded thread to be returned, got %#v", entries[0])
	}
	if entries[0].DirtyAt != "2026-03-21T10:00:00Z" {
		t.Fatalf("expected stranded dirty_at to come from status timestamps, got %#v", entries[0])
	}

	stats, err := store.GetDerivedTopicProjectionQueueStats(ctx)
	if err != nil {
		t.Fatalf("load queue stats: %v", err)
	}
	if stats.PendingCount != 1 {
		t.Fatalf("expected pending count to include stranded status rows, got %#v", stats)
	}
	if stats.OldestDirtyAt != "2026-03-21T10:00:00Z" {
		t.Fatalf("expected oldest dirty timestamp from stranded status row, got %#v", stats)
	}
}

func TestWorkspaceMigrationV6BackfillsLegacyDocumentLifecycleBeforeDroppingStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspaceRoot := t.TempDir()
	layout := storage.NewLayout(workspaceRoot)

	db, err := sql.Open("sqlite", "file:"+layout.DatabasePath)
	if err != nil {
		t.Fatalf("open legacy sqlite database: %v", err)
	}

	statements := []string{
		`CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE documents (
			id TEXT PRIMARY KEY,
			status TEXT,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			updated_by TEXT NOT NULL,
			trashed_at TEXT,
			trashed_by TEXT,
			trash_reason TEXT,
			archived_at TEXT,
			archived_by TEXT
		);`,
		`CREATE INDEX idx_documents_status_trashed_updated_at ON documents (status, trashed_at, updated_at DESC, id);`,
		`INSERT INTO documents(id, status, created_at, created_by, updated_at, updated_by) VALUES
			('doc-archived', 'archived', '2026-03-01T00:00:00Z', 'actor-create', '2026-03-02T00:00:00Z', 'actor-update'),
			('doc-trashed', 'trashed', '2026-03-03T00:00:00Z', 'actor-create-2', '2026-03-04T00:00:00Z', 'actor-update-2');`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
	for version := 1; version <= 5; version++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`, version); err != nil {
			t.Fatalf("seed schema migration %d: %v", version, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded database: %v", err)
	}

	workspace, err := storage.InitializeWorkspace(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("initialize migrated workspace: %v", err)
	}
	defer workspace.Close()

	assertColumnAbsent(t, workspace.DB(), "documents", "status")

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

func assertHealthOK(t *testing.T, workspace *storage.Workspace) {
	t.Helper()

	handler := server.NewHandler("0.2.2", server.WithHealthCheck(workspace.Ping))
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /health status: got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /health response: %v", err)
	}
	if body["ok"] != true {
		t.Fatalf("expected ok=true, got %#v", body["ok"])
	}
}

func assertColumnPresent(t *testing.T, db *sql.DB, tableName string, columnName string) {
	t.Helper()
	if !columnExists(t, db, tableName, columnName) {
		t.Fatalf("expected column %s.%s to exist", tableName, columnName)
	}
}

func assertColumnAbsent(t *testing.T, db *sql.DB, tableName string, columnName string) {
	t.Helper()
	if columnExists(t, db, tableName, columnName) {
		t.Fatalf("expected column %s.%s to be absent", tableName, columnName)
	}
}

func columnExists(t *testing.T, db *sql.DB, tableName string, columnName string) bool {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), "PRAGMA table_info("+tableName+")")
	if err != nil {
		t.Fatalf("describe table %s: %v", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("scan table info %s: %v", tableName, err)
		}
		if name == columnName {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info %s: %v", tableName, err)
	}
	return false
}

func assertDirExists(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", path)
	}
}

func assertTablesExist(t *testing.T, db *sql.DB, names []string) {
	t.Helper()

	for _, name := range names {
		var tableName string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&tableName)
		if err != nil {
			t.Fatalf("table %q not found: %v", name, err)
		}
		if tableName != name {
			t.Fatalf("unexpected table lookup result: got %q want %q", tableName, name)
		}
	}
}
