package readmodel

import (
	"agent-nexus-core/internal/scopes"
	"context"
)

const MaxLifecycleChunk = 64
const MaxAncestors = 8

// LifecycleRow is bounded metadata, not a full canonical body or history.
// Ancestry must be resolved by indexed same-scope lookups, with depth <=8.
type LifecycleRow struct {
	RID           int64
	ScopeID       scopes.ID
	Ancestors     []Ancestor
	Before, After *Projection
}
type Ancestor struct {
	ScopeID scopes.ID
	RID     int64
}

// Generation is the target projection generation. Rebuild an initially empty
// staging generation, never the selected generation; Before refers only to an
// already staged projection on a replay. Activate selects this exact generation
// under the job fence, rather than incrementing a generation independently.
type Job struct {
	ScopeID                   scopes.ID
	Generation, Fence, Cursor int64
}
type StepResult struct {
	Examined int
	Complete bool
}

// LifecycleTx is owned by the durable worker (stream D). Every method runs in
// one claimed/fenced write transaction. Rows may examine <=limit records via
// (scope_id,rid), not skip dead descendants until finding live rows. The adapter
// supplies its reviewed projection rules, including independent inbox asks.
// Apply/Checkpoint/Activate failures MUST roll back all changes in this slice.
type LifecycleTx interface {
	Job(context.Context) (Job, error)
	Rows(context.Context, int) ([]LifecycleRow, error)
	Apply(context.Context, Delta) error
	Checkpoint(context.Context, Job, int64) error
	Activate(context.Context, Job) error
}

// Step never activates on a partially examined chunk. An empty indexed seek
// proves completion; activation selects the fully rebuilt generation atomically.
// Foreground lifecycle initiation belongs to A: scope fence + parent write +
// durable job insert, in one transaction, with no descendant walk.
func Step(ctx context.Context, tx LifecycleTx) (StepResult, error) {
	j, err := tx.Job(ctx)
	if err != nil {
		return StepResult{}, err
	}
	if j.ScopeID == "" || j.Generation < 1 || j.Fence < 1 || j.Cursor < 0 {
		return StepResult{}, ErrProjection
	}
	rows, err := tx.Rows(ctx, MaxLifecycleChunk)
	if err != nil {
		return StepResult{}, err
	}
	if len(rows) > MaxLifecycleChunk {
		return StepResult{}, ErrBudget
	}
	if len(rows) == 0 {
		if err = tx.Activate(ctx, j); err != nil {
			return StepResult{}, err
		}
		return StepResult{Complete: true}, nil
	}
	plans := make([]Delta, len(rows))
	cursor := j.Cursor
	for i, row := range rows {
		if row.RID <= cursor || row.ScopeID != j.ScopeID || len(row.Ancestors) > MaxAncestors {
			return StepResult{}, ErrProjection
		}
		seen := map[int64]bool{row.RID: true}
		for _, a := range row.Ancestors {
			if a.ScopeID != j.ScopeID || a.RID < 1 || seen[a.RID] {
				return StepResult{}, ErrProjection
			}
			seen[a.RID] = true
		}
		for _, p := range []*Projection{row.Before, row.After} {
			if p != nil && (p.ScopeID != j.ScopeID || p.RID != row.RID || p.Generation != j.Generation) {
				return StepResult{}, ErrProjection
			}
		}
		// An ancestor lifecycle change does not rewrite descendants' canonical
		// versions; the fenced rebuild may replace a projection at equal version.
		plans[i], err = planDelta(row.Before, row.After, true)
		if err != nil {
			return StepResult{}, err
		}
		cursor = row.RID
	}
	// Validate the whole chunk before the first write. Repository rollback remains
	// mandatory for storage faults and checkpoint fencing failures.
	for _, plan := range plans {
		if err = tx.Apply(ctx, plan); err != nil {
			return StepResult{}, err
		}
	}
	if err = tx.Checkpoint(ctx, j, cursor); err != nil {
		return StepResult{}, err
	}
	return StepResult{Examined: len(rows)}, nil
}
