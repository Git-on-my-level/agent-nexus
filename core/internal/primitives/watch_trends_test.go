package primitives

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
)

type trendsFixture struct {
	t     *testing.T
	ctx   context.Context
	store *Store
	board map[string]any
	now   time.Time
}

func newTrendsFixture(t *testing.T) *trendsFixture {
	t.Helper()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { workspace.Close() })
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Portfolio", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	return &trendsFixture{t: t, ctx: ctx, store: store, board: board, now: time.Now().UTC().Add(time.Minute)}
}

func (f *trendsFixture) card(title, pmOwner string, fields map[string]any) map[string]any {
	f.t.Helper()
	body := map[string]any{"title": title}
	for k, v := range fields {
		body[k] = v
	}
	created, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(f.board["id"]), body)
	if err != nil {
		f.t.Fatal(err)
	}
	if pmOwner != "" {
		if _, err = f.store.PatchThread(f.ctx, "owner", anyStringValue(created["thread_id"]), map[string]any{"pm_actor_id": pmOwner}, nil); err != nil {
			f.t.Fatal(err)
		}
	}
	return created
}

// ask raises an ask on work's thread at raisedAgo before now, answering it
// answeredAfter later when that is positive.
func (f *trendsFixture) ask(work map[string]any, requester string, raisedAgo, answeredAfter time.Duration) {
	f.t.Helper()
	threadID := anyStringValue(work["thread_id"])
	event, err := f.store.AppendEvent(f.ctx, requester, map[string]any{
		"type": "human_attention_requested", "thread_id": threadID, "summary": "Which way?",
		"refs": []string{"thread:" + threadID},
		"payload": map[string]any{
			"kind": "ask", "title": "Which way?", "requester_actor_id": requester,
			"subject_ref": anyStringValue(work["ref"]), "response_proposals": []any{"Proceed"},
		},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	id := anyStringValue(event["id"])
	if answeredAfter > 0 {
		if _, _, err := f.store.AppendHumanAttentionResponse(f.ctx, "human", id, "inbox:"+id, "", "", map[string]any{
			"type": "human_attention_responded", "thread_id": threadID, "refs": []string{"event:" + id},
			"payload": map[string]any{"outcome": "answered", "response_text": "Proceed", "request_event_ref": "event:" + id},
		}, map[string]any{"requested": true, "target_actor_id": requester, "thread_id": threadID}); err != nil {
			f.t.Fatal(err)
		}
	}
	raised := f.now.Add(-raisedAgo)
	f.exec(`UPDATE events SET ts=? WHERE id=?`, loopTS(raised), id)
	f.exec(`UPDATE brief_asks SET created_at=? WHERE ask_id=?`, loopTS(raised), id)
	if answeredAfter > 0 {
		at := loopTS(raised.Add(answeredAfter))
		f.exec(`UPDATE brief_asks SET answered_at=? WHERE ask_id=?`, at, id)
		f.exec(`UPDATE events SET ts=? WHERE id=(SELECT resolution_event_id FROM human_attention_request_resolutions WHERE request_event_id=?)`, at, id)
		f.exec(`UPDATE human_attention_request_resolutions SET created_at=? WHERE request_event_id=?`, at, id)
	}
}

func (f *trendsFixture) run(work map[string]any, ago time.Duration, outcome string) {
	f.t.Helper()
	event, err := f.store.AppendEvent(f.ctx, "agent-1", map[string]any{
		"type": "run_recorded", "summary": "Ran", "thread_id": work["thread_id"], "refs": []string{anyStringValue(work["ref"])},
		"payload": map[string]any{"run_id": time.Now().Format(time.RFC3339Nano), "outcome": outcome},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec(`UPDATE events SET ts=? WHERE id=?`, loopTS(f.now.Add(-ago)), anyStringValue(event["id"]))
}

func (f *trendsFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.store.db.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *trendsFixture) trends(actor string, days int) map[string]map[string]any {
	f.t.Helper()
	panels := map[string]map[string]any{}
	for _, group := range WatchTrendGroups {
		for id, p := range f.group(actor, group, days) {
			panels[id] = p
		}
	}
	return panels
}

func (f *trendsFixture) group(actor, group string, days int) map[string]map[string]any {
	f.t.Helper()
	for drained := false; !drained; {
		var err error
		if drained, err = f.store.RefreshBriefInitiatives(f.ctx, 200); err != nil {
			f.t.Fatal(err)
		}
	}
	scoped := WithAccessScope(f.ctx, AccessScope{ActorID: actor})
	out, err := f.store.WatchTrends(scoped, BriefReader{ActorID: actor, Kind: "human"}, group, days, f.now)
	if err != nil {
		f.t.Fatal(err)
	}
	if out["schema"] != "watch_trends.v1" || out["range_days"] != days || out["group"] != group {
		f.t.Fatalf("envelope = %#v", out)
	}
	panels := map[string]map[string]any{}
	for _, p := range out["panels"].([]map[string]any) {
		panels[p["id"].(string)] = p
	}
	return panels
}

func trendValue(t *testing.T, p map[string]any, field string) float64 {
	t.Helper()
	v, ok := p[field].(*float64)
	if !ok || v == nil {
		t.Fatalf("%s.%s = %#v", p["id"], field, p[field])
	}
	return *v
}

func TestWatchTrendsComputesFlowsAndDeltas(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Battery", "", nil)
	day := 24 * time.Hour
	// Current period: three asks, two answered (1h and 3h); previous period:
	// one ask answered after 10h.
	f.ask(work, "agent-1", 2*day, time.Hour)
	f.ask(work, "agent-1", 3*day, 3*time.Hour)
	f.ask(work, "agent-2", 4*day, 0)
	f.ask(work, "agent-1", 40*day, 10*time.Hour)
	f.run(work, day, "acted")
	f.run(work, 2*day, "infra_error")
	f.run(work, 35*day, "measured")

	panels := f.trends("owner", 30)
	flow := panels["asks_flow"]
	if got := trendValue(t, flow, "value"); got != 2 {
		t.Fatalf("answered = %v", got)
	}
	if got := trendValue(t, flow, "previous"); got != 1 {
		t.Fatalf("answered previous = %v", got)
	}
	if got := trendValue(t, flow, "raised"); got != 3 {
		t.Fatalf("raised = %v", got)
	}
	series := flow["series"].([]map[string]any)
	if points := series[0]["points"].([]*float64); len(points) != 30 || flow["truncated"] != false {
		t.Fatalf("buckets = %d truncated=%v", len(points), flow["truncated"])
	}
	wait := panels["answer_time"]
	if got := trendValue(t, wait, "value"); got != 2*3600 {
		t.Fatalf("median wait = %v", got)
	}
	if got := trendValue(t, wait, "previous"); got != 10*3600 {
		t.Fatalf("previous median wait = %v", got)
	}
	backlog := panels["ask_backlog"]
	if got := trendValue(t, backlog, "value"); got != 1 {
		t.Fatalf("open now = %v", got)
	}
	runs := panels["loop_runs"]
	if trendValue(t, runs, "value") != 2 || trendValue(t, runs, "previous") != 1 || trendValue(t, runs, "failed") != 1 {
		t.Fatalf("runs = %#v", runs)
	}
	work7 := f.trends("owner", 7)["asks_flow"]
	if points := work7["series"].([]map[string]any)[0]["points"].([]*float64); len(points) != 7 {
		t.Fatalf("7d buckets = %d", len(points))
	}
	week := f.trends("owner", 90)["asks_flow"]
	if points := week["series"].([]map[string]any)[0]["points"].([]*float64); len(points) != 13 {
		t.Fatalf("90d buckets = %d", len(points))
	}
	sources := panels["ask_sources"]["rows"].([]map[string]any)
	if len(sources) != 2 || sources[0]["principal"].(map[string]any)["id"] != "agent-1" || sources[0]["value"] != 2.0 || sources[0]["previous"] != 1.0 {
		t.Fatalf("sources = %#v", sources)
	}
	active := panels["agent_runs"]["rows"].([]map[string]any)
	if len(active) != 1 || active[0]["principal"].(map[string]any)["id"] != "agent-1" || active[0]["value"] != 2.0 || active[0]["previous"] != 1.0 {
		t.Fatalf("agent runs = %#v", active)
	}
	if _, err := f.store.WatchTrends(WithAccessScope(f.ctx, AccessScope{ActorID: "owner"}), BriefReader{ActorID: "owner"}, "everything", 30, f.now); err == nil {
		t.Fatal("unknown group accepted")
	}
}

// A PM-private thread's asks and runs never reach another reader's counts,
// and its owner still sees them.
func TestWatchTrendsExcludesPrivateRecordsFromOtherReaders(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	public := f.card("Public", "", nil)
	private := f.card("Private", "owner", nil)
	day := 24 * time.Hour
	f.ask(public, "agent-1", day, time.Hour)
	f.ask(private, "agent-secret", day, 0)
	f.ask(private, "agent-secret", 2*day, 0)
	f.run(private, day, "acted")

	owner := f.trends("owner", 30)
	stranger := f.trends("stranger", 30)
	if got := trendValue(t, owner["asks_flow"], "raised"); got != 3 {
		t.Fatalf("owner raised = %v", got)
	}
	if got := trendValue(t, stranger["asks_flow"], "raised"); got != 1 {
		t.Fatalf("stranger raised = %v, private asks leaked", got)
	}
	if got := trendValue(t, owner["ask_backlog"], "value"); got != 2 {
		t.Fatalf("owner backlog = %v", got)
	}
	if got := trendValue(t, stranger["ask_backlog"], "value"); got != 0 {
		t.Fatalf("stranger backlog = %v, private asks leaked", got)
	}
	if got := trendValue(t, stranger["loop_runs"], "value"); got != 0 {
		t.Fatalf("stranger runs = %v, private runs leaked", got)
	}
	if got := trendValue(t, owner["loop_runs"], "value"); got != 1 {
		t.Fatalf("owner runs = %v", got)
	}
	for _, row := range stranger["ask_sources"]["rows"].([]map[string]any) {
		if row["principal"].(map[string]any)["id"] == "agent-secret" {
			t.Fatalf("private requester leaked: %#v", row)
		}
	}
	// Work started counts the private card only for its owner.
	if trendValue(t, owner["work_flow"], "started")-trendValue(t, stranger["work_flow"], "started") != 1 {
		t.Fatalf("started owner=%v stranger=%v", owner["work_flow"]["started"], stranger["work_flow"]["started"])
	}
}

// A capped family reports truncation and leaves buckets past its horizon null
// instead of claiming zero.
func TestWatchTrendsMarksCappedHistoryUnknown(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Busy", "", nil)
	for i := 0; i <= trendsRunCap; i++ {
		f.run(work, time.Duration(i)*time.Minute, "acted")
	}
	f.run(work, 20*24*time.Hour, "acted")
	runs := f.trends("owner", 30)["loop_runs"]
	if runs["truncated"] != true || runs["value"].(*float64) != nil {
		t.Fatalf("runs = %#v", runs)
	}
	points := runs["series"].([]map[string]any)[0]["points"].([]*float64)
	if points[0] != nil {
		t.Fatalf("bucket before the horizon = %v, want unknown", *points[0])
	}
}

// Promises are judged against each initiative's current promise: done on or
// before it is kept, done after it or still open past it is missed.
func TestWatchTrendsPromisesKeptAndPast(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	day := 24 * time.Hour
	kept := f.card("Kept", "", nil)
	late := f.card("Late", "", nil)
	overdue := f.card("Overdue", "", nil)
	future := f.card("Future", "", nil)
	for _, w := range []map[string]any{kept, late} {
		evidence, err := f.store.AppendEvent(f.ctx, "owner", map[string]any{"type": "message_posted", "thread_id": w["thread_id"], "summary": "Shipped", "refs": []string{"thread:" + anyStringValue(w["thread_id"])}, "payload": map[string]any{"body": "Shipped"}})
		if err != nil {
			t.Fatal(err)
		}
		resolution, refs := "done", []string{"event:" + anyStringValue(evidence["id"])}
		if _, err := f.store.MoveBoardCard(f.ctx, "owner", anyStringValue(f.board["id"]), anyStringValue(w["id"]), MoveBoardCardInput{ColumnKey: "done", Resolution: &resolution, ResolutionRefs: &refs}); err != nil {
			t.Fatal(err)
		}
		// The HTTP board route records the lifecycle event beside the move.
		if _, err := f.store.AppendEvent(f.ctx, "owner", map[string]any{"type": "card_moved", "thread_id": w["thread_id"], "summary": "Card moved", "refs": []string{anyStringValue(w["ref"]), "board:" + anyStringValue(f.board["id"])},
			"payload": map[string]any{"card_id": w["id"], "board_id": f.board["id"], "column_key": "done", "from_column_key": "todo"}}); err != nil {
			t.Fatal(err)
		}
	}
	f.trends("owner", 30) // fold the moves before rewriting promises
	doneAt := f.now.Add(-5 * day)
	f.exec(`UPDATE events SET ts=? WHERE type='card_moved'`, loopTS(doneAt))
	f.exec(`UPDATE cards SET created_at=?`, loopTS(f.now.Add(-50*day)))
	promise := func(w map[string]any, at time.Time) {
		f.exec(`UPDATE brief_initiatives SET promise_at=? WHERE card_id=?`, loopTS(at), anyStringValue(w["id"]))
	}
	promise(kept, doneAt.Add(day))
	promise(late, doneAt.Add(-day))
	promise(overdue, f.now.Add(-3*day))
	promise(future, f.now.Add(10*day))

	panels := f.trends("owner", 30)
	p := panels["promises"]
	if got := trendValue(t, p, "value"); got != 100.0/3 {
		t.Fatalf("kept rate = %v (%#v)", got, p)
	}
	past := panels["past_promise"]
	// Open and promised now: overdue (past) and future.
	if got := trendValue(t, past, "value"); got != 50 || past["past"] != 1.0 || past["total"] != 2.0 {
		t.Fatalf("past = %#v", past)
	}
}

// An answer counts in the period it was given, even to an ask raised before
// the read began, and that ask was open in the buckets before its answer.
func TestWatchTrendsCountsAnswersToOldAsks(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Battery", "", nil)
	day := 24 * time.Hour
	f.ask(work, "agent-1", 20*day, 18*day) // raised before the 14-day read, answered 2 days ago
	panels := f.trends("owner", 7)
	if got := trendValue(t, panels["asks_flow"], "value"); got != 1 {
		t.Fatalf("answered this week = %v", got)
	}
	if got := trendValue(t, panels["asks_flow"], "raised"); got != 0 {
		t.Fatalf("raised this week = %v", got)
	}
	if got := trendValue(t, panels["answer_time"], "value"); got != (18 * day).Seconds() {
		t.Fatalf("answer time = %v", got)
	}
	backlog := panels["ask_backlog"]
	if trendValue(t, backlog, "value") != 0 || trendValue(t, backlog, "previous") != 1 {
		t.Fatalf("backlog = %v → %v", *backlog["previous"].(*float64), *backlog["value"].(*float64))
	}
}

// Initiatives the reader cannot see still use places in the promise window,
// so a window full of them reads as truncated, never as "no promises".
func TestWatchTrendsHiddenPromisesMarkTruncation(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	private, err := f.store.CreateBoard(f.ctx, "owner", map[string]any{"title": "Private", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.PatchThread(f.ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	hidden := []string{}
	for i := 0; i <= trendsOpenPromCap; i++ {
		w, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(private["id"]), map[string]any{"title": "Hidden"})
		if err != nil {
			t.Fatal(err)
		}
		hidden = append(hidden, anyStringValue(w["id"]))
	}
	public := f.card("Public", "", nil)
	f.group("owner", "initiatives", 30) // fold every card once
	for i, id := range hidden {
		f.exec(`UPDATE brief_initiatives SET promise_at=? WHERE card_id=?`, loopTS(f.now.Add(-time.Duration(40+i)*time.Hour)), id)
	}
	f.exec(`UPDATE brief_initiatives SET promise_at=? WHERE card_id=?`, loopTS(f.now.Add(-time.Hour)), anyStringValue(public["id"]))
	past := f.group("stranger", "initiatives", 30)["past_promise"]
	if past["truncated"] != true || past["value"].(*float64) != nil {
		t.Fatalf("stranger past promise = %#v", past)
	}
}
