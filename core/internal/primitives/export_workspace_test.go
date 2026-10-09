// Fully migrated, isolated SQLite fixtures for this test package.
// It is test infrastructure only; migration/upgrade tests must use storage directly.
package primitives

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"agent-nexus-core/internal/storage"
)

var template struct {
	once sync.Once
	root string
	data []byte
	err  error
}

// InitializeWorkspace copies an immutable database after its last connection
// has closed (and checkpointed WAL). Each copy gets its own blobs, WAL and
// serving lock. Reopening uses the production driver and readiness checks.
// The destination must be a fresh test directory, never a database under test.
func initializeTestWorkspace(ctx context.Context, root string) (*storage.Workspace, error) {
	template.once.Do(func() {
		template.root, template.err = os.MkdirTemp("", "anx-test-template-")
		if template.err != nil {
			return
		}
		// A cancelled test must not poison every later fixture in this process.
		ws, err := storage.InitializeWorkspace(context.Background(), template.root)
		if err != nil {
			template.err = err
			return
		}
		if err = ws.Close(); err != nil {
			template.err = err
			return
		}
		template.data, template.err = os.ReadFile(ws.Layout().DatabasePath)
	})
	if template.err != nil {
		return nil, fmt.Errorf("migrate test template: %w", template.err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(root, "state.sqlite")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("create isolated test database: %w", err)
	}
	_, writeErr := file.Write(template.data)
	closeErr := file.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return storage.InitializeWorkspace(ctx, root)
}

// Cleanup runs from TestMain after all tests and their cleanup callbacks finish.
func cleanupTestWorkspace() error {
	if template.root == "" {
		return nil
	}
	return os.RemoveAll(template.root)
}

// Export the fixture to the external black-box test package only.
func InitializeTestWorkspace(ctx context.Context, root string) (*storage.Workspace, error) {
	return initializeTestWorkspace(ctx, root)
}
func CleanupTestWorkspace() error { return cleanupTestWorkspace() }
