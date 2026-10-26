package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceAskPresentationReadBudget(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("scale gate")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := p.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	ask, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "human_attention_requested", "refs": []string{}, "payload": map[string]any{"title": "Original?"}})
	if err != nil {
		t.Fatal(err)
	}
	audit, err := s.AppendEvent(ctx, "curator", map[string]any{"type": p.AskRevisedEventType, "refs": []string{fmt.Sprint(ask["ref"])}, "payload": map[string]any{"presentation_revision": 1, "previous": map[string]any{"title": "Original?"}, "presentation": map[string]any{"title": "Clear?"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO ask_presentation_heads VALUES(?,1,?)`, ask["id"], audit["id"]); err != nil {
		t.Fatal(err)
	}
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := p.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	var budget struct {
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VM      uint64 `json:"max_vm_steps"`
		Scans   uint64 `json:"max_full_scan_steps"`
	}
	raw, err := os.ReadFile("testdata/ask_presentation_read_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 4096} {
		if size > 0 {
			for _, query := range []string{
				`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) SELECT 'presentation-unrelated-'||i,'human_attention_requested','2026-01-01T00:00:00Z','other','[]','{}' FROM n`,
				`INSERT INTO ask_presentation_heads(ask_id,revision,event_id) SELECT id,1,id FROM events WHERE id LIKE 'presentation-unrelated-%'`,
			} {
				if _, err = ws.DB().Exec(query); err != nil {
					t.Fatal(err)
				}
			}
		}
		scope := p.WithReadTickSnapshot(p.WithAccessScope(ctx, p.AccessScope{ActorID: "owner"}))
		if _, err = measured.AskPresentationStates(scope, []map[string]any{ask}); err != nil {
			t.Fatal(err)
		}
		capture.Start()
		states, err := measured.AskPresentationStates(scope, []map[string]any{ask})
		stmts, queries, rows := capture.Stop()
		if err != nil {
			t.Fatal(err)
		}
		if states[fmt.Sprint(ask["id"])]["presentation_revision"] != 1 {
			t.Fatal(states)
		}
		work := capture.Work()
		if err = capture.WorkError(); err != nil {
			t.Fatal(err)
		}
		t.Logf("unrelated=%d queries=%d rows=%d VM=%d scans=%d", size, queries, rows, work.VMSteps, work.FullScanSteps)
		if queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps > budget.Scans {
			t.Fatalf("presentation budget exceeded: queries=%d rows=%d work=%+v", queries, rows, work)
		}
		for _, stmt := range stmts {
			plan, err := perfguard.Explain(ctx, ws.DB(), stmt)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range plan {
				if strings.Contains(line, "SCAN h") || strings.Contains(line, "SCAN e") {
					t.Fatalf("unindexed presentation read: %s", line)
				}
			}
		}
	}
}
