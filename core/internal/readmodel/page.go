// Package readmodel contains bounded projection algorithms. It has no database
// factory and cannot grant authority. The caller supplies one transaction-bound,
// authorized repository; production wiring belongs to the scope dispatcher.
package readmodel

import (
	"container/heap"
	"context"
	"encoding/json"
	"errors"
	"time"

	"agent-nexus-core/internal/scopes"
)

const (
	MaxScopes          = scopes.MaxScopes
	MaxStreams         = scopes.MaxStreams
	MaxStreamsPerScope = scopes.MaxStreamsPerScope
	MaxPageSize        = scopes.MaxPage
	MaxItemBytes       = 16 * 1024
)

var (
	ErrBudget     = errors.New("readmodel budget exceeded")
	ErrProjection = errors.New("invalid readmodel projection")
	ErrCursor     = errors.New("readmodel continuation invalid; restart required")
)

type Availability string

const (
	Available Availability = "available"
	Updating  Availability = "scope_updating"
	Disabled  Availability = "disabled"
)

// Scope is a repository-authenticated directory slot, including unavailable
// slots. Ready means audience, lifecycle, projection AND full-request legacy
// authorization budget gates passed for this generation. It defaults to false.
type Scope struct {
	ID           scopes.ID
	Generation   int64
	Availability Availability
	Ready        bool
}

type Stream = scopes.Stream

// Snapshot must be obtained in the same transaction as candidates, counts and
// hydration. Its binding includes principal authority, query/filter identity,
// grant generation, projection version and scope generations. Audience bindings
// must already be authorized; this package never filters candidates for privacy.
type Snapshot struct {
	Binding               string
	Scopes                []Scope
	Streams               []Stream
	MoreScopes            bool
	DirectoryContinuation string
	AsOf                  time.Time
}

// Key is internal and must never be serialized outside encrypted continuation.
type Key struct{ Sort, RID int64 }
type Candidate struct {
	Key     Key
	Version int64
}
type Reference struct {
	Stream    int
	Candidate Candidate
}
type Item struct {
	Ref  string          `json:"ref"`
	Data json.RawMessage `json:"data"`
}

// Page uses A's frozen shared response. Availability is explicit through
// Coverage.UnavailableScopeIDs; a nonempty list forbids treating empty items or
// absent counts as an available zero. Transport shaping remains lead-owned.
type Page = scopes.Page[Item]

func coverage(s Snapshot) scopes.Coverage {
	c := scopes.Coverage{CoveredScopeIDs: []scopes.ID{}, UnavailableScopeIDs: []scopes.ID{}, MoreScopes: s.MoreScopes, ScopeCursor: s.DirectoryContinuation, AsOf: s.AsOf}
	for _, scope := range s.Scopes {
		c.CoveredScopeIDs = append(c.CoveredScopeIDs, scope.ID)
		if scope.Availability != Available || !scope.Ready {
			c.UnavailableScopeIDs = append(c.UnavailableScopeIDs, scope.ID)
		}
	}
	return c
}

// Reader is a transaction-bound adapter implemented by the trusted repository.
// Candidates seeks (scope,generation,family,audience,sort,rid) before LIMIT;
// Hydrate performs one bounded batch, preserving order and validating versions.
// No method may rebuild legacy authorization separately for each row/statement.
type Reader interface {
	Snapshot(context.Context) (Snapshot, error)
	Candidates(context.Context, int, *Key, int) ([]Candidate, error)
	Hydrate(context.Context, []Reference) ([]Item, error)
}

func validateSnapshot(s Snapshot) (Availability, error) {
	if len(s.Scopes) > MaxScopes || len(s.Streams) > MaxStreams || s.Binding == "" || s.AsOf.IsZero() {
		return "", ErrBudget
	}
	selected := make(map[scopes.ID]int, len(s.Scopes))
	a := Available
	for _, scope := range s.Scopes {
		if scope.ID == "" || scope.Generation < 1 {
			return "", ErrProjection
		}
		if _, exists := selected[scope.ID]; exists {
			return "", ErrProjection
		}
		selected[scope.ID] = 0
		if scope.Availability == Updating {
			a = Updating
		} else if scope.Availability != Available {
			return "", ErrProjection
		}
		if !scope.Ready && a != Updating {
			a = Disabled
		}
	}
	seen := make(map[Stream]bool, len(s.Streams))
	for _, stream := range s.Streams {
		n, ok := selected[stream.Scope]
		if !ok || stream.Family == "" || stream.Audience == "" || seen[stream] {
			return "", ErrProjection
		}
		seen[stream] = true
		n++
		if n > MaxStreamsPerScope {
			return "", ErrBudget
		}
		selected[stream.Scope] = n
	}
	return a, nil
}

func before(a, b Key) bool { return a.Sort < b.Sort || a.Sort == b.Sort && a.RID < b.RID }

// Read admits at most S*(P+1) candidates and hydrates at most P items. The heap
// merge is O(S + P log S); repository seeks cost O(S log N + SP). A transitioning
// selection returns no items, including when other selected scopes are active.
func Read(ctx context.Context, r Reader, codec *CursorCodec, size int, token string) (Page, error) {
	if size < 1 || size > MaxPageSize || codec == nil {
		return Page{}, ErrBudget
	}
	s, err := r.Snapshot(ctx)
	if err != nil {
		return Page{}, err
	}
	a, err := validateSnapshot(s)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: []Item{}, Coverage: coverage(s)}
	if a != Available {
		return p, nil
	}
	heads := make([]*Key, len(s.Streams))
	if token != "" {
		heads, err = codec.decode(token, s)
		if err != nil {
			return Page{}, err
		}
	}
	batches := make([][]Candidate, len(s.Streams))
	h := candidateHeap{}
	admitted := make(map[int64]bool)
	for i := range s.Streams {
		batch, e := r.Candidates(ctx, i, heads[i], size+1)
		if e != nil {
			return Page{}, e
		}
		if len(batch) > size+1 {
			return Page{}, ErrBudget
		}
		for j, row := range batch {
			if row.Key.RID < 1 || row.Version < 1 || j == 0 && heads[i] != nil && !before(*heads[i], row.Key) || j > 0 && !before(batch[j-1].Key, row.Key) {
				return Page{}, ErrProjection
			}
			// Reject overlaps throughout admission, including lookahead rows that
			// would otherwise be deferred to a later page.
			if admitted[row.Key.RID] {
				return Page{}, ErrProjection
			}
			admitted[row.Key.RID] = true
		}
		batches[i] = batch
		if len(batch) > 0 {
			h = append(h, heapEntry{Reference: Reference{Stream: i, Candidate: batch[0]}})
		}
	}
	heap.Init(&h)
	refs := make([]Reference, 0, size)
	seen := make(map[int64]bool, size)
	for len(h) > 0 && len(refs) < size {
		e := heap.Pop(&h).(heapEntry)
		// Projections must assign disjoint audience identities before admission.
		// Reject corruption rather than scan additional rows to fill a page.
		if seen[e.Candidate.Key.RID] {
			return Page{}, ErrProjection
		}
		seen[e.Candidate.Key.RID] = true
		refs = append(refs, e.Reference)
		key := e.Candidate.Key
		heads[e.Stream] = &key
		e.index++
		if e.index < len(batches[e.Stream]) {
			e.Candidate = batches[e.Stream][e.index]
			heap.Push(&h, e)
		}
	}
	if len(refs) > 0 {
		p.Items, err = r.Hydrate(ctx, refs)
		if err != nil {
			return Page{}, err
		}
		if len(p.Items) != len(refs) {
			return Page{}, ErrProjection
		}
		for _, item := range p.Items {
			if item.Ref == "" || len(item.Ref) > 512 || len(item.Data) > MaxItemBytes || !json.Valid(item.Data) {
				return Page{}, ErrProjection
			}
		}
	}
	if len(h) > 0 {
		p.NextCursor, err = codec.encode(s, heads)
	}
	return p, err
}

type heapEntry struct {
	Reference
	index int
}
type candidateHeap []heapEntry

func (h candidateHeap) Len() int { return len(h) }
func (h candidateHeap) Less(i, j int) bool {
	a, b := h[i], h[j]
	if a.Candidate.Key == b.Candidate.Key {
		return a.Stream < b.Stream
	}
	return before(a.Candidate.Key, b.Candidate.Key)
}
func (h candidateHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *candidateHeap) Push(v any)   { *h = append(*h, v.(heapEntry)) }
func (h *candidateHeap) Pop() any     { n := len(*h) - 1; v := (*h)[n]; *h = (*h)[:n]; return v }
