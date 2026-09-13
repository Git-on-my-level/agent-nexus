package pm

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestRound16StaleHTTPDetails(t *testing.T) {
	for _, origin := range []string{"human", "pm_turn", "channel", ""} {
		for _, path := range []string{"answer", "dispatch", "dispatch_missing", "dispatch_executor", "reconcile_missing", "reconcile_read_failed", "reconcile_executor"} {
			t.Run(origin+"/"+path, func(t *testing.T) {
				s, st, p, _ := fixture(t)
				ctx := context.Background()
				d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "r16", WorkRef: "work:1", Instruction: "Block", Scope: "work.phase", TargetRevision: "r1", Payload: &ActionPayload{Phase: "blocked"}})
				if err != nil {
					t.Fatal(err)
				}
				// Seed each durable provenance, including legacy absent fields.
				d.OriginKind, d.ProposedBy = origin, p.ActorID
				if origin == "pm_turn" || origin == "channel" {
					d.ProposedBy = "pm-agent"
				} else if origin == "" {
					d.ProposedBy = ""
				}
				old := d.Revision
				d.Revision++
				if err := st.cas(ctx, "decision", d.ID, old, d); err != nil {
					t.Fatal(err)
				}
				if path != "answer" {
					d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: d.Revision, Approve: true, Text: "yes"})
					if err != nil {
						t.Fatal(err)
					}
				}
				endpoint := "/pm/decisions/" + d.ID + "/dispatch"
				var input any = struct{}{}
				reason, current := "revision_changed", "r2"
				message := "Approved source revision has changed (approved at r1, source now r2). Approval refused (revision_changed); inspect the work and create a fresh proposal if needed."
				s.deps.Execute = func(context.Context, Action) (Receipt, error) {
					t.Fatal("unexpected source write")
					return Receipt{}, nil
				}
				s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
					t.Fatal("unexpected source read")
					return Receipt{}, nil
				}
				switch path {
				case "answer":
					endpoint = "/pm/decisions/" + d.ID + "/answer"
					input = AnswerInput{Revision: d.Revision, Approve: true, Text: "yes"}
					s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
						return DecisionWork{Revision: "r2"}, nil
					}
					message = "Proposal target has changed (proposed at r1, source now r2). Approval refused (revision_changed); decline it or wait for a fresh proposal."
				case "dispatch":
					s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "r2", nil }
				case "dispatch_executor":
					s.deps.Execute = func(context.Context, Action) (Receipt, error) { return Receipt{}, ErrStale }
					current, message = "", ErrStale.Error()
				case "dispatch_missing", "reconcile_missing":
					s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) { return DecisionWork{}, ErrNotFound }
					reason, current = "work_missing", ""
					message = "Approved source revision has changed (approved at r1, source now unavailable). Approval refused (work_missing); inspect the work and create a fresh proposal if needed."
					if path == "reconcile_missing" {
						message = "Nothing was delivered for this approval: the task it refers to no longer exists, so there is nothing to read back."
					}
				case "reconcile_read_failed":
					s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
						return DecisionWork{}, errors.New("private read failure")
					}
					reason, current = "work_read_failed", ""
					message = "Could not read back this approval: the task it refers to could not be read. Retry reconciliation."
				case "reconcile_executor":
					var a Action
					if err := st.get(ctx, "action", d.ActionID, &a); err != nil {
						t.Fatal(err)
					}
					old := a.Revision
					a.Status, a.Revision = Unknown, a.Revision+1
					if err := st.cas(ctx, "action", a.ID, old, a); err != nil {
						t.Fatal(err)
					}
					s.deps.Reconcile = func(context.Context, Action) (Receipt, error) { return Receipt{}, ErrStale }
					current, message = "", ErrStale.Error()
				}
				reconcile := path == "reconcile_missing" || path == "reconcile_read_failed" || path == "reconcile_executor"
				if reconcile {
					endpoint = "/pm/actions/" + d.ActionID + "/reconcile"
				}
				decisionBefore := round13Body(t, st, "decision", d.ID)
				actionBefore := ""
				if reconcile {
					actionBefore = round13Body(t, st, "action", d.ActionID)
				}
				raw := round13Post(t, s, p, endpoint, input, 409)
				var out struct {
					Error struct {
						Code, Message string
						Details       map[string]any
					}
				}
				if err := json.Unmarshal(raw, &out); err != nil {
					t.Fatal(err)
				}
				details := out.Error.Details
				if out.Error.Code != "source_revision_changed" || out.Error.Message != message || details["reason"] != reason || details["approved_revision"] != "r1" || details["origin_kind"] != d.OriginKind || details["proposed_by"] != d.ProposedBy {
					t.Fatalf("unexpected error: %s", raw)
				}
				if revision, exists := details["current_revision"]; !exists || (current == "" && revision != nil) || (current != "" && revision != current) {
					t.Fatalf("revision: %s", raw)
				}
				if decisionBefore != round13Body(t, st, "decision", d.ID) {
					t.Fatal("error mutated decision")
				}
				if reconcile && actionBefore != round13Body(t, st, "action", d.ActionID) {
					t.Fatal("reconcile error mutated action")
				}
				if path == "dispatch" {
					var a Action
					if err := st.get(ctx, "action", d.ActionID, &a); err != nil {
						t.Fatal(err)
					}
					if a.Status != Failed || len(a.Attempts) != 1 || a.Attempts[0].SentAt != nil || a.Attempts[0].FinishedAt == nil || a.Attempts[0].Status != Failed || !reflect.DeepEqual(a.Attempts[0].Receipt, a.Receipt) {
						t.Fatalf("failed evidence: %+v", a)
					}
					if a.Receipt.Detail != "Approved source revision has changed (approved at r1, source now r2). This approval will not be sent; a fresh proposal and approval are needed." {
						t.Fatal(a.Receipt.Detail)
					}
					before := round13Body(t, st, "action", a.ID)
					raw = round13Post(t, s, p, "/pm/actions/"+a.ID+"/reconcile", struct{}{}, 400)
					if err := json.Unmarshal(raw, &out); err != nil || out.Error.Message != ErrNothingDelivered.Error() {
						t.Fatalf("failed read-back: %s (%v)", raw, err)
					}
					if before != round13Body(t, st, "action", a.ID) {
						t.Fatal("failed read-back mutated action")
					}
				}
			})
		}
	}
}
