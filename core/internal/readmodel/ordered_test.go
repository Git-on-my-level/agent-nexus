package readmodel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/scopes"
)

type orderedMemoryReader struct {
	snapshot        Snapshot
	batches         [][]OrderedCandidate
	calls, hydrated int
	mutate          bool
}

func (r *orderedMemoryReader) Snapshot(context.Context) (Snapshot, error) { return r.snapshot, nil }
func (r *orderedMemoryReader) OrderedCandidates(_ context.Context, stream int, after *OrderedKey, limit int) ([]OrderedCandidate, error) {
	r.calls++
	var result []OrderedCandidate
	for _, c := range r.batches[stream] {
		if after == nil || orderedBefore(*after, c.Key) {
			result = append(result, c)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}
func (r *orderedMemoryReader) HydrateOrdered(_ context.Context, refs []OrderedReference) ([]Item, error) {
	r.hydrated += len(refs)
	var result []Item
	for _, ref := range refs {
		result = append(result, Item{Ref: fmt.Sprint(ref.Candidate.Key.RID), Data: json.RawMessage(`{}`)})
		if r.mutate {
			ref.Candidate.Key.Order[0] = 255
		}
	}
	return result, nil
}
func orderedFixture() *orderedMemoryReader {
	r := &orderedMemoryReader{snapshot: Snapshot{Binding: "principal/epoch/receipts", AsOf: time.Now().UTC()}}
	for i := 0; i < 64; i++ {
		id := scopes.ID(fmt.Sprintf("scope-%d", i))
		r.snapshot.Scopes = append(r.snapshot.Scopes, Scope{ID: id, Generation: 1, Availability: Available, Ready: true})
		for j := 0; j < 4; j++ {
			stream := len(r.batches)
			r.snapshot.Streams = append(r.snapshot.Streams, Stream{Scope: id, Family: "inbox", Audience: fmt.Sprint(j)})
			r.batches = append(r.batches, []OrderedCandidate{{Key: OrderedKey{Order: []byte(fmt.Sprintf("private-canonical-%04d", stream)), RID: int64(stream + 1)}, Version: 1}})
		}
	}
	return r
}
func TestOrderedMaximumFanoutContinuationAndContextBinding(t *testing.T) {
	ctx := context.Background()
	r := orderedFixture()
	c := orderedCodec(t)
	seen := map[string]bool{}
	cursor := ""
	last := 0
	for page := 0; page < 10; page++ {
		size := []int{1, 100, 3, 100, 100}[page%5]
		before := r.calls
		p, err := ReadOrdered(ctx, r, c, size, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if r.calls-before != 256 || len(p.Items) > size || len(p.Coverage.CoveredScopeIDs) != 64 {
			t.Fatal("budget/coverage", r.calls-before, len(p.Items))
		}
		for _, item := range p.Items {
			var rid int
			if _, err := fmt.Sscan(item.Ref, &rid); err != nil {
				t.Fatal(err)
			}
			if seen[item.Ref] || rid <= last {
				t.Fatal("lost/duplicated/global comparator", item.Ref)
			}
			seen[item.Ref] = true
			last = rid
		}
		cursor = p.NextCursor
		if len(cursor) > 2048 {
			t.Fatal("fanout-sized cursor", len(cursor))
		}
		if cursor != "" {
			raw, _ := base64.RawURLEncoding.DecodeString(cursor)
			if strings.Contains(string(raw), "private-canonical-") {
				t.Fatal("cursor exposed private key")
			}
			if _, err := c.decode(cursor, r.snapshot); !errors.Is(err, ErrCursor) {
				t.Fatal("accepted ordered token as integer token", err)
			}
			other := r.snapshot
			other.Binding = "regranted-principal"
			if _, err := c.decodeOrdered(cursor, other); !errors.Is(err, ErrCursor) {
				t.Fatal("cross-authority cursor", err)
			}
		}
		if cursor == "" {
			break
		}
	}
	if len(seen) != 256 || r.hydrated != 256 {
		t.Fatal("incomplete page chain", len(seen), r.hydrated)
	}
	key := OrderedKey{Order: []byte(strings.Repeat("x", MaxOrderKeyBytes)), RID: 1}
	max, err := c.encodeOrdered(r.snapshot, key)
	if err != nil || len(max) > 2048 {
		t.Fatal("maximum cursor", len(max), err)
	}
	if _, err := c.decodeOrdered(max, r.snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestOrderedAdmissionRefusesLookaheadOverlapAndUnavailableZero(t *testing.T) {
	r := orderedFixture()
	r.batches[1][0].Key.RID = r.batches[0][0].Key.RID
	if _, err := ReadOrdered(context.Background(), r, orderedCodec(t), 1, ""); !errors.Is(err, ErrProjection) || r.hydrated != 0 {
		t.Fatal("overlap hydrated", err, r.hydrated)
	}
	r = orderedFixture()
	r.snapshot.Scopes[63].Ready = false
	p, err := ReadOrdered(context.Background(), r, orderedCodec(t), 10, "")
	if err != nil || len(p.Items) != 0 || len(p.Coverage.UnavailableScopeIDs) != 1 || r.calls != 0 {
		t.Fatal("synthetic available zero", p, err, r.calls)
	}
	r.snapshot.Scopes[63].Availability = Updating
	if _, err := ReadOrdered(context.Background(), r, orderedCodec(t), 10, ""); err != nil || r.calls != 0 {
		t.Fatal("transitioning served", err)
	}
}

func TestOrderedHydrationCannotChangeContinuation(t *testing.T) {
	r := orderedFixture()
	r.mutate = true
	c := orderedCodec(t)
	p, err := ReadOrdered(context.Background(), r, c, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	after, err := c.decodeOrdered(p.NextCursor, r.snapshot)
	if err != nil || string(after.Order) != "private-canonical-0000" || after.RID != 1 {
		t.Fatal("hydrator changed cursor", after, err)
	}
}

func orderedCodec(t *testing.T) *CursorCodec {
	t.Helper()
	c, err := NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
