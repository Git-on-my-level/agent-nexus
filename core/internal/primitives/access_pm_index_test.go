package primitives_test

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestResourceAccessPMWriteIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	db := resourceaccess.NewDB(ws.DB())
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = pm.NewStore(ws.DB()); err != nil {
		t.Fatal(err)
	}
	put := func(kind, id string, body any) {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = ws.DB().Exec(`INSERT INTO pm_records VALUES(?,?,'ws','owner','',1,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body`, kind, id, raw); e != nil {
			t.Fatal(e)
		}
	}
	scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	visible := func(id string, want int) {
		t.Helper()
		var count int
		if e := db.QueryRowContext(scope, `SELECT COUNT(*) FROM pm_records WHERE id=?`, id).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count != want {
			t.Fatalf("%s visibility=%d want=%d", id, count, want)
		}
	}
	// Children precede PM targets. Different kinds sharing an ID must retain
	// each contributor's privacy when another contributor is replaced/removed.
	put("action", "child", map[string]any{"decision_id": "shared"})
	put("turn", "grandchild", map[string]any{"action_id": "child"})
	visible("grandchild", 1)
	put("conversation", "shared", map[string]any{"work_ref": private["ref"]})
	put("decision", "shared", map[string]any{"text": "See **" + private["ref"].(string) + "**"})
	visible("shared", 0)
	visible("grandchild", 0)
	put("conversation", "shared", map[string]any{"text": "public"})
	visible("grandchild", 0)
	if _, err = ws.DB().Exec(`DELETE FROM pm_records WHERE kind='conversation' AND id='shared'`); err != nil {
		t.Fatal(err)
	}
	visible("grandchild", 0)
	put("decision", "shared", map[string]any{"text": "public"})
	visible("grandchild", 1)
	put("decision", "shared", map[string]any{"ref": private["ref"]})
	visible("grandchild", 0)
	var owners int
	if err = db.QueryRowContext(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "owner"}), `SELECT COUNT(*) FROM pm_records WHERE id='grandchild'`).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("owner=%d err=%v", owners, err)
	}
	put("decision", "123", map[string]any{"ref": private["ref"]})
	put("action", "numeric-child", map[string]any{"decision_id": 123})
	visible("numeric-child", 0)
	put("decision", "public-control", map[string]any{"text": "public"})
	visible("public-control", 1)
	// Captured snapshots must retain indexed PM point plans without body walks.
	policy, _ := resourceaccess.PolicyFrom(scope)
	query, args := policy.ReadOnDB(scope, ws.DB(), `SELECT body FROM pm_records WHERE kind='decision' AND id=?`, []any{"public-control"})
	rows, e := ws.DB().Query(`EXPLAIN QUERY PLAN `+query, args...)
	if e != nil {
		t.Fatal(e)
	}
	var plan strings.Builder
	for rows.Next() {
		var a, b, c int
		var d string
		if e = rows.Scan(&a, &b, &c, &d); e != nil {
			t.Fatal(e)
		}
		plan.WriteString(d + "\n")
	}
	rows.Close()
	if strings.Contains(plan.String(), "SCAN _row") || strings.Contains(strings.ToLower(query), "json_tree") || !strings.Contains(plan.String(), "SEARCH _row USING INDEX sqlite_autoindex_pm_records") {
		t.Fatalf("PM point authorization scans stored bodies:\n%s", plan.String())
	}
	// Invalidate the already-captured request with unrelated history. This was
	// the independent review's scaling repro; it must retain a bounded lookup,
	// measured as SQLite VM work rather than a wall-clock deadline.
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := resourceaccess.NewDB(pool)
	point := func(history int) perfguard.WorkStats {
		t.Helper()
		capture.Start()
		var count int
		err := measured.QueryRowContext(scope, `SELECT COUNT(*) FROM pm_records WHERE kind='decision' AND id='public-control'`).Scan(&count)
		_, queries, rows := capture.Stop()
		if err != nil || count != 1 || queries != 1 || rows != 1 || capture.WorkError() != nil {
			t.Fatalf("PM point after %d unrelated rows: count=%d queries=%d rows=%d err=%v work_err=%v", history, count, queries, rows, err, capture.WorkError())
		}
		return capture.Work()
	}
	base := point(0)
	for _, history := range []int{1000, 10000} {
		if _, err = ws.DB().Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO pm_records SELECT 'decision','unrelated-'||?||'-'||x,'ws','owner','',1,'{"text":"unrelated public PM"}' FROM n`, history, history); err != nil {
			t.Fatal(err)
		}
		// The first read after invalidation and a repeat must both stay flat.
		for i := 0; i < 2; i++ {
			work := point(history)
			if work.VMSteps > base.VMSteps+base.VMSteps/10 || work.FullScanSteps > base.FullScanSteps || work.AutoIndexRows > base.AutoIndexRows {
				t.Fatalf("PM point work grew with %d unrelated rows: base=%+v now=%+v", history, base, work)
			}
		}
	}
	t.Logf("PM point VM steps=%d with up to 11000 unrelated rows", base.VMSteps)
}
