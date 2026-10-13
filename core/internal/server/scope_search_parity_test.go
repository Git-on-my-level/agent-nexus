package server

// Differential evidence for the disabled writer and executor. The trusted
// reference reader is test-only; this does not certify the serving dispatcher.
import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/scopesearch"
)

type parityResource struct {
	id, parent, kind, scope, text string
	version                       int64
	deleted                       bool
}

func parityRepository(t *testing.T) *scopeCRepository {
	t.Helper()
	c := newScopeCRepository(t)
	// Replace the older reference writer's schema with the actual sealed writer
	// schema. Reuse only the test reader and foundation membership checks.
	scopeCExec(t, c.db, `DROP TABLE scope_search_postings; DROP TABLE scope_search_resources; DROP TABLE scope_search_generations`)
	if err := scopedrepo.New(c.db).InitializeSearchSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.grant(t, "public", "reader", "active")
	c.grant(t, "private", "owner", "active")
	scopeCExec(t, c.db, `CREATE VIRTUAL TABLE parity_legacy_fts USING fts5(document_id UNINDEXED,scope_id UNINDEXED,body,comments,tokenize='unicode61')`)
	return c
}

func parityWrite(t *testing.T, c *scopeCRepository, r *parityResource, text string, deleted bool) {
	t.Helper()
	ctx := context.Background()
	tx, err := resourceaccess.NewDB(c.db).BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	old := r.version
	r.version++
	if old == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO scope_resources VALUES(?,?,?,?,?)`, r.scope, r.kind, r.id, "canonical-"+r.id, r.version)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE scope_resources SET version=? WHERE scope_id=? AND kind=? AND id=?`, r.version, r.scope, r.kind, r.id)
	}
	if err != nil {
		t.Fatal(err)
	}
	var rid int64
	if err = tx.QueryRowContext(ctx, `SELECT rid FROM scope_resource_rids WHERE scope_id=? AND kind=? AND resource_id=?`, r.scope, r.kind, r.id).Scan(&rid); err != nil {
		t.Fatal(err)
	}
	after := &scopes.Projection{Text: text, ContainerID: r.parent, Timestamp: 100, Deleted: deleted}
	bounded, err := scopes.BoundSearchProjection(*after)
	if err != nil {
		t.Fatal(err)
	}
	after = &bounded
	delta := scopes.Change{ScopeID: scopes.ID(r.scope), Kind: r.kind, ResourceID: r.id, CanonicalVersion: r.version, Family: "events", Audience: "all", After: after}
	if old > 0 {
		before, err := scopes.BoundSearchProjection(scopes.Projection{Text: r.text, ContainerID: r.parent, Timestamp: 100, Deleted: r.deleted})
		if err != nil {
			t.Fatal(err)
		}
		delta.Before = &before
	}
	m := scopes.CanonicalMutation{Identity: scopes.ResourceIdentity{ScopeID: scopes.ID(r.scope), Kind: r.kind, ResourceID: r.id, CanonicalID: "canonical-" + r.id, RID: rid, CanonicalVersion: r.version}, PreviousVersion: old, Changes: []scopes.Change{delta}}
	if err = scopedrepo.ApplyRegisteredCanonicalHooks(ctx, tx, m, scopedrepo.SearchCanonicalHook{}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	r.text = text
	r.deleted = deleted
}

func parityLegacyRebuild(t *testing.T, c *scopeCRepository, resources []*parityResource) {
	t.Helper()
	scopeCExec(t, c.db, `DELETE FROM parity_legacy_fts`)
	comments := map[string][]string{}
	for _, r := range resources {
		if r.kind == "comment" && !r.deleted {
			comments[r.parent] = append(comments[r.parent], r.text)
		}
	}
	for _, r := range resources {
		if r.kind == "document" && !r.deleted {
			scopeCExec(t, c.db, `INSERT INTO parity_legacy_fts VALUES(?,?,?,?)`, r.id, r.scope, r.text, strings.Join(comments[r.id], "\n"))
		}
	}
}

func parityOld(t *testing.T, c *scopeCRepository, q string) []string {
	t.Helper()
	rows, err := c.db.Query(`SELECT document_id FROM parity_legacy_fts WHERE parity_legacy_fts MATCH ? AND scope_id='public' ORDER BY document_id`, q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func parityNew(t *testing.T, c *scopeCRepository, q string, phrase bool) []string {
	t.Helper()
	req := scopesearch.Request{Principal: "reader", Scopes: []string{"public"}, Query: q, Phrase: phrase, Limit: 3}
	tokens := scopeCSearchTokens(t)
	seen := map[string]bool{}
	for pages := 0; ; pages++ {
		if pages > 1000 {
			t.Fatal("continuation did not terminate")
		}
		page, err := scopesearch.Search(context.Background(), c, tokens, req)
		if err != nil {
			t.Fatal(err)
		}
		for _, hit := range page.Items {
			if hit.Scope != "public" || strings.Contains(hit.Snippet, "privatecanary") {
				t.Fatal("private comment leaked", hit)
			}
			id := hit.RID
			if hit.Kind == "comment" {
				id = hit.Parent
			}
			seen[id] = true
		}
		if page.Continuation == "" {
			break
		}
		req.Continuation = page.Continuation
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func TestScopeSearchRandomizedAggregateParity(t *testing.T) {
	for _, seed := range []int64{7, 760, 20261013} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			c := parityRepository(t)
			rng := rand.New(rand.NewSource(seed))
			words := func() string {
				var out []string
				for n := rng.Intn(8) + 1; n > 0; n-- {
					w := rng.Intn(12)
					out = append(out, fmt.Sprintf("Word%d, Mate%d!", w, w))
				}
				return strings.Join(out, "\n")
			}
			var resources []*parityResource
			for i := 0; i < 40; i++ {
				scope := "public"
				if i%5 == 0 {
					scope = "private"
				}
				doc := &parityResource{id: fmt.Sprintf("doc-%03d", i), kind: "document", scope: scope}
				parityWrite(t, c, doc, words(), false)
				resources = append(resources, doc)
				for j := 0; j < rng.Intn(6); j++ {
					r := &parityResource{id: fmt.Sprintf("comment-%03d-%d", i, j), parent: doc.id, kind: "comment", scope: scope}
					text := words()
					if scope == "private" {
						text += " privatecanary"
					}
					parityWrite(t, c, r, text, false)
					resources = append(resources, r)
				}
			}
			// The common semantic domain: bounded ASCII whole terms and paired
			// terms contained in one resource. Compare document membership, not
			// BM25 order/rank or the new resource-shaped results.
			for round := 0; round < 3; round++ {
				if round > 0 {
					for _, r := range resources {
						if r.kind == "comment" {
							parityWrite(t, c, r, words(), round == 1 && rng.Intn(3) == 0)
						}
					}
				}
				parityLegacyRebuild(t, c, resources)
				for w := 0; w < 12; w++ {
					for _, query := range []struct {
						old, next string
						phrase    bool
					}{
						{fmt.Sprintf(`"word%d"`, w), fmt.Sprintf("word%d", w), false},
						{fmt.Sprintf(`"word%d" "mate%d"`, w, w), fmt.Sprintf("word%d mate%d", w, w), false},
						{fmt.Sprintf(`"word%d mate%d"`, w, w), fmt.Sprintf("word%d mate%d", w, w), true},
					} {
						old, next := parityOld(t, c, query.old), parityNew(t, c, query.next, query.phrase)
						if !reflect.DeepEqual(old, next) {
							t.Fatalf("seed=%d round=%d query=%q old=%v new=%v", seed, round, query.next, old, next)
						}
					}
				}
			}
		})
	}
}

func TestScopeSearchAggregateSemanticDifferencesBlockCutover(t *testing.T) {
	c := parityRepository(t)
	doc := &parityResource{id: "doc", kind: "document", scope: "public"}
	a := &parityResource{id: "a", parent: "doc", kind: "comment", scope: "public"}
	b := &parityResource{id: "b", parent: "doc", kind: "comment", scope: "public"}
	parityWrite(t, c, doc, "documentword", false)
	parityWrite(t, c, a, "commentalpha", false)
	parityWrite(t, c, b, "commentbeta", false)
	parityLegacyRebuild(t, c, []*parityResource{doc, a, b})
	for _, q := range []struct {
		old, next string
		phrase    bool
	}{
		{`"documentword" "commentalpha"`, "documentword commentalpha", false},
		{`"commentalpha" "commentbeta"`, "commentalpha commentbeta", false},
		{`"commentalpha-commentbeta"`, "commentalpha-commentbeta", true},
	} {
		if got := parityOld(t, c, q.old); !reflect.DeepEqual(got, []string{"doc"}) {
			t.Fatal("legacy semantic fixture", got)
		}
		if got := parityNew(t, c, q.next, q.phrase); len(got) != 0 {
			t.Fatal("independent index recombined resources", got)
		}
	}
	parityWrite(t, c, a, "café", false)
	parityLegacyRebuild(t, c, []*parityResource{doc, a, b})
	if len(parityOld(t, c, `"cafe"`)) != 1 || len(parityNew(t, c, "cafe", false)) != 0 {
		t.Fatal("diacritic difference no longer characterized")
	}
	parityWrite(t, c, a, strings.Repeat("prefix ", 10000)+"outsidetoken", false)
	parityLegacyRebuild(t, c, []*parityResource{doc, a, b})
	if len(parityOld(t, c, `"outsidetoken"`)) != 1 || len(parityNew(t, c, "outsidetoken", false)) != 0 {
		t.Fatal("coverage difference no longer characterized")
	}
}

func TestScopeSearchSealedWriterPrivateCommentSelection(t *testing.T) {
	c := parityRepository(t)
	doc := &parityResource{id: "private-doc", kind: "document", scope: "private"}
	comment := &parityResource{id: "private-comment", parent: doc.id, kind: "comment", scope: "private"}
	parityWrite(t, c, doc, "private title", false)
	parityWrite(t, c, comment, "privatecanary", false)
	tokens := scopeCSearchTokens(t)
	for _, principal := range []string{"reader", "unauthorized-agent", "owner"} {
		req := scopesearch.Request{Principal: principal, Scopes: []string{"private"}, Query: "privatecanary"}
		page, err := scopesearch.Search(context.Background(), c, tokens, req)
		if principal == "owner" {
			if err != nil || len(page.Items) != 1 || page.Items[0].RID != comment.id {
				t.Fatal("owner lost its comment", page, err)
			}
		} else if err == nil || len(page.Items) != 0 {
			t.Fatal("private comment exposed", principal, page, err)
		}
	}
	scopeCExec(t, c.db, `DELETE FROM scope_memberships WHERE principal='owner' AND scope_id='private'`)
	page, err := scopesearch.Search(context.Background(), c, tokens, scopesearch.Request{Principal: "owner", Scopes: []string{"private"}, Query: "privatecanary"})
	if err == nil || len(page.Items) != 0 {
		t.Fatal("revoked owner retained comment access", page, err)
	}
}
