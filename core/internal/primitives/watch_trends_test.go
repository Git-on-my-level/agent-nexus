package primitives

import (
	"context"
	"encoding/json"
	"fmt"
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

// rawEvents inserts n events of one type on work's thread, newest at ago,
// one minute apart. Database triggers derive brief_asks rows and ownership
// edges exactly as for an appended event.
func (f *trendsFixture) rawEvents(work map[string]any, prefix, kind string, n int, ago time.Duration, payload func(i int) string) {
	f.t.Helper()
	for i := 0; i < n; i++ {
		f.exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,?,?,?,?,?)`,
			fmt.Sprintf("%s-%04d", prefix, i), kind, loopTS(f.now.Add(-ago-time.Duration(i)*time.Minute)), "agent-hidden",
			anyStringValue(work["thread_id"]), `["thread:`+anyStringValue(work["thread_id"])+`"]`, payload(i))
	}
}

// A read with the explicit PM rule and no access scope (an unauthenticated
// development reader) binds the predicate's argument in text order.
func TestWatchTrendsReadsWithoutAccessScope(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Battery", "", nil)
	f.ask(work, "agent-1", 24*time.Hour, time.Hour)
	f.run(work, 24*time.Hour, "acted")
	if _, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(f.board["id"]), map[string]any{"title": "Loop", "cadence": map[string]any{"every_seconds": 3600}}); err != nil {
		t.Fatal(err)
	}
	f.trends("owner", 30) // fold
	visible := func(_, owner string) bool { return owner == "" }
	panels := map[string]map[string]any{}
	for _, group := range WatchTrendGroups {
		out, err := f.store.WatchTrends(f.ctx, BriefReader{Visible: visible}, group, 30, f.now)
		if err != nil {
			t.Fatalf("%s: %v", group, err)
		}
		for _, p := range out["panels"].([]map[string]any) {
			panels[p["id"].(string)] = p
		}
	}
	if trendValue(t, panels["asks_flow"], "raised") != 1 || trendValue(t, panels["asks_flow"], "value") != 1 {
		t.Fatalf("asks = %#v", panels["asks_flow"])
	}
	if trendValue(t, panels["loop_runs"], "value") != 1 || trendValue(t, panels["work_flow"], "started") != 2 {
		t.Fatalf("runs = %#v work = %#v", panels["loop_runs"], panels["work_flow"])
	}
}

// When the answer cap cuts into the current period, that period has no
// median: the answers left out could move it either way.
func TestWatchTrendsAnswerTimeUnknownWhenCurrentPeriodIsCapped(t *testing.T) {
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
	wait := f.group("owner", "decisions", 7)["answer_time"]
	if wait["truncated"] != true || wait["value"].(*float64) != nil {
		t.Fatalf("answer time from a capped current period: %#v", wait)
	}
}

// With more loops than the read admits, every reporting share is unknown.
func TestWatchTrendsLoopHealthUnknownWhenLoopsAreCapped(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	for i := 0; i <= trendsLoopCap; i++ {
		w, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(f.board["id"]), map[string]any{"title": fmt.Sprintf("Loop %d", i), "cadence": map[string]any{"every_seconds": 3600}})
		if err != nil {
			t.Fatal(err)
		}
		f.run(w, time.Minute, "acted")
	}
	health := f.group("owner", "loops", 7)["loop_health"]
	if health == nil || health["truncated"] != true || health["value"].(*float64) != nil || health["previous"].(*float64) != nil {
		t.Fatalf("loop health over a partial population: %#v", health)
	}
	for _, p := range health["series"].([]map[string]any)[0]["points"].([]*float64) {
		if p != nil {
			t.Fatal("a share over a partial population was charted")
		}
	}
}

// The response a reader gets is byte-identical whether or not records it
// cannot see exist: hidden records newer and older than the visible ones, in
// every family, past every cap, private by board, by thread and by reference.
func TestWatchTrendsResponseIsInvariantToHiddenRecords(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	day := 24 * time.Hour
	public := f.card("Public", "", nil)
	f.ask(public, "agent-1", 2*day, 3*time.Hour)
	f.ask(public, "agent-2", 20*day, 0)
	f.ask(public, "agent-1", 70*day, day)
	f.run(public, day, "acted")
	f.run(public, 40*day, "infra_error")
	if _, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(f.board["id"]), map[string]any{"title": "Public loop", "cadence": map[string]any{"every_seconds": 3600}}); err != nil {
		t.Fatal(err)
	}
	f.group("owner", "initiatives", 30) // fold
	f.exec(`UPDATE brief_initiatives SET promise_at=? WHERE card_id=?`, loopTS(f.now.Add(-3*day)), anyStringValue(public["id"]))

	read := func() map[string][]byte {
		out := map[string][]byte{}
		for _, days := range WatchTrendRanges {
			for _, group := range WatchTrendGroups {
				body, err := f.store.WatchTrends(WithAccessScope(f.ctx, AccessScope{ActorID: "stranger"}), BriefReader{ActorID: "stranger", Kind: "human"}, group, days, f.now)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				out[fmt.Sprintf("%s/%d", group, days)] = raw
			}
		}
		return out
	}
	f.trends("owner", 30) // settle derived state before the first read
	before := read()
	ownerBefore := f.trends("owner", 30)

	// A private board, a PM-private card thread on the public board, and an
	// ask on a public thread about a private card.
	private, err := f.store.CreateBoard(f.ctx, "owner", map[string]any{"title": "Private", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.PatchThread(f.ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	secret, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(private["id"]), map[string]any{"title": "Secret"})
	if err != nil {
		t.Fatal(err)
	}
	pmThread := f.card("PM private", "owner", nil)
	ref := anyStringValue(secret["ref"])
	askPayload := func(int) string {
		return fmt.Sprintf(`{"payload":{"kind":"ask","title":"Hidden","requester_actor_id":"agent-hidden","subject_ref":%q,"response_proposals":["Proceed"]}}`, ref)
	}
	runPayload := func(batch string) func(int) string {
		return func(i int) string {
			return fmt.Sprintf(`{"subject_ref":%q,"payload":{"run_id":"%s-%d","outcome":"acted"}}`, ref, batch, i)
		}
	}
	for _, at := range []time.Duration{time.Hour, 50 * day} { // newer and older than the visible records
		tag := fmt.Sprint(int(at.Hours()))
		f.rawEvents(secret, "hidden-run-"+tag, "run_recorded", trendsRunCap+5, at, runPayload("hidden-"+tag))
		f.rawEvents(pmThread, "pm-run-"+tag, "run_recorded", 5, at, runPayload("pm-"+tag))
		f.rawEvents(secret, "hidden-ask-"+tag, "human_attention_requested", trendsAskCap+5, at, askPayload)
		for i := 0; i < trendsAnswerCap+5; i++ {
			id := fmt.Sprintf("hidden-ask-%s-%04d", tag, i)
			answer := fmt.Sprintf("hidden-answer-%s-%04d", tag, i)
			stamp := loopTS(f.now.Add(-at - time.Duration(i)*time.Minute + 30*time.Second))
			f.exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,?,?,?,?,?)`, answer, "human_attention_responded", stamp, "owner",
				anyStringValue(secret["thread_id"]), `["thread:`+anyStringValue(secret["thread_id"])+`"]`, `{"payload":{"outcome":"answered"}}`)
			f.exec(`INSERT INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES(?,?,'answered','owner',?)`, id, answer, stamp)
		}
		for i := 0; i < 2*trendsCreatedCap+5; i++ {
			column := "todo"
			if i%2 == 0 {
				column = "done"
			}
			stamp := loopTS(f.now.Add(-at - time.Duration(i)*time.Minute))
			f.exec(`INSERT INTO cards(id,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Hidden','',?,?,'owner',?,'owner')`,
				fmt.Sprintf("hidden-card-%s-%04d", tag, i), anyStringValue(private["id"]), column, stamp, stamp)
		}
	}
	if _, err := f.store.AppendEvent(f.ctx, "agent-1", map[string]any{
		"type": "human_attention_requested", "thread_id": public["thread_id"], "summary": "About a secret",
		"refs":    []string{"thread:" + anyStringValue(public["thread_id"])},
		"payload": map[string]any{"kind": "ask", "title": "About a secret", "requester_actor_id": "agent-1", "subject_ref": ref, "response_proposals": []any{"Proceed"}},
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= trendsLoopCap; i++ {
		if _, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(private["id"]), map[string]any{"title": "Hidden loop", "cadence": map[string]any{"every_seconds": 3600}}); err != nil {
			t.Fatal(err)
		}
	}
	f.trends("owner", 30) // fold every hidden card
	f.exec(`UPDATE brief_initiatives SET promise_at=? WHERE card_id IN (SELECT id FROM cards WHERE board_id=?)`, loopTS(f.now.Add(-2*day)), anyStringValue(private["id"]))

	after := read()
	for key, want := range before {
		if string(after[key]) != string(want) {
			t.Fatalf("%s changed with hidden records:\nbefore %s\nafter  %s", key, want, after[key])
		}
	}
	// The hidden records are real: the owner sees them.
	ownerAfter := f.trends("owner", 30)
	if ownerAfter["loop_runs"]["truncated"] != true || ownerBefore["loop_runs"]["truncated"] == true {
		t.Fatalf("owner runs before %#v after %#v", ownerBefore["loop_runs"], ownerAfter["loop_runs"])
	}
}

// A ranking is only offered for a period the read covered in full.
func TestWatchTrendsRankingsNeedACompleteCurrentPeriod(t *testing.T) {
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
	panels := f.trends("owner", 7)
	for _, id := range []string{"ask_sources", "agent_runs"} {
		p := panels[id]
		if p["truncated"] != true || len(p["rows"].([]map[string]any)) != 0 {
			t.Fatalf("%s ranked a capped current period: %#v", id, p)
		}
	}
}

// --- re-keying: a container populated while private, then changed ---

type trendCounts struct{ raised, answered, runs, started, done, due float64 }

func (f *trendsFixture) counts(actor string) trendCounts {
	f.t.Helper()
	return f.countsFrom(f.trends(actor, 30))
}

// countsUnfolded reads without folding pending brief changes first, as a
// reader does between a write and the next fold.
func (f *trendsFixture) countsUnfolded(actor string) trendCounts {
	f.t.Helper()
	p := map[string]map[string]any{}
	for _, group := range WatchTrendGroups {
		out, err := f.store.WatchTrends(WithAccessScope(f.ctx, AccessScope{ActorID: actor}), BriefReader{ActorID: actor, Kind: "human"}, group, 30, f.now)
		if err != nil {
			f.t.Fatal(err)
		}
		for _, panel := range out["panels"].([]map[string]any) {
			p[panel["id"].(string)] = panel
		}
	}
	return f.countsFrom(p)
}

func (f *trendsFixture) countsFrom(p map[string]map[string]any) trendCounts {
	f.t.Helper()
	v := func(id, field string) float64 { return trendValue(f.t, p[id], field) }
	return trendCounts{v("asks_flow", "raised"), v("asks_flow", "value"), v("loop_runs", "value"), v("work_flow", "started"), v("work_flow", "value"), v("promises", "due")}
}

// strangerBytes is every group's JSON for the stranger.
func (f *trendsFixture) strangerBytes() string {
	f.t.Helper()
	f.trends("stranger", 30) // settle derived state
	out := ""
	for _, group := range WatchTrendGroups {
		body, err := f.store.WatchTrends(WithAccessScope(f.ctx, AccessScope{ActorID: "stranger"}), BriefReader{ActorID: "stranger", Kind: "human"}, group, 30, f.now)
		if err != nil {
			f.t.Fatal(err)
		}
		raw, _ := json.Marshal(body)
		out += string(raw)
	}
	return out
}

func (f *trendsFixture) setOwner(thread, owner string) {
	f.t.Helper()
	if _, err := f.store.PatchThread(f.ctx, "owner", thread, map[string]any{"pm_actor_id": owner}, nil); err != nil {
		f.t.Fatal(err)
	}
}

// populate fills a container with one record of every family a re-key
// touches: the cards (created; the first done when there are two), a missed
// promise on the last, an ask and a run on a card's thread, and an ask on a
// public thread about a card, answered there. The answer's privacy comes only
// from its ask.
func (f *trendsFixture) populate(cards []map[string]any, host map[string]any) {
	f.t.Helper()
	first, last := cards[0], cards[len(cards)-1]
	if _, err := f.store.AppendEvent(f.ctx, "owner", map[string]any{"type": "message_posted", "thread_id": last["thread_id"], "summary": "Plan", "refs": []string{"thread:" + anyStringValue(last["thread_id"])},
		"payload": map[string]any{"body": "Plan", "next": map[string]any{"at": loopTS(f.now.Add(-2 * 24 * time.Hour)), "what": "Ship"}}}); err != nil {
		f.t.Fatal(err)
	}
	thread := anyStringValue(first["thread_id"])
	if _, err := f.store.AppendEvent(f.ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": thread, "summary": "In container", "refs": []string{"thread:" + thread},
		"payload": map[string]any{"kind": "ask", "title": "In container", "requester_actor_id": "agent-1", "subject_ref": anyStringValue(first["ref"]), "response_proposals": []any{"Proceed"}}}); err != nil {
		f.t.Fatal(err)
	}
	// On a container card's thread but about the public host: private only
	// through its thread.
	if _, err := f.store.AppendEvent(f.ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": thread, "summary": "About the host", "refs": []string{"thread:" + thread},
		"payload": map[string]any{"kind": "ask", "title": "About the host", "requester_actor_id": "agent-1", "subject_ref": anyStringValue(host["ref"]), "response_proposals": []any{"Proceed"}}}); err != nil {
		f.t.Fatal(err)
	}
	f.run(last, time.Hour, "acted")
	hostThread := anyStringValue(host["thread_id"])
	about, err := f.store.AppendEvent(f.ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": hostThread, "summary": "About it", "refs": []string{"thread:" + hostThread},
		"payload": map[string]any{"kind": "ask", "title": "About it", "requester_actor_id": "agent-1", "subject_ref": anyStringValue(first["ref"]), "response_proposals": []any{"Proceed"}}})
	if err != nil {
		f.t.Fatal(err)
	}
	id := anyStringValue(about["id"])
	if _, _, err := f.store.AppendHumanAttentionResponse(f.ctx, "human", id, "inbox:"+id, "", "", map[string]any{
		"type": "human_attention_responded", "thread_id": hostThread, "refs": []string{"event:" + id},
		"payload": map[string]any{"outcome": "answered", "response_text": "Proceed", "request_event_ref": "event:" + id},
	}, map[string]any{"requested": true, "target_actor_id": "agent-1", "thread_id": hostThread}); err != nil {
		f.t.Fatal(err)
	}
	if len(cards) > 1 {
		f.exec(`UPDATE cards SET column_key='done' WHERE id=?`, anyStringValue(first["id"]))
	}
}

// assertVisibleDelta checks that once a container is visible to actor, every
// family's count returns.
func (f *trendsFixture) assertVisibleDelta(actor string, base trendCounts, want trendCounts) {
	f.t.Helper()
	f.assertDelta(actor, f.counts(actor), base, want)
}

func (f *trendsFixture) assertDelta(actor string, got, base, want trendCounts) {
	f.t.Helper()
	delta := trendCounts{got.raised - base.raised, got.answered - base.answered, got.runs - base.runs, got.started - base.started, got.done - base.done, got.due - base.due}
	if delta != want {
		f.t.Fatalf("%s after the container became visible: delta %+v, want %+v", actor, delta, want)
	}
}

func (f *trendsFixture) assertStaleKeysCannotLeak(baseline string, ids func() []string) {
	f.t.Helper()
	for _, id := range ids() {
		f.exec(`UPDATE watch_trend_positions SET scope='' WHERE id=?`, id)
	}
	if got := f.strangerBytes(); got != baseline {
		f.t.Fatal("a record keyed public but hidden changed the stranger's response")
	}
}

func (f *trendsFixture) containerIDs(board string) func() []string {
	return func() []string {
		ids := []string{}
		rows, err := f.store.db.QueryContext(f.ctx, `SELECT id FROM cards WHERE board_id=? UNION SELECT e.id FROM events e JOIN cards c ON c.thread_id=e.thread_id WHERE c.board_id=?
		 UNION SELECT e.id FROM events e JOIN cards c ON json_extract(e.payload_json,'$.payload.subject_ref')='card:'||c.id WHERE c.board_id=?
		 UNION SELECT r.resolution_event_id FROM human_attention_request_resolutions r JOIN events e ON e.id=r.request_event_id JOIN cards c ON json_extract(e.payload_json,'$.payload.subject_ref')='card:'||c.id WHERE c.board_id=?`, board, board, board, board)
		if err != nil {
			f.t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		return ids
	}
}

var trendContainerDelta = trendCounts{raised: 3, answered: 1, runs: 1, started: 2, done: 1, due: 1}

func TestWatchTrendsRekeysBoardPrivacyChanges(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	host := f.card("Public host", "", nil)
	f.ask(host, "agent-2", 3*24*time.Hour, time.Hour)
	baseline, base := f.strangerBytes(), f.counts("stranger")
	board, err := f.store.CreateBoard(f.ctx, "owner", map[string]any{"title": "Container", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyStringValue(board["thread_id"])
	f.setOwner(thread, "owner")
	cards := []map[string]any{}
	for _, title := range []string{"One", "Two"} {
		w, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, w)
	}
	f.populate(cards, host)
	if f.strangerBytes() != baseline {
		t.Fatal("a private board's records changed the stranger's response")
	}
	f.setOwner(thread, "") // private -> public
	f.assertVisibleDelta("stranger", base, trendContainerDelta)
	f.setOwner(thread, "owner") // public -> private
	if f.strangerBytes() != baseline {
		t.Fatal("records of a board made private again changed the stranger's response")
	}
	f.assertStaleKeysCannotLeak(baseline, f.containerIDs(anyStringValue(board["id"])))
}

func TestWatchTrendsRekeysPMThreadPrivacyChanges(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	host := f.card("Public host", "", nil)
	f.ask(host, "agent-2", 3*24*time.Hour, time.Hour)
	baseline, base := f.strangerBytes(), f.counts("stranger")
	card := f.card("PM private", "owner", nil)
	f.populate([]map[string]any{card}, host)
	if f.strangerBytes() != baseline {
		t.Fatal("a PM-private card's records changed the stranger's response")
	}
	f.setOwner(anyStringValue(card["thread_id"]), "")
	f.assertVisibleDelta("stranger", base, trendCounts{raised: 3, answered: 1, runs: 1, started: 1, due: 1})
	f.setOwner(anyStringValue(card["thread_id"]), "owner")
	if f.strangerBytes() != baseline {
		t.Fatal("records of a thread made private again changed the stranger's response")
	}
	f.assertStaleKeysCannotLeak(baseline, f.containerIDs(anyStringValue(f.board["id"])))
}

func TestWatchTrendsRekeysCardsMovedBetweenBoards(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	host := f.card("Public host", "", nil)
	f.ask(host, "agent-2", 3*24*time.Hour, time.Hour)
	baseline, base := f.strangerBytes(), f.counts("stranger")
	private, err := f.store.CreateBoard(f.ctx, "owner", map[string]any{"title": "Private", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	f.setOwner(anyStringValue(private["thread_id"]), "owner")
	card, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(private["id"]), map[string]any{"title": "Mover"})
	if err != nil {
		t.Fatal(err)
	}
	f.populate([]map[string]any{card}, host)
	if f.strangerBytes() != baseline {
		t.Fatal("a private card's records changed the stranger's response")
	}
	f.exec(`UPDATE cards SET board_id=? WHERE id=?`, anyStringValue(f.board["id"]), anyStringValue(card["id"])) // to a public board
	// Read before the brief re-folds the moved card: its promise must already
	// be keyed to its new board.
	f.assertDelta("stranger", f.countsUnfolded("stranger"), base, trendCounts{raised: 3, answered: 1, runs: 1, started: 1, due: 1})
	f.exec(`UPDATE cards SET board_id=? WHERE id=?`, anyStringValue(private["id"]), anyStringValue(card["id"])) // and back
	if f.strangerBytes() != baseline {
		t.Fatal("records of a card moved back to a private board changed the stranger's response")
	}
	f.assertStaleKeysCannotLeak(baseline, f.containerIDs(anyStringValue(private["id"])))
}

func TestWatchTrendsRekeysOwnerTransfers(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	host := f.card("Public host", "", nil)
	f.ask(host, "agent-2", 3*24*time.Hour, time.Hour)
	baseline, newBase := f.strangerBytes(), f.counts("successor")
	board, err := f.store.CreateBoard(f.ctx, "owner", map[string]any{"title": "Container", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyStringValue(board["thread_id"])
	f.setOwner(thread, "owner")
	cards := []map[string]any{}
	for _, title := range []string{"One", "Two"} {
		w, err := f.store.CreateWork(f.ctx, "owner", anyStringValue(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, w)
	}
	f.populate(cards, host)
	f.setOwner(thread, "successor")
	f.assertVisibleDelta("successor", newBase, trendContainerDelta)
	if f.strangerBytes() != baseline {
		t.Fatal("a transferred board's records changed the stranger's response")
	}
}

// An answer recorded before the resolution that links it to a private ask
// is re-keyed when the resolution lands.
func TestWatchTrendsRekeysAnswersWhenResolved(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	host := f.card("Public host", "", nil)
	private := f.card("Private", "owner", nil)
	thread := anyStringValue(host["thread_id"])
	f.rawEvents(host, "ask", "human_attention_requested", 1, time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"kind":"ask","title":"Q","requester_actor_id":"agent-1","subject_ref":%q,"response_proposals":["Proceed"]}}`, anyStringValue(private["ref"]))
	})
	f.exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('answer-0','human_attention_responded',?,'human',?,?,'{"payload":{"outcome":"answered"}}')`, loopTS(f.now.Add(-time.Minute)), thread, `["thread:`+thread+`"]`)
	scope := func() string {
		var s string
		if err := f.store.db.QueryRowContext(f.ctx, `SELECT scope FROM watch_trend_positions WHERE kind='answer' AND id='answer-0'`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if got := scope(); got != "" {
		t.Fatalf("unlinked answer scope = %q", got)
	}
	f.exec(`INSERT INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES('ask-0000','answer-0','answered','human',?)`, loopTS(f.now.Add(-time.Minute)))
	if got := scope(); got != "owner" {
		t.Fatalf("answer to a private ask keyed %q, want owner", got)
	}
}
