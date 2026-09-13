package pm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func round4Approved(t *testing.T, s *Service, p Principal) Decision {
	t.Helper()
	d, err := s.ProposeDecision(context.Background(), p, DecisionInput{RequestKey: "r4", WorkRef: "work:1", Instruction: "Deliver", Scope: "github", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.AnswerDecision(context.Background(), p, d.ID, AnswerInput{Revision: d.Revision, Approve: true, Text: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRound4NeverSentCannotReconcile(t *testing.T) {
	for _, kind := range []string{"stale", "no_path", "legacy_failed_no_path"} {
		t.Run(kind, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			d := round4Approved(t, s, p)
			executed, reconciled := false, false
			s.deps.Execute = func(context.Context, Action) (Receipt, error) { executed = true; return Receipt{}, nil }
			s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
				reconciled = true
				return Receipt{Status: Unknown}, nil
			}
			if kind == "stale" {
				s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "r2", nil }
			} else {
				s.deps.CheckDelivery = func(context.Context, Action) error { return NoDeliveryPath("GitHub") }
			}
			_, err := s.DispatchDecision(ctx, p, d.ID)
			if kind == "stale" && !errors.Is(err, ErrStale) || kind != "stale" && !errors.Is(err, ErrUnavailable) {
				t.Fatal(err)
			}
			var before Action
			if err := st.get(ctx, "action", d.ActionID, &before); err != nil {
				t.Fatal(err)
			}
			if kind == "legacy_failed_no_path" {
				old := before.Revision
				before.Status, before.Receipt = Failed, Receipt{Status: Failed, Detail: "No delivery path"}
				before.Revision++
				if err := st.cas(ctx, "action", before.ID, old, before); err != nil {
					t.Fatal(err)
				}
			}
			if hasSentAttempt(before) {
				t.Fatal("preflight marked sent")
			}
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/actions/"+d.ActionID+"/reconcile", strings.NewReader("{}")))
			var body struct {
				Error struct{ Code, Message string }
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if rr.Code != 400 || body.Error.Code != "invalid_request" || body.Error.Message != ErrNothingDelivered.Error() {
				t.Fatalf("%d %s", rr.Code, rr.Body)
			}
			var after Action
			if err := st.get(ctx, "action", d.ActionID, &after); err != nil {
				t.Fatal(err)
			}
			if executed || reconciled || !reflect.DeepEqual(before, after) {
				t.Fatalf("changed never-sent failure: %+v", after)
			}
		})
	}
}

func TestRound4SentFailureCannotRegressToUnknown(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	d := round4Approved(t, s, p)
	s.deps.Execute = func(context.Context, Action) (Receipt, error) {
		return Receipt{Status: Failed, Detail: "Source refused"}, nil
	}
	a, err := s.DispatchDecision(ctx, p, d.ID)
	if err != nil || !hasSentAttempt(a) {
		t.Fatalf("%+v %v", a, err)
	}
	s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
		return Receipt{Status: Unknown, Detail: "Inconclusive"}, nil
	}
	a, err = s.ReconcileAction(ctx, p, a.ID)
	if err != nil || a.Status != Failed || !a.ReconciliationConflict || a.Receipt.Status != Unknown {
		t.Fatalf("%+v %v", a, err)
	}
	s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
		return Receipt{Status: Verified, ExternalID: "source", EvidenceRefs: []string{"work:1"}, IndependentlyVerified: true}, nil
	}
	a, err = s.ReconcileAction(ctx, p, a.ID)
	if err != nil || a.Status != Verified {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestRound4DispatchRequiresCurrentApprovingHuman(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	d := round4Approved(t, s, p)
	calls := 0
	s.deps.Execute = func(context.Context, Action) (Receipt, error) {
		calls++
		return Receipt{Status: Delivered, ExternalID: "source"}, nil
	}
	for _, caller := range []Principal{{WorkspaceID: p.WorkspaceID, ActorID: "second-human", Human: true}, {WorkspaceID: p.WorkspaceID, ActorID: p.ActorID}} {
		if _, err := s.DispatchDecision(ctx, caller, d.ID); !errors.Is(err, ErrForbidden) {
			t.Fatalf("foreign dispatch: %v", err)
		}
	}
	// Revoke between the first caller check and the handoff preflight recheck.
	revoked := false
	s.deps.Authorize = func(_ context.Context, _ Principal, permission, _ string) error {
		if revoked && permission == "pm.approve" {
			return ErrForbidden
		}
		return nil
	}
	s.deps.CheckDelivery = func(context.Context, Action) error { revoked = true; return nil }
	if _, err := s.DispatchDecision(ctx, p, d.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked dispatch: %v", err)
	}
	if calls != 0 {
		t.Fatal("unauthorized execution")
	}
	revoked = false
	s.deps.CheckDelivery = func(context.Context, Action) error { return nil }
	if _, err := s.DispatchDecision(ctx, p, d.ID); err != nil || calls != 1 {
		t.Fatalf("owner dispatch: %v calls=%d", err, calls)
	}
}

func TestRound4DecisionProvenancePersistsAcrossSupersessionAndReads(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	in := DecisionInput{RequestKey: "human", WorkRef: "work:1", Instruction: "Move", Scope: "github", TargetRevision: "r1"}
	human, err := s.ProposeDecision(ctx, p, in)
	if err != nil || human.ProposedBy != p.ActorID || human.OriginKind != "human" || human.TurnID != "" {
		t.Fatalf("%+v %v", human, err)
	}
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Discuss"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Propose"})
	if err != nil {
		t.Fatal(err)
	}
	in.RequestKey = "pm"
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
	proposed, err := s.ProposeForTurn(ctx, agent, turn.ID, in)
	if err != nil || proposed.ID == human.ID || proposed.ProposedBy != agent.ActorID || proposed.OriginKind != "pm_turn" || proposed.TurnID != turn.ID || proposed.ActorID != p.ActorID {
		t.Fatalf("%+v %v", proposed, err)
	}
	replay, err := s.ProposeForTurn(ctx, agent, turn.ID, in)
	if err != nil || replay.ID != proposed.ID || replay.TurnID != turn.ID {
		t.Fatalf("%+v %v", replay, err)
	}
	var old Decision
	if err := st.get(ctx, "decision", human.ID, &old); err != nil || old.Status != Superseded || old.ProposedBy != p.ActorID || old.OriginKind != "human" {
		t.Fatalf("%+v %v", old, err)
	}
	// Restore service from durable records and inspect GET and paginated list.
	restored, err := NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{Service: restored, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	for _, path := range []string{"/pm/decisions/" + proposed.ID, "/pm/decisions"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != 200 {
			t.Fatalf("%d %s", rr.Code, rr.Body)
		}
		var rows []Decision
		if path == "/pm/decisions" {
			var page Page[Decision]
			if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			rows = page.Items
		} else {
			var d Decision
			if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
				t.Fatal(err)
			}
			rows = []Decision{d}
		}
		found := false
		for _, d := range rows {
			if d.ID == proposed.ID {
				found = true
				if d.ProposedBy != agent.ActorID || d.OriginKind != "pm_turn" || d.TurnID != turn.ID {
					t.Fatal(d)
				}
			}
		}
		if !found {
			t.Fatal("proposal missing")
		}
	}
	// Legacy records omit unknown provenance instead of inventing a proposer.
	legacy := Decision{ID: "legacy", WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, CreatedAt: time.Now()}
	raw, _ := json.Marshal(legacy)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if _, exists := fields["proposed_by"]; exists {
		t.Fatal(string(raw))
	}
}

func TestRound4ValidatedChannelProposalProvenance(t *testing.T) {
	s, _, p, _ := fixture(t)
	origin := Origin{Transport: "telegram", TenantID: "bot", ChannelID: "channel", ExternalUserID: "user"}
	bind(t, s, p, origin, true)
	d, err := s.ProposeDecision(context.Background(), p, DecisionInput{RequestKey: "channel", WorkRef: "work:1", Instruction: "Move", Scope: "github", TargetRevision: "r1", Origin: &origin})
	if err != nil || d.ProposedBy != p.ActorID || d.OriginKind != "channel" || d.TurnID != "" {
		t.Fatalf("%+v %v", d, err)
	}
}
