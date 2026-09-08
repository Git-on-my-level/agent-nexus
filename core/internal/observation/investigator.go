package observation

import (
	"context"
)

// IsolatedInvestigator is an optional production injection. Implementations
// must independently enforce investigation limits inside the same isolation
// boundary as generated readers. A prompt, host subprocess, or directory
// restriction is not that boundary.
type IsolatedInvestigator interface {
	Available() error
	Investigate(context.Context, InvestigationSpec, Report) (Report, error)
}

// InvestigationRuntime evaluates saved investigation specs. Saving a spec is
// not dispatch. It never repairs sources, never falls back to host model
// execution, and fails closed when the host isolation runner is unavailable
// or no approved isolated executor is configured.
type InvestigationRuntime struct {
	store  *InvestigationStore
	runner isolatedExecutor
	exec   IsolatedInvestigator
}

func NewInvestigationRuntime(store *InvestigationStore) (*InvestigationRuntime, error) {
	if store == nil {
		return nil, failure(ErrConfiguration, "investigation runtime requires saved specifications")
	}
	return &InvestigationRuntime{store: store, runner: NewIsolatedRunner()}, nil
}

func (r *InvestigationRuntime) Run(ctx context.Context, workspaceID, id string, source Reader) (Report, error) {
	if r == nil {
		return Report{}, failure(ErrUnavailable, "investigation runtime is not configured")
	}
	saved, err := r.store.Get(ctx, workspaceID, id)
	if err != nil {
		return Report{}, err
	}
	spec := saved.Spec
	if err = spec.Validate(); err != nil {
		return Report{}, err
	}
	if source == nil || !source.Capabilities().TrustedBuiltin {
		return Report{}, failure(ErrPolicy, "investigations may only read through an approved built-in source reader")
	}
	if source.Capabilities().Source != spec.Target.Source {
		return Report{}, failure(ErrPolicy, "investigation source is outside the approved envelope")
	}
	// Isolation is checked before any source I/O so unsupported hosts never
	// leak target content into an unconstrained model process.
	if err = r.runner.Available(); err != nil {
		return Report{}, err
	}
	if r.exec == nil {
		return Report{}, failure(ErrUnavailable, "investigation execution requires an approved isolated model runner; host execution is not a fallback")
	}
	if err = r.exec.Available(); err != nil {
		return Report{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, spec.Limits.Timeout)
	defer cancel()
	snapshot, err := source.Read(ctx, spec.Target)
	if err != nil {
		return Report{}, err
	}
	if snapshot.Target != spec.Target {
		return Report{}, failure(ErrPolicy, "source snapshot target does not match the investigation")
	}
	report, err := r.exec.Investigate(ctx, spec, snapshot)
	if err != nil {
		return Report{}, err
	}
	if report.Knowledge == "verified" {
		return Report{}, failure(ErrPolicy, "investigators cannot self-certify verification")
	}
	if report.Knowledge == "" {
		report.Knowledge = "uncertain"
	}
	report.Target = spec.Target
	report.ReaderID = "investigator:" + id
	if report.ReaderRevision == "" {
		report.ReaderRevision = ReaderRevision
	}
	if report.ObservedAt.IsZero() {
		report.ObservedAt = snapshot.ObservedAt
	}
	return finishReport(report)
}
