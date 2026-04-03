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
		"commitments",
		"topics",
		"ref_edges",
		"artifacts",
		"actors",
		"documents",
		"document_revisions",
		"boards",
		"cards",
		"card_versions",
		"board_cards",
		"derived_inbox_items",
		"derived_thread_views",
		"derived_thread_dirty_queue",
		"thread_projection_refresh_status",
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
	if err := second.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = 1`).Scan(&migrationCount); err != nil {
		t.Fatalf("count schema migration rows: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected exactly one schema_migrations row for version 1, got %d", migrationCount)
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
		`INSERT INTO thread_projection_refresh_status(
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

	entries, err := store.ListDerivedThreadProjectionDirtyEntries(ctx, 10)
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

	stats, err := store.GetDerivedThreadProjectionQueueStats(ctx)
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
