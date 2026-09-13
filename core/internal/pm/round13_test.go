package pm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func round13Post(t *testing.T, s *Service, p Principal, path string, in any, status int) []byte {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", path, bytes.NewReader(raw)))
	if rr.Code != status {
		t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
	}
	return rr.Body.Bytes()
}

func round13Body(t *testing.T, st *Store, kind, id string) string {
	t.Helper()
	var body string
	if err := st.db.QueryRow(`SELECT body FROM pm_records WHERE kind=? AND id=?`, kind, id).Scan(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestRound13ProposalPriority(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		firstPM, secondPM, changed, sameKey bool
	}{
		{name: "pm_reuses_human", secondPM: true},
		{name: "pm_reuses_human_same_key", secondPM: true, sameKey: true},
		{name: "human_reuses_pm", firstPM: true},
		{name: "human_reuses_pm_same_key", firstPM: true, sameKey: true},
		{name: "different_pm_cannot_replace_human", secondPM: true, changed: true},
		{name: "human_board_move_replaces_pm", firstPM: true, changed: true},
		{name: "human_replaces_human", changed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Discuss"})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Propose"})
			if err != nil {
				t.Fatal(err)
			}
			agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
			claimTestTurn(t, s, ctx, agent, turn.ID)
			token := testTurnLease(t, s, ctx, turn.ID)
			in := DecisionInput{RequestKey: "first", WorkRef: "work:1", Scope: "work.phase", Instruction: "Move to blocked", TargetRevision: "r1", Payload: &ActionPayload{Phase: "blocked"}}
			var prior Decision
			if tc.firstPM {
				prior, err = s.ProposeForTurn(ctx, agent, turn.ID, in, token)
			} else {
				prior, err = s.ProposeDecision(ctx, p, in)
			}
			if err != nil {
				t.Fatal(err)
			}
			before := round13Body(t, st, "decision", prior.ID)
			turnBefore := round13Body(t, st, "turn", turn.ID)
			if !tc.sameKey {
				in.RequestKey = "second"
			}
			if tc.changed {
				in.Instruction = "Move to ready"
				in.Payload = &ActionPayload{Phase: "ready"}
			}
			caller, path, request := p, "/pm/decisions", any(in)
			want := 200
			if tc.changed {
				want = 201
			}
			if tc.secondPM {
				caller, path, request = agent, "/pm/turns/"+turn.ID+"/decisions", TurnProposeInput{DecisionInput: in, LeaseToken: token}
				if tc.changed {
					want = 409
				}
			}
			raw := round13Post(t, s, caller, path, request, want)
			var count int
			if err := st.db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='decision'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if want == 409 {
				var response struct {
					Error struct {
						Code, Message string
						Details       struct {
							PendingDecisionID string `json:"pending_decision_id"`
						}
					}
				}
				if err := json.Unmarshal(raw, &response); err != nil {
					t.Fatal(err)
				}
				if response.Error.Code != "human_proposal_pending" || response.Error.Details.PendingDecisionID != prior.ID || response.Error.Message != (&HumanProposalPendingError{PendingDecisionID: prior.ID}).Error() {
					t.Fatalf("%s", raw)
				}
				if count != 1 || before != round13Body(t, st, "decision", prior.ID) || turnBefore != round13Body(t, st, "turn", turn.ID) {
					t.Fatal("rejection mutated decision or turn")
				}
				return
			}
			var got Decision
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if !tc.changed {
				if count != 1 || got.ID != prior.ID || got.ProposedBy != prior.ProposedBy || got.OriginKind != prior.OriginKind || got.TurnID != prior.TurnID || !reflect.DeepEqual(got.Origin, prior.Origin) || !bytes.Contains(raw, []byte(`"replayed":true`)) || got.Supersedes != "" || before != round13Body(t, st, "decision", prior.ID) {
					t.Fatalf("reuse changed authorship or record: %s", raw)
				}
				if tc.secondPM {
					var linked Turn
					if err := st.get(ctx, "turn", turn.ID, &linked); err != nil {
						t.Fatal(err)
					}
					if len(linked.DecisionIDs) != 1 || linked.DecisionIDs[0] != prior.ID {
						t.Fatal(linked)
					}
				}
			} else {
				var old Decision
				if err := st.get(ctx, "decision", prior.ID, &old); err != nil {
					t.Fatal(err)
				}
				if count != 2 || got.Supersedes != prior.ID || old.Status != Superseded || old.SupersededBy != got.ID || strings.Contains(old.SupersededReason, "origin") {
					t.Fatalf("replacement: %+v %+v", got, old)
				}
			}
		})
	}
}

func TestRound13NonHumanOriginGuard(t *testing.T) {
	for _, origin := range []string{"channel", "pm_turn", "future_agent", ""} {
		for _, scope := range []string{"assignment", "priority"} {
			t.Run(origin+"/"+scope, func(t *testing.T) {
				s, st, p, _ := fixture(t)
				ctx := context.Background()
				prior, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "human", WorkRef: "work:1", Scope: "assignment", Instruction: "Assign", TargetRevision: "r1"})
				if err != nil {
					t.Fatal(err)
				}
				d := prior
				d.ID = "candidate"
				d.OriginKind = origin
				d.ProposedBy = "agent"
				d.Instruction = "Different"
				d.Scope = scope
				_, _, err = st.proposeDecision(ctx, d, "", "")
				var pending *HumanProposalPendingError
				if !errors.As(err, &pending) || pending.PendingDecisionID != prior.ID {
					t.Fatalf("%v", err)
				}
				// Origin alone is not changed intent, including a different channel destination.
				d.Scope = prior.Scope
				d.Instruction = prior.Instruction
				d.Origin = &Origin{Transport: "telegram", ChannelID: "another-chat"}
				reused, inserted, err := st.proposeDecision(ctx, d, "", "")
				if err != nil || inserted || reused.ID != prior.ID || reused.OriginKind != "human" || reused.Origin != nil {
					t.Fatalf("%+v %v %v", reused, inserted, err)
				}
			})
		}
	}
}

func TestRound13ApprovalTarget(t *testing.T) {
	for _, tc := range []struct {
		name, revision, phase, reason string
		readErr                       error
		noReader                      bool
	}{
		{name: "current", revision: "r1", phase: "backlog"},
		{name: "stale", revision: "r2", phase: "backlog", reason: "revision_changed"},
		{name: "already_at_target", revision: "r1", phase: "blocked", reason: "already_at_target"},
		{name: "missing", readErr: ErrNotFound, reason: "work_missing"},
		{name: "reader_error", readErr: errors.New("private backend error"), reason: "work_read_failed"},
		{name: "reader_unconfigured", noReader: true, reason: "work_read_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "proposal", WorkRef: "work:1", Scope: "work.phase", Instruction: "Block", TargetRevision: "r1", Payload: &ActionPayload{Phase: "blocked"}})
			if err != nil {
				t.Fatal(err)
			}
			s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
				return DecisionWork{Revision: tc.revision, Phase: tc.phase}, tc.readErr
			}
			if tc.noReader {
				s.deps.DecisionWork = nil
			}
			before := round13Body(t, st, "decision", d.ID)
			answer := AnswerInput{Revision: 1, Approve: true, Text: "yes"}
			want := 200
			if tc.reason != "" {
				want = 409
			}
			raw := round13Post(t, s, p, "/pm/decisions/"+d.ID+"/answer", answer, want)
			if want == 409 {
				var response struct {
					Error struct {
						Code, Message string
						Details       ApprovalTargetError
					}
				}
				if err := json.Unmarshal(raw, &response); err != nil {
					t.Fatal(err)
				}
				details := response.Error.Details
				current := "unavailable"
				if details.CurrentRevision != nil {
					current = *details.CurrentRevision
				}
				expectedMessage := fmt.Sprintf("Proposal target has changed (proposed at r1, source now %s). Approval refused (%s); decline it or wait for a fresh proposal.", current, tc.reason)
				if response.Error.Code != "source_revision_changed" || details.Reason != tc.reason || details.ApprovedRevision != "r1" || details.OriginKind != d.OriginKind || details.ProposedBy != d.ProposedBy || response.Error.Message != expectedMessage {
					t.Fatalf("%s", raw)
				}
				if tc.readErr != nil || tc.noReader {
					if details.CurrentRevision != nil {
						t.Fatal(details)
					}
				} else if details.CurrentRevision == nil || *details.CurrentRevision != tc.revision {
					t.Fatal(details)
				}
				if before != round13Body(t, st, "decision", d.ID) {
					t.Fatal("rejection mutated decision")
				}
				var actions int
				if err := st.db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='action'`).Scan(&actions); err != nil {
					t.Fatal(err)
				}
				if actions != 0 {
					t.Fatal("rejection created action")
				}
				answer.Approve = false
				answer.Text = "decline"
				round13Post(t, s, p, "/pm/decisions/"+d.ID+"/answer", answer, 200)
			}
			var recorded Decision
			if err := st.get(ctx, "decision", d.ID, &recorded); err != nil {
				t.Fatal(err)
			}
			if answer.Approve {
				if recorded.Status != Answered || recorded.ActionID == "" {
					t.Fatal(recorded)
				}
			} else if recorded.Status != Declined || recorded.ActionID != "" {
				t.Fatal(recorded)
			}
			// Recorded answers also replay when the current work is stale and moot.
			s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
				return DecisionWork{Revision: "r2", Phase: "blocked"}, nil
			}
			round13Post(t, s, p, "/pm/decisions/"+d.ID+"/answer", answer, 200)
			// Recorded approval/decline replays survive missing and unreadable work;
			// conflicting answers still return the existing state-conflict response.
			saved := round13Body(t, st, "decision", d.ID)
			for _, readErr := range []error{ErrNotFound, errors.New("unavailable")} {
				s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) { return DecisionWork{}, readErr }
				round13Post(t, s, p, "/pm/decisions/"+d.ID+"/answer", answer, 200)
				changed := answer
				changed.Text = "different"
				raw = round13Post(t, s, p, "/pm/decisions/"+d.ID+"/answer", changed, 409)
				if !bytes.Contains(raw, []byte(`"code":"conflict"`)) || saved != round13Body(t, st, "decision", d.ID) {
					t.Fatalf("replay: %s", raw)
				}
			}
		})
	}
}
