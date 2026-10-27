package primitives

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
)

type trendsFixture struct {
	t      *testing.T
	ctx    context.Context
	store  *Store
	board  map[string]any
	now    time.Time
	dbPath string
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
	return &trendsFixture{t: t, ctx: ctx, store: store, board: board, now: time.Now().UTC(), dbPath: workspace.Layout().DatabasePath}
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

// trends reads every group's panels through the unscoped calculation.
func (f *trendsFixture) trends(days int) map[string]map[string]any {
	f.t.Helper()
	panels := map[string]map[string]any{}
	for _, group := range WatchTrendGroups {
		for id, p := range f.group(group, days) {
			panels[id] = p
		}
	}
	return panels
}

func (f *trendsFixture) group(group string, days int) map[string]map[string]any {
	f.t.Helper()
	f.freezeLiveWrites()
	for drained := false; !drained; {
		var err error
		if drained, err = f.store.RefreshBriefInitiatives(f.ctx, 200); err != nil {
			f.t.Fatal(err)
		}
	}
	out, err := f.store.buildWatchTrends(f.ctx, group, days, f.now)
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

	panels := f.trends(30)
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
	work7 := f.trends(7)["asks_flow"]
	if points := work7["series"].([]map[string]any)[0]["points"].([]*float64); len(points) != 7 {
		t.Fatalf("7d buckets = %d", len(points))
	}
	week := f.trends(90)["asks_flow"]
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
	if _, err := f.store.buildWatchTrends(f.ctx, "everything", 30, f.now); err == nil {
		t.Fatal("unknown group accepted")
	}
}

// A capped family reports truncation and leaves buckets past its horizon null
// instead of claiming zero.
func TestWatchTrendsMarksCappedHistoryUnknown(t *testing.T) {
	if testing.Short() {
		t.Skip("scale-corpus budget guard; the non-short CI tier keeps the full fixture")
	}
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Busy", "", nil)
	for i := 0; i <= trendsRunCap; i++ {
		f.run(work, time.Duration(i)*time.Minute, "acted")
	}
	f.run(work, 20*24*time.Hour, "acted")
	runs := f.trends(30)["loop_runs"]
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
	f.trends(30) // fold the moves before rewriting promises
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

	panels := f.trends(30)
	p := panels["promises"]
	if got := trendValue(t, p, "value"); got != 100.0/3 {
		t.Fatalf("kept rate = %v (%#v)", got, p)
	}
	for _, series := range p["series"].([]map[string]any) {
		if series["unit"] != "count" {
			t.Fatalf("promise series %s unit = %v, want count", series["key"], series["unit"])
		}
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
	panels := f.trends(7)
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

// rawEvents inserts n events of one type on work's thread, newest at ago,
// one minute apart. Database triggers derive brief_asks rows exactly as for
// an appended event.
func (f *trendsFixture) rawEvents(work map[string]any, prefix, kind string, n int, ago time.Duration, payload func(i int) string) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		f.exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,?,?,?,?,?)`,
			fmt.Sprintf("%s-%04d", prefix, i), kind, loopTS(f.now.Add(-ago-time.Duration(i)*time.Minute)), "agent-hidden",
			anyStringValue(work["thread_id"]), `["thread:`+anyStringValue(work["thread_id"])+`"]`, payload(i))
	}
}

// When the answer cap cuts into the current period, that period has no
// median: the answers left out could move it either way.
func TestWatchTrendsAnswerTimeUnknownWhenCurrentPeriodIsCapped(t *testing.T) {
	if testing.Short() {
		t.Skip("scale-corpus budget guard; the non-short CI tier keeps the full fixture")
	}
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Busy", "", nil)
	thread := anyStringValue(work["thread_id"])
	f.rawEvents(work, "ask", "human_attention_requested", trendsAnswerCap+5, 3*24*time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"kind":"ask","title":"Q","requester_actor_id":"agent-1","subject_ref":%q,"response_proposals":["Proceed"]}}`, anyStringValue(work["ref"]))
	})
	for i := 0; i < trendsAnswerCap+5; i++ {
		at := loopTS(f.now.Add(-time.Duration(i) * time.Hour))
		f.exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,?,?,?,?,?)`,
			fmt.Sprintf("answer-%04d", i), "human_attention_responded", at, "human", thread, `["thread:`+thread+`"]`, `{"payload":{"outcome":"answered"}}`)
		f.exec(`INSERT INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES(?,?,'answered','human',?)`,
			fmt.Sprintf("ask-%04d", i), fmt.Sprintf("answer-%04d", i), at)
	}
	wait := f.group("decisions", 7)["answer_time"]
	if wait["truncated"] != true || wait["value"].(*float64) != nil {
		t.Fatalf("answer time from a capped current period: %#v", wait)
	}
}

// With more loops than the read admits, every reporting share is unknown.
func TestWatchTrendsLoopHealthUnknownWhenLoopsAreCapped(t *testing.T) {
	if testing.Short() {
		t.Skip("scale-corpus budget guard; the non-short CI tier keeps the full fixture")
	}
	t.Parallel()
	f := newTrendsFixture(t)
	for i := 0; i <= trendsLoopCap; i++ {
		w, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(f.board["id"]), map[string]any{"title": fmt.Sprintf("Loop %d", i), "cadence": map[string]any{"every_seconds": 3600}})
		if err != nil {
			t.Fatal(err)
		}
		f.run(w, time.Minute, "acted")
	}
	health := f.group("loops", 7)["loop_health"]
	if health == nil || health["truncated"] != true || health["value"].(*float64) != nil || health["previous"].(*float64) != nil {
		t.Fatalf("loop health over a partial population: %#v", health)
	}
	for _, p := range health["series"].([]map[string]any)[0]["points"].([]*float64) {
		if p != nil {
			t.Fatal("a share over a partial population was charted")
		}
	}
}

// A ranking is only offered for a period the read covered in full.
func TestWatchTrendsRankingsNeedACompleteCurrentPeriod(t *testing.T) {
	if testing.Short() {
		t.Skip("scale-corpus budget guard; the non-short CI tier keeps the full fixture")
	}
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Busy", "", nil)
	ref := anyStringValue(work["ref"])
	f.rawEvents(work, "ask", "human_attention_requested", trendsAskCap+5, time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"kind":"ask","title":"Q","requester_actor_id":"agent-1","subject_ref":%q,"response_proposals":["Proceed"]}}`, ref)
	})
	f.rawEvents(work, "run", "run_recorded", trendsRunCap+5, time.Hour, func(i int) string {
		return fmt.Sprintf(`{"subject_ref":%q,"payload":{"run_id":"%d","outcome":"acted"}}`, ref, i)
	})
	panels := f.trends(7)
	for _, id := range []string{"ask_sources", "agent_runs"} {
		p := panels[id]
		if p["truncated"] != true || len(p["rows"].([]map[string]any)) != 0 {
			t.Fatalf("%s ranked a capped current period: %#v", id, p)
		}
	}
}

// watchTrendsProofHook returns a context whose hook runs inside the pinned read
// snapshot immediately after the full-visibility proof succeeds and before any
// aggregate read, so a test can inject a concurrent transfer between proof and
// calculation.
func watchTrendsProofHook(ctx context.Context, hook func(context.Context)) context.Context {
	return context.WithValue(ctx, watchTrendsProofHookKey{}, hook)
}

// The eligibility proof and the aggregate reads must share one read snapshot:
// a transfer committed between them cannot leak into a response the proof
// admitted. The proof hook injects exactly that transfer on a second
// connection; the admitted response must describe the snapshot the proof
// examined, and the next request must observe the transfer. This fails if the
// proof ever moves outside the calculation's snapshot (the injected ask would
// land inside it) or stops running entirely (the follow-up denial would not).
func TestWatchTrendsProofSharesTheCalculationSnapshot(t *testing.T) {
	f := newTrendsFixture(t)
	work := f.card("Public", "", nil)
	f.ask(work, "agent-1", time.Hour, 0)
	thread := anyStringValue(work["thread_id"])
	writer, err := sql.Open("sqlite", "file:"+f.dbPath+"?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	hooked := false
	ctx := watchTrendsProofHook(WithAccessScope(f.ctx, AccessScope{ActorID: "viewer"}), func(context.Context) {
		hooked = true
		tx, err := writer.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		// After the proof: the board thread becomes private to another owner
		// (a fresh denial root) and a new ask arrives with the transfer.
		if _, err := tx.Exec(`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','owner'),updated_at=?,updated_by='owner' WHERE id=?`, loopTS(f.now), thread); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('post-proof-ask','human_attention_requested',?,'agent-1',?,?,'{"payload":{"kind":"ask","title":"After","requester_actor_id":"agent-1","subject_ref":"card:after","response_proposals":["Proceed"]}}')`,
			loopTS(f.now.Add(-time.Minute)), thread, `["thread:`+thread+`"]`); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	})
	out, err := f.store.WatchTrends(ctx, BriefReader{}, "decisions", 7, f.now)
	if err != nil {
		t.Fatalf("admitted read failed: %v", err)
	}
	if !hooked {
		t.Fatal("proof hook never ran")
	}
	for _, panel := range out["panels"].([]map[string]any) {
		if panel["id"] == "asks_flow" {
			if got := trendValue(t, panel, "raised"); got != 1 {
				t.Fatalf("raised = %v: a post-proof transfer leaked into the admitted snapshot", got)
			}
		}
	}
	// The transfer was real and eligibility is fresh: the next request denies.
	if _, err := f.store.WatchTrends(WithAccessScope(f.ctx, AccessScope{ActorID: "viewer"}), BriefReader{}, "decisions", 7, f.now); err != ErrForbidden {
		t.Fatalf("next request after the transfer: %v", err)
	}
}

// API writes use the wall clock. Pin only their future timestamps to the
// fixture's fixed instant; historical events already carry their intended time.
// The event timestamp trigger synchronizes every derived seek key atomically.
func (f *trendsFixture) freezeLiveWrites() {
	f.t.Helper()
	at := loopTS(f.now)
	f.exec(`UPDATE events SET ts=? WHERE anx_timestamp_key(ts)>anx_timestamp_key(?)`, at, at)
	f.exec(`UPDATE cards SET created_at=? WHERE anx_timestamp_key(created_at)>anx_timestamp_key(?)`, at, at)
	f.exec(`UPDATE cards SET updated_at=? WHERE anx_timestamp_key(updated_at)>anx_timestamp_key(?)`, at, at)
	f.exec(`UPDATE brief_asks SET created_at=? WHERE anx_timestamp_key(created_at)>anx_timestamp_key(?)`, at, at)
	f.exec(`UPDATE brief_asks SET answered_at=? WHERE anx_timestamp_key(answered_at)>anx_timestamp_key(?)`, at, at)
	f.exec(`UPDATE human_attention_request_resolutions SET created_at=? WHERE anx_timestamp_key(created_at)>anx_timestamp_key(?)`, at, at)
}
