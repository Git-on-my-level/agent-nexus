package primitives

import (
	"testing"
	"time"
)

func TestWatcherPreclaimedLegacyIDsCannotSuppressAlarm(t *testing.T) {
	s, ctx := attentionFixture(t)
	now := time.Now().UTC()
	id := watcherLoopFixture(t, s, ctx, now)
	if _, err := s.db.ExecContext(ctx, `UPDATE cards SET assignee=? WHERE id=?`, "actor:human", id); err != nil {
		t.Fatal(err)
	}
	attacker := WithAccessScope(ctx, AccessScope{ActorID: "placer"})
	thread, err := s.CreateThread(attacker, "placer", map[string]any{"id": watcherID("pm-thread"), "title": "Unrelated work"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"loop:" + id + ":0:0:ask", "loop:" + id + ":0:0:reminder", "loop:" + id + ":0:1:reminder"} {
		if _, err = s.AppendEvent(attacker, "placer", map[string]any{"id": watcherID(key), "type": "message_posted", "refs": []string{}, "thread_id": thread.Thread["id"], "payload": map[string]any{"text": "My ordinary event"}}); err != nil {
			t.Fatal(err)
		}
	}
	watcherReceipt(t, s, ctx, id, now, "infra_error", true)
	cfg := DefaultWatcherHealthConfig()
	for _, at := range []time.Time{now.Add(2 * time.Hour), now.Add(2 * time.Hour), now.Add(3 * time.Hour), now.Add(3 * time.Hour)} {
		if err = s.MaintainLoopHealth(ctx, at, cfg); err != nil {
			t.Fatal(err)
		}
	}
	if n := watcherCount(t, s, "human_attention_requested"); n != 1 {
		t.Fatal("preclaimed IDs suppressed or duplicated alarm", n)
	}
	var reminders int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type='message_posted' AND json_extract(payload_json,'$.payload.source')='watcher_health'`).Scan(&reminders); err != nil || reminders < 1 {
		t.Fatal("preclaimed IDs suppressed reminders", reminders, err)
	}
	// A learned ask ID must not expose a predictable recovery-event ID either.
	if _, err = s.AppendEvent(attacker, "placer", map[string]any{"id": watcherID("loop:" + id + ":recovered"), "type": "message_posted", "refs": []string{}, "thread_id": thread.Thread["id"], "payload": map[string]any{"text": "Another ordinary event"}}); err != nil {
		t.Fatal(err)
	}
	watcherReceipt(t, s, ctx, id, now.Add(3*time.Hour), "measured", false)
	if err = s.MaintainLoopHealth(ctx, now.Add(3*time.Hour), cfg); err != nil {
		t.Fatal(err)
	}
	if n := watcherCount(t, s, "human_attention_withdrawn"); n != 1 {
		t.Fatal("recovery suppressed or duplicated", n)
	}
}
