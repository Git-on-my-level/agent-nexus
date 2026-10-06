package readmodel

import (
	"agent-nexus-core/internal/scopes"
	"context"
	"sort"
)

const MaxMutationWrites = 16
const MaxBuckets = 4

// Projection contains only bounded fields from the transaction-local old/new
// mutation. A resource has at most four destinations, each with <=4 counters.
// Generation is explicit: deltas must never update another generation's totals.
type Projection struct {
	ScopeID      scopes.ID
	Generation   int64
	RID, Version int64
	Entries      []Entry
}
type Entry struct {
	Family, Audience string
	Sort             int64
	Buckets          []string
}
type CounterKey struct {
	ScopeID                  scopes.ID
	Generation               int64
	Family, Audience, Bucket string
}
type FeedChange struct {
	Projection Projection
	Entry      Entry
	Delete     bool
}
type CounterChange struct {
	Key   CounterKey
	Delta int64
}
type Delta struct {
	Feeds    []FeedChange
	Counters []CounterChange
}

func validateProjection(p *Projection) error {
	if p == nil {
		return nil
	}
	if p.ScopeID == "" || p.Generation < 1 || p.RID < 1 || p.Version < 1 || len(p.Entries) > MaxStreamsPerScope {
		return ErrProjection
	}
	seen := map[Stream]bool{}
	for _, e := range p.Entries {
		s := Stream{Scope: p.ScopeID, Family: e.Family, Audience: e.Audience}
		if e.Family == "" || e.Audience == "" || seen[s] || len(e.Buckets) > MaxBuckets {
			return ErrProjection
		}
		seen[s] = true
		buckets := map[string]bool{}
		for _, b := range e.Buckets {
			if b == "" || buckets[b] {
				return ErrProjection
			}
			buckets[b] = true
		}
	}
	return nil
}

// PlanDelta validates the entire write fanout before the caller mutates source
// state. Apply must run inside that SAME source transaction. Deletes and inserts
// are exact-key operations; no counts, history scans or member expansion occur.
func PlanDelta(before, after *Projection) (Delta, error) {
	return planDelta(before, after, false)
}

func planDelta(before, after *Projection, rebuilding bool) (Delta, error) {
	if err := validateProjection(before); err != nil {
		return Delta{}, err
	}
	if err := validateProjection(after); err != nil {
		return Delta{}, err
	}
	if before != nil && after != nil && (before.RID != after.RID || after.Version < before.Version || after.Version == before.Version && !rebuilding || before.ScopeID != after.ScopeID || before.Generation != after.Generation) {
		return Delta{}, ErrProjection
	}
	d := Delta{}
	counts := map[CounterKey]int64{}
	for i, p := range []*Projection{before, after} {
		if p == nil {
			continue
		}
		step := int64(-1)
		if i == 1 {
			step = 1
		}
		for _, e := range p.Entries {
			// Copy bounded slices: callers cannot modify a validated plan later.
			e.Buckets = append([]string(nil), e.Buckets...)
			identity := *p
			identity.Entries = nil
			d.Feeds = append(d.Feeds, FeedChange{identity, e, i == 0})
			for _, b := range e.Buckets {
				counts[CounterKey{p.ScopeID, p.Generation, e.Family, e.Audience, b}] += step
			}
		}
	}
	for k, v := range counts {
		if v != 0 {
			d.Counters = append(d.Counters, CounterChange{k, v})
		}
	}
	if len(d.Feeds)+len(d.Counters) > MaxMutationWrites {
		return Delta{}, ErrBudget
	}
	// Deterministic order simplifies transactional fault injection and replay.
	sort.Slice(d.Counters, func(i, j int) bool {
		a, b := d.Counters[i].Key, d.Counters[j].Key
		if a.ScopeID != b.ScopeID {
			return a.ScopeID < b.ScopeID
		}
		if a.Generation != b.Generation {
			return a.Generation < b.Generation
		}
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		if a.Audience != b.Audience {
			return a.Audience < b.Audience
		}
		return a.Bucket < b.Bucket
	})
	return d, nil
}

// CounterReader reads exact buckets in the Snapshot transaction. A missing row
// means zero only for an active, parity-proven generation. An unavailable
// selection never calls Buckets and returns a nil Values map (not synthetic 0).
type CounterReader interface {
	Snapshot(context.Context) (Snapshot, error)
	Buckets(context.Context, []string) (map[string]int64, error)
}
type Counts struct {
	Values   map[string]int64 `json:"values,omitempty"`
	Coverage scopes.Coverage  `json:"coverage"`
}

func Count(ctx context.Context, r CounterReader, buckets []string) (Counts, error) {
	if len(buckets) < 1 || len(buckets) > MaxBuckets {
		return Counts{}, ErrBudget
	}
	seen := map[string]bool{}
	for _, b := range buckets {
		if b == "" || seen[b] {
			return Counts{}, ErrProjection
		}
		seen[b] = true
	}
	s, err := r.Snapshot(ctx)
	if err != nil {
		return Counts{}, err
	}
	a, err := validateSnapshot(s)
	if err != nil {
		return Counts{}, err
	}
	c := Counts{Coverage: coverage(s)}
	if a != Available {
		return c, nil
	}
	c.Values, err = r.Buckets(ctx, append([]string(nil), buckets...))
	if err != nil {
		return Counts{}, err
	}
	if len(c.Values) != len(buckets) {
		return Counts{}, ErrProjection
	}
	for _, b := range buckets {
		n, ok := c.Values[b]
		if !ok || n < 0 {
			return Counts{}, ErrProjection
		}
	}
	return c, nil
}
