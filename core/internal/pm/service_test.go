package pm

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func fixture(t *testing.T) (*Service, *Store, Principal, *int) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "pm.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	st, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	deps := Dependencies{
		Authorize: func(_ context.Context, p Principal, permission, ref string) error {
			if p.ActorID == "other" {
				return ErrForbidden
			}
			return nil
		},
		EnsureThread: func(_ context.Context, p Principal, id, ref string) (string, error) { return "thread-" + id, nil },
		ReadContext: func(context.Context, Principal, string, string, int) (ContextPage, error) {
			return ContextPage{Items: []any{"current evidence"}}, nil
		},
		Dispatch:        func(context.Context, DispatchRequest) error { count++; return nil },
		CurrentRevision: func(context.Context, Principal, string) (string, error) { return "r1", nil },
		Execute: func(context.Context, Action) (Receipt, error) {
			return Receipt{Status: Delivered, ExternalID: "remote-1"}, nil
		},
	}
	s, err := NewService(st, Config{WorkspaceID: "ws", AgentActorID: "pm-agent", AgentHandle: "pm", TurnTimeout: time.Minute, MaxOutputBytes: 8000, MaxConcurrent: 2}, deps)
	if err != nil {
		t.Fatal(err)
	}
	return s, st, Principal{WorkspaceID: "ws", ActorID: "human", Human: true}, &count
}

func TestDiscussionIsNotApprovalAndReplayIsStable(t *testing.T) {
	s, _, p, count := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c1", Title: "Release", WorkRef: "work:1"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m1", Text: "Maybe deploy it?"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m1", Text: "Maybe deploy it?"})
	if err != nil || again.ID != turn.ID || *count != 1 {
		t.Fatalf("replay %v %d", err, *count)
	}
	if _, err = s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m1", Text: "different"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	ds, err := s.ListDecisions(ctx, p)
	if err != nil || len(ds) != 0 {
		t.Fatalf("discussion created decision: %v", ds)
	}
	if _, err = s.GetConversation(ctx, Principal{WorkspaceID: "ws", ActorID: "other"}, c.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross actor: %v", err)
	}
}

func TestDecisionAnswerRevisionAndUnknownDelivery(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "d1", WorkRef: "work:1", Instruction: "Assign owner", Scope: "assignment", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DispatchDecision(ctx, p, d.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("unapproved dispatch %v", err)
	}
	agent := p
	agent.Human = false
	if _, err = s.AnswerDecision(ctx, agent, d.ID, AnswerInput{Revision: 1, Approve: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent approval %v", err)
	}
	d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "Yes, assign owner"})
	if err != nil {
		t.Fatal(err)
	}
	s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "r2", nil }
	if _, err = s.DispatchDecision(ctx, p, d.ID); !errors.Is(err, ErrStale) {
		t.Fatalf("stale approval %v", err)
	}
	s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "r1", nil }
	calls := 0
	s.deps.Execute = func(context.Context, Action) (Receipt, error) { calls++; return Receipt{}, errors.New("lost response") }
	a, err := s.DispatchDecision(ctx, p, d.ID)
	if err != nil || a.Status != Unknown {
		t.Fatalf("unknown: %+v %v", a, err)
	}
	// Recreate service on the same durable database to prove restart safety.
	restarted, err := NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	a, err = restarted.DispatchDecision(ctx, p, d.ID)
	if err != nil || a.Status != Unknown || calls != 1 {
		t.Fatalf("resent after restart: %v %d", err, calls)
	}
}

func TestSourceReportedResolutionIsNotVerification(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	d, _ := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "d", WorkRef: "work:1", Instruction: "Change priority", Scope: "priority", TargetRevision: "r1"})
	if _, err := s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "Change priority"}); err != nil {
		t.Fatal(err)
	}
	a, err := s.DispatchDecision(ctx, p, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
		return Receipt{Status: Verified, ExternalID: "remote", EvidenceRefs: []string{"source says done"}}, nil
	}
	if _, err = s.ReconcileAction(ctx, p, a.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unverified source accepted: %v", err)
	}
	s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
		return Receipt{Status: Verified, ExternalID: "remote", EvidenceRefs: []string{"artifact:readback"}, IndependentlyVerified: true}, nil
	}
	a, err = s.ReconcileAction(ctx, p, a.ID)
	if err != nil || a.Status != Verified {
		t.Fatalf("readback: %v %+v", err, a)
	}
}

func TestOneActiveTurnPerConversationAcrossServices(t *testing.T) {
	s, st, p, count := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "concurrent", Title: "One session"})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := NewService(st, s.cfg, s.deps)
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "1", Text: "First"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = second.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "2", Text: "Second"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("overlapping session: %v", err)
	}
	if *count != 1 {
		t.Fatalf("overlap dispatched %d", *count)
	}
	if _, err = s.CompleteTurn(ctx, Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, turn.ID, "First reply", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = second.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "2", Text: "Second"}); err != nil {
		t.Fatal(err)
	}
}

func TestApprovalReplayRetainsOneActionIntent(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "approval-replay", WorkRef: "work:1", Instruction: "Assign owner", Scope: "assignment", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	answer := AnswerInput{Revision: d.Revision, Approve: true, Text: "Assign owner"}
	first, err := s.AnswerDecision(ctx, p, d.ID, answer)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AnswerDecision(ctx, p, d.ID, answer)
	if err != nil || first.ActionID != second.ActionID {
		t.Fatalf("approval replay %v", err)
	}
	actions, err := s.ListActions(ctx, p)
	if err != nil || len(actions) != 1 {
		t.Fatalf("duplicate intent %v %+v", err, actions)
	}
	answer.Text = "Different instruction"
	if _, err = s.AnswerDecision(ctx, p, d.ID, answer); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed answer replay %v", err)
	}
}

func TestAgentProposalHumanDiscoveryAndAgentReceiptDiscovery(t *testing.T) {
	s, _, human, _ := fixture(t)
	ctx := context.Background()
	agent := Principal{WorkspaceID: "ws", ActorID: "worker"}
	d, err := s.ProposeDecision(ctx, agent, DecisionInput{RequestKey: "agent-proposal", WorkRef: "work:1", Instruction: "Assign owner", Scope: "assignment", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	ds, err := s.ListDecisions(ctx, human)
	if err != nil || len(ds) != 1 || ds[0].ID != d.ID {
		t.Fatalf("human cannot discover authorized agent decision: %+v %v", ds, err)
	}
	page, err := s.DecisionPage(ctx, human, 50, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("paged human discovery: %+v %v", page, err)
	}
	answered, err := s.AnswerDecision(ctx, human, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "Assign owner"})
	if err != nil {
		t.Fatal(err)
	}
	as, err := s.ListActions(ctx, agent)
	if err != nil || len(as) != 1 || as[0].ID != answered.ActionID {
		t.Fatalf("agent cannot discover handoff receipt: %+v %v", as, err)
	}
	ap, err := s.ActionPage(ctx, agent, 50, "")
	if err != nil || len(ap.Items) != 1 {
		t.Fatalf("paged receipt discovery: %+v %v", ap, err)
	}
}

func TestContextCannotExceedRequestedBound(t *testing.T) {
	s, _, p, _ := fixture(t)
	s.deps.ReadContext = func(context.Context, Principal, string, string, int) (ContextPage, error) {
		return ContextPage{Items: []any{"one", "two"}}, nil
	}
	if _, err := s.QueryContext(context.Background(), p, "work:1", "", 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbounded context accepted %v", err)
	}
}

func TestContextPaginationUsesRequestingPrincipalAndBounds(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	s.deps.ReadContextPage = func(_ context.Context, got Principal, work, query, cursor string, limit int) (ContextPage, error) {
		if got.ActorID != p.ActorID || cursor != "next" || limit != 2 {
			t.Fatalf("context scope changed %+v %q %d", got, cursor, limit)
		}
		return ContextPage{Items: []any{"evidence"}}, nil
	}
	page, err := s.QueryContextPage(ctx, p, "work:1", "", "next", 2)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("context page %+v %v", page, err)
	}
}

func TestQueuedTurnWithoutDispatchIsClaimedByLease(t *testing.T) {
	s, _, p, count := fixture(t)
	s.deps.Dispatch = nil
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "queued", Title: "Queue"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m1", Text: "What needs my decision?"})
	if err != nil || turn.Status != Sending || *count != 0 {
		t.Fatalf("queued turn %+v %v count=%d", turn, err, *count)
	}
	human := p
	if _, err = s.ClaimTurn(ctx, human, ClaimInput{RunnerID: "runner-a"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("human claimed PM turn: %v", err)
	}
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	first, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "runner-a"})
	if err != nil || first.ID != turn.ID || first.LeaseToken == "" {
		t.Fatalf("claim %+v %v", first, err)
	}
	if _, err = s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "runner-b"}); !errors.Is(err, ErrEmpty) {
		t.Fatalf("second runner claimed leased turn: %v", err)
	}
	again, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "runner-a"})
	if err != nil || again.ID != first.ID || again.LeaseToken != first.LeaseToken {
		t.Fatalf("idempotent claim %+v %v", again, err)
	}
	if _, err = s.CompleteTurn(ctx, agent, first.ID, "Needs a decision on restock.", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("complete without lease token: %v", err)
	}
	done, err := s.CompleteTurnWithLease(ctx, agent, first.ID, "Needs a decision on restock.", []string{"card:emergency-restock"}, first.LeaseToken)
	if err != nil || done.Status != Delivered || done.Response == "" {
		t.Fatalf("complete %+v %v", done, err)
	}
}

func TestFailTurnRecordsReasonAndFreesSession(t *testing.T) {
	s, _, p, _ := fixture(t)
	s.deps.Dispatch = nil
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "fail", Title: "Fail"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "boom", Text: "Ping"})
	if err != nil || turn.Status != Sending {
		t.Fatalf("queued turn %+v %v", turn, err)
	}
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	claimed, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "runner-a"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.FailTurn(ctx, agent, claimed.ID, FailInput{Reason: "harness timeout", LeaseToken: claimed.LeaseToken})
	if err != nil || failed.Status != Failed || failed.Failure != "harness timeout" {
		t.Fatalf("fail %+v %v", failed, err)
	}
	replay, err := s.FailTurn(ctx, agent, claimed.ID, FailInput{Reason: "harness timeout", LeaseToken: claimed.LeaseToken})
	if err != nil || replay.ID != failed.ID {
		t.Fatalf("fail replay %v", err)
	}
	if _, err = s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "next", Text: "Retry"}); err != nil {
		t.Fatalf("session stayed occupied after fail: %v", err)
	}
}
