package server

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Certify all new batched card/activity selectors against the shared 4096-card
// corpus. Exact constant-time membership predicates are the only permitted
// plan exceptions; candidate-table scans still fail the gate.
func TestPerformancePMCardContext(t *testing.T) {
	requirePerformanceTest(t)
	env := newPerformanceEnv(t)
	var budget struct {
		MaxQueries int    `json:"max_queries"`
		MaxRows    int    `json:"max_rows"`
		WarmVM     uint64 `json:"max_warm_vm_steps"`
		ColdVM     uint64 `json:"max_cold_vm_steps"`
	}
	budgetJSON, e := os.ReadFile("testdata/pm_card_context_budget.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(budgetJSON, &budget); e != nil {
		t.Fatal(e)
	}
	allowed := performancePlanExceptions(t)
	observed := []perfguard.PlanException{}
	seen := map[string]bool{}
	t.Cleanup(func() {
		if path := os.Getenv("ANX_PERFORMANCE_REPORT"); path != "" {
			raw, _ := json.MarshalIndent(observed, "", "  ")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Error(err)
			}
		}
	})
	// Each selected thread has a dense denied prefix. Fixed windows must not
	// refill from older public history or scan through the hidden asks.
	if _, err := env.db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<32),c(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM c WHERE x<8)
INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) SELECT 'aaa-pm-hidden-'||c.x||'-'||n.x,'human_attention_requested','2030-01-01T00:00:00Z','scale-owner-actor','scale-thread-'||c.x,json_array('card:scale-card-16'),json_object('payload',json_object('title','Hidden evidence')) FROM n CROSS JOIN c`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(`INSERT INTO ask_subjects(ask_id,card_id,requester,thread_id,open) SELECT id,'scale-card-'||substr(thread_id,14),'scale-owner-actor',thread_id,1 FROM events WHERE id LIKE 'aaa-pm-hidden-%' ON CONFLICT(ask_id) DO UPDATE SET card_id=excluded.card_id`); err != nil {
		t.Fatal(err)
	}

	// The shared fixture co-locates a document with every card. Private
	// document contributors deliberately constrain that document and its thread;
	// move those documents so this case exercises readable cards with hidden
	// message candidates instead of intentionally denying the entire pin.
	if _, e := env.db.Exec(`UPDATE documents SET thread_id='scale-thread-11' WHERE id IN ('scale-doc-1','scale-doc-2','scale-doc-3','scale-doc-4','scale-doc-5','scale-doc-6','scale-doc-7','scale-doc-8','scale-doc-9','scale-doc-10')`); e != nil {
		t.Fatal(e)
	}
	// Populated plans force batched referenced-card facts, not just empty plans.
	planJSON := `{"steps":[{"id":"done","title":"Accepted","status":"done"},{"id":"next","title":"Referenced card","ref":"card:scale-card-10"}]}`
	for i := 1; i <= 8; i++ {
		if _, e := env.db.Exec(`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES(?,?,'2026-10-10T00:00:00Z') ON CONFLICT(card_id) DO UPDATE SET body_json=excluded.body_json`, fmt.Sprintf("scale-card-%d", i), planJSON); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := env.db.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<128),c(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM c WHERE x<10)
INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) SELECT 'zzz-pm-private-message-'||c.x||'-'||n.x,'message_posted','2031-01-01T00:00:00Z','scale-owner-actor','scale-thread-'||c.x,json_array('card:scale-card-16'),json_object('payload',json_object('text','PrivatePerformanceSecret')) FROM n CROSS JOIN c`); e != nil {
		t.Fatal(e)
	}
	large := largeTablesForPM(t, env)
	checkPlans := func(statements []perfguard.Statement) {
		t.Helper()
		for _, statement := range statements {
			details, e := perfguard.Explain(context.Background(), env.db, statement)
			if e != nil {
				t.Fatal(e)
			}
			findings := perfguard.Findings(statement.SQL, details, large)
			if len(findings) == 0 {
				continue
			}
			hash, planHash := perfguard.PointPlanSQLHash(statement.SQL), perfguard.PlanHash(details)
			key := hash + "\n" + planHash
			if !seen[key] {
				seen[key] = true
				observed = append(observed, perfguard.PlanException{SQLHash: hash, PlanHash: planHash, Findings: findings})
			}
			for _, finding := range findings {
				if !allowed[key+"\n"+finding] {
					t.Errorf("unreviewed card plan %s sql=%s plan=%s", finding, hash, planHash)
				}
			}
		}
	}
	var seq int
	var name, path string
	if err := env.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + path + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := primitives.NewTestStore(db, "")
	for _, principal := range env.principals {
		for _, size := range []int{1, 8} {
			refs := []string{}
			for i := 1; i <= size; i++ {
				refs = append(refs, fmt.Sprintf("card:scale-card-%d", i))
			}
			ctx := primitives.WithRequestAccessScope(context.Background(), primitives.AccessScope{ActorID: principal.ActorID, PMActorID: env.agent.ActorID})
			ctx, closeRead, err := store.BeginPMCardRead(ctx)
			if err != nil {
				t.Fatal(err)
			}
			capture.Start()
			snapshots, err := store.PMCardSnapshots(ctx, refs)
			cards := []map[string]any{}
			for _, ref := range refs {
				if card, ok := snapshots[ref]; ok {
					cards = append(cards, card)
				}
			}
			if err == nil {
				var activity map[string][]map[string]any
				activity, err = store.PMCardActivity(ctx, cards, false)
				if principal.ActorID == "scale-stranger-actor" {
					for _, items := range activity {
						if len(items) > 0 {
							t.Error("denied prefix refilled from public history")
						}
					}
				}
			}
			if err == nil {
				var asks map[string][]map[string]any
				asks, err = store.PMCardActivity(ctx, cards, true)
				if principal.ActorID == "scale-stranger-actor" {
					for _, items := range asks {
						if len(items) > 0 {
							t.Error("denied ask leaked")
						}
					}
				}
			}
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			closeRead()
			if err != nil || capture.WorkError() != nil {
				t.Fatal(err, capture.WorkError())
			}
			if len(cards) != size || queries > 100 || rows > 512 || work.VMSteps > 50000 {
				t.Errorf("card context budget size=%d cards=%d SQL=%d rows=%d VM=%d", size, len(cards), queries, rows, work.VMSteps)
			}
			checkPlans(statements)
			raw := fmt.Sprint(cards)
			if strings.Contains(raw, "PrivatePerformanceSecret") {
				t.Fatal("private content leaked")
			}
			t.Logf("actor=%s size=%d SQL=%d rows=%d VM=%d", principal.ActorID, size, queries, rows, work.VMSteps)
		}
	}
	// Capture the complete HTTP operation separately: auth, lease/pin lookup,
	// cold/warm denial admission, projection, and pending decision attachment.
	for _, principal := range env.principals {
		for _, size := range []int{1, 8} {
			refs := []string{}
			for i := 1; i <= size; i++ {
				refs = append(refs, fmt.Sprintf("card:scale-card-%d", i))
			}
			cid := fmt.Sprintf("pm-cards-%s-%d", principal.ActorID, size)
			tid := cid + "-turn"
			now := time.Now().UTC()
			c := pm.Conversation{ID: cid, WorkspaceID: "ws_main", ActorID: principal.ActorID, WorkRef: refs[0], ContextRefs: refs, ThreadID: "scale-thread-1", CreatedAt: now}
			turn := pm.Turn{ID: tid, ConversationID: cid, WorkspaceID: "ws_main", ActorID: principal.ActorID, AgentActorID: env.agent.ActorID, Status: pm.Pending, Deadline: now.Add(time.Hour), LeaseToken: "synthetic-lease", LeaseOwner: env.agent.ActorID, LeaseExpiresAt: now.Add(time.Hour)}
			for kind, record := range map[string]any{"conversation": c, "turn": turn} {
				raw, _ := json.Marshal(record)
				id := cid
				if kind == "turn" {
					id = tid
				}
				if _, err := env.db.Exec(`INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES(?,?, 'ws_main',?,?,1,?)`, kind, id, principal.ActorID, cid, raw); err != nil {
					t.Fatal(err)
				}
			}
			for i, ref := range refs {
				for n := 0; n < 8; n++ {
					id := fmt.Sprintf("aaa-pm-decision-%s-%d-%d-%d", principal.ActorID, size, i, n)
					d := pm.Decision{ID: id, WorkspaceID: "ws_main", ActorID: "scale-owner-actor", WorkRef: ref, Scope: "work.phase", Instruction: "Hidden evidence card:scale-card-16", Status: pm.AwaitingAnswer, Revision: 1, CreatedAt: now}
					raw, _ := json.Marshal(d)
					if _, err := env.db.Exec(`INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('decision',?,'ws_main','scale-owner-actor','',1,?)`, id, raw); err != nil {
						t.Fatal(err)
					}
				}
			}
			h, fullCapture, _, closePool := env.fresh(t)
			for sample := 0; sample < 2; sample++ {
				req := httptest.NewRequest("POST", "/pm/turns/"+tid+"/context", strings.NewReader(`{"view":"cards","lease_token":"synthetic-lease"}`))
				req.Header.Set("Authorization", "Bearer "+env.agent.AccessToken)
				w := httptest.NewRecorder()
				fullCapture.Start()
				h.ServeHTTP(w, req)
				statements, queries, rows := fullCapture.Stop()
				work := fullCapture.Work()
				if w.Code != 200 || fullCapture.WorkError() != nil {
					t.Fatalf("complete view %d %s %v", w.Code, w.Body, fullCapture.WorkError())
				}
				if principal.ActorID == "scale-stranger-actor" && strings.Contains(w.Body.String(), "Hidden evidence") {
					t.Fatal("private pending decision leaked")
				}
				t.Logf("complete actor=%s size=%d sample=%d SQL=%d rows=%d VM=%d", principal.ActorID, size, sample, queries, rows, work.VMSteps)
				checkPlans(statements)
				vmLimit := budget.ColdVM // cold, existing denial snapshot admission on this 4096-card corpus
				if sample > 0 {
					vmLimit = budget.WarmVM
				} // warm, no per-statement closure rebuild
				if queries > budget.MaxQueries || rows > budget.MaxRows || work.VMSteps > vmLimit {
					t.Error("complete HTTP view budget exceeded")
				}
			}
			closePool()
		}
	}

}

func largeTablesForPM(t *testing.T, env performanceEnv) map[string]bool {
	t.Helper()
	large, e := perfguard.LargeTables(context.Background(), env.db)
	if e != nil {
		t.Fatal(e)
	}
	return large
}
