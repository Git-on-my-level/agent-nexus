package delivery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/testutil/perfguard"
)

// TestPerformanceAttentionSummaryBudget: with a principal-addressed channel,
// every active human is notified. 200 humans × 2000 open asks must not put
// access evaluation or ledger materialization on the heartbeat path, and the
// refresh step must stay within its fixed budgets.
func TestPerformanceAttentionSummaryBudget(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") == "" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	const humans, asks = 200, 2000
	e := newEnv(t)
	ctx := context.Background()
	e.addAgent(t, "agent_notifier", "actor_notifier", "agent", "managed_grant", nil)
	if err := e.declareGrant(t, "agent_notifier", hostedChannel); err != nil {
		t.Fatal(err)
	}
	tx, err := e.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < humans; i++ {
		meta, _ := json.Marshal(map[string]any{"principal_kind": "human", "auth_method": "external_grant", "external_issuer": "https://issuer.example", "external_subject": fmt.Sprintf("acct_%03d", i)})
		if _, err := tx.Exec(`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json) VALUES(?,?,?,?,?,NULL,?)`,
			fmt.Sprintf("agent_h%03d", i), fmt.Sprintf("h%03d", i), fmt.Sprintf("actor_h%03d", i), rfc(baseNow), rfc(baseNow), string(meta)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < asks; i++ {
		if _, err := tx.Exec(`INSERT INTO ask_deadlines(request_event_id, requester_actor_id, ask_ts, by_raw, decide_by, normalized) VALUES(?,?,?,?,?,1)`,
			fmt.Sprintf("ask%05d", i), "actor-a", rfc(baseNow), "1h", rfc(baseNow.Add(time.Duration(i%7-3)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var seq int
	var name, path string
	if err := e.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := delivery.Store{DB: db}
	var checks atomic.Int64
	// Readable unless (principal + ask) % 3 == 0: about 1333 readable asks
	// per principal, past any row cap, and different per principal.
	index := func(id, prefix string) int {
		var n int
		fmt.Sscanf(strings.TrimPrefix(id, prefix), "%d", &n)
		return n
	}
	readable := func(p, a int) bool { return (p+a)%3 != 0 }
	canRead := func(_ context.Context, principal, ask string) bool {
		checks.Add(1)
		return readable(index(principal, "actor_h"), index(ask, "ask"))
	}

	// One refresh step: bounded access checks and SQL whatever the corpus.
	capture.Start()
	if err := store.RefreshReadability(ctx, baseNow, canRead); err != nil {
		t.Fatal(err)
	}
	_, stepSQL, _ := capture.Stop()
	if n := checks.Load(); n > 1000 {
		t.Fatalf("one refresh step evaluated %d access checks (budget 1000)", n)
	}
	if stepSQL > 1500 {
		t.Fatalf("one refresh step ran %d SQL statements", stepSQL)
	}
	t.Logf("refresh step: SQL=%d access checks=%d", stepSQL, checks.Load())

	// Fill everything, then measure the heartbeat path.
	for i := 0; i < 2000; i++ {
		if err := store.RefreshReadability(ctx, baseNow, canRead); err != nil {
			t.Fatal(err)
		}
		var stale int
		_ = e.db.QueryRow(`SELECT (SELECT COUNT(*) FROM principal_ask_readability WHERE checked_epoch < COALESCE((SELECT version FROM resource_access_epoch WHERE singleton=1),0)) + (SELECT COUNT(*) FROM ask_readability_queue) + (SELECT COUNT(*) FROM principal_readability_coverage WHERE complete=0)`).Scan(&stale)
		if stale == 0 {
			break
		}
	}
	checks.Store(0)
	started := time.Now()
	capture.Start()
	sum, err := store.AttentionSummary(ctx, baseNow)
	statements, queries, rows := capture.Stop()
	elapsed := time.Since(started)
	work := capture.Work()
	if err != nil || capture.WorkError() != nil {
		t.Fatalf("err=%v work=%v", err, capture.WorkError())
	}
	principals := sum["principals"].([]map[string]any)
	if len(principals) != humans {
		t.Fatalf("principals %d", len(principals))
	}
	for _, p := range principals {
		if p["counts_complete"] != true {
			t.Fatalf("incomplete after refresh: %v", p)
		}
		// Exact counts and earliest deadline from the fixture itself.
		pi := index(p["principal_id"].(string), "actor_h")
		wantDue, wantOverdue := 0, 0
		var wantEarliest time.Time
		for a := 0; a < asks; a++ {
			if !readable(pi, a) {
				continue
			}
			wantDue++
			by := baseNow.Add(time.Duration(a%7-3) * time.Hour)
			if !by.After(baseNow) {
				wantOverdue++
			}
			if wantEarliest.IsZero() || by.Before(wantEarliest) {
				wantEarliest = by
			}
		}
		if wantDue <= 1000 {
			t.Fatalf("fixture must exceed 1000 readable asks, has %d", wantDue)
		}
		if p["due"] != wantDue || p["overdue"] != wantOverdue || p["earliest_decide_by"] != rfc(wantEarliest) {
			t.Fatalf("%v: due=%v overdue=%v earliest=%v, want %d %d %s", p["principal_id"], p["due"], p["overdue"], p["earliest_decide_by"], wantDue, wantOverdue, rfc(wantEarliest))
		}
	}
	if checks.Load() != 0 {
		t.Fatalf("the heartbeat evaluated %d access checks", checks.Load())
	}
	// Fixed queries plus four indexed queries per principal.
	if queries > 20+4*humans || elapsed > 5*time.Second || work.VMSteps > 40_000_000 {
		t.Fatalf("heartbeat SQL=%d rows=%d VM=%d elapsed=%s", queries, rows, work.VMSteps, elapsed)
	}
	for _, statement := range statements {
		if !strings.Contains(statement.SQL, "principal_ask_readability") {
			continue
		}
		details, err := perfguard.Explain(ctx, e.db, statement)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(details, "\n")
		if strings.Contains(joined, "SCAN principal_ask_readability") || strings.Contains(joined, "SCAN d") || strings.Contains(joined, "SCAN ask_deadlines") {
			t.Fatalf("unindexed heartbeat read:\n%s\n%s", statement.SQL, joined)
		}
	}
	t.Logf("heartbeat with %d humans × %d asks: SQL=%d rows=%d VM=%d elapsed=%s", humans, asks, queries, rows, work.VMSteps, elapsed)
}
