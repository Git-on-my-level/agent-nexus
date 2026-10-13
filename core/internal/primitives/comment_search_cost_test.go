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
	// Save and restore only the new capture triggers to compare the same full
	// canonical API, source corpus and legacy graph/aggregate code path.
	rows, err := ws.DB().Query(`SELECT sql FROM sqlite_master WHERE type='trigger' AND name LIKE 'comment_search_event_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	triggers := []string{}
	for rows.Next() {
		var q string
		if err = rows.Scan(&q); err != nil {
			t.Fatal(err)
		}
		triggers = append(triggers, q)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if len(triggers) != 3 {
		t.Fatal(triggers)
	}
	if _, err = ws.DB().Exec(`DROP TRIGGER comment_search_event_insert;DROP TRIGGER comment_search_event_update;DROP TRIGGER comment_search_event_delete`); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"aggregate-only", "aggregate-plus-independent"} {
		if phase == "aggregate-plus-independent" {
			for _, q := range triggers {
				if _, err = ws.DB().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
		}
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
		pool.Close()
		t.Logf("phase=%s comments=1000 body_bytes=1000000 elapsed=%s sql_calls=%d rows=%d vm_steps=%d full_scan=%d", phase, elapsed, calls, readRows, work.VMSteps, work.FullScanSteps)
	}
}
