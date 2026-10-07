package readmodel

import (
	"bytes"
	"container/heap"
	"context"
	"encoding/json"
)

const MaxOrderKeyBytes = 770

// OrderedKey is a bounded, opaque BLOB comparator captured from canonical
// fields. Inbox needs category ASC, trigger text DESC and canonical ID ASC.
// It must never be returned outside an encrypted continuation.
type OrderedKey struct {
	Order []byte
	RID   int64
}
type OrderedCandidate struct {
	Key     OrderedKey
	Version int64
}
type OrderedReference struct {
	Stream    int
	Candidate OrderedCandidate
}

// OrderedReader is a proposed transaction-bound adapter for A. Its candidate
// query must seek the entire BLOB/RID tuple BEFORE LIMIT. Hydration joins the
// exact admitted tuple, version and private registry in one batch. The current
// integer-key FeedReader cannot implement this interface without shared work.
type OrderedReader interface {
	Snapshot(context.Context) (Snapshot, error)
	OrderedCandidates(context.Context, int, *OrderedKey, int) ([]OrderedCandidate, error)
	HydrateOrdered(context.Context, []OrderedReference) ([]Item, error)
}

func orderedBefore(a, b OrderedKey) bool {
	c := bytes.Compare(a.Order, b.Order)
	return c < 0 || c == 0 && a.RID < b.RID
}
func validOrderedKey(k OrderedKey) bool {
	return len(k.Order) > 0 && len(k.Order) <= MaxOrderKeyBytes && k.RID > 0
}
func cloneOrderedKey(k OrderedKey) OrderedKey {
	return OrderedKey{Order: append([]byte(nil), k.Order...), RID: k.RID}
}

// ReadOrdered uses one global continuation key for every stream. With certified
// disjoint RIDs, the total comparator prevents page-size or scope-density changes
// from losing rows, and bounds the cursor independently of 256-stream fanout.
// Like Read, it admits <=S(P+1) tuples and hydrates <=P, with no post-LIMIT filter
// or refill scan. Batching/complete HTTP budgets remain A's serving gate.
func ReadOrdered(ctx context.Context, r OrderedReader, codec *CursorCodec, size int, token string) (Page, error) {
	if size < 1 || size > MaxPageSize || codec == nil || r == nil {
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
	var after *OrderedKey
	if token != "" {
		after, err = codec.decodeOrdered(token, s)
		if err != nil {
			return Page{}, err
		}
	}
	admitted := map[int64]bool{}
	batches := make([][]OrderedCandidate, len(s.Streams))
	h := orderedHeap{}
	for stream := range s.Streams {
		var seek *OrderedKey
		if after != nil {
			key := cloneOrderedKey(*after)
			seek = &key
		}
		batch, err := r.OrderedCandidates(ctx, stream, seek, size+1)
		if err != nil {
			return Page{}, err
		}
		if len(batch) > size+1 {
			return Page{}, ErrBudget
		}
		batches[stream] = make([]OrderedCandidate, len(batch))
		for j, c := range batch {
			if !validOrderedKey(c.Key) || c.Version < 1 || admitted[c.Key.RID] || j == 0 && after != nil && !orderedBefore(*after, c.Key) || j > 0 && !orderedBefore(batch[j-1].Key, c.Key) {
				return Page{}, ErrProjection
			}
			admitted[c.Key.RID] = true
			batches[stream][j] = OrderedCandidate{Key: cloneOrderedKey(c.Key), Version: c.Version}
		}
		if len(batch) > 0 {
			h = append(h, orderedHeapEntry{OrderedReference: OrderedReference{Stream: stream, Candidate: batches[stream][0]}})
		}
	}
	heap.Init(&h)
	refs := make([]OrderedReference, 0, size)
	for len(h) > 0 && len(refs) < size {
		e := heap.Pop(&h).(orderedHeapEntry)
		refs = append(refs, e.OrderedReference)
		e.index++
		if e.index < len(batches[e.Stream]) {
			e.Candidate = batches[e.Stream][e.index]
			heap.Push(&h, e)
		}
	}
	var last OrderedKey
	if len(refs) > 0 {
		last = cloneOrderedKey(refs[len(refs)-1].Candidate.Key)
		p.Items, err = r.HydrateOrdered(ctx, refs)
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
		p.NextCursor, err = codec.encodeOrdered(s, last)
	}
	return p, err
}

type orderedHeapEntry struct {
	OrderedReference
	index int
}
type orderedHeap []orderedHeapEntry

func (h orderedHeap) Len() int { return len(h) }
func (h orderedHeap) Less(i, j int) bool {
	return orderedBefore(h[i].Candidate.Key, h[j].Candidate.Key)
}
func (h orderedHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *orderedHeap) Push(v any)   { *h = append(*h, v.(orderedHeapEntry)) }
func (h *orderedHeap) Pop() any     { n := len(*h) - 1; v := (*h)[n]; *h = (*h)[:n]; return v }
