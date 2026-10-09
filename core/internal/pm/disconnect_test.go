package pm

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDisconnectResetsOnboardingSettlesTurnsAndSurvivesRestart(t *testing.T) {
	s, st, human, _ := fixture(t)
	ctx := context.Background()
	principal := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	if _, err := s.Connect(ctx, principal, ConnectionInput{Runner: "custom", Host: "computer"}); err != nil {
		t.Fatal(err)
	}
	decision, err := s.ProposeDecision(ctx, human, DecisionInput{RequestKey: "existing", WorkRef: "card:one", Instruction: `{"next_action":"Review"}`, Scope: "work.annotate", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	s.deps.Dispatch = nil
	conversation, err := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "uninstall", Title: "Pending question"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, human, conversation.ID, MessageInput{RequestKey: "pending", Text: "Please help"})
	if err != nil {
		t.Fatal(err)
	}
	// Ordinary silence preserves the expectation that the PM will return.
	if _, err = st.db.Exec(`UPDATE pm_presence SET last_seen_at=?`, time.Now().Add(-2*time.Minute).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	state, err := s.Presence(ctx, human)
	if err != nil || state.State != "offline" {
		t.Fatalf("quiet PM %+v %v", state, err)
	}
	for _, unauthorized := range []Principal{human, {WorkspaceID: "ws", ActorID: "stranger"}, {WorkspaceID: "other", ActorID: "pm-agent"}} {
		if _, err = s.Disconnect(ctx, unauthorized); !errors.Is(err, ErrForbidden) {
			t.Fatalf("unauthorized reset: %v", err)
		}
	}
	if _, err = st.db.Exec(`INSERT INTO pm_presence(workspace_id,actor_id,last_seen_at,signal,first_seen_at) VALUES('ws','previous-pm',?,'connect',?)`, time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		state, err = s.Disconnect(ctx, principal)
		if err != nil || state.State != "not_onboarded" || state.LastSeen != nil || state.Runner != nil || state.Host != nil || state.Connected {
			t.Fatalf("reset %d %+v %v", i, state, err)
		}
	}
	if _, err = s.decision(ctx, human, decision.ID, "pm.read"); err != nil {
		t.Fatalf("existing decision hidden %v", err)
	}
	if _, err = s.AnswerDecision(ctx, human, decision.ID, AnswerInput{Revision: 1, Text: "Declined"}); err != nil {
		t.Fatalf("existing decision stranded %v", err)
	}
	var priorFirst, priorSignal string
	if err = st.db.QueryRow(`SELECT first_seen_at,signal FROM pm_presence WHERE workspace_id='ws' AND actor_id='previous-pm'`).Scan(&priorFirst, &priorSignal); err != nil || priorFirst != "" || priorSignal != "disconnect" {
		t.Fatalf("previous selection can resurrect %q %q %v", priorFirst, priorSignal, err)
	}
	var failed Turn
	if err = st.get(ctx, "turn", turn.ID, &failed); err != nil {
		t.Fatal(err)
	}
	if failed.Status != Failed || failed.FailureKind != "pm_not_onboarded" || failed.Revision != turn.Revision+1 {
		t.Fatalf("unfinished work not settled %+v", failed)
	}
	// A stale claim/heartbeat must not revive registration after uninstall.
	if err = s.notePresence(ctx, principal, "claim"); err != nil {
		t.Fatal(err)
	}
	if err = s.notePresence(ctx, principal, "heartbeat"); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(st.db)
	if err != nil {
		t.Fatal(err)
	}
	s, err = NewService(reopened, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.Presence(ctx, human)
	if err != nil || state.State != "not_onboarded" {
		t.Fatalf("restart revived registration %+v %v", state, err)
	}
	if _, err = s.PostMessage(ctx, human, conversation.ID, MessageInput{RequestKey: "new", Text: "New question"}); !errors.Is(err, ErrNotOnboarded) {
		t.Fatalf("new ask %v", err)
	}
	// Admission rejects a turn even if its caller passed the gate before reset.
	turn.ID = "raced-turn"
	if _, err = st.insertTurn(ctx, turn, 10); !errors.Is(err, ErrNotOnboarded) {
		t.Fatalf("raced admission %v", err)
	}
	state, err = s.Connect(ctx, principal, ConnectionInput{Runner: "custom", Host: "new computer"})
	if err != nil || state.State != "connected" {
		t.Fatalf("explicit reconnect %+v %v", state, err)
	}
}
