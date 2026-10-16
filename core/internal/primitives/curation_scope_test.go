package primitives

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCurationMetricsScopedPromiseIndex(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	now := time.Now().UTC()
	for _, private := range []bool{false, true} {
		card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Promise", "due_at": loopTS(now.Add(-time.Hour))})
		if err != nil {
			t.Fatal(err)
		}
		if private {
			if _, err = s.PatchThread(ctx, "owner", workString(card["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	for done := false; !done; {
		done, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, actor := range []string{"owner", "stranger"} {
		scope := AccessScope{ActorID: actor}
		for _, request := range []context.Context{WithAccessScope(ctx, scope), WithRequestAccessScope(ctx, scope)} {
			metrics, err := s.CurationMetrics(request, 7*24*time.Hour, now)
			if err != nil {
				t.Fatal(actor, err)
			}
			want := 1
			if actor == "owner" {
				want = 2
			}
			if metrics.S20.Promises != want {
				t.Fatalf("%s promises=%d want=%d", actor, metrics.S20.Promises, want)
			}
		}
		rows, err := ws.DB().QueryContext(ctx, "EXPLAIN QUERY PLAN "+scopeRead(WithAccessScope(ctx, scope), curationOpenPromisesSQL), loopTS(now.Add(-7*24*time.Hour)), loopTS(now), 100)
		if err != nil {
			t.Fatal(err)
		}
		details := []string{}
		for rows.Next() {
			var a, b, c int
			var detail string
			if err = rows.Scan(&a, &b, &c, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(details, "\n")
		if !strings.Contains(plan, "USING INDEX idx_brief_initiatives_page") || strings.Contains(plan, "SCAN _row") {
			t.Fatal(plan)
		}
	}
}
