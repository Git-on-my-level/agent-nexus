package primitives

import (
	"context"
	"testing"
	"time"
)

func TestWatcherPrivateCardCannotEscalateToUnrelatedHuman(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	board, err := s.CreateBoard(ctx, "placer", map[string]any{"title": "Private audit"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "placer", workString(board["thread_id"]), map[string]any{"pm_actor_id": "placer"}, nil); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "placer", workString(board["id"]), map[string]any{"title": "Private loop", "cadence": map[string]any{"every_seconds": 60}})
	if err != nil {
		t.Fatal(err)
	}
	id := workString(w["id"])
	watcherReceipt(t, s, ctx, id, now, "infra_error", true)
	watcherStep(t, s, ctx, now.Add(2*time.Hour))
	if n := watcherCount(t, s, "human_attention_requested"); n != 0 {
		t.Fatal("private stop ask leaked", n)
	}
	if got := attentionAll(t, s, ctx, "human", "human", "decide"); len(got) != 0 {
		t.Fatal(got)
	}
	// All bookkeeping columns inherit the subject card's visibility.
	for _, actor := range []string{"placer", "human"} {
		var n int
		err = s.db.QueryRowContext(WithAccessScope(ctx, AccessScope{ActorID: actor}), `SELECT COUNT(*) FROM watcher_health WHERE card_id=?`, id).Scan(&n)
		if err != nil || (n == 1) != (actor == "placer") {
			t.Fatal(actor, n, err)
		}
	}
}

func TestWatcherRecoveryPreservesOtherPlacersAndRestoresDefaults(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	id := watcherLoopFixture(t, s, ctx, now)
	watcherStep(t, s, ctx, now.Add(6*time.Minute))
	// The card had no new actor activity, so fallback is the human.
	if _, err := s.PlaceAttention(ctx, "preferred", AttentionPlacement{SubjectRef: "card:" + id, For: "actor:human", To: "watch", Reason: "Keep watching", RequestKey: "other"}); err != nil {
		t.Fatal(err)
	}
	watcherReceipt(t, s, ctx, id, now.Add(7*time.Minute), "measured", false)
	watcherStep(t, s, ctx, now.Add(7*time.Minute))
	got := attentionAll(t, s, ctx, "human", "human", "watch")
	if len(got) != 1 || got[0].PlacedBy != "actor:preferred" {
		t.Fatal("unrelated placement lost", got)
	}
	// A second loop with no other placer returns to the ordinary Watch default.
	id2 := watcherLoopFixture(t, s, ctx, now)
	watcherStep(t, s, ctx, now.Add(6*time.Minute))
	watcherReceipt(t, s, ctx, id2, now.Add(7*time.Minute), "measured", false)
	watcherStep(t, s, ctx, now.Add(7*time.Minute))
	got = attentionAll(t, s, ctx, "human", "human", "watch")
	if len(got) != 2 {
		t.Fatal("default Watch item suppressed", got)
	}
}

func TestWatcherCursorDoesNotStarveAfterFullPage(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	for i := 0; i < 26; i++ {
		watcherLoopFixture(t, s, ctx, now)
	}
	watcherStep(t, s, ctx, now.Add(6*time.Minute))
	var n int
	if err := s.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM watcher_health WHERE level='decide'`).Scan(&n); err != nil || n != 25 {
		t.Fatal(n, err)
	}
	watcherStep(t, s, ctx, now.Add(6*time.Minute))
	if err := s.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM watcher_health WHERE level='decide'`).Scan(&n); err != nil || n != 26 {
		t.Fatal(n, err)
	}
}

func TestWatcherPrivateLatestReceiptWithholdsHumanJudgment(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	id := watcherLoopFixture(t, s, ctx, now)
	board, err := s.CreateBoard(ctx, "placer", map[string]any{"title": "Private evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "placer", workString(board["thread_id"]), map[string]any{"pm_actor_id": "placer"}, nil); err != nil {
		t.Fatal(err)
	}
	_, err = s.AppendEvent(ctx, "placer", map[string]any{"type": "run_recorded", "summary": "Private stop evidence", "refs": []string{"card:" + id, "thread:" + workString(board["thread_id"])}, "payload": map[string]any{"run_id": "private-stop", "outcome": "infra_error", "stop_rule_triggered": true}})
	if err != nil {
		t.Fatal(err)
	}
	watcherStep(t, s, ctx, now.Add(2*time.Hour))
	if n := watcherCount(t, s, "human_attention_requested"); n != 0 {
		t.Fatal("private receipt generated a human ask", n)
	}
	if got := attentionAll(t, s, ctx, "human", "human", "decide"); len(got) != 0 {
		t.Fatal("filtered prefix invented health", got)
	}
}

func TestWatcherPlacementRetainsReceiptPrivacyAfterRevocation(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	id := watcherLoopFixture(t, s, ctx, now)
	board, err := s.CreateBoard(ctx, "placer", map[string]any{"title": "Private receipt evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "placer", workString(board["thread_id"]), map[string]any{"pm_actor_id": "placer"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"first", "second"} {
		_, err = s.AppendEvent(ctx, "placer", map[string]any{"type": "run_recorded", "summary": "Private failure", "refs": []string{"card:" + id, "thread:" + workString(board["thread_id"])}, "payload": map[string]any{"run_id": run, "outcome": "infra_error"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	watcherStep(t, s, ctx, now.Add(time.Minute))
	watcherStep(t, s, ctx, now.Add(3*time.Minute))
	if got := attentionAll(t, s, ctx, "placer", "agent", "digest"); len(got) != 1 {
		t.Fatal("missing placement", got)
	}
	if _, err = s.PatchThread(ctx, "placer", workString(board["thread_id"]), map[string]any{"pm_actor_id": "preferred"}, nil); err != nil {
		t.Fatal(err)
	}
	// No clock tick is needed to stop exposing the formerly readable allegation.
	if got := attentionAll(t, s, ctx, "placer", "agent", "digest"); len(got) != 0 {
		t.Fatal("revoked receipt leaked via placement", got)
	}
}

func TestWatcherPrivateOldEffectDoesNotHidePublicMissedRuns(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	id := watcherLoopFixture(t, s, ctx, now)
	board, err := s.CreateBoard(ctx, "placer", map[string]any{"title": "Private old effect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "placer", workString(board["thread_id"]), map[string]any{"pm_actor_id": "placer"}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := s.AppendEvent(ctx, "placer", map[string]any{"type": "run_recorded", "summary": "Old private effect", "refs": []string{"card:" + id, "thread:" + workString(board["thread_id"])}, "payload": map[string]any{"run_id": "private-effect", "outcome": "acted"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE events SET ts=? WHERE id=?`, loopTS(now.Add(-time.Hour)), event["id"]); err != nil {
		t.Fatal(err)
	}
	watcherReceipt(t, s, ctx, id, now.Add(-time.Minute), "measured", false)
	watcherReceipt(t, s, ctx, id, now, "measured", false)
	// No recent activity from the assignee; only the old effect is private.
	watcherStep(t, s, ctx, now.Add(2*time.Hour))
	if got := attentionAll(t, s, ctx, "human", "human", "decide"); len(got) != 1 {
		t.Fatal("public missed runs suppressed by an irrelevant private effect", got)
	}
}
