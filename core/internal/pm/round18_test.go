package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRound18ClaimCapacityHTTP(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		held, waiting, status int
	}{
		{"capacity_with_waiting", 2, 18, 429},
		{"capacity_without_waiting", 2, 0, 204},
		{"below_capacity", 1, 1, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st, human, _ := fixture(t)
			ctx := context.Background()
			agent := Principal{WorkspaceID: human.WorkspaceID, ActorID: "pm-agent"}
			now := time.Now().UTC()
			seed := func(id string, held bool, actor, workspace string, status Status, deadline time.Time) {
				t.Helper()
				turn := Turn{ID: id, WorkspaceID: workspace, ActorID: human.ActorID, AgentActorID: actor, Status: status, Deadline: deadline, Revision: 1}
				if held {
					turn.LeaseToken, turn.LeaseOwner, turn.LeaseExpiresAt = "token-"+id, "runner-"+id, now.Add(time.Minute)
				}
				if _, err := st.insert(ctx, "turn", id, workspace, human.ActorID, id, turn); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < tc.held; i++ {
				seed(fmt.Sprintf("held-%d", i), true, agent.ActorID, agent.WorkspaceID, Sending, now.Add(time.Minute))
			}
			for i := 0; i < tc.waiting; i++ {
				status := Sending
				if i%2 == 1 {
					status = Unknown
				}
				seed(fmt.Sprintf("waiting-%d", i), false, agent.ActorID, agent.WorkspaceID, status, now.Add(time.Minute))
			}
			// These must not inflate eligible waiting counts or turn an empty queue into busy.
			seed("other-actor", false, "other-agent", agent.WorkspaceID, Sending, now.Add(time.Minute))
			seed("other-workspace", true, agent.ActorID, "other-workspace", Sending, now.Add(time.Minute))
			seed("expired", false, agent.ActorID, agent.WorkspaceID, Sending, now.Add(-time.Minute))
			seed("terminal", false, agent.ActorID, agent.WorkspaceID, Failed, now.Add(time.Minute))
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/turns/claim", strings.NewReader(`{"runner_id":"new-runner"}`)))
			if rr.Code != tc.status {
				t.Fatalf("%d %s", rr.Code, rr.Body)
			}
			switch tc.status {
			case 429:
				var out map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{"error": map[string]any{"code": "busy", "message": "Workspace PM capacity reached (2 in flight; limit 2)", "details": map[string]any{"reason": "capacity", "in_flight": float64(2), "limit": float64(2), "waiting": float64(18)}}}
				if !reflect.DeepEqual(out, want) {
					t.Fatalf("%s", rr.Body)
				}
				var waiting Turn
				if err := st.get(ctx, "turn", "waiting-0", &waiting); err != nil {
					t.Fatal(err)
				}
				if waiting.LeaseToken != "" || waiting.Revision != 1 {
					t.Fatalf("busy mutated waiting turn: %+v", waiting)
				}
			case 204:
				if rr.Body.Len() != 0 {
					t.Fatalf("204 body: %s", rr.Body)
				}
			case 200:
				var turn Turn
				if err := json.Unmarshal(rr.Body.Bytes(), &turn); err != nil {
					t.Fatal(err)
				}
				if turn.ID != "waiting-0" || turn.LeaseToken == "" || turn.LeaseOwner != "new-runner" {
					t.Fatalf("invalid lease: %+v", turn)
				}
			}
			// Recovery must still work with or without waiting work at the cap.
			recovered, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "runner-held-0"})
			if err != nil || recovered.LeaseToken != "token-held-0" {
				t.Fatalf("recovery: %+v %v", recovered, err)
			}
		})
	}
}

func TestRound18HumanPriorityScopeAndChannel(t *testing.T) {
	for _, channel := range []bool{false, true} {
		for _, scope := range []string{"work.phase", "work.annotate"} {
			t.Run(fmt.Sprintf("channel=%v/scope=%s", channel, scope), func(t *testing.T) {
				s, st, p, _ := fixture(t)
				ctx := context.Background()
				input := func(key, scope string) DecisionInput {
					in := DecisionInput{RequestKey: key, WorkRef: "work:1", Scope: scope, Instruction: key, TargetRevision: "r1"}
					if scope == "work.annotate" {
						in.Instruction = fmt.Sprintf(`{"next_action":%q}`, key)
					}
					if scope == "work.phase" {
						in.Payload = &ActionPayload{Phase: "blocked"}
					}
					return in
				}
				in := input("human", scope)
				if channel {
					origin := Origin{Transport: "telegram", TenantID: "bot", ChannelID: "channel", ExternalUserID: "user"}
					bind(t, s, p, origin, true)
					in.Origin = &origin
				}
				var human Decision
				if err := json.Unmarshal(round13Post(t, s, p, "/pm/decisions", in, 201), &human); err != nil {
					t.Fatal(err)
				}
				if human.OriginKind != "human" || human.ProposedBy != p.ActorID || !reflect.DeepEqual(human.Origin, in.Origin) {
					t.Fatalf("provenance: %+v", human)
				}
				var saved Decision
				if err := st.get(ctx, "decision", human.ID, &saved); err != nil {
					t.Fatal(err)
				}
				if saved.OriginKind != "human" || !reflect.DeepEqual(saved.Origin, in.Origin) {
					t.Fatalf("stored provenance: %+v", saved)
				}
				before := round13Body(t, st, "decision", human.ID)
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
				path := "/pm/turns/" + turn.ID + "/decisions"
				otherScope := "work.annotate"
				if scope == otherScope {
					otherScope = "work.phase"
				}
				round13Post(t, s, agent, path, TurnProposeInput{DecisionInput: input("other-scope", otherScope), LeaseToken: token}, 200)
				raw := round13Post(t, s, agent, path, TurnProposeInput{DecisionInput: input("same-scope", scope), LeaseToken: token}, 409)
				var out struct {
					Error struct {
						Code, Message string
						Details       HumanProposalPendingError
					}
				}
				if err := json.Unmarshal(raw, &out); err != nil {
					t.Fatal(err)
				}
				if out.Error.Code != "human_proposal_pending" || out.Error.Details.PendingDecisionID != human.ID || out.Error.Message != (&HumanProposalPendingError{PendingDecisionID: human.ID}).Error() {
					t.Fatalf("%s", raw)
				}
				if round13Body(t, st, "decision", human.ID) != before {
					t.Fatal("human proposal changed")
				}
				if channel {
					// A human using the channel can still replace their pending proposal.
					in.RequestKey, in.Instruction = "human-replacement", "Updated request"
					if scope == "work.annotate" {
						in.Instruction = `{"next_action":"updated request"}`
					}
					var replacement Decision
					if err := json.Unmarshal(round13Post(t, s, p, "/pm/decisions", in, 201), &replacement); err != nil {
						t.Fatal(err)
					}
					if replacement.OriginKind != "human" || replacement.Supersedes != human.ID || !reflect.DeepEqual(replacement.Origin, in.Origin) {
						t.Fatalf("channel human replacement: %+v", replacement)
					}
				}
				// The same scope on a different work ref remains independent too.
				differentWork := input("other-work", scope)
				differentWork.WorkRef = "work:2"
				round13Post(t, s, agent, path, TurnProposeInput{DecisionInput: differentWork, LeaseToken: token}, 200)
			})
		}
	}
}
