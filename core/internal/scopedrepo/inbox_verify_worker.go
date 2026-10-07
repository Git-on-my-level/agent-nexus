package scopedrepo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"agent-nexus-core/internal/scopes"
)

var ErrInboxVerificationLease = errors.New("inbox comparison lease lost")
var ErrInboxVerificationCancelled = errors.New("inbox comparison cancelled")

// SQLite time, never caller time, decides lease ownership. Every takeover or
// renewal advances the integral token, so an old attempt cannot become valid
// again even when the process owner is unchanged.
const inboxVerificationNowSQL = `(CAST(strftime('%s','now') AS INTEGER)*1000+CAST(substr(strftime('%f','now'),4,3) AS INTEGER))`

// InboxVerificationWorkerOptions are trusted maintenance limits. The legacy
// oracle can construct a graph off-request: 64 enumeration rows is not a bound
// on that SQL's prepare/examined work. A deadline aborts a slice without advancing
// its checkpoint; it does not certify a CPU or HTTP latency budget.
type InboxVerificationWorkerOptions struct {
	Interval, SliceDuration, LeaseDuration time.Duration
}

func DefaultInboxVerificationWorkerOptions() InboxVerificationWorkerOptions {
	return InboxVerificationWorkerOptions{Interval: 100 * time.Millisecond, SliceDuration: 50 * time.Second, LeaseDuration: time.Minute}
}

func (o InboxVerificationWorkerOptions) validate() error {
	if o.Interval < time.Millisecond || o.Interval > time.Minute || o.SliceDuration <= 0 || o.SliceDuration > 50*time.Second || o.LeaseDuration < time.Second || o.LeaseDuration > time.Minute || o.LeaseDuration <= o.SliceDuration {
		return scopes.ErrBudget
	}
	return nil
}

type inboxVerificationRunner struct {
	store      *Store
	job, owner string
	options    InboxVerificationWorkerOptions
}

// Lease fields and the runner are private: consumers cannot submit a token,
// certificate, alternate transaction, computation or success callback.
type inboxVerificationLease struct{ token int64 }

func newInboxVerificationRunner(s *Store, job string, options InboxVerificationWorkerOptions) (*inboxVerificationRunner, error) {
	if s == nil || s.db == nil || !feedText(job, 64) {
		return nil, scopes.ErrBudget
	}
	if err := options.validate(); err != nil {
		return nil, err
	}
	owner, err := opaque()
	if err != nil {
		return nil, err
	}
	return &inboxVerificationRunner{store: s, job: job, owner: owner, options: options}, nil
}

// Pin the connection, disable its busy wait for this transaction, and restore
// the original timeout before returning it to serving. Contention pauses work
// instead of holding a writer beyond the operation context. A failed restoration
// discards the connection. This follows D's scope-migration transaction contract.
func beginInboxVerificationChunk(ctx context.Context, db *sql.DB) (*sql.Tx, func(), error) {
	return beginInboxVerificationTransaction(ctx, db, false)
}

func beginInboxVerificationTransaction(ctx context.Context, db *sql.DB, readOnly bool) (*sql.Tx, func(), error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	var timeout int
	if err = conn.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		conn.Close()
		return nil, nil, err
	}
	var tx *sql.Tx
	cleanup := func() {
		if tx != nil {
			_ = tx.Rollback()
		}
		restore, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, e := conn.ExecContext(restore, fmt.Sprintf("PRAGMA busy_timeout=%d", timeout)); e != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}
	if _, err = conn.ExecContext(ctx, `PRAGMA busy_timeout=0`); err != nil {
		cleanup()
		return nil, nil, err
	}
	tx, err = conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return tx, cleanup, nil
}

func (r *inboxVerificationRunner) acquireLease(ctx context.Context) (inboxVerificationLease, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tx, cleanup, err := beginInboxVerificationChunk(ctx, r.store.db)
	if err != nil {
		return inboxVerificationLease{}, err
	}
	defer cleanup()
	var lease inboxVerificationLease
	err = tx.QueryRowContext(ctx, `UPDATE scope_inbox_verification_jobs SET lease_owner=?,lease_token=lease_token+1,lease_until=`+inboxVerificationNowSQL+`+?
 WHERE id=? AND failure='' AND phase<>'pruning' AND lease_token<9223372036854775807 AND (lease_owner=? OR lease_until<=`+inboxVerificationNowSQL+`)
 RETURNING lease_token`, r.owner, r.options.LeaseDuration.Milliseconds(), r.job, r.owner).Scan(&lease.token)
	if errors.Is(err, sql.ErrNoRows) {
		var failure, phase string
		if e := tx.QueryRowContext(ctx, `SELECT failure,phase FROM scope_inbox_verification_jobs WHERE id=?`, r.job).Scan(&failure, &phase); e != nil {
			return inboxVerificationLease{}, e
		}
		if phase == "pruning" || failure == "cancelled" {
			return inboxVerificationLease{}, ErrInboxVerificationCancelled
		}
		if failure != "" {
			return inboxVerificationLease{}, inboxVerificationStoredFailure(failure)
		}
		return inboxVerificationLease{}, ErrInboxVerificationLease
	}
	if err != nil {
		return inboxVerificationLease{}, err
	}
	if err = ctx.Err(); err != nil {
		return inboxVerificationLease{}, err
	}
	return lease, tx.Commit()
}

func inboxVerificationAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrInboxVerificationLease
	}
	return err
}

func (r *inboxVerificationRunner) verifySlice(ctx context.Context, lease inboxVerificationLease) (InboxVerificationProgress, error) {
	var out InboxVerificationProgress
	if lease.token < 1 {
		return out, ErrInboxVerificationLease
	}
	ctx, cancel := context.WithTimeout(ctx, r.options.SliceDuration)
	defer cancel()
	prepared, err := r.prepareCanonical(ctx, lease)
	if err != nil {
		return out, err
	}
	tx, cleanup, err := beginInboxVerificationChunk(ctx, r.store.db)
	if err != nil {
		return out, err
	}
	defer cleanup()
	// The expensive oracle has already closed its read-only transaction. This
	// short write transaction validates the same source/shadow/directory epochs
	// and exact job checkpoint before accepting its private, immutable result.
	err = inboxVerificationAffected(tx.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET lease_until=lease_until
 WHERE id=? AND lease_owner=? AND lease_token=? AND lease_until>`+inboxVerificationNowSQL, r.job, r.owner, lease.token))
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `SAVEPOINT inbox_comparison_slice`); err != nil {
		return out, err
	}
	out, workErr := runInboxVerificationSliceTx(ctx, tx, r.job, prepared)
	if workErr != nil {
		category := inboxVerificationTerminalCategory(workErr)
		if category == "" {
			return InboxVerificationProgress{}, workErr
		}
		// A mismatch may occur after comparing other rows in the chunk. Persist
		// only terminal refusal, never those partial rows or any receipt.
		if _, err = tx.ExecContext(ctx, `ROLLBACK TO inbox_comparison_slice`); err != nil {
			return InboxVerificationProgress{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET failure=?,finished_at=CASE WHEN finished_at=0 THEN `+inboxVerificationNowSQL+` ELSE finished_at END WHERE id=?`, category, r.job); err != nil {
			return InboxVerificationProgress{}, err
		}
		out = InboxVerificationProgress{}
	} else if out.Complete {
		if _, err = tx.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET finished_at=CASE WHEN finished_at=0 THEN `+inboxVerificationNowSQL+` ELSE finished_at END WHERE id=?`, r.job); err != nil {
			return InboxVerificationProgress{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `RELEASE inbox_comparison_slice`); err != nil {
		return InboxVerificationProgress{}, err
	}
	// Recheck expiry AFTER every phase, checkpoint and receipt write. Expiry,
	// deadline or failed commit rolls the entire slice back, including refusal.
	err = inboxVerificationAffected(tx.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET lease_until=`+inboxVerificationNowSQL+`+?
 WHERE id=? AND lease_owner=? AND lease_token=? AND lease_until>`+inboxVerificationNowSQL, r.options.LeaseDuration.Milliseconds(), r.job, r.owner, lease.token))
	if err != nil {
		return InboxVerificationProgress{}, err
	}
	if err = ctx.Err(); err != nil {
		return InboxVerificationProgress{}, err
	}
	if err = tx.Commit(); err != nil {
		return InboxVerificationProgress{}, err
	}
	return out, workErr
}

// Legacy denial preparation can exceed the foreground write budget. It runs
// only on a deferred read snapshot; canonical writers continue while it works.
// No caller supplies eligible IDs. Every affecting write advances a checked
// epoch, so a changed source between the read and write transactions refuses
// comparison rather than publishing a receipt from mixed snapshots.
func (r *inboxVerificationRunner) prepareCanonical(ctx context.Context, lease inboxVerificationLease) (*inboxVerificationPrepared, error) {
	tx, cleanup, err := beginInboxVerificationTransaction(ctx, r.store.db, true)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	var held bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scope_inbox_verification_jobs WHERE id=? AND lease_owner=? AND lease_token=? AND lease_until>`+inboxVerificationNowSQL+`)`, r.job, r.owner, lease.token).Scan(&held); err != nil {
		return nil, err
	}
	if !held {
		return nil, ErrInboxVerificationLease
	}
	j, err := loadInboxVerificationJob(ctx, tx, r.job)
	if err != nil {
		return nil, nil
	} // write phase durably records the refusal
	if j.Phase != "canonical" {
		return nil, nil
	}
	fence, err := inboxVerificationClock(ctx, tx)
	if err != nil || fence != j.Fence {
		return nil, nil
	} // no obsolete graph construction
	prepared, err := prepareInboxVerificationCanonical(ctx, tx, j)
	if err != nil {
		if inboxVerificationTerminalCategory(err) == "" {
			return nil, err
		}
		return &inboxVerificationPrepared{job: j, failure: err}, nil
	}
	return prepared, nil
}

func inboxVerificationTerminalCategory(err error) string {
	switch {
	case errors.Is(err, ErrInboxVerificationCancelled):
		return "cancelled"
	case errors.Is(err, ErrInboxVerificationStale):
		return "stale"
	case errors.Is(err, ErrInboxVerificationCoverage):
		return "coverage"
	case errors.Is(err, ErrInboxVerificationMismatch):
		return "mismatch"
	case errors.Is(err, scopes.ErrBudget):
		return "budget"
	case errors.Is(err, scopes.ErrUpdating):
		return "updating"
	default:
		return ""
	}
}

func inboxVerificationStoredFailure(category string) error {
	switch category {
	case "cancelled":
		return ErrInboxVerificationCancelled
	case "stale":
		return ErrInboxVerificationStale
	case "coverage":
		return ErrInboxVerificationCoverage
	case "budget":
		return scopes.ErrBudget
	case "updating":
		return scopes.ErrUpdating
	default:
		return ErrInboxVerificationMismatch
	}
}

// Best-effort release cannot clear a successor's lease. Expiry remains a durable
// crash-recovery fallback; release is bounded and joined before worker shutdown.
func (r *inboxVerificationRunner) releaseLease(lease inboxVerificationLease) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	tx, cleanup, err := beginInboxVerificationChunk(ctx, r.store.db)
	if err != nil {
		return
	}
	defer cleanup()
	_, err = tx.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET lease_owner='',lease_until=0 WHERE id=? AND lease_owner=? AND lease_token=?`, r.job, r.owner, lease.token)
	if err == nil {
		_ = tx.Commit()
	}
}

// CancelInboxVerification explicitly retires an unfinished comparison. It
// fences every existing lease before making its temporary rows pruneable.
// Comparison receipts are maintenance evidence and never serving authority.
func (s *Store) CancelInboxVerification(ctx context.Context, id string) error {
	if !feedText(id, 64) {
		return scopes.ErrBudget
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tx, cleanup, err := beginInboxVerificationChunk(ctx, s.db)
	if err != nil {
		return err
	}
	defer cleanup()
	result, err := tx.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET failure='cancelled',finished_at=CASE WHEN finished_at=0 THEN `+inboxVerificationNowSQL+` ELSE finished_at END,lease_owner='',lease_until=0
 WHERE id=? AND phase<>'done'`, id)
	if err = inboxVerificationAffected(result, err); err != nil {
		return err
	}
	return tx.Commit()
}

type InboxVerificationWorkerDiagnostics struct{ Slices, Pauses, Completed uint64 }

// InboxVerificationWorker is only a shutdown/diagnostics handle. It contains no
// raw DB or computation factory. Zero enablement performs no DB I/O or work.
// Close cancels and JOINS before the owner may close the database. It is safe
// concurrently and repeatedly. No server constructor enables this worker yet.
type InboxVerificationWorker struct {
	cancel                    context.CancelFunc
	done                      chan struct{}
	once                      sync.Once
	err                       error
	slices, pauses, completed atomic.Uint64
}

func StartInboxVerificationWorker(ctx context.Context, enabled bool, s *Store, job string, options InboxVerificationWorkerOptions) (*InboxVerificationWorker, error) {
	w := &InboxVerificationWorker{done: make(chan struct{})}
	if !enabled {
		close(w.done)
		return w, nil
	}
	r, err := newInboxVerificationRunner(s, job, options)
	if err != nil {
		return nil, err
	}
	ctx, w.cancel = context.WithCancel(ctx)
	go func() { defer close(w.done); w.err = w.runComparison(ctx, r) }()
	return w, nil
}

func (w *InboxVerificationWorker) runComparison(ctx context.Context, r *inboxVerificationRunner) error {
	var held inboxVerificationLease
	defer func() {
		if held.token > 0 {
			r.releaseLease(held)
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		lease, err := r.acquireLease(ctx)
		if err == nil {
			held = lease
			var result InboxVerificationProgress
			result, err = r.verifySlice(ctx, lease)
			if err == nil {
				w.slices.Add(1)
				if result.Complete {
					w.completed.Add(1)
					return nil
				}
			}
		}
		if err != nil {
			if inboxVerificationTerminalCategory(err) != "" {
				return err
			}
			if errors.Is(err, sql.ErrNoRows) {
				return ErrInboxVerificationCancelled
			}
			w.pauses.Add(1)
		}
		timer := time.NewTimer(r.options.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *InboxVerificationWorker) Diagnostics() InboxVerificationWorkerDiagnostics {
	return InboxVerificationWorkerDiagnostics{Slices: w.slices.Load(), Pauses: w.pauses.Load(), Completed: w.completed.Load()}
}

func (w *InboxVerificationWorker) Close() error {
	if w == nil {
		return nil
	}
	w.once.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
	})
	<-w.done
	return w.err
}
