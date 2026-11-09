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

func TestWatcherPMTimeoutReductionCannotHideNewExpiry(t *testing.T) {
	s, st, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	if _, err := s.Connect(ctx, agent, ConnectionInput{Runner: "runner", Host: "host"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.database().ExecContext(ctx, `UPDATE pm_presence SET first_seen_at=? WHERE workspace_id=?`, now.Add(-5*time.Minute).Format(time.RFC3339Nano), "ws"); err != nil {
		t.Fatal(err)
	}
	for _, turn := range []Turn{
		{ID: "old-long-timeout", WorkspaceID: "ws", ActorID: "human", AgentActorID: "pm-agent", Status: Delivered, CreatedAt: now.Add(-4 * time.Minute), Deadline: now.Add(5 * time.Minute), Revision: 1},
		{ID: "new-short-timeout", WorkspaceID: "ws", ActorID: "human", AgentActorID: "pm-agent", Status: Failed, FailureKind: "expired", CreatedAt: now.Add(-2 * time.Minute), Deadline: now.Add(-time.Minute), Revision: 1},
	} {
		if _, err := st.insert(ctx, "turn", turn.ID, "ws", "human", "", turn); err != nil {
			t.Fatal(err)
		}
	}
	unhealthy, err := s.WatcherUnhealthy(ctx, now, time.Hour)
	if err != nil || !unhealthy {
		t.Fatal("old success hid a newer unanswered turn", unhealthy, err)
	}
	if _, err = s.Disconnect(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Connect(ctx, agent, ConnectionInput{Runner: "runner", Host: "host"}); err != nil {
		t.Fatal(err)
	}
	unhealthy, err = s.WatcherUnhealthy(ctx, time.Now().UTC(), time.Hour)
	if err != nil || unhealthy {
		t.Fatal("reconnect revived a pre-disconnect expiry", unhealthy, err)
	}
}
