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
	RootDir      string
	DatabasePath string
	LogsDir      string
	TmpDir       string
}

type Workspace struct {
	layout Layout
	db     *sql.DB
}

func NewLayout(root string) Layout {
	cleanRoot := filepath.Clean(root)
	return Layout{
		RootDir:      cleanRoot,
		DatabasePath: filepath.Join(cleanRoot, "state.sqlite"),
		LogsDir:      filepath.Join(cleanRoot, "logs"),
		TmpDir:       filepath.Join(cleanRoot, "tmp"),
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
	for _, dir := range []string{layout.RootDir, layout.LogsDir, layout.TmpDir} {
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
		return errors.New("control-plane database is not initialized")
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
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	dsn.RawQuery = query.Encode()
	return dsn.String()
}
