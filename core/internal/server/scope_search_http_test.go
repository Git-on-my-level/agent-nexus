package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"agent-nexus-core/internal/scopesearch"
)

func scopeCSearchTokens(t *testing.T) *scopesearch.Tokens {
	t.Helper()
	tokens, err := scopesearch.NewTokens([]byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}
func scopeCSearchHTTP(t *testing.T, c *scopeCRepository, ids []string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(scopesearch.Handler(scopesearch.HTTPOptions{Repository: c, Tokens: scopeCSearchTokens(t), Ready: func() bool { return true }, Selection: func(*http.Request) (string, []string, error) { return "reader", ids, nil }}))
	t.Cleanup(s.Close)
	return s
}
func scopeCSearchPage(t *testing.T, base, query, cursor string, limit int) scopesearch.Page {
	t.Helper()
	u := base + "?q=" + url.QueryEscape(query) + "&phrase=true&limit=" + fmt.Sprint(limit) + "&continuation=" + url.QueryEscape(cursor)
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("search status %d", resp.StatusCode)
	}
	var page scopesearch.Page
	if err = json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestScopeSearchHTTPOversizedRepetitiveProgressAndCoverage(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "public", "reader", "active")
	c.grant(t, "private", "owner", "active")
	// Five driver-term candidates have a phrase outside declared coverage. The
	// first page must be empty with progress rather than skip them or loop.
	for i := 0; i < 5; i++ {
		c.searchWrite(t, "public", "document", fmt.Sprintf("large-%d", i), strings.Repeat("anchor x ", 10000)+"anchor needle", int64(100-i))
	}
	c.searchWrite(t, "public", "document", "inside", strings.Repeat("x ", 30000)+"anchor needle", 90)
	c.searchWrite(t, "public", "comment", "tail", "anchor needle", 80)
	c.searchWrite(t, "private", "document", "secret", "anchor needle private sentinel", 200)
	s := scopeCSearchHTTP(t, c, []string{"public"})
	first := scopeCSearchPage(t, s.URL, "anchor needle", "", 1)
	if len(first.Items) != 0 || first.Continuation == "" || first.CoverageBytes != 65536 {
		t.Fatalf("no bounded progress: %#v", first)
	}
	second := scopeCSearchPage(t, s.URL, "anchor needle", first.Continuation, 1)
	if len(second.Items) != 1 || second.Items[0].RID != "inside" || second.Items[0].Version != "v1" || second.Items[0].IndexedBytes < 60000 {
		t.Fatalf("late prefix phrase missing: %#v", second)
	}
	third := scopeCSearchPage(t, s.URL, "anchor needle", second.Continuation, 1)
	if len(third.Items) != 1 || third.Items[0].RID != "tail" || third.Items[0].Kind != "comment" {
		t.Fatalf("individual comment missing: %#v", third)
	}
	for _, page := range []scopesearch.Page{first, second, third} {
		b, _ := json.Marshal(page)
		if strings.Contains(string(b), "private sentinel") || strings.Contains(string(b), "secret") {
			t.Fatal("private search derivation leaked")
		}
	}
	if scopeCCount(t, c.db, `SELECT count(*) FROM scope_search_postings WHERE scope_id='public' AND term='needle'`) != 2 {
		t.Fatal("outside-coverage or private text entered public postings")
	}
}

func TestScopeSearchHTTPDoesNotSkipSaturatedScopeFrontier(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "a", "reader", "active")
	c.grant(t, "b", "reader", "active")
	for i := 1; i <= 6; i++ {
		text := "anchor reject"
		if i == 6 {
			text = "anchor needle"
		}
		c.searchWrite(t, "a", "document", fmt.Sprintf("a%d", i), text, int64(100-i))
	}
	c.searchWrite(t, "b", "document", "older", "anchor needle", 1)
	s := scopeCSearchHTTP(t, c, []string{"b", "a"})
	page := scopeCSearchPage(t, s.URL, "anchor needle", "", 1)
	if len(page.Items) != 0 || page.Continuation == "" {
		t.Fatalf("advanced past unfetched scope frontier: %#v", page)
	}
	page = scopeCSearchPage(t, s.URL, "anchor needle", page.Continuation, 1)
	if len(page.Items) != 1 || page.Items[0].RID != "a6" {
		t.Fatalf("skipped scope match: %#v", page)
	}
	page = scopeCSearchPage(t, s.URL, "anchor needle", page.Continuation, 1)
	if len(page.Items) != 1 || page.Items[0].RID != "older" {
		t.Fatalf("missing older hit: %#v", page)
	}
}

func TestScopeSearchHTTPGenerationQueryAndSelectionBinding(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "a", "reader", "active")
	c.grant(t, "b", "reader", "active")
	for i := 0; i < 6; i++ {
		c.searchWrite(t, "a", "document", fmt.Sprint(i), "needle", int64(10-i))
	}
	s := scopeCSearchHTTP(t, c, []string{"a"})
	page := scopeCSearchPage(t, s.URL, "needle", "", 1)
	if page.Continuation == "" {
		t.Fatal("missing continuation")
	}
	tokens := scopeCSearchTokens(t)
	base := scopesearch.Request{Principal: "reader", Scopes: []string{"a"}, Query: "needle", Phrase: true, Limit: 1, Continuation: page.Continuation}
	for _, mutation := range []func(*scopesearch.Request){func(r *scopesearch.Request) { r.Query = "different" }, func(r *scopesearch.Request) { r.Principal = "another" }, func(r *scopesearch.Request) { r.Scopes = []string{"b"} }, func(r *scopesearch.Request) { r.Continuation = "!" + r.Continuation }} {
		r := base
		mutation(&r)
		if _, err := scopesearch.Search(context.Background(), c, tokens, r); err == nil {
			t.Fatal("unbound continuation accepted")
		}
	}
	// A different scope's writes must not invalidate this selected generation.
	c.searchWrite(t, "b", "document", "other", "needle private", 1)
	if _, err := scopesearch.Search(context.Background(), c, tokens, base); err != nil {
		t.Fatal(err)
	}
	c.searchWrite(t, "a", "comment", "new", "needle", 100)
	if _, err := scopesearch.Search(context.Background(), c, tokens, base); !errors.Is(err, scopesearch.ErrRestart) {
		t.Fatalf("stale generation accepted: %v", err)
	}
	resp, err := http.Get(s.URL + "?q=needle&phrase=true&continuation=" + url.QueryEscape(page.Continuation))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("restart status %d", resp.StatusCode)
	}
}

func TestScopeSearchRejectedBodiesChargeVerificationBudget(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "a", "reader", "active")
	c.searchWrite(t, "a", "document", "large", strings.Repeat("anchor x ", 10000)+"needle", 3)
	c.searchWrite(t, "a", "comment", "small", "anchor needle", 2)
	r := scopesearch.Request{Principal: "reader", Scopes: []string{"a"}, Query: "anchor needle", Limit: 1, VerificationBytes: scopesearch.MaxTextBytes}
	page, err := scopesearch.Search(context.Background(), c, scopeCSearchTokens(t), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.Candidates != 1 || page.VerificationBytes < scopesearch.MaxTextBytes-8 || page.VerificationBytes > scopesearch.MaxTextBytes || page.Continuation == "" {
		t.Fatalf("rejection skipped/uncharged: %#v", page)
	}
	r.Continuation = page.Continuation
	page, err = scopesearch.Search(context.Background(), c, scopeCSearchTokens(t), r)
	if err != nil || len(page.Items) != 1 || page.Items[0].RID != "small" {
		t.Fatalf("oversized candidate loop: %#v %v", page, err)
	}
	r.Continuation = ""
	r.VerificationBytes = scopesearch.MaxTextBytes - 1
	scopeCExamined.Store(0)
	if _, err = scopesearch.Search(context.Background(), c, scopeCSearchTokens(t), r); !errors.Is(err, scopesearch.ErrBudget) || scopeCExamined.Load() != 0 {
		t.Fatal("under-cap budget queried candidates")
	}
}

func TestScopeSearchIncrementalCommentWriteIgnoresHistoryAndRollsBack(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	c := newScopeCRepository(t)
	c.grant(t, "s", "reader", "active")
	c.searchWrite(t, "s", "document", "head", "head current", 1)
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		_, err = tx.Exec(`INSERT INTO scope_search_resources VALUES('s','comment',?,'v1','head',?,'old',3,0)`, fmt.Sprint(i), -i)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(`INSERT INTO scope_search_postings VALUES('s','old',?,?,'comment')`, -i, fmt.Sprint(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	before := scopeCCount(t, c.db, `SELECT total_changes()`)
	c.searchWrite(t, "s", "comment", "new", "new bounded comment", 20000)
	after := scopeCCount(t, c.db, `SELECT total_changes()`)
	if after-before != 5 {
		t.Fatalf("comment rebuilt history: %d writes", after-before)
	} // resource +3 postings +generation.
	if scopeCCount(t, c.db, `SELECT count(*) FROM scope_search_postings WHERE scope_id='s' AND kind='document' AND rid='head'`) != 2 {
		t.Fatal("comment changed document head")
	}
	before = scopeCCount(t, c.db, `SELECT generation FROM scope_search_generations WHERE scope_id='s'`)
	tx, err = c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = scopesearch.Apply(context.Background(), &scopeCWriter{tx, "s"}, scopesearch.Change{Scope: "s", Kind: "comment", RID: "rollback", Version: "v1", Content: scopesearch.Prepare("s", "uncommitted")})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if scopeCCount(t, c.db, `SELECT count(*) FROM scope_search_postings WHERE rid='rollback'`) != 0 || scopeCCount(t, c.db, `SELECT generation FROM scope_search_generations WHERE scope_id='s'`) != before {
		t.Fatal("posting delta escaped transaction")
	}
}

func TestScopeSearchHTTPDisabledByDefault(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	r := httptest.NewRequest(http.MethodGet, "/scope-search?q=needle", nil)
	w := httptest.NewRecorder()
	scopesearch.Handler(scopesearch.HTTPOptions{}).ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("partly migrated reader enabled: %d", w.Code)
	}
}

func TestScopeSearchHTTPUnchangedQueryResumesEscapedIdentity(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	for _, character := range []string{"<", ">", "&", "\x01"} {
		t.Run(character, func(t *testing.T) {
			c := newScopeCRepository(t)
			scope := strings.Repeat(character, 256)
			c.grant(t, scope, "reader", "active")
			rid := strings.Repeat(character, 512)
			c.searchWrite(t, scope, "document", rid, "needle", math.MaxInt64)
			c.searchWrite(t, scope, "document", "second", "needle", 1)
			s := scopeCSearchHTTP(t, c, []string{scope})
			first := scopeCSearchPage(t, s.URL, "needle", "", 1)
			if len(first.Items) != 1 || first.Items[0].RID != rid || len(first.Continuation) != 6471 {
				t.Fatalf("missing worst-case continuation: hits=%d bytes=%d", len(first.Items), len(first.Continuation))
			}
			second := scopeCSearchPage(t, s.URL, "needle", first.Continuation, 1)
			if len(second.Items) != 1 || second.Items[0].RID != "second" || second.Continuation != "" {
				t.Fatalf("unchanged-query continuation failed: %#v", second)
			}
		})
	}
}
