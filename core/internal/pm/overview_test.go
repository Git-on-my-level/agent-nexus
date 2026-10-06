package pm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestOverviewDecisionsFiltersOwnershipBeforeCandidateWindow(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	insert := func(d Decision) {
		t.Helper()
		if _, err := st.insert(ctx, "decision", d.ID, "ws", d.ActorID, "", d); err != nil {
			t.Fatal(err)
		}
	}
	insert(Decision{ID: "mine", ActorID: p.ActorID, WorkspaceID: "ws", WorkRef: "card:mine", Status: AwaitingAnswer, CreatedAt: time.Now()})
	insert(Decision{ID: "legacy-pending", ActorID: "someone", WorkspaceID: "ws", WorkRef: "card:mine", Status: Answered, CreatedAt: time.Now()})
	if _, err := st.insert(ctx, "action", "legacy", "ws", p.ActorID, "legacy-pending", Action{ID: "legacy", DecisionID: "legacy-pending", ActorID: p.ActorID, WorkspaceID: "ws", WorkRef: "card:mine", Status: Pending}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 150; i++ {
		insert(Decision{ID: fmt.Sprint("other-", i), ActorID: "someone", WorkspaceID: "ws", WorkRef: "card:other", Status: AwaitingAnswer, CreatedAt: time.Now()})
	}
	ds, actions, partial, err := s.OverviewDecisions(ctx, p)
	if err != nil || partial || len(ds) != 2 || len(actions["legacy-pending"]) != 1 {
		t.Fatalf("overview lost eligible decisions: %v %v %v %v", ds, actions, partial, err)
	}
}

func TestDecisionPageUsesOneProjectionBatch(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	for i := 0; i < 21; i++ {
		id := fmt.Sprintf("batch-%d", i)
		d := Decision{ID: id, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, WorkRef: "card:" + id, Status: AwaitingAnswer, CreatedAt: time.Now()}
		if _, err := st.insert(ctx, "decision", id, p.WorkspaceID, p.ActorID, "", d); err != nil {
			t.Fatal(err)
		}
	}
	s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
		t.Fatal("point projection was called for page")
		return DecisionWork{}, nil
	}
	calls := 0
	s.deps.DecisionWorkBatch = func(_ context.Context, _ Principal, refs []string) (map[string]DecisionWork, error) {
		calls++
		out := map[string]DecisionWork{}
		for _, ref := range refs {
			out[ref] = DecisionWork{Revision: "r1"}
		}
		return out, nil
	}
	page, err := s.DecisionPage(ctx, p, 20, "")
	if err != nil || calls != 1 || len(page.Items) != 20 || !page.HasMore {
		t.Fatalf("batch: %d %+v %v", calls, page, err)
	}
}
