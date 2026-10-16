package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceReportActivitySubjectsBudgetAndPlans(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("advisory scale gate")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if err = perfguard.Seed(ctx, ws.DB(), ws.Layout().ArtifactContentDir, "activity-owner", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var budget struct {
		Subjects int    `json:"max_subjects"`
		Queries  int    `json:"max_queries"`
		Rows     int    `json:"max_rows"`
		VM       uint64 `json:"max_vm_steps"`
	}
	raw, err := os.ReadFile("testdata/report_activity_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	refs, threads := []string{}, []string{}
	rows, err := ws.DB().Query(`SELECT 'card:'||COALESCE(NULLIF(handle,''),id),'thread:'||thread_id FROM cards WHERE id LIKE 'scale-card-%' ORDER BY id LIMIT ?`, budget.Subjects)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ref, thread string
		if err = rows.Scan(&ref, &thread); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
		threads = append(threads, thread)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(refs) != 200 {
		t.Fatalf("fixture subjects=%d err=%v", len(refs), err)
	}
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := p.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	for _, actor := range []string{"activity-owner", "stranger"} {
		for _, threadOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/thread=%v", actor, threadOnly), func(t *testing.T) {
				scope := p.WithAccessScope(ctx, p.AccessScope{ActorID: actor, PMActorID: actor})
				scope, closeRead, err := store.BeginResourceRead(scope)
				if err != nil {
					t.Fatal(err)
				}
				defer closeRead()
				batch := refs
				if threadOnly {
					batch = threads
				}
				// Admission is measured by the existing HTTP scale gates; this budget pins
				// the incremental enrichment after admission, at the maximum candidate cap.
				capture.Start()
				facts, err := store.ReportActivitySubjects(scope, batch, func(_ string, owner string) bool { return owner == "" || owner == actor })
				if err != nil {
					t.Fatal(err)
				}
				names, err := actors.NewStore(pool).DisplayNames(scope, []string{"activity-owner"})
				if err != nil {
					t.Fatal(err)
				}
				statements, queries, returned := capture.Stop()
				work := capture.Work()
				if err = capture.WorkError(); err != nil {
					t.Fatal(err)
				}
				resolved := 0
				for _, fact := range facts {
					if fact.Resolvable {
						resolved++
					}
				}
				if len(facts) != 200 || resolved == 0 {
					t.Fatalf("missing positive subject control: %d/%d", resolved, len(facts))
				}
				_ = names
				t.Logf("queries=%d rows=%d VM=%d fullscan=%d", queries, returned, work.VMSteps, work.FullScanSteps)
				if queries > budget.Queries || returned > budget.Rows || work.VMSteps > budget.VM {
					t.Fatalf("activity enrichment exceeded fixed budget: %d %d %+v", queries, returned, work)
				}
				for _, statement := range statements {
					if strings.Contains(statement.SQL, "owner.thread_id=requested.value") {
						plan, e := perfguard.Explain(scope, ws.DB(), statement)
						if e != nil {
							t.Fatal(e)
						}
						if !strings.Contains(fmt.Sprint(plan), "idx_cards_access_thread") && !strings.Contains(fmt.Sprint(plan), "idx_cards_thread_id") {
							t.Fatalf("thread owner lookup lost index: %v", plan)
						}
					}
				}
			})
		}
	}
}
