package primitives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"agent-nexus-core/internal/blob"
)

const blobUsageTotalsRowID = 1

type blobLedgerWritePlan struct {
	contentHash    string
	sizeBytes      int64
	ledgerPresent  bool
	backendPresent bool
}

type BlobUsageLedgerRebuildResult struct {
	CanonicalHashes    int64  `json:"canonical_hash_count"`
	MissingBlobObjects int64  `json:"missing_blob_objects"`
	BlobBytes          int64  `json:"blob_bytes"`
	BlobObjects        int64  `json:"blob_objects"`
	RebuiltAt          string `json:"rebuilt_at"`
}

func (p blobLedgerWritePlan) growthBytes() int64 {
	if strings.TrimSpace(p.contentHash) == "" || p.ledgerPresent || p.backendPresent {
		return 0
	}
	return p.sizeBytes
}

func (p blobLedgerWritePlan) needsLedgerInsert() bool {
	return strings.TrimSpace(p.contentHash) != "" && !p.ledgerPresent
}

func (s *Store) prepareBlobLedgerWritePlan(ctx context.Context, contentHash string, uploadBytes int64) (blobLedgerWritePlan, error) {
	contentHash = strings.TrimSpace(contentHash)
	if contentHash == "" {
		return blobLedgerWritePlan{}, nil
	}
	if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
		return blobLedgerWritePlan{}, err
	}

	sizeBytes, found, err := s.lookupBlobLedgerEntry(ctx, contentHash)
	if err != nil {
		return blobLedgerWritePlan{}, err
	}
	if found {
		return blobLedgerWritePlan{
			contentHash:   contentHash,
			sizeBytes:     sizeBytes,
			ledgerPresent: true,
		}, nil
	}

	if s.blob == nil {
		return blobLedgerWritePlan{}, fmt.Errorf("blob backend is not configured")
	}

	stat, err := s.blob.Stat(ctx, contentHash)
	if err == nil {
		return blobLedgerWritePlan{
			contentHash:    contentHash,
			sizeBytes:      stat.Bytes,
			backendPresent: true,
		}, nil
	}
	if !errors.Is(err, blob.ErrBlobNotFound) {
		return blobLedgerWritePlan{}, fmt.Errorf("check blob existence: %w", err)
	}

	return blobLedgerWritePlan{
		contentHash: contentHash,
		sizeBytes:   uploadBytes,
	}, nil
}

func (s *Store) ensureBlobUsageLedgerInitialized(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return fmt.Errorf("blob backend is not configured")
	}

	var present int
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT 1 FROM blob_usage_totals WHERE id = ?`,
		blobUsageTotalsRowID,
	).Scan(&present); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("query blob usage totals: %w", err)
	}

	_, err := s.rebuildBlobUsageLedger(ctx)
	return err
}

func (s *Store) lookupBlobLedgerEntry(ctx context.Context, contentHash string) (int64, bool, error) {
	var sizeBytes int64
	err := s.db.QueryRowContext(
		ctx,
		`SELECT size_bytes FROM blob_usage_ledger WHERE content_hash = ?`,
		contentHash,
	).Scan(&sizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("query blob usage ledger entry: %w", err)
	}
	return sizeBytes, true, nil
}

func (s *Store) loadBlobUsageTotals(ctx context.Context) (blob.Usage, error) {
	if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
		return blob.Usage{}, err
	}

	var usage blob.Usage
	err := s.db.QueryRowContext(
		ctx,
		`SELECT blob_bytes, blob_objects FROM blob_usage_totals WHERE id = ?`,
		blobUsageTotalsRowID,
	).Scan(&usage.Bytes, &usage.Objects)
	if errors.Is(err, sql.ErrNoRows) {
		return blob.Usage{}, fmt.Errorf("blob usage totals are not initialized")
	}
	if err != nil {
		return blob.Usage{}, fmt.Errorf("query blob usage totals: %w", err)
	}

	return usage, nil
}

func (s *Store) applyBlobLedgerWritePlanTx(ctx context.Context, tx *sql.Tx, plan blobLedgerWritePlan) error {
	if !plan.needsLedgerInsert() {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(
		ctx,
		`INSERT INTO blob_usage_ledger(content_hash, size_bytes, created_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(content_hash) DO NOTHING`,
		plan.contentHash,
		plan.sizeBytes,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("insert blob usage ledger entry: %w", err)
	}

	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read blob usage ledger insert result: %w", err)
	}
	if inserted == 0 {
		return nil
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE blob_usage_totals
		    SET blob_bytes = blob_bytes + ?,
		        blob_objects = blob_objects + 1,
		        updated_at = ?
		  WHERE id = ?`,
		plan.sizeBytes,
		now,
		blobUsageTotalsRowID,
	); err != nil {
		return fmt.Errorf("update blob usage totals: %w", err)
	}

	return nil
}

func (s *Store) removeBlobLedgerEntryTx(ctx context.Context, tx *sql.Tx, contentHash string) error {
	contentHash = strings.TrimSpace(contentHash)
	if contentHash == "" {
		return nil
	}

	var sizeBytes int64
	err := tx.QueryRowContext(
		ctx,
		`SELECT size_bytes FROM blob_usage_ledger WHERE content_hash = ?`,
		contentHash,
	).Scan(&sizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query blob ledger entry for removal: %w", err)
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM blob_usage_ledger WHERE content_hash = ?`, contentHash)
	if err != nil {
		return fmt.Errorf("delete blob usage ledger entry: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("read blob ledger delete rows: %w", err)
	}
	if n == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE blob_usage_totals
		    SET blob_bytes = blob_bytes - ?,
		        blob_objects = blob_objects - 1,
		        updated_at = ?
		  WHERE id = ?`,
		sizeBytes,
		now,
		blobUsageTotalsRowID,
	); err != nil {
		return fmt.Errorf("decrement blob usage totals: %w", err)
	}

	return nil
}

func (s *Store) RebuildBlobUsageLedger(ctx context.Context) (BlobUsageLedgerRebuildResult, error) {
	if s == nil || s.db == nil {
		return BlobUsageLedgerRebuildResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return BlobUsageLedgerRebuildResult{}, fmt.Errorf("blob backend is not configured")
	}
	return s.rebuildBlobUsageLedger(ctx)
}

func (s *Store) rebuildBlobUsageLedger(ctx context.Context) (BlobUsageLedgerRebuildResult, error) {
	hashes, err := s.listCanonicalBlobHashes(ctx)
	if err != nil {
		return BlobUsageLedgerRebuildResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BlobUsageLedgerRebuildResult{}, fmt.Errorf("begin blob usage ledger rebuild transaction: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM blob_usage_ledger`); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BlobUsageLedgerRebuildResult{}, fmt.Errorf("clear blob usage ledger: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM blob_usage_totals WHERE id = ?`, blobUsageTotalsRowID); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BlobUsageLedgerRebuildResult{}, fmt.Errorf("clear blob usage totals: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	var (
		blobBytes          int64
		blobObjects        int64
		missingBlobObjects int64
	)
	for _, contentHash := range hashes {
		stat, err := s.blob.Stat(ctx, contentHash)
		if errors.Is(err, blob.ErrBlobNotFound) {
			missingBlobObjects++
			continue
		}
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BlobUsageLedgerRebuildResult{}, fmt.Errorf("inspect blob %s during rebuild: %w", contentHash, err)
		}

		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO blob_usage_ledger(content_hash, size_bytes, created_at, updated_at)
			 VALUES (?, ?, ?, ?)`,
			contentHash,
			stat.Bytes,
			now,
			now,
		); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BlobUsageLedgerRebuildResult{}, fmt.Errorf("insert blob usage ledger rebuild entry: %w", err)
		}
		blobBytes += stat.Bytes
		blobObjects++
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO blob_usage_totals(id, blob_bytes, blob_objects, rebuilt_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		blobUsageTotalsRowID,
		blobBytes,
		blobObjects,
		now,
		now,
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BlobUsageLedgerRebuildResult{}, fmt.Errorf("insert blob usage totals rebuild row: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return BlobUsageLedgerRebuildResult{}, fmt.Errorf("commit blob usage ledger rebuild transaction: %w", err)
	}

	return BlobUsageLedgerRebuildResult{
		CanonicalHashes:    int64(len(hashes)),
		MissingBlobObjects: missingBlobObjects,
		BlobBytes:          blobBytes,
		BlobObjects:        blobObjects,
		RebuiltAt:          now,
	}, nil
}

func (s *Store) listCanonicalBlobHashes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT DISTINCT content_hash
		   FROM artifacts
		  WHERE TRIM(COALESCE(content_hash, '')) != ''
		  ORDER BY content_hash ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query canonical blob hashes: %w", err)
	}
	defer rows.Close()

	hashes := make([]string, 0)
	for rows.Next() {
		var contentHash string
		if err := rows.Scan(&contentHash); err != nil {
			return nil, fmt.Errorf("scan canonical blob hash: %w", err)
		}
		hashes = append(hashes, contentHash)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate canonical blob hashes: %w", err)
	}

	return hashes, nil
}
