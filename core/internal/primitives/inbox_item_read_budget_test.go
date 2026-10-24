package primitives_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Measure the changed point read independently of shared cold authorization.
// The HTTP lifecycle tests cover principal admission and projection freshness.
func TestPerformanceInboxItemResolutionRead(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("advisory performance tier")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := p.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := s.CreateThread(ctx, "owner", map[string]any{"title": "Point reads"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := fmt.Sprint(thread.Thread["id"])
	items := []p.DerivedInboxItem{}
	for _, id := range []string{"open", "closed"} {
		ask, e := s.AppendEvent(ctx, "owner", map[string]any{"type": "human_attention_requested", "thread_id": threadID, "refs": []string{"thread:" + threadID}, "payload": map[string]any{"requester_actor_id": "owner", "kind": "ask", "title": "Proceed?", "response_proposals": []string{"Proceed"}}})
		if e != nil {
			t.Fatal(e)
		}
		items = append(items, p.DerivedInboxItem{ID: id, ThreadID: threadID, Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), SourceEventID: fmt.Sprint(ask["id"]), Data: map[string]any{"kind": "ask"}})
	}
	if err = s.ReplaceDerivedInboxItems(ctx, threadID, items); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendHumanAttentionWithdrawal(ctx, "owner", items[1].SourceEventID, map[string]any{"payload": map[string]any{"reason": "superseded"}}); err != nil {
		t.Fatal(err)
	}
	var budget struct {
		MaxQueries int    `json:"max_queries"`
		MaxRows    int    `json:"max_rows"`
		MaxVM      uint64 `json:"max_vm_steps"`
		MaxScan    uint64 `json:"max_full_scan_steps"`
	}
	data, err := os.ReadFile("testdata/inbox_item_read_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &budget); err != nil {
		t.Fatal(err)
	}
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := p.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	for _, size := range []int{0, 4096} {
		if size > 0 {
			for _, query := range []string{
				`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) SELECT 'noise-request-'||i,'message_posted','2026-01-01T00:00:00Z','owner','[]','{}' FROM n`,
				`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) SELECT 'noise-response-'||i,'message_posted','2026-01-01T00:00:00Z','owner','[]','{}' FROM n`,
				`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) SELECT 'noise-request-'||i,'noise-response-'||i,'withdrawn','owner','2026-01-01T00:00:00Z' FROM n`,
			} {
				if _, err = ws.DB().ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, id := range []string{"open", "closed"} {
			t.Run(fmt.Sprintf("%d/%s", size, id), func(t *testing.T) {
				scope := p.WithRequestAccessScope(ctx, p.AccessScope{ActorID: "owner"})
				if _, err = measured.GetOpenDerivedInboxItem(scope, "open"); err != nil {
					t.Fatal(err)
				}
				capture.Start()
				_, err = measured.GetOpenDerivedInboxItem(scope, id)
				statements, queries, rows := capture.Stop()
				if (id == "open" && err != nil) || (id == "closed" && !errors.Is(err, p.ErrNotFound)) {
					t.Fatalf("read: %v", err)
				}
				work := capture.Work()
				if err = capture.WorkError(); err != nil {
					t.Fatal(err)
				}
				t.Logf("queries=%d rows=%d vm=%d scans=%d", queries, rows, work.VMSteps, work.FullScanSteps)
				if queries > budget.MaxQueries || rows > budget.MaxRows || work.VMSteps > budget.MaxVM || work.FullScanSteps > budget.MaxScan {
					t.Fatalf("point budget: queries=%d rows=%d work=%+v", queries, rows, work)
				}
				found := false
				for _, stmt := range statements {
					plan, e := perfguard.Explain(ctx, ws.DB(), stmt)
					if e != nil {
						t.Fatal(e)
					}
					for _, line := range plan {
						if strings.Contains(line, "SEARCH r USING COVERING INDEX sqlite_autoindex_human_attention_request_resolutions_") {
							found = true
						}
						if strings.Contains(line, "SCAN r") {
							t.Fatalf("resolution scan: %s", line)
						}
					}
				}
				if !found {
					t.Fatal("missing indexed resolution lookup")
				}
			})
		}
	}
}
