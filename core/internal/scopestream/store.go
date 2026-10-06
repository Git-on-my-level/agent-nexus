package scopestream

import (
	"context"
	"sort"

	"agent-nexus-core/internal/scopes"
)

// Store adapts the executor to A's frozen StreamTicker contract. Factories and
// transaction capabilities remain in the trusted scoped repository dispatcher.
type Store struct {
	Repository Repository
	Tokens     *Tokens
}

// Tick emits one item per shared-contract page because StreamChange currently
// carries only a page cursor. This preserves disconnect correctness for an SSE
// consumer of that interface. ScopeHandler uses the full executor's per-item
// continuations for bounded multi-item ticks. A must retain that transport path
// or add an explicit per-item cursor before framing a whole shared page as SSE.
func (s *Store) Tick(ctx context.Context, req scopes.TickRequest) (scopes.TickResult, error) {
	var result scopes.TickResult
	if s == nil || len(req.Selection.ScopeIDs) == 0 || len(req.Selection.ScopeIDs) > MaxScopes || req.Limit < 0 || req.Limit > MaxPageSize {
		return result, ErrBudget
	}
	selected := map[scopes.ID]bool{}
	for _, id := range req.Selection.ScopeIDs {
		if id == "" || selected[id] {
			return result, ErrBudget
		}
		selected[id] = true
	}
	covered := map[scopes.ID]bool{}
	for _, stream := range req.Streams {
		if !selected[stream.Scope] {
			return result, ErrBudget
		}
		covered[stream.Scope] = true
	}
	if len(covered) != len(selected) {
		return result, ErrBudget
	}
	page, err := Tick(ctx, s.Repository, s.Tokens, Request{Principal: req.Selection.Principal, Streams: req.Streams, Limit: 1, Continuation: req.Cursor})
	if err != nil {
		return result, err
	}
	result.Items = make([]scopes.StreamChange, 0, len(page.Items))
	for _, item := range page.Items {
		r := item.Record
		result.Items = append(result.Items, scopes.StreamChange{ScopeID: r.Key.Stream.Scope, Family: r.Key.Stream.Family, Audience: r.Key.Stream.Audience, ResourceID: r.Key.Head.RID, Sequence: r.Key.Head.Sequence, Payload: append([]byte(nil), r.Payload...)})
	}
	result.NextCursor = page.Continuation
	result.Coverage.CoveredScopeIDs = append([]scopes.ID(nil), req.Selection.ScopeIDs...)
	sort.Slice(result.Coverage.CoveredScopeIDs, func(i, j int) bool { return result.Coverage.CoveredScopeIDs[i] < result.Coverage.CoveredScopeIDs[j] })
	return result, nil
}

var _ scopes.StreamTicker = (*Store)(nil)
