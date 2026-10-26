package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceCanonicalAskAnswerLookup(t *testing.T) {
	requirePerformanceTest(t)
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	work, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Lookup subject"})
	if err != nil {
		t.Fatal(err)
	}
	ask, err := store.AppendEvent(ctx, "owner", map[string]any{"type": humanAttentionRequestedEventType, "thread_id": work["thread_id"], "refs": []string{anyString(work["ref"]), "thread:" + anyString(work["thread_id"])}, "payload": map[string]any{"kind": "ask", "title": "Proceed?", "requester_actor_id": "owner", "subject_ref": work["ref"], "response_proposals": []string{"Proceed"}}})
	if err != nil {
		t.Fatal(err)
	}
	item, ok := deriveHumanAttentionInboxItem(ask)
	if !ok {
		t.Fatal("missing identity")
	}
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	var budget struct {
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VM      uint64 `json:"max_vm_steps"`
		Scans   uint64 `json:"max_full_scan_steps"`
	}
	raw, err := os.ReadFile("testdata/ask_answer_lookup_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 4096} {
		if size > 0 {
			if _, err = ws.DB().ExecContext(ctx, `WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) SELECT 'answer-noise-'||i,'human_attention_requested','2026-01-01T00:00:00Z','owner','[]','{}' FROM n`); err != nil {
				t.Fatal(err)
			}
		}
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
			if _, err = resolveInboxItemByVariants(scope, measured, item.ID); err != nil {
				t.Fatal(err)
			}
			capture.Start()
			got, err := resolveInboxItemByVariants(scope, measured, item.ID)
			_, queries, rows := capture.Stop()
			if err != nil || got.SourceEventID != ask["id"] {
				t.Fatalf("lookup %#v %v", got, err)
			}
			work := capture.Work()
			if err = capture.WorkError(); err != nil {
				t.Fatal(err)
			}
			t.Logf("queries=%d rows=%d vm=%d scans=%d", queries, rows, work.VMSteps, work.FullScanSteps)
			if queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps > budget.Scans {
				t.Fatalf("lookup exceeds budget: queries=%d rows=%d work=%+v", queries, rows, work)
			}
		})
	}
}
