package primitives_test

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestResourceAccessPMWriteIndex(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
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
	start := time.Now()
	for i := 0; i < 20; i++ {
		visible("public-control", 1)
	}
	t.Logf("indexed PM point mean=%s", time.Since(start)/20)
	// Invalidate the already-captured request with unrelated history. This was
	// the independent review's scaling repro; it must retain a bounded lookup.
	if _, err = ws.DB().Exec(`WITH RECURSIVE n(x) AS (VALUES(0) UNION ALL SELECT x+1 FROM n WHERE x<999) INSERT INTO pm_records SELECT 'decision','unrelated-'||x,'ws','owner','',1,'{"text":"unrelated public PM"}' FROM n`); err != nil {
		t.Fatal(err)
	}
	var samples []time.Duration
	for i := 0; i < 25; i++ {
		limited, cancel := context.WithTimeout(scope, 200*time.Millisecond)
		start := time.Now()
		var count int
		err = db.QueryRowContext(limited, `SELECT COUNT(*) FROM pm_records WHERE kind='decision' AND id='public-control'`).Scan(&count)
		elapsed := time.Since(start)
		cancel()
		if err != nil || count != 1 {
			t.Fatalf("scaled PM point count=%d err=%v", count, err)
		}
		samples = append(samples, elapsed)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	t.Logf("PM point after 1000 unrelated rows p95=%s", samples[23])
}
