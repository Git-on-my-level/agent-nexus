package scopemigrate_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/scopemigrate"
	"agent-nexus-core/internal/scopes"
)

func TestFrozenMigrationStepperEpochAndBudgets(t *testing.T) {
	w, runner := fixture(t, 10)
	ctx := context.Background()
	token := begin(t, runner)
	before, err := runner.Report(ctx)
	must(t, err)
	var stepper scopes.MigrationStepper = &scopemigrate.Store{Runner: runner}
	request := scopes.StepRequest{JobID: runner.Job, ExpectedEpoch: before.SourceEpoch, LeaseToken: token, MaxRecords: 4, MaxBytes: 1024, MaxDuration: 50 * time.Millisecond}
	bad := request
	bad.ExpectedEpoch++
	result, err := stepper.Step(ctx, bad)
	if !errors.Is(err, scopemigrate.ErrSourceChanged) || result != (scopes.StepResult{}) {
		t.Fatal(result, err)
	}
	after, err := runner.Report(ctx)
	must(t, err)
	if after != before {
		t.Fatal("expected-epoch mismatch advanced a checkpoint")
	}
	bad = request
	bad.LeaseToken++
	_, err = stepper.Step(ctx, bad)
	if !errors.Is(err, scopemigrate.ErrLeaseLost) {
		t.Fatal(err)
	}
	for _, edit := range []func(*scopes.StepRequest){
		func(r *scopes.StepRequest) { r.MaxRecords = 65 },
		func(r *scopes.StepRequest) { r.MaxBytes = (4 << 20) + 1 },
		func(r *scopes.StepRequest) { r.MaxDuration = time.Second },
	} {
		bad = request
		edit(&bad)
		_, err = stepper.Step(ctx, bad)
		if !errors.Is(err, scopemigrate.ErrBudget) {
			t.Fatal(err)
		}
	}
	// A valid smaller request commits its own counters, not a report snapshot
	// gathered outside the transaction. Unknown manifests consume a sink slot.
	result, err = stepper.Step(ctx, request)
	must(t, err)
	if result.Visited != 4 || result.Changed != 4 || result.PermanentPrivateExceptions != 1 || result.Complete {
		t.Fatal(result)
	}
	before, err = runner.Report(ctx)
	must(t, err)
	bad = request
	bad.MaxBytes = 1
	_, err = stepper.Step(ctx, bad)
	if !errors.Is(err, scopemigrate.ErrBudget) {
		t.Fatal(err)
	}
	after, err = runner.Report(ctx)
	must(t, err)
	if before != after {
		t.Fatal("byte-limit failure advanced checkpoint")
	}
	_, err = w.DB().Exec(`UPDATE resource_access_epoch SET version=version+1`)
	must(t, err)
	_, err = stepper.Step(ctx, request)
	if !errors.Is(err, scopemigrate.ErrSourceChanged) {
		t.Fatal(err)
	}
	after, err = runner.Report(ctx)
	must(t, err)
	if before != after {
		t.Fatal("source changed but old request advanced generation")
	}
}

type durationSource struct{ artifactSource }

func (s durationSource) Page(ctx context.Context, tx *sql.Tx, after string, limit, maxBytes int) ([]scopemigrate.Record, bool, error) {
	_, err := tx.ExecContext(ctx, `INSERT INTO scope_migration_placements VALUES('duration',1,'uncommitted','artifact','uncommitted',1,'no-grants',1,1)`)
	if err != nil {
		return nil, false, err
	}
	<-ctx.Done()
	return nil, false, ctx.Err()
}

func TestFrozenMigrationDurationRollsBack(t *testing.T) {
	w, runner := fixture(t, 1)
	ctx := context.Background()
	token := begin(t, runner)
	before, err := runner.Report(ctx)
	must(t, err)
	runner.Source = durationSource{}
	store := &scopemigrate.Store{Runner: runner}
	result, err := store.Step(ctx, scopes.StepRequest{JobID: runner.Job, ExpectedEpoch: before.SourceEpoch, LeaseToken: token, MaxRecords: 1, MaxBytes: 1024, MaxDuration: 10 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) || result != (scopes.StepResult{}) {
		t.Fatal(result, err)
	}
	after, err := runner.Report(ctx)
	must(t, err)
	if after != before {
		t.Fatal("expired chunk advanced checkpoint")
	}
	var rows int
	must(t, w.DB().QueryRow(`SELECT count(*) FROM scope_migration_placements WHERE job='duration'`).Scan(&rows))
	if rows != 0 {
		t.Fatal("expired chunk committed writes")
	}
}
