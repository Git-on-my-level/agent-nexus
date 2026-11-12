package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// MigrateAndDrainAccess opens a writable workspace copy with the current binary,
// runs its migrations, then executes every bounded access maintenance job until
// all of them report idle. It finishes with the full compact-access verification
// and closes the workspace, including its writers, before returning.
//
// The jobs are the access-owned maintenance cursors used by the serving
// lifecycle: ask subjects, inbox lifecycle, document access history, scoped
// inbox, compact backfill, stable raw identities and retired raw edges. Search
// projection jobs are deliberately excluded: they do not affect authorization
// decisions and would add unrelated full-workspace work to a release gate.
func MigrateAndDrainAccess(ctx context.Context, databasePath string, logf func(string)) error {
	if logf == nil {
		logf = func(string) {}
	}
	w, err := OpenWorkspaceDatabase(ctx, databasePath)
	if err != nil {
		return fmt.Errorf("open writable workspace copy: %w", err)
	}
	defer w.Close()

	// Install the compact shadow when missing and let a small workspace certify
	// immediately. A large workspace resumes through the bounded batches below.
	_, resume, err := w.PrepareCompactAccessMigration(ctx)
	if err != nil && !resume {
		// An explicit operator rollback (reader=2) is not a harness failure; the
		// remaining access jobs and canonical verification still run.
		var migrationErr *CompactAccessMigrationError
		if !errors.As(err, &migrationErr) || migrationErr.Reason != "operator_rollback" {
			return fmt.Errorf("prepare compact access: %w", err)
		}
		logf("compact access is held in operator rollback; skipping shadow certification")
	}

	type job struct {
		name  string
		table string
		run   func(context.Context) (bool, error)
	}
	jobs := []job{
		{"ask_subjects", "ask_subjects_job", w.MaintainAskSubjectsBatch},
		{"inbox_lifecycle", "inbox_lifecycle_job", func(ctx context.Context) (bool, error) {
			return w.MaintainInboxLifecycleBatch(ctx, 200)
		}},
		{"document_history", documentHistoryJobTable, w.MaintainDocumentAccessHistoryBatch},
		{"scope_inbox", "scope_inbox_live_job", w.MaintainScopeInboxBatch},
		{"compact_backfill", "resource_access_compact_job", w.MaintainCompactAccessBatch},
		{"stable_backfill", "resource_access_compact_control", w.MaintainStableAccessBatch},
		{"compact_retired", "resource_access_compact_control", w.MaintainCompactAccessRetiredBatch},
	}

	active := make([]job, 0, len(jobs))
	for _, candidate := range jobs {
		exists, existsErr := w.accessTableExists(ctx, candidate.table)
		if existsErr != nil {
			return fmt.Errorf("inspect %s: %w", candidate.name, existsErr)
		}
		if !exists {
			logf(candidate.name + ": job table absent, idle")
			continue
		}
		active = append(active, candidate)
	}

	for {
		allIdle := true
		for _, candidate := range active {
			done, runErr := candidate.run(ctx)
			if runErr != nil {
				return fmt.Errorf("%s: %w", candidate.name, runErr)
			}
			if !done {
				allIdle = false
			}
		}
		if allIdle {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}

	if _, err := w.VerifyCompactAccessCutover(ctx); err != nil {
		return fmt.Errorf("verify compact access: %w", err)
	}
	logf("verification: compact, exact and raw access ledgers agree")
	return nil
}

// MaintainCompactAccessRetiredBatch exposes the retired-edge drain to the
// offline harness without changing its serving worker behavior.
func (w *Workspace) MaintainCompactAccessRetiredBatch(ctx context.Context) (bool, error) {
	return w.maintainCompactAccessRetiredBatch(ctx)
}

func (w *Workspace) accessTableExists(ctx context.Context, table string) (bool, error) {
	var exists bool
	if err := w.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type IN ('table','view') AND name=?)`, table).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
