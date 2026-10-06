package scopemigrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"agent-nexus-core/internal/scopes"
)

var (
	ErrLeaseLost     = errors.New("scope migration lease lost")
	ErrLowDisk       = errors.New("scope migration paused for disk headroom")
	ErrSourceChanged = errors.New("scope migration source epoch changed")
	ErrBudget        = errors.New("scope migration adapter exceeded chunk budget")
)

// Source is implemented by trusted integration code. Epoch must capture every
// relevant canonical, alias, grant, PM and purge change, in the same transaction
// as that change. Page reads ONLY recorded metadata/authority and must use an
// indexed strict keyset, with at most limit candidates and maxBytes bytes. It
// must consume precomputed legacy authority, never run a graph closure per row.
// Done is an explicit end-of-source proof, not inferred from a short byte page.
type Source interface {
	Epoch(context.Context, *sql.Tx) (int64, error)
	Page(context.Context, *sql.Tx, string, int, int) (records []Record, done bool, err error)
}

// LifecycleRebuilder lets B participate in D's supervised worker without a
// dependency on B's package. Its writes/checkpoint must use this transaction;
// commit, background work and external side effects are forbidden. A supplies
// an adapter for B's frozen signature. Limit bounds candidate work, not outputs.
type LifecycleRebuilder interface {
	Step(context.Context, *sql.Tx, int, int64) (bool, error)
}

type Runner struct {
	DB                      *sql.DB
	Source                  Source
	Job, Owner, SealedScope string
	LeaseDuration           time.Duration
	MaxBytes                int
	// CheckDisk runs before opening a write transaction. Its filesystem must be
	// the database/WAL filesystem. SQLite FULL/ENOSPC is still handled by rollback.
	CheckDisk func(context.Context) error
	Rebuilder LifecycleRebuilder
}

type Progress struct {
	Generation, SourceEpoch, Token int64
	Cursor                         string
	Processed, Exceptions          int64
	Done                           bool
}

const nowSQL = `(CAST(strftime('%s','now') AS INTEGER)*1000+CAST(substr(strftime('%f','now'),4,3) AS INTEGER))`

func (r *Runner) validate() error {
	if r == nil || r.DB == nil || r.Source == nil || r.Job == "" || r.Owner == "" || r.SealedScope == "" {
		return errors.New("scope migration runner is not configured")
	}
	if r.LeaseDuration < time.Second || r.LeaseDuration > time.Minute || r.MaxBytes < 1 || r.MaxBytes > 4<<20 {
		return errors.New("invalid scope migration limits")
	}
	return nil
}

// Acquire renews a live same-owner lease or takes over an expired lease with a
// strictly larger database token. Owners must be unique per process attempt.
// No lease/checkpoint writes happen at server initialization.
func (r *Runner) Acquire(ctx context.Context) (int64, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO scope_migration_jobs(job) VALUES(?) ON CONFLICT DO NOTHING`, r.Job)
	if err != nil {
		return 0, err
	}
	query := `UPDATE scope_migration_jobs SET token=token+CASE WHEN lease_until<=` + nowSQL + ` THEN 1 ELSE 0 END,
owner=?,lease_until=` + nowSQL + `+? WHERE job=? AND (owner=? OR lease_until<=` + nowSQL + `) RETURNING token`
	var token int64
	err = tx.QueryRowContext(ctx, query, r.Owner, r.LeaseDuration.Milliseconds(), r.Job, r.Owner).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrLeaseLost
	}
	if err != nil {
		return 0, err
	}
	return token, tx.Commit()
}

// Step atomically stages one strict keyset chunk and its success watermark.
// Every failure (including FULL, cancellation, expiry and commit failure) rolls
// back all assignments and counters. Epoch invalidation starts a new unreadable
// generation without deleting old rows or doing an unbounded cleanup.
func (r *Runner) Step(ctx context.Context, token int64, limit int) (Progress, error) {
	if err := r.validate(); err != nil {
		return Progress{}, err
	}
	return r.step(ctx, token, limit, r.MaxBytes, 50*time.Millisecond, nil, nil)
}

// Store implements A's frozen MigrationStepper without exposing checkpoints to
// callers. Runner is trusted worker configuration; it must remain immutable
// while the store is running. Source facts and all writes use one transaction.
type Store struct{ Runner *Runner }

var _ scopes.MigrationStepper = (*Store)(nil)

func (s *Store) Step(ctx context.Context, request scopes.StepRequest) (scopes.StepResult, error) {
	if s == nil || s.Runner == nil {
		return scopes.StepResult{}, errors.New("migration store unavailable")
	}
	r := s.Runner
	if err := r.validate(); err != nil {
		return scopes.StepResult{}, err
	}
	if request.JobID != r.Job || request.ExpectedEpoch < 0 {
		return scopes.StepResult{}, errors.New("invalid migration job or epoch")
	}
	if request.LeaseToken < 1 {
		return scopes.StepResult{}, ErrLeaseLost
	}
	if request.MaxBytes < 1 || request.MaxBytes > int64(r.MaxBytes) {
		return scopes.StepResult{}, ErrBudget
	}
	var result scopes.StepResult
	_, err := r.step(ctx, request.LeaseToken, request.MaxRecords, int(request.MaxBytes), request.MaxDuration, &request.ExpectedEpoch, &result)
	if err != nil {
		return scopes.StepResult{}, err
	}
	return result, nil
}

func (r *Runner) step(ctx context.Context, token int64, limit, maxBytes int, duration time.Duration, expectedEpoch *int64, result *scopes.StepResult) (Progress, error) {
	if err := r.validate(); err != nil {
		return Progress{}, err
	}
	if limit < 1 || limit > MaxChunk || maxBytes < 1 || maxBytes > r.MaxBytes || duration <= 0 || duration > 50*time.Millisecond {
		return Progress{}, ErrBudget
	}
	if r.CheckDisk != nil {
		if err := r.CheckDisk(ctx); err != nil {
			return Progress{}, err
		}
	}
	// Bind the transaction itself to the work budget, so expiration also
	// rolls back pending writes and prevents a later successful Commit.
	// Time spent acquiring SQLite's write lock consumes the same budget.
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return Progress{}, err
	}
	defer tx.Rollback()
	p, err := r.progress(ctx, tx, token)
	if err != nil {
		return Progress{}, err
	}
	epoch, err := r.Source.Epoch(ctx, tx)
	if err != nil {
		return Progress{}, err
	}
	if epoch < 0 {
		return Progress{}, errors.New("invalid source epoch")
	}
	if expectedEpoch != nil && epoch != *expectedEpoch {
		return Progress{}, ErrSourceChanged
	}
	if p.SourceEpoch != epoch {
		p.Generation++
		p.SourceEpoch = epoch
		p.Cursor = ""
		p.Processed = 0
		p.Exceptions = 0
		p.Done = false
		if err := r.save(ctx, tx, p); err != nil {
			return Progress{}, err
		}
		if err := tx.Commit(); err != nil {
			return Progress{}, err
		}
		return p, nil
	}
	if p.Done {
		if result != nil {
			result.Complete = true
		}
		return p, nil
	}
	if verifier, ok := r.Source.(interface {
		VerifySink(context.Context, *sql.Tx, string) error
	}); ok {
		if err := verifier.VerifySink(ctx, tx, r.SealedScope); err != nil {
			return Progress{}, err
		}
	}
	batch, done, err := r.Source.Page(ctx, tx, p.Cursor, limit, maxBytes)
	if err != nil {
		return Progress{}, err
	}
	if len(batch) > limit || (len(batch) == 0 && !done) {
		return Progress{}, ErrBudget
	}
	bytes := 0
	initialExceptions := p.Exceptions
	for _, record := range batch {
		bytes += recordBytes(record)
		if bytes > maxBytes || record.Key <= p.Cursor {
			return Progress{}, ErrBudget
		}
		placement, err := Place(record, r.SealedScope)
		if err != nil {
			return Progress{}, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO scope_migration_placements(job,generation,source_key,kind,resource_id,source_version,scope_id,exception,content_unavailable)
VALUES(?,?,?,?,?,?,?,?,?)`, r.Job, p.Generation, record.Key, record.Kind, record.ID, record.Version, placement.ScopeID, placement.Exception, placement.ContentUnavailable)
		if err != nil {
			return Progress{}, err
		}
		p.Cursor = record.Key
		p.Processed++
		if placement.Exception {
			p.Exceptions++
		}
	}
	currentEpoch, err := r.Source.Epoch(ctx, tx)
	if err != nil {
		return Progress{}, err
	}
	if currentEpoch != epoch {
		return Progress{}, ErrSourceChanged
	}
	p.Done = done
	if err := r.save(ctx, tx, p); err != nil {
		return Progress{}, err
	}
	if err := tx.Commit(); err != nil {
		return Progress{}, err
	}
	if result != nil {
		*result = scopes.StepResult{Visited: len(batch), Changed: len(batch), Complete: p.Done, PermanentPrivateExceptions: int(p.Exceptions - initialExceptions)}
	}
	return p, nil
}

func recordBytes(r Record) int {
	n := 64 + len(r.Key) + len(r.Kind) + len(r.ID) + len(r.Container.ScopeID) + len(r.Creator.ScopeID) + len(r.Admins.ScopeID) + len(r.RestrictionOwner.ScopeID)
	for _, owner := range r.RestrictingOwners {
		n += len(owner)
	}
	return n
}

func (r *Runner) progress(ctx context.Context, tx *sql.Tx, token int64) (Progress, error) {
	p := Progress{Token: token}
	err := tx.QueryRowContext(ctx, `SELECT generation,source_epoch,cursor,processed,exceptions,done FROM scope_migration_jobs WHERE job=? AND owner=? AND token=? AND lease_until>`+nowSQL, r.Job, r.Owner, token).Scan(&p.Generation, &p.SourceEpoch, &p.Cursor, &p.Processed, &p.Exceptions, &p.Done)
	if errors.Is(err, sql.ErrNoRows) {
		return Progress{}, ErrLeaseLost
	}
	return p, err
}

func (r *Runner) save(ctx context.Context, tx *sql.Tx, p Progress) error {
	result, err := tx.ExecContext(ctx, `UPDATE scope_migration_jobs SET generation=?,source_epoch=?,cursor=?,processed=?,exceptions=?,done=? WHERE job=? AND owner=? AND token=? AND lease_until>`+nowSQL, p.Generation, p.SourceEpoch, p.Cursor, p.Processed, p.Exceptions, p.Done, r.Job, r.Owner, p.Token)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrLeaseLost
	}
	return err
}

// LifecycleStep fences B's worker transaction with the SAME migration lease.
func (r *Runner) LifecycleStep(ctx context.Context, token int64) (bool, error) {
	if err := r.validate(); err != nil {
		return false, err
	}
	if r.Rebuilder == nil {
		return true, nil
	}
	if r.CheckDisk != nil {
		if err := r.CheckDisk(ctx); err != nil {
			return false, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	p, err := r.progress(ctx, tx, token)
	if err != nil {
		return false, err
	}
	done, err := r.Rebuilder.Step(ctx, tx, MaxChunk, token)
	if err != nil {
		return false, err
	}
	if err := r.save(ctx, tx, p); err != nil {
		return false, err
	}
	return done, tx.Commit()
}

// Run is the worker's foreground lifecycle. The server supervises it with its
// shutdown context; it must join Run before closing the DB. Readiness never
// waits for it. Failures pause conversion; reports expose categories only.
func (r *Runner) Run(ctx context.Context, interval time.Duration, report func(string)) error {
	if err := r.validate(); err != nil {
		return err
	}
	if interval < time.Millisecond {
		return errors.New("invalid worker interval")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		token, err := r.Acquire(ctx)
		if err == nil {
			_, err = r.Step(ctx, token, MaxChunk)
		}
		if err == nil {
			_, err = r.LifecycleStep(ctx, token)
		}
		if err != nil && report != nil {
			category := "paused"
			if errors.Is(err, ErrLeaseLost) {
				category = "lease_lost"
			}
			if errors.Is(err, ErrLowDisk) {
				category = "low_disk"
			}
			report(category)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Report reads a single job row; it does not scan placements or content.
func (r *Runner) Report(ctx context.Context) (Progress, error) {
	if r == nil || r.DB == nil {
		return Progress{}, fmt.Errorf("migration database unavailable")
	}
	var p Progress
	err := r.DB.QueryRowContext(ctx, `SELECT generation,source_epoch,token,cursor,processed,exceptions,done FROM scope_migration_jobs WHERE job=?`, r.Job).Scan(&p.Generation, &p.SourceEpoch, &p.Token, &p.Cursor, &p.Processed, &p.Exceptions, &p.Done)
	return p, err
}
