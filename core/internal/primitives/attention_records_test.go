package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func attentionFixture(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for _, id := range []string{"human", "other", "placer", "preferred"} {
		kind := "agent"
		if id == "human" || id == "other" {
			kind = "human"
		}
		_, err = ws.DB().Exec(`INSERT INTO actors(id,display_name,created_at) VALUES(?,?,?)`, id, id, time.Now().Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
		_, err = ws.DB().Exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,?,?,?)`, id+"-id", id, id, time.Now().Format(time.RFC3339Nano), time.Now().Format(time.RFC3339Nano), `{"principal_kind":"`+kind+`"}`)
		if err != nil {
			t.Fatal(err)
		}
	}
	return s, ctx
}
func attentionAll(t *testing.T, s *Store, ctx context.Context, actor, kind, surface string) []AttentionItem {
	t.Helper()
	items := []AttentionItem{}
	cursor := ""
	for n := 0; n < 100; n++ {
		page, err := s.AttentionItems(ctx, actor, kind, surface, "", "builtin:initiatives", cursor, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, page.Items...)
		if page.NextCursor == nil {
			return items
		}
		cursor = *page.NextCursor
	}
	t.Fatal("nonterminating attention cursor")
	return nil
}

func TestAttentionDefaultsKeepWorkOrdering(t *testing.T) {
	s, ctx := attentionFixture(t)
	for _, title := range []string{"Alpha older work", "Zulu newer work"} {
		if _, err := s.CreateWork(ctx, "placer", "", map[string]any{"title": title}); err != nil {
			t.Fatal(err)
		}
	}
	work, err := s.ListWork(ctx, WorkListFilter{Limit: 100})
	if err != nil || len(work.Work) != 2 {
		t.Fatal(work, err)
	}
	items := attentionAll(t, s, ctx, "human", "human", "watch")
	if len(items) != 2 || items[0].Title != anyStringValue(work.Work[0]["title"]) || items[1].Title != anyStringValue(work.Work[1]["title"]) {
		t.Fatalf("default attention order=%v work=%v", items, work.Work)
	}
}

func TestAttentionPrecedenceIsolationAndReplay(t *testing.T) {
	s, ctx := attentionFixture(t)
	w, err := s.CreateWork(ctx, "placer", "", map[string]any{"title": "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	place := func(actor, surface, key string) map[string]any {
		t.Helper()
		v, err := s.PlaceAttention(ctx, actor, AttentionPlacement{SubjectRef: anyStringValue(w["ref"]), For: "human:human-id", To: surface, Reason: "Useful news", RequestKey: key})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	first := place("placer", "watch", "stable")
	replay := place("placer", "watch", "stable")
	if first["id"] != replay["id"] {
		t.Fatal("retry appended another placement")
	}
	if _, err = s.PlaceAttention(ctx, "placer", AttentionPlacement{SubjectRef: anyStringValue(w["ref"]), For: "actor:human", To: "routine", Reason: "Useful news", RequestKey: "stable"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	place("preferred", "digest", "")
	place("placer", "routine", "")
	items := attentionAll(t, s, ctx, "human", "human", "all")
	if len(items) != 1 || items[0].Surface != "routine" || items[0].PlacedBy != "actor:placer" {
		t.Fatal(items)
	}
	preferred := "agent:preferred-id"
	if err = s.SetAttentionAgent(ctx, "human", &preferred); err != nil {
		t.Fatal(err)
	}
	items = attentionAll(t, s, ctx, "human", "human", "all")
	if len(items) != 1 || items[0].Surface != "digest" || items[0].PlacedBy != "actor:preferred" {
		t.Fatal(items)
	}
	if err = s.SetAttentionAgent(ctx, "human", nil); err != nil {
		t.Fatal(err)
	}
	other := attentionAll(t, s, ctx, "other", "human", "all")
	if len(other) != 1 || other[0].PlacedBy != "anx_default" {
		t.Fatal(other)
	}
	// Canonical event visibility protects a placement's authored reason too.
	hidden, err := s.CreateWork(ctx, "placer", "", map[string]any{"title": "Confidential"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "placer", anyStringValue(hidden["thread_id"]), map[string]any{"pm_actor_id": "placer"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PlaceAttention(ctx, "placer", AttentionPlacement{SubjectRef: anyStringValue(w["ref"]), For: "actor:human", To: "decide", Reason: "Review " + anyStringValue(hidden["ref"])}); err != nil {
		t.Fatalf("placement failed: %v", err)
	}
	scoped := WithRequestAccessScope(ctx, AccessScope{ActorID: "human"})
	for _, item := range attentionAll(t, s, scoped, "human", "human", "all") {
		if item.Reason == "Review "+anyStringValue(hidden["ref"]) {
			t.Fatal("private reason disclosed")
		}
	}
}
func TestAttentionMandatoryAsksPaginationAndFeedback(t *testing.T) {
	s, ctx := attentionFixture(t)
	w, err := s.CreateWork(ctx, "placer", "", map[string]any{"title": "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 105; i++ {
		id := fmt.Sprintf("ask-%03d", i)
		event, err := s.AppendEvent(ctx, "placer", map[string]any{"id": id, "type": "human_attention_requested", "summary": "Choose", "thread_id": w["thread_id"], "refs": []string{anyStringValue(w["ref"])}, "payload": map[string]any{"kind": "ask", "title": "Choose", "recipient_actor_id": "human", "subject_ref": "card:" + anyStringValue(w["id"]), "requester_actor_id": "placer"}})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			_, err = s.PlaceAttention(ctx, "placer", AttentionPlacement{SubjectRef: "event:" + anyStringValue(event["id"]), For: "actor:human", To: "routine", Reason: "An agent cannot hide this ask"})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	items := attentionAll(t, s, ctx, "human", "human", "decide")
	if len(items) != 105 {
		t.Fatalf("asks=%d", len(items))
	}
	for _, item := range items {
		if item.Surface != "decide" {
			t.Fatal(item)
		}
	}
	for _, item := range attentionAll(t, s, ctx, "human", "human", "routine") {
		if item.SubjectRef != "" {
			t.Fatal("open ask demoted", item)
		}
	}
	page, err := s.AttentionItems(ctx, "human", "human", "decide", "", "builtin:initiatives", "", time.Now())
	if err != nil || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	if _, err = s.AttentionItems(ctx, "other", "human", "decide", "", "builtin:initiatives", *page.NextCursor, time.Now()); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("foreign cursor: %v", err)
	}
	note := AttentionFeedback{SubjectRef: anyStringValue(w["ref"]), Signal: "needed_sooner", Surface: "watch", RequestKey: "feedback"}
	a, err := s.RecordAttentionFeedback(ctx, "human", note)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.RecordAttentionFeedback(ctx, "human", note)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("feedback replay", err)
	}
	note.Signal = "more_like_this"
	if _, err = s.RecordAttentionFeedback(ctx, "human", note); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed feedback replay: %v", err)
	}
	feedback, err := s.AttentionFeedbackList(ctx, "human", "", "")
	if err != nil || len(feedback["items"].([]map[string]any)) != 1 {
		t.Fatal(feedback, err)
	}
	note.Signal = "invalid"
	if _, err = s.RecordAttentionFeedback(ctx, "human", note); err == nil {
		t.Fatal("invalid signal accepted")
	}
}

func TestAttentionConcurrentFeedbackReplayAndRollback(t *testing.T) {
	s, ctx := attentionFixture(t)
	w, err := s.CreateWork(ctx, "placer", "", map[string]any{"title": "Feedback target"})
	if err != nil {
		t.Fatal(err)
	}
	note := AttentionFeedback{SubjectRef: anyStringValue(w["ref"]), Signal: "needed_sooner", Surface: "watch", RequestKey: "concurrent-feedback"}
	type result struct {
		event map[string]any
		err   error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			event, err := s.RecordAttentionFeedback(WithAccessScope(ctx, AccessScope{ActorID: "human"}), "human", note)
			results <- result{event, err}
		}()
	}
	close(start)
	var first map[string]any
	for i := 0; i < cap(results); i++ {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		if first == nil {
			first = got.event
		} else if !reflect.DeepEqual(first, got.event) {
			t.Fatalf("retry changed receipt: first=%v got=%v", first, got.event)
		}
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE type='attention_feedback'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent event count=%d err=%v", count, err)
	}
	// Failure to persist a receipt must roll back the event too.
	if _, err = s.db.ExecContext(ctx, `CREATE TRIGGER fail_attention_receipt BEFORE INSERT ON idempotency_replays WHEN NEW.request_key='failed-feedback' BEGIN SELECT RAISE(ABORT,'receipt unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	note.RequestKey = "failed-feedback"
	if _, err = s.RecordAttentionFeedback(ctx, "human", note); err == nil {
		t.Fatal("receipt failure accepted")
	}
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE type='attention_feedback'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("event escaped rollback: count=%d err=%v", count, err)
	}
	if _, err = s.db.ExecContext(ctx, `DROP TRIGGER fail_attention_receipt`); err != nil {
		t.Fatal(err)
	}
	// The failed key can be retried, and a second actor has a separate keyspace.
	if _, err = s.RecordAttentionFeedback(ctx, "human", note); err != nil {
		t.Fatal(err)
	}
	note.RequestKey = "concurrent-feedback"
	if other, err := s.RecordAttentionFeedback(ctx, "other", note); err != nil || other["id"] == first["id"] {
		t.Fatalf("actor-bound replay: %v %v", other, err)
	}
}
func TestAttentionVisitsScopesTransitionsAndOrdering(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	initial := AttentionSnapshot{"card:a": {Title: "Loop", State: "working", Plan: map[string]any{"step": "todo"}, Metric: 0.8}}
	for _, scope := range []string{"builtin:initiatives", "document:first"} {
		if err := s.RecordAttentionVisit(ctx, "actor:human", scope, initial, now); err != nil {
			t.Fatal(err)
		}
	}
	current := AttentionSnapshot{"card:a": {Title: "Loop", State: "blocked", Plan: map[string]any{"step": "done"}, Metric: 0.9}}
	changes, err := s.AttentionVisitChanges(ctx, "actor:human", "document:first", current, false)
	if err != nil || len(changes.Items) != 3 || len(changes.ChangedRefs) != 1 {
		t.Fatal(changes, err)
	}
	if err = s.RecordAttentionVisit(ctx, "actor:human", "document:first", current, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	repeat, err := s.AttentionVisitChanges(ctx, "actor:human", "document:first", current, false)
	if err != nil || *repeat.Since != *changes.Since || len(repeat.Items) != 3 {
		t.Fatal(repeat, err)
	}
	for _, pair := range [][2]string{{"actor:other", "document:first"}, {"actor:human", "document:second"}, {"actor:human", "builtin:overview"}} {
		d, err := s.AttentionVisitChanges(ctx, pair[0], pair[1], current, false)
		if err != nil || d.Since != nil || len(d.Items) != 0 {
			t.Fatal(d, err)
		}
	}
	// Decoder normalization avoids false typed-struct vs JSON-map changes.
	raw, _ := json.Marshal(initial)
	var decoded AttentionSnapshot
	json.Unmarshal(raw, &decoded)
	d, err := s.AttentionVisitChanges(ctx, "actor:human", "builtin:initiatives", decoded, false)
	if err != nil || len(d.Items) != 0 {
		t.Fatal(d, err)
	}
}
func TestAttentionPlacementPageIndexPlan(t *testing.T) {
	s, ctx := attentionFixture(t)
	rows, err := s.db.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT s.subject_ref,p.placed_at FROM attention_subjects s JOIN attention_placements p ON p.for_actor=s.for_actor AND p.subject_ref=s.subject_ref AND p.placed_by=`+attentionWinnerSQL+` WHERE s.for_actor=? AND s.subject_ref>? ORDER BY s.subject_ref LIMIT 101`, "actor:preferred", "human", "")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if containsAttentionScan(detail) {
			t.Fatal("unbounded plan", detail)
		}
	}
}
func containsAttentionScan(s string) bool { return len(s) >= 5 && s[:5] == "SCAN " }

func TestAttentionWorkSnapshotCapturesRealLoopMetricMoves(t *testing.T) {
	s, ctx := attentionFixture(t)
	w, err := s.CreateWork(ctx, "placer", "", map[string]any{"title": "Measured loop", "goal": map[string]any{"metric": "score", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	run := func(id string, value float64) {
		t.Helper()
		_, err := s.AppendEvent(ctx, "placer", map[string]any{"type": "run_recorded", "summary": "Measured", "refs": []string{anyStringValue(w["ref"])}, "payload": map[string]any{"run_id": id, "outcome": "measured", "metrics": map[string]any{"score": value}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	run("first", 0.8)
	now := time.Now().UTC()
	before, truncated, err := s.AttentionWorkSnapshot(ctx, now)
	if err != nil || truncated {
		t.Fatal(before, truncated, err)
	}
	if before[anyStringValue(w["ref"])].Metric != 0.8 {
		t.Fatal("missing actual goal value", before)
	}
	if err = s.RecordAttentionVisit(ctx, "actor:human", "builtin:initiatives", before, now); err != nil {
		t.Fatal(err)
	}
	run("next", 0.9)
	after, truncated, err := s.AttentionWorkSnapshot(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	changes, err := s.AttentionVisitChanges(ctx, "actor:human", "builtin:initiatives", after, truncated)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range changes.Items {
		if change.Kind == "metric_moved" && change.From == 0.8 && change.To == 0.9 {
			found = true
		}
	}
	if !found {
		t.Fatal(changes)
	}
}
