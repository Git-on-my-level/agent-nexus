package primitives_test

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCommentSearchFullWriteCost1000Comments(t *testing.T) {
	if testing.Short() {
		t.Skip("full 1000-comment write measurement")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "cost report"}, strings.Repeat("body ", 200000), "text", []string{})
	if err != nil {
		t.Fatal(err)
	}
	did, thread := doc["id"].(string), doc["thread_id"].(string)
	var target string
	for i := 0; i < 999; i++ {
		id := fmt.Sprintf("cost-%04d", i)
		target = id
		if _, err = ws.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,'message_posted','2026-10-01T00:00:00Z','owner',?,'[]','{"payload":{"text":"peer history"}}')`, id, thread); err != nil {
			t.Fatal(err)
		}
	}
	comment, err := s.CreateDocumentComment(ctx, "owner", did, "peer history", "")
	if err != nil {
		t.Fatal(err)
	}
	target = comment["id"].(string)
	for done := false; !done; {
		done, err = ws.MaintainCommentSearchBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, phase := range []string{"independent-cutover"} {
		pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
		if err != nil {
			t.Fatal(err)
		}
		measured := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
		request := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
		if _, err = measured.UpdateDocumentComment(request, "owner", did, target, "peer history"); err != nil {
			t.Fatal(err)
		}
		if _, _, err = measured.GetDocument(request, did); err != nil {
			t.Fatal(err)
		}
		capture.Start()
		start := time.Now()
		_, err = measured.UpdateDocumentComment(request, "owner", did, target, "measured token")
		elapsed := time.Since(start)
		_, calls, readRows := capture.Stop()
		if err != nil {
			t.Fatal(err)
		}
		if err = capture.WorkError(); err != nil {
			t.Fatal(err)
		}
		work := capture.Work()
		if readRows > 50 {
			t.Fatalf("comment edit still reads history: %d rows", readRows)
		}
		pool.Close()
		t.Logf("phase=%s comments=1000 body_bytes=1000000 elapsed=%s sql_calls=%d rows=%d vm_steps=%d full_scan=%d", phase, elapsed, calls, readRows, work.VMSteps, work.FullScanSteps)
	}
	// Same source corpus and authorized request, with the fallback repaired
	// before its sample. Both modes must return the edited parent.
	for _, query := range []string{"measured", "body measured", "history measured"} {
		for _, enabled := range []string{"false", "true"} {
			t.Setenv("ANX_INDEPENDENT_DOCUMENT_SEARCH", enabled)
			pool, capture, e := perfguard.Open("file:" + ws.Layout().DatabasePath)
			if e != nil {
				t.Fatal(e)
			}
			measured := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
			request := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
			limit := 1
			coverage := primitives.DocumentSearchCoverage{}
			filter := primitives.DocumentSearchFilter{Query: query, Limit: &limit, Coverage: &coverage}
			if _, _, e = measured.SearchDocuments(request, filter); e != nil {
				t.Fatal(e)
			}
			capture.Start()
			start := time.Now()
			docs, _, e := measured.SearchDocuments(request, filter)
			elapsed := time.Since(start)
			_, calls, rows := capture.Stop()
			if e != nil || len(docs) != 1 || docs[0]["id"] != did {
				t.Fatal(docs, e)
			}
			if e = capture.WorkError(); e != nil {
				t.Fatal(e)
			}
			work := capture.Work()
			if enabled == "true" && work.VMSteps > 50000 {
				t.Fatalf("search exceeds bounded VM budget: %d", work.VMSteps)
			}
			t.Logf("query=%q search_mode=%s comments=1000 elapsed=%s sql_calls=%d rows=%d vm_steps=%d full_scan=%d", query, coverage.Mode, elapsed, calls, rows, work.VMSteps, work.FullScanSteps)
			pool.Close()
		}

	}
}
