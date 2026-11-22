package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceAddressedHandoffPointRead(t *testing.T) {
	requirePerformanceTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	sender := seedMachinePrincipalForLockoutTest(t, ctx, db, "handoff-sender", "actor-sender", "sender", "sender-test-token")
	stranger := seedMachinePrincipalForLockoutTest(t, ctx, db, "handoff-stranger", "actor-stranger", "stranger", "stranger-test-token")
	id := seedHandoffFixture(t, env.primitiveStore.(*primitives.Store), sender.ActorID, "actor-recipient",
		[]string{"https://github.com/example/repo/issues/10"})
	pool, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	handler := NewHandler("handoff-scale", WithAuthStore(auth.NewStore(pool)),
		WithPrimitiveStore(primitives.NewTestStore(pool, env.workspace.Layout().ArtifactContentDir)))

	for _, size := range []int{0, 4096} {
		if size > 0 {
			_, err := db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<?)
				INSERT INTO agent_wakeups(wakeup_id,status,target_actor_id,target_handle,created_at,updated_at)
				SELECT 'other-wake-'||i,'requested','actor-unrelated','unrelated',?,? FROM n`, size, perfguard.Timestamp, perfguard.Timestamp)
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, principal := range []lockoutPrincipalSeed{sender, stranger} {
			req := httptest.NewRequest(http.MethodGet, agentHandoffPath+"/"+id, nil)
			req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
			w := httptest.NewRecorder()
			capture.Start()
			handler.ServeHTTP(w, req)
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			if err := capture.WorkError(); err != nil {
				t.Fatal(err)
			}
			want := http.StatusNotFound
			if principal.ActorID == sender.ActorID {
				want = http.StatusOK
			}
			if w.Code != want || (strings.Contains(w.Body.String(), "pickup_status") != (want == http.StatusOK)) {
				t.Fatalf("history=%d actor=%s status=%d want=%d body=%s", size, principal.ActorID, w.Code, want, w.Body)
			}
			indexed := false
			for _, statement := range statements {
				if !strings.Contains(statement.SQL, "handoff_acknowledged_at") {
					continue
				}
				plan, err := perfguard.Explain(ctx, db, statement)
				if err != nil {
					t.Fatal(err)
				}
				indexed = strings.Contains(strings.Join(plan, "\n"), "sqlite_autoindex_agent_wakeups_1 (wakeup_id=?)")
			}
			t.Logf("history=%d status=%d queries=%d rows=%d vm=%d full_scan=%d", size, w.Code, queries, rows, work.VMSteps, work.FullScanSteps)
			if !indexed || queries > 20 || rows > 32 || work.VMSteps > 50000 || work.FullScanSteps > 32 {
				t.Fatal("handoff read exceeded its indexed point-read budget")
			}
		}
	}
}
