package storage

import (
	"agent-nexus-core/internal/contentusage"
	"context"
)

// InitializeUsageTracking covers optional module tables before workers start.
func (w *Workspace) InitializeUsageTracking(ctx context.Context) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = contentusage.Install(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
