package pm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecisionAnswerBoundToActor(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "actor", WorkRef: "work:1", Scope: "work.phase", Instruction: "Move", TargetRevision: "r1", Payload: &ActionPayload{Phase: "ready"}})
	if err != nil {
		t.Fatal(err)
	}
	second := Principal{WorkspaceID: p.WorkspaceID, ActorID: "second-human", Human: true}
	if _, err = s.AnswerDecision(ctx, second, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "yes"}); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	ds, err := s.ListDecisions(ctx, second)
	if err != nil || len(ds) != 1 {
		t.Fatalf("workspace read: %v %v", ds, err)
	}
	d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	var a Action
	if err = st.get(ctx, "action", d.ActionID, &a); err != nil {
		t.Fatal(err)
	}
	a.ActorID = second.ActorID
	if err = st.cas(ctx, "action", a.ID, a.Revision, a); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DispatchDecision(ctx, p, d.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("mismatched approver: %v", err)
	}
}
func TestPhaseApprovalRequiresStructuredTarget(t *testing.T) {
	for i, payload := range []*ActionPayload{nil, {Phase: "bogus"}, {Phase: "done"}} {
		s, _, p, _ := fixture(t)
		ctx := context.Background()
		d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: fmt.Sprint(i), WorkRef: "work:1", Scope: "work.phase", Instruction: "Move to ready", TargetRevision: "r1", Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "yes"}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("payload %+v: %v", payload, err)
		}
	}
}
func TestPMIdentityFailsClosed(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Context"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Question"})
	if err != nil {
		t.Fatal(err)
	}
	for _, configured := range []string{"", "replacement-pm"} {
		s.cfg.AgentActorID = configured
		actor := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
		calls := []func() error{
			func() error { _, e := s.ClaimTurn(ctx, actor, ClaimInput{}); return e },
			func() error { _, e := s.GetTurnContext(ctx, actor, turn.ID, "", 10); return e },
			func() error { _, e := s.ProposeForTurn(ctx, actor, turn.ID, DecisionInput{}); return e },
			func() error { _, e := s.CompleteTurn(ctx, actor, turn.ID, "reply", nil); return e },
			func() error { _, e := s.FailTurn(ctx, actor, turn.ID, FailInput{Reason: "failed"}); return e },
		}
		for i, call := range calls {
			err = call()
			if !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrUnavailable) {
				t.Fatalf("%q op %d: %v", configured, i, err)
			}
		}
	}
	s.cfg.AgentActorID = ""
	if _, err = s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "new", Text: "Question"}); !errors.Is(err, ErrPMIdentity) {
		t.Fatal(err)
	}
}
func TestTurnProposalDedupeAndLink(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
	var first Decision
	for i := 0; i < 2; i++ {
		c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: fmt.Sprint(i), Title: "Context"})
		if err != nil {
			t.Fatal(err)
		}
		turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Question"})
		if err != nil {
			t.Fatal(err)
		}
		d, err := s.ProposeForTurn(ctx, agent, turn.ID, DecisionInput{RequestKey: fmt.Sprint(i), WorkRef: "work:1", Scope: "work.phase", Instruction: "Move", TargetRevision: "r1", Payload: &ActionPayload{Phase: "ready"}})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = d
		} else if d.ID != first.ID {
			t.Fatalf("duplicate %s != %s", d.ID, first.ID)
		}
		var saved Turn
		if err = st.get(ctx, "turn", turn.ID, &saved); err != nil {
			t.Fatal(err)
		}
		if len(saved.DecisionIDs) != 1 || saved.DecisionIDs[0] != first.ID {
			t.Fatalf("missing decision link: %+v", saved)
		}
	}
	ds, err := s.ListDecisions(ctx, p)
	if err != nil || len(ds) != 1 {
		t.Fatalf("duplicates %v %v", ds, err)
	}
}

func TestUnpaginatedListsCrossMultiplePages(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	for i := 0; i < 405; i++ {
		id := fmt.Sprintf("%04d", i)
		values := map[string]any{
			"conversation": Conversation{ID: id, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID},
			"decision":     Decision{ID: id, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, Status: AwaitingAnswer},
			"action":       Action{ID: id, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, Status: Pending},
			"binding":      Binding{ID: id, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID},
		}
		for kind, value := range values {
			if _, err := st.insert(ctx, kind, id, p.WorkspaceID, p.ActorID, "", value); err != nil {
				t.Fatal(err)
			}
		}
	}
	cs, e := s.ListConversations(ctx, p)
	if e != nil || len(cs) != 405 {
		t.Fatalf("conversations %d %v", len(cs), e)
	}
	ds, e := s.ListDecisions(ctx, p)
	if e != nil || len(ds) != 405 || !ds[404].CanAnswer {
		t.Fatalf("decisions %d %v", len(ds), e)
	}
	as, e := s.ListActions(ctx, p)
	if e != nil || len(as) != 405 {
		t.Fatalf("actions %d %v", len(as), e)
	}
	bs, e := s.BindingPage(ctx, p)
	if e != nil || len(bs.Items) != 405 {
		t.Fatalf("bindings %d %v", len(bs.Items), e)
	}
	other := Principal{WorkspaceID: p.WorkspaceID, ActorID: "second-human", Human: true}
	page, e := s.DecisionPage(ctx, other, 200, "")
	if e != nil || page.Items[0].CanAnswer {
		t.Fatalf("another actor answerable: %+v %v", page, e)
	}
}
func TestClaimAllocatesNewWorkAndEnforcesCapacityAcrossServices(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	s.cfg.MaxConcurrent = 2
	for i := 0; i < 4; i++ {
		id := fmt.Sprint(i)
		turn := Turn{ID: id, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, AgentActorID: "pm-agent", Status: Sending, Deadline: time.Now().Add(time.Minute), Revision: 1}
		if _, err := st.insert(ctx, "turn", id, p.WorkspaceID, p.ActorID, id, turn); err != nil {
			t.Fatal(err)
		}
	}
	second, err := NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
	first, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "same"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := second.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "same"})
	if err != nil || first.ID == next.ID {
		t.Fatalf("did not allocate new work: %+v %v", next, err)
	}
	if _, err = second.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "third"}); !errors.Is(err, ErrEmpty) {
		t.Fatalf("exceeded cap: %v", err)
	}
	// Free one slot, then race two independent service instances for that slot.
	if _, err = s.CompleteTurnWithLease(ctx, agent, first.ID, "complete", nil, first.LeaseToken); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, svc := range []*Service{s, second} {
		go func(svc *Service) { _, err := svc.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "race"}); results <- err }(svc)
	}
	successes := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrEmpty) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("raced capacity: %d", successes)
	}
}
func TestReconcilePersistsContradictoryReadback(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	a := Action{ID: "reported", WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, Status: Reported, Revision: 1, Receipt: Receipt{Status: Reported, ExternalID: "source"}}
	if _, err := st.insert(ctx, "action", a.ID, p.WorkspaceID, p.ActorID, "", a); err != nil {
		t.Fatal(err)
	}
	s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
		return Receipt{Status: Unknown, Detail: "Canonical state disproves the report"}, nil
	}
	updated, err := s.ReconcileAction(ctx, p, a.ID)
	if err != nil || updated.Status != Reported || !updated.ReconciliationConflict || updated.Receipt.Detail == "" || updated.Receipt.Status != Unknown {
		t.Fatalf("lost contradiction: %+v %v", updated, err)
	}
	var persisted Action
	if err = st.get(ctx, "action", a.ID, &persisted); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted, updated) {
		t.Fatalf("not durable: %+v", persisted)
	}
	s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
		return Receipt{Status: Verified, ExternalID: "source", EvidenceRefs: []string{"work:1"}, IndependentlyVerified: true}, nil
	}
	updated, err = s.ReconcileAction(ctx, p, a.ID)
	if err != nil || updated.Status != Verified || updated.ReconciliationConflict {
		t.Fatalf("verification: %+v %v", updated, err)
	}
}

func TestConcurrentTurnProposalsReuseExactPendingTarget(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	second, err := NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
	ids := make([]string, 2)
	for i := range ids {
		c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: fmt.Sprint(i), Title: "Concurrent"})
		if err != nil {
			t.Fatal(err)
		}
		turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Question"})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = turn.ID
	}
	type result struct {
		d   Decision
		err error
	}
	results := make(chan result, 2)
	for i, svc := range []*Service{s, second} {
		go func(i int, svc *Service) {
			d, err := svc.ProposeForTurn(ctx, agent, ids[i], DecisionInput{RequestKey: fmt.Sprint(i), WorkRef: "work:1", Scope: "work.phase", Instruction: "Same target", TargetRevision: "r1", Payload: &ActionPayload{Phase: "ready"}})
			results <- result{d, err}
		}(i, svc)
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.d.ID != b.d.ID || !reflect.DeepEqual(a.d.Payload, b.d.Payload) || a.d.TargetRevision != b.d.TargetRevision {
		t.Fatalf("conflicting duplicates: %+v %+v", a, b)
	}
}

func TestDecisionCardDisplaysStructuredAuthorizationTarget(t *testing.T) {
	text := decisionCardText(Decision{ID: "decision", Revision: 1, WorkRef: "work:1", Scope: "work.phase", TargetRevision: "r1", Instruction: "Discuss next step", Payload: &ActionPayload{Phase: "review"}})
	for _, want := range []string{`"phase":"review"`, "Scope: work.phase", "Target revision: r1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("approval card hides target: %s", text)
		}
	}
}
