package primitives_test

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFrozenHomeReadMembershipPlanAndCost(t *testing.T) {
	if testing.Short() {
		t.Skip("synthetic home-read scale fixture")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	topic, err := s.CreateTopic(ctx, "owner", map[string]any{"title": "Frozen read", "summary": "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	thread := fmt.Sprint(topic.Topic["thread_id"])
	ref := fmt.Sprint(topic.Topic["ref"])
	if _, err = s.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	edgeTopic, err := s.CreateTopic(ctx, "owner", map[string]any{"title": "Separate source", "summary": "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	edgeThread := fmt.Sprint(edgeTopic.Topic["thread_id"])
	if _, err = s.PatchThread(ctx, "owner", edgeThread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := s.AppendEvent(ctx, "owner", map[string]any{"id": "frozen-read-event", "thread_id": edgeThread, "type": "message_posted", "refs": []string{ref}, "payload": map[string]any{"text": "Seen"}})
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]primitives.EventCursor{ref: {ID: fmt.Sprint(event["id"]), TS: fmt.Sprint(event["ts"])}}
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	scope := func() context.Context {
		return primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
	}
	for _, corpus := range []int{0, 4096} {
		if corpus != 0 {
			if err = perfguard.Seed(ctx, ws.DB(), ws.Layout().ArtifactContentDir, "owner", time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}
		request := scope()
		if _, err = measured.GetEvent(request, fmt.Sprint(event["id"])); err != nil {
			t.Fatal(err)
		}
		capture.Start()
		err = measured.CheckHomeReadCursors(request, []string{ref}, expected)
		statements, calls, rows := capture.Stop()
		if err != nil {
			t.Fatal(err)
		}
		if e := capture.WorkError(); e != nil {
			t.Fatal(e)
		}
		work := capture.Work()
		t.Logf("corpus=%d calls=%d rows=%d VM=%d fullscan=%d", corpus, calls, rows, work.VMSteps, work.FullScanSteps)
		if calls > 20 || rows > 32 || work.VMSteps > 50000 || work.FullScanSteps > 128 {
			t.Fatalf("unbounded frozen read proof: %+v", work)
		}
		checked := 0
		for _, statement := range statements {
			if !strings.Contains(statement.SQL, "requested(kind,ref,event_id)") {
				continue
			}
			checked++
			plan, e := perfguard.Explain(ctx, ws.DB(), statement)
			if e != nil {
				t.Fatal(e)
			}
			joined := strings.Join(plan, "\n")
			t.Log(joined)
			for _, line := range plan {
				if strings.Contains(line, "SCAN events") || strings.Contains(line, "SCAN ref_edges") || strings.Contains(line, "SCAN topics") || strings.Contains(line, "SCAN boards") {
					t.Fatal("unbounded canonical scan", line)
				}
			}
		}
		if checked != 1 {
			t.Fatal("expected one bounded group proof", checked)
		}
	}
}

func TestFrozenHomeReadRejectsUnrelatedAndHiddenMembership(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	makeTopic := func(actor, title string) map[string]any {
		topic, e := s.CreateTopic(ctx, actor, map[string]any{"title": title, "summary": "Synthetic"})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.PatchThread(ctx, actor, fmt.Sprint(topic.Topic["thread_id"]), map[string]any{"pm_actor_id": actor}, nil); e != nil {
			t.Fatal(e)
		}
		return topic.Topic
	}
	seen := makeTopic("reader", "Seen")
	unrelated := makeTopic("reader", "Unrelated")
	hidden := makeTopic("other", "Hidden")
	event, err := s.AppendEvent(ctx, "reader", map[string]any{"id": "visible-frozen", "thread_id": seen["thread_id"], "type": "message_posted", "refs": []string{}, "payload": map[string]any{"text": "Seen update"}})
	if err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	check := func(ref string, id, ts any) error {
		return s.CheckHomeReadCursors(scope, []string{ref}, map[string]primitives.EventCursor{ref: {ID: fmt.Sprint(id), TS: fmt.Sprint(ts)}})
	}
	if err = check(fmt.Sprint(seen["ref"]), event["id"], event["ts"]); err != nil {
		t.Fatal("valid cursor", err)
	}
	for _, topic := range []map[string]any{unrelated, hidden} {
		if err = check(fmt.Sprint(topic["ref"]), event["id"], event["ts"]); err == nil {
			t.Fatal("unrelated or hidden group admitted", topic["ref"])
		}
	}
	hiddenEvent, err := s.AppendEvent(ctx, "other", map[string]any{"id": "hidden-frozen", "thread_id": hidden["thread_id"], "type": "message_posted", "refs": []string{}, "payload": map[string]any{"text": "Hidden update"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = check(fmt.Sprint(hidden["ref"]), hiddenEvent["id"], hiddenEvent["ts"]); err == nil {
		t.Fatal("hidden cursor admitted")
	}
	// Same context, ownership changed after the initial read: epoch admission
	// must refresh before the point proof rather than replay old visibility.
	if _, err = s.PatchThread(ctx, "reader", fmt.Sprint(seen["thread_id"]), map[string]any{"pm_actor_id": "other"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = check(fmt.Sprint(seen["ref"]), event["id"], event["ts"]); err == nil {
		t.Fatal("revoked cursor admitted")
	}
}
