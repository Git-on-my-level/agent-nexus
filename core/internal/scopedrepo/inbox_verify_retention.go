package scopedrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"agent-nexus-core/internal/scopes"
)

type InboxVerificationPruneProgress struct {
	Deleted  int
	Complete bool
}

// PruneInboxVerification removes at most 64 rows from ONE terminal job per
// transaction. Its index seek and each child-table delete are job-prefix
// bounded; no cascade walks a generation. A live lease refuses pruning, and an
// unfinished crash checkpoint is retained until explicitly cancelled/resumed.
// The caller's trusted retention cutoff cannot select serving/shadow data.
// No startup or HTTP code calls this disabled maintenance operation.
func (s *Store) PruneInboxVerification(ctx context.Context, before time.Time, limit int) (InboxVerificationPruneProgress, error) {
	var out InboxVerificationPruneProgress
	if s == nil || s.db == nil || before.IsZero() || before.UnixMilli() < 1 || limit < 1 || limit > InboxVerificationSliceLimit {
		return out, scopes.ErrBudget
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tx, cleanup, err := beginInboxVerificationChunk(ctx, s.db)
	if err != nil {
		return out, err
	}
	defer cleanup()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM scope_inbox_verification_jobs WHERE finished_at>0 AND finished_at<=? ORDER BY finished_at,id LIMIT 1`, before.UnixMilli()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		out.Complete = true
		return out, nil
	}
	if err != nil {
		return out, err
	}
	// Mark as retired under the same write lock as deletion. Any detached worker
	// is refused even if deletion spans many chunks and its old lease was expired.
	err = inboxVerificationAffected(tx.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET phase='pruning',failure='cancelled',lease_owner='',lease_until=0
 WHERE id=? AND finished_at>0 AND finished_at<=? AND lease_until<=`+inboxVerificationNowSQL, id, before.UnixMilli()))
	if err != nil {
		return out, err
	}
	// Exact internal statements. Each table's PK begins with job_id, so the
	// bounded subquery cannot examine another principal's temporary rows.
	queries := []string{
		`DELETE FROM scope_inbox_verification_scopes WHERE (job_id,scope_id) IN (SELECT job_id,scope_id FROM scope_inbox_verification_scopes WHERE job_id=? LIMIT ?)`,
		`DELETE FROM scope_inbox_verification_streams WHERE (job_id,scope_id,audience_key) IN (SELECT job_id,scope_id,audience_key FROM scope_inbox_verification_streams WHERE job_id=? LIMIT ?)`,
		`DELETE FROM scope_inbox_verification_seen WHERE (job_id,rid) IN (SELECT job_id,rid FROM scope_inbox_verification_seen WHERE job_id=? LIMIT ?)`,
		`DELETE FROM scope_inbox_verification_counts WHERE (job_id,scope_id,audience_key,bucket) IN (SELECT job_id,scope_id,audience_key,bucket FROM scope_inbox_verification_counts WHERE job_id=? LIMIT ?)`,
	}
	for _, query := range queries {
		result, e := tx.ExecContext(ctx, query, id, limit-out.Deleted)
		if e != nil {
			return InboxVerificationPruneProgress{}, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return InboxVerificationPruneProgress{}, e
		}
		if n < 0 || n > int64(limit-out.Deleted) {
			return InboxVerificationPruneProgress{}, ErrInboxVerificationMismatch
		}
		out.Deleted += int(n)
		if out.Deleted == limit {
			return commitInboxVerificationPrune(tx, out)
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM scope_inbox_comparison_receipts WHERE job_id=?`, id)
	if err != nil {
		return InboxVerificationPruneProgress{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return InboxVerificationPruneProgress{}, err
	}
	if n < 0 || n > 1 {
		return InboxVerificationPruneProgress{}, ErrInboxVerificationMismatch
	}
	out.Deleted += int(n)
	if out.Deleted == limit {
		return commitInboxVerificationPrune(tx, out)
	}
	if err = inboxVerificationAffected(tx.ExecContext(ctx, `DELETE FROM scope_inbox_verification_jobs WHERE id=? AND phase='pruning' AND lease_until=0`, id)); err != nil {
		return InboxVerificationPruneProgress{}, err
	}
	out.Deleted++
	// Complete means this job is gone, not an assertion that no later terminal
	// jobs exist. A subsequent empty seek reports Deleted=0,Complete=true.
	out.Complete = true
	return commitInboxVerificationPrune(tx, out)
}

func commitInboxVerificationPrune(tx *sql.Tx, out InboxVerificationPruneProgress) (InboxVerificationPruneProgress, error) {
	if err := tx.Commit(); err != nil {
		return InboxVerificationPruneProgress{}, err
	}
	return out, nil
}
