package primitives

import (
	"context"
	"errors"
	"fmt"

	"agent-nexus-core/internal/blob"
)

// collectUnreferencedBlobs deletes each content-addressed blob in hashes that no
// committed artifact row references, and removes its usage ledger entry.
//
// Blobs are shared by every artifact with the same content hash, and creators
// reuse an existing blob object instead of storing it again. Counting references
// in one transaction and deleting the object after that transaction commits lets
// a creator reuse the object and commit its reference in between, leaving the
// new row pointing at a deleted blob.
//
// Purges therefore commit their row deletions first and then call this. The
// recount and the delete happen inside one write transaction, which holds the
// SQLite write lock (shared by every process that opens the workspace) from its
// first statement until the delete is done. Every creator promotes its staged
// blob either inside its own write transaction or after committing it, so:
//   - a creator that committed before this transaction is counted, and the blob
//     is kept;
//   - a creator that commits after it finds the object gone when it promotes
//     and writes its own copy.
//
// The object is deleted before the ledger change commits. A failed delete rolls
// back, and a failed commit after a delete leaves only a ledger entry for an
// unreferenced blob. Neither leaves a committed reference to a missing blob.
func (s *Store) collectUnreferencedBlobs(ctx context.Context, hashes []string) error {
	candidates := uniqueSortedStrings(hashes)
	if len(candidates) == 0 {
		return nil
	}
	if s.blobCollectHook != nil {
		s.blobCollectHook(candidates)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin blob collection transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Take the write lock before counting, even if the connection was opened
	// with deferred transactions; an unmatched UPDATE still acquires it.
	if _, err := tx.ExecContext(ctx, `UPDATE blob_usage_totals SET updated_at = updated_at WHERE id = ?`, blobUsageTotalsRowID); err != nil {
		return fmt.Errorf("lock blob collection: %w", err)
	}

	unreferenced := make([]string, 0, len(candidates))
	for _, contentHash := range candidates {
		var cnt int
		if err := tx.QueryRowContext(CanonicalMaintenanceContext(ctx), `SELECT COUNT(*) FROM artifacts WHERE content_hash = ?`, contentHash).Scan(&cnt); err != nil {
			return fmt.Errorf("count artifact blob references: %w", err)
		}
		if cnt != 0 {
			continue
		}
		if err := s.removeBlobLedgerEntryTx(ctx, tx, contentHash); err != nil {
			return err
		}
		unreferenced = append(unreferenced, contentHash)
	}

	if s.blob != nil {
		for _, contentHash := range unreferenced {
			if err := s.blob.Delete(ctx, contentHash); err != nil && !errors.Is(err, blob.ErrBlobNotFound) {
				return fmt.Errorf("delete blob object: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit blob collection transaction: %w", err)
	}
	return nil
}
