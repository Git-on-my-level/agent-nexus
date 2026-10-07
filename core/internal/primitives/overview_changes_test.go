package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestOverviewDigestNetTransitionsVisibilityAndBounds(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	s := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "actor", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	work := []map[string]any{}
	for _, kind := range []string{"complete", "stall", "block", "hidden"} {
		w, err := s.CreateWork(ctx, "actor", board["id"].(string), map[string]any{"title": kind, "handle": kind})
		if err != nil {
			t.Fatal(err)
		}
		p := plans.Plan{Steps: []plans.Step{{ID: "step", Title: "Step", After: []string{}}}}
		if err = s.SetCardPlan(ctx, "actor", w["id"].(string), w["updated_at"].(string), p); err != nil {
			t.Fatal(err)
		}
		work = append(work, w)
	}
	if err = s.EnrichCardPlans(ctx, work, nil, now, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordOverviewVisit(ctx, "alice", work, now); err != nil {
		t.Fatal(err)
	}
	// Native writes and plan writes can produce the same completion digest.
	for i, status := range []string{"done", "", "blocked", "blocked"} {
		if status == "" {
			continue
		}
		p := plans.Plan{Steps: []plans.Step{{ID: "step", Title: "Step", Status: status, After: []string{}}}}
		raw, _ := json.Marshal(p)
		if _, err = db.Exec(`UPDATE card_plans SET body_json=?,updated_at=? WHERE card_id=?`, string(raw), now.Add(time.Minute).Format(time.RFC3339Nano), work[i]["id"]); err != nil {
			t.Fatal(err)
		}
	}
	// Closing the initiative must not lose completed steps.
	if _, err = db.Exec(`UPDATE cards SET column_key='done' WHERE id=?`, work[0]["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "actor", work[3]["thread_id"].(string), map[string]any{"pm_actor_id": "secret"}, nil); err != nil {
		t.Fatal(err)
	}
	visible := func(_ string, owner string) bool { return owner == "" }
	for _, e := range []struct {
		id, thread string
		at         time.Time
		refs       []string
	}{
		{"before", board["thread_id"].(string), now.Add(-time.Second), nil},
		{"public", board["thread_id"].(string), now.Add(time.Nanosecond), []string{board["ref"].(string)}},
		{"private", work[3]["thread_id"].(string), now.Add(time.Minute), nil},
		{"hidden-ref", board["thread_id"].(string), now.Add(time.Minute), []string{work[3]["ref"].(string)}},
		{"future", board["thread_id"].(string), now.Add(3 * time.Hour), nil},
	} {
		refs := e.refs
		if refs == nil {
			refs = []string{}
		}
		raw, _ := json.Marshal(refs)
		if _, err = db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json) VALUES(?,'human_attention_responded',?,'actor',?,?)`, e.id, e.at.Format(time.RFC3339Nano), e.thread, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	counter.Reset()
	d, err := s.LoadOverviewChanges(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), "alice", visible, now.Add(2*time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	queries := counter.Count()
	// Closed IDs are bounded before hydration; the answer probe shares the
	// visit read. This fixture uses eight reads; growing its corpus adds none.
	if queries != 8 {
		t.Fatalf("queries=%d want 8", queries)
	}
	got := map[string]int{}
	for _, item := range d.Items {
		got[item.Kind]++
		if item.Title == "hidden" || item.Ref == "event:private" || item.Ref == "event:hidden-ref" {
			t.Fatal(item)
		}
	}
	want := map[string]int{"step_completed": 1, "initiative_stalled": 1, "initiative_blocked": 1, "ask_answered": 1}
	if !reflect.DeepEqual(got, want) || d.Truncated {
		t.Fatalf("digest=%+v got=%v", d, got)
	}
	fixture, err := os.ReadFile("../../../contracts/fixtures/initiative-overview/digest.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected primitives.OverviewChanges
	if err = json.Unmarshal(fixture, &expected); err != nil {
		t.Fatal(err)
	}
	d.Since = expected.Since
	d.GeneratedAt = expected.GeneratedAt
	for i := range d.Items {
		if d.Items[i].TS != "" {
			d.Items[i].TS = "2026-10-04T12:01:00Z"
		}
	}
	sort.Slice(d.Items, func(i, j int) bool { return d.Items[i].Kind < d.Items[j].Kind })
	sort.Slice(expected.Items, func(i, j int) bool { return expected.Items[i].Kind < expected.Items[j].Kind })
	d.PriorPhases = nil
	if !reflect.DeepEqual(d, expected) {
		t.Fatalf("digest wire fixture differs: got=%+v want=%+v", d, expected)
	}
	// A fresh principal and a new workspace never inherit Alice's baseline.
	if d, err = s.LoadOverviewChanges(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), "bob", visible, now.Add(2*time.Hour), time.Hour); err != nil || d.Since != nil || len(d.Items) != 0 {
		t.Fatalf("%+v %v", d, err)
	}
	other, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherStore := primitives.NewTestStore(other.DB(), other.Layout().ArtifactContentDir)
	if d, err = otherStore.LoadOverviewChanges(ctx, "alice", nil, now, 0); err != nil || d.Since != nil {
		t.Fatalf("workspace leak: %+v %v", d, err)
	}
	// More candidates than the read/output caps are explicitly truncated.
	for i := 0; i < 205; i++ {
		if _, err = db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id) VALUES(?,'human_attention_responded',?,'actor',?)`, fmt.Sprintf("answer-%03d", i), now.Add(time.Minute).Format(time.RFC3339Nano), board["thread_id"]); err != nil {
			t.Fatal(err)
		}
	}
	counter.Reset()
	d, err = s.LoadOverviewChanges(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), "alice", visible, now.Add(2*time.Hour), time.Hour)
	if err != nil || len(d.Items) != primitives.MaxOverviewChanges || !d.Truncated || counter.Count() > queries {
		t.Fatalf("bounded digest items=%d truncated=%v queries=%d error=%v", len(d.Items), d.Truncated, counter.Count(), err)
	}
}

func TestRefPreviewRealWireFixtureAndBoardPrivacy(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	board := b["id"].(string)
	if _, err = ws.DB().Exec(`INSERT INTO actors(id,display_name,created_at) VALUES('actor-1','Actor 1',?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Initiative", "handle": "initiative", "owner": "actor:actor-1", "priority": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	p := plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build", After: []string{}}}}
	if err = s.SetCardPlan(ctx, "actor-1", w["id"].(string), w["updated_at"].(string), p); err != nil {
		t.Fatal(err)
	}
	items, err := s.ResolveRefs(ctx, []string{"card:initiative", "card:unknown"}, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"items": items})
	var got map[string]any
	json.Unmarshal(raw, &got)
	item := got["items"].([]any)[0].(map[string]any)
	if _, err = time.Parse(time.RFC3339Nano, item["last_moved_at"].(string)); err != nil {
		t.Fatal(err)
	}
	item["last_moved_at"] = "2026-10-04T12:00:00Z"
	health := item["plan_health"].(map[string]any)
	if _, err = time.Parse(time.RFC3339Nano, health["since"].(string)); err != nil {
		t.Fatal(err)
	}
	health["since"] = "2026-10-04T12:00:00Z"
	fixture, err := os.ReadFile("../../../contracts/fixtures/initiative-overview/refs.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	json.Unmarshal(fixture, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire differs got=%v want=%v", got, want)
	}
	b, err = s.GetBoard(ctx, board)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "actor-1", b["thread_id"].(string), map[string]any{"pm_actor_id": "private"}, nil); err != nil {
		t.Fatal(err)
	}
	items, err = s.ResolveRefs(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), []string{"card:initiative"}, func(_ string, owner string) bool { return owner == "" }, time.Now(), 0)
	raw, _ = json.Marshal(items)
	if err != nil || string(raw) != `[{"ref":"card:initiative","resolvable":false}]` {
		t.Fatalf("private board leak: %s %v", raw, err)
	}
}
