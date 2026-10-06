package scopemigrate_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/scopemigrate"
	"agent-nexus-core/internal/scopes"
)

func TestFrozenStepperContract(t *testing.T) {
	_, runner := fixture(t, 130)
	ctx := context.Background()
	token := begin(t, runner)
	before, e := runner.Report(ctx)
	must(t, e)
	store := scopemigrate.NewStore(runner)
	req := scopes.StepRequest{JobID: runner.Job, ExpectedEpoch: before.SourceEpoch, LeaseToken: token, MaxRecords: 64, MaxBytes: 4 << 20, MaxDuration: 50 * time.Millisecond}
	stale := req
	stale.ExpectedEpoch++
	if _, e = store.Step(ctx, stale); !errors.Is(e, scopemigrate.ErrSourceChanged) {
		t.Fatal(e)
	}
	same, e := runner.Report(ctx)
	must(t, e)
	if same != before {
		t.Fatal("epoch rejection advanced checkpoint", same, before)
	}
	result, e := store.Step(ctx, req)
	expectedRows, expectedExceptions := 64, 7
	// The race detector may exhaust the real 50ms slice. Refusal must leave the
	// checkpoint unchanged; retry a smaller slice rather than weakening the cap.
	if errors.Is(e, context.DeadlineExceeded) {
		unchanged, reportErr := runner.Report(ctx)
		must(t, reportErr)
		if unchanged != before {
			t.Fatal("deadline advanced checkpoint", unchanged, before)
		}
		smaller := req
		smaller.MaxRecords = 1
		result, e = store.Step(ctx, smaller)
		expectedRows, expectedExceptions = 1, 1
	}
	must(t, e)
	if result.Visited != expectedRows || result.Changed != expectedRows || result.Complete || result.PermanentPrivateExceptions != expectedExceptions {
		t.Fatal(result)
	}
	bad := req
	bad.MaxRecords = 65
	if _, e = store.Step(ctx, bad); !errors.Is(e, scopemigrate.ErrBudget) {
		t.Fatal(e)
	}
	lost := req
	lost.LeaseToken++
	if _, e = store.Step(ctx, lost); !errors.Is(e, scopemigrate.ErrLeaseLost) {
		t.Fatal(e)
	}
}
