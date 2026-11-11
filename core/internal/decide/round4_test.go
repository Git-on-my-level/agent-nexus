package decide

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
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
