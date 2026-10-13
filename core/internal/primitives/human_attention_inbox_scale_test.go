package primitives_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
)

const inboxAskPlanIndex = "USING INDEX idx_events_ask_requester"

// seedInboxAskScale inserts background human-attention asks owned by other
// principals directly into events. The inbox page query filters on the
// requester inside the payload, so scale must come from rows the query reads,
// not from store-level fixtures.
func seedInboxAskScale(t *testing.T, ctx context.Context, ws *storage.Workspace, background int) {
	t.Helper()
	query := `WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<?)
	INSERT INTO events(id,handle,type,ts,actor_id,refs_json,payload_json)
	SELECT 'inbox-scale-'||i,
	       '',
	       'human_attention_requested',
	       strftime('%Y-01-01T00:00:00.000Z','2000-01-01','+'||(i%9000)||' seconds'),
	       'other-'||(i%64),
	       '[]',
	       json_object('kind','ask','title','Background decision','requester_actor_id','other-'||(i%64))
	FROM n`
	if _, err := ws.DB().ExecContext(ctx, query, background+1); err != nil {
		t.Fatalf("seed background asks: %v", err)
	}
}

func newInboxAskStore(t *testing.T) (context.Context, *storage.Workspace, *p.Store) {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	store := p.NewStore(ws.DB(), blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir)
	return ctx, ws, store
}

// The inbox page must seek one principal's asks through the partial requester
// index on every plan, including the fast tier; the index is useless if the
// query and the migration expression drift apart.
func TestAgentInboxAsksPagePlansUseRequesterIndex(t *testing.T) {
	ctx, ws, _ := newInboxAskStore(t)
	seedInboxAskScale(t, ctx, ws, 8)
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := p.NewStore(pool, blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir)
	capture.Start()
	if _, err := measured.ListHumanAttentionInboxAsksPage(ctx, "caller", "", 100); err != nil {
		t.Fatal(err)
	}
	statements, _, _ := capture.Stop()
	if len(statements) == 0 {
		t.Fatal("no statements captured")
	}
	planSawIndex := false
	for _, statement := range statements {
		plan, err := perfguard.Explain(ctx, ws.DB(), statement)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range plan {
			if strings.Contains(line, "SCAN events") || strings.Contains(line, "SCAN ask") {
				t.Fatalf("inbox asks page scans events instead of seeking: %s", line)
			}
			if strings.Contains(line, inboxAskPlanIndex) {
				planSawIndex = true
			}
		}
	}
	if !planSawIndex {
		t.Fatalf("inbox asks page does not use %s: %d statements planned", "idx_events_ask_requester", len(statements))
	}
}

// Scale tier: one caller with few asks inside a workspace where other
// principals own the ask history. The page read must cost the caller's page,
// not the workspace's history.
func TestPerformanceAgentInboxAsksPageBudgets(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("advisory performance tier: make -C core test-perf")
	}
	ctx, ws, store := newInboxAskStore(t)
	seedInboxAskScale(t, ctx, ws, 20000)
	for i := 0; i < 24; i++ {
		if _, err := store.AppendEvent(ctx, "caller", map[string]any{
			"type": "human_attention_requested", "thread_id": fmt.Sprintf("thread-caller-%02d", i),
			"refs":    []string{"thread:thread-caller"},
			"payload": map[string]any{"kind": "ask", "title": fmt.Sprintf("Caller decision %d", i), "requester_actor_id": "caller"},
		}); err != nil {
			t.Fatalf("seed caller ask: %v", err)
		}
	}
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := p.NewStore(pool, blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir)
	// Warm statement preparation so the budget measures steady-state reads.
	if _, err := measured.ListHumanAttentionInboxAsksPage(ctx, "caller", "", 100); err != nil {
		t.Fatal(err)
	}
	capture.Start()
	page, err := measured.ListHumanAttentionInboxAsksPage(ctx, "caller", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	statements, queries, rows := capture.Stop()
	work := capture.Work()
	if e := capture.WorkError(); e != nil {
		t.Fatal(e)
	}
	if len(page.Items) != 24 {
		t.Fatalf("expected the caller's 24 asks, got %d", len(page.Items))
	}
	// Steady state is one indexed seek over the caller's asks plus one
	// public-ref lookup per returned row (the shared page-bounded decode), so
	// every budget scales with the requested page, never with the 20k-ask
	// background history. FullScanSteps must stay near zero: any scan of the
	// ask history means the requester index stopped being used.
	t.Logf("queries=%d rows=%d VM=%d fullscan=%d", queries, rows, work.VMSteps, work.FullScanSteps)
	if queries > 2*len(page.Items)+8 || rows > 4*len(page.Items)+16 || work.VMSteps > 20000 || work.FullScanSteps > 0 {
		t.Fatalf("inbox asks page grows with workspace history: queries=%d rows=%d work=%+v items=%d", queries, rows, work, len(page.Items))
	}
	for _, statement := range statements {
		plan, e := perfguard.Explain(ctx, ws.DB(), statement)
		if e != nil {
			t.Fatal(e)
		}
		for _, line := range plan {
			if strings.Contains(line, "SCAN events") || strings.Contains(line, "SCAN ask") {
				t.Fatalf("unindexed inbox page plan at scale: %s", line)
			}
		}
	}
}
