package pm

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPMCardViewsKeepPinnedRequesterAndLeaseScope(t *testing.T) {
	s, _, human, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "cards", Title: "Cards", ContextRefs: []string{"card:one", "card:two"}})
	if err != nil {
		t.Fatal(err)
	}
	turn, _ := s.PostMessage(ctx, human, c.ID, MessageInput{RequestKey: "m", Text: "Decision?"})
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	claim, _ := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "runner"})
	calls := 0
	s.deps.ReadPinnedCards = func(_ context.Context, p Principal, refs []string, full bool) (ContextPage, error) {
		calls++
		if p.ActorID != human.ActorID {
			t.Fatal("ambient PM authority")
		}
		want := []string{"card:one", "card:two"}
		if full {
			want = refs
			if len(refs) != 1 {
				t.Fatal(refs)
			}
		}
		if !reflect.DeepEqual(refs, want) {
			t.Fatal(refs)
		}
		return ContextPage{Items: []any{}}, nil
	}
	for _, tc := range []struct {
		ref, view, token string
		want             error
	}{{"", "cards", claim.LeaseToken, nil}, {"card:two", "card", claim.LeaseToken, nil}, {"card:other", "card", claim.LeaseToken, nil}, {"", "card", claim.LeaseToken, ErrInvalid}, {"", "cards", "wrong", ErrConflict}, {"", "unknown", claim.LeaseToken, ErrInvalid}} {
		_, err := s.getTurnContextView(ctx, agent, turn.ID, tc.ref, "", "", 8, tc.token, tc.view)
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	for _, ref := range []string{"card:", "card:" + strings.Repeat("x", 513), "card:x\n", "card:\xff", "document:x"} {
		if _, err := s.getTurnContextView(ctx, agent, turn.ID, ref, "", "", 8, claim.LeaseToken, "card"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid ref %q: %v", ref, err)
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}
func TestPMCardViewWithoutPinsUsesRequesterOverview(t *testing.T) {
	s, _, human, _ := fixture(t)
	ctx := context.Background()
	c, _ := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "empty", Title: "Empty"})
	turn, _ := s.PostMessage(ctx, human, c.ID, MessageInput{RequestKey: "m", Text: "Decision?"})
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	claim, _ := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "runner"})
	s.deps.ReadWorkspaceOverview = func(_ context.Context, p Principal) (ContextPage, error) {
		if p.ActorID != human.ActorID {
			t.Fatal(p)
		}
		return ContextPage{Items: []any{}}, nil
	}
	s.deps.ReadPinnedCards = func(context.Context, Principal, []string, bool) (ContextPage, error) {
		t.Fatal("pinned reader called")
		return ContextPage{}, nil
	}
	s.deps.ReadContextPage = func(context.Context, Principal, string, string, string, int) (ContextPage, error) {
		t.Fatal("workspace listing called")
		return ContextPage{}, nil
	}
	page, err := s.getTurnContextView(ctx, agent, turn.ID, "", "", "", 8, claim.LeaseToken, "cards")
	if err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
}
func TestCardNotePayloadIsStructured(t *testing.T) {
	for _, p := range []*ActionPayload{nil, {Note: " "}, {Phase: "ready", Note: "x"}, {Note: "x", ResolutionRefs: []string{"event:x"}}} {
		if validActionPayload("work.note", p) {
			t.Fatal(p)
		}
	}
	if !validActionPayload("work.note", &ActionPayload{Note: "Context for review"}) {
		t.Fatal("valid note rejected")
	}
}
