package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Layout struct {
	RootDir            string
	DatabasePath       string
	ArtifactsDir       string
	ArtifactContentDir string
	LogsDir            string
	TmpDir             string
}

type Workspace struct {
	layout Layout
	db     *sql.DB
}

func NewLayout(root string) Layout {
	cleanRoot := filepath.Clean(root)
	artifactsDir := filepath.Join(cleanRoot, "artifacts")

	return Layout{
		RootDir:            cleanRoot,
		DatabasePath:       filepath.Join(cleanRoot, "state.sqlite"),
		ArtifactsDir:       artifactsDir,
		ArtifactContentDir: filepath.Join(artifactsDir, "content"),
		LogsDir:            filepath.Join(cleanRoot, "logs"),
		TmpDir:             filepath.Join(cleanRoot, "tmp"),
	}
}

func InitializeWorkspace(ctx context.Context, workspaceRoot string) (*Workspace, error) {
	layout := NewLayout(workspaceRoot)
	if err := ensureLayout(layout); err != nil {
		return nil, err
	}

	databasePath, err := filepath.Abs(layout.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite database path: %w", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(databasePath))
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	if err := applyMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Workspace{layout: layout, db: db}, nil
}

func ensureLayout(layout Layout) error {
	dirs := []string{
		layout.RootDir,
		layout.ArtifactsDir,
		layout.ArtifactContentDir,
		layout.LogsDir,
		layout.TmpDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create workspace directory %q: %w", dir, err)
		}
	}

	return nil
}

func (w *Workspace) Layout() Layout {
	return w.layout
}

func (w *Workspace) DB() *sql.DB {
	return w.db
}

func (w *Workspace) Ping(ctx context.Context) error {
	if w == nil || w.db == nil {
		return errors.New("workspace database is not initialized")
	}
	return w.db.PingContext(ctx)
}

func (w *Workspace) Close() error {
	if w == nil || w.db == nil {
		return nil
	}
	return w.db.Close()
}

func sqliteDSN(databasePath string) string {
	dsn := &url.URL{
		Scheme: "file",
		Path:   databasePath,
	}
	query := dsn.Query()
	// Dev and multi-writer paths (projection maintenance + API) can contend on SQLite;
	// a longer busy wait reduces spurious "database is locked" under bursty local traffic.
	query.Add("_pragma", "busy_timeout(20000)")
	query.Add("_pragma", "journal_mode(WAL)")
	// Every transaction here writes, and a deferred one that reads first fails
	// SQLITE_BUSY outright when it upgrades to a write -- busy_timeout does not
	// cover a lock upgrade, because retrying it could deadlock. BEGIN IMMEDIATE
	// takes the write lock up front, where busy_timeout does apply, so
	// concurrent writers queue instead of erroring with "database is locked".
	query.Set("_txlock", "immediate")
	dsn.RawQuery = query.Encode()
	return dsn.String()
}
