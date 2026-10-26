package server

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// A SCAN of a business table without an index is an O(workspace) read.
var trendsFullScan = regexp.MustCompile(`^SCAN (events|cards|brief_asks|brief_initiatives|threads|e|c|a|bi)$`)

func trendsRaised(t *testing.T, body map[string]any) float64 {
	t.Helper()
	for _, raw := range body["panels"].([]any) {
		panel := raw.(map[string]any)
		if panel["id"] == "asks_flow" {
			return panel["raised"].(float64)
		}
	}
	t.Fatalf("no asks_flow panel: %v", body)
	return 0
}

func TestWatchTrendsRouteScopesEachReader(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "trends-owner", "trends-owner-actor", "trends-owner", "trends-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "trends-stranger", "trends-stranger-actor", "trends-stranger", "trends-stranger-token")
	privateBoard, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(privateBoard["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	publicBoard, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	ask := func(board map[string]any, title string) {
		t.Helper()
		work, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		thread := anyString(work["thread_id"])
		if _, err = store.AppendEvent(ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": thread, "summary": title, "refs": []string{"thread:" + thread},
			"payload": map[string]any{"kind": "ask", "title": title, "requester_actor_id": "agent-1", "subject_ref": anyString(work["ref"]), "response_proposals": []any{"Proceed"}}}); err != nil {
			t.Fatal(err)
		}
	}
	ask(publicBoard, "Public question")
	ask(privateBoard, "Private question")

	status, body := hostHTTP(t, http.MethodGet, env.server.URL+"/watch/trends?group=decisions&range=7d", owner.AccessToken, nil)
	if status != http.StatusOK || body["schema"] != "watch_trends.v1" || body["range_days"] != 7.0 {
		t.Fatalf("owner %d %v", status, body)
	}
	if got := trendsRaised(t, body); got != 2 {
		t.Fatalf("owner raised = %v", got)
	}
	status, body = hostHTTP(t, http.MethodGet, env.server.URL+"/watch/trends?group=decisions&range=7d", stranger.AccessToken, nil)
	if status != http.StatusOK {
		t.Fatalf("stranger %d %v", status, body)
	}
	if got := trendsRaised(t, body); got != 1 {
		t.Fatalf("stranger raised = %v: private ask leaked", got)
	}
	if strings.Contains(fmt.Sprint(body), "Private question") {
		t.Fatal("private title leaked")
	}
	if status, _ = hostHTTP(t, http.MethodGet, env.server.URL+"/watch/trends?group=decisions&range=1y", owner.AccessToken, nil); status != http.StatusBadRequest {
		t.Fatalf("invalid range status = %d", status)
	}
	if status, _ = hostHTTP(t, http.MethodGet, env.server.URL+"/watch/trends?range=7d", owner.AccessToken, nil); status != http.StatusBadRequest {
		t.Fatalf("missing group status = %d", status)
	}
	if status, _ = hostHTTP(t, http.MethodGet, env.server.URL+"/watch/trends?group=decisions", "", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d", status)
	}
}

// The trends read is a fixed set of capped seeks: its statement, row and VM
// work stay within the ordinary read budget with every cap exceeded and 4,096
// unrelated cards plus a private board, for the owner and an unrelated reader.
func TestPerformanceWatchTrendsBudget(t *testing.T) {
	requirePerformanceTest(t)
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	s := h.primitiveStore.(*primitives.Store)
	db := h.workspace.DB()
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Watch", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('unrelated-thread','now','owner','{}')`); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = db.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<4096) INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) SELECT printf('unrelated-%05d',i),'Unrelated','',CASE WHEN i%2=0 THEN 'done' ELSE 'todo' END,'unrelated-thread',?,'owner',?,'owner' FROM n`, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	for b, n := range map[string]int{anyString(board["id"]): 30, anyString(private["id"]): 10} {
		for i := 0; i < n; i++ {
			w, err := s.CreateWork(ctx, "owner", b, map[string]any{"title": fmt.Sprintf("Loop %d", i), "cadence": map[string]any{"every_seconds": 3600}})
			if err != nil {
				t.Fatal(err)
			}
			thread := anyString(w["thread_id"])
			for j := 0; j < 6; j++ {
				asked, err := s.AppendEvent(ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": thread, "summary": "Q", "refs": []string{"thread:" + thread},
					"payload": map[string]any{"kind": "ask", "title": "Q", "requester_actor_id": fmt.Sprintf("agent-%d", j), "subject_ref": anyString(w["ref"]), "response_proposals": []any{"Proceed"}}})
				if err != nil {
					t.Fatal(err)
				}
				if j < 2 {
					id := anyString(asked["id"])
					if _, _, err = s.AppendHumanAttentionResponse(ctx, "owner", id, "inbox:"+id, "", "", map[string]any{
						"type": "human_attention_responded", "thread_id": thread, "refs": []string{"event:" + id},
						"payload": map[string]any{"outcome": "answered", "response_text": "Proceed", "request_event_ref": "event:" + id},
					}, map[string]any{"requested": true, "target_actor_id": "agent-1", "thread_id": thread}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err = s.AppendEvent(ctx, "agent-1", map[string]any{"type": "run_recorded", "summary": "Ran", "thread_id": thread, "refs": []string{anyString(w["ref"])},
					"payload": map[string]any{"run_id": fmt.Sprint(j), "outcome": "acted"}}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for drained := false; !drained; {
		if drained, err = s.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	// Every initiative, related or not, carries a promise inside the window,
	// so both promise seeks reach their caps.
	if _, err = db.Exec(`UPDATE brief_initiatives SET promise_at=strftime('%Y-%m-%dT%H:%M:%SZ','now','-'||(abs(random())%50)||' days')`); err != nil {
		t.Fatal(err)
	}
	pool, capture, err := perfguard.Open("file:" + h.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, h.workspace.Layout().ArtifactContentDir)
	for _, actor := range []string{"owner", "stranger"} {
		for _, group := range primitives.WatchTrendGroups {
			days := 90
			scoped := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
			capture.Start()
			start := time.Now()
			readCtx, closeRead, err := measured.BeginResourceRead(scoped)
			if err != nil {
				t.Fatal(err)
			}
			visible := func(_, owner string) bool { return owner == "" || owner == actor }
			out, err := measured.WatchTrends(readCtx, primitives.BriefReader{ActorID: actor, Kind: "human", Visible: visible}, group, days, time.Now())
			closeRead()
			elapsed := time.Since(start)
			statements, queries, returned := capture.Stop()
			// Every family seeks an index; none scans the large tables.
			for _, st := range statements {
				details, err := perfguard.Explain(ctx, db, st)
				if err != nil {
					t.Fatal(err)
				}
				for _, line := range details {
					if trendsFullScan.MatchString(strings.TrimSpace(line)) {
						t.Fatalf("unbounded trends read: %s\n%s", line, strings.Join(details, "\n"))
					}
				}
			}
			work := capture.Work()
			if err != nil || capture.WorkError() != nil {
				t.Fatal(err, capture.WorkError())
			}
			if len(out["panels"].([]map[string]any)) < 2 {
				t.Fatalf("panels = %v", out["panels"])
			}
			t.Logf("trends %s %s %dd: queries=%d rows=%d vm=%d latency=%s", actor, group, days, queries, returned, work.VMSteps, elapsed)
			if queries > 100 || returned > 1024 || work.VMSteps > 50000 {
				t.Errorf("trends exceeded the ordinary read budget: queries=%d rows=%d vm=%d", queries, returned, work.VMSteps)
			}
		}
	}
}
