package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestInboxAskStalenessListGetSummaryAndStream(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Inactive subject", "phase": "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	item := streamPrivacyInboxItem(anyString(card["thread_id"]), "stale-projection", "Still open")
	item.Data["subject_ref"] = card["ref"]
	seedStreamPrivacyInbox(t, s, anyString(card["thread_id"]), item)
	if _, err = h.workspace.DB().Exec(`UPDATE cards SET updated_at=? WHERE id=?`, time.Now().Add(-8*24*time.Hour).UTC().Format(time.RFC3339Nano), card["id"]); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/inbox", "/inbox/stale-projection", "/inbox/summary"} {
		body := workGetJSON(t, h.baseURL+path, 200)
		var row map[string]any
		if path == "/inbox/stale-projection" {
			row = body["item"].(map[string]any)
		} else {
			key := "items"
			if path == "/inbox/summary" {
				key = "asks"
			}
			row = body[key].([]any)[0].(map[string]any)
		}
		if row["is_stale"] != true || row["stale_reason"] != "subject_inactive" || row["stale_since"] == nil {
			t.Fatalf("%s: %#v", path, row)
		}
	}
	req := httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "owner"}))
	rows, _, err := loadInboxStreamPage(req, handlerOptions{primitiveStore: s}, primitives.DerivedInboxListFilter{})
	if err != nil || len(rows) != 1 || rows[0]["is_stale"] != true {
		t.Fatalf("stream: %#v %v", rows, err)
	}
}

func TestInboxAskStalenessCrossesDeadlineWithoutWrite(t *testing.T) {
	requireIntegrationTest(t)
	h := newMetaStreamTestHarness(t, WithStreamPollInterval(100*time.Millisecond))
	s := h.primitiveStore.(*primitives.Store)
	primitives.WithAskDeliveryPolicy(nil, 2*time.Second)(s)
	ctx := context.Background()
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Aging subject", "phase": "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	item := streamPrivacyInboxItem(anyString(card["thread_id"]), "aging-ask", "Still open")
	item.Data["subject_ref"] = card["ref"]
	seedStreamPrivacyInbox(t, s, anyString(card["thread_id"]), item)
	resp := openSSEStream(t, h.baseURL+"/stream/inbox", "")
	events, stop := startSSEReader(resp.Body)
	defer stop()
	initial := awaitSSEEvent(t, events, 5*time.Second)
	first := initial.Data["item"].(map[string]any)
	if first["is_stale"] != false || first["stale_at"] == nil {
		t.Fatalf("initial %#v", initial)
	}
	changed := awaitSSEEvent(t, events, 5*time.Second)
	stale := changed.Data["item"].(map[string]any)
	if stale["is_stale"] != true || changed.ID == initial.ID || stale["stale_at"] != first["stale_at"] {
		t.Fatalf("deadline not emitted: %#v", changed)
	}
	select {
	case extra := <-events:
		t.Fatalf("stale item emitted repeatedly: %#v", extra)
	case <-time.After(300 * time.Millisecond):
	}
}

// Isolate the changed projection from preexisting page/authorization costs.
// Scope is primed before measuring, as it is by an authorized page loader.
func TestPerformanceInboxAskStalenessBudgetAndPlans(t *testing.T) {
	requirePerformanceTest(t)
	allowed := performancePlanExceptions(t)
	env := newPerformanceEnv(t)
	var sequence int
	var name, path string
	if err := env.db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + path + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := primitives.NewTestStore(db, "")
	large, err := perfguard.LargeTables(context.Background(), env.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, principal := range env.principals {
		for _, size := range []int{1, 100, 200} {
			ctx := primitives.WithRequestAccessScope(context.Background(), primitives.AccessScope{ActorID: principal.ActorID, PMActorID: env.agent.ActorID})
			// An authorized page loader admits the request denial snapshot
			// before projection; reproduce that boundary before capture.
			if _, err := s.ListWork(ctx, primitives.WorkListFilter{Limit: 1}); err != nil {
				t.Fatal(err)
			}
			ctx, closeRead, err := s.BeginOverviewRead(ctx)
			if err != nil {
				t.Fatal(err)
			}
			items := make([]map[string]any, size)
			for i := range items {
				items[i] = map[string]any{"kind": "ask", "subject_ref": fmt.Sprintf("card:scale-card-%d", i+1)}
			}
			capture.Start()
			start := time.Now()
			err = s.EnrichInboxAskStaleness(ctx, items, time.Now().UTC())
			elapsed := time.Since(start)
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			closeRead()
			if err != nil || capture.WorkError() != nil || queries > 100 || rows > 1024 || work.VMSteps > 50000 || elapsed > 500*time.Millisecond {
				t.Fatalf("staleness size=%d SQL=%d rows=%d VM=%d elapsed=%s err=%v instrumentation=%v", size, queries, rows, work.VMSteps, elapsed, err, capture.WorkError())
			}
			for _, statement := range statements {
				details, err := perfguard.Explain(context.Background(), env.db, statement)
				if err != nil {
					t.Fatal(err)
				}
				if findings := perfguard.Findings(statement.SQL, details, large); len(findings) > 0 {
					hash, planHash := perfguard.PlanSQLHash(statement.SQL), perfguard.PlanHash(details)
					for _, finding := range findings {
						if !allowed[hash+"\n"+planHash+"\n"+finding] {
							t.Errorf("unreviewed staleness projection sql_sha256=%s plan_sha256=%s finding=%s\n%s\n%s", hash, planHash, finding, statement.SQL, strings.Join(details, "\n"))
						}
					}
				}
			}
			if items[0]["stale_at"] == nil {
				t.Fatal("fixture failed to hydrate a visible card subject")
			}
			t.Logf("staleness actor=%s size=%d SQL=%d rows=%d VM=%d elapsed=%s", principal.ActorID, size, queries, rows, work.VMSteps, elapsed)
		}
	}
}
