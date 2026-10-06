package scopestream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/scopes"
)

type unopenedRepo struct{ calls int }

func (r *unopenedRepo) ReadStreams(context.Context, string, []Stream, func(Snapshot) error) error {
	r.calls++
	return ErrUnavailable
}
func testTokens(t *testing.T) *Tokens {
	t.Helper()
	tokens, err := NewTokens([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}
func TestStreamCapsDuplicatesAndInvalidCursorBeforeRepository(t *testing.T) {
	base := Request{Principal: "reader", Streams: []Stream{{Scope: "s", Family: "inbox", Audience: "all"}}, Limit: 1}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.Limit = 101 },
		func(r *Request) { r.OutputBytes = MaxPayloadBytes },
		func(r *Request) { r.Streams = append(r.Streams, r.Streams[0]) },
		func(r *Request) {
			r.Streams = nil
			for i := 0; i < 5; i++ {
				r.Streams = append(r.Streams, Stream{Scope: "s", Family: fmt.Sprint(i), Audience: "all"})
			}
		},
		func(r *Request) {
			r.Streams = nil
			for i := 0; i < 65; i++ {
				r.Streams = append(r.Streams, Stream{Scope: scopes.ID(fmt.Sprint(i)), Family: "inbox", Audience: "all"})
			}
		},
		func(r *Request) { r.Streams = make([]Stream, 257) },
		func(r *Request) { r.Continuation = "bad" },
	} {
		req := base
		mutate(&req)
		repo := &unopenedRepo{}
		if _, err := Tick(context.Background(), repo, testTokens(t), req); err == nil || repo.calls != 0 {
			t.Fatalf("invalid selection opened repository: %v calls=%d", err, repo.calls)
		}
	}
}

type fixtureRepo struct {
	binding Binding
	payload json.RawMessage
	calls   int
}

func (r *fixtureRepo) ReadStreams(ctx context.Context, p string, s []Stream, fn func(Snapshot) error) error {
	r.calls++
	return fn(r)
}
func (r *fixtureRepo) Binding() Binding { return r.binding }
func (r *fixtureRepo) Heads(_ context.Context, s Stream, after int64, limit int) ([]Head, error) {
	if after >= 1 {
		return nil, nil
	}
	return []Head{{Sequence: 1, RID: strings.Repeat("r", 200), Version: strings.Repeat("v", 200), PayloadBytes: len(r.payload)}}, nil
}
func (r *fixtureRepo) Records(_ context.Context, keys []Key) ([]Record, error) {
	out := make([]Record, len(keys))
	for i, k := range keys {
		out[i] = Record{k, r.payload}
	}
	return out, nil
}
func Test256StreamsChargeFullEncryptedPrefixTokensAndPreserveHeads(t *testing.T) {
	repo := &fixtureRepo{binding: Binding{Principal: "reader", AuthorityGeneration: 1}, payload: json.RawMessage(`"` + strings.Repeat("x", MaxPayloadBytes-2) + `"`)}
	req := Request{Principal: "reader", Limit: 100}
	for scope := 0; scope < 64; scope++ {
		for family := 0; family < 4; family++ {
			s := Stream{Scope: scopes.ID(fmt.Sprint(scope)), Family: fmt.Sprint(family), Audience: "all"}
			req.Streams = append(req.Streams, s)
			repo.binding.Streams = append(repo.binding.Streams, StreamGeneration{Stream: s, ScopeGeneration: 1, BindingGeneration: 1, AudienceGeneration: 1, RetentionGeneration: 1})
		}
	}
	page, err := Tick(context.Background(), repo, testTokens(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 || len(page.Items) >= 100 || page.ExaminedHeads != 256 || page.OutputBytes > MaxOutputBytes {
		t.Fatalf("wire bytes not bounded: items=%d bytes=%d", len(page.Items), page.OutputBytes)
	}
	actual := 0
	for _, item := range page.Items {
		actual += len(item.Continuation) + len(item.Record.Payload) + MaxEnvelopeBytes
	}
	if page.OutputBytes != actual {
		t.Fatalf("cursor bytes omitted: reported=%d actual=%d", page.OutputBytes, actual)
	}
	var cursor tickCursor
	if err = testTokens(t).open(page.Items[0].Continuation, &cursor); err != nil {
		t.Fatal(err)
	}
	advanced, pending := 0, 0
	for _, p := range cursor.Positions {
		if p.After == 1 {
			advanced++
		}
		if p.Pending != nil {
			pending++
		}
	}
	if advanced != 1 || pending != 255 {
		t.Fatalf("prefix token dropped un-emitted heads: advanced=%d pending=%d", advanced, pending)
	}
}

type countingWriter struct {
	scope string
	calls int
	last  []Change
}

func (w *countingWriter) ScopeID() string { return w.scope }
func (w *countingWriter) AppendChanges(_ context.Context, c []Change) error {
	w.calls++
	w.last = c
	return nil
}

func TestFrozenCanonicalDeltaAndSharedTickContract(t *testing.T) {
	w := &countingWriter{scope: "s"}
	d := scopes.Change{ScopeID: "s", Kind: "comment", ResourceID: strings.Repeat("r", 512), CanonicalVersion: 7, Family: "events", Audience: "all", After: &scopes.Projection{Text: strings.Repeat("x", 60000), Timestamp: 1}}
	if err := ApplyCanonical(context.Background(), w, []scopes.Change{d}); err != nil {
		t.Fatal(err)
	}
	if w.last[0].Version != "7" || len(w.last[0].Payload.Bytes()) > MaxPayloadBytes || len(w.last[0].RID) != 512 {
		t.Fatal("canonical delta adapter violated bounds")
	}
	if err := ApplyCanonical(context.Background(), w, []scopes.Change{d, d}); !errors.Is(err, ErrProjection) || w.calls != 1 {
		t.Fatal("invalid fanout reached canonical writer")
	}
	s := Stream{Scope: "s", Family: "events", Audience: "all"}
	repo := &fixtureRepo{binding: Binding{Principal: "reader", AuthorityGeneration: 1, Streams: []StreamGeneration{{Stream: s, ScopeGeneration: 1, BindingGeneration: 1, AudienceGeneration: 1, RetentionGeneration: 1}}}, payload: json.RawMessage(`{}`)}
	store := &Store{Repository: repo, Tokens: testTokens(t)}
	req := scopes.TickRequest{Selection: scopes.RequestSelection{Principal: "reader", ScopeIDs: []scopes.ID{"s"}}, Streams: []Stream{s}, Limit: 100}
	page, err := store.Tick(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" || len(page.Coverage.CoveredScopeIDs) != 1 {
		t.Fatal("shared Tick contract lost prefix cursor or coverage")
	}
	req.Selection.ScopeIDs = []scopes.ID{"other"}
	repo.calls = 0
	if _, err = store.Tick(context.Background(), req); !errors.Is(err, ErrBudget) || repo.calls != 0 {
		t.Fatal("shared selection ignored before repository")
	}
}
func TestStreamFanoutAndScopePinningBeforeMutation(t *testing.T) {
	payload, err := PreparePayload("public", json.RawMessage(`{"text":"safe"}`))
	if err != nil {
		t.Fatal(err)
	}
	private, err := PreparePayload("private", json.RawMessage(`{"text":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	w := &countingWriter{scope: "public"}
	base := Change{Stream: Stream{Scope: "public", Family: "events", Audience: "all"}, RID: "r", Version: "1", Payload: payload}
	if err = Apply(context.Background(), w, []Change{base, base}); !errors.Is(err, ErrProjection) {
		t.Fatal("duplicate stream admitted")
	}
	bad := base
	bad.Payload = private
	if err = Apply(context.Background(), w, []Change{bad}); !errors.Is(err, ErrDerivation) {
		t.Fatal("cross-scope payload admitted")
	}
	if err = Apply(context.Background(), w, make([]Change, 5)); !errors.Is(err, ErrBudget) {
		t.Fatal("fanout overflow admitted")
	}
	if w.calls != 0 {
		t.Fatal("invalid fanout mutated streams")
	}
	if err = Apply(context.Background(), w, []Change{base}); err != nil || w.calls != 1 {
		t.Fatal(err)
	}
	copy := payload.Bytes()
	copy[0] = '!'
	if !json.Valid(payload.Bytes()) {
		t.Fatal("payload escaped by mutable alias")
	}
}
