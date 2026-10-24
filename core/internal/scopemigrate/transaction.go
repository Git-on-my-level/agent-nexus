package scopemigrate

import (
	"agent-nexus-core/internal/sqliteutil"
	"context"
	"database/sql"
	"errors"
)

// Preserve the migration worker entry point while sharing its pinned,
// contention-safe transaction with canonical projection maintenance.
func beginChunk(ctx context.Context, db *sql.DB) (*sql.Tx, func(), error) {
	return sqliteutil.BeginMaintenanceChunk(ctx, db)
}

// budgetError reports a chunk that outlived its budget as the context's error.
// When the deadline interrupts SQLite, the failing statement may surface only
// "interrupted (9)"; the original error stays wrapped for diagnosis.
func budgetError(ctx context.Context, err error) error {
	if err == nil || ctx.Err() == nil || errors.Is(err, ctx.Err()) {
		return err
	}
	return errors.Join(ctx.Err(), err)
}
