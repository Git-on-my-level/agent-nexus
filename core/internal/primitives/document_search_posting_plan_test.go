package primitives_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Measure the production reader after authority admission. A private matching
// control keeps the real scoped predicate active; unrelated canonical documents
// contribute postings but cannot change the target's point-query work.
func TestDocumentSearchParentANDPostingPlanAndBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("100/10000-document scoped VM integration")
	}
	ctx := context.Background()
	previous := map[int]uint64{}
	for _, count := range []int{100, 10000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			target, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "target"}, "anchor", "text", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.CreateDocumentComment(ctx, "owner", target["id"].(string), "bravo charlie delta echo foxtrot golf hotel", ""); err != nil {
				t.Fatal(err)
			}
			private, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "private"}, "anchor bravo charlie delta echo foxtrot golf hotel", "text", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
			// Synthetic imported documents without revisions have empty canonical heads.
			// Seed their derived head terms with the same transactional source triggers;
			// no constraints or authority triggers are disabled. Setup is not measured.
			tx, err := ws.DB().BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			insert, err := tx.Prepare(`INSERT INTO documents(id,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES(?,'unrelated filler','',0,'now','owner','now','owner')`)
			if err != nil {
				t.Fatal(err)
			}
			defer insert.Close()
			head, err := tx.Prepare(`UPDATE document_search_heads SET ready=1,projection_json=anx_comment_search_projection(CAST('unrelated filler other words' AS BLOB)) WHERE document_id=?`)
			if err != nil {
				t.Fatal(err)
			}
			defer head.Close()
			for i := 0; i < count; i++ {
				id := fmt.Sprintf("unrelated-%05d", i)
				if _, err = insert.Exec(id); err != nil {
					t.Fatal(err)
				}
				if _, err = head.Exec(id); err != nil {
					t.Fatal(err)
				}
			}
			// These imported empty-thread heads have no historical comments to attach.
			if _, err = tx.Exec(`DELETE FROM document_search_links_pending WHERE document_id LIKE 'unrelated-%'`); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var postings int
			if err = ws.DB().QueryRow(`SELECT count(*) FROM document_search_postings`).Scan(&postings); err != nil {
				t.Fatal(err)
			}
			pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			measured := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
			request := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
			queries := []string{"anchor bravo", "anchor bravo charlie delta echo foxtrot golf hotel"}
			for _, query := range queries {
				terms := len(strings.Fields(query))
				coverage := primitives.DocumentSearchCoverage{}
				filter := primitives.DocumentSearchFilter{Query: query, Coverage: &coverage}
				if _, _, err = measured.SearchDocuments(request, filter); err != nil {
					t.Fatal(err)
				}
				capture.Start()
				got, next, err := measured.SearchDocuments(request, filter)
				statements, calls, rows := capture.Stop()
				if err != nil || len(got) != 1 || got[0]["id"] != target["id"] || next != "" || coverage.Mode != "independent" {
					t.Fatal(query, got, next, coverage, err)
				}
				if err = capture.WorkError(); err != nil {
					t.Fatal(err)
				}
				work := capture.Work()
				t.Logf("unrelated_documents=%d postings=%d terms=%d sql_calls=%d rows=%d vm_steps=%d full_scan=%d", count, postings, terms, calls, rows, work.VMSteps, work.FullScanSteps)
				if work.VMSteps > 10000 || work.FullScanSteps > 20 || work.AutoIndexRows != 0 {
					t.Errorf("parent AND exceeded fixed warm budget: %+v", work)
				}
				if before := previous[terms]; before > 0 && work.VMSteps > before*2 {
					t.Errorf("term count %d grew with unrelated postings: %d -> %d", terms, before, work.VMSteps)
				}
				previous[terms] = work.VMSteps
				found := false
				for _, statement := range statements {
					if !strings.Contains(statement.SQL, "k(ord,doc_n,parent)") {
						continue
					}
					found = true
					if !strings.Contains(statement.SQL, "document_search_postings AS NOT MATERIALIZED (") {
						t.Error("posting policy lacks explicit inlining")
					}
					plan, err := perfguard.Explain(ctx, ws.DB(), statement)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("terms=%d parent intersection plan:\n%s", terms, strings.Join(plan, "\n"))
					seeks := 0
					for _, detail := range plan {
						upper := strings.ToUpper(detail)
						if strings.Contains(upper, "MATERIALIZE DOCUMENT_SEARCH_POSTINGS") || strings.Contains(upper, "SCAN DOCUMENT_SEARCH_POSTINGS") || strings.HasPrefix(upper, "SCAN P") || strings.HasPrefix(upper, "SCAN _ROW") {
							t.Errorf("posting shadow materialized/scanned: %s", detail)
						}
						if strings.HasPrefix(upper, "SEARCH _ROW USING") && strings.Contains(upper, "TERM=? AND DOC_N=?") {
							seeks++
						}
					}
					if seeks != terms {
						t.Errorf("expected %d indexed term/parent seeks, got %d", terms, seeks)
					}
				}
				if !found {
					t.Fatal("production parent-intersection statement was not measured")
				}
			}
		})
	}
}
