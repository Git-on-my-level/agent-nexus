package pm

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestPinnedConversationRefsPersistAndFenceReplay(t *testing.T) {
	s, _, human, _ := fixture(t)
	ctx := context.Background()
	in := CreateConversation{RequestKey: "context", Title: "Question", WorkRef: "card:release", ContextRefs: []string{"topic:project", "card:release"}}
	c, err := s.CreateConversation(ctx, human, in)
	if err != nil || len(c.ContextRefs) != 2 {
		t.Fatalf("conversation %+v %v", c, err)
	}
	replay, err := s.CreateConversation(ctx, human, in)
	if err != nil || replay.ID != c.ID {
		t.Fatalf("replay %v", err)
	}
	in.ContextRefs = []string{"topic:other"}
	if _, err = s.CreateConversation(ctx, human, in); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed context %v", err)
	}
	turn, err := s.PostMessage(ctx, human, c.ID, MessageInput{RequestKey: "ask", Text: "What is this?"})
	if err != nil || len(turn.ContextRefs) != 2 {
		t.Fatalf("turn context %+v %v", turn, err)
	}
	in.RequestKey = "too-many"
	in.ContextRefs = nil
	for i := 0; i < 9; i++ {
		in.ContextRefs = append(in.ContextRefs, fmt.Sprintf("card:%d", i))
	}
	if _, err = s.CreateConversation(ctx, human, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bound %v", err)
	}
}

func TestActivityLeaseBoundsRetriesAndDraftOrdering(t *testing.T) {
	s, _, human, _ := fixture(t)
	ctx := context.Background()
	c, _ := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "c", Title: "Question"})
	turn, _ := s.PostMessage(ctx, human, c.ID, MessageInput{RequestKey: "m", Text: "Explain"})
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	claimed, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "worker"})
	if err != nil || claimed.ID != turn.ID {
		t.Fatalf("claim %v", err)
	}
	draft := "The task is awaiting review."
	in := HeartbeatInput{LeaseToken: claimed.LeaseToken, Activity: []TurnActivity{{Sequence: 1, Kind: "tool", Label: "Reading the task", Target: "card:release"}}, PartialResponse: &draft, PartialSequence: 1}
	if _, err = s.HeartbeatTurn(ctx, human, turn.ID, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("human write %v", err)
	}
	in.LeaseToken = "stale"
	if _, err = s.HeartbeatTurn(ctx, agent, turn.ID, in); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("stale lease %v", err)
	}
	in.LeaseToken = claimed.LeaseToken
	result, err := s.HeartbeatTurn(ctx, agent, turn.ID, in)
	if err != nil || len(result.Activity) != 1 || result.Activity[0].RecordedAt.IsZero() || result.PartialResponse != draft {
		t.Fatalf("heartbeat %+v %v", result, err)
	}
	replay, err := s.HeartbeatTurn(ctx, agent, turn.ID, in)
	if err != nil || len(replay.Activity) != 1 || replay.Activity[0].RecordedAt != result.Activity[0].RecordedAt {
		t.Fatalf("duplicate %+v %v", replay, err)
	}
	in.Activity[0].Label = "Different"
	if _, err = s.HeartbeatTurn(ctx, agent, turn.ID, in); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting sequence %v", err)
	}
	in.Activity = nil
	draft = "new draft"
	in.PartialSequence = 2
	if _, err = s.HeartbeatTurn(ctx, agent, turn.ID, in); err != nil {
		t.Fatal(err)
	}
	in.PartialSequence = 1
	if _, err = s.HeartbeatTurn(ctx, agent, turn.ID, in); !errors.Is(err, ErrConflict) {
		t.Fatalf("old draft %v", err)
	}
	in.PartialResponse = nil
	for i := 2; i <= 60; i++ {
		in.Activity = []TurnActivity{{Sequence: i, Kind: "status", Label: "Checking evidence"}}
		if _, err = s.HeartbeatTurn(ctx, agent, turn.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	result, err = s.GetTurn(ctx, human, turn.ID)
	if err != nil || len(result.Activity) != 50 || result.Activity[0].Sequence != 11 {
		t.Fatalf("retention %+v %v", result, err)
	}
	in.Activity = []TurnActivity{{Sequence: 61, Kind: "tool", Label: "bad\nlabel"}}
	if _, err = s.HeartbeatTurn(ctx, agent, turn.ID, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsafe label %v", err)
	}
	in.Activity = nil
	if _, err = s.CompleteTurnWithLease(ctx, agent, turn.ID, "Final answer", nil, claimed.LeaseToken); err != nil {
		t.Fatal(err)
	}
	if _, err = s.HeartbeatTurn(ctx, agent, turn.ID, in); !errors.Is(err, ErrTurnClosed) {
		t.Fatalf("closed write %v", err)
	}
}

func TestTurnContextUsesPinnedSelectorAndRequestingPrincipal(t *testing.T) {
	s, _, human, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "refs", Title: "Project question", ContextRefs: []string{"topic:project", "card:release"}})
	if err != nil {
		t.Fatal(err)
	}
	turn, _ := s.PostMessage(ctx, human, c.ID, MessageInput{RequestKey: "m", Text: "What is this?"})
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	claimed, _ := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "worker"})
	var gotRef string
	s.deps.ReadContext = func(_ context.Context, p Principal, ref, query string, limit int) (ContextPage, error) {
		if p.ActorID != human.ActorID || limit != 10 {
			t.Fatalf("ambient scope or unbounded limit %+v %d", p, limit)
		}
		gotRef = ref
		return ContextPage{Items: []any{ref}}, nil
	}
	if _, err = s.GetTurnPinnedContextPage(ctx, agent, turn.ID, "", "", "", 10, claimed.LeaseToken); err != nil || gotRef != "topic:project" {
		t.Fatalf("default pin %s %v", gotRef, err)
	}
	if _, err = s.GetTurnPinnedContextPage(ctx, agent, turn.ID, "card:release", "", "", 10, claimed.LeaseToken); err != nil || gotRef != "card:release" {
		t.Fatalf("selected pin %s %v", gotRef, err)
	}
	if _, err = s.GetTurnPinnedContextPage(ctx, agent, turn.ID, "card:unrelated", "", "", 10, claimed.LeaseToken); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unrelated selector %v", err)
	}
}
