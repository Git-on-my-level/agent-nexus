package scopemigrate

import (
	"agent-nexus-core/internal/sqliteutil"
	"context"
	"database/sql"
)

// Preserve the migration worker entry point while sharing its pinned,
// contention-safe transaction with canonical projection maintenance.
func beginChunk(ctx context.Context, db *sql.DB) (*sql.Tx, func(), error) {
	return sqliteutil.BeginMaintenanceChunk(ctx, db)
}
