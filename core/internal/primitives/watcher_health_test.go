package primitives

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func watcherLoopFixture(t *testing.T, s *Store, ctx context.Context, now time.Time) string {
	t.Helper()
	w, err := s.CreateWork(ctx, "placer", "", map[string]any{"title": "Recurring audit", "assignee": "actor:placer", "cadence": map[string]any{"every_seconds": 60}})
	if err != nil {
		t.Fatal(err)
	}
	id := workString(w["id"])
	_, err = s.db.ExecContext(ctx, `UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.cadence_set_at',?) WHERE card_id=?`, loopTS(now), id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func watcherStep(t *testing.T, s *Store, ctx context.Context, at time.Time) {
	t.Helper()
	if err := s.MaintainLoopHealth(ctx, at, DefaultWatcherHealthConfig()); err != nil {
		t.Fatal(err)
	}
}
func watcherCount(t *testing.T, s *Store, typ string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE type=?`, typ).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func watcherReceipt(t *testing.T, s *Store, ctx context.Context, id string, at time.Time, outcome string, stop bool) {
	t.Helper()
	_, err := s.AppendEvent(ctx, "placer", map[string]any{"id": watcherID(fmt.Sprint(id, at)), "ts": loopTS(at), "type": "run_recorded", "summary": "Audit receipt", "refs": []string{"card:" + id}, "payload": map[string]any{"run_id": loopTS(at), "outcome": outcome, "stop_rule_triggered": stop}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `UPDATE events SET ts=? WHERE id=?`, loopTS(at), watcherID(fmt.Sprint(id, at))); err != nil {
		t.Fatal(err)
	}
}
func TestWatcherLoopEscalatesFallsBackAndRecovers(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	id := watcherLoopFixture(t, s, ctx, now)
	// A recently active agent remains the owner at the digest stage.
	if _, err := s.AppendEvent(ctx, "placer", map[string]any{"type": "message_posted", "refs": []string{}, "summary": "Working", "payload": map[string]any{"text": "Working"}}); err != nil {
		t.Fatal(err)
	}
	watcherStep(t, s, ctx, now.Add(3*time.Minute))
	if got := attentionAll(t, s, ctx, "placer", "agent", "digest"); len(got) != 0 {
		t.Fatal("early alarm", got)
	}
	watcherStep(t, s, ctx, now.Add(4*time.Minute))
	if got := attentionAll(t, s, ctx, "placer", "agent", "digest"); len(got) != 1 {
		t.Fatal("missing agent digest", got)
	}
	watcherStep(t, s, ctx, now.Add(6*time.Minute))
	if got := attentionAll(t, s, ctx, "placer", "agent", "decide"); len(got) != 1 {
		t.Fatal("missing decide escalation", got)
	}
	if n := watcherCount(t, s, "human_attention_requested"); n != 0 {
		t.Fatal("active agent decide opened an ask", n)
	}
	// The owner stops using the workspace. The human gets one recovery ask.
	watcherStep(t, s, ctx, now.Add(2*time.Hour))
	got := attentionAll(t, s, ctx, "human", "human", "decide")
	if len(got) != 1 || got[0].Title != "Restore reliable loop runs?" {
		t.Fatal("human decide skipped recovery ask", got)
	}
	if got := attentionAll(t, s, ctx, "placer", "agent", "decide"); len(got) != 0 {
		t.Fatal("old placement survived", got)
	}
	agent := "agent:preferred-id"
	if err := s.SetAttentionAgent(ctx, "human", &agent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlaceAttention(ctx, "preferred", AttentionPlacement{SubjectRef: "card:" + id, For: "actor:human", To: "routine", Reason: "Nothing to decide"}); err != nil {
		t.Fatal(err)
	}
	got = attentionAll(t, s, ctx, "human", "human", "decide")
	if len(got) != 1 || got[0].Title != "Restore reliable loop runs?" {
		t.Fatal("attention agent demoted the recovery ask", got)
	}
	watcherStep(t, s, ctx, now.Add(2*time.Hour))
	if n := watcherCount(t, s, "human_attention_requested"); n != 1 {
		t.Fatal("duplicate recovery ask", n)
	}
	watcherReceipt(t, s, ctx, id, now.Add(2*time.Hour+time.Minute), "measured", false)
	watcherStep(t, s, ctx, now.Add(2*time.Hour+time.Minute))
	if got := attentionAll(t, s, ctx, "human", "human", "decide"); len(got) != 0 {
		t.Fatal("recovery left alarm", got)
	}
}
func TestWatcherHumanOwnedLoopDecideOpensRecoveryAsk(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	id := watcherLoopFixture(t, s, ctx, now)
	if _, err := s.db.ExecContext(ctx, `UPDATE cards SET assignee=? WHERE id=?`, "actor:human", id); err != nil {
		t.Fatal(err)
	}
	watcherStep(t, s, ctx, now.Add(4*time.Minute))
	if n := watcherCount(t, s, "human_attention_requested"); n != 0 {
		t.Fatal("digest opened an ask", n)
	}
	if got := attentionAll(t, s, ctx, "human", "human", "digest"); len(got) != 1 {
		t.Fatal("missing human digest", got)
	}
	watcherStep(t, s, ctx, now.Add(6*time.Minute))
	got := attentionAll(t, s, ctx, "human", "human", "decide")
	if len(got) != 1 || got[0].Title != "Restore reliable loop runs?" {
		t.Fatal("human decide skipped recovery ask", got)
	}
	if got := attentionAll(t, s, ctx, "human", "human", "digest"); len(got) != 0 {
		t.Fatal("decide ask left a card placement", got)
	}
}
func TestWatcherPMCoalescesRetriesRepeatsAndRecovery(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	cfg := DefaultWatcherHealthConfig()
	step := func(unhealthy bool, at time.Time) {
		t.Helper()
		if err := s.ReconcilePMHealth(ctx, unhealthy, at, cfg); err != nil {
			t.Fatal(err)
		}
	}
	step(true, now)
	step(true, now)
	if n := watcherCount(t, s, "human_attention_requested"); n != 1 {
		t.Fatal(n)
	}
	step(true, now.Add(time.Hour))
	step(true, now.Add(2*time.Hour))
	if n := watcherCount(t, s, "human_attention_requested"); n != 1 {
		t.Fatal("duplicate open ask", n)
	}
	if n := watcherCount(t, s, "message_posted"); n != 2 {
		t.Fatal("repeat backoff", n)
	}
	step(true, now.Add(3*time.Hour))
	if n := watcherCount(t, s, "message_posted"); n != 3 {
		t.Fatal("missing repeat", n)
	}
	step(false, now.Add(4*time.Hour))
	step(false, now.Add(4*time.Hour))
	if n := watcherCount(t, s, "human_attention_withdrawn"); n != 1 {
		t.Fatal("withdrawal", n)
	}
	step(true, now.Add(5*time.Hour))
	if n := watcherCount(t, s, "human_attention_requested"); n != 2 {
		t.Fatal("new episode", n)
	}
}
func TestWatcherStopRuleAsksOnceAndClearsOnReceipt(t *testing.T) {
	for _, timer := range []bool{false, true} {
		t.Run(fmt.Sprint(timer), func(t *testing.T) {
			s, ctx := attentionFixture(t)
			now := time.Now().UTC()
			id := watcherLoopFixture(t, s, ctx, now)
			if timer {
				if _, err := s.db.ExecContext(ctx, `UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.cadence.stop_after_unhealthy_seconds',120) WHERE card_id=?`, id); err != nil {
					t.Fatal(err)
				}
			} else {
				watcherReceipt(t, s, ctx, id, now, "infra_error", true)
			}
			watcherStep(t, s, ctx, now.Add(5*time.Minute))
			watcherStep(t, s, ctx, now.Add(5*time.Minute))
			if n := watcherCount(t, s, "human_attention_requested"); n != 1 {
				t.Fatal("stop ask", n)
			}
			watcherReceipt(t, s, ctx, id, now.Add(6*time.Minute), "measured", false)
			watcherStep(t, s, ctx, now.Add(6*time.Minute))
			if n := watcherCount(t, s, "human_attention_withdrawn"); n != 1 {
				t.Fatal("recovery", n)
			}
		})
	}
}
func TestWatcherAlarmWritesRollbackTogether(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	cfg := DefaultWatcherHealthConfig()
	_, err := s.db.ExecContext(ctx, `CREATE TRIGGER fail_health_reminder BEFORE INSERT ON events WHEN NEW.type='message_posted' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcilePMHealth(ctx, true, now, cfg); err == nil {
		t.Fatal("expected failure")
	}
	if n := watcherCount(t, s, "human_attention_requested"); n != 0 {
		t.Fatal("partial ask", n)
	}
	if _, err = s.db.ExecContext(ctx, `DROP TRIGGER fail_health_reminder`); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcilePMHealth(ctx, true, now, cfg); err != nil {
		t.Fatal(err)
	}
	if n := watcherCount(t, s, "human_attention_requested"); n != 1 {
		t.Fatal("retry", n)
	}
}

func TestWatcherStopRuleValidation(t *testing.T) {
	for _, v := range []any{true, false, "true", 1, nil} {
		e := map[string]any{"summary": "Receipt", "refs": []string{"card:loop"}, "payload": map[string]any{"run_id": "r", "outcome": "measured", "stop_rule_triggered": v}}
		err := ValidateRunRecorded(e)
		_, valid := v.(bool)
		if (err == nil) != valid {
			t.Fatalf("value=%v error=%v", v, err)
		}
	}
	for _, v := range []any{60, 3155760000, 59, 3155760001, "60"} {
		err := validateLoopMetadata(map[string]any{"cadence": map[string]any{"every_seconds": 60, "stop_after_unhealthy_seconds": v}})
		n, ok := v.(int)
		valid := ok && n >= 60 && n <= 3155760000
		if (err == nil) != valid {
			t.Fatalf("timer=%v error=%v", v, err)
		}
	}
}

func TestWatcherClosedPMAskReraisesOnlyWhenDue(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	cfg := DefaultWatcherHealthConfig()
	if err := s.ReconcilePMHealth(ctx, true, now, cfg); err != nil {
		t.Fatal(err)
	}
	var ask string
	if err := s.db.QueryRowContext(ctx, `SELECT ask_id FROM watcher_health WHERE alarm_key='pm'`).Scan(&ask); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendHumanAttentionWithdrawal(ctx, "system", ask, map[string]any{"payload": map[string]any{"reason": "Human follow-through recorded"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcilePMHealth(ctx, true, now.Add(time.Minute), cfg); err != nil {
		t.Fatal(err)
	}
	if n := watcherCount(t, s, "human_attention_requested"); n != 1 {
		t.Fatal("immediate duplicate", n)
	}
	if err := s.ReconcilePMHealth(ctx, true, now.Add(time.Hour), cfg); err != nil {
		t.Fatal(err)
	}
	if n := watcherCount(t, s, "human_attention_requested"); n != 2 {
		t.Fatal("persistent incident not raised", n)
	}
}

func TestWatcherFailingAndStalledDebounce(t *testing.T) {
	for _, state := range []string{"failing", "stalled"} {
		t.Run(state, func(t *testing.T) {
			s, ctx := attentionFixture(t)
			now := time.Now().UTC()
			id := watcherLoopFixture(t, s, ctx, now)
			if state == "failing" {
				watcherReceipt(t, s, ctx, id, now.Add(-time.Minute), "infra_error", false)
				watcherReceipt(t, s, ctx, id, now, "infra_error", false)
			} else {
				if _, err := s.db.ExecContext(ctx, `UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.goal',json_object('metric','score','direction','up','target',1)) WHERE card_id=?`, id); err != nil {
					t.Fatal(err)
				}
				watcherReceipt(t, s, ctx, id, now, "measured", false)
			}
			watcherStep(t, s, ctx, now)
			watcherStep(t, s, ctx, now.Add(time.Minute))
			var level string
			if err := s.db.QueryRowContext(ctx, `SELECT level FROM watcher_health WHERE card_id=?`, id).Scan(&level); err != nil || level != "" {
				t.Fatal(level, err)
			}
			watcherStep(t, s, ctx, now.Add(2*time.Minute))
			if err := s.db.QueryRowContext(ctx, `SELECT level FROM watcher_health WHERE card_id=?`, id).Scan(&level); err != nil || level != "digest" {
				t.Fatal(level, err)
			}
		})
	}
}

func TestWatcherOneFailedCardDoesNotBlockOtherAlarms(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	bad := watcherLoopFixture(t, s, ctx, now)
	good := watcherLoopFixture(t, s, ctx, now)
	_, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_one_alarm BEFORE INSERT ON events WHEN NEW.type='message_posted' AND json_extract(NEW.refs_json,'$[0]')='card:`+bad+`' BEGIN SELECT RAISE(ABORT,'one alarm failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MaintainLoopHealth(ctx, now.Add(6*time.Minute), DefaultWatcherHealthConfig()); err == nil {
		t.Fatal("expected one card failure")
	}
	var level string
	if err = s.db.QueryRowContext(ctx, `SELECT level FROM watcher_health WHERE card_id=?`, good).Scan(&level); err != nil || level != "decide" {
		t.Fatal("unrelated alarm starved", level, err)
	}
}
