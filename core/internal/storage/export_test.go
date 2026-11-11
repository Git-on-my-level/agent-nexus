package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"testing"
)

// OpenWorkspaceWithDriverForTest mirrors InitializeWorkspace but lets an
// external test wrap the registered driver beneath the production scope
// connector. It is test-only and lives here because the storage package cannot
// import the HTTP server package without an import cycle.
func OpenWorkspaceWithDriverForTest(t *testing.T, root string, wrap func(driver.Driver) driver.Driver) *Workspace {
	t.Helper()
	layout := NewLayout(root)
	if err := ensureLayout(layout); err != nil {
		t.Fatal(err)
	}
	processLock := &scopeProcessLease{refs: 1}
	databasePath, err := filepath.Abs(layout.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	dsn := sqliteDSN(databasePath)
	registered, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	registeredDriver := registered.Driver()
	if err := registered.Close(); err != nil {
		t.Fatal(err)
	}
	if wrap != nil {
		registeredDriver = wrap(registeredDriver)
	}
	db := sql.OpenDB(&scopeConnector{registered: registeredDriver, dsn: dsn, lease: processLock})
	db.SetMaxIdleConns(8)
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := enableExpandedScopeLease(ctx, db, processLock, layout.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := enableCompactAccessLease(ctx, db, processLock, layout.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := applyWorkspaceMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := enableExpandedScopeLease(ctx, db, processLock, layout.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := reconcileInboxLifecyclePreview(ctx, db); err != nil {
		t.Fatal(err)
	}
	w := &Workspace{layout: layout, db: db, processLock: processLock}
	if err := w.initializeCompactAccessMetadata(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func SQLiteDSNForTest(databasePath string) string {
	return sqliteDSN(databasePath)
}

func LeaseRefsForTest(w *Workspace) int {
	w.processLock.mu.Lock()
	defer w.processLock.mu.Unlock()
	return w.processLock.refs
}
