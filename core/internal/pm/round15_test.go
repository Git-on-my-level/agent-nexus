package pm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRound15DispatchWorkReadFailure(t *testing.T) {
	for _, reader := range []string{"decision_work", "revision_fallback", "final_revision"} {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/missing=%t", reader, missing), func(t *testing.T) {
				s, st, p, _ := fixture(t)
				ctx := context.Background()
				d := round4Approved(t, s, p)
				before := round13Body(t, st, "action", d.ActionID)
				readErr := errors.New("temporary reader failure")
				reason := "work_read_failed"
				if missing {
					readErr, reason = fmt.Errorf("target: %w", ErrNotFound), "work_missing"
				}
				switch reader {
				case "decision_work":
					s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
						return DecisionWork{}, readErr
					}
				case "revision_fallback":
					s.deps.DecisionWork = nil
					s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "", readErr }
				case "final_revision":
					s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "", readErr }
				}
				s.deps.Execute = func(context.Context, Action) (Receipt, error) {
					t.Fatal("source called")
					return Receipt{}, nil
				}
				s.deps.Reconcile = s.deps.Execute
				_, err := s.DispatchDecision(ctx, p, d.ID)
				var target *ApprovalTargetError
				if !errors.As(err, &target) || target.Reason != reason {
					t.Fatal(err)
				}
				if !missing {
					if before != round13Body(t, st, "action", d.ActionID) {
						t.Fatal("transient read failed action")
					}
					// Final revision is only checked at dispatch; reconciliation
					// otherwise rejects this unsent action without changing it.
					_, _ = s.ReconcileAction(ctx, p, d.ActionID)
					if before != round13Body(t, st, "action", d.ActionID) {
						t.Fatal("reconcile mutated action")
					}
					return
				}
				var a Action
				if err := st.get(ctx, "action", d.ActionID, &a); err != nil {
					t.Fatal(err)
				}
				if a.Status != Failed || len(a.Attempts) != 1 || a.Attempts[0].SentAt != nil || a.Attempts[0].FinishedAt == nil || a.Receipt.Detail != missingActionWorkDetail {
					t.Fatal(a)
				}
				failed := round13Body(t, st, "action", a.ID)
				_, _ = s.DispatchDecision(ctx, p, d.ID)
				_, _ = s.ReconcileAction(ctx, p, a.ID)
				if failed != round13Body(t, st, "action", a.ID) {
					t.Fatal("repeat dispatch/reconcile mutated failed action")
				}
				// Restoring the work cannot make the old approval sendable.
				s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
					return DecisionWork{Revision: "r1", Phase: "backlog"}, nil
				}
				s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "r1", nil }
				if _, err := s.ReconcileAction(ctx, p, a.ID); !errors.Is(err, ErrNothingDelivered) {
					t.Fatal(err)
				}
				if _, err := s.DispatchDecision(ctx, p, d.ID); err != nil {
					t.Fatal(err)
				}
				if failed != round13Body(t, st, "action", a.ID) {
					t.Fatal("restored work resurrected action")
				}
				ack, err := s.AcknowledgeAction(ctx, p, a.ID)
				if err != nil || ack.Status != Acknowledged || ack.Receipt.Detail != a.Receipt.Detail || len(ack.Attempts) != 1 {
					t.Fatalf("%+v %v", ack, err)
				}
			})
		}
	}
}

func TestRound15MissingWorkPreservesAttemptedHandoff(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d := round4Approved(t, s, p)
	var a Action
	if err := st.get(ctx, "action", d.ActionID, &a); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	a.Attempts = []Attempt{{Status: Unknown, SentAt: &now}}
	old := a.Revision
	a.Revision++
	if err := st.cas(ctx, "action", a.ID, old, a); err != nil {
		t.Fatal(err)
	}
	s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) { return DecisionWork{}, ErrNotFound }
	before := round13Body(t, st, "action", a.ID)
	_, err := s.DispatchDecision(ctx, p, d.ID)
	if !errors.Is(err, ErrStale) || before != round13Body(t, st, "action", a.ID) {
		t.Fatalf("uncertain send overwritten: %v", err)
	}
}

func TestRound15ProposalAuthorizationReaderFailure(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Question"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Next?"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
	lease := claimTestTurn(t, s, ctx, agent, turn.ID).LeaseToken
	before := round13Body(t, st, "turn", turn.ID)
	input := TurnProposeInput{DecisionInput: DecisionInput{RequestKey: "proposal", WorkRef: "work:1", Scope: "work.phase", Instruction: "Ready", TargetRevision: "r1", Payload: &ActionPayload{Phase: "ready"}}, LeaseToken: lease}
	for _, permission := range []string{"pm.respond", "pm.propose"} {
		s.deps.Authorize = func(_ context.Context, _ Principal, perm, ref string) error {
			if perm == permission {
				return fmt.Errorf("%w: principal authorization could not be read; retry the request", ErrUnavailable)
			}
			return nil
		}
		raw := round13Post(t, s, agent, "/pm/turns/"+turn.ID+"/decisions", input, 503)
		var out struct {
			Error struct{ Code, Message string }
		}
		if err := json.Unmarshal(raw, &out); err != nil || out.Error.Code != "unavailable" || out.Error.Message != "PM capability is not configured: principal authorization could not be read; retry the request" {
			t.Fatalf("%s: %v", raw, err)
		}
		if before != round13Body(t, st, "turn", turn.ID) {
			t.Fatal("authorization failure mutated turn")
		}
		var count int
		if err := st.db.QueryRow("SELECT count(*) FROM pm_records WHERE kind='decision'").Scan(&count); err != nil || count != 0 {
			t.Fatalf("proposal recorded: %d %v", count, err)
		}
	}
	s.deps.Authorize = func(context.Context, Principal, string, string) error { return nil }
	round13Post(t, s, agent, "/pm/turns/"+turn.ID+"/decisions", input, 200)
}
