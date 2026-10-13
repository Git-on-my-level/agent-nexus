package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopesearch"
)

func TestCommentSearchCanonicalAPIsAndAudience(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "report"}, "body", "text", []string{})
	if err != nil {
		t.Fatal(err)
	}
	did := anyStringValue(doc["id"])
	c, err := s.CreateDocumentComment(ctx, "owner", did, "original needle", "")
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(c["id"])
	check := func(actor, term string, want int) {
		t.Helper()
		request := WithRequestAccessScope(ctx, AccessScope{ActorID: actor})
		for _, table := range []string{"comment_search_heads", "comment_search_postings"} {
			q := `SELECT count(*) FROM ` + table + ` WHERE event_id=?`
			args := []any{id}
			if table == "comment_search_postings" {
				q += ` AND term=?`
				args = append(args, term)
			}
			var got int
			if err := s.db.QueryRowContext(request, q, args...).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("%s %s: got %d want %d", actor, table, got, want)
			}
		}
	}
	check("stranger", "needle", 1)
	if _, err = s.UpdateDocumentComment(ctx, "owner", did, id, "edited replacement"); err != nil {
		t.Fatal(err)
	}
	check("stranger", "replacement", 1)
	if _, err = s.DeleteDocumentComment(ctx, "owner", did, id); err != nil {
		t.Fatal(err)
	}
	check("owner", "replacement", 0)
	if _, err = s.RestoreEvent(ctx, "owner", id); err != nil {
		t.Fatal(err)
	}
	check("stranger", "replacement", 1)
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private source"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyStringValue(private["thread_id"])
	if _, err = s.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateDocumentComment(ctx, "owner", did, id, "replacement [private]("+anyStringValue(private["ref"])+")"); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"stranger", "unauthorized-agent"} {
		check(actor, "replacement", 0)
	}
	check("owner", "replacement", 1)
	// Reuse the same request context across visibility changes: authority epochs,
	// not copied audiences or backfill timestamps, must govern the derived rows.
	request := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	read := func(want int) {
		t.Helper()
		var got int
		if err := s.db.QueryRowContext(request, `SELECT count(*) FROM comment_search_postings WHERE event_id=? AND term='replacement'`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("visibility: got %d want %d", got, want)
		}
	}
	read(0)
	if _, err = s.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": ""}, nil); err != nil {
		t.Fatal(err)
	}
	read(1)
	if _, err = s.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	read(0)
	// Certification survives privacy changes because each projection remains
	// source-bound and every scoped read rechecks current canonical authority.
	for done := false; !done; {
		done, err = ws.MaintainCommentSearchBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	check("stranger", "replacement", 0)
	check("owner", "replacement", 1)
}

// Test-only adapter: exercises the real executor over the live trigger-written
// tables and scoped snapshots. Fixed generations are safe only in these tests,
// which never mutate between pages. This is not a production reader adapter.
type commentParityRepository struct {
	s       *Store
	parents map[string]string
}
type commentParitySnapshot struct {
	tx      *resourceaccess.ReadSnapshot
	binding scopesearch.Binding
	parents map[string]string
}

func (r commentParityRepository) ReadSearch(ctx context.Context, principal string, scopes []string, fn func(scopesearch.Snapshot) error) error {
	ctx = WithRequestAccessScope(ctx, AccessScope{ActorID: principal})
	tx, err := r.s.db.BeginReadSnapshot(ctx, `SELECT event_id FROM comment_search_heads`)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b := scopesearch.Binding{Principal: principal, AuthorityGeneration: 1}
	for _, scope := range scopes {
		var id string
		if err = tx.QueryRowContext(ctx, `SELECT id FROM documents WHERE id=? AND thread_id=?`, r.parents[scope], scope).Scan(&id); err != nil {
			return err
		}
		b.Scopes = append(b.Scopes, scopesearch.ScopeGeneration{Scope: scope, Generation: 1, ScopeGeneration: 1})
	}
	// Carry the same authority context into all callback operations.
	return fn(commentParityContextSnapshot{commentParitySnapshot{tx, b, r.parents}, ctx})
}

type commentParityContextSnapshot struct {
	commentParitySnapshot
	ctx context.Context
}

func (s commentParitySnapshot) Binding() scopesearch.Binding { return s.binding }
func (s commentParityContextSnapshot) Candidates(_ context.Context, scope, term string, after *scopesearch.Key, limit int) ([]scopesearch.Candidate, error) {
	rid := ""
	if after != nil {
		rid = after.RID
	}
	rows, err := s.tx.QueryContext(s.ctx, `SELECT p.event_id,h.source_version,length(CAST(json_extract(h.projection_json,'$.text') AS BLOB)),json_extract(h.projection_json,'$.truncated') FROM comment_search_postings p JOIN comment_search_heads h ON h.event_id=p.event_id WHERE p.thread_id=? AND p.term=? AND p.event_id>? ORDER BY p.event_id LIMIT ?`, scope, term, rid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []scopesearch.Candidate{}
	for rows.Next() {
		c := scopesearch.Candidate{Scope: scope, Parent: s.parents[scope], Key: scopesearch.Key{Scope: scope, Kind: "comment"}}
		if err = rows.Scan(&c.Key.RID, &c.Version, &c.IndexedBytes, &c.Truncated); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s commentParityContextSnapshot) Texts(_ context.Context, keys []scopesearch.Candidate) ([]string, error) {
	out := make([]string, len(keys))
	for i, k := range keys {
		if err := s.tx.QueryRowContext(s.ctx, `SELECT json_extract(projection_json,'$.text') FROM comment_search_heads WHERE event_id=? AND thread_id=?`, k.Key.RID, k.Scope).Scan(&out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func TestCommentSearchRandomizedAggregateParity(t *testing.T) {
	if testing.Short() {
		t.Skip("randomized canonical-store differential integration")
	}
	ctx := context.Background()
	comparisons := 0
	for _, seed := range []int64{7, 41, 2026} {
		ws, err := initializeTestWorkspace(ctx, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()
		s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
		repo := commentParityRepository{s: s, parents: map[string]string{}}
		selected := []string{}
		rng := rand.New(rand.NewSource(seed))
		ids := []string{}
		for i := 0; i < 12; i++ {
			doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": fmt.Sprintf("report %d", i)}, "body", "text", []string{})
			if err != nil {
				t.Fatal(err)
			}
			did, thread := anyStringValue(doc["id"]), anyStringValue(doc["thread_id"])
			repo.parents[thread] = did
			selected = append(selected, thread)
			for j := 0; j < 1+rng.Intn(5); j++ {
				k := rng.Intn(12)
				id := fmt.Sprintf("parity-%02d-%02d", i, j)
				ids = append(ids, id)
				payload, _ := json.Marshal(map[string]any{"payload": map[string]any{"text": fmt.Sprintf("word%d mate%d", k, k)}})
				if _, err = ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,'message_posted','now','owner',?,'[]',?)`, id, thread, string(payload)); err != nil {
					t.Fatal(err)
				}
			}
		}

		for round := 0; round < 3; round++ {
			if round == 1 {
				for i, id := range ids {
					if i%3 == 0 {
						_, err = ws.DB().Exec(`UPDATE events SET trashed_at='now' WHERE id=?`, id)
					} else if i%3 == 1 {
						_, err = ws.DB().Exec(`UPDATE events SET payload_json='{"payload":{"text":"word0 mate0"}}' WHERE id=?`, id)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if round == 2 {
				if _, err = ws.DB().Exec(`UPDATE events SET trashed_at=NULL WHERE id LIKE 'parity-%'`); err != nil {
					t.Fatal(err)
				}
			}
			for _, did := range repo.parents {
				if _, err = s.db.ExecContext(ctx, `INSERT INTO document_search_dirty VALUES(?) ON CONFLICT DO NOTHING`, did); err != nil {
					t.Fatal(err)
				}
				if _, err = s.repairAggregateSearchOne(ctx); err != nil {
					t.Fatal(err)
				}
			}
			for done := false; !done; {
				done, err = s.repairAggregateSearchOne(ctx)
				if err != nil {
					t.Fatal(err)
				}
			}
			for k := 0; k < 12; k++ {
				for mode := 0; mode < 3; mode++ {
					q := fmt.Sprintf("word%d", k)
					legacyQ := q
					if mode > 0 {
						q += fmt.Sprintf(" mate%d", k)
						legacyQ = q
					}
					if mode == 2 {
						legacyQ = strings.ReplaceAll(q, " ", "-")
					}
					old, _, err := s.searchAggregateDocuments(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), DocumentSearchFilter{Query: legacyQ})
					if err != nil {
						t.Fatal(err)
					}
					want := []string{}
					for _, doc := range old {
						want = append(want, anyStringValue(doc["id"]))
					}
					sort.Strings(want)
					pageLimit := 3
					req := DocumentSearchFilter{Query: legacyQ, Limit: &pageLimit}
					seen := map[string]bool{}
					for n := 0; ; n++ {
						if n > 100 {
							t.Fatal("paging did not terminate")
						}
						page, next, err := s.SearchDocuments(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), req)
						if err != nil {
							t.Fatal(err)
						}
						for _, hit := range page {
							seen[anyStringValue(hit["id"])] = true
						}
						if next == "" {
							break
						}
						req.Cursor = next
					}
					got := []string{}
					for id := range seen {
						got = append(got, id)
					}
					sort.Strings(got)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("seed=%d round=%d query=%s phrase=%v got=%v want=%v", seed, round, q, mode == 2, got, want)
					}
					comparisons++
				}
			}
		}
	}
	t.Logf("aggregate membership comparisons=%d (3 seeds, edits, deletes, restores, AND, phrase, 3-hit pages)", comparisons)
}

func TestCommentSearchIntentionalSemanticImprovements(t *testing.T) {
	// Each difference is a deliberate contract choice for the production reader:
	// coherent single-resource evidence, significant accents, bounded coverage.
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for _, tc := range []struct {
		name, body string
		comments   []string
		query      string
		phrase     bool
	}{
		{"cross-comment AND", "body", []string{"alpha", "omega"}, "alpha omega", false},
		{"cross-comment phrase", "body", []string{"alpha", "omega"}, "alpha omega", true},
		{"head/comment AND", "alpha", []string{"omega"}, "alpha omega", false},
		{"accent identity", "body", []string{"café"}, "cafe", false},
		{"bounded coverage", "body", []string{strings.Repeat("x ", scopesearch.MaxTextBytes/2) + "outsideprefix"}, "outsideprefix", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "report"}, tc.body, "text", []string{})
			if err != nil {
				t.Fatal(err)
			}
			did, thread := anyStringValue(doc["id"]), anyStringValue(doc["thread_id"])
			for _, text := range tc.comments {
				if _, err = s.CreateDocumentComment(ctx, "owner", did, text, ""); err != nil {
					t.Fatal(err)
				}
			}
			q := tc.query
			if tc.phrase {
				q = strings.ReplaceAll(q, " ", "-")
			}
			for done := false; !done; {
				done, err = s.repairAggregateSearchOne(ctx)
				if err != nil {
					t.Fatal(err)
				}
			}
			old, _, err := s.searchAggregateDocuments(ctx, DocumentSearchFilter{Query: q})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, d := range old {
				found = found || anyStringValue(d["id"]) == did
			}
			if !found {
				t.Fatal("legacy difference fixture did not match")
			}
			coverage := DocumentSearchCoverage{}
			production, _, e := s.SearchDocuments(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), DocumentSearchFilter{Query: q, Coverage: &coverage})
			if e != nil || len(production) != 0 || coverage.Mode != "independent" {
				t.Fatal("production reader violates intentional difference", production, coverage, e)
			}
			if tc.name == "accent identity" {
				production, _, e = s.SearchDocuments(ctx, DocumentSearchFilter{Query: "CAFÉ"})
				if e != nil || len(production) != 1 {
					t.Fatal("production accent identity", production, e)
				}
			}
			tokens, err := scopesearch.NewTokens([]byte(strings.Repeat("k", 32)))
			if err != nil {
				t.Fatal(err)
			}
			repo := commentParityRepository{s: s, parents: map[string]string{thread: did}}
			page, err := scopesearch.Search(ctx, repo, tokens, scopesearch.Request{Principal: "reader", Scopes: []string{thread}, Query: tc.query, Phrase: tc.phrase})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 0 {
				t.Fatal("independent match violates chosen contract", page)
			}
			if tc.name == "accent identity" {
				page, err = scopesearch.Search(ctx, repo, tokens, scopesearch.Request{Principal: "reader", Scopes: []string{thread}, Query: "CAFÉ"})
				if err != nil || len(page.Items) != 1 {
					t.Fatal("accented spelling must match case-insensitively", page, err)
				}
			}
			if tc.name == "bounded coverage" {
				page, err = scopesearch.Search(ctx, repo, tokens, scopesearch.Request{Principal: "reader", Scopes: []string{thread}, Query: "x"})
				if err != nil || len(page.Items) != 1 || !page.Items[0].Truncated || page.Items[0].IndexedBytes > scopesearch.MaxTextBytes {
					t.Fatal("coverage not explicit", page, err)
				}
			}
		})
	}
}
