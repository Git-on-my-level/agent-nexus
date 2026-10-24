package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
	reports "agent-nexus-visualreport"
)

func TestWatchTwentyTwoLoopPanelsPerformance(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	s := h.primitiveStore.(*primitives.Store)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Loops", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	panels := []reports.Panel{}
	for i := 0; i < 22; i++ {
		w, err := s.CreateWork(ctx, "owner", anyString(board["id"]), map[string]any{"title": fmt.Sprintf("Loop %d", i), "cadence": map[string]any{"every_seconds": 3600}, "goal": map[string]any{"metric": "score", "direction": "up"}})
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 2; j++ {
			_, err = s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Measured", "thread_id": w["thread_id"], "refs": []string{anyString(w["ref"])}, "payload": map[string]any{"run_id": fmt.Sprint(j), "outcome": "acted", "metrics": map[string]any{"score": float64(j)}}})
			if err != nil {
				t.Fatal(err)
			}
		}
		panels = append(panels, reports.Panel{ID: fmt.Sprintf("loop-%d", i), Type: "live-loop", Query: reports.Query{Card: anyString(w["ref"]), Limit: 10}})
	}
	pool, capture, err := perfguard.Open("file:" + h.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, h.workspace.Layout().ArtifactContentDir)
	req := httptest.NewRequest("GET", "/docs/loops/report", nil).WithContext(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "owner"}))
	opts := handlerOptions{primitiveStore: measured, reportPanels: &reportPanelCache{ttl: time.Hour}}
	// The original implementation materialized all panels serially without a
	// result cache; preserve that comparator rather than fabricating a sleep.
	capture.Start()
	start := time.Now()
	reader := reportReader{r: req, opts: opts, now: time.Now(), visibility: map[string]bool{}}
	for _, panel := range panels {
		if r := reader.renderPanel(panel); r["status"] != "ok" {
			t.Fatal(r)
		}
	}
	before := time.Since(start)
	_, beforeQueries, _ := capture.Stop()
	capture.Start()
	start = time.Now()
	_, cold := materializeReportPanels(req, opts, panels)
	coldTime := time.Since(start)
	_, coldQueries, _ := capture.Stop()
	for _, r := range cold {
		if r["status"] != "ok" {
			t.Fatal(r)
		}
	}
	capture.Start()
	start = time.Now()
	_, warm := materializeReportPanels(req, opts, panels)
	warmTime := time.Since(start)
	_, warmQueries, _ := capture.Stop()
	work := capture.Work()
	for i, r := range warm {
		if r["status"] != "ok" || r["id"] != panels[i].ID || r["role"] != "primary" {
			t.Fatal(r)
		}
	}
	t.Logf("22 loops: serial=%s queries=%d; bounded cold=%s queries=%d; cached=%s queries=%d vm=%d", before, beforeQueries, coldTime, coldQueries, warmTime, warmQueries, work.VMSteps)
	if warmQueries >= beforeQueries/2 {
		t.Fatalf("cache did not remove panel SQL: before=%d warm=%d", beforeQueries, warmQueries)
	}
	if warmQueries > 32 || work.VMSteps > 50000 {
		t.Fatalf("warm report exceeded fixed budget: queries=%d vm=%d", warmQueries, work.VMSteps)
	}
}

// No database or scheduler sleeps: the barrier proves four different panels
// are in flight and preserves the authored order when they complete.
type watchConcurrentStore struct {
	PrimitiveStore
	entered chan struct{}
	release chan struct{}
}

func (s *watchConcurrentStore) ListReportWork(context.Context, primitives.ReportWorkFilter) (primitives.ReportWorkPage, error) {
	s.entered <- struct{}{}
	<-s.release
	return primitives.ReportWorkPage{Work: []map[string]any{}}, nil
}
func TestWatchReportWorkerBound(t *testing.T) {
	store := &watchConcurrentStore{entered: make(chan struct{}, 32), release: make(chan struct{})}
	panels := []reports.Panel{}
	for i := 0; i < 22; i++ {
		panels = append(panels, reports.Panel{ID: fmt.Sprint(i), Type: "live-cards", Query: reports.Query{Label: fmt.Sprint(i), Limit: 10}})
	}
	done := make(chan []map[string]any, 1)
	go func() {
		_, rows := materializeReportPanels(httptest.NewRequest("GET", "/", nil), handlerOptions{primitiveStore: store}, panels)
		done <- rows
	}()
	for i := 0; i < reportWorkers; i++ {
		select {
		case <-store.entered:
		case <-time.After(5 * time.Second):
			close(store.release)
			t.Fatal("panels did not run concurrently")
		}
	}
	select {
	case <-store.entered:
		close(store.release)
		t.Fatal("worker bound exceeded")
	default:
	}
	close(store.release)
	select {
	case rows := <-done:
		for i, row := range rows {
			if row["id"] != panels[i].ID {
				t.Fatal("panel order changed")
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("workers did not finish")
	}
}

func TestPerformanceWatchCollectionBudget(t *testing.T) {
	requirePerformanceTest(t)
	var budget struct {
		Cards   int    `json:"fixture_unrelated_cards"`
		Loops   int    `json:"fixture_loops"`
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VM      uint64 `json:"max_vm_steps"`
	}
	raw, err := os.ReadFile("testdata/watch_collection_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	if budget.Cards != 4096 || budget.Loops != 22 {
		t.Fatal("scale fixture drift")
	}

	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	s := h.primitiveStore.(*primitives.Store)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Watch", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.workspace.DB().Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('unrelated-thread','now','owner','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = h.workspace.DB().Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<4096) INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) SELECT printf('unrelated-%05d',i),'Unrelated','','todo','unrelated-thread','now','owner','now','owner' FROM n`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 22; i++ {
		if _, err = s.CreateWork(ctx, "owner", anyString(board["id"]), map[string]any{"title": fmt.Sprintf("Loop %d", i), "cadence": map[string]any{"every_seconds": 3600}}); err != nil {
			t.Fatal(err)
		}
	}
	pool, capture, err := perfguard.Open("file:" + h.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, h.workspace.Layout().ArtifactContentDir)
	for _, actor := range []string{"owner", "stranger"} {
		for _, collection := range []string{"initiatives", "loops"} {
			req := httptest.NewRequest("GET", "/docs/watch/report", nil).WithContext(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor}))
			capture.Start()
			start := time.Now()
			_, rows := materializeReportPanels(req, handlerOptions{primitiveStore: measured}, []reports.Panel{{ID: "collection", Type: "live-collection", Query: reports.Query{Collection: collection, Limit: 24, GroupBy: "none"}}})
			elapsed := time.Since(start)
			statements, queries, returned := capture.Stop()
			work := capture.Work()
			if capture.WorkError() != nil {
				t.Fatal(capture.WorkError())
			}
			if rows[0]["status"] != "ok" || reportMap(rows[0]["data"])["has_more"] != false {
				t.Fatal(rows)
			}
			groups := reportMap(rows[0]["data"])["groups"].([]map[string]any)
			if len(groups) != 1 || len(groups[0]["rows"].([]map[string]any)) != 22 {
				t.Fatal(groups)
			}
			t.Logf("%s %s +4096 unrelated: queries=%d rows=%d vm=%d latency=%s", collection, actor, queries, returned, work.VMSteps, elapsed)
			if queries > budget.Queries || returned > budget.Rows || work.VMSteps > budget.VM {
				t.Fatalf("collection exceeded fixed budget: queries=%d rows=%d vm=%d", queries, returned, work.VMSteps)
			}
			for _, statement := range statements {
				if strings.Contains(statement.SQL, "_work_candidates") {
					details, err := perfguard.Explain(ctx, h.workspace.DB(), statement)
					if err != nil {
						t.Fatal(err)
					}
					plan := strings.Join(details, "\n")
					if strings.Contains(plan, "SCAN c") || strings.Contains(plan, "SCAN cards") || strings.Contains(plan, "SCAN events") {
						t.Fatal("unbounded collection admission: " + plan)
					}
				}
			}
		}
	}
}

func TestPerformanceWatchAgentCollectionAdmission(t *testing.T) {
	requirePerformanceTest(t)
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	// Humans sort before agents: a return-row LIMIT alone cannot bound a scan
	// through unrelated principal kinds.
	if _, err := h.workspace.DB().Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<4096) INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) SELECT printf('human-%05d',i),printf('a-human-%05d',i),printf('human-actor-%05d',i),'now','now','{"principal_kind":"human"}' FROM n`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workspace.DB().Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<22) INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) SELECT printf('agent-%05d',i),printf('z-agent-%05d',i),printf('agent-actor-%05d',i),'now','now','{"principal_kind":"agent"}' FROM n`); err != nil {
		t.Fatal(err)
	}
	pool, capture, err := perfguard.Open("file:" + h.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	opts := handlerOptions{
		primitiveStore: primitives.NewTestStore(pool, h.workspace.Layout().ArtifactContentDir),
		runStore:       commandcenter.NewStore(pool, commandcenter.SQLIdentities{DB: pool}),
	}
	req := httptest.NewRequest("GET", "/docs/watch/report", nil).WithContext(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "viewer"}))
	capture.Start()
	_, rows := materializeReportPanels(req, opts, []reports.Panel{{ID: "agents", Type: "live-collection", Query: reports.Query{Collection: "agents", Limit: 24, GroupBy: "none"}}})
	statements, queries, returned := capture.Stop()
	work := capture.Work()
	if err := capture.WorkError(); err != nil {
		t.Fatal(err)
	}
	if rows[0]["status"] != "ok" || reportMap(rows[0]["data"])["has_more"] != false {
		t.Fatal(rows)
	}
	groups := reportMap(rows[0]["data"])["groups"].([]map[string]any)
	if len(groups) != 1 || len(groups[0]["rows"].([]map[string]any)) != 22 {
		t.Fatal(groups)
	}
	t.Logf("22 agents +4096 unrelated humans: queries=%d rows=%d vm=%d", queries, returned, work.VMSteps)
	indexedAdmission := false
	for _, statement := range statements {
		if strings.Contains(statement.SQL, "FROM agents a") {
			details, err := perfguard.Explain(ctx, h.workspace.DB(), statement)
			if err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(details, "\n")
			t.Logf("agent admission plan:\n%s", plan)
			indexedAdmission = strings.Contains(plan, "SEARCH a USING INDEX idx_agents_collection_roster") || strings.Contains(plan, "SEARCH _row USING INDEX idx_agents_collection_roster")
		}
	}
	if !indexedAdmission {
		t.Error("agent admission must seek principal kind through idx_agents_collection_roster")
	}
	if queries > 50 || returned > 512 || work.VMSteps > 50000 {
		t.Fatalf("agent collection exceeded standard VM budget: queries=%d rows=%d vm=%d", queries, returned, work.VMSteps)
	}
}
