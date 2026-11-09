package pm

import (
	"context"
	"testing"
	"time"
)

func TestWatcherPMDebounceDeadlineRecoveryAndDisconnect(t *testing.T) {
	s, st, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	if _, err := s.Connect(ctx, agent, ConnectionInput{Runner: "runner", Host: "host"}); err != nil {
		t.Fatal(err)
	}
	check := func(at time.Time, want bool) {
		t.Helper()
		got, err := s.WatcherUnhealthy(ctx, at, time.Hour)
		if err != nil || got != want {
			t.Fatalf("unhealthy=%v want=%v err=%v", got, want, err)
		}
	}
	if _, err := st.database().ExecContext(ctx, `UPDATE pm_presence SET first_seen_at=? WHERE workspace_id=?`, now.Add(-5*time.Minute).Format(time.RFC3339Nano), "ws"); err != nil {
		t.Fatal(err)
	}
	check(now.Add(10*time.Minute), false)
	check(now.Add(time.Hour+90*time.Second+time.Second), true)
	// Deadline expiry is visible even when the normal expiry job did the write.
	deadline := now.Add(-time.Minute)
	turn := Turn{ID: "late", WorkspaceID: "ws", ActorID: "private-owner", AgentActorID: "pm-agent", Status: Pending, CreatedAt: now.Add(-2 * time.Minute), Deadline: deadline, Revision: 1}
	if _, err := st.insert(ctx, "turn", turn.ID, "ws", turn.ActorID, "", turn); err != nil {
		t.Fatal(err)
	}
	if err := s.ExpireTurns(ctx, now); err != nil {
		t.Fatal(err)
	}
	check(now, true)
	// Idle claims/presence cannot claim that unanswered work recovered.
	if err := s.notePresence(ctx, agent, "claim"); err != nil {
		t.Fatal(err)
	}
	check(now, true)
	turn.ID = "recovered"
	turn.Status = Delivered
	turn.Deadline = now.Add(time.Minute)
	turn.CreatedAt = now
	if _, err := st.insert(ctx, "turn", turn.ID, "ws", turn.ActorID, "", turn); err != nil {
		t.Fatal(err)
	}
	check(now, false)
	if _, err := s.Disconnect(ctx, agent); err != nil {
		t.Fatal(err)
	}
	check(now.Add(24*time.Hour), false)
}

func TestWatcherPMUnregisteredDoesNotAlarm(t *testing.T) {
	s, _, _, _ := fixture(t)
	got, err := s.WatcherUnhealthy(context.Background(), time.Now().Add(24*time.Hour), time.Hour)
	if err != nil || got {
		t.Fatal(got, err)
	}
}
