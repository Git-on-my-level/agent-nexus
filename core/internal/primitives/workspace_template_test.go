package primitives_test

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"context"
)

func initializeTestWorkspace(ctx context.Context, root string) (*storage.Workspace, error) {
	return primitives.InitializeTestWorkspace(ctx, root)
}
func cleanupTestWorkspace() error { return primitives.CleanupTestWorkspace() }
