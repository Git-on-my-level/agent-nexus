package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/scopesearch"
	"agent-nexus-core/internal/scopestream"
	streamhttp "agent-nexus-core/internal/server/stream"
)

func scopeCStreamTokens(t *testing.T) *scopestream.Tokens {
	t.Helper()
	tokens, err := scopestream.NewTokens([]byte(strings.Repeat("t", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}
func scopeCStreamHTTP(t *testing.T, c *scopeCRepository, streams []scopestream.Stream, limit int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	streamhttp.Mount(mux, "scope-test", streamhttp.ScopeHandler(streamhttp.ScopeOptions{Repository: c, Tokens: scopeCStreamTokens(t), Ready: func() bool { return true }, PollInterval: 20 * time.Millisecond, Selection: func(*http.Request) (scopestream.Request, error) {
		return scopestream.Request{Principal: "reader", Streams: streams, Limit: limit}, nil
	}}))
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func TestScopeStreamHTTP64ScopesWrongAudienceAndUnavailableScopes(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	var streams []scopestream.Stream
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 10064; i++ {
		id := fmt.Sprintf("s%05d", i)
		state := "transitioning"
		if i < 64 {
			state = "active"
		} else if i%2 == 0 {
			state = "inaccessible"
		}
		if _, err = tx.Exec(`INSERT INTO scope_domains VALUES(?,?,1)`, id, state); err != nil {
			t.Fatal(err)
		}
		if state != "inaccessible" {
			if _, err = tx.Exec(`INSERT INTO scope_memberships VALUES('reader',?,'reader',1)`, id); err != nil {
				t.Fatal(err)
			}
		}
		if i >= 64 {
			continue
		}
		streams = append(streams, scopestream.Stream{Scope: scopes.ID(id), Family: "inbox", Audience: "person:reader"})
		for seq := 1; seq <= 1000; seq++ {
			payload := `{"text":"wrong-audience-private"}`
			if _, err = tx.Exec(`INSERT INTO scope_changes VALUES(?,'inbox','person:stranger',?,?,'v1',?,?)`, id, seq, fmt.Sprint(seq), payload, len(payload)); err != nil {
				t.Fatal(err)
			}
		}
		payload := `{"text":"visible"}`
		if _, err = tx.Exec(`INSERT INTO scope_changes VALUES(?,'inbox','person:reader',1,?,'v1',?,?)`, id, id, payload, len(payload)); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO scope_stream_sequences VALUES(?,'inbox','person:reader',1,1,0)`, id); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO scope_stream_sequences VALUES(?,'inbox','person:stranger',1000,1,0)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(`INSERT INTO scope_principal_generations VALUES('reader',1)`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	req := scopestream.Request{Principal: "reader", Streams: streams, Limit: 1}
	scopeCExamined.Store(0)
	page, err := scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || scopeCExamined.Load() != 64 || page.ExaminedHeads != 64 {
		t.Fatalf("tick scanned wrong audience or sealed directory: items=%d examined=%d", len(page.Items), scopeCExamined.Load())
	}
	seen := map[scopes.ID]bool{page.Items[0].Record.Key.Stream.Scope: true}
	for len(seen) < 64 {
		req.Continuation = page.Continuation
		page, err = scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req)
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("lost pending stream heads: %#v %v", page, err)
		}
		id := page.Items[0].Record.Key.Stream.Scope
		if seen[id] {
			t.Fatalf("duplicate %s", id)
		}
		seen[id] = true
	}
	req.Continuation = page.Continuation
	scopeCExamined.Store(0)
	idle, err := scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req)
	if err != nil || len(idle.Items) != 0 || idle.Continuation != req.Continuation || scopeCExamined.Load() != 0 {
		t.Fatalf("idle token/seek changed: %#v %v", idle, err)
	}
	c.streamWrite(t, scopestream.Stream{Scope: "s00000", Family: "inbox", Audience: "person:stranger"}, "hidden-next", "hidden new activity")
	idle, err = scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req)
	if err != nil || len(idle.Items) != 0 || idle.Continuation != req.Continuation {
		t.Fatal("idle token revealed hidden activity")
	}
	// The production transport exercises the same durable snapshot/seek path.
	s := scopeCStreamHTTP(t, c, streams, 1)
	resp := openSSEStream(t, s.URL+"/stream/scope-test", "")
	reader, stop := startSSEReader(resp.Body)
	defer stop()
	for i := 0; i < 64; i++ {
		event := awaitSSEEvent(t, reader, 2*time.Second)
		body, _ := json.Marshal(event.Data)
		if event.ID == "" || event.Event != "change" || strings.Contains(string(body), "wrong-audience") {
			t.Fatalf("bad scoped SSE: %#v", event)
		}
	}
	stop()
	// A selected unavailable slot fails before change queries; unselected slots
	// above never consume the scope/stream budget or force a directory scan.
	req.Streams = append([]scopestream.Stream(nil), streams[:1]...)
	req.Streams = append(req.Streams, scopestream.Stream{Scope: "s00065", Family: "inbox", Audience: "person:reader"})
	req.Continuation = ""
	scopeCExamined.Store(0)
	if _, err = scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req); !errors.Is(err, scopes.ErrUpdating) || scopeCExamined.Load() != 0 {
		t.Fatalf("selected unavailable scope queried changes: %v", err)
	}
	req.Streams[1].Scope = "s00064"
	if _, err = scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req); !errors.Is(err, scopes.ErrDenied) || scopeCExamined.Load() != 0 {
		t.Fatalf("sealed no-grants scope queried changes: %v", err)
	}
}

func TestScopeStreamHTTPReconnectAcknowledgesOnlyEmittedPrefix(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "s", "reader", "active")
	stream := scopestream.Stream{Scope: "s", Family: "events", Audience: "all"}
	for i := 0; i < 10; i++ {
		c.streamWrite(t, stream, fmt.Sprint(i), "visible")
	}
	s := scopeCStreamHTTP(t, c, []scopestream.Stream{stream}, 8)
	resp := openSSEStream(t, s.URL+"/stream/scope-test", "")
	reader, stop := startSSEReader(resp.Body)
	first := awaitSSEEvent(t, reader, 2*time.Second)
	stop()
	if first.Data["rid"] != "0" {
		t.Fatalf("wrong first event: %#v", first)
	}
	resp = openSSEStream(t, s.URL+"/stream/scope-test", first.ID)
	reader, stop = startSSEReader(resp.Body)
	defer stop()
	for i := 1; i < 10; i++ {
		event := awaitSSEEvent(t, reader, 2*time.Second)
		if event.Data["rid"] != fmt.Sprint(i) {
			t.Fatalf("reconnect skipped unemitted record %d: %#v", i, event)
		}
	}
}

func TestScopeStreamHTTPReauthorizationAndBindingValidation(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "s", "reader", "active")
	stream := scopestream.Stream{Scope: "s", Family: "inbox", Audience: "person:reader"}
	c.streamWrite(t, stream, "first", "visible")
	s := scopeCStreamHTTP(t, c, []scopestream.Stream{stream}, 1)
	resp := openSSEStream(t, s.URL+"/stream/scope-test", "")
	reader, stop := startSSEReader(resp.Body)
	defer stop()
	first := awaitSSEEvent(t, reader, 2*time.Second)
	scopeCExec(t, c.db, `DELETE FROM scope_memberships WHERE principal='reader' AND scope_id='s'; UPDATE scope_principal_generations SET generation=generation+1 WHERE principal='reader'`)
	reset := awaitSSEEvent(t, reader, 2*time.Second)
	if reset.Event != "reset" || reset.ID != "" || len(reset.Data) != 0 {
		t.Fatalf("revocation serialized shared error: %#v", reset)
	}
	stop()
	// Regrant with a newer principal epoch: old encrypted resume remains invalid.
	scopeCExec(t, c.db, `INSERT INTO scope_memberships VALUES('reader','s','reader',2)`)
	req := scopestream.Request{Principal: "reader", Streams: []scopestream.Stream{stream}, Limit: 1, Continuation: first.ID}
	if _, err := scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req); !errors.Is(err, scopestream.ErrRestart) {
		t.Fatalf("old grant resume accepted: %v", err)
	}
	req.Continuation = ""
	req.Streams = append(req.Streams, scopestream.Stream{Scope: "s", Family: "inbox", Audience: "person:stranger"})
	scopeCExamined.Store(0)
	if _, err := scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req); err == nil || scopeCExamined.Load() != 0 {
		t.Fatal("wrong binding validated after queries")
	}
	role := scopestream.Stream{Scope: "s", Family: "inbox", Audience: "role:reviewers"}
	scopeCExec(t, c.db, `INSERT INTO scope_stream_bindings VALUES('reader','s','role:reviewers',1)`)
	c.streamWrite(t, role, "role-first", "visible role")
	req.Streams = []scopestream.Stream{role}
	page, err := scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Continuation = page.Continuation
	scopeCExec(t, c.db, `UPDATE scope_stream_bindings SET generation=2 WHERE principal='reader' AND scope_id='s' AND audience_key='role:reviewers'`)
	scopeCExamined.Store(0)
	if _, err = scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req); !errors.Is(err, scopestream.ErrRestart) || scopeCExamined.Load() != 0 {
		t.Fatalf("role generation not bound before query: %v", err)
	}
	scopeCExec(t, c.db, `DELETE FROM scope_stream_bindings WHERE principal='reader' AND scope_id='s' AND audience_key='role:reviewers'; UPDATE scope_principal_generations SET generation=generation+1 WHERE principal='reader'`)
	scopeCExamined.Store(0)
	if _, err = scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req); err == nil || scopeCExamined.Load() != 0 {
		t.Fatal("revoked role binding consumed change range")
	}
}

func TestScopeStreamRetentionResyncAndNoTokenTampering(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "s", "reader", "active")
	stream := scopestream.Stream{Scope: "s", Family: "events", Audience: "all"}
	c.streamWrite(t, stream, "r", "visible")
	req := scopestream.Request{Principal: "reader", Streams: []scopestream.Stream{stream}, Limit: 1}
	page, err := scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Continuation = page.Continuation
	for _, mutate := range []func(*scopestream.Request){func(r *scopestream.Request) { r.Principal = "another" }, func(r *scopestream.Request) {
		r.Streams = []scopestream.Stream{{Scope: "s", Family: "inbox", Audience: "all"}}
	}, func(r *scopestream.Request) { r.Continuation = "!" + r.Continuation }} {
		r := req
		mutate(&r)
		scopeCExamined.Store(0)
		if _, err = scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), r); !errors.Is(err, scopestream.ErrCursor) || scopeCExamined.Load() != 0 {
			t.Fatal("unbound cursor queried ranges")
		}
	}
	scopeCExec(t, c.db, `UPDATE scope_stream_sequences SET retention_generation=2,compacted_through=1 WHERE scope_id='s' AND family='events' AND audience_key='all'`)
	if _, err = scopestream.Tick(context.Background(), c, scopeCStreamTokens(t), req); !errors.Is(err, scopestream.ErrResync) {
		t.Fatalf("compacted resume accepted: %v", err)
	}
}

func TestScopeStreamAndSearchRejectCrossScopeDerivations(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "public", "owner", "active")
	c.grant(t, "private", "owner", "active")
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	writer := &scopeCWriter{tx, "public"}
	search := scopesearch.Change{Scope: "public", Kind: "document", RID: "leak", Version: "v1", Content: scopesearch.Prepare("private", "private sentinel")}
	if err = scopesearch.Apply(context.Background(), writer, search); !errors.Is(err, scopesearch.ErrDerivation) {
		t.Fatalf("private posting derivation accepted: %v", err)
	}
	payload, err := scopestream.PreparePayload("private", json.RawMessage(`{"text":"private sentinel"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = scopestream.Apply(context.Background(), writer, []scopestream.Change{{Stream: scopestream.Stream{Scope: "public", Family: "events", Audience: "all"}, RID: "leak", Version: "v1", Payload: payload}}); !errors.Is(err, scopestream.ErrDerivation) {
		t.Fatalf("private stream derivation accepted: %v", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if scopeCCount(t, c.db, `SELECT count(*) FROM scope_search_postings WHERE scope_id='public'`) > 0 || scopeCCount(t, c.db, `SELECT count(*) FROM scope_changes WHERE scope_id='public'`) > 0 {
		t.Fatal("private text persisted publicly")
	}
	// Real foundation Derived values cannot be rebound or converted into either
	// projection type. Test the automatic constant implicit-flow boundary too.
	repo := scopedrepo.New(c.db)
	err = repo.Compute(context.Background(), "owner", "private", func(comp *scopedrepo.Computation) error {
		value, err := comp.Constant("private sentinel")
		if err != nil {
			return err
		}
		if err = comp.Persist("public", "public-search", value); !errors.Is(err, scopes.ErrDerivation) {
			t.Fatalf("scope-pinned constant persisted publicly: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScopeStreamHTTPDisabledByDefault(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	r := httptest.NewRequest(http.MethodGet, "/stream/scope-test", nil)
	w := httptest.NewRecorder()
	streamhttp.ScopeHandler(streamhttp.ScopeOptions{}).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("partly migrated stream enabled: %d", w.Code)
	}
}

type scopeCCancelOnFlush struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *scopeCCancelOnFlush) Flush() { w.ResponseRecorder.Flush(); w.cancel() }

func TestScopeStreamHTTPActualWireBytesForHTMLSensitivePayload(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "s", "reader", "active")
	s := scopestream.Stream{Scope: "s", Family: "events", Audience: "all"}
	// Raw JSON is allowed by the projection API. The SSE serializer must not
	// expand every '<' into six bytes after charging the unescaped payload.
	payload, err := scopestream.PreparePayload("s", json.RawMessage(`"`+strings.Repeat("<", scopestream.MaxPayloadBytes-2)+`"`))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 100; i++ {
		err = scopestream.Apply(context.Background(), &scopeCWriter{tx, "s"}, []scopestream.Change{{Stream: s, RID: fmt.Sprint(i), Version: "1", Payload: payload}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &scopeCCancelOnFlush{httptest.NewRecorder(), cancel}
	r := httptest.NewRequest(http.MethodGet, "/stream/scope-test", nil).WithContext(ctx)
	streamhttp.ScopeHandler(streamhttp.ScopeOptions{Repository: c, Tokens: scopeCStreamTokens(t), Ready: func() bool { return true }, Selection: func(*http.Request) (scopestream.Request, error) {
		return scopestream.Request{Principal: "reader", Streams: []scopestream.Stream{s}, Limit: 100}, nil
	}}).ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() > scopestream.MaxOutputBytes || strings.Contains(w.Body.String(), `\u003c`) || !strings.Contains(w.Body.String(), strings.Repeat("<", 100)) {
		t.Fatalf("actual serialized bytes exceed tick budget or expanded: status=%d bytes=%d", w.Code, w.Body.Len())
	}
}
