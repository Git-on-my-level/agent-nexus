package readmodel

import (
	"context"
	"errors"
	"math"
	"testing"
)

func projection(version int64, audience string, buckets ...string) *Projection {
	return &Projection{ScopeID: "scope", Generation: 1, RID: 42, Version: version, Entries: []Entry{{Family: "inbox", Audience: audience, Sort: 1, Buckets: buckets}}}
}

func TestDeltaAnswerMovesExactBuckets(t *testing.T) {
	before := projection(1, "person:a", "open", "total")
	after := projection(2, "person:a", "answered", "total")
	d, e := PlanDelta(before, after)
	if e != nil {
		t.Fatal(e)
	}
	if len(d.Feeds) != 2 || !d.Feeds[0].Delete || d.Feeds[1].Delete || len(d.Counters) != 2 {
		t.Fatalf("%+v", d)
	}
	values := map[string]int64{"total": 1, "open": 1, "answered": 0}
	for _, change := range d.Counters {
		values[change.Key.Bucket] += change.Delta
	}
	if values["total"] != 1 || values["open"] != 0 || values["answered"] != 1 {
		t.Fatal(values)
	}
	after.Entries[0].Buckets[0] = "mutated"
	if d.Feeds[1].Entry.Buckets[0] != "answered" {
		t.Fatal("plan aliases caller fields")
	}
}

func TestDeltaFanoutAndCrossScopeRefusal(t *testing.T) {
	p := projection(1, "person:a", "a", "b", "c", "d")
	for i := 0; i < 3; i++ {
		e := p.Entries[0]
		e.Audience = string(rune('b' + i))
		p.Entries = append(p.Entries, e)
	}
	if _, e := PlanDelta(nil, p); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
	old := projection(1, "all", "open")
	next := projection(2, "all", "open")
	next.ScopeID = "other"
	if _, e := PlanDelta(old, next); !errors.Is(e, ErrProjection) {
		t.Fatal(e)
	}
	if _, e := PlanDelta(old, old); !errors.Is(e, ErrProjection) {
		t.Fatal("ordinary write accepted stale version", e)
	}
}

func TestCountsRejectMissingAndNegativeBuckets(t *testing.T) {
	f := fixture(1)
	for _, counts := range []map[string]int64{nil, {"open": -1}, {"other": 1}} {
		f.counts = counts
		if _, e := Count(context.Background(), f, []string{"open"}); !errors.Is(e, ErrProjection) {
			t.Fatal(e)
		}
	}
	f.counts = map[string]int64{"open": 0}
	if got, e := Count(context.Background(), f, []string{"open"}); e != nil || got.Values["open"] != 0 {
		t.Fatalf("%+v %v", got, e)
	}
}

func TestRankFullRangeAndGapRefusal(t *testing.T) {
	for _, pair := range [][2]int64{{math.MinInt64, math.MaxInt64}, {-10, 10}, {math.MinInt64, math.MinInt64 + 2}, {math.MaxInt64 - 2, math.MaxInt64}} {
		got, e := RankBetween(&pair[0], &pair[1])
		if e != nil || got <= pair[0] || got >= pair[1] {
			t.Fatalf("%v -> %d %v", pair, got, e)
		}
	}
	for _, pair := range [][2]int64{{1, 2}, {1, 1}, {2, 1}} {
		if _, e := RankBetween(&pair[0], &pair[1]); !errors.Is(e, ErrRankGap) {
			t.Fatal(pair, e)
		}
	}
	max, min := int64(math.MaxInt64), int64(math.MinInt64)
	if _, e := RankBetween(&max, nil); !errors.Is(e, ErrRankGap) {
		t.Fatal(e)
	}
	if _, e := RankBetween(nil, &min); !errors.Is(e, ErrRankGap) {
		t.Fatal(e)
	}
}
