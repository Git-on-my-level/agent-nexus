package storage

import (
	"context"
	"database/sql"

	"agent-nexus-core/internal/resourceaccess"
)

// Repair existing v61 installations as well as fresh workspaces. SQL bodies
// are backfilled without touching artifact manifests or fetching blob content.
func repairResourceAccessReadIndexes(ctx context.Context, tx *sql.Tx) error {
	for _, source := range resourceaccess.OwnershipSources {
		if source.Kind == "inbox" {
			if err := installResourceAccessSourceEdges(ctx, tx, []resourceaccess.OwnershipSource{source}); err != nil {
				return err
			}
		}
	}
	if err := installResourceAccessRevisionHandles(ctx, tx); err != nil {
		return err
	}
	return resourceaccess.InstallPMAccess(ctx, tx, true)
}
