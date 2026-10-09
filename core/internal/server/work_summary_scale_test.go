package server

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Measure summary enrichment after the canonical page selector has authorized
// and hydrated its bounded inputs. The broad route matrix remains separate;
// its source-pinned existing-main exceptions must not hide new summary costs.
func TestPerformanceWorkSummaryBudgetAndPlans(t *testing.T) {
	// Serial: performance samples must not compete with parallel fixtures.
	requirePerformanceTest(t)
	allowed := performancePlanExceptions(t)
	var observed []perfguard.PlanException
	seenPlans := map[string]bool{}
	t.Cleanup(func() {
		if path := os.Getenv("ANX_PERFORMANCE_REPORT"); path != "" {
			raw, err := json.MarshalIndent(observed, "", "  ")
			if err == nil {
				err = os.WriteFile(path, raw, 0600)
			}
			if err != nil {
				t.Error(err)
			}
		}
	})
	env := newPerformanceEnv(t)
	if _, err := env.db.Exec(`UPDATE derived_inbox_items SET category='ask',data_json=json_set(data_json,'$.kind','ask','$.subject_ref','card:'||source_card_id) WHERE source_card_id LIKE 'scale-card-%'`); err != nil {
		t.Fatal(err)
	}
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
	if !large["work_summary_asks"] {
		t.Fatal("scale fixture lacks dense attention index")
	}
	for _, principal := range env.principals {
		for _, size := range []int{1, 50} {
			ctx := primitives.WithRequestAccessScope(context.Background(), primitives.AccessScope{ActorID: principal.ActorID, PMActorID: env.agent.ActorID})
			page, err := s.ListWork(ctx, primitives.WorkListFilter{Limit: size})
			if err != nil || len(page.Work) != size {
				t.Fatalf("page size %d: rows=%d err=%v", size, len(page.Work), err)
			}
			ctx, closeRead, err := s.BeginOverviewRead(ctx)
			if err != nil {
				t.Fatal(err)
			}
			capture.Start()
			start := time.Now()
			err = s.EnrichCardPlans(ctx, page.Work, func(string, string) bool { return true }, time.Now().UTC(), 0)
			elapsed := time.Since(start)
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			closeRead()
			if err != nil || capture.WorkError() != nil {
				t.Fatalf("summary error=%v instrumentation=%v", err, capture.WorkError())
			}
			if queries > 100 || rows > 1024 || work.VMSteps > 50000 {

				t.Errorf("summary budget size=%d SQL=%d rows=%d VM=%d", size, queries, rows, work.VMSteps)
			}
			for _, statement := range statements {
				details, err := perfguard.Explain(context.Background(), env.db, statement)
				if err != nil {
					t.Fatal(err)
				}
				if findings := perfguard.Findings(statement.SQL, details, large); len(findings) > 0 {
					hash, planHash := perfguard.PlanSQLHash(statement.SQL), perfguard.PlanHash(details)
					key := hash + "\n" + planHash
					if !seenPlans[key] {
						seenPlans[key] = true
						observed = append(observed, perfguard.PlanException{SQLHash: hash, PlanHash: planHash, Findings: findings})
					}
					for _, finding := range findings {
						if !allowed[key+"\n"+finding] {
							t.Errorf("unreviewed summary query plan finding=%s sql_sha256=%s plan_sha256=%s", finding, hash, planHash)
						}
					}
				}
			}
			for _, card := range page.Work {
				summary, ok := card["work_summary"].(*primitives.WorkSummary)
				if !ok || summary.Status.State == "" || summary.Status.Label == "" || summary.Status.Reason == "" {
					t.Fatal("missing status", card)
				}
				raw, _ := json.Marshal(summary)
				if strings.Contains(string(raw), "PrivatePerformanceSecret") {
					t.Fatal("private summary leaked")
				}
			}
			t.Logf("summary actor=%s size=%d SQL=%d rows=%d VM=%d elapsed=%s", principal.ActorID, size, queries, rows, work.VMSteps, elapsed)
			// Timeline refs are capped before this batched selector, including
			// handle aliases. Certify hydration as well as summary computation.
			refs := make([]string, len(page.Work))
			for i, card := range page.Work {
				refs[i] = anyString(card["ref"])
			}
			ctx = primitives.WithRequestAccessScope(context.Background(), primitives.AccessScope{ActorID: principal.ActorID, PMActorID: env.agent.ActorID})
			ctx, closeRead, err = s.BeginOverviewRead(ctx)
			if err != nil {
				t.Fatal(err)
			}
			capture.Start()
			loaded, err := s.SummaryCardSnapshots(ctx, refs)
			statements, queries, rows = capture.Stop()
			work = capture.Work()
			closeRead()
			if err != nil || capture.WorkError() != nil || len(loaded) != size || queries > 100 || rows > 1024 || work.VMSteps > 50000 {
				t.Fatalf("timeline summary selector size=%d loaded=%d SQL=%d rows=%d VM=%d err=%v instrumentation=%v", size, len(loaded), queries, rows, work.VMSteps, err, capture.WorkError())
			}
			for _, statement := range statements {
				details, err := perfguard.Explain(context.Background(), env.db, statement)
				if err != nil {
					t.Fatal(err)
				}
				if findings := perfguard.Findings(statement.SQL, details, large); len(findings) > 0 {
					hash, planHash := perfguard.PlanSQLHash(statement.SQL), perfguard.PlanHash(details)
					key := hash + "\n" + planHash
					if !seenPlans[key] {
						seenPlans[key] = true
						observed = append(observed, perfguard.PlanException{SQLHash: hash, PlanHash: planHash, Findings: findings})
					}
					for _, finding := range findings {
						if !allowed[key+"\n"+finding] {
							t.Errorf("unreviewed timeline query plan finding=%s sql_sha256=%s plan_sha256=%s", finding, hash, planHash)
						}
					}
				}
			}
			t.Logf("timeline selector actor=%s size=%d SQL=%d rows=%d VM=%d", principal.ActorID, size, queries, rows, work.VMSteps)
		}
	}
}

func TestPerformanceAgentSummarySelection(t *testing.T) {
	// Serial: performance samples must not compete with parallel fixtures.
	requirePerformanceTest(t)
	env := newPerformanceEnv(t)
	if _, err := env.db.Exec(`UPDATE cards SET assignee='scale-summary-actor' WHERE id LIKE 'scale-card-%'`); err != nil {
		t.Fatal(err)
	}
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
	for _, private := range []bool{false, true} {
		if private {
			if _, err := env.db.Exec(`UPDATE cards SET board_id='scale-board-1' WHERE id LIKE 'scale-card-%'`); err != nil {
				t.Fatal(err)
			}
		}
		for _, principal := range env.principals {
			ctx := primitives.WithRequestAccessScope(context.Background(), primitives.AccessScope{ActorID: principal.ActorID, PMActorID: env.agent.ActorID})
			ctx, closeRead, err := s.BeginOverviewRead(ctx)
			if err != nil {
				t.Fatal(err)
			}
			capture.Start()
			cards, truncated, err := s.AgentSummaryCards(ctx, "actor:scale-summary-actor", nil)
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			closeRead()
			if err != nil || capture.WorkError() != nil || len(cards) > 50 || !truncated || queries > 10 || rows > 150 || work.VMSteps > 50000 {
				t.Fatalf("agent selection cards=%d truncated=%v SQL=%d rows=%d VM=%d err=%v instrumentation=%v", len(cards), truncated, queries, rows, work.VMSteps, err, capture.WorkError())
			}
			if private && principal.ActorID == "scale-stranger-actor" && len(cards) != 0 {
				t.Fatal("dense private candidates leaked", cards)
			}
			if !private && len(cards) == 0 {
				t.Fatal("public positive control missing")
			}
			indexed := false
			for _, statement := range statements {
				if !strings.Contains(statement.SQL, "assigned_raw AS MATERIALIZED") {
					continue
				}
				details, err := perfguard.Explain(context.Background(), env.db, statement)
				if err != nil {
					t.Fatal(err)
				}
				plan := strings.Join(details, "\n")
				indexed = strings.Count(plan, "SEARCH cards USING INDEX idx_cards_agent_summary (assignee=?)") == 2
				if !indexed {
					t.Fatalf("assignment selectors lost indexed seeks: %s", plan)
				}
			}
			if !indexed {
				t.Fatal("agent selector was not exercised")
			}
			for _, card := range cards {
				if strings.Contains(anyString(card["title"]), "PrivatePerformanceSecret") {
					t.Fatal("private card leaked")
				}
			}
			t.Logf("agent selector private_prefix=%v actor=%s cards=%d SQL=%d rows=%d VM=%d", private, principal.ActorID, len(cards), queries, rows, work.VMSteps)
		}
	}
}
