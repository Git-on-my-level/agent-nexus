package primitives

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWatcherPMPreclaimedLegacyIDsCannotSuppressAlarm(t *testing.T) {
	s, ctx := attentionFixture(t)
	attacker := WithAccessScope(ctx, AccessScope{ActorID: "placer"})
	thread, err := s.CreateThread(attacker, "placer", map[string]any{"id": watcherID("pm-thread"), "title": "Unrelated work", "pm_actor_id": "placer"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"pm:0:0:ask", "pm:0:0:reminder", "pm:0:1:reminder"} {
		if _, err = s.AppendEvent(attacker, "placer", map[string]any{"id": watcherID(key), "type": "message_posted", "refs": []string{}, "thread_id": thread.Thread["id"], "payload": map[string]any{"text": "My ordinary event"}}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	cfg := DefaultWatcherHealthConfig()
	for _, at := range []time.Time{now, now, now.Add(time.Hour), now.Add(time.Hour)} {
		if err = s.ReconcilePMHealth(ctx, true, at, cfg); err != nil {
			t.Fatal(err)
		}
	}
	if n := watcherCount(t, s, "human_attention_requested"); n != 1 {
		t.Fatal("preclaimed IDs suppressed or duplicated alarm", n)
	}
	var reminders int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type='message_posted' AND json_extract(payload_json,'$.payload.source')='watcher_health'`).Scan(&reminders); err != nil || reminders != 2 {
		t.Fatal("preclaimed IDs suppressed or duplicated reminders", reminders, err)
	}
	var ask, healthThread string
	if err = s.db.QueryRowContext(ctx, `SELECT ask_id,thread_id FROM watcher_health WHERE alarm_key='pm'`).Scan(&ask, &healthThread); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{ask, healthThread} {
		u, err := uuid.Parse(id)
		if err != nil || u.Version() != 4 {
			t.Fatal("alarm identity must be unpredictable", id, err)
		}
	}
	// A learned ask ID must not expose a predictable recovery-event ID either.
	if _, err = s.AppendEvent(attacker, "placer", map[string]any{"id": watcherID(ask + ":recovered"), "type": "message_posted", "refs": []string{}, "thread_id": thread.Thread["id"], "payload": map[string]any{"text": "Another ordinary event"}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = s.ReconcilePMHealth(ctx, false, now.Add(2*time.Hour), cfg); err != nil {
			t.Fatal(err)
		}
	}
	if n := watcherCount(t, s, "human_attention_withdrawn"); n != 1 {
		t.Fatal("recovery suppressed or duplicated", n)
	}
}

func TestWatcherPMAlarmHasReadableHumanDecideAudience(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	cfg := DefaultWatcherHealthConfig()
	if err := s.ReconcilePMHealth(ctx, true, now, cfg); err != nil {
		t.Fatal(err)
	}
	var ask, thread, recipient, owner, handle string
	if err := s.db.QueryRowContext(ctx, `SELECT a.ask_id,a.thread_id,a.recipient,json_extract(t.body_json,'$.pm_actor_id'),e.handle FROM watcher_health a JOIN threads t ON t.id=a.thread_id JOIN events e ON e.id=a.ask_id WHERE alarm_key='pm'`).Scan(&ask, &thread, &recipient, &owner, &handle); err != nil || recipient != "human" || owner != recipient {
		t.Fatal("PM alarm lacks human ownership", recipient, owner, err)
	}
	items := attentionAll(t, s, ctx, recipient, "human", "decide")
	if len(items) != 1 || items[0].SubjectRef != "event:"+handle {
		t.Fatal("PM alarm absent from human Decide", items)
	}
	for _, actor := range []string{"human", "other", "placer"} {
		_, err := s.GetThread(WithAccessScope(ctx, AccessScope{ActorID: actor}), thread)
		if actor == recipient && err != nil || actor != recipient && !errors.Is(err, ErrNotFound) {
			t.Fatal("wrong PM thread audience", actor, err)
		}
	}
	// Revoked recipients cannot strand the alarm; choose the next human and
	// create a thread they own without granting access to the old conversation.
	if _, err := s.db.ExecContext(ctx, `UPDATE agents SET revoked_at=? WHERE actor_id=?`, loopTS(now), recipient); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcilePMHealth(ctx, true, now.Add(time.Minute), cfg); err != nil {
		t.Fatal(err)
	}
	if got := attentionAll(t, s, ctx, "other", "human", "decide"); len(got) != 1 {
		t.Fatal("replacement human missing alarm", got)
	}
	if got := attentionAll(t, s, ctx, "human", "human", "decide"); len(got) != 0 {
		t.Fatal("old ask survived reroute", got)
	}
}
