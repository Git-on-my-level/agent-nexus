package readmodel

import (
	"agent-nexus-core/internal/scopes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

type fixtureReader struct {
	s                       Snapshot
	rows                    [][]Candidate
	visits, hydrated, calls int
	counts                  map[string]int64
}

func (f *fixtureReader) Snapshot(context.Context) (Snapshot, error) { return f.s, nil }
func (f *fixtureReader) Candidates(_ context.Context, stream int, after *Key, limit int) ([]Candidate, error) {
	f.calls++
	var out []Candidate
	for _, row := range f.rows[stream] {
		if after == nil || before(*after, row.Key) {
			out = append(out, row)
			if len(out) == limit {
				break
			}
		}
	}
	f.visits += len(out)
	return out, nil
}
func (f *fixtureReader) Hydrate(_ context.Context, refs []Reference) ([]Item, error) {
	f.hydrated += len(refs)
	out := make([]Item, len(refs))
	for i, r := range refs {
		out[i] = Item{Ref: fmt.Sprintf("card:opaque-%d", r.Candidate.Key.RID), Data: json.RawMessage(`{"title":"visible"}`)}
	}
	return out, nil
}
func (f *fixtureReader) Buckets(context.Context, []string) (map[string]int64, error) {
	f.calls++
	return f.counts, nil
}
func fixture(n int) *fixtureReader {
	f := &fixtureReader{s: Snapshot{Binding: "principal/grant/query/version", AsOf: time.Now().UTC()}, rows: make([][]Candidate, n)}
	for i := 0; i < n; i++ {
		id := scopes.ID(fmt.Sprintf("scope-%d", i))
		f.s.Scopes = append(f.s.Scopes, Scope{id, 1, Available, true})
		f.s.Streams = append(f.s.Streams, Stream{Scope: id, Family: "work", Audience: "all"})
	}
	return f
}
func codec(t *testing.T) *CursorCodec {
	t.Helper()
	c, e := NewCursorCodec(make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestReadPreservesUnemittedHeadsAcrossPages(t *testing.T) {
	f := fixture(3)
	f.rows = [][]Candidate{{{Key{1, 1}, 1}, {Key{4, 4}, 1}}, {{Key{2, 2}, 1}, {Key{5, 5}, 1}}, {{Key{3, 3}, 1}, {Key{6, 6}, 1}}}
	c := codec(t)
	token := ""
	var got []string
	for page := 0; page < 3; page++ {
		p, e := Read(context.Background(), f, c, 2, token)
		if e != nil {
			t.Fatal(e)
		}
		for _, item := range p.Items {
			got = append(got, item.Ref)
		}
		token = p.NextCursor
		if page < 2 && token == "" {
			t.Fatal("lost pending streams")
		}
	}
	want := []string{"card:opaque-1", "card:opaque-2", "card:opaque-3", "card:opaque-4", "card:opaque-5", "card:opaque-6"}
	if !reflect.DeepEqual(got, want) || token != "" || f.hydrated != 6 {
		t.Fatalf("got %v token=%q hydrated=%d", got, token, f.hydrated)
	}
}

func TestReadBounded64ScopeCoverage(t *testing.T) {
	f := fixture(64)
	f.s.MoreScopes = true
	f.s.DirectoryContinuation = "opaque-directory-token"
	for i := range f.rows {
		for j := 0; j < 1000; j++ {
			f.rows[i] = append(f.rows[i], Candidate{Key{int64(j), int64(i*1000 + j + 1)}, 1})
		}
	}
	p, e := Read(context.Background(), f, codec(t), 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if f.visits != 128 || f.hydrated != 1 || len(p.Coverage.CoveredScopeIDs) != 64 || !p.Coverage.MoreScopes || p.Coverage.ScopeCursor != f.s.DirectoryContinuation || p.NextCursor == "" {
		t.Fatalf("page=%+v visits=%d hydrated=%d", p, f.visits, f.hydrated)
	}
	f.s.Scopes = append(f.s.Scopes, Scope{"extra", 1, Available, true})
	if _, e = Read(context.Background(), f, codec(t), 1, ""); !errors.Is(e, ErrBudget) {
		t.Fatal(e)
	}
}

func TestUnavailableSelectionHasNoCountsOrCandidates(t *testing.T) {
	for _, state := range []Availability{Updating, Disabled} {
		f := fixture(2)
		f.counts = map[string]int64{"open": 99}
		if state == Updating {
			f.s.Scopes[1].Availability = Updating
		} else {
			f.s.Scopes[1].Ready = false
		}
		p, e := Read(context.Background(), f, codec(t), 1, "")
		if e != nil {
			t.Fatal(e)
		}
		counts, e := Count(context.Background(), f, []string{"open"})
		if e != nil {
			t.Fatal(e)
		}
		want := []scopes.ID{f.s.Scopes[1].ID}
		if !reflect.DeepEqual(p.Coverage.UnavailableScopeIDs, want) || !reflect.DeepEqual(counts.Coverage.UnavailableScopeIDs, want) || counts.Values != nil || len(p.Items) != 0 || f.calls != 0 {
			t.Fatalf("state=%s page=%+v counts=%+v calls=%d", state, p, counts, f.calls)
		}
	}
}

func TestCursorRejectsChangedAuthorityCoverageAndTampering(t *testing.T) {
	f := fixture(1)
	f.rows[0] = []Candidate{{Key{1, 1}, 1}, {Key{2, 2}, 1}}
	c := codec(t)
	p, e := Read(context.Background(), f, c, 1, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(){func() { f.s.Binding = "other-principal" }, func() { f.s.Scopes[0].Generation++ }, func() { f.s.Streams[0].Audience = "other" }, func() { f.s.MoreScopes = true }} {
		saved := f.s
		saved.Scopes = append([]Scope(nil), f.s.Scopes...)
		saved.Streams = append([]Stream(nil), f.s.Streams...)
		change()
		if _, e = Read(context.Background(), f, c, 1, p.NextCursor); !errors.Is(e, ErrCursor) {
			t.Fatal(e)
		}
		f.s = saved
	}
	tampered := []byte(p.NextCursor)
	tampered[len(tampered)/2] ^= 1
	if _, e = Read(context.Background(), f, c, 1, string(tampered)); !errors.Is(e, ErrCursor) {
		t.Fatal(e)
	}
	other, _ := NewCursorCodec(append([]byte{1}, make([]byte, 31)...))
	if _, e = Read(context.Background(), f, other, 1, p.NextCursor); !errors.Is(e, ErrCursor) {
		t.Fatal(e)
	}
}

func TestReadRejectsCorruptRangeWithoutHydration(t *testing.T) {
	for _, rows := range [][]Candidate{{{Key{2, 2}, 1}, {Key{1, 1}, 1}}, {{Key{1, 1}, 0}}, {{Key{1, 0}, 1}}} {
		f := fixture(1)
		f.rows[0] = rows
		if _, e := Read(context.Background(), f, codec(t), 2, ""); !errors.Is(e, ErrProjection) || f.hydrated != 0 {
			t.Fatalf("err=%v hydrated=%d", e, f.hydrated)
		}
	}
	f := fixture(2)
	f.rows[0] = []Candidate{{Key{1, 1}, 1}}
	f.rows[1] = []Candidate{{Key{1, 1}, 1}}
	if _, e := Read(context.Background(), f, codec(t), 2, ""); !errors.Is(e, ErrProjection) {
		t.Fatal(e)
	}
}

func TestStreamFanoutRefusedBeforeReading(t *testing.T) {
	f := fixture(1)
	for i := 0; i < 4; i++ {
		f.s.Streams = append(f.s.Streams, Stream{Scope: "scope-0", Family: "work", Audience: fmt.Sprintf("role-%d", i)})
	}
	if _, e := Read(context.Background(), f, codec(t), 1, ""); !errors.Is(e, ErrBudget) || f.calls != 0 {
		t.Fatalf("err=%v calls=%d", e, f.calls)
	}
}
