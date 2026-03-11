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
		"snapshots",
		"artifacts",
		"actors",
		"derived_views",
		"derived_inbox_items",
		"derived_thread_views",
		"agents",
		"agent_keys",
		"auth_refresh_sessions",
		"auth_access_tokens",
		"auth_used_assertions",
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
