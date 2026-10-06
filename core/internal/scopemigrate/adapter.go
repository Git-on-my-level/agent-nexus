package scopemigrate

import (
	"context"
	"sync"
	"time"

	"agent-nexus-core/internal/scopes"
)

// Store adapts the private, serial worker to the shared stream contract. The
// integration owner creates one Store per Runner and must not concurrently call
// Runner.Step/Run directly. It exposes neither raw connections nor Source facts.
type Store struct {
	mu     sync.Mutex
	runner *Runner
}

func NewStore(r *Runner) *Store { return &Store{runner: r} }

var _ scopes.MigrationStepper = (*Store)(nil)

func (s *Store) Step(ctx context.Context, request scopes.StepRequest) (scopes.StepResult, error) {
	out := scopes.StepResult{}
	if s == nil || s.runner == nil {
		return out, ErrBudget
	}
	if request.JobID != s.runner.Job || request.ExpectedEpoch < 0 || request.LeaseToken < 1 || request.MaxRecords < 1 || request.MaxRecords > MaxChunk || request.MaxBytes < 1 || request.MaxBytes > int64(s.runner.MaxBytes) || request.MaxDuration <= 0 || request.MaxDuration > 50*time.Millisecond {
		return out, ErrBudget
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, request.MaxDuration)
	defer cancel()
	before, e := s.runner.Report(ctx)
	if e != nil {
		return out, e
	}
	runner := *s.runner
	runner.MaxBytes = int(request.MaxBytes)
	after, e := runner.step(ctx, request.LeaseToken, request.MaxRecords, &request.ExpectedEpoch)
	if e != nil {
		return out, e
	}
	processed, exceptions := after.Processed, after.Exceptions
	if before.Generation == after.Generation {
		processed -= before.Processed
		exceptions -= before.Exceptions
	}
	out.Visited = int(processed)
	out.Changed = int(processed)
	out.PermanentPrivateExceptions = int(exceptions)
	out.Complete = after.Done
	return out, nil
}
