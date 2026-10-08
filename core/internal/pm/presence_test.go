package pm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPresenceAcceptedClaimsScopeAndExpiry(t *testing.T) {
	s, st, human, _ := fixture(t)
	ctx := context.Background()
	if _, err := st.db.Exec(`DELETE FROM pm_presence`); err != nil {
		t.Fatal(err)
	}
	absent, err := s.Presence(ctx, human)
	if err != nil || absent.Connected || !absent.Configured {
		t.Fatalf("initial: %+v %v", absent, err)
	}
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	_, err = s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "local"})
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty claim: %v", err)
	}
	seen, err := s.Presence(ctx, human)
	if err != nil || !seen.Connected || seen.Signal != "claim" {
		t.Fatalf("accepted idle poll: %+v %v", seen, err)
	}
	for _, p := range []Principal{{WorkspaceID: "elsewhere", ActorID: "human"}, {WorkspaceID: "ws", ActorID: "other"}} {
		if _, err = s.Presence(ctx, p); !errors.Is(err, ErrForbidden) {
			t.Fatalf("scope: %v", err)
		}
	}
	if _, err = s.ClaimTurn(ctx, human, ClaimInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("spoof: %v", err)
	}
	old := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err = st.db.Exec(`UPDATE pm_presence SET last_seen_at=?`, old); err != nil {
		t.Fatal(err)
	}
	stale, err := s.Presence(ctx, human)
	if err != nil || stale.Connected {
		t.Fatalf("stale: %+v %v", stale, err)
	}
	s.cfg.AgentActorID = "replacement"
	replacement, err := s.Presence(ctx, human)
	if err != nil || replacement.Connected || replacement.LastSeenAt != "" {
		t.Fatalf("rebinding: %+v %v", replacement, err)
	}
	rows, err := st.db.Query(`EXPLAIN QUERY PLAN SELECT last_seen_at,signal FROM pm_presence WHERE workspace_id=? AND actor_id=?`, "ws", "pm-agent")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var a, b, c int
		var detail string
		if err = rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(detail, "SEARCH") || !strings.Contains(detail, "INDEX") {
			t.Fatalf("unbounded plan: %s", detail)
		}
	}
}

func TestPresenceProjectionFailureDoesNotChangeAcceptedLease(t *testing.T) {
	s, st, human, _ := fixture(t)
	ctx := context.Background()
	// Fail just the projection, keeping canonical turn storage healthy.
	if _, err := st.db.Exec(`CREATE TRIGGER fail_presence BEFORE INSERT ON pm_presence BEGIN SELECT RAISE(FAIL,'synthetic projection failure'); END`); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "projection-c", Title: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PostMessage(ctx, human, c.ID, MessageInput{RequestKey: "projection-m", Text: "test"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	claimed, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "local"})
	if err != nil || claimed.LeaseToken == "" {
		t.Fatalf("accepted claim lost: %+v %v", claimed, err)
	}
	renewed, err := s.HeartbeatTurn(ctx, agent, claimed.ID, HeartbeatInput{LeaseToken: claimed.LeaseToken})
	if err != nil || renewed.LeaseToken != claimed.LeaseToken {
		t.Fatalf("accepted heartbeat lost: %+v %v", renewed, err)
	}
}
