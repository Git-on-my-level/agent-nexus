package storage_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
		"snapshots",
		"artifacts",
		"actors",
		"derived_views",
		"derived_inbox_items",
		"derived_thread_views",
		"thread_projection_refresh_status",
		"agents",
		"agent_keys",
		"auth_refresh_sessions",
		"auth_access_tokens",
		"auth_used_assertions",
		"auth_bootstrap_state",
		"auth_invites",
		"auth_audit_events",
		"boards",
		"board_cards",
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

func TestWorkspaceMigrationRemovesArtifactContentPathAndPreservesHashReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspaceRoot := t.TempDir()
	layout := storage.NewLayout(workspaceRoot)
	if err := os.MkdirAll(layout.ArtifactContentDir, 0o755); err != nil {
		t.Fatalf("create artifact content dir: %v", err)
	}

	legacyDB, err := sql.Open("sqlite", "file:"+layout.DatabasePath)
	if err != nil {
		t.Fatalf("open legacy sqlite database: %v", err)
	}

	evidenceContent := []byte("legacy artifact content")
	evidenceHash := sha256Hex(evidenceContent)
	evidencePath := filepath.Join(layout.ArtifactContentDir, evidenceHash)
	if err := os.WriteFile(evidencePath, evidenceContent, 0o644); err != nil {
		t.Fatalf("write legacy artifact content: %v", err)
	}

	documentContent := []byte("legacy document revision")
	documentHash := sha256Hex(documentContent)
	documentPath := filepath.Join(layout.ArtifactContentDir, documentHash)
	if err := os.WriteFile(documentPath, documentContent, 0o644); err != nil {
		t.Fatalf("write legacy document content: %v", err)
	}

	if err := seedLegacyWorkspace(ctx, legacyDB, evidenceHash, evidencePath, documentHash, documentPath); err != nil {
		t.Fatalf("seed legacy workspace: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy sqlite database: %v", err)
	}

	workspace, err := storage.InitializeWorkspace(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("initialize migrated workspace: %v", err)
	}
	defer workspace.Close()

	assertArtifactColumnAbsent(t, workspace.DB(), "content_path")
	assertArtifactMetadataScrubbed(t, workspace.DB(), "artifact-legacy")
	assertArtifactMetadataScrubbed(t, workspace.DB(), "artifact-doc-legacy")

	store := primitives.NewStore(
		workspace.DB(),
		blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir),
		workspace.Layout().ArtifactContentDir,
	)

	artifact, err := store.GetArtifact(ctx, "artifact-legacy")
	if err != nil {
		t.Fatalf("get migrated artifact: %v", err)
	}
	if _, ok := artifact["content_path"]; ok {
		t.Fatalf("expected migrated artifact metadata to omit content_path: %#v", artifact)
	}
	if artifact["content_hash"] != evidenceHash {
		t.Fatalf("unexpected migrated artifact content_hash: %#v", artifact["content_hash"])
	}

	content, contentType, err := store.GetArtifactContent(ctx, "artifact-legacy")
	if err != nil {
		t.Fatalf("get migrated artifact content: %v", err)
	}
	if contentType != "text" {
		t.Fatalf("unexpected migrated artifact content_type: %q", contentType)
	}
	if string(content) != string(evidenceContent) {
		t.Fatalf("unexpected migrated artifact content: %q", string(content))
	}

	revision, err := store.GetDocumentRevision(ctx, "legacy-doc", "rev-legacy")
	if err != nil {
		t.Fatalf("get migrated document revision: %v", err)
	}
	if revision["content_hash"] != documentHash {
		t.Fatalf("unexpected migrated revision content_hash: %#v", revision["content_hash"])
	}
	artifactMeta, ok := revision["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("expected revision artifact metadata map, got %#v", revision["artifact"])
	}
	if _, ok := artifactMeta["content_path"]; ok {
		t.Fatalf("expected migrated revision artifact metadata to omit content_path: %#v", artifactMeta)
	}
	if revision["content"] != string(documentContent) {
		t.Fatalf("unexpected migrated revision content: %#v", revision["content"])
	}
}

func TestWorkspaceMigrationBackfillsProjectionGenerationsConservatively(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspaceRoot := t.TempDir()
	layout := storage.NewLayout(workspaceRoot)
	if err := os.MkdirAll(layout.RootDir, 0o755); err != nil {
		t.Fatalf("create workspace root: %v", err)
	}

	legacyDB, err := sql.Open("sqlite", "file:"+layout.DatabasePath)
	if err != nil {
		t.Fatalf("open legacy sqlite database: %v", err)
	}

	statements := []string{
		`CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);`,
		`CREATE TABLE derived_thread_dirty_queue (
			thread_id TEXT PRIMARY KEY,
			dirty_at TEXT NOT NULL
		);`,
		`CREATE TABLE derived_thread_views (
			thread_id TEXT PRIMARY KEY
		);`,
		`CREATE TABLE thread_projection_refresh_status (
			thread_id TEXT PRIMARY KEY,
			is_dirty INTEGER NOT NULL DEFAULT 0,
			in_progress INTEGER NOT NULL DEFAULT 0,
			queued_at TEXT,
			started_at TEXT,
			completed_at TEXT,
			last_error_at TEXT,
			last_error TEXT,
			updated_at TEXT NOT NULL
		);`,
	}
	for _, statement := range statements {
		if _, err := legacyDB.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
	for version := 1; version <= 19; version++ {
		if _, err := legacyDB.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, version, "2026-03-04T00:00:00Z"); err != nil {
			t.Fatalf("seed schema_migrations: %v", err)
		}
	}

	for _, threadID := range []string{"current-thread", "dirty-thread", "inflight-thread"} {
		if _, err := legacyDB.ExecContext(ctx, `INSERT INTO derived_thread_views(thread_id) VALUES (?)`, threadID); err != nil {
			t.Fatalf("seed derived_thread_views: %v", err)
		}
	}

	if _, err := legacyDB.ExecContext(
		ctx,
		`INSERT INTO thread_projection_refresh_status(
			thread_id, is_dirty, in_progress, queued_at, started_at, completed_at, updated_at
		) VALUES
			('current-thread', 0, 0, NULL, '2026-03-04T10:00:00Z', '2026-03-04T10:01:00Z', '2026-03-04T10:01:00Z'),
			('dirty-thread', 1, 0, '2026-03-04T10:02:00Z', NULL, '2026-03-04T10:01:00Z', '2026-03-04T10:02:00Z'),
			('inflight-thread', 0, 1, NULL, '2026-03-04T10:03:00Z', '2026-03-04T10:01:00Z', '2026-03-04T10:03:00Z')`,
	); err != nil {
		t.Fatalf("seed legacy thread projection refresh status: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy sqlite database: %v", err)
	}

	workspace, err := storage.InitializeWorkspace(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("initialize migrated workspace: %v", err)
	}
	defer workspace.Close()

	statuses := primitives.NewStore(
		workspace.DB(),
		blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir),
		workspace.Layout().ArtifactContentDir,
	)
	got, err := statuses.GetThreadProjectionRefreshStatuses(ctx, []string{"current-thread", "dirty-thread", "inflight-thread"})
	if err != nil {
		t.Fatalf("load migrated refresh statuses: %v", err)
	}

	if status := got["current-thread"]; status.DesiredGeneration != 1 || status.MaterializedGeneration != 1 || status.IsDirty() {
		t.Fatalf("expected current-thread to remain current after migration, got %#v", status)
	}

	if status := got["dirty-thread"]; status.DesiredGeneration != 1 || status.MaterializedGeneration != 0 || !status.IsDirty() || status.InProgressGeneration != nil {
		t.Fatalf("expected dirty-thread to remain pending after migration, got %#v", status)
	}

	if status := got["inflight-thread"]; status.DesiredGeneration != 1 || status.MaterializedGeneration != 0 || !status.IsDirty() || status.InProgressGeneration == nil || *status.InProgressGeneration != 1 {
		t.Fatalf("expected inflight-thread to remain pending after migration, got %#v", status)
	}

	var queueEntries int
	if err := workspace.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM derived_thread_dirty_queue WHERE thread_id IN ('dirty-thread', 'inflight-thread')`).Scan(&queueEntries); err != nil {
		t.Fatalf("count migrated dirty queue entries: %v", err)
	}
	if queueEntries != 2 {
		t.Fatalf("expected dirty/inflight threads to be requeued after migration, got %d entries", queueEntries)
	}

	assertColumnPresent(t, workspace.DB(), "thread_projection_refresh_status", "desired_generation")
	assertColumnPresent(t, workspace.DB(), "thread_projection_refresh_status", "materialized_generation")
	assertColumnPresent(t, workspace.DB(), "thread_projection_refresh_status", "in_progress_generation")
	assertColumnAbsent(t, workspace.DB(), "thread_projection_refresh_status", "is_dirty")
	assertColumnAbsent(t, workspace.DB(), "thread_projection_refresh_status", "in_progress")
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

func seedLegacyWorkspace(ctx context.Context, db *sql.DB, evidenceHash string, evidencePath string, documentHash string, documentPath string) error {
	statements := []string{
		`CREATE TABLE schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		);`,
		`CREATE TABLE artifacts (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			thread_id TEXT,
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL,
			content_type TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			content_path TEXT NOT NULL,
			refs_json TEXT NOT NULL DEFAULT '[]',
			metadata_json TEXT NOT NULL DEFAULT '{}',
			tombstoned_at TEXT,
			tombstoned_by TEXT,
			tombstone_reason TEXT
		);`,
		`CREATE TABLE document_revisions (
			revision_id TEXT PRIMARY KEY,
			document_id TEXT NOT NULL,
			revision_number INTEGER NOT NULL,
			prev_revision_id TEXT,
			artifact_id TEXT NOT NULL,
			thread_id TEXT,
			refs_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL,
			revision_hash TEXT NOT NULL DEFAULT '',
			UNIQUE(document_id, revision_number)
		);`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for version := 1; version <= 16; version++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, version, "2026-03-04T00:00:00Z"); err != nil {
			return err
		}
	}

	evidenceMetadata, err := json.Marshal(map[string]any{
		"id":           "artifact-legacy",
		"kind":         "evidence",
		"created_at":   "2026-03-04T10:00:00Z",
		"created_by":   "actor-1",
		"content_type": "text",
		"content_hash": evidenceHash,
		"content_path": evidencePath,
		"refs":         []string{"thread:thread-legacy"},
		"summary":      "legacy artifact",
	})
	if err != nil {
		return err
	}
	documentMetadata, err := json.Marshal(map[string]any{
		"id":               "artifact-doc-legacy",
		"kind":             "doc",
		"created_at":       "2026-03-04T11:00:00Z",
		"created_by":       "actor-1",
		"content_type":     "text",
		"content_hash":     documentHash,
		"content_path":     documentPath,
		"refs":             []string{"thread:thread-legacy"},
		"document_id":      "legacy-doc",
		"revision_id":      "rev-legacy",
		"revision_number":  1,
		"prev_revision_id": nil,
		"summary":          "legacy document revision",
	})
	if err != nil {
		return err
	}

	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO artifacts(
			id, kind, thread_id, created_at, created_by, content_type, content_hash, content_path, refs_json, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"artifact-legacy",
		"evidence",
		"thread-legacy",
		"2026-03-04T10:00:00Z",
		"actor-1",
		"text",
		evidenceHash,
		evidencePath,
		`["thread:thread-legacy"]`,
		string(evidenceMetadata),
	); err != nil {
		return err
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO artifacts(
			id, kind, thread_id, created_at, created_by, content_type, content_hash, content_path, refs_json, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"artifact-doc-legacy",
		"doc",
		"thread-legacy",
		"2026-03-04T11:00:00Z",
		"actor-1",
		"text",
		documentHash,
		documentPath,
		`["thread:thread-legacy"]`,
		string(documentMetadata),
	); err != nil {
		return err
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO document_revisions(
			revision_id, document_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, created_at, created_by, revision_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"rev-legacy",
		"legacy-doc",
		1,
		nil,
		"artifact-doc-legacy",
		"thread-legacy",
		`["thread:thread-legacy"]`,
		"2026-03-04T11:00:00Z",
		"actor-1",
		"legacy-revision-hash",
	); err != nil {
		return err
	}

	return nil
}

func assertArtifactColumnAbsent(t *testing.T, db *sql.DB, columnName string) {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(artifacts)`)
	if err != nil {
		t.Fatalf("query artifacts table_info: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			typeName   string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultVal, &primaryKey); err != nil {
			t.Fatalf("scan artifacts table_info: %v", err)
		}
		if name == columnName {
			t.Fatalf("expected artifacts.%s to be removed", columnName)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate artifacts table_info: %v", err)
	}
}

func assertArtifactMetadataScrubbed(t *testing.T, db *sql.DB, artifactID string) {
	t.Helper()

	var metadataJSON string
	if err := db.QueryRow(`SELECT metadata_json FROM artifacts WHERE id = ?`, artifactID).Scan(&metadataJSON); err != nil {
		t.Fatalf("load artifact metadata_json for %s: %v", artifactID, err)
	}

	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		t.Fatalf("decode artifact metadata_json for %s: %v", artifactID, err)
	}
	if _, ok := metadata["content_path"]; ok {
		t.Fatalf("expected artifact %s metadata_json to omit content_path: %#v", artifactID, metadata)
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

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
