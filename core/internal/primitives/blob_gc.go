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
// Purges therefore commit their row deletions first and then call this. Each
// hash gets its own short write transaction that recounts, removes the ledger
// entry and deletes the object before committing. That transaction holds the
// SQLite write lock (shared by every process that opens the workspace) from its
// first statement until the delete is done. Every creator promotes (or, after
// a preload, confirms and if needed restores) its staged blob inside the write
// transaction that commits its reference (see blob_preload.go), so:
//   - a creator that committed before the collection is counted, and the blob
//     is kept;
//   - a creator that commits after it finds the object gone when it promotes
//     and writes its own copy.
//
// One hash per transaction bounds how long the write lock is held to a single
// object delete, so a purge of many revisions on a remote object store does not
// stall other writers past their busy timeout.
//
// The object is deleted before the ledger change commits. A failed delete rolls
// back, and a failed commit after a delete leaves only a ledger entry for an
// unreferenced blob. Neither leaves a committed reference to a missing blob. A
// failure on one hash does not stop collection of the others.
func (s *Store) collectUnreferencedBlobs(ctx context.Context, hashes []string) error {
	candidates := uniqueSortedStrings(hashes)
	if len(candidates) == 0 {
		return nil
	}
	if s.blobCollectHook != nil {
		s.blobCollectHook(candidates)
	}
	var errs []error
	for _, contentHash := range candidates {
		if err := s.collectUnreferencedBlob(ctx, contentHash); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Store) collectUnreferencedBlob(ctx context.Context, contentHash string) error {
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

	var cnt int
	if err := tx.QueryRowContext(CanonicalMaintenanceContext(ctx), `SELECT COUNT(*) FROM artifacts WHERE content_hash = ?`, contentHash).Scan(&cnt); err != nil {
		return fmt.Errorf("count artifact blob references: %w", err)
	}
	if cnt != 0 {
		return nil
	}
	if err := s.removeBlobLedgerEntryTx(ctx, tx, contentHash); err != nil {
		return err
	}
	if s.blobCollectLockedHook != nil {
		s.blobCollectLockedHook(contentHash)
	}
	if s.blob != nil {
		if err := s.blob.Delete(ctx, contentHash); err != nil && !errors.Is(err, blob.ErrBlobNotFound) {
			return fmt.Errorf("delete blob object: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit blob collection transaction: %w", err)
	}
	return nil
}
